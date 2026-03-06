# Feature Research: Quality Improvements

**Domain:** Go/Wails/Lit desktop music player — consolidation milestone
**Researched:** 2026-02-27
**Confidence:** HIGH (improvements grounded in codebase analysis + verified patterns)

## Feature Landscape

This is a consolidation milestone. "Features" here are quality improvements, not new user-facing functionality. Each improvement addresses a specific concern documented in `.planning/codebase/CONCERNS.md`.

---

### Table Stakes (Must Fix — Codebase Is Unreliable Without These)

These are correctness and reliability issues. Leaving them unfixed means the codebase has known race conditions, swallowed errors, and untested critical paths.

| Improvement | Why Required | Complexity | Concern Ref |
|-------------|-------------|------------|-------------|
| **Fix SetContext data races in Queue, Library, Playlist** | `q.ctx`, `l.ctx`, `s.ctx` are written without locks but read under locks. This is a textbook data race detectable by `-race`. Even if startup ordering makes it safe today, any refactoring that changes init order silently introduces corruption. | LOW | Concurrency Concerns |
| **Fix package-level `startupErr` variable** | Mutable package-level variable shared between `OnStartup` and `OnDomReady`. Not thread-safe, untestable. Move to `YellowJacketApp` struct field. | LOW | Tech Debt |
| **Fix config file permissions (0o666 → 0o644)** | Writing world-writable config files is a security defect. One-line fix. | LOW | Error Handling Gaps |
| **Fix swallowed errors in MPRIS lifecycle callbacks** | `_ =` on `Pause()` and `Seek()` errors from OS media controls. Invisible failures. At minimum log; ideally emit frontend notification. | LOW | Error Handling Gaps |
| **Fix silently swallowed artist credit link error** | `_, _ = CreateArtistCreditArtist(...)` discards non-duplicate errors. Check error, ignore only UNIQUE constraint violations. | LOW | Error Handling Gaps |
| **Separate scan warnings from fatal errors** | `Scan()` returns `errors.Join()` of all errors. Callers cannot distinguish "scan completed with 3 file warnings" from "scan completely failed". Return warnings in metrics, fatal errors as the error return. | MEDIUM | Error Handling Gaps |
| **Unit tests for queue operations** | Queue is central to playback — SetQueue, navigation, shuffle, repeat, persistence — all untested. Bugs here cause tracks to skip, repeat wrong, or lose queue on restart. | HIGH | Test Coverage Gaps |
| **Unit tests for library scan logic** | Metadata processing, entity cache, orphan cleanup — all untested. Bugs silently drop tracks or create duplicates. | HIGH | Test Coverage Gaps |
| **Unit tests for database layer (FTS5, migrations)** | FTS5 edge cases (special chars, empty queries) and migration failures are completely untested. | MEDIUM | Test Coverage Gaps |
| **Unit tests for config (load/save roundtrip)** | Config corruption or silent settings loss on upgrade has no safety net. | MEDIUM | Test Coverage Gaps |

#### Concurrency Fix Details

**Pattern:** For `SetContext` race conditions, the fix is uniform across Queue, Library, and Playlist:

```go
// BEFORE (Queue — race condition):
func (q *Queue) SetContext(ctx context.Context) {
    q.ctx = ctx  // no lock, but q.ctx read under q.mu elsewhere
}

// AFTER (correct):
func (q *Queue) SetContext(ctx context.Context) {
    q.mu.Lock()
    defer q.mu.Unlock()
    q.ctx = ctx
}
```

Player already does this correctly (locks around `p.ctx = ctx` in `SetContext`). Apply the same pattern to Queue, Library, and Playlist. For Library and Playlist which don't currently have a mutex, add one — or document the "set during startup only, before any concurrent access" contract with a comment and `// SAFETY:` annotation.

**Recommendation:** Add a `sync.Mutex` to Library and Playlist. The cost is negligible, and it eliminates the `-race` detector finding permanently. Documenting "safe because startup ordering" is fragile — the next developer (or future-you) may change init order. *Confidence: HIGH — standard Go concurrency practice.*

