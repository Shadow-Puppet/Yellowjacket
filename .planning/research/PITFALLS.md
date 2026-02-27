# Pitfalls Research

**Domain:** Go/Wails/SQLite Desktop Music Player — Consolidation & Refactoring
**Researched:** 2026-02-27
**Confidence:** HIGH (based on codebase analysis + established Go/SQLite patterns)

## Critical Pitfalls

### Pitfall 1: Refactoring Concurrency Without Tests Creates Invisible Regressions

**What goes wrong:**
You fix a data race (e.g., adding `q.mu.Lock()` to `Queue.SetContext()`) and the fix itself introduces a deadlock because you didn't understand the full call graph. Alternatively, the race fix changes timing semantics that other code implicitly depended on (e.g., Phase 2 of `SetQueue` now acquires the lock at a different time relative to `playCurrentTrack()`). Because there are no tests, the regression only manifests during specific usage patterns — like quickly switching playlists while a background resolve is running.

**Why it happens:**
The instinct is "add mutex → race fixed." But mutexes change scheduling behavior. In YellowJacket, the Player has a documented lock ordering (`p.mu` before `speaker.Lock()`), the Queue has a generation counter pattern with `setQueueGen`, and the beep callback dispatches to a goroutine. These are three interacting concurrency mechanisms. Adding a lock to one path changes how the other two paths interleave.

**How to avoid:**
1. **Write characterization tests first for the non-racy behavior.** Before fixing the race in `Queue.SetContext()`, write tests that verify `SetQueue` → `resolveRemainingTracks` → `emitQueueChanged` produces correct results. These tests won't catch the race (they're single-goroutine), but they'll catch if your mutex addition breaks the non-concurrent path.
2. **Fix races in a specific order:** First fix `SetContext()` patterns (they're called once during startup, lowest risk). Then fix the Queue mutation paths. Leave the Player's dual-lock pattern for last — it's the most complex and already works correctly.
3. **Use `go test -race` on every change.** Build a test binary with `-tags webkit2_41 -race` and run it. The race detector will confirm fixes and catch new races.
4. **Map the lock acquisition graph before adding any mutex.** For each public method, trace which locks it acquires and which callbacks it invokes. The `onPlaybackFinished()` goroutine dispatch (player.go line 350) is the critical pattern — it exists specifically to break a lock cycle.

**Warning signs:**
- App hangs/freezes after a refactoring change (deadlock)
- "Previous" or "Next" track skips incorrectly after rapid clicks
- Queue panel briefly shows wrong tracks then corrects itself
- `-race` flag reports on code paths you didn't change

**Phase to address:**
Testing phase should come first — write tests for queue operations, then fix concurrency. Specifically: (1) characterization tests for queue, (2) fix SetContext races, (3) fix mutation races, (4) fix player double-lock.

---

### Pitfall 2: SQLite In-Memory Tests Behave Differently From File-Based Production DB

**What goes wrong:**
You write tests using `:memory:` SQLite and they pass. In production with a file-based WAL-mode database and `SetMaxOpenConns(1)`, the behavior differs. Common divergences:
- `:memory:` doesn't persist `PRAGMA foreign_keys = ON` across connections (each new connection starts with FK enforcement off)
- `:memory:` with `SetMaxOpenConns(1)` doesn't surface contention the way file-based does (because there's only one connection, it never blocks — same as production, but WAL checkpoint behavior differs)
- FTS5 `search_index` tokenization may behave differently if the test doesn't apply the same schema setup sequence as `NewDB()`
- `PRAGMA user_version` is per-connection for `:memory:`, so migration tests that open a second connection see version 0

**Why it happens:**
`:memory:` is faster and doesn't leave test artifacts, so it's the default choice. But SQLite's `:memory:` is a distinct database per connection, not per DSN. The production code opens a file with specific pragmas (`_busy_timeout=5000&_journal_mode=WAL`), `PRAGMA foreign_keys = ON`, and runs schema files in alphabetical order. Any test that doesn't replicate this sequence is testing a different database.

