# Stack Research: Multi-Library Support

**Researched:** 2026-03-08
**Confidence:** HIGH

## Summary

Zero new Go packages needed. The existing stack handles everything required for multi-library support.

## Existing Stack (No Changes)

| Component | Package | Version | Role in Multi-Library |
|-----------|---------|---------|----------------------|
| Database | modernc.org/sqlite | v1.46.1 | ALTER TABLE, new tables, migration 6 |
| Query Gen | sqlc | v1.30.0 | New query files for libraries CRUD + filtered queries |
| Config | BurntSushi/toml | v1.6.0 | Migration source (DirectoryPath to DB) |
| Desktop | Wails | v2.10.2 | Binding patterns for library CRUD |
| Frontend | Lit | 3.2.1 | Reactive controllers for library state |
| Audio | gopxl/beep | v2 | No changes needed |

## Do NOT Add

- No ORM or query builder (fights existing sqlc architecture)
- No migration framework (goose, golang-migrate) — PRAGMA user_version works well with 5 existing migrations
- No UUID package — INTEGER PRIMARY KEY is the pattern

## SQLite Migration Patterns

### Adding library_id to audio_files

```sql
ALTER TABLE audio_files ADD COLUMN library_id INTEGER NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_audio_files_library_id ON audio_files(library_id);
```

SQLite limitation: `ALTER TABLE ADD COLUMN` cannot add FK constraints. Enforce at application level.

### track_metadata VIEW

Must DROP VIEW + CREATE VIEW (SQLite doesn't support ALTER VIEW). Add `af.library_id` to SELECT list. Follows migration 5 pattern exactly.

### playlist_tracks Table Rebuild

Change `ON DELETE CASCADE` to `ON DELETE SET NULL` on `audio_file_id`. Requires full table rebuild (migration 5 pattern: PRAGMA foreign_keys OFF -> create _new -> copy -> drop -> rename -> PRAGMA foreign_keys ON).

## sqlc Query Patterns

- Create `sql/queries/libraries.sql` for CRUD
- Create `ByLibrary` variants of key queries (GetAllTracksWithFullMetadataByLibrary, GetAllAlbumsWithDetailsByLibrary, etc.)
- Separate queries preferred over dynamic WHERE (cleaner types, better query plans)
- Hand-crafted FTS5 queries get `AND tm.library_id = ?` filter

## Frontend Patterns

- `LibraryStore` gains `selectedLibraryId` state (null = all libraries)
- Backend filtering (not frontend) — don't load 150K tracks when viewing one library
- Persist selection in localStorage
- `invalidate()` on library switch triggers refetch

## Config Migration

Libraries stored in SQLite (not TOML). Config `[Library].DirectoryPath` read once during migration, then deprecated. All library management through DB-backed methods.
