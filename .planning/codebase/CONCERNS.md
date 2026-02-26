# Codebase Concerns

**Analysis Date:** 2026-02-26

## Tech Debt

**Hardcoded Speaker Configuration:**
- Issue: Speaker sample rate (44100) and buffer size (100ms) are hardcoded constants with no user configuration
- Files: `backend/player/player.go` line 104, line 127
- Impact: Users with high-resolution audio (96kHz, 192kHz) get resampled down to 44.1kHz. Users cannot tune buffer size for latency vs. stability tradeoff
- Fix approach: Add `AudioOutput` section to config TOML (`SampleRate`, `BufferSizeMs`). Plumb through to `InitSpeaker()` and `updateStreamers()` resample quality param (currently hardcoded `4` at line 308)

**Fixed Resample Quality:**
- Issue: Resample quality is hardcoded to `4` in `beep.Resample()` call
- Files: `backend/player/player.go` line 307-309
- Impact: No ability to trade CPU for quality. Low quality may produce audible artifacts on large sample rate deltas
- Fix approach: Make resample quality configurable via config, expose in settings UI. The TODO comment at line 307 acknowledges this

**Tag Writing Not Implemented:**
- Issue: Track details editing UI exists but save is a no-op
- Files: `frontend/src/components/track-details/track-details.ts` line 651
- Impact: Users see an edit interface that doesn't persist changes. Misleading UX
- Fix approach: Implement backend tag writing endpoint using a tag library (e.g. `github.com/dhowden/tag` already in deps supports reading; writing may need additional library). Gate the save button behind a "tag writing supported" check

**HTML Template Component Incomplete:**
- Issue: The `struct2html` templ component has a TODO for supporting more types
- Files: `pkg/templcomp/struct2html_templ.go` line 242
- Impact: Config page form generation may not handle all field types correctly
- Fix approach: Extend the type switch to cover missing types (maps, nested structs, etc.)

**Package-Level `startupErr` Variable:**
- Issue: `startupErr` is a package-level mutable variable used to communicate startup failures between `OnStartup` and `OnDomReady`
- Files: `backend/app.go` line 134
- Impact: Not thread-safe if Wails calls these lifecycle methods concurrently. Also makes testing difficult
- Fix approach: Move to a field on `YellowJacketApp` struct, protected by the struct's lifecycle guarantees

## Code Quality

**Large Frontend Components:**
- Issue: Several Lit components exceed 1000+ lines, combining rendering, state management, event handling, drag-and-drop, context menus, and search filtering
- Files:
  - `frontend/src/components/playlist-view/playlist-view.ts` (2669 lines)
  - `frontend/src/components/cover-grid/cover-grid.ts` (2092 lines)
  - `frontend/src/components/track-list/track-list.ts` (1875 lines)
  - `frontend/src/components/config-page/config-page.ts` (1464 lines)
  - `frontend/src/components/queue-panel/queue-panel.ts` (1424 lines)
- Impact: Difficult to reason about, test in isolation, or modify without regressions. High coupling between rendering and business logic
- Fix approach: Extract reusable behaviors into additional controllers (the project already uses `SelectionController`, `ContextMenuController`, etc.). Consider splitting rendering into sub-components

**Large Backend Files:**
- Issue: `backend/playlist/playlist.go` (1778 lines) and `backend/library/library.go` (1328 lines) handle too many responsibilities
- Files: `backend/playlist/playlist.go`, `backend/library/library.go`
- Impact: Hard to navigate; mixing CRUD, M3U8 file management, phantom resolution, and search in a single file
- Fix approach: `playlist.go` already has some splitting (m3u.go, match.go, favorites.go). Consider further extraction: phantom resolution into `phantom.go`, M3U file management is already split. Library could extract `saveAudioFile`/`updateAudioFileMetadata`/`processMetadata` into a dedicated `import.go` file