**How to avoid:**
1. **Create a test helper that mirrors `NewDB()` exactly:** Open a temp file (`t.TempDir() + "/test.db"`), apply the same pragmas, run the same embedded schemas, run `runMigrations()`. Export a `NewTestDB(t *testing.T) *DB` helper.
2. **Use `t.TempDir()`** — Go cleans it up automatically. This is enforced by the `usetesting` linter already configured.
3. **Always set `PRAGMA foreign_keys = ON`** in the test helper — the production code does this, and cascade deletes (like `queue_tracks` → `audio_files`) depend on it.
4. **If you do use `:memory:` for pure unit tests** (testing a single query), use the DSN `file::memory:?cache=shared` and document that it won't test WAL behavior.

**Warning signs:**
- Tests pass but `ON DELETE CASCADE` doesn't fire in production
- FTS5 queries return different results in tests vs. app
- Migration tests pass but real migrations fail on existing databases
- Queue persistence tests pass but tracks are lost on restart

**Phase to address:**
First phase — the test infrastructure setup. `NewTestDB()` must be correct before any database tests are written.

---

### Pitfall 3: Deadlock From Player mutex + speaker.Lock() Ordering Violation

**What goes wrong:**
The Player has a critical invariant: always acquire `p.mu` before `speaker.Lock()`. The beep library's playback callback runs with the speaker lock held. If you refactor a method to call `speaker.Lock()` while holding `p.mu` in a way that blocks, and the callback tries to acquire `p.mu`, you get a classic ABBA deadlock:
- Goroutine 1: holds `p.mu`, waiting for `speaker.Lock()`
- Goroutine 2 (beep callback): holds speaker lock, goroutine dispatch calls `onPlaybackFinished()` which waits for `p.mu`

Currently this is avoided by the `go p.onPlaybackFinished()` dispatch pattern (player.go line 350), which means the callback itself doesn't hold `p.mu` — it just launches a goroutine. But the `startPaused()` method (line 340-354) acquires `speaker.Lock()` while `p.mu` is held by the caller. This works because it's a non-blocking lock/unlock sequence — but if you move speaker operations into a new method without understanding the lock context, deadlock follows.

**Why it happens:**
Refactoring moves code between methods. If you extract `startPaused()` into a helper or inline it into another method, you might accidentally change the lock nesting. The `speaker.Lock()/Unlock()` inside `startPaused()` is safe because it's called with `p.mu` held (correct ordering), but `speaker.Play()` on line 347 is called with `p.mu` held too — and that's where the callback is registered. If the callback fires immediately (e.g., for a zero-length stream), the goroutine dispatch is the only thing preventing deadlock.

**How to avoid:**
1. **Never refactor player lock code without drawing the lock acquisition graph first.** Document which methods hold which locks at each point.
2. **Keep the `go p.onPlaybackFinished()` dispatch pattern.** Never change this to a direct call. Add a comment explaining why.
3. **Extract pure logic (volume math, state serialization) into lock-free functions** that can be tested independently. Don't extract methods that need to hold locks.
4. **Add a regression test** that rapidly calls `LoadFile` → `Play` → `LoadFile` → `Play` to exercise the callback timing. Even without hardware, this can be tested with a mock streamer.

**Warning signs:**
- App freezes when track finishes naturally (not when user clicks Next)
- App freezes specifically when rapidly changing tracks
- `SIGQUIT` goroutine dump shows both `p.mu.Lock()` and `speaker.Lock()` in different goroutines' stacks

**Phase to address:**
Player refactoring phase. Extract testable pure logic first, leave lock-sensitive code paths for last. Document the lock ordering invariant with a test that validates the goroutine dispatch pattern.

---

### Pitfall 4: FTS5 Query Consolidation Breaks Search Ranking or Returns

**What goes wrong:**
You consolidate the 5+ copies of the FTS5 JOIN pattern into a shared constant or query builder. The consolidated query subtly differs from one of the originals — maybe a `LEFT JOIN` becomes an `INNER JOIN`, or the `COALESCE` default changes from `''` to `NULL`, or the subquery for `release_group_recordings` uses `MAX` instead of `MIN`. Search results change: tracks without albums stop appearing, or ranking changes because FTS5's `rank` function scores differently when join columns are NULL vs empty string.

