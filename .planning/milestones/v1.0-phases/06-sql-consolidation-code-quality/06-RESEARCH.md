# Phase 6: SQL Consolidation & Code Quality - Research

**Researched:** 2026-03-04
**Domain:** SQLite VIEW consolidation, Go codegen, sqlc advanced features
**Confidence:** HIGH

## Summary

Phase 6 eliminates duplicated SQL JOIN patterns, automates Go→TypeScript event synchronization, migrates eligible hand-crafted SQL to sqlc, and documents all intentional sqlc exceptions. The codebase has a well-defined 5-table JOIN pattern (audio_files → recordings → artist_credit → release_group_recordings subquery → release_groups) duplicated across **10+ locations** in both hand-crafted Go SQL and sqlc query files. This pattern can be consolidated into a single SQLite VIEW named `track_metadata`.

Verification confirms that sqlc v1.30.0 (the project's current version) fully supports querying from VIEWs and using `sqlc.slice()` for IN clauses with the SQLite engine — both features were tested directly against the project's toolchain. The event codegen task is straightforward: `go/ast` can parse the 4 const blocks in `events.go` (21 constants) and produce the matching TypeScript `events.ts` output. The existing `codegen-check` lefthook hook currently runs `go generate ./...` which was observed to hang in earlier phases (templ generation timeout), but testing now shows it completes in under 1 second — the fix may simply be wiring the new generator into the existing hook and verifying it works end-to-end.

**Primary recommendation:** Create the `track_metadata` VIEW as migration 4, update all search/rebuild queries to use it, write the event codegen tool using `go/ast`, migrate `lookupChunk` to sqlc with `sqlc.slice()`, and annotate all remaining hand-crafted SQL with `// SAFETY:` comments.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
- Single VIEW named `track_metadata` with all 16 columns (file_path, length, title, artist, album, track_number, disc_number, genre, year, composer, file_type, sample_rate, bit_depth, channels, bitrate, file_size)
- VIEW rooted on `audio_files` (not `search_index`) so it's usable for both search queries (JOIN search_index to VIEW) and index rebuilds (SELECT directly from VIEW)
- Created as migration 4 (next sequential PRAGMA user_version bump)
- Lightweight search queries (SearchFTS, SearchFTSByFilename) SELECT only the 5 columns they need from the VIEW — SQLite optimizes unused columns away
- RebuildSearchIndex and migration2 INSERT INTO search_index use the VIEW instead of duplicating the JOIN
- Go constants in `backend/events/events.go` are the source of truth
- Generator written in Go, using `go/ast` to parse the const block from events.go
- Wired into `go generate` via `//go:generate` directive on events.go
- Output format matches current `frontend/src/events.ts` structure exactly: `export const Events = { ... } as const;` — zero changes needed in frontend import sites
- Fix the existing `codegen-check` lefthook pre-commit hook (currently hangs) to run the generator and diff the output — fail if TypeScript file is stale
- Note: `LibraryConfigChanged` exists in Go but is missing from TypeScript — the generator will fix this automatically
- Migrate `lookupChunk()` query fully to sqlc: the SELECT + JOINs + `sqlc.slice()` for the IN clause — use the new `track_metadata` VIEW instead of hand-crafted JOINs
- `insertTrackBatch()` (multi-row VALUES with variable row count) stays hand-crafted — sqlc cannot generate variable-length batch INSERTs. Document as exception.
- All FTS5 operations (~11 statements across search.go, library.go, rescan.go) stay hand-crafted — sqlc does not support FTS5 virtual tables (MATCH, rank, content='' tables). Document all as exceptions.
- Format: two parts — WHY sqlc can't handle it AND what makes it safe
- Example: `// SAFETY: FTS5 MATCH syntax unsupported by sqlc. Query is parameterized; no string interpolation.`
- Scope: runtime query SQL only — migration DDL (ALTER TABLE, CREATE INDEX, PRAGMA) does NOT need SAFETY comments
- Per-statement annotation only — no central registry file. The comments ARE the documentation.
- Cross-reference related operations: FTS5 INSERT/DELETE in library.go and rescan.go should reference search.go functions they relate to

### Claude's Discretion
- Exact VIEW column ordering and COALESCE/NULL handling
- Generator CLI interface (flags, output path defaults)
- How to structure the sqlc query file for lookupChunk (naming, placement)
- Exact wording of SAFETY comments (as long as they follow the two-part format)
- How to handle the lefthook codegen-check fix (may need to investigate why it hangs)

### Deferred Ideas (OUT OF SCOPE)
None — discussion stayed within phase scope
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|-----------------|
| QUAL-01 | Duplicated FTS5 JOIN pattern (5+ copies) consolidated into single SQLite VIEW | VIEW `track_metadata` verified working with sqlc v1.30.0; 10+ duplicate JOIN sites identified across search.go, database.go, audio_files.sql, playlists.sql, genres.sql, persistence.go |
| QUAL-02 | Event constants generated from Go to TypeScript via codegen, wired into go generate and pre-commit hook | 21 Go constants in 4 const blocks parseable by `go/ast`; TypeScript has 20 (missing `LibraryConfigChanged`); `go generate ./...` completes in <1s; lefthook codegen-check hook exists but needs generator wiring |
| QUAL-03 | Queue batch lookups use sqlc.slice() instead of fmt.Sprintf placeholder construction | `sqlc.slice()` confirmed working with SQLite engine in sqlc v1.30.0 (tested directly); `lookupChunk` in persistence.go is the target; chunking logic must be preserved at caller level |
| QUAL-04 | Hand-crafted SQL exceptions documented with // SAFETY: comments | ~11 FTS5 statements + 1 insertTrackBatch identified; two-part comment format decided |
</phase_requirements>

## Standard Stack

### Core
| Tool | Version | Purpose | Why Standard |
|------|---------|---------|--------------|
| sqlc | v1.30.0 | SQL-to-Go codegen | Already in use (`go tool sqlc`); supports VIEWs and `sqlc.slice()` for SQLite |
| go/ast | stdlib (Go 1.25) | Parse Go const blocks for event codegen | Standard library, no dependencies; reliable AST parsing |
| go/parser | stdlib (Go 1.25) | Parse Go source files | Used with go/ast for the event generator |
| go/token | stdlib (Go 1.25) | Token positions for AST parsing | Required by go/parser |

### Supporting
| Tool | Version | Purpose | When to Use |
|------|---------|---------|-------------|
| lefthook | v1.13.6+ | Pre-commit hook runner | Wire event codegen check into existing `codegen-check` hook |
| modernc.org/sqlite | v1.45.0 | SQLite driver (pure Go) | Already in use; VIEW support is standard SQLite |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| go/ast | Regex parsing of events.go | Fragile, breaks on comments/formatting changes; go/ast is robust |
| SQLite VIEW | Rewrite all queries in sqlc | FTS5 queries can't use sqlc; VIEW gives partial consolidation |
| sqlc.slice() | Keep hand-crafted lookupChunk | sqlc.slice() is cleaner and eliminates manual placeholder construction |

## Architecture Patterns

### VIEW Schema Location
```
backend/database/sql/schemas/
├── ...existing schema files...
└── track_metadata_view.sql    # CREATE VIEW IF NOT EXISTS track_metadata
```

The VIEW SQL file goes in the schemas directory so sqlc can see it during code generation. File naming should sort after the tables it depends on (alphabetical ordering puts `track_metadata_view.sql` after all table schemas).

**Important:** `CREATE VIEW IF NOT EXISTS` is the correct DDL for the schema file. The VIEW will also be created by migration 4 for existing databases, but the schema file ensures sqlc knows about it and new databases get it automatically.

### Pattern 1: VIEW Definition
**What:** The `track_metadata` VIEW consolidates the 5-table JOIN into a reusable SQL object
**When to use:** Any query needing audio file metadata with title/artist/album
**Example:**
```sql
-- In backend/database/sql/schemas/track_metadata_view.sql
CREATE VIEW IF NOT EXISTS track_metadata AS
SELECT
    af.id,
    af.file_path,
    af.length_milliseconds,
    COALESCE(r.name, '') AS title,
    COALESCE(ac.text, '') AS artist_name,
    r.track_number,
    r.disc_number,
    COALESCE(rg.name, '') AS album,
    CAST(COALESCE(
        (SELECT GROUP_CONCAT(g.name, '||')
         FROM recording_genres rg_sub
         JOIN genres g ON rg_sub.genre_id = g.id
         WHERE rg_sub.recording_id = r.id),
        ''
    ) AS TEXT) AS genre,
    COALESCE(r.year, 0) AS year,
    COALESCE(r.composer, '') AS composer,
    COALESCE(ft.extension, '') AS file_type,
    af.sample_rate,
    af.bit_depth,
    af.channels,
    af.bitrate,
    af.file_size
FROM audio_files af
LEFT JOIN recordings r ON af.recording_id = r.id
LEFT JOIN artist_credit ac ON r.artist_credit_id = ac.id
LEFT JOIN (
    SELECT recording_id,
        MIN(release_group_id) AS release_group_id
    FROM release_group_recordings
    GROUP BY recording_id
) rgr ON r.id = rgr.recording_id
LEFT JOIN release_groups rg ON rgr.release_group_id = rg.id
LEFT JOIN file_types ft ON af.file_type_id = ft.id;
```

**Note:** The VIEW includes `af.id` (needed for FTS5 rowid matching and queue lookups). The `id` column is included in the VIEW but queries don't have to select it. It also uses LEFT JOIN throughout (not INNER JOIN) to match the existing pattern — some audio files may have recording_id=0 (no metadata yet).

### Pattern 2: Search Queries Using VIEW
**What:** FTS5 search queries JOIN search_index to the VIEW
**When to use:** SearchFTS, SearchFTSByFilename, SearchFTSTracks
**Example:**
```sql
-- Hand-crafted (stays in search.go — FTS5 MATCH unsupported by sqlc)
SELECT
    tm.file_path,
    tm.length_milliseconds,
    tm.title,
    tm.artist_name,
    tm.album
FROM search_index si
JOIN track_metadata tm ON tm.id = si.rowid
WHERE search_index MATCH ?
ORDER BY rank
LIMIT ?
```

### Pattern 3: Rebuild Using VIEW
**What:** RebuildSearchIndex selects directly from VIEW
**When to use:** Full FTS5 index rebuild, migration 2 FTS population
**Example:**
```sql
-- Hand-crafted (stays in search.go — FTS5 INSERT unsupported by sqlc)
INSERT INTO search_index(rowid, file_path, title, artist, album)
SELECT id, file_path, title, artist_name, album
FROM track_metadata
```

### Pattern 4: sqlc.slice() for Batch Lookups
**What:** Queue lookupChunk migrated to sqlc query using VIEW + sqlc.slice()
**When to use:** Batch file path lookups in queue persistence
**Example:**
```sql
-- In backend/database/sql/queries/queue.sql (or audio_files.sql)
-- name: LookupTrackMetaBatch :many
SELECT id, file_path, title, artist_name
FROM track_metadata
WHERE file_path IN (sqlc.slice('paths'));
```

**Critical note:** The generated sqlc code does NOT handle chunking — it generates a single query with all placeholders. The caller (`lookupTrackMetaBatch`) must still chunk the paths array at `maxSQLiteVars = 900` before calling the generated method. The chunking loop stays; only the inner SQL construction moves to sqlc.

### Pattern 5: Event Codegen with go/ast
**What:** Go program reads events.go const blocks, generates events.ts
**When to use:** Automated via `//go:generate` directive
**Example structure:**
```go
// backend/events/gen_events_ts.go (or cmd/gen-events/main.go)
package main

import (
    "go/ast"
    "go/parser"
    "go/token"
    // ...
)

func main() {
    fset := token.NewFileSet()
    f, err := parser.ParseFile(fset, "events.go", nil, parser.ParseComments)
    // Walk AST, extract const declarations
    // Group by comment blocks (Playback, Queue, Config, Playlist, Library)
    // Generate TypeScript output matching current format
}
```

### Anti-Patterns to Avoid
- **Don't put the VIEW in a migration-only file without the schema file:** sqlc needs the VIEW definition in the schema directory to generate code against it. The migration creates it for existing DBs; the schema file teaches sqlc about it.
- **Don't remove chunking from lookupTrackMetaBatch:** `sqlc.slice()` doesn't auto-chunk. SQLite has a bind variable limit (~32766 in newer versions, but the project uses a conservative 900). The chunking loop must remain.
- **Don't try to make FTS5 queries use sqlc:** FTS5 MATCH syntax, `content=''` virtual tables, and rank ordering are unsupported by sqlc's parser. These must stay hand-crafted.
- **Don't change the migration2 code to use the VIEW for DB version < 4:** Migration 2 runs before migration 4 in sequence. For databases upgrading from version 1→4, migration 2 must still work without the VIEW. Only databases already at version ≥ 4 (including fresh DBs) should use the VIEW in the rebuild path.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Go AST parsing | Regex/string matching on events.go | `go/ast` + `go/parser` + `go/token` | Handles comments, multiline, formatting robustly |
| SQL IN clause placeholder construction | `fmt.Sprintf` with manual `?` joining | `sqlc.slice()` | Generates correct placeholder expansion; type-safe |
| Duplicate JOIN patterns | Copy-paste SQL across files | SQLite VIEW | Single source of truth; SQLite optimizes unused columns |

**Key insight:** The manual placeholder construction in `lookupChunk` is exactly the pattern `sqlc.slice()` was designed to replace — sqlc generates the same `strings.Replace` / `strings.Repeat` code but with type safety and no manual `args` slice building.

## Common Pitfalls

### Pitfall 1: Migration Ordering with VIEW
**What goes wrong:** migration2 tries to SELECT from `track_metadata` VIEW before migration 4 creates it
**Why it happens:** Migrations run sequentially by version number. A database at version 1 runs migration 2 (which populates FTS) before migration 4 (which creates the VIEW).
**How to avoid:** Keep the existing inline JOIN in `migration2BasenameAndFTS`. Only the `RebuildSearchIndex` function (called at runtime, not during migration) should use the VIEW. The VIEW schema file handles fresh databases; migration 4 handles existing databases.
**Warning signs:** `no such table: track_metadata` error during migration

### Pitfall 2: sqlc Schema File Ordering
**What goes wrong:** sqlc fails to parse the VIEW definition because it references tables not yet defined
**Why it happens:** sqlc processes schema files in filesystem order. If `track_metadata_view.sql` sorts before the tables it references, sqlc can't resolve them.
**How to avoid:** Name the file so it sorts after all dependencies. `track_metadata_view.sql` sorts after `recordings.sql`, `release_groups.sql`, etc. (all start with lowercase letters before 't'). Alternatively, prefix with `zz_` if needed, but alphabetical ordering of `track_metadata_view.sql` already works.
**Warning signs:** sqlc generate errors about unknown tables/columns

### Pitfall 3: VIEW Column Mismatch with Existing Queries
**What goes wrong:** Queries that used INNER JOINs (e.g., `GetAllTracksWithFullMetadata` uses `JOIN recordings r` not `LEFT JOIN`) return different results when switched to the VIEW (which uses LEFT JOINs)
**Why it happens:** The VIEW uses LEFT JOINs to handle audio files without metadata. Existing sqlc queries that use INNER JOINs implicitly filter out unmatched rows.
**How to avoid:** Only replace queries that already use LEFT JOINs (search queries, playlist metadata queries, SearchAudioFilesByBasename). Leave queries with intentional INNER JOINs (like `GetAllTracksWithFullMetadata`) as-is, or add `WHERE r.id IS NOT NULL` to preserve INNER JOIN semantics. Carefully review each query's JOIN type before converting.
**Warning signs:** Extra rows with empty metadata appearing in results

### Pitfall 4: codegen-check Hook Scope
**What goes wrong:** The event generator is added to `go generate` but the codegen-check hook still runs the full `go generate ./...` which includes templ and sqlc, making it slow
**Why it happens:** The hook runs all generators, not just the event one
**How to avoid:** The hook currently runs `go generate ./...` and then diffs. This approach is actually fine — testing shows `go generate ./...` completes in <1 second when nothing has changed. The hanging issue from earlier phases appears to be resolved. Verify the hook works end-to-end after wiring in the new generator.
**Warning signs:** Hook taking >5 seconds (should be <2s)

### Pitfall 5: sqlc.slice() Empty Slice Behavior
**What goes wrong:** Passing an empty slice to a `sqlc.slice()` query
**Why it happens:** The generated code replaces the placeholder with `NULL` for empty slices, which means `WHERE file_path IN (NULL)` — this matches nothing (correct behavior), but the caller should still handle it
**How to avoid:** The chunking logic in `lookupTrackMetaBatch` already handles empty input (returns empty map). The sqlc-generated code also handles empty slices gracefully (returns empty results). No action needed, but be aware of the behavior.
**Warning signs:** N/A — behavior is correct

### Pitfall 6: Generated TypeScript File Must Be Deterministic
**What goes wrong:** The event generator produces different output on different runs (e.g., map iteration order), causing the codegen-check hook to always fail
**Why it happens:** Go maps don't have deterministic iteration order
**How to avoid:** Use `ast.Inspect` or iterate `f.Decls` in source order (AST preserves declaration order). Don't collect into a map and iterate — iterate the AST directly and emit in declaration order.
**Warning signs:** `codegen-check` hook always shows diff even when events.go hasn't changed

## Code Examples

### Example 1: Migration 4 — Create track_metadata VIEW
```sql
-- In migration 4 (backend/database/database.go)
CREATE VIEW IF NOT EXISTS track_metadata AS
SELECT
    af.id,
    af.file_path,
    af.length_milliseconds,
    COALESCE(r.name, '') AS title,
    COALESCE(ac.text, '') AS artist_name,
    r.track_number,
    r.disc_number,
    COALESCE(rg.name, '') AS album,
    CAST(COALESCE(
        (SELECT GROUP_CONCAT(g.name, '||')
         FROM recording_genres rg_sub
         JOIN genres g ON rg_sub.genre_id = g.id
         WHERE rg_sub.recording_id = r.id),
        ''
    ) AS TEXT) AS genre,
    COALESCE(r.year, 0) AS year,
    COALESCE(r.composer, '') AS composer,
    COALESCE(ft.extension, '') AS file_type,
    af.sample_rate,
    af.bit_depth,
    af.channels,
    af.bitrate,
    af.file_size
FROM audio_files af
LEFT JOIN recordings r ON af.recording_id = r.id
LEFT JOIN artist_credit ac ON r.artist_credit_id = ac.id
LEFT JOIN (
    SELECT recording_id,
        MIN(release_group_id) AS release_group_id
    FROM release_group_recordings
    GROUP BY recording_id
) rgr ON r.id = rgr.recording_id
LEFT JOIN release_groups rg ON rgr.release_group_id = rg.id
LEFT JOIN file_types ft ON af.file_type_id = ft.id;
```

### Example 2: Consolidated SearchFTS Using VIEW
```go
// In search.go — replaces the inline 5-table JOIN
rows, err := d.db.QueryContext(d.Ctx, `
    SELECT
        tm.file_path,
        tm.length_milliseconds,
        tm.title,
        tm.artist_name,
        tm.album
    FROM search_index si
    JOIN track_metadata tm ON tm.id = si.rowid
    WHERE search_index MATCH ?
    ORDER BY rank
    LIMIT ?
`, ftsQuery, limit)
```

### Example 3: Consolidated RebuildSearchIndex Using VIEW
```go
// In search.go — replaces inline JOIN for rebuild
_, err := d.db.ExecContext(d.Ctx, `
    INSERT INTO search_index(rowid, file_path, title, artist, album)
    SELECT id, file_path, title, artist_name, album
    FROM track_metadata
`)
```

### Example 4: sqlc Query for lookupChunk Replacement
```sql
-- In backend/database/sql/queries/queue.sql (or a new track_metadata.sql)
-- name: LookupTrackMetaByPaths :many
SELECT id, file_path, title, artist_name
FROM track_metadata
WHERE file_path IN (sqlc.slice('paths'));
```

### Example 5: Event Generator Core Logic
```go
// Using go/ast to extract constants from events.go
fset := token.NewFileSet()
f, err := parser.ParseFile(fset, eventsGoPath, nil, parser.ParseComments)
if err != nil {
    log.Fatal(err)
}

type eventConst struct {
    Name  string
    Value string
}

var events []eventConst

ast.Inspect(f, func(n ast.Node) bool {
    genDecl, ok := n.(*ast.GenDecl)
    if !ok || genDecl.Tok != token.CONST {
        return true
    }
    for _, spec := range genDecl.Specs {
        vs, ok := spec.(*ast.ValueSpec)
        if !ok || len(vs.Names) == 0 || len(vs.Values) == 0 {
            continue
        }
        lit, ok := vs.Values[0].(*ast.BasicLit)
        if !ok || lit.Kind != token.STRING {
            continue
        }
        name := vs.Names[0].Name
        value := strings.Trim(lit.Value, `"`)
        events = append(events, eventConst{Name: name, Value: value})
    }
    return true
})
```

### Example 6: SAFETY Comment Examples
```go
// SAFETY: FTS5 MATCH syntax unsupported by sqlc. Query is parameterized; no string interpolation.
rows, err := d.db.QueryContext(d.Ctx, `SELECT ... FROM search_index si ... WHERE search_index MATCH ?`, ...)

