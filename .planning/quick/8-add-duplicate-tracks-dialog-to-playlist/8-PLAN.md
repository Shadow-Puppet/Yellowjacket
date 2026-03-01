---
phase: quick
plan: 8
type: execute
wave: 1
depends_on: []
files_modified:
  - backend/playlist/playlist.go
  - frontend/src/components/duplicate-tracks-dialog/duplicate-tracks-dialog.ts
  - frontend/src/components/playlist-picker/playlist-picker.ts
  - frontend/src/components/playlist-view/playlist-view.ts
autonomous: true
requirements: [QUICK-008]

must_haves:
  truths:
    - "When adding tracks that already exist in a playlist, user sees a dialog listing duplicates"
    - "User can add or skip each duplicate track one at a time"
    - "User can toggle 'apply to all remaining' to batch-apply current choice"
    - "Non-duplicate tracks are added silently without dialog"
    - "If no duplicates exist, tracks are added directly with no dialog"
  artifacts:
    - path: "backend/playlist/playlist.go"
      provides: "FindDuplicateTracksInPlaylist method"
      contains: "FindDuplicateTracksInPlaylist"
    - path: "frontend/src/components/duplicate-tracks-dialog/duplicate-tracks-dialog.ts"
      provides: "Modal dialog for stepping through duplicate tracks"
      exports: ["DuplicateTracksDialog"]
    - path: "frontend/src/components/playlist-picker/playlist-picker.ts"
      provides: "Updated to check for duplicates before adding"
    - path: "frontend/src/components/playlist-view/playlist-view.ts"
      provides: "Updated drag-drop handler to check for duplicates"
  key_links:
    - from: "frontend/src/components/playlist-picker/playlist-picker.ts"
      to: "backend/playlist/playlist.go"
      via: "FindDuplicateTracksInPlaylist Wails binding"
      pattern: "FindDuplicateTracksInPlaylist"
    - from: "frontend/src/components/playlist-picker/playlist-picker.ts"
      to: "frontend/src/components/duplicate-tracks-dialog/duplicate-tracks-dialog.ts"
      via: "dialog.show() call when duplicates found"
      pattern: "duplicateDialog.*show"
---

<objective>
Add a duplicate tracks dialog that intercepts track additions to playlists. When the user
adds tracks that already exist in the target playlist, a modal dialog appears showing each
duplicate one at a time with track details (title, artist, album). The user can "Add" or
"Skip" each duplicate, with a toggle to apply the current choice to all remaining duplicates.

Purpose: Prevent accidental duplicate track additions while giving the user full control.
Output: Backend duplicate detection method, new dialog component, updated playlist-picker and
playlist-view drag-drop to use the dialog.
</objective>

<execution_context>
@/home/caleb/.config/Claude/get-shit-done/workflows/execute-plan.md
@/home/caleb/.config/Claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/STATE.md
@frontend/src/components/playlist-picker/playlist-picker.ts
@frontend/src/components/track-details/track-details.ts
@frontend/src/components/phantom-resolver/phantom-resolver.ts
@frontend/src/components/playlist-view/playlist-view.ts
@backend/playlist/playlist.go
@backend/database/sql/queries/playlists.sql
</context>

<interfaces>
<!-- Key types and contracts the executor needs. -->

From backend/playlist/playlist.go:
```go
type Track struct {
    ID             int64  `json:"ID"`
    Position       int64  `json:"Position"`
    FilePath       string `json:"FilePath"`
    Title          string `json:"Title"`
    Artist         string `json:"Artist"`
    Album          string `json:"Album"`
    CoverArtPath   string `json:"CoverArtPath"`
    CoverArtSmall  string `json:"CoverArtSmall"`
    CoverArtMedium string `json:"CoverArtMedium"`
    CoverArtLarge  string `json:"CoverArtLarge"`
    Duration       string `json:"Duration"`
    Phantom        bool   `json:"Phantom"`
}

func (s *Service) AddTracksToPlaylist(playlistID int64, filePaths []string) error
```

From backend/database/sql/queries/playlists.sql:
```sql
-- name: IsTrackInPlaylist :one
SELECT EXISTS(
    SELECT 1 FROM playlist_tracks pt
    JOIN audio_files af ON pt.audio_file_id = af.id
    WHERE pt.playlist_id = ? AND af.file_path = ?
) AS in_playlist;

-- name: GetPlaylistTrackFilePaths :many
SELECT af.file_path
FROM playlist_tracks pt
JOIN audio_files af ON pt.audio_file_id = af.id
WHERE pt.playlist_id = ?
ORDER BY pt.position;
```

From frontend — playlist-picker fires `playlist-action-complete` event on success.

