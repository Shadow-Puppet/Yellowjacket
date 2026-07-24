# Autotag (v1.3) — MusicBrainz Autotagger

The MusicBrainz autotagger, collectively **v1.3**. Builds on the explore-browser API client + cache foundation. Five sequential phases (008–012), each depending on the prior one.

| Phase | Title | Status |
|-------|-------|--------|
| 008 | Schema & Grouping Foundation | shipped 2026-04-20 |
| 009 | Scoring Engine & MB Orchestration | shipped 2026-04-21 |
| 010 | Review UI & Apply Pipeline | **active** |
| 011 | Auto-Accept & Entry Points | pending |
| 012 | Settings & Polish | pending |

---

## 008 — Schema & Grouping Foundation · shipped 2026-04-20

> First v1.3 phase. Lays down the schema + bookkeeping — no scoring, no UI, no MB calls yet.

### What landed

- **008.1 — Migration 31: `audio_files.tag_status`.** Text column with inline `CHECK(tag_status IN (...))` constraint (SQLite `ALTER TABLE ADD COLUMN` supports column constraints, so both fresh and upgraded DBs enforce it). Partial index `idx_audio_files_tag_status_untagged ON audio_files(library_id) WHERE tag_status = 'untagged'` powers the pending badge. Backfill sets `user_confirmed` where the joined recording already has an MBID; everything else defaults to `untagged`.
- **008.2 — Migration 32: `tagging_items` + `group_key`.** New `tagging_items` table (PK `group_key`, `status CHECK IN ('pending','matched','confirmed','skipped')`, two indexes including the partial `WHERE status = 'pending'` for the badge). `audio_files.group_key TEXT NOT NULL DEFAULT ''` with partial index `WHERE group_key != ''`. Column-referencing indexes live in the migration, not the schema file, because `CREATE TABLE IF NOT EXISTS` is a no-op on existing tables and the partial-index predicate would reference a column that hasn't been added yet.
- **008.3 — `backend/autotag.GroupKey` + scan-path integration.** Lower-case hex SHA-1 over `libraryID || 0 || parentDirLower || 0 || albumTrimmed || 0 || discNumber` — intentionally shallow, deeper normalization is scoring territory (009). `saveAudioFile` switched to `CreateAudioFileWithGroupKey` + `UpsertTaggingItemOnTrackAdd`. `updateAudioFileMetadata` rebinds on key change: decrement old group → delete if empty → upsert new group → write new key onto the audio_files row. Migration 32 streams existing rows in batches of 500 and aggregates `tagging_items` at the end, defaulting status to `confirmed` when every track in the group is `user_confirmed`.
- **008.4 — Pending-queue sqlc queries.** `CountPendingTaggingItems`, three `ListPendingTaggingItems...` variants (alphabetical, by-score nulls-last, by-recent) — sqlc has no dynamic ORDER BY so each sort has its own query. `CAST(@param AS INTEGER|TEXT)` hints give typed params (otherwise sqlc emits `interface{}`). `GetTaggingItem` and `ListAudioFilesInTaggingGroup` round out the queue API. `EXPLAIN QUERY PLAN` test asserts the badge query uses `idx_tagging_items_status_pending` so a future schema change that breaks the partial index fails loudly.

### Key decisions retained

- **Hash algorithm in Go, not SQL.** `autotag.GroupKey` is the single source of truth for the key format so we can evolve it without coupling to SQLite functions.
- **SHA-1 over alternatives.** Matches the codebase's existing non-crypto deterministic-key convention. Collision risk at album-group cardinality (millions) is irrelevant.
- **Null-byte separators between hash inputs.** Prevents `a|b` vs `ab|` ambiguity.
- **Per-track integration inside `commitBatch`'s transaction**, not a post-scan callback — keeps `tagging_items` coherent after partial scans.
- **`best_match_release_mbid`, `score`, `last_checked_at` shapes fixed now** even though they stay NULL until 009-010. Avoids schema churn later.

---

## 009 — Scoring Engine & MB Orchestration · shipped 2026-04-21

> Second v1.3 phase. Given an album-group, produce a ranked list of candidate releases with per-track alignment data, using as few MusicBrainz calls as the API-minimization playbook allows. No UI yet — 010 surfaces this to the user.

### What landed

