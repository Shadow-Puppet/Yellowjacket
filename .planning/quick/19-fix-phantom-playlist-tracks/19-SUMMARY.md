---
phase: quick-19
plan: 19
subsystem: playlist
tags: [bugfix, multi-library, path-resolution]
dependency_graph:
  requires: [Phase 10 schema, Phase 11 scan pipeline]
  provides: [multi-root M3U8 path resolution]
  affects: [playlist merge, playlist restore, phantom resolution]
tech_stack:
  added: []
  patterns: [resolveM3UPath multi-root lookup, toRelativePathMultiRoot]
key_files:
  created: []
  modified:
    - backend/playlist/m3u.go
    - backend/playlist/playlist.go
    - backend/playlist/m3u_test.go
    - backend/library/scan_queue.go
decisions:
  - resolveM3UPath with knownPaths set for O(1) lookup instead of O(n) per-root DB queries
  - Import and restore use per-root DB queries since no pre-built path set available
  - toRelativePathMultiRoot picks first root that contains the path
metrics:
  duration: 7 min
  completed: "2026-03-16"
  tasks: 2
  files: 4
---

# Quick Task 19: Fix Phantom Playlist Tracks Summary

Multi-root M3U8 path resolution replacing single-library `getLibraryRoot()` with `getAllLibraryRoots()` across all playlist path operations.

## What Changed

### Task 1: Multi-root path resolution in m3u.go and playlist.go (9144ded)

**m3u.go — New functions:**
- `resolveM3UPath(relativePath, libraryRoots, knownPaths)` — resolves a relative M3U path against multiple roots, returning the first absolute path found in the knownPaths set. Falls back to first root for phantom tracks.
- `toRelativePathMultiRoot(absolutePath, libraryRoots)` — converts an absolute path to relative using the first root that contains it.

**m3u.go — Updated signatures:**
- `removeM3UEntries` — `libraryRoot string` → `libraryRoots []string`
- `replaceM3UEntryPaths` — `libraryRoot string` → `libraryRoots []string`
- `findM3UEntry` — `libraryRoot string` → `libraryRoots []string`

**playlist.go — Core change:**
- Replaced `getLibraryRoot() string` with `getAllLibraryRoots() []string` — queries all libraries from DB (or falls back to legacy config)

**playlist.go — 8 call sites updated:**
1. `mergeTracksForPlaylist` — builds knownPaths set from dbTracks, uses resolveM3UPath
2. `ImportPlaylist` — tries each root via GetAudioFileByPath until match found
3. `RestoreAllPlaylists` — passes libraryRoots to restoreSinglePlaylist
4. `restoreSinglePlaylist` — tries each root for each M3U8 entry
5. `buildM3UEntries` — uses toRelativePathMultiRoot for correct relative paths per track
6. `saveImportedPlaylistFile` — uses toRelativePathMultiRoot
7. `FindPhantomMatches` — uses resolveM3UPath for entry lookup
8. `GetPhantomCandidates` — passes libraryRoots to findM3UEntry
9. `ResolvePhantomTracks` — uses toRelativePathMultiRoot and multi-root replaceM3UEntryPaths
10. `RemovePhantomTracks` — passes libraryRoots to removeM3UEntries

### Task 2: Multi-root path resolution tests (a902850)

Added 5 new test functions with 20 test cases:
- `TestResolveM3UPath` — 7 cases covering abs paths, multi-root resolution, fallback, nil/empty
- `TestToRelativePathMultiRoot` — 5 cases covering first root, second root, no match, empty
- `TestRemoveM3UEntriesMultiRoot` — entries from different roots correctly removed
- `TestFindM3UEntryMultiRoot` — entry under second root found correctly
- `TestReplaceM3UEntryPathsMultiRoot` — replacement under second root applied

Updated 5 existing tests for new `[]string` parameter types.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Pre-existing golines formatting in scan_queue.go**
- **Found during:** Task 1 commit
- **Issue:** `golangci-lint` pre-commit hook checks all Go files, not just staged; `scan_queue.go` had a line too long for golines
- **Fix:** Split the SQL string literal across multiple lines
- **Files modified:** backend/library/scan_queue.go
- **Commit:** 9144ded (included in Task 1)

**2. [Rule 3 - Blocking] Existing tests use old single-root signatures**
- **Found during:** Task 1
- **Issue:** Changing function signatures broke existing test compilation, blocking the commit
- **Fix:** Updated 5 existing test call sites from `string` to `[]string{...}` (merged into Task 1 instead of waiting for Task 2)
- **Files modified:** backend/playlist/m3u_test.go
- **Commit:** 9144ded

## Verification Results

- `go build ./backend/...` — PASS
- `go vet ./backend/playlist/...` — PASS
- `go test ./backend/playlist/...` — PASS (38 tests)
- `go test ./backend/...` — PASS (all packages, no regressions)
- `golangci-lint` — PASS (0 issues)

## Self-Check: PASSED