From frontend — wa-dialog pattern (from track-details.ts):
```typescript
import '@awesome.me/webawesome/dist/components/dialog/dialog.js';
@query('wa-dialog')
private dialog!: HTMLElement & { open: boolean };
show() { this.updateComplete.then(() => { this.dialog.open = true; }); }
close() { this.dialog.open = false; }
```

From frontend — wa-switch component is available at:
```
@awesome.me/webawesome/dist/components/switch/switch.js
```
</interfaces>

<tasks>

<task type="auto">
  <name>Task 1: Add backend FindDuplicateTracksInPlaylist method</name>
  <files>
    backend/playlist/playlist.go
  </files>
  <action>
Add new exported types and a method `FindDuplicateTracksInPlaylist` to the playlist `Service`. Place the types near the existing `Track`, `CandidateTrack` etc. structs at the top of the file.

**Important:** Wails bindings only support `(T, error)` or `error` return signatures. Use a wrapper struct:

```go
// DuplicateTrackInfo holds metadata for a track that already exists in a playlist.
type DuplicateTrackInfo struct {
    FilePath string `json:"FilePath"`
    Title    string `json:"Title"`
    Artist   string `json:"Artist"`
    Album    string `json:"Album"`
    Duration string `json:"Duration"`
}

// DuplicateCheckResult contains the outcome of checking for duplicate tracks.
type DuplicateCheckResult struct {
    Duplicates []DuplicateTrackInfo `json:"Duplicates"`
    Unique     []string             `json:"Unique"`
}

// FindDuplicateTracksInPlaylist checks which of the given file paths
// already exist in the specified playlist. Returns metadata for each
// duplicate and a list of non-duplicate file paths.
func (s *Service) FindDuplicateTracksInPlaylist(
    playlistID int64,
    filePaths []string,
) (DuplicateCheckResult, error)
```

Implementation:
1. Call `s.db.Queries.GetPlaylistTracksWithMetadata(s.db.Ctx, playlistID)` once.
2. Build `existingPaths map[string]sqlcgen.GetPlaylistTracksWithMetadataRow` from results, keyed by `row.FilePath`.
3. For each incoming filePath:
   - If in map → append `DuplicateTrackInfo` with Title, Artist, Album, LengthMilliseconds from the row.
   - If not in map → append to `Unique` slice.
4. Return `DuplicateCheckResult{Duplicates: duplicates, Unique: unique}, nil`.
5. If the initial query fails, return the error.

After adding the method, run `wails generate module` from the project root to regenerate the TypeScript bindings.
  </action>
  <verify>
    `go build ./backend/playlist/...` compiles without errors. Run `wails generate module` and confirm `frontend/wailsjs/go/playlist/Service.d.ts` contains `FindDuplicateTracksInPlaylist`.
  </verify>
  <done>
    Backend exposes `FindDuplicateTracksInPlaylist(playlistID, filePaths)` returning duplicate track info and unique paths. Wails TypeScript bindings regenerated.
  </done>
</task>

<task type="auto">
  <name>Task 2: Create duplicate-tracks-dialog component</name>
  <files>
    frontend/src/components/duplicate-tracks-dialog/duplicate-tracks-dialog.ts
  </files>
  <action>
Create a new Lit component `<duplicate-tracks-dialog>` following the same patterns as `track-details.ts` and `phantom-resolver.ts` for wa-dialog usage.

**Component API:**
```typescript
import { LitElement, html, css, nothing } from 'lit';
import { customElement, state, query } from 'lit/decorators.js';
import '@awesome.me/webawesome/dist/components/dialog/dialog.js';
import '@awesome.me/webawesome/dist/components/icon/icon.js';
import '@awesome.me/webawesome/dist/components/switch/switch.js';
import { AddTracksToPlaylist } from '@go/playlist/Service';
import type { playlist } from '@go/models';

interface DuplicateTrack {
    FilePath: string;
    Title: string;
    Artist: string;
    Album: string;
    Duration: string;
}

@customElement('duplicate-tracks-dialog')
export class DuplicateTracksDialog extends LitElement {
    @query('wa-dialog')
    private dialog!: HTMLElement & { open: boolean };

    @state() private duplicates: DuplicateTrack[] = [];
    @state() private currentIndex = 0;
    @state() private applyToAll = false;
    private playlistId = 0;
    private uniquePaths: string[] = [];
    private tracksToAdd: string[] = [];  // accumulated "Add" choices

    /** Opens the dialog. Called by playlist-picker when duplicates are found. */
    show(
        playlistId: number,
        duplicates: DuplicateTrack[],
        uniquePaths: string[],
    ): void { ... }

    close(): void { ... }
}
```