- **`backend/autotag/` domain types** — `Candidate`, `TrackAlignment`, `GroupScore`, `LocalTrack`, `CandidateSource` (`local` / `musicbrainz`), `AlignmentStatus` (`matched` / `missing` / `extra` / `mismatched`).
- **`Normalize(s)`** — Unicode NFC → qualifier-suffix strip (`(Remastered 2009)`, `[Bonus Track]`, `(feat. X)`, etc.) → case fold → punctuation drop → whitespace collapse. Comparison-only, not human-readable.
- **Per-track distance** — weighted 60% title similarity (1.0 − Levenshtein/max-len), 30% length delta (linear: ≤1 s = 1.0, ≥30 s = 0.0, neutral 0.5 when either side unknown), 10% track-number match.
- **Greedy alignment** — `AlignTracks` picks the highest-scoring (local, cand) pair iteratively; not Hungarian-optimal but fine at album cardinalities. Emits `matched` / `missing` / `extra` / `mismatched` rows so the review UI can render the diff.
- **Local-first resolver** — `ListLocalReleaseGroupCandidates` sqlc query pre-filters on `rg.mbid != '' AND r.mbid != '' AND rg.name = ? COLLATE NOCASE`, then Go applies full normalization. Candidate track lists only include recordings that themselves have MBIDs (avoids untagged dupes polluting the "canonical" view when the same `(name, artist)` release group is shared across libraries).
- **MB orchestration** — `MBClient` interface (`SearchReleaseGroups`, `BrowseReleases`, `LookupArtist`) hides `explore.MusicBrainzClient`; `backend/explore/autotagclient.go` adapts one to the other. `buildMBQuery` assembles `release:"X" AND arid:<mbid> AND tracks:N` — `arid:` makes the search cache key deterministic, `tracks:N` filters out box-set-style releases. One search per album + one `BrowseReleases` per candidate RG.
- **Release-level ranker** — aggregate track score (70%) + track-count match (15%, zero at ≥50% delta) + meta (15%, avg of year/Official/country bonuses). `Scorer.ScoreGroup` hits local first, runs MB only when no local candidate scores ≥ 0.90.
- **Persistence** — `SetTaggingItemBestMatch` writes `best_match_release_mbid`, `score`, `last_checked_at = CURRENT_TIMESTAMP`, `status = 'matched'`.

### Key decisions retained

- **SHA-1-grade normalization vs. full MB-equivalent.** Qualifier regex handles the common cases (remaster, deluxe, explicit, feat., bonus, etc.) without dragging in a full MB title-parsing library. Edges that bite in real libraries will show up in 010 review UX and can be patched then.
- **`*sqlcgen.Queries` as the DB boundary for autotag**, not `*database.DB`. The `database` package already depends on `autotag.GroupKey` (from 008.3), so reversing the dependency via `database` would create a cycle. Using `sqlcgen` directly is acyclic and keeps `autotag` swappable.
- **Scorer constructor takes `MBClient` as interface, not `*explore.MusicBrainzClient`.** Lets tests inject a stub without spinning up the HTTP + cache layer. The concrete adapter lives in `explore/autotagclient.go`.
- **`localSufficient = 0.90` threshold for skipping MB.** Empirical guess — will get retuned in 011 auto-accept phase when we observe real corpus scores.
- **Weights `60/30/10` for title/length/track-number.** Cribbed from beets' broad intuition; tuned to emphasize title-matching since length data can be unreliable from Vorbis Comments. Tests document the expected floors (e.g. "exact match should score ≥ 0.99") so nudging weights won't silently regress.
- **`release_groups` and `recordings` must both carry MBIDs** for a local candidate. Otherwise untagged dupes of the same album (across libraries) falsely expand the "canonical" track list.
- **UTF-8 em-dashes in SQL comments broke sqlc's string-literal emitter**, truncating generated query strings mid-word. All autotag SQL comments use ASCII punctuation.

### Known follow-ups into 010+

- **`guessArtistMBID` is a stub** returning `""` because `LocalTrack` doesn't currently carry artist MBIDs. 010 should thread artist MBIDs through `ListAudioFilesInTaggingGroup` so the MB resolver can use `arid:` filters.
- **`yearBonus` uses `time.Now().Year()`** as a placeholder target. Should become the earliest release-date hint from the group's tracks once 010 provides it.
- **VA compilation detection threshold** is still open. The scorer doesn't special-case per-track artist credits differing from album-artist.

---

## 010 — Review UI & Apply Pipeline · ACTIVE

> Third v1.3 phase. User reviews one album at a time, sees the diff clearly, and applies or skips — file tags get written, DB gets synced, cover art follows the never-replace-existing rule.

**Requirements:** REVIEW-01..07 · **Depends on:** 009 (needs candidates + scores)

### Success criteria

1. `/autotag` shows the next pending album with its top candidate as a field-by-field diff. Missing-from-local and extra-in-local tracks are shown explicitly.
2. Keyboard shortcuts work without the mouse: `A` apply, `M` more candidates, `S` skip, `L` leave-as-is, `U` paste URL, `→`/`←` navigate.
3. Apply writes tags to every track in the group via the existing format-specific writers + atomic write + DB sync + FTS5 sync. Whitelisted fields only: title, artist, album, album-artist, year, track#, track-total, disc#, disc-total, all MBIDs.
4. Cover art rule: embed only when the file has no existing art **and** CAA returns ≥500 px on the shortest side. Never replace existing embedded art (auto or manual).
5. First-ever apply per library shows an irreversibility warning. "Don't show again" sets a flag on the `libraries` row; never shows again for that library.
6. While the user reviews album N, candidates for album N+1 are prefetched into `http_cache` so advancing feels instant.
7. "Paste MB URL" dialog accepts a release URL, extracts the MBID, runs one `LookupRelease`, renders the diff against the current album.

