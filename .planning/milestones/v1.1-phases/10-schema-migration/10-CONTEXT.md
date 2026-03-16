# Phase 10: Schema & Migration - Context

**Gathered:** 2026-03-09
**Status:** Ready for planning

<domain>
## Phase Boundary

The database supports multiple libraries and phantom tracks — existing users upgrade seamlessly. Delivers: `libraries` table, `audio_files.library_id` FK, `playlist_tracks` phantom metadata columns, config migration from TOML to SQLite, and atomic migration guarantees. No UI, no CRUD API, no scan pipeline changes — just schema and migration.

Requirements: DATA-01, DATA-04, LIB-04, LIB-05, LSCAN-05

</domain>

<decisions>
## Implementation Decisions

### Migration experience
- Silent auto-migrate on startup — no user interaction, no progress indicator, no confirmation dialog
- Migration runs automatically when the app detects the schema version is behind
- On migration failure: show error dialog and refuse to start — no degraded/read-only mode
- Automatic database backup before migration runs (copy .db file before any schema changes)
- Schema version tracked via integer (SQLite `user_version` pragma or schema_version table) — app checks on startup, runs pending migrations sequentially

### Default library identity
- Migrated library name derived from the directory name (e.g., `/home/user/Music` becomes "Music")
- `music_directory` key removed from TOML config after successful migration — libraries table is the sole source of truth
- Old config key ignored if still present (no crash on stale config)
- Fresh installs start with an empty libraries table — no default library auto-created, user adds their first library when they want to scan
- Libraries table is minimal: name, path, created_at — no scan metadata columns yet (Phase 11 can add those)

### Phantom track schema
- Rich cached metadata on `playlist_tracks`: title, artist, album, duration, genre, cover art path
- Eager population: metadata columns filled on every playlist_tracks insert (not lazily on library removal)
- Phantom tracks identified by NULL `audio_file_id` — no separate `is_phantom` boolean column needed
- Migration adds new columns via ALTER TABLE ADD COLUMN (not table rebuild) — existing playlist_tracks rows get NULL metadata columns, backfilled from audio_files data

### Migration rollback strategy
- One-way migration — downgrade to pre-multi-library versions is unsupported
- Pre-migration backup is the user's safety net for rollback
- Backup file naming is timestamp-based (e.g., `yellowjacket.db.bak.20260309`) — multiple backups can coexist
- No automatic backup cleanup — user manages old backup files
- Migration events (start, success, backup path, errors) logged at INFO level to standard app log

### Claude's Discretion
- Exact column types and constraints for the libraries table
- Index strategy for library_id FK on audio_files
- Whether to use SQLite `user_version` pragma vs a dedicated schema_version table
- Migration transaction boundaries (single transaction vs per-step)
- Backfill query strategy for populating phantom metadata on existing playlist_tracks rows

</decisions>

<specifics>
## Specific Ideas

No specific requirements — open to standard approaches

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope

</deferred>

---

*Phase: 10-schema-migration*
*Context gathered: 2026-03-09*
