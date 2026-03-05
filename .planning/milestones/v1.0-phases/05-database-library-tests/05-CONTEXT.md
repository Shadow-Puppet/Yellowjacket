# Phase 5: Database & Library Tests - Context

**Gathered:** 2026-03-04
**Status:** Ready for planning

<domain>
## Phase Boundary

Write unit tests for FTS5 search queries, migrations, library scan, and entity cache — locking down current behavior before SQL consolidation (Phase 6) and performance optimization (Phase 7). Covers requirements TEST-03 (~10-15 database tests) and TEST-06 (~10-15 library tests). All tests must pass with `-race` flag enabled.

</domain>

<decisions>
## Implementation Decisions

### FTS5 search test coverage
- Test all three search functions independently: SearchFTS (general), SearchFTSByFilename (column-scoped), SearchFTSTracks (full track details) — each has its own SQL and result mapping
- Test tokenizer/query builder as separate unit tests: tokeniseForFTS, buildFTSQuery, stripExtForSearch — catches edge cases without needing a database
- Assert exact result ordering for ranking tests — seed specific data and verify precise BM25 ordering for known inputs
- Test diacritics behavior: searching 'Beyonce' must find 'Beyoncé' — this is a configured tokenizer behavior (unicode61 remove_diacritics 2) that could break if config changes
- Test scenarios: basic terms, empty query, special characters (quotes, slashes like AC/DC), multi-word queries, column-scoped filename search

### Library scan test boundaries
- Unit test individual functions only — no full Scan() integration tests, no filesystem walking, no Wails event mocking
- Testable functions: processMetadata, commitBatch, orphan deletion (DeleteAudioFile + DeleteSearchIndex), entity cache functions, pure helpers (getRecordingName, toNullInt64, toNullString, splitGenres, mapTrackRow)
- Construct metadata structs inline in each test — maximum clarity per test, no shared metadata builders
- Orphan cleanup: test at DB level only — seed audio files + search index entries in DB, call delete functions, verify they're gone. Do not test the sync.Map tracking pattern
- Verify functions work with plain context.Context (t.Context()) — documents that core processing functions have no Wails runtime dependency

### Entity cache test strategy
- Test cache functions directly: cachedUpsertArtistCredit, cachedLinkArtist, cachedUpsertGenre, resolveReleaseGroup — each with a test DB and fresh entityCache
- Test multi-credit scenario: same artist name appearing in different credits (e.g., solo artist vs. band member) — verify artist cached once but linked to multiple credits correctly
- Test linkedCredits cache prevents duplicate INSERTs: calling cachedLinkArtist twice with same artist+credit should not attempt a second INSERT (prevents hitting UNIQUE constraint)
- Test behavior with missing/empty fields: empty artist name, no album, missing title — documents what happens when metadata is incomplete

### Test data & fixture approach
- Seed data via raw SQL (db.ExecContext) — consistent with queue test patterns from Phase 4, explicit control, no dependency on production code correctness
- Use realistic music metadata: real-looking names like 'Bohemian Rhapsody', 'Queen', 'A Night at the Opera' — easier to reason about search behavior and ranking
- Shared seed helper for search tests: one function (e.g., seedSearchData) seeds ~5-10 tracks with varied metadata for search tests to query against
- New seed function, not extending existing seedAudioFiles — Phase 5 needs the full entity graph (release_groups, genres, search_index entries, cover_art) beyond what seedAudioFiles provides

### Claude's Discretion
- Exact number of tests per function (within the ~10-15 targets per package)
- Test file organization (single file vs. split by concern)
- Specific realistic metadata values chosen for seed data
- Helper function signatures and API design
- Which pure helper functions are worth individual tests vs. tested through higher-level functions
- Migration test specifics (what to verify beyond "migrations run successfully")

</decisions>

<specifics>
## Specific Ideas

- Follow established patterns from queue tests: t.Parallel(), setupTest helpers, standard library testing (no testify), mock interfaces for dependencies, TestFunctionName_Scenario naming
- NewTestDB(t) already exists in database/testhelper.go — use it directly for database package tests (same package, access to unexported functions)
- The contentless FTS5 table (content='') means rowid must be manually managed in seed data — rowid must match audio_files.id
- Search functions share the same 5-table JOIN pattern — testing all three independently creates a safety net before Phase 6's VIEW consolidation

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope

</deferred>

---

*Phase: 05-database-library-tests*
*Context gathered: 2026-03-04*
