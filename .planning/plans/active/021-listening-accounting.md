# 021 — Listening accounting: smart plays, skips, and a real history

**Issue:** none yet — open one before the first edit (tracker is the
source of truth; `./scripts/issue.sh search "skip play count"` comes
back empty as of this writing).

**Status:** plan — not started.

**Relates:** play-count rendering (`frontend/src/components/track-list/columns.ts`),
smart playlists (`backend/smartplaylist/`), the event contract
(`TrackPlayCountChanged`), and any future Wrapped / "minutes listened"
surface.

---

## What exists now

Three facts, all load-bearing.

**A "play" is recorded only on a natural finish.** `recordPlay`
(`backend/queue/playhistory.go:9`) is called from exactly one place —
`OnPlaybackFinished` (`backend/queue/handlers.go:14`), and only when
`srcErr == nil`. A track the user skips past at 90% is *not* a play;
neither is one they pause at 60% and abandon. `play_count` /
`last_played` on `audio_files` reflect "finished to the end," nothing
more.

**There is no skip concept at all.** Skipping is indistinguishable
from a natural finish, a pause, or a shutdown. Nothing records "the
user rejected this track," so no downstream feature (smart playlists,
shuffle, the revisit shelf, a future skip-rate heuristic) can ask
about it.

**`play_history` is a write-only log.** It holds
`(audio_file_id, played_at)` and nothing reads it — no sqlc query
touches it, no `PlayHistory` read path exists. Its only recorded
purpose is the timestamps a future "minutes listened over time"
feature would need. It is classified `Authored, Cascade` in
`backend/datamap/datamap.go:272` ("Listening history").

So the gaps are: (1) skips are invisible, and (2) "played" is
under-counted — the opposite of the usual over-counting fear. The
scrobble intuition (count a play once `min(50%, 4:00)` has been
*heard*, independent of how it ends) is the fix for both.

---

## What we're building

A single classification of every track *exit*, plus one row per exit in
a listening log, plus the existing denormalized `play_count` /
`last_played` updated to match the new meaning. Three exit kinds:

| kind | condition |
|---|---|
| `complete` | reached natural end, **or** abandoned with `remaining <= tail` |
| `play` | heard `>= playThreshold`, abandoned before the tail |
| `skip` | user moved to a *different* track before `playThreshold` |

Not counted, not any kind: decode failure, pause/stop/shutdown before
the threshold, and tracks shorter than `minTrackLength`.

### The thresholds — named judgements, one file

Follow the `PreviousRestartThreshold` precedent (`backend/queue/queue.go:28`,
a bare `const` with a comment). A new `backend/queue/listen.go` (or a
tiny `backend/listencount` package) declares:

```go
const (
    // A track this short is deliberated jingle / interstitial and is
    // never counted, either way.
    minTrackLength = 30 * time.Second
    // The scrobble rule: half the track, or four minutes, whichever
    // comes first (Last.fm / ListenBrainz).
    playThresholdMax = 4 * time.Minute
    // "Finished enough": within 15s of the end, or the last 10%,
    // whichever is larger.  A 10:00 ambient track gets a 60s fade
    // window; a 2:00 pop song gets 15s.
    tailWindowFloor    = 15 * time.Second
    tailWindowFraction = 0.10
)

func playThreshold(d time.Duration) time.Duration {
    return min(d/2, playThresholdMax)
}
func tailWindow(d time.Duration) time.Duration {
    return max(d/10, tailWindowFloor)
}
```

Classification is a pure function of `(reason, position, duration)` and
*therefore unit-testable without a player*:

```go
func classify(reason ExitReason, pos, dur time.Duration) Kind
```

`ExitReason` is `finished | skipped | failed | abandoned`. `skipped`
means the queue moved to a different track by user action (Next,
Previous past the restart threshold, PlayIndex, queue replacement,
select-from-a-list). `failed` is the decode-error path. `abandoned` is
pause/stop/unload/shutdown — and in v1 is a no-op (see open question 3).

**"Heard" is approximated by the position at exit.** We read
`player.CurrentPositionSeconds()` at the moment of the transition, not
an accumulated listen-time ledger. A user who seeks to 80% and listens
5 seconds reads as "heard 80%." That is deliberately accepted for v1:
it is how most players actually behave, it is drastically simpler, and
the failure mode ("counted a track you skimmed as played") is mild and
exactly what the scrobble threshold already forgives. Written down
because "position is not listen time" is the one assumption that will
look like a bug if it is not.

**Fires once per listen.** Leaving a track already leaves it; the
`chainID` guard in `player.onPlaybackFinished` (`backend/player/player.go:633`)
already swallows a stale finish callback, and a transition advances
`currentIndex` past the finished track. The classifier needs the same
guard so a Next-then-stale-finish cannot produce two rows. Key it on the
`(audioFileID, chainID)` the transition was about.

---

## Schema — resolved: fresh design, no migration

The A/B migration agonizing is moot.  This app has two users and both
are devs, and play counts are explicitly not worth preserving yet — so
the schema is written as if listening accounting had been designed in
from the start, and the existing two databases rebuild what they need
(see below).  There is no migration step and none is re-introduced.

**`play_history` is renamed to `listening_events`** and grows the three
kinds, plus the raw position/duration the classification was made from:

```sql
CREATE TABLE IF NOT EXISTS listening_events (
    id               INTEGER PRIMARY KEY,
    audio_file_id    INTEGER NOT NULL,
    kind             TEXT NOT NULL DEFAULT 'complete'
                     CHECK (kind IN ('complete','play','skip')),
    position_seconds INTEGER NOT NULL DEFAULT 0,
    duration_seconds INTEGER NOT NULL DEFAULT 0,
    occurred_at      DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY(audio_file_id) REFERENCES audio_files(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_listening_events_audio_file_id
    ON listening_events(audio_file_id);
CREATE INDEX IF NOT EXISTS idx_listening_events_occurred_at
    ON listening_events(occurred_at);
```