// SAFETY: FTS5 virtual table INSERT unsupported by sqlc. All values come from track_metadata VIEW; no user input.
_, err := d.db.ExecContext(d.Ctx, `INSERT INTO search_index(rowid, ...) SELECT ... FROM track_metadata`)

// SAFETY: FTS5 virtual table, see search.go:InsertSearchIndex. Parameterized.
_, err := tx.ExecContext(l.ctx, `INSERT INTO search_index(rowid, ...) VALUES (?, ?, ?, ?, ?)`, ...)

// SAFETY: Multi-row INSERT with variable row count unsupported by sqlc. Placeholder count matches args length; no string interpolation.
_, err := tx.ExecContext(q.db.Ctx, query, args...)
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Manual IN clause placeholder | `sqlc.slice()` | sqlc v1.18+ | Type-safe slice parameters for MySQL/SQLite |
| Duplicate JOINs everywhere | SQLite VIEWs | Always available | Single source of truth, optimizer handles unused columns |
| Manual event sync | Codegen from Go→TS | This phase | Eliminates drift (LibraryConfigChanged already missing) |

**Deprecated/outdated:**
- None relevant — all tools are current versions

## Existing Duplicate JOIN Inventory

All locations with the 5-table audio metadata JOIN pattern:

### Hand-Crafted SQL in Go (stay hand-crafted, get SAFETY comments)
| File | Function/Line | Pattern | VIEW Applicable? |
|------|--------------|---------|-----------------|
| `backend/database/search.go:34` | SearchFTS | FTS5 MATCH + 5-table JOIN | Yes — replace JOIN with `JOIN track_metadata` |
| `backend/database/search.go:92` | SearchFTSByFilename | FTS5 MATCH + 5-table JOIN | Yes — replace JOIN with `JOIN track_metadata` |
| `backend/database/search.go:232` | SearchFTSTracks | FTS5 MATCH + 6-table JOIN (+ file_types) | Yes — replace JOIN with `JOIN track_metadata` |
| `backend/database/search.go:168` | RebuildSearchIndex | INSERT INTO FTS from 5-table JOIN | Yes — `SELECT FROM track_metadata` |
| `backend/database/database.go:344` | migration2BasenameAndFTS | INSERT INTO FTS from 5-table JOIN | **No** — must keep inline (runs before migration 4) |
| `backend/library/library.go:798` | commitNewAudioFile | FTS5 INSERT VALUES | No — single-row parameterized insert, no JOIN |
| `backend/library/library.go:879` | updateAudioFileMetadata | FTS5 DELETE + INSERT | No — single-row operations, no JOIN |
| `backend/library/rescan.go:165` | clearAllLibraryData | FTS5 DELETE all | No — simple DELETE, no JOIN |
| `backend/queue/persistence.go:64` | lookupChunk | 3-table JOIN + fmt.Sprintf IN | Yes — migrate to sqlc with VIEW |
| `backend/queue/persistence.go:195` | insertTrackBatch | Multi-row INSERT with variable VALUES | No — stays hand-crafted (no JOINs) |

