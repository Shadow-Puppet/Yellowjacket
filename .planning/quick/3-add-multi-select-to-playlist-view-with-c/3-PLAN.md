---
phase: quick
plan: 3
type: execute
wave: 1
depends_on: []
files_modified:
  - frontend/src/components/playlist-view/playlist-view.ts
autonomous: true
requirements: [QUICK-03]

must_haves:
  truths:
    - "User can Ctrl+Click to toggle-select multiple playlist headers"
    - "User can Shift+Click to range-select playlists"
    - "Right-click on a selected playlist shows context menu with 'Delete N Playlists' option"
    - "Delete action removes all selected playlists and refreshes the list"
    - "Clicking a single playlist header without modifier still expands/collapses normally"
    - "Track-level multi-select within expanded playlists still works independently"
  artifacts:
    - path: "frontend/src/components/playlist-view/playlist-view.ts"
      provides: "Playlist-level multi-select with batch delete"
  key_links:
    - from: "playlist-view.ts (handlePlaylistHeaderClick)"
      to: "selectedPlaylistIndices state"
      via: "Ctrl/Shift+Click modifiers"
    - from: "playlist-view.ts (onPlaylistContextAction 'delete')"
      to: "DeletePlaylist backend call"
      via: "batch iteration over selected playlist IDs"
---

<objective>
Add playlist-level multi-select to the playlist view, allowing users to Ctrl+Click or Shift+Click playlist headers to select multiple playlists, then right-click to batch-delete them via the context menu.

Purpose: Currently users can only delete playlists one at a time. This adds standard multi-select UX (matching the existing track-level and album-level multi-select patterns) so users can quickly clean up multiple playlists.

Output: Updated playlist-view.ts with playlist-level multi-select and batch delete.
</objective>

<execution_context>
@.planning/quick/3-add-multi-select-to-playlist-view-with-c/3-PLAN.md
</execution_context>

<context>
@frontend/src/components/playlist-view/playlist-view.ts
@frontend/src/utils/selection-controller.ts
@frontend/src/utils/context-menu-controller.ts

<interfaces>
<!-- SelectionController is already imported and used for track-level selection.
     For playlist-level selection, we add a SECOND SelectionController instance
     (or use a simple Set<number> like cover-grid does for albums). -->

From selection-controller.ts:
```typescript
export interface SelectionHost extends ReactiveControllerHost {
    getItemKey(index: number): string | undefined;
    getItemCount(): number;
    onSelectionChanged?(): void;
}

export class SelectionController {
    handleItemClick(e: MouseEvent, key: string, index: number): void;
    handleContextMenu(key: string): void;
    clear(): void;
    isSelected(key: string): boolean;
    get hasSelection(): boolean;
    get selectionCount(): number;
    getSelectedIndices(): number[];
}
```

Existing playlist-view patterns:
- Track selection uses `SelectionController` with `activePlaylistIndex` scoping
- `SelectionHost` interface is already implemented for track selection
- Playlist context menu uses `playlistContextMenuOpen`, `playlistContextMenuIndex`, `playlistContextMenuPopup`
- `DeletePlaylist(id: number)` is the Go backend binding (deletes one at a time)
</interfaces>
</context>

<tasks>

<task type="auto">
  <name>Task 1: Add playlist-level multi-select state and selection handling</name>
  <files>frontend/src/components/playlist-view/playlist-view.ts</files>
  <action>
Add playlist-level multi-select using a simple `Set<number>` pattern (matching how cover-grid handles album selection — simpler than a second SelectionController since playlists use index-based identity and there are typically few of them).

**New state:**
- `@state() private selectedPlaylists: Set<number> = new Set();` — stores indices of selected playlists in the `entries` array
- `private lastSelectedPlaylistIndex: number | null = null;` — anchor for Shift+Click range selection

**Modify `handleToggle` (line ~1025):**
Rename to a new `handlePlaylistHeaderClick(e: MouseEvent, index: number)` that checks modifier keys:
- **No modifier:** Clear playlist selection, toggle expand/collapse as before (existing `handleToggle` logic). Set `lastSelectedPlaylistIndex = null`.
- **Ctrl/Cmd+Click (`e.ctrlKey || e.metaKey`):** Toggle the playlist at `index` in `selectedPlaylists`. Set `lastSelectedPlaylistIndex = index`. Do NOT expand/collapse.
- **Shift+Click (`e.shiftKey`):** If `lastSelectedPlaylistIndex !== null`, select all playlists in range `[lastSelectedPlaylistIndex, index]` (inclusive). Add to existing selection (like existing track selection behavior). Do NOT expand/collapse.

