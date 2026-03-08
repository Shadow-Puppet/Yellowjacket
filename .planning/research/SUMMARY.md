# Research Summary: Multi-Library Support

**Synthesized:** 2026-03-08
**Sources:** STACK.md, FEATURES.md, ARCHITECTURE.md, PITFALLS.md

## Key Findings

### Stack
- **Zero new packages needed** — existing SQLite, sqlc, Wails, Lit stack handles everything
- Migration 6 follows existing PRAGMA user_version pattern (5 precedents)
- sqlc queries: create ByLibrary variants for ~5 key queries

### Features
- **Desktop player pattern:** Merged view by default (foobar2000, Roon), optional filter (Navidrome)
- **Table stakes:** Multiple folders, unified view, per-folder scan, graceful removal, offline handling
- **Anti-features:** Separate databases, user access control, auto-dedup
- Cross-library playlists are expected by users (Navidrome, foobar2000)

### Architecture
- **Hybrid model:** `library_id` on `audio_files` only; artists/albums/genres stay global
- **Migration 6:** Create libraries table -> add library_id column -> rebuild playlist_tracks for SET NULL + phantom columns -> recreate track_metadata VIEW
- **Scan pipeline:** `ScanLibrary(id)` replaces `Scan()`, sequential coordination
- **Orphan cleanup:** Reference-counting bottom-up deletes for shared entities
- **FTS5:** Contentless table works via JOIN filtering; consider contentless_delete migration

### Critical Pitfalls
1. ALTER TABLE ADD COLUMN requires DEFAULT for NOT NULL — create libraries first
2. Table rebuild must audit ALL CASCADE FKs (playlist_tracks AND queue_tracks)
3. FTS5 contentless can't DELETE rows — stale entries accumulate after library removal
4. Orphan cleanup must not delete shared entities across libraries
5. Existing user migration must be seamless (TOML to DB)
6. Scan coordination must serialize (single-writer SQLite)

## Architecture Decision Record

| Decision | Rationale |
|----------|-----------|
| library_id on audio_files only | Physical files belong to libraries; logical entities (artists, albums) are global |
| Libraries in DB, not TOML | CRUD through UI shouldn't require TOML manipulation; DB is already source of truth |
| SET NULL for playlist_tracks FK | Phantom tracks preserve playlist structure when library removed |
| CASCADE for queue_tracks FK | Queue is ephemeral, not user-curated like playlists |
| Sequential scanning | SQLite single-writer makes parallel scans pointless |
| Backend filtering, not frontend | Don't load 150K tracks when viewing one library |

## Build Order

1. **Schema & Migration** — Foundation everything else depends on
2. **Backend Scan Pipeline** — Per-library scanning before exposing in UI
3. **Backend API** — CRUD, filtered queries, events, orphan cleanup
4. **Frontend** — Library manager, filter, store updates, phantom display
