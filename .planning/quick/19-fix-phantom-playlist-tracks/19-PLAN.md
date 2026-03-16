---
phase: quick-19
plan: 19
type: execute
wave: 1
depends_on: []
files_modified:
  - backend/playlist/playlist.go
  - backend/playlist/m3u.go
  - backend/playlist/m3u_test.go
autonomous: true
requirements: []

must_haves:
  truths:
    - "Playlist tracks resolve correctly when multiple libraries exist"
    - "Playlist tracks resolve correctly after FullRescan with multiple libraries"
    - "M3U8 relative paths are resolved against all library roots, not just the first"
    - "M3U8 relative paths are saved relative to the correct library root for each track"
  artifacts:
    - path: "backend/playlist/playlist.go"
      provides: "getAllLibraryRoots() replacing getLibraryRoot(), multi-root resolution at all call sites"
    - path: "backend/playlist/m3u.go"
      provides: "toAbsolutePathMultiRoot() and updated helper functions"
    - path: "backend/playlist/m3u_test.go"
      provides: "Tests for multi-root path resolution"
  key_links:
    - from: "backend/playlist/playlist.go mergeTracksForPlaylist()"
      to: "backend/playlist/m3u.go toAbsolutePathMultiRoot()"
      via: "function call for each M3U8 entry"
      pattern: "toAbsolutePathMultiRoot"
---

<objective>
Fix phantom playlist tracks caused by single-library-root path resolution in a multi-library environment.

Purpose: The playlist M3U8 merge logic uses `getLibraryRoot()` which returns only the first library's path. When tracks belong to other libraries, their relative M3U8 paths resolve against the wrong root, causing them to appear as phantoms. After `FullRescan`, `RestoreAllPlaylists` also only uses one root, so tracks from non-first libraries fail to re-link.

Output: Multi-root path resolution across all playlist path operations — tracks from any library resolve correctly.
</objective>

<execution_context>
@/home/caleb/.config/opencode/get-shit-done/workflows/execute-plan.md
@/home/caleb/.config/opencode/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/STATE.md
@backend/playlist/playlist.go
@backend/playlist/m3u.go
@backend/playlist/m3u_test.go
</context>

<interfaces>
<!-- Key types and contracts the executor needs. -->

From backend/playlist/m3u.go:
```go
func toAbsolutePath(relativePath, libraryRoot string) string
func toRelativePath(absolutePath, libraryRoot string) string
func removeM3UEntries(entries []m3uEntry, targetAbsPaths map[string]struct{}, libraryRoot string) []m3uEntry
func replaceM3UEntryPaths(entries []m3uEntry, replacements map[string]string, libraryRoot string) []m3uEntry
func findM3UEntry(entries []m3uEntry, targetAbsPath string, libraryRoot string) (m3uEntry, int)
```

From backend/playlist/playlist.go:
```go
func (s *Service) getLibraryRoot() string  // returns single root — THE BUG
// Called at lines: 370, 844, 984, 1304, 1448, 1536, 1616, 1743
```

From backend/database/sql/sqlcgen/libraries.sql.go:
```go
func (q *Queries) GetAllLibraries(ctx context.Context) ([]Library, error)
// Library has: ID int64, Name string, Path string
```
</interfaces>

<tasks>

<task type="auto">
  <name>Task 1: Add multi-root path resolution to m3u.go and update playlist.go call sites</name>
  <files>
    backend/playlist/m3u.go
    backend/playlist/playlist.go
  </files>
  <action>
**Part A — m3u.go: Add multi-root resolution functions**