**Why it happens:**
The 5 copies look identical but have small contextual differences. `SearchFTS` uses `ORDER BY rank`, `SearchFTSTracks` might have a different LIMIT, `RebuildSearchIndex` doesn't need the rank column at all. When consolidating, you pick one version as the "canonical" form and the others silently regress. Additionally, FTS5's ranking is sensitive to which columns contain data — a `COALESCE` that returns `''` instead of the actual NULL affects the `bm25()` algorithm differently.

**How to avoid:**
1. **Write search tests BEFORE consolidating.** Test each current function with known data: a track with full metadata, a track with no artist, a track with no album, a track matched only by file path. Capture the exact result set and ranking order.
2. **Consolidate the JOIN clause only, not the full query.** Extract the `FROM ... JOIN` chain as a SQL fragment constant. Let each function keep its own SELECT, WHERE, and ORDER BY clauses.
3. **Verify FTS5 `INSERT INTO search_index` uses the same column values as the search queries.** If the index stores `COALESCE(r.name, '')` but the search query expects `r.name`, the match behavior differs.
4. **Run the consolidation as a pure refactor with zero-diff tests** — if any test changes results, the consolidation introduced a bug.

**Warning signs:**
- Search returns fewer results than before
- Search ranking changes (previously top result now buried)
- Tracks with missing metadata (no artist, no album) disappear from search
- `RebuildSearchIndex` produces different results than incremental inserts

**Phase to address:**
Database/code quality phase. Write FTS5 search tests first, then consolidate.

---

### Pitfall 5: Eager-to-Lazy Library Loading Creates Visible UX Regression

**What goes wrong:**
You change `libraryStore` from eager-fetching all data on construction to lazy-loading per view. The first time the user navigates to the tracks view, there's a loading delay that didn't exist before. The cover grid flickers as albums load in chunks. Worse: components that used synchronous `getCachedTracks()` (which previously always returned data because of eager fetch) now return `null` and render empty states. The user, who has been using this app daily with instant library display, perceives this as a regression.

**Why it happens:**
The current `eagerFetch()` fires all four fetches (`getTracks`, `getAlbums`, `getArtists`, `getGenres`) in the constructor. By the time the user interacts, data is already cached. Switching to lazy loading means the first interaction hits an async boundary. Every component that calls `getCachedTracks()` synchronously (used by at least `track-list`, `cover-grid`, `playlist-view`) will get `null` on first render and must handle a loading state that was previously invisible.

**How to avoid:**
1. **Keep eager fetch for the initial view.** If the user's default view is "tracks," fetch tracks eagerly and lazy-load the rest. The library store already has the lazy `getTracks()` / `getAlbums()` pattern with `tracksLoading` / `albumsLoading` flags — the issue is that `eagerFetch()` triggers them all.
2. **Audit every `getCachedTracks()` / `getCachedAlbums()` call site.** Each one needs a loading state or skeleton UI. Don't change the store without updating all consumers.
3. **Measure before optimizing.** Profile the actual startup time with a large library. If `GetAllTracks()` takes 200ms for 50k tracks, that's fast enough to keep eager. The bottleneck might be rendering, not fetching.
4. **If lazy loading, implement skeleton/shimmer states** that feel faster than the current blank-then-populate pattern. The perceived performance matters more than actual latency.

**Warning signs:**
- Empty track list visible for a fraction of a second on app start
- Cover grid shows placeholder then jumps as albums load
- Components flash between empty and populated states
- User says "it feels slower" even if total time is the same

**Phase to address:**
Performance phase. Profile first, then decide whether lazy loading is actually needed. If yes, update all consumer components in the same change.

---

### Pitfall 6: Queue Persistence Migration Loses Queue State

**What goes wrong:**
You change queue persistence from full-rewrite (`DELETE + INSERT ALL`) to incremental (`INSERT/DELETE individual rows`). The schema or persistence format changes. The user restarts the app and their queue is empty because the new `RestoreState()` can't read the old format, or the migration from full-rewrite to incremental left the `queue_tracks` table in an inconsistent state (e.g., duplicate positions, missing foreign keys).

