# Pitfalls Research: Multi-Library Support

**Researched:** 2026-03-08
**Confidence:** HIGH

## Critical Pitfalls

### P1: SQLite ALTER TABLE ADD COLUMN with NOT NULL Requires DEFAULT

SQLite requires NOT NULL columns added via ALTER TABLE to have a default. Create `libraries` table and insert default library BEFORE adding `library_id` to `audio_files`. Use `DEFAULT {id}` where id is the auto-created library's ID.

**Phase:** Schema & Migration

### P2: Table Rebuild for CASCADE to SET NULL Must Audit ALL Tables

Both `playlist_tracks` AND `queue_tracks` have `ON DELETE CASCADE` on `audio_file_id`. Decision: playlist_tracks -> SET NULL (phantom support), queue_tracks -> keep CASCADE (queue is ephemeral). Must explicitly document this choice.

**Phase:** Schema & Migration

### P3: FTS5 Contentless Table Cannot Delete Individual Rows

After removing a library with 10K tracks, 10K stale FTS5 entries remain. Current JOIN filtering handles this, but FTS5 scoring is affected. Consider migrating to `contentless_delete=1` (SQLite 3.43.0+). Alternative: full rebuild after library removal.

**Phase:** Schema & Migration

### P4: Orphan Cleanup Through Entity Graph Is Complex

Reference-counting deletes must handle shared entities. Two libraries with same artist — removing one must not delete the artist if the other still references it. Use `NOT IN (SELECT ...)` or `LEFT JOIN ... WHERE ... IS NULL` pattern. Single transaction required.

**Phase:** Backend API / Library CRUD

### P5: Existing User Migration Must Be Seamless

First launch after update: migration 6 reads TOML DirectoryPath, creates library row, backfills audio_files.library_id. Test on real user database snapshot, not just fresh DB.

**Phase:** Schema & Migration

## Moderate Pitfalls

### P6: Frontend Memory Pressure with Multiple Large Libraries

`libraryStore.eagerFetch()` loads ALL data. 150K tracks x ~500 bytes = 75MB. Use backend filtering (pass library_id to queries). When "All Libraries" is selected, this is unavoidable for now — pagination is a future optimization.

**Phase:** Frontend

### P7: Scan Coordination — No Concurrent Scans

Single `scanActive` bool, single entity cache, single writer SQLite. Enforce one-scan-at-a-time globally with scan coordinator. Track which library is scanning for UI display.

**Phase:** Backend Scan Pipeline

### P8: Phantom Track Resolution with Multiple Library Roots

`LibraryDirProvider` returns single string. M3U8 path resolution checks one root. With multi-library, try all library roots for phantom resolution. Store phantom_file_path as absolute path to avoid ambiguity.

**Phase:** Frontend / Playlist Integration

### P9: Queue and Now-Playing During Library Removal

If currently playing track belongs to removed library: stop playback, advance to next non-removed track. Check queue and player state before proceeding with removal.

**Phase:** Backend API / Library CRUD

### P10: Cross-Library Entity Deduplication

Same artist in two libraries -> one `artists` row (UNIQUE constraint handles this). Removing one library's audio_files must NOT delete shared artist. Reference-counting cleanup handles this correctly.

**Phase:** Backend API / Library CRUD

## Minor Pitfalls

### P11: Config Migration — TOML to DB Split Creates Two Sources of Truth

Move ALL library-related config to DB. TOML only for app-level settings (theme, shortcuts, window). TOML `[Library]` section is migration source only.

**Phase:** Schema & Migration

### P12: Library Filter State Interacting with Everything

Single filter state in libraryStore. All data-fetching functions accept the filter. Trigger invalidate+refetch on filter change.

**Phase:** Frontend

### P13: Scan-While-Remove Race Condition

Before removing a library, cancel any active scan on it and wait for completion. Serialize scan and remove operations.

**Phase:** Backend API

### P14: Cover Art Files Not Library-Scoped

Cover art stored by content hash (shared). Removing a library: only delete cover_art DB rows that are truly orphaned (no remaining release_groups reference them). Then delete corresponding files.

**Phase:** Backend API

### P15: Testing Gaps

Create "two-library fixture" test helper. Test: add two libraries with overlapping artists -> remove one -> verify other is intact. Test migration on pre-multi-library DB snapshot.

**Phase:** All phases (accompanying tests)
