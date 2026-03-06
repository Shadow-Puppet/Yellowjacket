---
phase: quick
plan: 002
type: execute
wave: 1
depends_on: []
files_modified:
  - backend/database/sql/queries/playlists.sql
  - backend/database/sql/sqlcgen/playlists.sql.go
  - backend/playlist/playlist.go
autonomous: true
requirements: [QUICK-002]

must_haves:
  truths:
    - "Importing an M3U whose name matches an existing playlist auto-renames to Name (1)"
    - "Importing again produces Name (2), Name (3), etc."
    - "CreatePlaylist and CreatePlaylistWithTracks do NOT auto-rename (only import)"
  artifacts:
    - path: "backend/database/sql/queries/playlists.sql"
      provides: "CountPlaylistsByName query"
      contains: "CountPlaylistsByName"
    - path: "backend/database/sql/sqlcgen/playlists.sql.go"
      provides: "Generated CountPlaylistsByName function"
      contains: "CountPlaylistsByName"
    - path: "backend/playlist/playlist.go"
      provides: "uniquePlaylistName helper and ImportPlaylist integration"
      contains: "uniquePlaylistName"
  key_links:
    - from: "backend/playlist/playlist.go"
      to: "backend/database/sql/sqlcgen/playlists.sql.go"
      via: "s.db.Queries.CountPlaylistsByName"
      pattern: "CountPlaylistsByName"
---

<objective>
Auto-rename duplicate playlists on import — when importing an M3U/M3U8 file whose
derived name matches an existing playlist, automatically append (1), (2), etc. instead
of creating a duplicate. Only applies to import, not manual CreatePlaylist.

Purpose: Prevent confusing duplicate playlist names when importing the same file multiple times.
Output: Modified playlist SQL queries (+ regenerated sqlc), updated ImportPlaylist flow.
</objective>

<execution_context>
@/home/caleb/.config/Claude/get-shit-done/workflows/execute-plan.md
@/home/caleb/.config/Claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@backend/database/sql/queries/playlists.sql
@backend/database/sql/sqlcgen/playlists.sql.go
@backend/playlist/playlist.go
@backend/database/sqlc.yaml
</context>

<tasks>

<task type="auto">
  <name>Task 1: Add CountPlaylistsByName SQL query and regenerate sqlc</name>
  <files>
    backend/database/sql/queries/playlists.sql
    backend/database/sql/sqlcgen/playlists.sql.go
  </files>
  <action>
    Add a new sqlc query to `backend/database/sql/queries/playlists.sql` at the end of the
    existing playlist queries (after `CreatePlaylist`, before track queries):

    ```sql
    -- name: CountPlaylistsByName :one
    SELECT COUNT(*) AS count FROM playlists WHERE name = ?;
    ```

    Then regenerate sqlc from `backend/database`:
    ```bash
    cd backend/database && sqlc generate
    ```

    This produces a `CountPlaylistsByName(ctx, name string) (int64, error)` function in
    `playlists.sql.go`. Verify the generated function exists and compiles.
  </action>
  <verify>
    `grep -q "CountPlaylistsByName" backend/database/sql/sqlcgen/playlists.sql.go` succeeds
    AND `go build ./backend/database/sql/sqlcgen/` compiles cleanly.
  </verify>
  <done>CountPlaylistsByName query exists in SQL and generated Go code compiles.</done>
</task>

<task type="auto">
  <name>Task 2: Add uniquePlaylistName helper and wire into ImportPlaylist</name>
  <files>backend/playlist/playlist.go</files>
  <action>
    **Add helper method** to `backend/playlist/playlist.go` (place it just above ImportPlaylist,
    around line 684):

    ```go
    // uniquePlaylistName returns a name that doesn't collide with existing
    // playlists. If "Chill Vibes" exists, returns "Chill Vibes (1)".
    // If that also exists, returns "Chill Vibes (2)", etc.
    func (s *Service) uniquePlaylistName(name string) string {
        count, err := s.db.Queries.CountPlaylistsByName(s.db.Ctx, name)
        if err != nil || count == 0 {
            return name
        }
        for i := 1; ; i++ {
            candidate := fmt.Sprintf("%s (%d)", name, i)
            c, err := s.db.Queries.CountPlaylistsByName(s.db.Ctx, candidate)
            if err != nil || c == 0 {
                return candidate
            }
        }
    }
    ```

    **Wire into ImportPlaylist** — after the `playlistName` derivation block (after the
    closing brace of the `if playlistName == ""` block, around line 715) and BEFORE the
    `s.db.Queries.CreatePlaylist` call (line 718), add:

    ```go
    playlistName = s.uniquePlaylistName(playlistName)
    ```

    **Important:** Do NOT add this call to `CreatePlaylist`, `CreatePlaylistWithTracks`, or
    any other method. Only `ImportPlaylist` gets auto-rename behavior.

    Verify the full package compiles: `go build ./backend/playlist/`
  </action>
  <verify>
    `go build ./backend/playlist/` compiles cleanly AND
    `grep -q "uniquePlaylistName" backend/playlist/playlist.go` succeeds AND
    `grep -c "uniquePlaylistName" backend/playlist/playlist.go` returns 3 (definition + method body call to self isn't counted — should be: func signature, call in ImportPlaylist, plus the function body references = at least 2-3 occurrences).
  </verify>
  <done>
    uniquePlaylistName helper exists and is called from ImportPlaylist (and ONLY ImportPlaylist).
    `go build ./backend/...` compiles. `go vet ./backend/...` passes.
  </done>
</task>

</tasks>

<verification>
```bash
# Full backend build
go build ./backend/...

# Vet check
go vet ./backend/...

# Verify CountPlaylistsByName exists in generated code
grep "CountPlaylistsByName" backend/database/sql/sqlcgen/playlists.sql.go

# Verify uniquePlaylistName is ONLY called from ImportPlaylist, not CreatePlaylist
# This grep should show the function definition and one call site in ImportPlaylist
grep -n "uniquePlaylistName" backend/playlist/playlist.go

# Verify CreatePlaylist method does NOT reference uniquePlaylistName
# (manual scan — the grep above should show it's only in ImportPlaylist context)
```
</verification>

<success_criteria>
- `go build ./backend/...` passes
- `go vet ./backend/...` passes
- CountPlaylistsByName query exists in SQL and generated Go
- uniquePlaylistName helper exists and is called only from ImportPlaylist
- CreatePlaylist / CreatePlaylistWithTracks unchanged
</success_criteria>

<output>
After completion, create `.planning/quick/002-auto-rename-duplicate-playlists-on-import/002-SUMMARY.md`
</output>