#### Testing Strategy Details

**In-memory SQLite for DB-dependent tests:** Use `sql.Open("sqlite", ":memory:")` with the `modernc.org/sqlite` driver (already in deps). Apply the same schema migrations used in production. This gives:
- Fast test execution (no disk I/O)
- Clean state per test (new DB per test function)
- Identical query behavior to production

**Pattern for queue/library tests:**
```go
func setupTestDB(t *testing.T) *database.DB {
    t.Helper()
    db, err := database.NewTestDB(t)  // in-memory, migrations applied
    require.NoError(t, err)
    return db
}

func TestSetQueueAndNavigate(t *testing.T) {
    db := setupTestDB(t)
    q := queue.NewQueue(slog.Default(), db)
    // No SetContext needed — test without Wails runtime
    // Test pure queue logic without event emission
}
```

**Extract testable pure logic from Player:** Volume math (`UserVolume` → `Volume` conversion), state serialization, and format detection can be tested without audio hardware. Create `volume_test.go` with pure function tests. *Confidence: HIGH — standard Go testing pattern.*

**Event-driven testing approach:** For packages that emit events, provide a test double or capture mechanism. Options:
1. Accept an `EventEmitter` interface (allows mock in tests)
2. Make event emission optional when `ctx == nil` (already partially the case — `emit` methods check for nil context)
3. Test state mutations independent of event emission

**Recommendation:** Option 2 is already partially implemented. Lean into it: test queue/library state mutations without Wails context, verify state is correct, don't test event emission in unit tests. *Confidence: HIGH.*

---

### Differentiators (Raises Quality Significantly)

These improvements go beyond "not broken" to "genuinely well-engineered." They improve performance, maintainability, and user experience noticeably.

| Improvement | Value Proposition | Complexity | Concern Ref |
|-------------|-------------------|------------|-------------|
| **Eliminate duplicated FTS5 JOIN query pattern** | Same 5-table JOIN repeated 5+ times across search functions. Schema changes require updating all copies. Extract into shared constant or consolidate into fewer sqlc queries. | MEDIUM | Code Quality |
| **Migrate raw SQL in queue persistence to sqlc** | `lookupChunk` and `insertTrackBatch` use `fmt.Sprintf` for batch operations. Use `sqlc.slice()` for lookups. Batch inserts can remain hand-crafted but documented. | MEDIUM | Code Quality |
| **Optimize library store — lazy loading instead of eager fetch** | `eagerFetch()` loads all tracks, albums, artists, genres simultaneously on startup. For 50k+ tracks, this is tens of MB of JS objects loaded before user sees anything. Load only the active view's data. | HIGH | Performance |
| **Optimize queue persistence — incremental updates** | Every add/remove/move does DELETE ALL + INSERT ALL. For a 5000-track queue, every single mutation rewrites the entire table. Use INSERT/DELETE for individual operations; reserve full rewrite for SetQueue. | MEDIUM | Performance |
| **Fix SetQueue Phase 2 redundant lookups** | Phase 2 re-fetches metadata for ALL file paths including those already resolved in Phase 1. Pass Phase 1 results to Phase 2, only lookup remaining paths. | LOW | Performance |
| **Extract testable player logic** | Volume conversion, state serialization, format detection — all testable without audio hardware. Currently locked inside Player struct behind hardware dependency. | LOW | Test Coverage |
| **Event name parity validation** | Event names must match exactly between Go and TypeScript. No compile-time or runtime verification. Add a build-time check (code generation or test). | LOW | Fragile Areas |
| **Polish UI transitions and visual consistency** | CSS transitions for panel open/close, list item hover states, loading skeletons. Makes the app feel responsive and intentional. | MEDIUM | UX |
| **Improve frontend rendering for large libraries** | Even with `lit-virtualizer`, store updates trigger re-renders. Optimize with `repeat()` directive keyed by stable IDs, memoized render functions, and avoiding full-array replacement on updates. | MEDIUM | Performance |

#### FTS5 Query Consolidation Details