`position_seconds`/`duration_seconds` are kept raw so a future re-tune
of the threshold does not force the events to be re-recorded.  `kind`
stays the write-time classification; the raw reading is evidence, not
a second copy of the rule.

**The counters are denormalized onto `audio_files`** — `skip_count` /
`last_skipped` join the existing `play_count` / `last_played`, because
that is where the hot read path already lives and a log join per track
row is not acceptable.  This does grow the MIXED-KIND wart (see the
survey below for the structural answer), but it is the *continuation* of
the existing design, not a new leak: play counts sat on `audio_files`
from before this feature existed.

**What happens to the two real databases on next launch.**
`listening_events` is a new table, created verbatim.  `audio_files`
gains two columns, which `retireStaleTables` treats as a stale Owned
table and rebuilds by rescan — dropping `play_count` / `last_played` /
`tag_status` with it, which is the accepted cost stated in the issue.
`play_history` is gone from the schema and the datamap, so
`obsoleteTables` drops it; its (natural-finish-only) timestamp rows go
with it.  Nothing here is wrong on a fresh install, and on the two dev
machines the answer is the documented "delete and rescan."

---

## Wiring: where the classifier is called

The risk is not the classifier — it is that **every track-replacement
path must classify the outgoing track**, and there are many: `Next`,
`Previous` (past the 3s restart threshold), `PlayIndex`, `playFromStart`,
`SetQueue` / clear-and-play, remove-current, and select-from-a-list.
Miss one and that path silently never records a skip.

So the classification is centralized in one queue method —

```go
// leaveCurrent(reason) classifies the track at currentIndex as it is
// about to be replaced, and records exactly one listening event.
// Must be called without q.mu held (it writes to SQLite).
func (q *Queue) leaveCurrent(reason ExitReason)
```

— which reads position/duration from the player, calls `classify`, and
emits the play/skip row + `TrackPlayCountChanged` when `kind != skip`.
`OnPlaybackFinished(nil)` routes through `leaveCurrent(finished)`, the
navigation methods route through `leaveCurrent(skipped)` before they
advance, and `recordPlay` becomes the "did a play happen" half of it.

Because "one path forgot to call it" is the failure mode, a **source
sweep** pins it, on the pattern of `TestNoDirectRuntimeEmits`
(`backend/events/noemit_test.go`) and `TestCatalogCoversSchema`: a test
walks `backend/queue` for assignments to `currentIndex` (and the
`SetQueue` / remove paths) and fails if a mutation site does not sit
adjacent to a `leaveCurrent` call. The sweep is the enforcement; the
central method is the convenience.

`recordPlay` keeps its existing contract *when a play happens* —
`TrackPlayCountChanged` with `{audioFileId, filePath, playCount,
lastPlayed}` — so the frontend patch path and
`playhistory_test.go` keep passing. A skip emits no per-track event in
v1 (open question 4).

---

## Phases

1. **The classifier.** `listen.go`: the constants, `playThreshold`,
   `tailWindow`, `classify`. Table-driven unit tests covering every
   cell of the tristate, the <30s exemption, the tail window on both a
   10:00 and a 2:00 track, and the clip at the 4:00 cap. No I/O.
2. **Schema.** *Done in this session.* `listening_events` replaces
   `play_history`; `skip_count` / `last_skipped` added to
   `audio_files`; datamap entry and `TestAuthoredCascadesAreDeliberate`
   allow-list renamed; `recordPlay` writes `listening_events
   ('complete')`.  `make generate` run; database / datamap / queue
   tests green.
3. **Wiring.** `leaveCurrent`, the navigation/finish/error call sites,
   the `fires once per listen` guard, and the source sweep. Extend
   `playhistory_test.go` for skip/complete classification through the
   queue rather than the pure function.
4. **Smart-playlist field.** `skip_count` (and optionally
   `days_since_skipped`) in `smartplaylist.go` field/numeric maps and
   the editor's field list, via subquery. A frontend event for skip —
   if a UI wants a skip column — follows separately.

## Verification

- **Go:** the classifier is pure and exhaustively unit-tested; the
  queue wiring is tested in-process with `events.WithSink`
  (`backend/queue/emit_test.go` is the model), asserting a Next at 90%
  emits a *play*, a Next at 10% emits a *skip and no play*, a natural
  finish emits a *complete*.
- **Database:** schema + datamap tests fail-loud on any new or
  reclassified table; `database_test.go`'s listening-events round-trip
  asserts the new table and the four denormalized counter columns.
- **e2e:** `e2e/specs/play-count.spec.ts` already awaits
  `TrackPlayCountChanged`; add the skip case (advance early, assert no
  `TrackPlayCountChanged` and a `skip` row via the `__/test/sql`
  endpoint if convenient, or via the playlist effect).
- No visual/component tier needed unless a skip column ships (phase 4).

## Open questions / decisions needed

1. **Migration mechanism.** Resolved — fresh design, no migration (see the schema section).  Play counts are not worth preserving, both users are devs, and `audio_files` / `play_history` rebuild-or-drop on next launch.
2. **"Position is not listen time."** Accept the approximation for v1,
   or track accumulated listen seconds (a real ledger on the player) now?
3. **Abandon on shutdown.** A track paused at 70% and then app-killed:
   count a `play` (scrobble says heard) or leave it unrecorded? v1
   proposes *unrecorded* — same as today — to keep the write path off
   the shutdown critical path.
4. **Skip event to the frontend.** Emit now (parallel to
   `TrackPlayCountChanged`) or only when a surface consumes it?
