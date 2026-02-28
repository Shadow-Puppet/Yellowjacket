---
phase: quick-001
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - backend/frontendutil/frontendutil.go
  - backend/playlist/playlist.go
  - frontend/src/components/playlist-view/playlist-view.ts
autonomous: true
requirements: [MULTI-IMPORT]

must_haves:
  truths:
    - "File picker allows selecting multiple M3U/M3U8 files at once"
    - "All selected playlists are imported sequentially into the database"
    - "Each imported playlist emits a PlaylistCreated event and appears in the UI"
    - "Cancelling the file picker (selecting nothing) is a no-op"
    - "Errors during individual imports are collected and reported"
  artifacts:
    - path: "backend/frontendutil/frontendutil.go"
      provides: "Multi-file picker returning []string"
      contains: "OpenMultipleFilesDialog"
    - path: "backend/playlist/playlist.go"
      provides: "ImportPlaylists batch method"
      contains: "func (s *Service) ImportPlaylists"
    - path: "frontend/src/components/playlist-view/playlist-view.ts"
      provides: "Updated import handler calling batch API"
      contains: "ImportPlaylists"
  key_links:
    - from: "frontend/src/components/playlist-view/playlist-view.ts"
      to: "backend/frontendutil/frontendutil.go"
      via: "Wails binding PlaylistFilePicker"
      pattern: "PlaylistFilePicker"
    - from: "frontend/src/components/playlist-view/playlist-view.ts"
      to: "backend/playlist/playlist.go"
      via: "Wails binding ImportPlaylists"
      pattern: "ImportPlaylists"
---

<objective>
Make the "Import Playlist" feature support selecting and importing multiple M3U/M3U8 files at once.

Purpose: Users often have several playlist files to import — forcing one-at-a-time selection is tedious.
Output: Updated backend methods, regenerated Wails bindings, and updated frontend handler.
</objective>

<execution_context>
@/home/caleb/.config/Claude/get-shit-done/workflows/execute-plan.md
@/home/caleb/.config/Claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@backend/frontendutil/frontendutil.go
@backend/playlist/playlist.go
@frontend/src/components/playlist-view/playlist-view.ts
@frontend/wailsjs/go/frontendutil/FrontendUtil.d.ts
@frontend/wailsjs/go/playlist/Service.d.ts

<interfaces>
<!-- Current signatures that will change -->

From backend/frontendutil/frontendutil.go:
```go
func (fe *FrontendUtil) PlaylistFilePicker() (string, error)
// Uses runtime.OpenFileDialog — single file selection
```

From backend/playlist/playlist.go:
```go
func (s *Service) ImportPlaylist(filePath string) (Summary, error)
// Imports a single M3U/M3U8 file, creates DB entry, emits PlaylistCreated event

type Summary struct {
    ID   int64  `json:"ID"`
    Name string `json:"Name"`
}

var errEmptyFilePath    = errors.New("file path cannot be empty")
var errNoFilePaths      = errors.New("no file paths provided")
var errUnsupportedFileType = errors.New("unsupported file type")
```

From Wails runtime API:
```go
func OpenMultipleFilesDialog(ctx context.Context, dialogOptions OpenDialogOptions) ([]string, error)
```

From frontend bindings:
```typescript
// Current:
export function PlaylistFilePicker(): Promise<string>;
export function ImportPlaylist(arg1: string): Promise<playlist.Summary>;

// After change (auto-generated):
// PlaylistFilePicker(): Promise<Array<string>>;
// ImportPlaylists(arg1: Array<string>): Promise<Array<playlist.Summary>>;
```
</interfaces>
</context>

<tasks>

<task type="auto">
  <name>Task 1: Update backend — multi-file picker and batch import</name>
  <files>
    backend/frontendutil/frontendutil.go
    backend/playlist/playlist.go
  </files>
  <action>
1. In `backend/frontendutil/frontendutil.go`, update `PlaylistFilePicker()`:
   - Change return type from `(string, error)` to `([]string, error)`
   - Replace `runtime.OpenFileDialog(...)` with `runtime.OpenMultipleFilesDialog(...)` using the same `runtime.OpenDialogOptions` (Title, Filters unchanged)
   - Update the log message to say "selecting playlist files"
   - Update the error message to "could not open file dialog: %w" (keep consistent)

2. In `backend/playlist/playlist.go`, add a new exported method `ImportPlaylists` that accepts a batch of file paths. Place it directly after the existing `ImportPlaylist` method (after line 794):