**Current state:** The same JOIN pattern appears in:
1. `SearchFTS()` — 5 columns
2. `SearchFTSByFilename()` — 5 columns (same query, different WHERE)
3. `SearchFTSTracks()` — 16 columns (extended version)
4. `RebuildSearchIndex()` — 5 columns (INSERT INTO ... SELECT)
5. `migration2BasenameAndFTS()` — same pattern in migration

**Recommended approach:** Create a SQL view for the common JOIN:

```sql
CREATE VIEW IF NOT EXISTS track_metadata_view AS
SELECT
    af.id AS audio_file_id,
    af.file_path,
    af.length_milliseconds,
    COALESCE(r.name, '') AS title,
    COALESCE(ac.text, '') AS artist,
    COALESCE(rg.name, '') AS album,
    r.track_number,
    r.disc_number,
    -- ... other fields
FROM audio_files af
LEFT JOIN recordings r ON af.recording_id = r.id
LEFT JOIN artist_credit ac ON r.artist_credit_id = ac.id
LEFT JOIN (
    SELECT recording_id, MIN(release_group_id) AS release_group_id
    FROM release_group_recordings
    GROUP BY recording_id
) rgr ON r.id = rgr.recording_id
LEFT JOIN release_groups rg ON rgr.release_group_id = rg.id;
```

Then search queries become `SELECT ... FROM search_index si JOIN track_metadata_view tmv ON tmv.audio_file_id = si.rowid WHERE search_index MATCH ?`. Single source of truth for the JOIN pattern.

**Alternative:** Extract the JOIN clause as a Go string constant and compose queries from it. Less elegant but simpler to implement.

**Recommendation:** Use the SQL view approach. SQLite views are essentially macros — no performance penalty. They can be referenced in sqlc queries. Add the view to the schema, then rewrite search queries against it. *Confidence: MEDIUM — SQLite views in sqlc need verification during implementation. The concept is sound, but sqlc's handling of views with FTS5 virtual tables may have edge cases.*

#### Queue Persistence Optimization Details

**Current pattern:**
```
Every mutation → commitMutation() → persistTracks() → DELETE ALL + batch INSERT ALL
```

**Improved pattern:**
```
AddTrack → INSERT single row + shift positions
RemoveTrack → DELETE single row + shift positions  
MoveTrack → UPDATE positions for affected range
SetQueue / RestoreState → DELETE ALL + batch INSERT ALL (keep current)
```

The sqlc queries `InsertQueueTrack`, `RemoveQueueTrack`, `ShiftQueuePositionsDown`, `ShiftQueuePositionsUp` already exist but aren't used by `commitMutation()`. Wire them up for single-track operations.

*Confidence: HIGH — the individual queries already exist in sqlc.*

#### Library Store Lazy Loading Details

**Current:** Constructor calls `eagerFetch()` → 4 parallel Wails binding calls → 4 full table scans with JOINs → all data in JS memory.

**Improved pattern:**
```typescript
class LibraryStore {
    // Load on first access, not constructor
    async getTracks(): Promise<library.Track[]> {
        if (this.tracks !== null) return this.tracks;
        // ... existing lazy logic (already implemented!)
    }

    // Remove eagerFetch() from constructor
    constructor() {
        EventsOn(Events.LibraryScanComplete, () => this.invalidate());
        this.loadCoverSize();
        // Don't call eagerFetch() — let components trigger loading
    }
}
```

The store *already has* lazy loading logic in `getTracks()`, `getAlbums()`, etc. The only change needed is removing the `eagerFetch()` call from the constructor and from `invalidate()`. Components already call the async getters. The eager fetch is redundant.

**For even larger libraries (100k+):** Consider pagination. Backend already returns full result sets — add `LIMIT/OFFSET` or cursor-based pagination to the sqlc queries. Frontend virtualizer already handles rendering — it just needs a data provider that fetches pages instead of the full list.

*Confidence: HIGH — the lazy loading infrastructure already exists.*

#### Frontend Performance Details

**Already in place:** `@lit-labs/virtualizer` with `flow` layout for track-list and `grid` layout for cover-grid. This handles DOM virtualization.