**Why it happens:**
The current `persistTracks()` does `DELETE FROM queue_tracks` + batch INSERT inside a transaction. This is a clean slate every time — position values are always sequential and consistent. An incremental approach must maintain position ordering through individual INSERT/DELETE/UPDATE operations. If you change the persistence strategy without migrating existing data, or if the new code assumes positions are always contiguous when the old code may have left gaps, the restore fails.

**How to avoid:**
1. **The new persistence code must be able to read the old format.** The `queue_tracks` table has `(id, audio_file_id, position)`. As long as you don't change the schema, `RestoreState()` works unchanged. Only change the write path.
2. **Write a test that persists with the old method, then restores with the new method.** This is the backward compatibility test.
3. **Keep the full-rewrite as a fallback** for `SetQueue` (which replaces the entire queue anyway). Only use incremental for `AddTrack`, `RemoveTrack`, and `MoveTrack`.
4. **Validate position ordering after every incremental mutation** in debug builds. Assert that positions are monotonically increasing.

**Warning signs:**
- Queue is empty after app restart
- Queue tracks are in wrong order after restart
- `RestoreState` logs errors about missing audio files
- Queue tracks have duplicate or negative positions

**Phase to address:**
Performance phase. Write queue persistence tests first, then change the write strategy.

---

### Pitfall 7: Wails Binding Regeneration Silently Breaks Frontend After Go Struct Changes

**What goes wrong:**
You rename a Go struct field (e.g., `queue.Track.Position` → `queue.Track.SortOrder`), change a method signature, or add a new exported method to a bound struct. The Wails binding generator creates new TypeScript files in `frontend/wailsjs/go/`, but the generated types don't match what the frontend code expects. The TypeScript compiler may or may not catch this depending on whether the frontend uses the generated types or inline types. If the frontend uses `any` casts or untyped event payloads, the mismatch is silent.

**Why it happens:**
Wails v2 binding generation (`wails generate module`) creates TypeScript interfaces from Go structs. But the event payloads emitted via `runtime.EventsEmit()` are untyped — they're `any` on the TypeScript side. So if you change the shape of `queue.TracksModified` in Go, the `EventsOn` handler in `queue-store.ts` receives the new shape but TypeScript doesn't enforce it. The `applyTracksDelta` method accesses `.action`, `.tracks`, `.index`, `.positions` — if any of these rename, the delta application silently fails (produces `undefined`).

**How to avoid:**
1. **After any Go struct change to a type used in events, grep the frontend for all usages of that type's fields.** Event payloads are the blind spot — Wails bindings don't cover them.
2. **Run `wails generate module` after every Go struct change** and check the git diff of the generated TypeScript files. If a field renamed, the diff will show it.
3. **Consider adding a shared event payload validation layer.** The `TracksModified` struct in Go and the `TracksModified` type in `queue-store.ts` must match — add a build step or test that verifies field parity.
4. **Never change JSON tags on event payload structs without updating the TypeScript counterpart.** The JSON tags (`json:"currentIndex"`) are what actually matters for the frontend, not the Go field names.

**Warning signs:**
- Queue panel stops updating after a Go struct change
- Event handlers silently receive `undefined` for renamed fields
- `wails dev` works but production build has broken types
- Frontend TypeScript compiles but runtime behavior is wrong

**Phase to address:**
Every phase that touches Go structs used in events. Add a validation check (build script or test) early.

## Technical Debt Patterns

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|-------------------|----------------|-----------------|
| Fixing race without test first | Faster to ship the fix | If fix introduces deadlock or regression, no test catches it. May need to fix again. | Only for trivial races like `SetContext` one-liners where the fix is mechanical (add lock around single assignment) |
| Using `:memory:` SQLite for all tests | Faster tests, no cleanup | Hides WAL behavior, FK enforcement, migration ordering issues | Acceptable for pure query logic tests. Never for integration or migration tests. |
| Keeping raw SQL for batch operations | Avoids sqlc limitations with dynamic IN clauses | Diverges from project's type-safe query pattern. No compile-time checking. | Acceptable when documented. sqlc's `sqlc.slice()` has limitations with SQLite that may not support the batch pattern. |
| Full queue rewrite on every mutation | Simple, always-consistent persistence | O(n) for every single add/remove. For 5000-track queues, this is noticeable. | Acceptable for SetQueue and RestoreState. Not acceptable for AddTrack/RemoveTrack hot paths. |
| Skipping player tests due to hardware | No CI flakiness from audio devices | Player regressions only caught manually. Volume math, state serialization, streamer chain setup are all untested. | Extract pure logic into testable functions. The actual speaker interaction can stay integration-only. |
| `startupErr` as package-level var | Simple error propagation between OnStartup and OnDomReady | Not thread-safe, not testable, global mutable state | Never — move to struct field. Low effort, high correctness gain. |