**Dialog layout:**
- `wa-dialog` with label "Duplicate Tracks Found"
- `--width: 480px`
- Header text: "**{N} duplicate track(s)** already exist in this playlist."
- Progress indicator: "Track {current} of {total}"
- Current track card showing: Title (bold, 15px), Artist (secondary, 13px), Album (tertiary, 13px), Duration (tertiary, 12px, tabular-nums)
- A `wa-switch` with label "Apply to all remaining" — when toggled on, the next Add/Skip applies to all remaining duplicates at once.
- Two action buttons at the bottom: "Skip" (secondary .btn style) and "Add" (primary .btn-primary style, accent colored).

**Behavior:**
1. `show()` stores playlistId, duplicates, uniquePaths. Sets currentIndex=0, applyToAll=false, tracksToAdd=[]. Opens dialog.
2. When "Add" is clicked:
   - Push `duplicates[currentIndex].FilePath` to `tracksToAdd`.
   - If `applyToAll` is true: push ALL remaining duplicate file paths to `tracksToAdd`, then finalize.
   - Else: advance `currentIndex`. If past end, finalize.
3. When "Skip" is clicked:
   - Do NOT add the current track.
   - If `applyToAll` is true: skip all remaining (finalize immediately).
   - Else: advance `currentIndex`. If past end, finalize.
4. `finalize()`:
   - Combine `uniquePaths` + `tracksToAdd` into one array.
   - If array is non-empty, call `await AddTracksToPlaylist(this.playlistId, combined)`.
   - Dispatch `playlist-action-complete` event (bubbles: true, composed: true).
   - Close dialog.

**Styling:** Follow project conventions — use `--yj-*` CSS custom properties. Match the `track-details.ts` dialog styling for consistency (same `wa-dialog::part(*)` rules). The track card should have a subtle background (`--yj-bg-elevated`), rounded corners (6px), padding (16px), and the info stacked vertically.

Use `formatMilliseconds` from `@utils/time` for duration display.