**Additional optimizations:**
1. **Use `repeat()` with stable keys for virtualized lists.** Lit's `repeat` directive reorders DOM nodes instead of recreating them when list order changes. Use `track.filePath` as key (unique, stable).
2. **Avoid full-array replacement in store updates.** When a scan completes, `invalidate()` sets `tracks = null` forcing a full refetch. Instead, diff the new data against cached data and apply deltas. For scan completion, a full invalidation is appropriate, but for queue mutations, use the delta protocol already in place (`applyTracksDelta`).
3. **Debounce store notifications.** When multiple store properties update in rapid succession (e.g., during scan), batch notifications using `queueMicrotask()` instead of notifying per-property.

*Confidence: MEDIUM — `repeat()` performance gains depend on the update patterns. For initially sorted lists that rarely reorder, `map()` is equally fast. For the cover-grid with resize/reflow, `repeat()` is clearly beneficial.*

---

### Anti-Features (Things to Deliberately NOT Do During Refactoring)

| Anti-Pattern | Why Tempting | Why Problematic | What to Do Instead |
|-------------|-------------|-----------------|-------------------|
| **Splitting large files purely for line count** | `playlist.go` (1778 lines) and `library.go` (1328 lines) feel large. Some components exceed 2000 lines. | The project explicitly decided against cosmetic splitting (PROJECT.md: "No cosmetic file splitting"). Splitting for its own sake creates navigation overhead and can break logical grouping. | Extract only when it enables reuse (e.g., shared controllers) or fixes a real problem (e.g., testing). |
| **Adding a full ORM or query builder** | Raw SQL in `lookupChunk`/`insertTrackBatch` feels inconsistent with sqlc-generated code. | An ORM would fight the existing sqlc architecture. A query builder adds a dependency for 2-3 queries. The hand-crafted SQL is safe (parameterized) and performant. | Document the hand-crafted queries with `// SAFETY:` comments explaining why they're not in sqlc. Use `sqlc.slice()` where it fits. Accept that batch INSERT with dynamic row count is a legitimate sqlc gap for SQLite. |
| **Rewriting the event system** | Event names are fragile strings that must match between Go and TypeScript. A typed event system would be safer. | The current system works. A rewrite touches every component in both frontend and backend. The risk-to-reward ratio is terrible for a consolidation milestone. | Add a build-time parity check (a test or codegen script that compares event constants). Fix the symptom (fragility) not the architecture. |
| **Adding frontend unit tests for all components** | No frontend tests exist. The temptation is to add comprehensive Lit component testing. | Large Lit components (1400-2600 lines) are expensive to test in isolation. Testing requires JSDOM or a browser harness, Shadow DOM handling, and Wails binding mocks. The backend is the source of truth — frontend bugs are visual, not data-corruption. | Test frontend-only logic (search ranking, column sorting, selection controller) as pure function tests if extracted. Defer full component testing to a future milestone. |
| **Making all queue mutations atomic/transactional from Go to frontend** | The delta protocol between queue store and backend could diverge. Adding sequence numbers or full-state hashes seems robust. | The existing `QueueChanged` event already acts as periodic full-state correction. Adding a sequence protocol adds complexity to every mutation path for a problem that manifests as a temporary visual glitch, self-correcting on the next full emit. | Keep the existing delta + periodic full-state pattern. If divergence becomes a real problem (not theoretical), add a generation counter then. |
| **Over-engineering error types** | The project uses sentinel errors and `fmt.Errorf("%w")`. Defining custom error types with fields (e.g., `ScanError{File, Phase, Cause}`) seems more structured. | Custom error types add boilerplate for minimal benefit in a desktop app. The structured logging already captures context via slog key-value pairs. Error types shine in API servers where callers branch on error details — not here. | Keep sentinel errors for `errors.Is()` checks. Keep `fmt.Errorf("%w")` for wrapping with context. Use `errors.Join()` for accumulation. Separate warnings from fatal errors in scan results via the return signature, not error types. |
| **Adding connection pooling or health checks for SQLite** | PROJECT.md mentions "No Database Connection Pooling/Health Check" in missing features. | This is a desktop app with a local SQLite file and `SetMaxOpenConns(1)`. Connection pooling is meaningless. Health checks add complexity for a failure mode (corrupt SQLite file) that's better handled by "show error dialog, suggest DB reset." | Leave as-is. This was correctly scoped as out-of-scope in PROJECT.md. |
| **Wrapping the entire test suite in Docker for CI** | Integration tests require audio hardware. Docker could theoretically provide a virtual audio device. | Massive CI complexity for marginal benefit. The goal is to make unit tests work without hardware, not to make integration tests work in CI. | Extract testable pure logic. Run unit tests in CI. Keep integration tests as manual/local-only with `YELLOWJACKET_INTEGRATION=1`. |