**Clear playlist selection on appropriate events:**
- When track selection starts (`ensureSelectionScope`), clear `selectedPlaylists` — prevent having both playlist-level and track-level selections active simultaneously.
- In the existing `clearSelectionHandler` (line ~243), also clear `selectedPlaylists` when clicking outside.

**Visual feedback — add CSS class:**
Add a `.playlist-header.selected` style:
```css
.playlist-header.selected {
    background-color: var(--yj-selection-bg, rgba(100, 160, 255, 0.15));
}
```

**Update `renderPlaylistItem` (line ~2387):**
Add `selected` class to `.playlist-header` div when `this.selectedPlaylists.has(index)`.

Wire the header's `@click` to the new `handlePlaylistHeaderClick(e, index)` instead of the old `handleToggle(index)`.
  </action>
  <verify>
    <automated>cd frontend && npx tsc --noEmit --pretty 2>&1 | head -30</automated>
  </verify>
  <done>Ctrl+Click toggles playlist selection (blue highlight), Shift+Click range-selects playlists, plain click still expands/collapses. Track selection and playlist selection are mutually exclusive.</done>
</task>

<task type="auto">
  <name>Task 2: Wire playlist context menu to support batch delete of selected playlists</name>
  <files>frontend/src/components/playlist-view/playlist-view.ts</files>
  <action>
**Modify `handlePlaylistContextMenu` (line ~1582):**
When right-clicking a playlist header:
- If the right-clicked playlist is NOT in `selectedPlaylists`, replace the selection with just that playlist (matching context menu convention — same as track selection's `handleContextMenu`).
- If the right-clicked playlist IS in `selectedPlaylists`, preserve the current multi-selection.
- Set `playlistContextMenuIndex` as before (for positioning).

**Modify the playlist context menu template (line ~2237, the `#playlist-context-menu` wa-popup):**
Update the menu items based on selection count:

When `selectedPlaylists.size > 1`:
- Hide "Rename" (can't rename multiple playlists at once)
- Show "Delete N Playlists" (with count) instead of "Delete Playlist"

When `selectedPlaylists.size <= 1` (single or none):
- Show "Rename" and "Delete Playlist" as before (existing behavior)

**Modify `onPlaylistContextAction` (line ~1627):**
For the `'delete'` case:
- If `selectedPlaylists.size > 1`, iterate over all selected playlist indices, call `DeletePlaylist(entry.summary.ID)` for each, then `refreshPlaylists()` once at the end. Clear `selectedPlaylists` after.
- If single selection (existing behavior), delete just that one playlist as before.

Implementation for batch delete:
```typescript
case 'delete': {
    if (this.selectedPlaylists.size > 1) {
        const ids = [...this.selectedPlaylists]
            .map(i => this.entries[i])
            .filter((e): e is PlaylistEntry => e !== undefined)
            .map(e => e.summary.ID);
        for (const id of ids) {
            await DeletePlaylist(id);
        }
        this.selectedPlaylists = new Set();
        await this.refreshPlaylists();
    } else {
        await this.handleDeletePlaylist(entry.summary.ID);
    }
    break;
}
```

Make `onPlaylistContextAction` async (it currently isn't — change signature to `private async onPlaylistContextAction(action: string)`).

**Clear playlist selection after any context action completes** (rename or delete).
  </action>
  <verify>
    <automated>cd frontend && npx tsc --noEmit --pretty 2>&1 | head -30</automated>
  </verify>
  <done>Right-clicking with multiple playlists selected shows "Delete N Playlists" (no rename). Clicking delete removes all selected playlists. Right-clicking an unselected playlist replaces the selection. Single playlist context menu still shows rename + delete as before.</done>
</task>

</tasks>

<verification>
1. `cd frontend && npx tsc --noEmit` — TypeScript compilation passes with zero errors
2. Manual: Open playlist view, Ctrl+Click two playlist headers → both highlight blue
3. Manual: Shift+Click a third → range fills in
4. Manual: Right-click → context menu shows "Delete 3 Playlists" (no rename option)
5. Manual: Click delete → all three are removed
6. Manual: Plain click a playlist header → expands/collapses normally, no selection artifacts
7. Manual: Select tracks within an expanded playlist → playlist-level selection clears
</verification>

<success_criteria>
- Playlist headers support Ctrl+Click toggle and Shift+Click range selection with blue highlight
- Playlist context menu adapts: shows "Delete N Playlists" for multi-select, "Rename" + "Delete Playlist" for single
- Batch delete works — all selected playlists are removed
- Plain click still expands/collapses playlists
- Track-level multi-select still works independently
- TypeScript compiles cleanly
</success_criteria>

<output>
After completion, create `.planning/quick/3-add-multi-select-to-playlist-view-with-c/3-SUMMARY.md`
</output>
