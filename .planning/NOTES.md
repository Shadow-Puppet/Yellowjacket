# Notes

Miscellaneous things worth knowing that aren't documented elsewhere. CLAUDE.md owns the architecture overview, the build commands, and the per-package responsibilities — this file is for the gotchas, the deferred-but-tracked items, the "we already considered and rejected" decisions, and the things that bite if you forget them.

## Hard SQLite-driver constraints (forget at your peril)

- `MaxOpenConns(1)` is set on the SQLite connection. **Holding `*sql.Rows` open while calling another function that queries the same `*database.DB` will deadlock.** Close rows explicitly before downstream calls; don't rely on `defer` when the deferred close has to happen *after* a downstream query. This bit smart-playlist S01/T03 and the smart-playlist auto-edit feature; it bites the play-history hook so `OnPlaybackFinished` unlocks the player mutex *before* recording a play.
- No CGo: the driver is `modernc.org/sqlite`. Cannot switch to a CGo-based driver — design around it.
- Connection pooling, dynamic ORDER BY in sqlc, and `FILTER (WHERE ...)` in older modernc versions all require care. (`FILTER` works in current `modernc.org/sqlite`, but if it ever fails, fall back to `SUM(CASE WHEN ... THEN 1 ELSE 0 END)`.)

## Explore + Autotagger API-call-minimization playbook

Every MusicBrainz interaction follows this resolution order — check before sending:

1. **Local DB** — does a matching `release_groups` row already exist? (zero network cost)
2. **Existing partial MBIDs** on the album's tracks — use as Lucene filters (`arid:`, `rgid:`) to narrow search and produce deterministic cache keys.
3. **`http_cache`** (already wrapped transparently by `MusicBrainzClient`) — serves hits for any previously-fetched entity.
4. **Live MB fetch** — last resort.

Plus: share fan-out (one `LookupArtist` per album, not per track); persist decisions to `tagging_items` so reopening the review UI fires zero MB calls; prefetch album N+1 while user reviews album N; cover art is pull-on-apply only; auto-accept never fetches incrementally.

Cache TTLs: 24 h for searches (data updates often), 7 d for entity lookups (artist details, discography are stable).

## Tag-editing pipeline invariants

- **`AtomicWrite`** writes to `<filename>.yj-tmp` in the same directory, then renames. Same-directory rename avoids cross-device issues; deterministic suffix enables orphan cleanup on startup.
- **Upsert-and-relink for entity sync.** Never mutate shared `artists` / `albums` / `genres` rows in place — create new ones or relink to existing rows. Safe under concurrent reads.
- **Currently-playing file gets stopped before its tag write.** `PlayerStopper` interface (implemented by `playerAdapter` in `app.go`) breaks the import cycle.
- **Scan and write are mutually exclusive** via `pipelineMu` in the library package.
- **Batch writes coalesce events** with a `suppressEvents` flag — one `TrackMetadataChanged` per batch instead of N.

## Frontend gotchas

- **Wails TS bindings for smart-playlist methods are manually maintained** in `frontend/wailsjs/go/playlist/Service.{d.ts,js}`. The Wails build does *not* re-generate them in worktrees, and no build-time check catches drift if Go signatures change. Same for any explore method added outside a clean Wails build.
- **`pnpm build` runs from `frontend/`, not project root.** Root `package.json` is empty.
- **Combobox blur-vs-click race** — the `mousedown` + `preventDefault()` + `requestAnimationFrame` fallback in `combobox.ts` is fragile if option rendering moves into a separate shadow DOM. Re-verify click-to-select after any combobox refactor.
- **`go build ./...` fails in git worktrees** because `main.go:28` embeds `frontend/dist`, which doesn't exist in a fresh worktree. Use `go build ./backend/...` for backend-only verification.
- Explore detail components duplicate `CoverArtGroupURL`, `nameToHue`, `extractYear`, `formatDuration` from `explore-view`. Three consumers as of v1.3 planning. If a fourth emerges, extract to `explore-utils.ts`.

## Lint baseline

`make lint` may report a small number of pre-existing warnings (3 wsl_v5 in `database_test.go`, 1 gci + 1 revive in `smartplaylist.go` last time anyone counted). Don't chase these in unrelated PRs. Note them as pre-existing in verification evidence and move on. Anything new must be clean.

## Out-of-scope decisions worth remembering the *why* for

- **Separate databases per library** — defeats unified presentation; rejected.
- **Auto-dedup across libraries** — complex matching logic, not table stakes.
- **Parallel library scanning** — SQLite single-writer; pointless.
- **ORM / query builder** — fights existing sqlc architecture.
- **Connection pooling for SQLite** — meaningless under `MaxOpenConns(1)`.
- **Database health checking / reconnection** — desktop app context, low priority.
- **Cosmetic file splitting** — large files are only a problem if they cause real issues. Extract for reuse or correctness, not aesthetics.
- **Parenthesized boolean logic in smart playlists** — UI complexity not worth the use case; AND-only with multi-value `is_any_of` covers the vast majority.
- **"Is favorited" as a smart-playlist filter** — favoriting is a special-case relationship, not a queryable field.
- **Playing audio remotely from MB** — MB is a metadata catalog, not a streaming service.
- **Fuzzy auto-accept threshold slider** — strict all-match is the trustworthy default; a slider is a power-user foot-gun.
- **Manual MB search UI in autotag** — Paste-URL covers the escape hatch; full search is surface area we don't need yet.
- **Configurable field whitelist for autotag writes** — hardcoded list in v1; add config only if users actually ask.
- **Folder-level cover art (`folder.jpg`/`cover.png`)** — separate feature area from embedded art.
- **Per-library autotagger on/off** — single global setting; per-library adds UI for no clear benefit.

## Known gap from milestone 007

R032 (offline visual indicator for cached Explore data) was scoped into M004 but not implemented. The cache layer works correctly — entries are served when offline until TTL expires — but `Cache.Get()` doesn't propagate a "from cache" flag, and no frontend component renders a "Cached" badge. If picked up: backend modifies `Cache.Get()` to return a `fromCache` bool, frontend adds a subtle badge. ~30 min of work.

## Open architecture questions

- **VA compilation detection threshold for the autotagger.** Per-track artist credits differing from album-artist is the easy heuristic. What's the threshold on number of differing tracks before we relax the artist-match rule? Will surface during scoring tuning in plan 009.
- **Cover Art Archive minimum-dimension check.** REVIEW-05 (plan 010) sets 500 px on the shortest side. Coarse but cheap. Expect tuning once we see real CAA quality variance across genres.
- **Rate-limit priority queue design.** CFG-02 (plan 012) wants user-initiated MB calls to jump ahead of background auto-accept. Implementation strategy is open — priority channel? Two limiters with a yield mechanism? Solve when 012 starts.