```go
// ImportPlaylists imports multiple playlists from external M3U/M3U8
// files. Each file is imported sequentially using ImportPlaylist.
// Errors from individual imports are collected; partial success is
// possible. Returns the summaries of successfully imported playlists
// and the first error encountered (if any).
func (s *Service) ImportPlaylists(
	filePaths []string,
) ([]Summary, error) {
	if len(filePaths) == 0 {
		return nil, errNoFilePaths
	}

	summaries := make([]Summary, 0, len(filePaths))
	var firstErr error

	for _, fp := range filePaths {
		summary, err := s.ImportPlaylist(fp)
		if err != nil {
			s.logger.Warn(
				"Failed to import playlist file",
				"path", fp,
				"err", err,
			)

			if firstErr == nil {
				firstErr = fmt.Errorf(
					"import %q failed: %w", fp, err,
				)
			}

			continue
		}

		summaries = append(summaries, summary)
	}

	return summaries, firstErr
}
```

Key design decisions:
- Sequential, NOT parallel — SQLite lock contention avoidance per research.
- Partial success — continues importing remaining files even if one fails.
- Returns first error + all successful summaries so the frontend can show what worked and what didn't.
- Reuses existing `ImportPlaylist` — no logic duplication.
- `errNoFilePaths` sentinel already exists (line 28).

Do NOT modify the existing `ImportPlaylist` method signature or behavior — it remains available for single-file import internally.
  </action>
  <verify>
    <automated>cd /mnt/vault/dev/golang/yellowjacket && go vet ./backend/frontendutil/... ./backend/playlist/...</automated>
  </verify>
  <done>
    - `PlaylistFilePicker()` returns `([]string, error)` and uses `OpenMultipleFilesDialog`
    - `ImportPlaylists([]string) ([]Summary, error)` exists and delegates to `ImportPlaylist` per file
    - `go vet` passes for both packages
  </done>
</task>

<task type="auto">
  <name>Task 2: Regenerate Wails bindings and update frontend</name>
  <files>
    frontend/wailsjs/go/frontendutil/FrontendUtil.js
    frontend/wailsjs/go/frontendutil/FrontendUtil.d.ts
    frontend/wailsjs/go/playlist/Service.js
    frontend/wailsjs/go/playlist/Service.d.ts
    frontend/src/components/playlist-view/playlist-view.ts
  </files>
  <action>
1. Regenerate Wails bindings:
   ```
   wails generate module
   ```
   This will update the auto-generated files at:
   - `frontend/wailsjs/go/frontendutil/FrontendUtil.{js,d.ts}` — `PlaylistFilePicker` return type becomes `Promise<Array<string>>`
   - `frontend/wailsjs/go/playlist/Service.{js,d.ts}` — new `ImportPlaylists` binding appears

2. In `frontend/src/components/playlist-view/playlist-view.ts`, update the imports (around line 15-17):
   - Change `ImportPlaylist` to `ImportPlaylists` in the import from `@go/playlist/Service`

3. Update `handleImportPlaylist` method (starting at line 1874). Replace the entire method body:

```typescript
private handleImportPlaylist = async () => {
    try {
        const filePaths =
            await PlaylistFilePicker();

        if (!filePaths || filePaths.length === 0) return;

        this.importError = '';
        await ImportPlaylists(filePaths);
    } catch (err) {
        console.error(
            'Failed to import playlist:',
            err,
        );
        this.importError =
            err instanceof Error
                ? err.message
                : String(err);
        setTimeout(() => {
            this.importError = '';
        }, 6000);
    }
};
```

Key changes:
- `PlaylistFilePicker()` now returns `string[]` — check for empty array instead of falsy string
- Call `ImportPlaylists(filePaths)` instead of `ImportPlaylist(filePath)`
- Error handling logic stays the same (toast with 6s auto-clear)
- No need to manually refresh — each imported playlist fires `PlaylistCreated` event which triggers the existing reactive refresh via `PlaylistController`
  </action>
  <verify>
    <automated>cd /mnt/vault/dev/golang/yellowjacket && wails generate module && cd frontend && npx tsc --noEmit</automated>
  </verify>
  <done>
    - Wails bindings regenerated with new signatures
    - `FrontendUtil.d.ts` shows `PlaylistFilePicker(): Promise<Array<string>>`
    - `Service.d.ts` shows `ImportPlaylists(arg1: Array<string>): Promise<Array<playlist.Summary>>`
    - Frontend imports `ImportPlaylists` (not `ImportPlaylist`)
    - `handleImportPlaylist` handles array of file paths
    - TypeScript compiles with no errors
  </done>
</task>

</tasks>

<verification>
1. `go vet ./backend/...` — no issues
2. `wails generate module` — succeeds
3. `npx tsc --noEmit` (from frontend/) — no type errors
4. Build check: `go build ./...` — compiles successfully
</verification>

<success_criteria>
- Multi-file selection dialog opens when clicking Import
- Backend accepts and processes array of file paths sequentially
- Frontend correctly passes array to new ImportPlaylists binding
- All code compiles and type-checks cleanly
</success_criteria>

<output>
After completion, create `.planning/quick/001-multi-playlist-import-support/001-SUMMARY.md`
</output>