---

## Feature Dependencies

```
[Fix SetContext races]
    └── (no deps — standalone fix)

[Fix error handling gaps (MPRIS, artist credit, config perms)]
    └── (no deps — standalone fixes)

[Separate scan warnings from fatal errors]
    └── (no deps — changes Library.Scan return signature)

[Add in-memory SQLite test infrastructure]
    └──requires──> [database.NewTestDB() helper]
                       └──enables──> [Queue unit tests]
                       └──enables──> [Library unit tests]  
                       └──enables──> [Database layer tests]
                       └──enables──> [Config tests]

[Extract testable player logic]
    └── (no deps — pure function extraction)
    └──enables──> [Player pure logic tests]

[FTS5 query consolidation (SQL view)]
    └──should-precede──> [Database layer tests]
        (test the consolidated queries, not the duplicated ones)

[Queue persistence optimization (incremental updates)]
    └──should-precede──> [Queue unit tests]
        (test the optimized persistence, not the DELETE-ALL pattern)

[Library store lazy loading]
    └── (no deps — remove eagerFetch() call)

[SetQueue Phase 2 optimization]
    └──requires──> [Queue unit tests]
        (need tests to verify the optimization doesn't break resolution)

[Event name parity validation]
    └── (no deps — standalone build-time check)

[UI polish / transitions]
    └── (no deps — CSS-only or Lit reactive changes)

[Frontend rendering optimization]
    └──benefits-from──> [Library store lazy loading]
        (less data in memory = faster re-renders)
```

### Dependency Notes

- **Test infrastructure is the critical enabler:** Almost all other improvements benefit from having tests first (to verify refactoring safety) or should happen before tests (to test the right code). The ordering matters: fix persistence patterns *before* writing persistence tests, consolidate SQL *before* writing SQL tests.
- **Concurrency fixes are independent:** They're small, self-contained, and should be done first — they represent known correctness issues.
- **Performance optimizations benefit from tests:** The queue persistence optimization and SetQueue Phase 2 fix both modify core queue logic. Having queue tests first provides a safety net.
- **Frontend work is independent of backend work:** Library store lazy loading, UI polish, and rendering optimization don't depend on backend changes.

---

## Prioritization

### Phase 1: Correctness & Test Foundation (Do First)

Fixes known bugs and establishes the test infrastructure that makes everything else safe.

- [ ] Fix SetContext data races (Queue, Library, Playlist) — LOW effort, HIGH value
- [ ] Fix package-level `startupErr` → struct field — LOW effort
- [ ] Fix config file permissions — LOW effort
- [ ] Fix swallowed errors (MPRIS, artist credit) — LOW effort
- [ ] Separate scan warnings from fatal errors — MEDIUM effort
- [ ] Create in-memory SQLite test helper (`database.NewTestDB()`) — MEDIUM effort
- [ ] Extract testable player pure logic (volume, state) — LOW effort

### Phase 2: SQL & Performance Foundations (Do Second)

Improves the code that tests will be written against.

- [ ] Consolidate FTS5 JOIN pattern (SQL view or constant) — MEDIUM effort
- [ ] Migrate queue lookups to `sqlc.slice()` — MEDIUM effort
- [ ] Optimize queue persistence (incremental updates) — MEDIUM effort
- [ ] Fix SetQueue Phase 2 redundant lookups — LOW effort
- [ ] Remove `eagerFetch()` from library store constructor — LOW effort