1. Add `toAbsolutePathMultiRoot(relativePath string, libraryRoots []string) string`:
   - If `filepath.IsAbs(relativePath)`, return as-is (same as current)
   - For each root in `libraryRoots`, compute `filepath.Join(root, relativePath)` — return the first result (all roots will produce valid-looking paths; the caller uses the result as a map key, so the first root that matches wins)
   - Actually, this function should try each root and check if the resulting path exists as a key in a provided map. BUT that couples m3u.go to the caller's map. Instead, return ALL possible absolute paths and let the caller check.
   - **Better approach**: Keep it simple. Add `resolveM3UPath(relativePath string, libraryRoots []string, knownPaths map[string]struct{}) string` that:
     - If `filepath.IsAbs(relativePath)` and path is in `knownPaths`, return it
     - For each root, compute `absPath := filepath.Join(root, relativePath)` and check `knownPaths[absPath]`; if found, return `absPath`
     - If no root matches, fall back to `filepath.Join(libraryRoots[0], relativePath)` if roots non-empty, else return `relativePath` (preserves current behavior for phantoms)
   - This keeps resolution in one place and avoids O(n) loops at each call site.

2. Update `removeM3UEntries` signature: `removeM3UEntries(entries []m3uEntry, targetAbsPaths map[string]struct{}, libraryRoots []string) []m3uEntry`
   - Inside, for each entry, try `toAbsolutePath` against each root, check if any resolves into `targetAbsPaths`

3. Update `replaceM3UEntryPaths` signature: `replaceM3UEntryPaths(entries []m3uEntry, replacements map[string]string, libraryRoots []string) []m3uEntry`
   - Inside, for each entry, try each root, check if any resolves into `replacements`

4. Update `findM3UEntry` signature: `findM3UEntry(entries []m3uEntry, targetAbsPath string, libraryRoots []string) (m3uEntry, int)`
   - Inside, for each entry, try each root, compare against target

5. Add `toRelativePathMultiRoot(absolutePath string, libraryRoots []string) string`:
   - Try `filepath.Rel(root, absolutePath)` for each root
   - Return the first result that doesn't start with ".." (i.e., the path is under that root)
   - If no root matches, return `absolutePath` (keeps absolute, same as current fallback)

**Part B — playlist.go: Replace getLibraryRoot with getAllLibraryRoots**

1. Replace `getLibraryRoot() string` with `getAllLibraryRoots() []string`:
   ```go
   func (s *Service) getAllLibraryRoots() []string {
       // Legacy config fallback.
       if s.libraryDir != nil {
           if dir := s.libraryDir.GetLibraryDirectory(); dir != "" {
               return []string{dir}
           }
       }
       libs, err := s.db.Queries.GetAllLibraries(s.db.Ctx)
       if err != nil || len(libs) == 0 {
           return nil
       }
       roots := make([]string, len(libs))
       for i, lib := range libs {
           roots[i] = lib.Path
       }
       return roots
   }
   ```

2. Update `mergeTracksForPlaylist` (line ~370):
   - Change `libraryRoot := s.getLibraryRoot()` to `libraryRoots := s.getAllLibraryRoots()`
   - Build a `knownPaths` set from `dbTracks` keys: `knownPaths := make(map[string]struct{}, len(dbTracks)); for k := range dbTracks { knownPaths[k] = struct{}{} }`
   - Replace the loop body to use `resolveM3UPath(entry.RelativePath, libraryRoots, knownPaths)` instead of `toAbsolutePath(entry.RelativePath, libraryRoot)`

3. Update `importPlaylist` (line ~844):
   - Change to `libraryRoots := s.getAllLibraryRoots()`
   - For each entry, try resolving against each root: `absPath := toAbsolutePath(entry.RelativePath, root)` then `GetAudioFileByPath(absPath)`
   - But that's O(entries × roots) DB queries. Better: just try each root's absolute path, use first that works. Wrap in a helper if cleaner.
   - Actually, for import, the current approach makes one DB query per entry. With multi-root, wrap in a loop: try each root until `GetAudioFileByPath` succeeds.

4. Update `RestoreAllPlaylists` / `restoreSinglePlaylist` (line ~984, ~1018):
   - Same pattern: pass `libraryRoots` instead of `libraryRoot`
   - `restoreSinglePlaylist` already receives `libraryRoot string` — change to `libraryRoots []string`
   - Inside, for each M3U8 entry, try each root

5. Update `buildM3UEntries` (line ~1304):
   - Use `toRelativePathMultiRoot(row.FilePath, libraryRoots)` — this finds the correct root for each track's absolute path

