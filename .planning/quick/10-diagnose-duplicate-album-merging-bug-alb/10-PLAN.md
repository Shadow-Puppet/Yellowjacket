---
phase: quick-10
plan: 10
type: execute
wave: 1
depends_on: []
files_modified:
  - backend/database/sql/schemas/release_groups.sql
  - backend/database/sql/queries/release_groups.sql
  - backend/database/sql/sqlcgen/release_groups.sql.go
  - backend/database/database.go
  - backend/library/library.go
autonomous: true
requirements: []

must_haves:
  truths:
    - "Two albums with the same name but different artists are stored as separate release_groups rows"
    - "Scanning a library with two 'Classics' albums (Aphex Twin + Ratatat) produces two distinct entries"
    - "The cover grid shows both albums as separate entries with correct artist names"
    - "Opening each album shows only its own tracks, not tracks from the other"
  artifacts:
    - path: "backend/database/sql/schemas/release_groups.sql"
      provides: "UNIQUE constraint on (name, album_artist_credit_id) instead of name alone"
    - path: "backend/database/sql/queries/release_groups.sql"
      provides: "UpsertReleaseGroup with ON CONFLICT(name, album_artist_credit_id)"
    - path: "backend/database/database.go"
      provides: "Migration 5 to rebuild release_groups table with new unique constraint"
    - path: "backend/library/library.go"
      provides: "Entity cache keyed by album name + artist credit ID"
  key_links:
    - from: "backend/library/library.go"
      to: "backend/database/sql/sqlcgen/release_groups.sql.go"
      via: "UpsertReleaseGroup call in resolveReleaseGroup"
      pattern: "UpsertReleaseGroup"
    - from: "backend/database/database.go"
      to: "backend/database/sql/schemas/release_groups.sql"
      via: "Migration 5 rebuilds release_groups with new constraint"
      pattern: "migration.*5"
---

<objective>
Fix the album merging bug where albums with the same name but different artists are incorrectly stored as a single entry. Root cause: the `release_groups` table has `UNIQUE(name)` instead of `UNIQUE(name, album_artist_credit_id)`, causing `ON CONFLICT` to merge distinct albums.

Purpose: Two users' "Classics" albums (Aphex Twin and Ratatat) should appear as separate entries in the cover grid, each with correct cover art, artist name, and track listing.

Output: Schema migration, updated SQL queries, regenerated sqlc code, and fixed entity cache.
</objective>

<execution_context>
@/home/caleb/.config/opencode/get-shit-done/workflows/execute-plan.md
@/home/caleb/.config/opencode/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/STATE.md
@backend/database/sql/schemas/release_groups.sql
@backend/database/sql/queries/release_groups.sql
@backend/database/database.go
@backend/library/library.go
</context>

<tasks>

<task type="auto">
  <name>Task 1: Fix schema, queries, and regenerate sqlc</name>
  <files>
    backend/database/sql/schemas/release_groups.sql
    backend/database/sql/queries/release_groups.sql
    backend/database/sql/sqlcgen/release_groups.sql.go
  </files>
  <action>
1. **Update `release_groups.sql` schema** (line 3): Remove `UNIQUE` from the `name` column definition. Add a composite unique constraint at the table level:
   ```sql
   name TEXT NOT NULL,
   ```
   And after the FOREIGN KEY lines, before the closing `);`:
   ```sql
   UNIQUE(name, album_artist_credit_id)
   ```

   **IMPORTANT**: SQLite treats each NULL as unique in UNIQUE constraints, so albums without an album_artist_credit_id will each get their own row. This is the desired behavior — an album with no tagged artist should not conflict with named-artist albums.

2. **Update `release_groups.sql` queries**:
   - `UpsertReleaseGroup` (line 22): Change `ON CONFLICT(name)` to `ON CONFLICT(name, album_artist_credit_id)`. This ensures upsert only matches when BOTH album name and artist match.
   - `GetReleaseGroupByName` (lines 15-17): Add an `album_artist_credit_id` parameter. Rename to `GetReleaseGroupByNameAndArtist`:
     ```sql
     -- name: GetReleaseGroupByNameAndArtist :one
     SELECT * FROM release_groups
     WHERE name = ? AND album_artist_credit_id = ? LIMIT 1;
     ```
     **Check first**: grep codebase for any callers of `GetReleaseGroupByName`. If there are callers, update them to pass the artist credit ID. If no callers exist outside generated code, safe to rename.

3. **Regenerate sqlc**: Run `sqlc generate` from `backend/database/` directory:
   ```bash
   cd backend/database && sqlc generate
   ```
   Verify the generated `release_groups.sql.go` has the updated function signatures (UpsertReleaseGroup params unchanged since it already takes album_artist_credit_id; GetReleaseGroupByNameAndArtist now takes two params).