### sqlc Query Files (already managed by sqlc, may benefit from VIEW)
| File | Query Name | Pattern | VIEW Applicable? |
|------|-----------|---------|-----------------|
| `audio_files.sql:106` | SearchAudioFilesByBasename | 5-table JOIN (same subquery pattern) | Yes — could use VIEW |
| `audio_files.sql:75` | GetAllTracksWithFullMetadata | 6-table JOIN (INNER JOINs) | Partial — uses INNER JOINs (different semantics) |
| `playlists.sql:37` | GetPlaylistTracksWithMetadata | 6-table JOIN + cover_art | Partial — includes cover_art JOIN not in VIEW |
| `playlists.sql:63` | GetAllPlaylistTracksWithMetadata | 6-table JOIN + cover_art | Partial — includes cover_art JOIN not in VIEW |
| `genres.sql:26` | GetTracksByGenre | 7-table JOIN (genre-rooted) | Partial — rooted on genres, not audio_files |
| `queue.sql:15` | GetQueueTracks | 3-table JOIN | Partial — simpler pattern (no rgr subquery) |

### Scope Decision for sqlc Queries
The VIEW consolidation primarily targets the **hand-crafted Go SQL** where the duplication is most problematic (search.go has 3 copies of the identical pattern). For sqlc queries, converting to use the VIEW is optional and should be done case-by-case:
- `SearchAudioFilesByBasename` — good candidate (exact same pattern)
- Playlist/genre queries — involve additional JOINs (cover_art, genre tables) beyond what the VIEW provides, so the benefit is lower
- `GetAllTracksWithFullMetadata` — uses INNER JOINs intentionally, semantics differ from VIEW's LEFT JOINs