6. Update `saveImportedPlaylistFile` (line ~1245):
   - Change to multi-root, use `toRelativePathMultiRoot`

7. Update `FindPhantomMatches` (line ~1448):
   - Pass `libraryRoots` to `findM3UEntry` and entry resolution

8. Update `GetPhantomCandidates` (line ~1536):
   - Same pattern

9. Update `ResolvePhantomTracks` (line ~1616):
   - Same pattern

10. Update `RemovePhantomTracks` (line ~1743):
    - Same pattern

**Delete `getLibraryRoot`** after all call sites are migrated. Keep `toAbsolutePath` and `toRelativePath` — they're still useful as single-root primitives called by the multi-root wrappers.

**IMPORTANT CODEBASE PATTERNS:**
- Mutex-protected setter pattern (lock → write → release → callbacks)
- SAFETY comment convention for hand-crafted SQL
- Do NOT change any SQL queries — this is purely Go-side path resolution
  </action>
  <verify>
    go build ./backend/... compiles without errors
    go vet ./backend/playlist/... passes
  </verify>
  <done>
    - `getLibraryRoot()` replaced with `getAllLibraryRoots()` returning all library paths
    - `mergeTracksForPlaylist` resolves M3U8 entries against all roots
    - `restoreSinglePlaylist` resolves M3U8 entries against all roots
    - `buildM3UEntries` saves relative paths using correct root per track
    - All m3u.go helper functions accept `[]string` roots
    - All 9 call sites updated
  </done>
</task>

<task type="auto">
  <name>Task 2: Add multi-root path resolution tests</name>
  <files>backend/playlist/m3u_test.go</files>
  <action>
Add tests to m3u_test.go for the new multi-root functions:

1. `TestToAbsolutePathMultiRoot` (or `TestResolveM3UPath`):
   - Relative path resolves against first matching root
   - Absolute path returned as-is
   - Empty roots returns relative path unchanged
   - Path under second root resolves correctly (not just first)

2. `TestToRelativePathMultiRoot`:
   - Path under first root returns relative to first root
   - Path under second root returns relative to second root
   - Path under no root returns absolute
   - Empty roots returns absolute

3. Update existing `TestRemoveM3UEntries` to use `[]string{"/music"}` instead of `"/music"`

4. Update existing `TestRemoveM3UEntriesAll` similarly

5. Update existing `TestReplaceM3UEntryPaths` similarly

6. Update existing `TestFindM3UEntry` similarly

7. Add a multi-root variant of `TestRemoveM3UEntries`:
   - Entries from different roots, targets from mixed roots, correct entries removed

8. Add a multi-root variant of `TestFindM3UEntry`:
   - Entry relative to second root found correctly

Follow existing test patterns: table-driven, `t.Parallel()`, descriptive names.
  </action>
  <verify>
    go test ./backend/playlist/... -run "TestResolveM3UPath|TestToRelativePathMultiRoot|TestRemoveM3UEntries|TestReplaceM3UEntryPaths|TestFindM3UEntry" -v passes
  </verify>
  <done>
    - Multi-root resolution tested with multiple library roots
    - Edge cases covered (empty roots, absolute paths, no match)
    - Existing tests updated for new signatures
    - All tests pass
  </done>
</task>

</tasks>

<verification>
1. `go build ./backend/...` — compiles cleanly
2. `go vet ./backend/playlist/...` — no issues
3. `go test ./backend/playlist/...` — all tests pass (existing + new)
4. `go test ./backend/...` — full backend test suite passes (no regressions)
</verification>

<success_criteria>
- Playlist M3U8 path resolution tries all library roots instead of just the first
- M3U8 save uses the correct library root for each track's absolute path
- All existing playlist tests continue to pass
- New tests verify multi-root resolution behavior
- No changes to SQL queries or schema
</success_criteria>

<output>
After completion, create `.planning/quick/19-fix-phantom-playlist-tracks/19-SUMMARY.md`
</output>
