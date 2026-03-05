# Phase 3: Test Infrastructure - Context

**Gathered:** 2026-03-02
**Status:** Ready for planning

<domain>
## Phase Boundary

Create `database.NewTestDB(t)` — an in-memory SQLite test helper that mirrors production setup (migrations + PRAGMAs) — and apply production SQLite PRAGMAs (`synchronous=NORMAL`, `cache_size=-8000`, `mmap_size=67108864`) to the real `NewDB()`. This phase delivers the test foundation; actual test writing happens in Phases 4-5.

</domain>

<decisions>
## Implementation Decisions

### Test Helper API Shape
- `NewTestDB(t *testing.T)` returns `*DB` only — no cleanup function, no error return
- Cleanup registered internally via `t.Cleanup()` — callers just use the DB and forget
- No functional options — every test DB gets the full production-mirror setup (PRAGMAs + all migrations)
- Does NOT expose raw `*sql.DB` — tests use `DB.ExecContext()` / `DB.Queries` like production code
- Lives in `database/testhelper.go` (exported, importable by other packages)

### PRAGMA Behavior
- All PRAGMAs applied identically in tests and production — even `mmap_size` on `:memory:` (verifies code path, true mirror)
- Shared `applyPRAGMAs(*sql.DB)` internal function called by both `NewDB()` and `NewTestDB()` — single source of truth
- Test DBs use the same connection string params as production (`?_busy_timeout=5000&_journal_mode=WAL`)
- PRAGMAs applied before schema creation — tuning first, then DDL/DML

### Test Helper Scope
- No test data seeding helpers in Phase 3 — Phases 4-5 create fixtures as needed
- Future test phases should use `sqlcgen.Queries` (not raw SQL) for inserting test data — same path as production
- Skip the orphan cleanup query in `NewTestDB` — test DBs start empty, no orphans to clean
- No health check (SELECT 1) — trust that successful Open + PRAGMAs + migrations means the DB is usable

### Claude's Discretion
- Internal helper function naming (`applyPRAGMAs` vs `configurePRAGMAs` vs similar)
- Whether `NewTestDB` calls `t.Fatal()` or `t.Helper()` + `t.Fatal()` on setup failure
- Exact error wrapping style in the shared PRAGMA function

</decisions>

<specifics>
## Specific Ideas

- The shared `applyPRAGMAs` function is the key architectural piece — it prevents production and test PRAGMA sets from drifting apart
- `NewTestDB` should mirror the `NewDB` code path as closely as possible, minus the file-path resolution and orphan cleanup
- Connection string for test: `":memory:?_busy_timeout=5000&_journal_mode=WAL"` (same params, in-memory URI)

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope

</deferred>

---

*Phase: 03-test-infrastructure*
*Context gathered: 2026-03-02*
