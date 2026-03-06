---
phase: quick
plan: 002
subsystem: playlist-import
tags: [playlist, import, deduplication, sqlc]
dependency_graph:
  requires: []
  provides: [unique-playlist-names-on-import]
  affects: [playlist-import-flow]
tech_stack:
  added: []
  patterns: [count-query-for-uniqueness, sequential-rename-suffix]
key_files:
  created: []
  modified:
    - backend/database/sql/queries/playlists.sql
    - backend/database/sql/sqlcgen/playlists.sql.go
    - backend/playlist/playlist.go
decisions:
  - Placed CountPlaylistsByName query between playlist CRUD and track queries for logical grouping
  - uniquePlaylistName is a private method — only ImportPlaylist calls it, keeping CreatePlaylist/CreatePlaylistWithTracks unchanged
metrics:
  duration: 10 min
  completed: "2026-02-28"
---

# Quick Task 002: Auto-rename Duplicate Playlists on Import Summary

**One-liner:** CountPlaylistsByName query + uniquePlaylistName helper auto-appends (1), (2), etc. on M3U import when name collides

## What Was Done

### Task 1: Add CountPlaylistsByName SQL query and regenerate sqlc
- Added `CountPlaylistsByName :one` query to `playlists.sql` — counts playlists with exact name match
- Regenerated sqlc producing `CountPlaylistsByName(ctx, name) (int64, error)` in Go
- **Commit:** `04b2088`

### Task 2: Add uniquePlaylistName helper and wire into ImportPlaylist
- Added `uniquePlaylistName(name string) string` method to playlist Service
- Logic: if name exists, tries "Name (1)", "Name (2)", etc. until a free name is found
- Wired single call `playlistName = s.uniquePlaylistName(playlistName)` in ImportPlaylist, between name derivation and CreatePlaylist call
- CreatePlaylist and CreatePlaylistWithTracks remain unchanged — no auto-rename on manual creation
- **Commit:** `8ba8bbe`

## Verification Results

| Check | Result |
|-------|--------|
| `go build ./backend/...` | ✅ Pass |
| `go vet ./backend/...` | ✅ Pass |
| CountPlaylistsByName in generated Go | ✅ Present |
| uniquePlaylistName only in ImportPlaylist | ✅ 3 occurrences (comment, definition, one call site) |
| CreatePlaylist unchanged | ✅ No uniquePlaylistName reference |
| CreatePlaylistWithTracks unchanged | ✅ No uniquePlaylistName reference |

## Deviations from Plan

None — plan executed exactly as written.

## Commits

| # | Hash | Message |
|---|------|---------|
| 1 | `04b2088` | feat(quick-002): add CountPlaylistsByName SQL query and regenerate sqlc |
| 2 | `8ba8bbe` | feat(quick-002): add uniquePlaylistName helper and wire into ImportPlaylist |
