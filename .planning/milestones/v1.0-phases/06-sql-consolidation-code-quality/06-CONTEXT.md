# Phase 6: SQL Consolidation & Code Quality - Context

**Gathered:** 2026-03-04
**Status:** Ready for planning

<domain>
## Phase Boundary

Eliminate duplicated SQL patterns (FTS5 5-table JOIN), automate Go-to-TypeScript event constant synchronization, migrate eligible hand-crafted SQL to sqlc, and document all intentional sqlc exceptions with SAFETY comments. No new features, no schema changes beyond the VIEW migration.

</domain>

<decisions>
## Implementation Decisions

### FTS5 VIEW Design
- Single VIEW named `track_metadata` with all 16 columns (file_path, length, title, artist, album, track_number, disc_number, genre, year, composer, file_type, sample_rate, bit_depth, channels, bitrate, file_size)
- VIEW rooted on `audio_files` (not `search_index`) so it's usable for both search queries (JOIN search_index to VIEW) and index rebuilds (SELECT directly from VIEW)
- Created as migration 4 (next sequential PRAGMA user_version bump)
- Lightweight search queries (SearchFTS, SearchFTSByFilename) SELECT only the 5 columns they need from the VIEW — SQLite optimizes unused columns away
- RebuildSearchIndex and migration2 INSERT INTO search_index use the VIEW instead of duplicating the JOIN

### Event Codegen Approach
- Go constants in `backend/events/events.go` are the source of truth
- Generator written in Go, using `go/ast` to parse the const block from events.go
- Wired into `go generate` via `//go:generate` directive on events.go
- Output format matches current `frontend/src/events.ts` structure exactly: `export const Events = { ... } as const;` — zero changes needed in frontend import sites
- Fix the existing `codegen-check` lefthook pre-commit hook (currently hangs) to run the generator and diff the output — fail if TypeScript file is stale
- Note: `LibraryConfigChanged` exists in Go but is missing from TypeScript — the generator will fix this automatically

### sqlc Migration Scope
- Migrate `lookupChunk()` query fully to sqlc: the SELECT + JOINs + `sqlc.slice()` for the IN clause — use the new `track_metadata` VIEW instead of hand-crafted JOINs
- `insertTrackBatch()` (multi-row VALUES with variable row count) stays hand-crafted — sqlc cannot generate variable-length batch INSERTs. Document as exception.
- All FTS5 operations (~11 statements across search.go, library.go, rescan.go) stay hand-crafted — sqlc does not support FTS5 virtual tables (MATCH, rank, content='' tables). Document all as exceptions.

### SAFETY Comment Convention
- Format: two parts — WHY sqlc can't handle it AND what makes it safe
- Example: `// SAFETY: FTS5 MATCH syntax unsupported by sqlc. Query is parameterized; no string interpolation.`
- Scope: runtime query SQL only — migration DDL (ALTER TABLE, CREATE INDEX, PRAGMA) does NOT need SAFETY comments
- Per-statement annotation only — no central registry file. The comments ARE the documentation.
- Cross-reference related operations: FTS5 INSERT/DELETE in library.go and rescan.go should reference search.go functions they relate to (e.g., `// SAFETY: FTS5 virtual table, see search.go:RebuildSearchIndex. Parameterized.`)

### Claude's Discretion
- Exact VIEW column ordering and COALESCE/NULL handling
- Generator CLI interface (flags, output path defaults)
- How to structure the sqlc query file for lookupChunk (naming, placement)
- Exact wording of SAFETY comments (as long as they follow the two-part format)
- How to handle the lefthook codegen-check fix (may need to investigate why it hangs)

</decisions>

<specifics>
## Specific Ideas

- The `track_metadata` VIEW name matches the roadmap suggestion — keep it familiar
- Generator should use `go/ast` for reliable parsing, not regex/string matching on the Go source
- The existing `codegen-check` hook hangs per STATE.md — fixing it is part of this phase, not a separate effort
- `lookupChunk` uses chunking at `maxSQLiteVars = 900` — the sqlc migration must preserve this chunking logic even if the SQL itself moves to sqlc
- The migration code in database.go (migration2) that duplicates the rebuild JOIN should also switch to the VIEW once migration 4 creates it — but since migration 2 runs before migration 4 in sequence, the migration2 code may need to stay as-is for existing databases (Claude should handle this ordering carefully)

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope

</deferred>

---

*Phase: 06-sql-consolidation-code-quality*
*Context gathered: 2026-03-04*