## FTS5 Statements Requiring SAFETY Comments

Complete inventory of hand-crafted FTS5 SQL statements:

| # | File | Line | Operation | Comment Needed |
|---|------|------|-----------|---------------|
| 1 | `search.go` | 34 | SearchFTS — `WHERE search_index MATCH ?` | Yes |
| 2 | `search.go` | 92 | SearchFTSByFilename — `WHERE search_index MATCH ?` | Yes |
| 3 | `search.go` | 133 | InsertSearchIndex — `INSERT INTO search_index` | Yes |
| 4 | `search.go` | 143 | DeleteSearchIndex — `DELETE FROM search_index WHERE rowid = ?` | Yes |
| 5 | `search.go` | 152 | ClearSearchIndex — `DELETE FROM search_index` | Yes |
| 6 | `search.go` | 168 | RebuildSearchIndex — `INSERT INTO search_index ... SELECT FROM` | Yes |
| 7 | `search.go` | 232 | SearchFTSTracks — `WHERE search_index MATCH ?` | Yes |
| 8 | `library.go` | 798 | commitNewAudioFile — `INSERT INTO search_index ... VALUES` | Yes (cross-ref search.go) |
| 9 | `library.go` | 879 | updateAudioFileMetadata — `DELETE FROM search_index` | Yes (cross-ref search.go) |
| 10 | `library.go` | 891 | updateAudioFileMetadata — `INSERT INTO search_index ... VALUES` | Yes (cross-ref search.go) |
| 11 | `rescan.go` | 165 | clearAllLibraryData — `DELETE FROM search_index` | Yes (cross-ref search.go) |
| 12 | `persistence.go` | 195 | insertTrackBatch — multi-row `INSERT INTO queue_tracks` | Yes (variable VALUES count) |