**Duplicated FTS Search Query:**
- Issue: The same complex FTS5 JOIN query pattern (audio_files + recordings + artist_credit + release_group_recordings + release_groups) is repeated in `SearchFTS`, `SearchFTSByFilename`, `SearchFTSTracks`, `RebuildSearchIndex`, and `migration2BasenameAndFTS`
- Files: `backend/database/search.go` lines 34-57, 92-116, 232-274, 168-188; `backend/database/database.go` lines 287-311
- Impact: Changes to the schema require updating 5+ copies of essentially the same JOIN pattern. Risk of them diverging
- Fix approach: Extract the common JOIN clause into a constant or query builder helper. Alternatively, consolidate into fewer sqlc-generated queries

**Raw SQL in Persistence Layer:**
- Issue: Queue persistence and search use hand-crafted SQL with string concatenation for batch operations (`lookupChunk`, `insertTrackBatch`) instead of sqlc-generated queries
- Files: `backend/queue/persistence.go` lines 56-73, 186-203; `backend/database/search.go`
- Impact: These queries bypass sqlc's type-safety guarantees. The `fmt.Sprintf` pattern for IN clauses is safe (only `?` placeholders are interpolated) but diverges from the project's pattern of using generated queries
- Fix approach: Consider using sqlc's `sqlc.slice()` feature or a query builder for batch operations. Alternatively, document these as intentional exceptions

## Error Handling Gaps

**Swallowed Errors in App Lifecycle Callbacks:**
- Issue: MPRIS callbacks in `app.go` discard errors from `Pause()` and `Seek()` with `_ =`
- Files: `backend/app.go` lines 183, 186, 191, 195
- Impact: If pause or seek fails from OS media controls, the failure is invisible to the user and to logs
- Fix approach: Log errors at minimum. Consider emitting a frontend notification for user-visible failures

**Silently Swallowed Artist Credit Link Error:**
- Issue: `CreateArtistCreditArtist` result and error are both discarded with `_, _`
- Files: `backend/library/library.go` line 1092
- Impact: If the link creation fails for a non-duplicate reason, the data model is silently incomplete
- Fix approach: Check error; ignore only `UNIQUE constraint` violations (which are expected for idempotent upserts), log all others

**Library Scan Error Accumulation:**
- Issue: `Scan()` accumulates errors via `errors.Join` but individual file failures don't stop the scan — which is correct behavior — but the accumulated `scanErr` is returned alongside valid metrics, and callers may not distinguish "scan completed with warnings" from "scan failed"
- Files: `backend/library/library.go` lines 216-218, 310-320, 427-430
- Impact: Callers cannot differentiate between partial success and total failure
- Fix approach: Consider separating scan warnings from fatal scan errors. Return warnings in metrics, fatal errors as the error return

**Config File Permissions:**
- Issue: Config file is written with `0o666` permissions
- Files: `backend/config/config.go` line 152
- Impact: On multi-user systems, any user can read/write the config file. While this is a desktop app, it's not best practice
- Fix approach: Use `0o644` or `0o600` for user-only read/write

## Performance Concerns

**Eager Full-Library Fetch on Startup:**
- Issue: `libraryStore.eagerFetch()` calls `GetAllTracks()`, `GetAllAlbums()`, `GetAllArtists()`, `GetAllGenres()` simultaneously on construction
- Files: `frontend/src/store/library-store.ts` lines 300-305
- Impact: For large libraries (50k+ tracks), this loads all track data into memory at once. Each call triggers a full table scan with multiple JOINs
- Fix approach: Consider lazy loading only the active view's data, or implement pagination. The `GetAllTracks` query with full metadata joins is particularly expensive for large libraries

**Full Queue Re-persist on Every Mutation:**
- Issue: `commitMutation()` calls `persistTracks()` which does `DELETE FROM queue_tracks` + batch INSERT for the entire queue on every add/remove/move operation
- Files: `backend/queue/persistence.go` lines 118-178; `backend/queue/queue.go` line 1157
- Impact: For a queue with thousands of tracks, every single track add/remove triggers a full table rewrite. This is O(n) for every mutation
- Fix approach: Use incremental persistence (INSERT/DELETE individual rows) for add/remove operations. Reserve full rewrite for SetQueue and restore