### Sub-plans

- Wails bindings — `StartAutotagQueue`, `GetCurrentCandidate`, `GetCandidates(groupKey)`, `Apply(groupKey, releaseMBID)`, `Skip`, `LeaveAsIs(groupKey)`, `RetagGroup(groupKey)`.
- `/autotag` page layout — focused album header, diff table, candidate sidebar, missing/extra panel.
- Keyboard shortcut wiring through the existing scope-aware dispatch.
- Apply pipeline integration with existing tag writers + DB sync.
- Cover art apply rule + CAA fetch + 500 px minimum check.
- File-write warning dialog with per-library persistence.
- Prefetch-next-album goroutine, rate-limiter aware.
- Paste-MB-URL escape hatch.

### Risk callout

Every apply rewrites a file. The `AtomicWrite` pipeline mitigates corruption risk; the per-library warning mitigates surprise. Dry-run mode (from 009) lets developers validate scoring changes without file writes.

---

## 011 — Auto-Accept & Entry Points · pending

> Fourth v1.3 phase. The strict all-match auto-accept path runs as a background job; the tool is reachable from every place a user expects.

**Requirements:** AUTO-01..06 · **Depends on:** 010 (needs the apply pipeline)

### Success criteria

1. An album-group qualifies for auto-accept iff: exact track-count match, every track's normalized title matches, every track's length within ±2s, no cover-art replacement required, no existing-MBID conflicts. Decision uses already-cached candidate data — **no additional MB calls**.
2. Auto-accept job processes all qualifying groups in the queue, emits progress events, is cancellable at any point, honors the shared rate limiter.
3. Right-click on a track / album / artist exposes "Autotag this album" (queues + jumps to review) and "Retag" (flips status to `untagged` and requeues).
4. After a library scan finishes with N new untagged albums, a non-blocking toast appears linking to `/autotag`.
5. Sidebar has an "Autotag" nav entry with a pending-count badge, updates reactively.
6. Pasting a MB release URL into the Paste-URL dialog renders a full diff against the current album with one `LookupRelease` call.

### Sub-plans

- Strict all-match rule + unit tests.
- Auto-accept background job — progress events, cancellation, queue traversal.
- Context menu integrations on track/album/artist views.
- Post-scan toast wiring via the existing scan-complete event.
- Sidebar nav entry + pending-count badge store integration.

### Risk callout

Release selection can pick the wrong edition. The exact-track-count gate prevents most silent misbehavior, but bonus-track editions and remaster reissues with matching track counts are genuine ambiguity. Manual review handles the edge cases — that's why auto-accept is strict by design, and the slider for fuzzy auto-accept is explicitly out of scope.

---

## 012 — Settings & Polish · pending

> Fifth and final v1.3 phase. Configuration surfaces in the existing settings system; the known sharp edges (rate-limit contention, VA compilations, singleton files) get sanded; the fingerprinting seam is in place for the future.

**Requirements:** CFG-01..06 · **Depends on:** 011

### Success criteria

1. Autotag settings panel accessible via the existing templ/HTMX settings UI. Exposes: enable/disable auto-accept, per-library file-write warning reset, default review filter, default sort order.
2. Shared rate limiter distinguishes interactive from background requests. User-initiated MB calls (paste-URL, opening a review, explore browsing) are never blocked behind a running auto-accept job.
3. VA compilation albums (per-track artist credits differ from album-artist) are detected. The auto-accept artist-match rule relaxes for them; the ranker prefers MB releases credited to "Various Artists".
4. Singleton files (`track_count = 1`, no sibling context) use a recording-level match path (`SearchRecordings` with title + artist + length filters). Lower confidence ceiling — never eligible for auto-accept regardless of confidence.
5. `type Identifier interface { Identify(path) ([]Candidate, error) }` exists with `MetadataIdentifier` as the v1 implementation. No fpcalc integration, but the seam is in place for a future `AcoustIDIdentifier`.
6. User-facing quickstart docs exist; CLAUDE.md gets a `backend/autotag/` package description; scoring-function dev notes are committed.

### Sub-plans

- Autotag settings panel (templ + HTMX).
- Rate-limiter priority support.
- VA compilation detection and scoring adjustments.
- Singleton-file match path.
- `Identifier` interface seam with `MetadataIdentifier`.
- Docs — user quickstart + dev notes + CLAUDE.md update.

---

## Ship criteria for v1.3 overall

- All 29 SCHEMA/MATCH/REVIEW/AUTO/CFG requirements complete.
- All five phases' success criteria verified end-to-end on a real library (10k+ tracks, mixed match quality).
- Auto-accept run against a well-tagged subset produces zero incorrect matches.
- Manual review workflow can process 100 albums in under 30 minutes without mouse use.
