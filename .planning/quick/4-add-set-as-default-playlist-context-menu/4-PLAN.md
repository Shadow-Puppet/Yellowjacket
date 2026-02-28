---
phase: quick
plan: 4
type: execute
wave: 1
depends_on: []
files_modified:
  - frontend/src/components/playlist-view/playlist-view.ts
autonomous: true
requirements: []
must_haves:
  truths:
    - "Right-clicking a single playlist shows 'Set as Default Playlist' option"
    - "Clicking 'Set as Default Playlist' updates the default/favorites playlist to that playlist"
    - "Option does NOT appear when multiple playlists are selected"
  artifacts:
    - path: "frontend/src/components/playlist-view/playlist-view.ts"
      provides: "Set as Default Playlist context menu item + handler"
  key_links:
    - from: "playlist-view.ts context menu"
      to: "favCtrl.setDefaultPlaylist()"
      via: "onPlaylistContextAction('set-default')"
      pattern: "favCtrl\\.setDefaultPlaylist"
---

<objective>
Add a "Set as Default Playlist" option to the playlist-level context menu in the playlist view.

Purpose: Allow users to quickly set any playlist as the default (favorites) playlist via right-click, instead of navigating to Settings.
Output: Updated playlist-view.ts with new context menu item and handler.
</objective>

<execution_context>
@/home/caleb/.config/Claude/get-shit-done/workflows/execute-plan.md
@/home/caleb/.config/Claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@frontend/src/components/playlist-view/playlist-view.ts
@frontend/src/store/controllers/favorites-controller.ts
@frontend/src/store/favorites-store.ts

<interfaces>
<!-- Key contracts the executor needs — no codebase exploration required. -->

From playlist-view.ts (already instantiated):
```typescript
private favCtrl = new FavoritesController(this);
```

From favorites-controller.ts:
```typescript
async setDefaultPlaylist(id: number): Promise<void>;
get playlistId(): number;  // current default playlist ID
```

Playlist context menu handler pattern (line ~1707):
```typescript
private async onPlaylistContextAction(action: string) {
    const index = this.playlistContextMenuIndex;
    const entry = this.entries[index];
    if (!entry) return;
    switch (action) {
        case 'rename': ...
        case 'delete': ...
    }
    // cleanup at end
    this.selectedPlaylists = new Set();
    this.lastSelectedPlaylistIndex = null;
    this.closePlaylistContextMenu();
}
```

Playlist entry shape:
```typescript
interface PlaylistEntry {
    summary: playlist.Summary;  // .ID: number, .Name: string
    expanded: boolean;
    tracks: playlist.Track[];
}
```

Single-select guard pattern (line ~2341):
```typescript
${this.selectedPlaylists.size <= 1 ? html`...single-select-only items...` : nothing}
```
</interfaces>
</context>

<tasks>

<task type="auto">
  <name>Task 1: Add "Set as Default Playlist" context menu item and handler</name>
  <files>frontend/src/components/playlist-view/playlist-view.ts</files>
  <action>
Two changes in playlist-view.ts:

1. **Add handler case** in `onPlaylistContextAction()` (around line 1716, inside the switch statement, after the `'rename'` case and before `'delete'`):

```typescript
case 'set-default':
    void this.favCtrl
        .setDefaultPlaylist(entry.summary.ID)
        .catch((err: unknown) => {
            console.error(
                'Failed to set default playlist:',
                err,
            );
        });
    break;
```

This follows the exact same pattern used in config-page.ts (line ~812).

2. **Add menu item** in the playlist context menu template (around line 2341). Insert a new `wa-dropdown-item` AFTER the existing Rename item but still inside the `this.selectedPlaylists.size <= 1` guard block. The Rename item block currently ends at line ~2356 with `: nothing}`. Restructure so that both Rename AND Set as Default are inside the single-select guard:

```html
${this.selectedPlaylists.size <= 1
    ? html`
          <wa-dropdown-item
              @click=${() =>
                  void this.onPlaylistContextAction('rename')}
          >
              <wa-icon slot="icon" name="pen"></wa-icon>
              Rename
          </wa-dropdown-item>
          <wa-dropdown-item
              @click=${() =>
                  void this.onPlaylistContextAction('set-default')}
          >
              <wa-icon slot="icon" name="star"></wa-icon>
              Set as Default Playlist
          </wa-dropdown-item>
      `
    : nothing}
```

Use the "star" icon name since this relates to the favorites/default playlist concept and matches the icon style option in settings.

Do NOT add any new imports — `FavoritesController` is already imported and instantiated as `this.favCtrl`.
  </action>
  <verify>
    <automated>cd frontend && npx tsc --noEmit --pretty 2>&1 | head -30</automated>
  </verify>
  <done>
    - Right-clicking a single playlist in the playlist view shows "Set as Default Playlist" option with a star icon
    - Clicking it calls favCtrl.setDefaultPlaylist() with the playlist's ID
    - The option does NOT appear when multiple playlists are selected (same guard as Rename)
    - TypeScript compiles without errors
  </done>
</task>

</tasks>

<verification>
1. `cd frontend && npx tsc --noEmit` — TypeScript compilation passes
2. Manual: Right-click a single playlist → context menu shows Rename, Set as Default Playlist, Delete
3. Manual: Select multiple playlists → right-click → context menu shows only Delete (no Rename, no Set as Default)
4. Manual: Click "Set as Default Playlist" → verify in Settings that the default playlist updated
</verification>

<success_criteria>
- Single playlist right-click menu shows "Set as Default Playlist" between Rename and Delete
- Multi-select right-click menu does NOT show the option
- Clicking the option successfully changes the default/favorites playlist
- No TypeScript compilation errors
</success_criteria>

<output>
After completion, create `.planning/quick/4-add-set-as-default-playlist-context-menu/4-SUMMARY.md`
</output>