**Important:** The wa-switch `@wa-change` event fires with `e.target.checked` as a boolean. Use:
```html
<wa-switch
    size="small"
    ?checked=${this.applyToAll}
    @wa-change=${(e: Event) => {
        this.applyToAll = (e.target as HTMLInputElement).checked;
    }}
>
    Apply to all remaining
</wa-switch>
```
  </action>
  <verify>
    `npm run build` (or the project's build command) compiles without errors. The new component file exists at `frontend/src/components/duplicate-tracks-dialog/duplicate-tracks-dialog.ts`.
  </verify>
  <done>
    `<duplicate-tracks-dialog>` component renders a wa-dialog stepping through duplicate tracks one by one with Add/Skip buttons and an "apply to all" toggle. Dispatches `playlist-action-complete` when done.
  </done>
</task>

<task type="auto">
  <name>Task 3: Wire duplicate detection into playlist-picker and playlist-view drag-drop</name>
  <files>
    frontend/src/components/playlist-picker/playlist-picker.ts
    frontend/src/components/playlist-view/playlist-view.ts
  </files>
  <action>
**playlist-picker.ts changes:**

1. Add imports:
```typescript
import {
    GetAllPlaylists,
    AddTracksToPlaylist,
    CreatePlaylistWithTracks,
    FindDuplicateTracksInPlaylist,
} from '@go/playlist/Service';
import '@components/duplicate-tracks-dialog/duplicate-tracks-dialog.js';
import type { DuplicateTracksDialog } from '@components/duplicate-tracks-dialog/duplicate-tracks-dialog.js';
```

2. Add a query for the dialog (render it in the template):
```typescript
@query('duplicate-tracks-dialog')
private duplicateDialog!: DuplicateTracksDialog;
```

3. Modify `handleSelectPlaylist` to check for duplicates BEFORE adding:
```typescript
private handleSelectPlaylist = async (playlistId: number) => {
    if (this.loading || this.filePaths.length === 0) return;
    this.loading = true;

    try {
        const result = await FindDuplicateTracksInPlaylist(playlistId, this.filePaths);
        const duplicates = result.Duplicates ?? [];
        const unique = result.Unique ?? [];

        if (duplicates.length > 0) {
            // Show dialog — it will handle adding tracks and dispatching completion
            this.loading = false;
            await this.updateComplete;
            this.duplicateDialog.show(playlistId, duplicates, unique);
            return;
        }

        // No duplicates — add all directly
        await AddTracksToPlaylist(playlistId, this.filePaths);
        this.dispatchComplete();
    } catch (err) {
        console.error('Failed to add tracks to playlist:', err);
    } finally {
        this.loading = false;
    }
};
```

4. Add the dialog element to the render template, just before the closing of `renderPlaylistList()` and `renderCreateForm()` — or better, add it to the main `render()` method so it's always in the DOM:
```typescript
override render() {
    return html`
        ${this.mode === 'create' ? this.renderCreateForm() : this.renderPlaylistList()}
        <duplicate-tracks-dialog
            @playlist-action-complete=${this.dispatchComplete}
        ></duplicate-tracks-dialog>
    `;
}
```

Note: The `dispatchComplete` call from the dialog will bubble up through the playlist-picker, which is exactly what consumers listen for. The dialog's `playlist-action-complete` event is caught here and re-dispatched by the picker's own `dispatchComplete`.

**playlist-view.ts changes:**

1. Add imports at top (near existing imports):
```typescript
import { FindDuplicateTracksInPlaylist } from '@go/playlist/Service';
import '@components/duplicate-tracks-dialog/duplicate-tracks-dialog.js';
import type { DuplicateTracksDialog } from '@components/duplicate-tracks-dialog/duplicate-tracks-dialog.js';
```

2. Add a query for the dialog:
```typescript
@query('duplicate-tracks-dialog')
private duplicateDialog!: DuplicateTracksDialog;
```

3. Find the drag-drop handler `handlePlaylistDrop` (around line ~1823) that calls `await AddTracksToPlaylist(entry.summary.ID, payload.filePaths)` and wrap it with duplicate detection:
```typescript
// Replace the direct AddTracksToPlaylist call:
const result = await FindDuplicateTracksInPlaylist(
    entry.summary.ID,
    payload.filePaths,
);
const duplicates = result.Duplicates ?? [];
const unique = result.Unique ?? [];

if (duplicates.length > 0) {
    await this.updateComplete;
    this.duplicateDialog.show(entry.summary.ID, duplicates, unique);
    return;
}

await AddTracksToPlaylist(entry.summary.ID, payload.filePaths);
await this.refreshPlaylists();
```

4. Add `<duplicate-tracks-dialog>` to the playlist-view's render output. Find the location where `<track-details>` and `<phantom-resolver>` are rendered (likely near the end of the main render method) and add alongside them:
```html
<duplicate-tracks-dialog
    @playlist-action-complete=${() => this.refreshPlaylists()}
></duplicate-tracks-dialog>
```

**Return type handling:** The Go method returns `([]DuplicateTrackInfo, []string, error)`. Wails will generate a TypeScript binding that returns an object. After running `wails generate module` in Task 1, check the generated types in `frontend/wailsjs/go/playlist/Service.d.ts` and `frontend/wailsjs/go/models.ts` to confirm the return shape. Go functions with multiple return values are mapped by Wails — typically a struct wrapper is needed. 

**Important adjustment:** Go functions exposed to Wails can only return `(T, error)` or `error`. Multiple return values won't work. So in Task 1, the method must return a struct:

```go
type DuplicateCheckResult struct {
    Duplicates []DuplicateTrackInfo `json:"Duplicates"`
    Unique     []string             `json:"Unique"`
}

func (s *Service) FindDuplicateTracksInPlaylist(
    playlistID int64,
    filePaths []string,
) (DuplicateCheckResult, error)
```

This way Wails generates `FindDuplicateTracksInPlaylist(playlistID: number, filePaths: string[]): Promise<playlist.DuplicateCheckResult>` and the frontend accesses `result.Duplicates` and `result.Unique`.
  </action>
  <verify>
    `npm run build` compiles. Test manually: drag tracks that are already in a playlist onto that playlist in the playlist-view sidebar — the duplicate dialog should appear. Using the context menu "Add to playlist" picker with tracks that already exist should also trigger the dialog. Adding tracks with no duplicates should work without any dialog.
  </verify>
  <done>
    Playlist-picker and playlist-view drag-drop both check for duplicates before adding. When duplicates found, the dialog appears for one-by-one resolution. When no duplicates, tracks are added directly as before.
  </done>
</task>

</tasks>

<verification>
1. `go build ./...` — backend compiles
2. `npm run build` (in frontend/) — frontend compiles  
3. `wails build` — full app builds
4. Manual test: Add tracks to a playlist that already contains some of them → dialog appears
5. Manual test: Add tracks to a playlist with zero duplicates → no dialog, tracks added directly
6. Manual test: Use "Apply to all remaining" toggle → batch add/skip works
7. Manual test: Drag-drop tracks onto playlist in sidebar → same duplicate detection behavior
</verification>

<success_criteria>
- Duplicate detection works for both playlist-picker (context menu) and playlist-view (drag-drop) flows
- Dialog shows track details (title, artist, album, duration) for each duplicate
- Add/Skip buttons advance through duplicates one at a time
- "Apply to all remaining" toggle batch-applies the current choice
- Non-duplicate tracks are always added regardless of dialog choices
- No dialog appears when there are zero duplicates
</success_criteria>

<output>
After completion, create `.planning/quick/8-add-duplicate-tracks-dialog-to-playlist/8-SUMMARY.md`
</output>