**SAFETY NOTE (hand-crafted SQL follows in Task 2)**: The schema file change only affects NEW databases. Existing databases need the migration in Task 2.
  </action>
  <verify>
    - `sqlc generate` completes without errors from `backend/database/`
    - `go build ./...` passes from project root
    - Schema file has `UNIQUE(name, album_artist_credit_id)` instead of `name TEXT NOT NULL UNIQUE`
    - UpsertReleaseGroup query uses `ON CONFLICT(name, album_artist_credit_id)`
  </verify>
  <done>Schema and queries updated for composite uniqueness, sqlc regenerated, project compiles.</done>
</task>

<task type="auto">
  <name>Task 2: Add migration 5 and fix entity cache</name>
  <files>
    backend/database/database.go
    backend/library/library.go
  </files>
  <action>
1. **Add migration 5 in `database.go`** after the migration 4 block (after line 286). Follow the existing migration pattern (check `version < 5`, bump to `PRAGMA user_version = 5`).

   Migration 5 must:
   - **SAFETY**: This is hand-crafted SQL for a schema migration. SQLite cannot ALTER a UNIQUE constraint, so we must rebuild the table.
   - Create `release_groups_new` with the corrected schema (matching the updated `release_groups.sql` exactly — same columns, same foreign keys, but `UNIQUE(name, album_artist_credit_id)` instead of `UNIQUE(name)`).
   - Copy all data: `INSERT INTO release_groups_new SELECT * FROM release_groups`
   - Drop old table: `DROP TABLE release_groups`
   - Rename: `ALTER TABLE release_groups_new RENAME TO release_groups`
   - Recreate both indexes:
     ```sql
     CREATE INDEX IF NOT EXISTS idx_release_groups_cover_art_id ON release_groups(cover_art_id);
     CREATE INDEX IF NOT EXISTS idx_release_groups_album_artist_credit_id ON release_groups(album_artist_credit_id);
     ```
   - Set `PRAGMA user_version = 5`
   - Log: `"applying migration 5: release_groups composite unique constraint"`
   - Log completion: `"migration 5 complete"`

   **NOTE**: The migration does NOT split already-merged albums. That requires a full library rescan which the user triggers manually. The migration just removes the bad constraint so future scans work correctly.

   **NOTE**: The `release_group_recordings` table has a foreign key `REFERENCES release_groups(id)`. Since we're dropping and recreating, we need to handle this. SQLite defers FK checks by default when foreign_keys is ON. Wrap the migration in:
   ```go
   // Temporarily disable FK checks for table rebuild.
   db.ExecContext(ctx, "PRAGMA foreign_keys = OFF")
   // ... migration steps ...
   db.ExecContext(ctx, "PRAGMA foreign_keys = ON")
   ```

2. **Fix entity cache in `library.go`**:
   - Line 44: Change cache type from `map[string]sqlcgen.ReleaseGroup` to `map[string]sqlcgen.ReleaseGroup` (type stays same, but key semantics change).
   - In `resolveReleaseGroup()` (lines 1253-1327): Change all cache key accesses from `tags.Album` to a composite key. Create a helper or inline:
     ```go
     // Build composite cache key: "albumName\x00artistCreditID" (or "albumName\x00-1" if no artist).
     artistID := int64(-1)
     if albumArtistCreditID.Valid {
         artistID = albumArtistCreditID.Int64
     }
     cacheKey := fmt.Sprintf("%s\x00%d", tags.Album, artistID)
     ```
   - Replace all 3 occurrences of `cache.releaseGroups[tags.Album]` with `cache.releaseGroups[cacheKey]`:
     - Line 1265: cache lookup
     - Line 1283: cache update after cover art
     - Line 1324: cache store after upsert
  </action>
  <verify>
    - `go build ./...` passes
    - `go test ./backend/database/...` passes (existing migration tests should still work since migration 5 is additive)
    - `go test ./backend/library/...` passes
    - `go vet ./...` passes
  </verify>
  <done>Migration 5 rebuilds release_groups with composite unique constraint. Entity cache uses composite key (album name + artist credit ID). Existing databases upgraded on next app start. User triggers full rescan to split previously merged albums.</done>
</task>

</tasks>

<verification>
- `go build ./...` — project compiles
- `go test ./...` — all tests pass
- `go vet ./...` — no issues
- Schema file reflects `UNIQUE(name, album_artist_credit_id)`
- UpsertReleaseGroup uses `ON CONFLICT(name, album_artist_credit_id)`
- Migration 5 exists and rebuilds the release_groups table
- Entity cache key includes artist credit ID
</verification>

<success_criteria>
- Two albums named "Classics" by different artists stored as separate release_groups rows after rescan
- Each album shows only its own tracks when opened
- Cover grid displays both albums as distinct entries
- Existing databases migrated safely (constraint changed, rescan needed to split merged data)
</success_criteria>

<output>
After completion, create `.planning/quick/10-diagnose-duplicate-album-merging-bug-alb/10-SUMMARY.md`
</output>