## Integration Gotchas

| Integration | Common Mistake | Correct Approach |
|-------------|----------------|------------------|
| beep speaker + Player mutex | Calling `speaker.Lock()` from a code path that already holds `p.mu` in a blocking manner, or removing the goroutine dispatch in the beep callback | Maintain strict ordering: `p.mu` before `speaker.Lock()`. Keep `go p.onPlaybackFinished()` as a goroutine dispatch. Never hold both locks when calling into queue. |
| Wails event system + TypeScript stores | Assuming event delivery order matches emission order. Wails events are async from Go → JS bridge. Two events emitted sequentially in Go may arrive in either order in TS. | Design stores to handle events in any order. Use full-state events (`QueueChanged`) as periodic correction. Don't rely on `QueueTracksModified` always arriving before `QueueIndexChanged`. |
| sqlc + FTS5 virtual tables | Expecting sqlc to generate queries against FTS5 `MATCH` syntax. sqlc's SQLite support doesn't fully understand FTS5 virtual table syntax. | Keep FTS5 queries as hand-crafted SQL. Only use sqlc for standard table queries. Document FTS queries as intentional exceptions to the sqlc pattern. |
| modernc.org/sqlite + PRAGMA | Assuming PRAGMAs persist across connections. With the pure-Go driver, each new connection (from the pool) starts fresh. `SetMaxOpenConns(1)` mitigates this but `foreign_keys` must still be set per connection. | Set `PRAGMA foreign_keys = ON` immediately after opening, as the codebase already does. For tests, replicate this in the test helper. |
| TOML config + new fields | Adding a new config section without a default. Existing users' TOML files don't have the new section. `toml.Decode` leaves it as `nil`. `applyDefaults()` runs after decode but only creates defaults for `nil` sections — doesn't fill in missing fields within existing sections. | Always add defaults in `applyDefaults()` for new fields. Test config loading with an empty file and a minimal file (only `[Library]` section). |
| Wails lifecycle + SetContext ordering | Calling `RestoreState()` before `SetContext()`. The restore tries to emit events but context is nil. Or calling `SetPlayer()` after `RestoreState()` — the restored queue tries to auto-advance but player reference is nil. | Follow the exact ordering in `OnStartup()`: SetContext → SetPlayer → RestoreState. Document this ordering requirement. Test with a mock that verifies call order. |

## Performance Traps

| Trap | Symptoms | Prevention | When It Breaks |
|------|----------|------------|----------------|
| Full queue persistence on every mutation | Slight lag when adding/removing single tracks. `commitMutation()` calls `persistTracks()` which does DELETE + INSERT ALL. | Profile `persistTracks()` for queue sizes of 100, 1000, 5000 tracks. Implement incremental persistence for single-track operations. | Queues > 1000 tracks with frequent mutations (drag-reorder, bulk add). ~50-100ms per operation at 5000 tracks with SQLite writes. |
| Eager full-library fetch on startup | Slow initial load for large libraries. Four simultaneous `GetAll*` queries each doing full table scans with JOINs. | Measure actual query times: if < 300ms for target library size, keep eager. If > 300ms, lazy-load non-default views. | Libraries > 50k tracks. Each `GetAllTracks` query with JOIN chain may take 500ms+. |
| FTS5 JOIN chain in every search query | Search latency scales with library size. The 5-table JOIN chain runs for every keystroke (debounced). | The JOIN chain is necessary for displaying results. Optimize by ensuring FTS5 index is populated correctly so `MATCH` reduces the result set before JOINs. Add `LIMIT` to all search queries. | Libraries > 100k tracks without proper FTS5 indexing. |
| Frontend re-renders on every store notification | Track list with 10k+ items re-renders when any store property changes. Virtual scrolling helps but the data array replacement triggers Lit's dirty check. | Use `===` reference equality checks. Only replace arrays when contents actually changed, not on every event. Lit's `@state()` triggers re-render on any assignment. | Track lists > 5000 items with frequent events (playback position updates). |
| SetQueue Phase 2 re-fetches all tracks | `resolveRemainingTracks` calls `lookupTrackMetaBatch(filePaths)` for ALL paths including those already resolved in Phase 1. | Pass Phase 1 results to Phase 2. Only look up the delta. For a 5000-track album, this saves ~50 lookups. | Large playlists/albums > 500 tracks where Phase 1's 50-track window is a small fraction. |