### Phase 3: Comprehensive Tests (Do Third)

Tests verify the improved code from Phases 1-2.

- [ ] Queue unit tests (SetQueue, navigation, shuffle, repeat, persistence) — HIGH effort
- [ ] Library scan unit tests (metadata, entity cache, orphan cleanup) — HIGH effort
- [ ] Database layer tests (FTS5 queries, migrations) — MEDIUM effort
- [ ] Config tests (load/save roundtrip, validation, defaults) — MEDIUM effort
- [ ] Player pure logic tests (volume math, state serialization) — LOW effort
- [ ] Event name parity test — LOW effort

### Phase 4: Polish & Frontend (Do Last)

Visual and frontend improvements that don't affect backend correctness.

- [ ] UI transitions and responsive feedback — MEDIUM effort
- [ ] Frontend rendering optimization (repeat directive, debounced notifications) — MEDIUM effort
- [ ] Document intentional exceptions (hand-crafted SQL, singleton store lifecycle) — LOW effort

## Feature Prioritization Matrix

| Improvement | Reliability Value | Implementation Cost | Priority |
|-------------|-------------------|---------------------|----------|
| Fix SetContext data races | HIGH | LOW | **P1** |
| Fix startupErr, config perms | HIGH | LOW | **P1** |
| Fix swallowed errors | HIGH | LOW | **P1** |
| Separate scan warnings/errors | HIGH | MEDIUM | **P1** |
| In-memory SQLite test helper | HIGH | MEDIUM | **P1** |
| Extract testable player logic | MEDIUM | LOW | **P1** |
| FTS5 query consolidation | MEDIUM | MEDIUM | **P2** |
| Queue persistence optimization | MEDIUM | MEDIUM | **P2** |
| SetQueue Phase 2 fix | MEDIUM | LOW | **P2** |
| Library store lazy loading | MEDIUM | LOW | **P2** |
| Queue unit tests | HIGH | HIGH | **P2** |
| Library unit tests | HIGH | HIGH | **P2** |
| Database tests | MEDIUM | MEDIUM | **P2** |
| Config tests | MEDIUM | MEDIUM | **P2** |
| Event name parity validation | MEDIUM | LOW | **P2** |
| Player pure logic tests | MEDIUM | LOW | **P2** |
| UI transitions / polish | LOW | MEDIUM | **P3** |
| Frontend rendering optimization | LOW | MEDIUM | **P3** |
| Migrate queue SQL to sqlc | LOW | MEDIUM | **P3** |

**Priority key:**
- P1: Must do — correctness issues or critical enablers
- P2: Should do — significant quality improvement
- P3: Nice to have — polish, can defer if time-constrained

## Sources

- Go race detector: https://go.dev/doc/articles/race_detector — HIGH confidence (official Go docs)
- sqlc `sqlc.slice()` for SQLite: https://docs.sqlc.dev/en/stable/reference/macros.html — HIGH confidence (official sqlc docs, verified via WebFetch)
- sqlc batch operations: https://docs.sqlc.dev/en/stable/howto/select.html#mysql-and-sqlite — HIGH confidence (official docs)
- Lit `repeat` directive: https://lit.dev/docs/templates/lists/#the-repeat-directive — HIGH confidence (official Lit docs, verified via WebFetch)
- Lit rendering model: https://lit.dev/docs/components/rendering/ — HIGH confidence (official docs)
- `@lit-labs/virtualizer` — already in use in codebase (track-list, cover-grid)
- `modernc.org/sqlite` in-memory DB — HIGH confidence (`:memory:` is standard SQLite, driver already in deps)
- Go `errors.Join()` — HIGH confidence (standard library since Go 1.20, already used in codebase)
- Go mutex patterns — HIGH confidence (standard library, matches existing codebase conventions)

---
*Feature research for: YellowJacket consolidation milestone*
*Researched: 2026-02-27*