**SetQueue Phase 2 Re-lookups All Tracks:**
- Issue: `resolveRemainingTracks` re-fetches metadata for ALL file paths including those already resolved in Phase 1
- Files: `backend/queue/queue.go` lines 258-311
- Impact: For large albums/playlists, this doubles the DB work for the initial batch
- Fix approach: Pass the already-resolved metadata from Phase 1 to Phase 2, only lookup the remaining paths

**Entity Cache Never Evicted During Scan:**
- Issue: The `entityCache` in library scanning grows unbounded during a scan - it accumulates every artist, album, genre, and cover art seen
- Files: `backend/library/library.go` lines 41-61
- Impact: For very large libraries with thousands of unique artists/albums, this could consume significant memory. However, since it's only held for the duration of a scan and reduces DB round-trips, this is an acceptable tradeoff for most libraries
- Fix approach: Low priority. Could add an LRU eviction policy if memory becomes an issue with extremely large libraries

## Security Considerations

**File Path Handling:**
- Risk: Library scan uses `filepath.Join(basePath, path)` where `path` comes from `fs.WalkDir` which should be safe, but playlist import accepts user-provided file paths (`ImportPlaylist`, `AddTracksToPlaylist`)
- Files: `backend/playlist/playlist.go` lines 677-784, 442-484; `backend/library/library.go` line 247
- Current mitigation: File paths come from Wails file dialogs (OS-level) and are validated by checking file existence. sqlc parameterized queries prevent SQL injection
- Recommendations: Consider adding path traversal validation (ensure paths don't escape expected directories). Validate that playlist import paths resolve within the library directory

**SQL Injection Protection:**
- Risk: Most queries use sqlc-generated parameterized queries, but hand-crafted SQL exists in search and queue persistence
- Files: `backend/queue/persistence.go` lines 64-73, 195-198; `backend/database/search.go` lines 34-58, 92-116
- Current mitigation: All hand-crafted queries use `?` placeholders with separate args — no string interpolation of user values into SQL
- Recommendations: The `fmt.Sprintf` in `lookupChunk` only interpolates placeholder strings (`"?"` literals), not user data. This is safe but should be documented with a comment explaining why

**Config Data Logged:**
- Risk: Config struct is attached to the logger context at construction time
- Files: `backend/config/config.go` line 46
- Current mitigation: Config currently contains no secrets (file paths, theme settings, window dimensions)
- Recommendations: If secrets are ever added to config (API keys, auth tokens), the logger attachment must be removed or filtered

## Fragile Areas

**Event Name Synchronization:**
- Files: `backend/events/events.go`, `frontend/src/events.ts`
- Why fragile: Event names must match exactly between Go and TypeScript. There is no compile-time or runtime verification that they match. A typo in either file silently breaks communication
- Safe modification: Always update both files simultaneously. The AGENTS.md documents this requirement
- Test coverage: No automated test verifies event name parity

**Player Lock Ordering:**
- Files: `backend/player/player.go` lines 31-39
- Why fragile: The player has two locks (its own `sync.Mutex` and the global `speaker.Lock()`) with a documented ordering requirement: "always acquire p.mu BEFORE speaker.Lock()". The `onPlaybackFinished` callback runs on a goroutine to avoid holding both locks simultaneously
- Safe modification: Never call `speaker.Lock()` while holding `p.mu` in a code path that could block. The `go p.onPlaybackFinished()` pattern in the beep callback (line 351) is critical — removing the goroutine dispatch would deadlock
- Test coverage: No test validates the lock ordering. The integration test requires hardware

**Two-Phase Queue Initialization:**
- Files: `backend/queue/queue.go` lines 152-251
- Why fragile: `SetQueue` uses a two-phase approach with generation counters to handle concurrent calls. The background goroutine (`resolveRemainingTracks`) must check the generation counter under the lock to avoid overwriting newer state
- Safe modification: Always increment `setQueueGen` before starting background work. Always check the counter both before and after acquiring the lock
- Test coverage: No unit test for concurrent SetQueue calls

**Player SetContext Double Lock:**
- Files: `backend/player/player.go` lines 163-171
- Why fragile: `SetContext` acquires and releases `p.mu` twice in succession. Between the two lock acquisitions, another goroutine could modify state
- Safe modification: Consider combining into a single lock acquisition, or document why the two-phase approach is intentional (it appears to be separating the context set from the state restore for clarity)
- Test coverage: Integration test only

**Config TOML Serialization Roundtrip:**
- Files: `backend/config/config.go` lines 100-139, 142-160
- Why fragile: `Load()` applies defaults, then decodes TOML over them, then validates. If a new config field is added without a proper default, existing config files will have the zero value. The `applyDefaults()` runs after decode which could overwrite valid zero values
- Safe modification: Always add defaults in `applyDefaults()` for new fields. Test with an empty config file

## Missing Features

**No Graceful Scan Cancellation:**
- Problem: Library scan cannot be cancelled by the user once started
- Files: `backend/library/library.go` lines 166-540
- Blocks: Users with large libraries cannot abort a scan that's taking too long. The `l.ctx.Done()` checks exist but depend on the Wails context which is only cancelled on app shutdown
- Fix approach: Add a separate cancellation context that can be triggered from the frontend

**No Database Connection Pooling/Health Check:**
- Problem: The database connection is opened once at startup with no health checking or reconnection logic
- Files: `backend/database/database.go` lines 35-136
- Blocks: If the SQLite file becomes corrupted or the disk fills up, errors propagate to every component with no recovery path
- Fix approach: Add a health check method and consider periodic PRAGMA integrity_check for dev builds

**No Cross-Platform Media Controls:**
- Problem: Media controls only work on Linux (MPRIS). macOS and Windows get a no-op stub
- Files: `backend/mediacontrols/mpris_linux.go`, `backend/mediacontrols/stub.go`
- Blocks: macOS users cannot control playback from the media keys overlay or Control Center
- Fix approach: Implement `NSMPRemoteCommandCenter` for macOS, `SystemMediaTransportControls` for Windows

## Test Coverage Gaps

**No Queue Unit Tests:**
- What's not tested: Queue operations (SetQueue, AddTrack, RemoveTrack, Next, Previous, shuffle, repeat modes, persistence)
- Files: `backend/queue/queue.go`, `backend/queue/navigation.go`, `backend/queue/persistence.go`, `backend/queue/handlers.go`
- Risk: The queue is central to playback. Bugs in index tracking, shuffle order, or persistence could cause tracks to skip, repeat incorrectly, or lose the queue on restart
- Priority: High

**No Library Service Unit Tests:**
- What's not tested: Library scan logic, metadata processing, entity cache behavior, batch commit logic, orphan cleanup
- Files: `backend/library/library.go`, `backend/library/rescan.go`, `backend/library/coverart.go`
- Risk: Scan bugs could silently drop tracks, create duplicate entities, or fail to clean up orphans
- Priority: High

**No Database Layer Tests:**
- What's not tested: Search index operations (FTS5 queries), migration logic, transaction handling
- Files: `backend/database/search.go`, `backend/database/database.go`
- Risk: FTS5 query edge cases (special characters, empty queries, very long queries) and migration failures on existing databases
- Priority: Medium

**No Config Tests:**
- What's not tested: Config load/save roundtrip, validation, default application, migration from older config formats
- Files: `backend/config/config.go`
- Risk: Config corruption or silent loss of settings on upgrade
- Priority: Medium

**Player Tests Require Hardware:**
- What's not tested: All player tests require an audio device and are skipped in CI
- Files: `backend/player/player_test.go` line 21
- Risk: Player regressions are only caught manually. The volume conversion, streamer chain, and state persistence logic could all be tested without hardware
- Priority: Medium — extract pure logic (volume math, state serialization) into testable functions

**No Frontend Tests:**
- What's not tested: All TypeScript/Lit components, stores, and controllers
- Files: `frontend/src/` (entire directory)
- Risk: Frontend regressions in event handling, state synchronization, search filtering, drag-and-drop, and selection logic
- Priority: Medium — the backend is the source of truth, but frontend-only logic (search ranking, column sorting, selection controller) could have unit tests

## Concurrency Concerns

**Queue Context Set Without Lock:**
- Issue: `Queue.SetContext()` sets `q.ctx` without holding `q.mu`, while `q.ctx` is read by emit methods that are called under `q.mu`
- Files: `backend/queue/queue.go` lines 134-136
- Impact: Technically a data race on `q.ctx` if SetContext is called concurrently with emit methods. In practice, SetContext is called once during startup before any other queue operations
- Fix approach: Acquire `q.mu` in SetContext for correctness

**Library Fields Not Protected:**
- Issue: `Library` struct fields (`ctx`, `conf`, `rescanHooks`) are set via setter methods without any synchronization
- Files: `backend/library/library.go` lines 78-84, 88-90, 120-123
- Impact: If `SetContext`, `SetRescanHooks`, or config updates occur concurrently with a scan, there could be data races. In practice, these are called during the single-threaded startup phase
- Fix approach: Low priority — document the "set during startup only" contract, or add a mutex if the initialization order becomes less predictable

**Playlist Service Context Race:**
- Issue: `playlist.Service` has a `ctx` field set by `SetContext()` without synchronization, read by `emitEvent()` and all methods
- Files: `backend/playlist/playlist.go` lines 98-104, 130-133, 1169-1178
- Impact: Same pattern as Queue — safe in practice due to startup ordering but technically a race
- Fix approach: Same as Queue — acquire lock or document contract

## Frontend Concerns

**No Event Listener Cleanup:**
- Issue: Singleton stores (`playerStore`, `queueStore`, `libraryStore`) register `EventsOn` listeners in their constructors but never unregister them
- Files: `frontend/src/store/player-store.ts` lines 54-71, `frontend/src/store/queue-store.ts` lines 65-105, `frontend/src/store/library-store.ts` line 51
- Impact: As singletons that live for the app lifetime, this is acceptable — they never need cleanup. However, the Wails `EventsOn` API returns a cancel function that is never captured. If the architecture ever changes to non-singleton stores, this would leak
- Fix approach: Low priority — capture the cancel functions for documentation purposes even if they're never called

**Library Store Potential Memory Pressure:**
- Issue: `libraryStore` caches the entire track, album, artist, and genre lists in memory simultaneously
- Files: `frontend/src/store/library-store.ts` lines 29-32
- Impact: For a library with 100k+ tracks, this could be tens of MB of JavaScript objects. The eager fetch on construction (`eagerFetch()`) means all four datasets are loaded simultaneously
- Fix approach: Consider lazy loading per-view and releasing data for inactive views, or implementing virtual scrolling data providers that don't require holding the full dataset

**Queue Store Delta Application Trusts Backend:**
- Issue: The `applyTracksDelta` method in `QueueStore` applies backend-sent delta operations without validation. If the frontend state diverges from the backend (e.g. missed event), the delta application produces incorrect state
- Files: `frontend/src/store/queue-store.ts` lines 107-171
- Impact: Could cause visual glitches where the queue panel shows incorrect tracks or indices. The full-state `QueueChanged` event acts as a periodic correction mechanism
- Fix approach: Consider adding a sequence number or hash to detect state divergence and trigger a full re-sync

## Dependencies at Risk

**Wails v2 Framework Lock-in:**
- Risk: Wails v2 uses WebView2 (Windows), WebKit2 (Linux), WKWebView (macOS). The project requires `-tags webkit2_41` for Linux builds. Wails v3 is in active development with breaking API changes
- Impact: Migration to Wails v3 will require significant refactoring of the lifecycle management (`OnStartup`, `OnDomReady`, `OnShutdown`), event system, and binding registration
- Migration plan: Monitor Wails v3 stability. The event-based architecture and clean separation of concerns make migration more feasible than a tightly coupled approach

**beep Audio Library:**
- Risk: The `gopxl/beep/v2` library handles all audio decoding and playback. It wraps platform-specific audio output (oto) and codec libraries. The speaker is initialized with global state (`speaker.Init`, `speaker.Lock`)
- Impact: The global speaker lock creates an implicit coupling between all audio operations. If beep has bugs in seeking or resampling, workarounds are limited
- Migration plan: The `metadata.DecodeFile()` abstraction and `TrackLoader` interface provide some insulation. A replacement would require reimplementing the streamer chain

---

*Concerns audit: 2026-02-26*