## UX Pitfalls

| Pitfall | User Impact | Better Approach |
|---------|-------------|-----------------|
| Introducing loading states where none existed | User who has been using the app daily suddenly sees spinners or empty states on startup. Perceives app as slower even if total time is the same. | Preserve instant-display for the default view. Only add loading states for lazily-loaded secondary views (artist detail, genre browsing). Use skeleton UIs, not spinners. |
| Fixing queue persistence timing | If incremental persistence introduces a delay between mutation and save, a crash between mutation and save loses the change. User adds 50 tracks, app crashes, queue is reverted. | Persist synchronously for user-initiated mutations (add, remove). Only defer persistence for background operations (Phase 2 resolve). |
| Changing search result ranking | Consolidating FTS5 queries might change which columns are weighted. User's muscle memory for search ("typing 'beat' always shows Beatles first") breaks silently. | Capture current search results for common queries before refactoring. Validate ranking stability after changes. |
| Config migration failures | User's config.toml has custom theme settings. A config change causes parse failure on startup. App doesn't start. User has no way to recover without deleting config. | Always handle TOML parse errors gracefully — log the error, use defaults, don't crash. The current code returns an error from `NewConfig()` which is fatal. Consider falling back to defaults with a warning. |
| Event ordering changes | Refactoring changes when events are emitted relative to state changes. Frontend shows stale data for a frame (queue shows old index while track changed). | Ensure state is consistent before emitting any events. Emit all related events together. Use the full-state `QueueChanged` event as the ground truth; deltas are optimizations. |

## "Looks Done But Isn't" Checklist

- [ ] **Queue tests:** Often missing concurrent SetQueue test — verify two rapid SetQueue calls don't corrupt state (generation counter works)
- [ ] **Search consolidation:** Often missing empty-string and special-character test cases for FTS5 — verify `"`, `*`, `(`, `)` in search queries don't crash
- [ ] **Config roundtrip:** Often missing test with unknown TOML keys — verify future config fields don't cause parse errors on older app versions
- [ ] **Migration tests:** Often missing test on existing database with data — verify migration doesn't drop existing rows
- [ ] **Incremental persistence:** Often missing test for queue order after remove-from-middle — verify remaining tracks keep correct positions
- [ ] **Lock ordering:** Often missing test for rapid LoadFile during playback — verify the beep callback + new LoadFile don't deadlock
- [ ] **Event parity:** Often missing validation that Go event constants match TypeScript — verify no typos exist between `events.go` and `events.ts`
- [ ] **Lazy loading:** Often missing test for component render with null data — verify all components handle loading state without errors
- [ ] **FTS rebuild:** Often missing test for `RebuildSearchIndex` idempotency — verify running it twice doesn't create duplicate index entries

## Recovery Strategies

