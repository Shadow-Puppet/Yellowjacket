# Architecture Research: Multi-Library Integration

**Researched:** 2026-03-08
**Confidence:** HIGH

## Design Decision: Hybrid Model

- **`library_id` on `audio_files` only** — physical file binding
- **Artists, albums, recordings, genres stay global** — shared reference data
- **Unified presentation by default** — optional library filter
- **Cross-library playlists** — playlists reference audio_file_id
- **Phantom tracks on removal** — playlist entries preserved with metadata

## Database Changes

### New Table: libraries

```sql
CREATE TABLE IF NOT EXISTS libraries (
    id              INTEGER PRIMARY KEY,
    name            TEXT NOT NULL,
    path            TEXT NOT NULL UNIQUE,
    scan_concurrency TEXT NOT NULL DEFAULT 'auto',
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_scanned_at DATETIME
);
```

### Migration 6: Multi-Library Support

Order of operations:
1. Create `libraries` table
2. Read TOML `DirectoryPath`, insert as default library
3. `ALTER TABLE audio_files ADD COLUMN library_id INTEGER NOT NULL DEFAULT {defaultLibID}`
4. Create index on `audio_files(library_id)`
5. Rebuild `playlist_tracks` with SET NULL FK + phantom columns
6. Drop and recreate `track_metadata` VIEW with `library_id`
7. Set `PRAGMA user_version = 6`

### playlist_tracks Rebuild (for phantom support)

```sql
CREATE TABLE playlist_tracks_new (
    id            INTEGER PRIMARY KEY,
    playlist_id   INTEGER NOT NULL,
    audio_file_id INTEGER,                     -- NOW NULLABLE
    position      INTEGER NOT NULL,
    phantom_file_path TEXT,
    phantom_title     TEXT,
    phantom_artist    TEXT,
    phantom_album     TEXT,
    FOREIGN KEY(playlist_id) REFERENCES playlists(id) ON DELETE CASCADE,
    FOREIGN KEY(audio_file_id) REFERENCES audio_files(id) ON DELETE SET NULL
);
```

Two-phase library removal:
1. Populate phantom metadata BEFORE deleting audio_files
2. Delete audio_files -> SET NULL triggers -> phantom columns preserve display info

### track_metadata VIEW (updated)

Add `af.library_id` to SELECT list. Same JOIN structure. Consumers get library_id for filtering.

## Scan Pipeline Changes

- `Scan()` -> `ScanLibrary(libraryID int64)` — accepts library ID, loads path from DB
- `ScanAllLibraries()` — sequential iteration, one at a time
- Orphan cleanup scoped to library being scanned
- Entity cache (artists, albums) remains per-scan and works correctly (shared entities)
- Progress events include library_id and library_name

## Orphan Cleanup (Library Removal)

Reference-counting bottom-up deletes in single transaction:
```
audio_files (library_id = X) -> DELETE
recordings (no remaining audio_files) -> DELETE
release_group_recordings (orphaned) -> DELETE
recording_genres (orphaned) -> DELETE
release_groups (no remaining recordings) -> DELETE
artist_credit (no remaining references) -> DELETE
artist_credit_artist (orphaned) -> DELETE
artists (no remaining credits) -> DELETE
genres (no remaining recording links) -> DELETE
cover_art (no remaining release_groups) -> DELETE
```

## FTS5 Search Index

Contentless FTS5 (`content=''`) works naturally:
- Search queries JOIN `search_index` on `track_metadata` (which now has `library_id`)
- Library-filtered search: add `AND tm.library_id = ?` to WHERE clause
- Stale entries after library removal filtered out by JOIN (same as current orphan behavior)
- Consider migrating to `contentless_delete=1` (SQLite 3.43.0+) for per-row DELETE support

## Frontend Architecture

- `libraryStore` gains: library list, active filter (null = all), persistence in localStorage
- Backend filtering (not frontend) — pass libraryID to backend queries
- `library-manager` component redesigned: library list view, add/remove/rename, per-library scan
- All browse views check active filter when fetching data
- New events: LibraryAdded, LibraryRemoved, LibraryRenamed
- Existing scan events gain library_id in payload

## Config Migration

- TOML `[Library].DirectoryPath` read once during migration 6, inserted as default library
- Post-migration: library management through DB only
- `ScanConcurrency` moves per-library (DB column) with global default fallback
- `SetLibraryDirectory()` and `GetLibraryDirectory()` deprecated

## Build Order

1. Schema & Migration (foundation)
2. Backend scan pipeline (per-library scanning)
3. Backend API (CRUD, filtered queries, events)
4. Frontend (library manager, filter, store updates)
