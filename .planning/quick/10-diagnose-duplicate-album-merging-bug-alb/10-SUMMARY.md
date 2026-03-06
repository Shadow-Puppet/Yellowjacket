---
phase: quick-10
plan: 10
subsystem: database, library
tags: [bugfix, schema-migration, sqlite, entity-cache]
dependency_graph:
  requires: []
  provides: [composite-unique-release-groups, migration-5]
  affects: [release_groups, library-scan, cover-grid]
tech_stack:
  added: []
  patterns: [composite-unique-constraint, table-rebuild-migration, composite-cache-key]
key_files:
  created: []
  modified:
    - backend/database/sql/schemas/release_groups.sql
    - backend/database/sql/queries/release_groups.sql
    - backend/database/sql/sqlcgen/release_groups.sql.go
    - backend/database/database.go
    - backend/library/library.go
    - backend/library/scan_test.go
decisions:
  - "Rename GetReleaseGroupByName to GetReleaseGroupByNameAndArtist (no callers outside generated code)"
  - "Use null byte separator in composite cache key for safety"
  - "Drop and recreate track_metadata VIEW during migration to avoid SQLite VIEW dependency error"
metrics:
  duration: 5m25s
  completed: "2026-03-05"
  tasks_completed: 2
  tasks_total: 2
---

# Quick Task 10: Fix Duplicate Album Merging Bug

Composite unique constraint on (name, album_artist_credit_id) for release_groups, with migration 5 to rebuild existing tables and entity cache fix to key by album+artist.

## Task Summary

| # | Task | Commit | Key Changes |
|---|------|--------|-------------|
| 1 | Fix schema, queries, and regenerate sqlc | 999ab96 | UNIQUE(name, album_artist_credit_id) in schema; ON CONFLICT updated; GetReleaseGroupByNameAndArtist |
| 2 | Add migration 5 and fix entity cache | d43ba7b | Migration 5 rebuilds table; cache keys by album+artistID; VIEW drop/recreate |

## What Changed

### Schema (`release_groups.sql`)
- Removed `UNIQUE` from `name TEXT NOT NULL UNIQUE`
- Added table-level `UNIQUE(name, album_artist_credit_id)` — SQLite treats NULL as unique, so albums without an artist won't collide

### Queries (`release_groups.sql`)
- `UpsertReleaseGroup`: `ON CONFLICT(name)` → `ON CONFLICT(name, album_artist_credit_id)`
- `GetReleaseGroupByName` → `GetReleaseGroupByNameAndArtist` with two params (name + album_artist_credit_id)

### Migration 5 (`database.go`)
- Disables FK checks temporarily
- Drops `track_metadata` VIEW (depends on release_groups)
- Creates `release_groups_new` with composite unique constraint
- Copies data, drops old, renames new
- Recreates both indexes and the `track_metadata` VIEW
- Re-enables FK checks, bumps user_version to 5

### Entity Cache (`library.go`)
- Cache key changed from `tags.Album` to `fmt.Sprintf("%s\x00%d", tags.Album, artistID)` where artistID is -1 when no album artist credit exists
- All 3 cache access points updated (lookup, cover art update, store after upsert)

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] track_metadata VIEW blocking table rename**
- **Found during:** Task 2
- **Issue:** SQLite refuses to rename `release_groups_new` to `release_groups` when the `track_metadata` VIEW references the old table
- **Fix:** Drop the VIEW before table rebuild, recreate it after rename (VIEW definition matches embedded schema exactly)
- **Files modified:** backend/database/database.go
- **Commit:** d43ba7b

**2. [Rule 1 - Bug] Tests using old cache key format**
- **Found during:** Task 2
- **Issue:** `TestResolveReleaseGroup` and `TestResolveReleaseGroup_CacheHit` use bare album name as cache key
- **Fix:** Updated test assertions to use composite cache key format (`albumName\x00artistCreditID`)
- **Files modified:** backend/library/scan_test.go
- **Commit:** d43ba7b

## Verification

- `go build ./...` — passes
- `go test ./...` — all 14 test packages pass
- `go vet ./...` — no issues
- Schema has `UNIQUE(name, album_artist_credit_id)` ✓
- UpsertReleaseGroup uses `ON CONFLICT(name, album_artist_credit_id)` ✓
- Migration 5 exists and rebuilds table ✓
- Entity cache key includes artist credit ID ✓

## Notes

- **Existing databases**: Migration 5 changes the constraint but does NOT split already-merged albums. Users must trigger a full library rescan after upgrading.
- **NULL handling**: SQLite's UNIQUE treats each NULL as distinct, so albums without `album_artist_credit_id` will each get their own row — this is desired behavior.