| Pitfall | Recovery Cost | Recovery Steps |
|---------|---------------|----------------|
| Deadlock from lock ordering violation | LOW | Identify the two goroutines holding locks (SIGQUIT dump). Fix the ordering. Add a comment. The app just needs restart — no data loss. |
| Silent search regression from FTS consolidation | MEDIUM | Revert the consolidation. Write the tests that should have existed. Re-apply consolidation with tests passing. Data is intact — only query logic changed. |
| Queue state loss from persistence change | HIGH | If queue_tracks table was corrupted, user loses their queue. No automatic recovery. Prevention: always write persistence tests before changing the write path. Mitigation: keep a backup of queue state in a second table during migration period. |
| Config parse failure on startup | MEDIUM | App won't start. User must manually edit or delete config.toml. Prevention: handle TOML errors gracefully, fall back to defaults. Recovery: add a `--reset-config` CLI flag. |
| Frontend empty state regressions | LOW | Components show blank instead of data. Fix by adding null checks and loading states. No data loss. But user trust is eroded. |
| Wails binding mismatch after struct rename | MEDIUM | Frontend silently receives undefined fields. Fix by running `wails generate module` and updating TypeScript event handlers. No data loss but broken UI until fixed. |
| In-memory test false positive | HIGH (delayed) | Tests pass, bug ships. Discovered when user reports data loss or corruption in production. Prevention: use file-based SQLite in tests from the start. Recovery depends on which bug shipped. |

## Pitfall-to-Phase Mapping

| Pitfall | Prevention Phase | Verification |
|---------|------------------|--------------|
| Refactoring concurrency without tests | Testing infrastructure (first phase) | Queue characterization tests pass. `-race` flag clean on all test runs. |
| In-memory SQLite test divergence | Testing infrastructure (first phase) | `NewTestDB()` helper uses file-based SQLite with identical pragma setup. All DB tests use it. |
| Player deadlock from lock ordering | Player refactoring phase (after testing) | Pure logic extracted and tested. Lock-sensitive code unchanged or minimally changed with lock graph documented. No SIGQUIT needed. |
| FTS5 query consolidation breaks search | Database/code quality phase | Search tests capture before/after results for: full metadata track, metadata-less track, special characters, empty query. Zero-diff after consolidation. |
| Eager-to-lazy loading UX regression | Performance phase | Profile data establishes baseline. If lazy loading applied, all `getCached*()` call sites handle null. Skeleton UI visible for < 200ms. |
| Queue persistence state loss | Performance phase | Queue persistence roundtrip tests pass. Old-format → new-format compatibility test passes. Queue survives app restart in all modes. |
| Wails binding mismatch | Every phase (continuous) | `wails generate module` runs in CI or pre-commit. Event payload types have TypeScript interface definitions that match Go struct JSON tags. |
| Config migration failure | Correctness phase | Config roundtrip test with empty file, minimal file, and full file. Unknown keys don't crash. Missing sections get defaults. |
| Event ordering assumptions | Correctness/UX phase | Frontend stores handle events in any order. Full-state events correct drift. No visible flicker between events. |

## Sources

- Codebase analysis: `backend/player/player.go` (lock ordering, lines 30-40, 340-394)
- Codebase analysis: `backend/queue/queue.go` (SetQueue two-phase, lines 152-311)
- Codebase analysis: `backend/queue/persistence.go` (full rewrite pattern, lines 116-204)
- Codebase analysis: `backend/database/database.go` (pragma setup, lines 49-65; migrations, lines 153-335)
- Codebase analysis: `backend/database/search.go` (duplicated FTS5 JOINs, lines 34-58, 92-116)
- Codebase analysis: `frontend/src/store/library-store.ts` (eager fetch, lines 300-305; lazy accessors, lines 64-154)
- Codebase analysis: `frontend/src/store/queue-store.ts` (delta application, lines 107-171)
- Codebase analysis: `backend/config/config.go` (load/save roundtrip, lines 100-139, 142-160)
- Codebase analysis: `backend/app.go` (lifecycle ordering, lines 136-212; package-level startupErr, line 134)
- Documented concerns: `.planning/codebase/CONCERNS.md` (all sections)
- Go testing best practices: `t.TempDir()` for file-based test databases (enforced by usetesting linter)
- SQLite documentation: PRAGMA scoping, WAL mode behavior, FTS5 ranking (HIGH confidence — well-established SQLite behavior)
- beep library: speaker lock semantics (HIGH confidence — observed in codebase, consistent with beep v2 design)
- Wails v2: binding generation, event system limitations (MEDIUM confidence — based on codebase patterns and Wails v2 documented behavior)

---
*Pitfalls research for: YellowJacket consolidation milestone*
*Researched: 2026-02-27*