## Event Constant Inventory

### Go (backend/events/events.go) — 21 constants in 4 blocks
```
Playback: PlaybackStateChanged, PlaybackFinished, TrackChanged, SeekFailed, VolumeChanged
Queue:    QueueChanged, QueueIndexChanged, QueueModeChanged, QueueTracksModified
Config:   LibraryConfigChanged, ThemeConfigChanged, TrackListConfigChanged, FavoritesConfigChanged
Playlist: PlaylistCreated, PlaylistDeleted, PlaylistRenamed, PlaylistTracksChanged, PlaylistsRestored, DefaultPlaylistChanged
Library:  LibraryScanStarted, LibraryScanComplete
```

### TypeScript (frontend/src/events.ts) — 20 constants
Missing: `LibraryConfigChanged` (exists in Go, absent from TypeScript)

### Generator Output Format Target
```typescript
export const Events = {
    // Playback events (backend → frontend push)
    PlaybackStateChanged: "PlaybackStateChanged",
    // ... preserving comment groups and ordering
} as const;

export type EventName = (typeof Events)[keyof typeof Events];
```

## Open Questions

1. **Should sqlc queries (SearchAudioFilesByBasename, etc.) also be updated to use the VIEW?**
   - What we know: The VIEW consolidation is primarily targeting hand-crafted Go SQL in search.go. Sqlc queries are already managed and less prone to drift.
   - What's unclear: Whether updating sqlc queries provides enough benefit to justify the churn and testing.
   - Recommendation: Update `SearchAudioFilesByBasename` (exact same pattern). Leave playlist/genre queries as-is (they have additional JOINs the VIEW doesn't cover). This is Claude's discretion per CONTEXT.md.

2. **Where should the event generator Go file live?**
   - What we know: It needs to be a `main` package (standalone executable for `go:generate`). Options: `backend/events/cmd/gen-events-ts/main.go` or `cmd/gen-events-ts/main.go` or inline in `backend/events/`.
   - What's unclear: Project convention for codegen tools (none exist yet).
   - Recommendation: `backend/events/cmd/genevents/main.go` — keeps it close to the source of truth. The `//go:generate` directive on events.go runs it.

3. **codegen-check hook — is it actually fixed?**
   - What we know: `go generate ./...` now completes in <1 second in testing. Previous hanging was during Phase 2 (Feb 2026).
   - What's unclear: Whether the fix was a templ version update, environment change, or something else.
   - Recommendation: After wiring the event generator, test the full hook manually (`lefthook run pre-commit`) before declaring it fixed. If it still hangs, narrow the hook scope to only run event codegen check (not full `go generate ./...`).

## Sources

### Primary (HIGH confidence)
- sqlc v1.30.0 official docs — [select.html#mysql-and-sqlite](https://docs.sqlc.dev/en/stable/howto/select.html#mysql-and-sqlite) — `sqlc.slice()` syntax and generated code
- sqlc v1.30.0 official docs — [ddl.html](https://docs.sqlc.dev/en/stable/howto/ddl.html) — Schema handling including VIEWs
- Direct verification: `go tool sqlc generate` tested with VIEW + `sqlc.slice()` against project's sqlc v1.30.0 — both work correctly
- Go stdlib `go/ast`, `go/parser`, `go/token` documentation — standard library, stable API

### Secondary (MEDIUM confidence)
- Codebase analysis: 10+ duplicate JOIN instances identified by grep across .go and .sql files
- lefthook.yml examination: `codegen-check` hook structure and `go generate ./...` command
- `go generate ./...` timing test: completes in <1s (2 templ + 1 sqlc generators, all no-op)

### Tertiary (LOW confidence)
- None — all findings verified against primary sources or direct testing

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — sqlc v1.30.0 verified directly; go/ast is stable stdlib
- Architecture: HIGH — VIEW + sqlc.slice() both tested against project toolchain
- Pitfalls: HIGH — migration ordering verified by reading database.go; JOIN semantics verified by reading query files

**Research date:** 2026-03-04
**Valid until:** 2026-04-04 (stable tools, no fast-moving dependencies)
