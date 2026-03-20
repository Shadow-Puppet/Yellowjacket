---
phase: quick-17
plan: 1
type: execute
wave: 1
depends_on: []
files_modified:
  - frontend/src/components/playlist-details/playlist-details.ts
  - frontend/src/components/playlist-view/playlist-view.ts
  - frontend/index.ts
autonomous: true
requirements: [QUICK-17]

must_haves:
  truths:
    - "Clicking a playlist in playlist-view navigates to a dedicated playlist-details subpage"
    - "Playlist-details shows a header with back button, playlist icon, playlist name, and track count"
    - "Playlist-details shows the full track list for that playlist with all existing interactions (click, dblclick, context menu, drag, phantom handling)"
    - "Back button in playlist-details navigates back to the playlist list"
    - "Playlist-view no longer expands/collapses playlists inline"
  artifacts:
    - path: "frontend/src/components/playlist-details/playlist-details.ts"
      provides: "Playlist detail subpage component"
    - path: "frontend/src/components/playlist-view/playlist-view.ts"
      provides: "Playlist list view (no more inline track expansion)"
    - path: "frontend/index.ts"
      provides: "Navigation routing for playlist-details"
  key_links:
    - from: "frontend/src/components/playlist-view/playlist-view.ts"
      to: "frontend/index.ts"
      via: "CustomEvent('navigate', { view: 'playlist-details', playlistId, playlistName })"
      pattern: "navigate.*playlist-details"
    - from: "frontend/src/components/playlist-details/playlist-details.ts"
      to: "frontend/index.ts"
      via: "CustomEvent('navigate', { view: 'playlists' })"
      pattern: "navigate.*playlists"
---

<objective>
Refactor the playlist view from an expand/collapse dropdown pattern to a subpage navigation pattern, matching how genre-details and artist-details work. Clicking a playlist navigates to a dedicated playlist-details page with a header (back button + playlist icon + title + track count) and the full track list below.

Purpose: Consistent navigation UX across playlists, genres, and artists — all use the subpage pattern.
Output: New `playlist-details` component, simplified `playlist-view`, updated navigation routing.
</objective>

<execution_context>
@/home/caleb/.config/Claude/get-shit-done/workflows/execute-plan.md
@/home/caleb/.config/Claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@frontend/src/components/genre-details/genre-details.ts (reference pattern — header + back button + content)
@frontend/src/components/genres-view/genres-view.ts (reference pattern — click navigates to details)
@frontend/src/components/playlist-view/playlist-view.ts (source — refactor this)
@frontend/index.ts (navigation routing — add playlist-details case)
@frontend/wailsjs/go/playlist/Service.d.ts (available backend APIs)
@frontend/src/store/playlist-store.ts (playlist data store)
@frontend/src/store/search-store.ts (search store — update SEARCHABLE_VIEWS if needed)

<interfaces>
<!-- Navigation pattern from genre-details (the pattern to follow): -->

From frontend/src/components/genre-details/genre-details.ts:
```typescript
// Header: back-button → avatar → genre-info (title + track count)
// Content: <track-list .externalTracks=${this.tracks}>
// Back navigation: dispatches CustomEvent('navigate', { view: 'genres' })
```

From frontend/index.ts — navigation handler:
```typescript
// Genre details routing (line 73-82):
case 'genre-details': {
    const { genreName } = (e as CustomEvent).detail;
    const genreEl = document.createElement('genre-details');
    genreEl.setAttribute('genre-name', genreName);
    mainContent.innerHTML = '';
    mainContent.appendChild(genreEl);
    break;
}
```

From frontend/wailsjs/go/playlist/Service.d.ts:
```typescript
export function GetPlaylistTracks(arg1:number):Promise<Array<playlist.Track>>;
export function GetAllPlaylistsWithTracks():Promise<Array<playlist.WithTracks>>;
export function AddTracksToPlaylist(arg1:number,arg2:Array<string>):Promise<void>;
export function RemoveTracksFromPlaylist(arg1:number,arg2:Array<number>):Promise<void>;
export function RenamePlaylist(arg1:number,arg2:string):Promise<void>;
export function DeletePlaylist(arg1:number):Promise<void>;
export function RemovePhantomTracks(arg1:number,arg2:Array<string>):Promise<void>;
export function FindDuplicateTracksInPlaylist(arg1:number,arg2:Array<string>):Promise<playlist.DuplicateCheckResult>;
```

From frontend/src/components/playlist-view/playlist-view.ts:
```typescript
// PlaylistEntry = { summary: playlist.Summary, expanded: boolean, tracks: playlist.Track[] }
// SelectionHost + ContextMenuHost interfaces
// Uses: PlayerController, PlaylistController, SearchController, SelectionController, ContextMenuController, FavoritesController
// Track interactions: click (select), dblclick (play), contextmenu, drag source, phantom handling
// Playlist-level: context menu (rename, delete, set-default), drag target, multi-select, sort
```

From frontend/wailsjs/go/models.ts:
```typescript
// playlist.Summary: { ID: number, Name: string, CreatedAt: string, UpdatedAt: string }
// playlist.Track: { ID, Position, FilePath, Title, Artist, Album, CoverArtPath, CoverArtSmall, CoverArtMedium, CoverArtLarge, Duration, Phantom }
// playlist.WithTracks: { Summary: Summary, Tracks: Track[] }
```
</interfaces>
</context>

<tasks>

<task type="auto">
  <name>Task 1: Create playlist-details component</name>
  <files>frontend/src/components/playlist-details/playlist-details.ts, frontend/index.ts</files>
  <action>
Create `frontend/src/components/playlist-details/playlist-details.ts` — a new LitElement component modeled closely on `genre-details.ts` but with all the track interaction capabilities currently in `playlist-view`'s `renderPlaylistBody`.

**Component structure:**

1. **Properties:** `playlistId` (Number attribute), `playlistName` (String attribute).

2. **Header** (same layout as genre-details):
   - Back button (dispatches `navigate` with `{ view: 'playlists' }`)
   - Playlist avatar: 80×80 rounded square with `list` icon (wa-icon name="list") centered inside, using the same gradient background as genre-details' `.genre-avatar`
   - Playlist info: h1 title + track count subtitle

3. **Content area:** Render playlist tracks using the same track-item rendering pattern currently in `playlist-view`'s `renderPlaylistBody` method. This includes:
   - "Play All" button at the top
   - Track list with `<track-info>` for normal tracks and phantom row rendering for phantom tracks
   - All track interactions: click (selection), dblclick (play from playlist), right-click context menu, drag source
   - Phantom track interactions: click (select), right-click context menu, locate button, remove button
   - Active track highlighting (uses PlayerController)
   - Track selection (uses SelectionController — this component is the SelectionHost)

4. **Context menus:** Move the track-level context menu (play, add to queue, play next, remove from playlist, add to playlist submenu, favorites toggle, track details) and the playlist submenu from `playlist-view` into this component. Include `<track-details>`, `<phantom-resolver>`, and `<duplicate-tracks-dialog>` elements.

5. **Data loading:**
   - On connectedCallback, call `GetPlaylistTracks(this.playlistId)` to load tracks
   - Listen for `Events.PlaylistTracksChanged` and `Events.PlaylistDeleted` to refresh/navigate back
   - Implement a `refreshTracks()` method that re-fetches tracks after mutations

6. **Drag target:** Support dropping tracks onto this playlist (from queue, track-list, or other playlists). Use the same `AddTracksToPlaylist` + `FindDuplicateTracksInPlaylist` flow currently in `playlist-view`'s `onPlaylistDrop`.

7. **Drag source:** Support dragging tracks from this playlist to queue or other destinations. Same pattern as `playlist-view`'s `onTrackDragStart`/`onTrackDragEnd`.

8. **Search integration:** Wire up SearchController. When search term is active, filter visible tracks (same logic as `playlist-view`'s `getVisibleTracks`). Add `playlist-details` to `SEARCHABLE_VIEWS` in `search-store.ts`.

9. **Styles:** Use `designTokens` mixin. Copy the relevant styles from `genre-details` for the header section (`.back-button`, avatar, title, track-count). Copy track-item styles (`.track-item`, `.track-item.active`, `.track-item.selected`, `.phantom-row`, etc.) and context menu styles from `playlist-view`. Include `.play-all-button` styles.

10. **Select-all support:** Listen for `shortcut:select-all` event (same as playlist-view does).

**In `frontend/index.ts`:**
- Add `import '@components/playlist-details/playlist-details.ts';` at the top with the other imports
- Add a `case 'playlist-details':` block in the navigation switch, following the same pattern as `genre-details`:
  ```
  case 'playlist-details': {
      const { playlistId, playlistName } = (e as CustomEvent).detail;
      const el = document.createElement('playlist-details');
      el.setAttribute('playlist-id', String(playlistId));
      el.setAttribute('playlist-name', playlistName);
      mainContent.innerHTML = '';
      mainContent.appendChild(el);
      break;
  }
  ```
  </action>
  <verify>
`npx tsc --noEmit` passes. The new component file exists with @customElement('playlist-details'), has back button, header, track rendering, context menus, drag support, and the index.ts routing case exists.
  </verify>
  <done>playlist-details component renders a header with back button + playlist icon + title + track count, displays playlist tracks with all interactions (select, play, context menu, drag, phantom handling), and navigating to `playlist-details` view works via index.ts routing.</done>
</task>

<task type="auto">
  <name>Task 2: Simplify playlist-view to navigate instead of expand</name>
  <files>frontend/src/components/playlist-view/playlist-view.ts</files>
  <action>
Refactor `playlist-view.ts` to remove all inline track expansion and instead navigate to `playlist-details` on click. This is a significant simplification:

**Remove from playlist-view:**
1. The `expanded` field from `PlaylistEntry` interface (no longer needed — set type to just `{ summary: playlist.Summary; tracks: playlist.Track[] }`, keep tracks for count display and drag-drop track resolution)
2. The `renderPlaylistBody` method entirely
3. The chevron icon in `renderPlaylistItem` (no more expand/collapse)
4. All track-level interaction handlers: `handleTrackClick`, `handleTrackDblClick`, `handleTrackContextMenu`, `onTrackDragStart`, `onTrackDragEnd`, `ensureSelectionScope`, `getSelectedTrackIDs`, `getSelectedFilePaths`, `removeSelectedTracks`, `removeSelectedPhantoms`, `handlePhantomClick`, `handlePhantomContextMenu`, `openPhantomResolver`, `removePhantomTrack`
5. The `SelectionController` (no more track selection in the list view) and `SelectionHost` implementation (`getItemKey`, `getItemCount`, `onSelectionChanged`)
6. The `ContextMenuController` and `ContextMenuHost` implementation, and all track-level context menu rendering (the `#context-menu` popup, `#playlist-submenu` popup)
7. Remove `activePlaylistIndex` state
8. The `<track-details>`, `<phantom-resolver>`, and `<duplicate-tracks-dialog>` elements from the render method (they move to playlist-details)
9. The `isActiveTrack`, `isPhantomSelection`, `resolvePlaylistCoverArt`, `openTrackDetails` methods
10. The `getVisibleTracks` and `filteredEntries` search-related track filtering (search on the list page can just filter playlist names)
11. Remove `SelectionHost` and `ContextMenuHost` from the class declaration
12. Remove imports that are no longer needed: `SelectionController`, track-info, track-details, phantom-resolver, duplicate-tracks-dialog, etc.
13. Remove the `clearSelectionHandler` and `handleSelectAll` handlers since track selection is gone
14. Remove the search-triggered auto-expand logic in `updated()` (the part that sets `expanded: true` based on track matches)

**Keep in playlist-view:**
1. Playlist list rendering (the grid of playlist items with name + track count)
2. Playlist-level context menu (rename, delete, set-default) — the `#playlist-context-menu` popup
3. Playlist-level selection (Ctrl/Shift+Click for multi-select of playlists for bulk delete)
4. Create playlist functionality (new playlist button, create form)
5. Import playlist button
6. Sort toolbar (sort by name, created, modified, tracks)
7. Drag-and-drop TARGET: dropping tracks onto a playlist item to add them (keep `onPlaylistDragOver`, `onPlaylistDragLeave`, `onPlaylistDrop`). Also keep empty zone drop and new-button drop.
8. Search filtering (but simplified to just filter by playlist name, not tracks)
9. Scroll position persistence
10. PlaylistController for data loading

**Modify `handlePlaylistHeaderClick`:**
- Remove the expand/collapse toggle behavior for plain clicks
- Instead, plain click dispatches navigation:
  ```typescript
  this.dispatchEvent(
      new CustomEvent('navigate', {
          bubbles: true,
          composed: true,
          detail: {
              view: 'playlist-details',
              playlistId: entry.summary.ID,
              playlistName: entry.summary.Name,
          },
      }),
  );
  ```
- Keep Ctrl+Click and Shift+Click for playlist multi-selection (same as before)

**Modify `renderPlaylistItem`:**
- Remove the chevron icon
- Remove the `${entry.expanded ? this.renderPlaylistBody(...) : nothing}` conditional
- The playlist-header row now just shows: playlist-icon (if favorites) + playlist-name + track-count
- Keep the right-click context menu handler on the header
- Keep the drag-over styling for drop targets

**Simplify `filteredEntries`:**
- Only filter by playlist name (remove the track title/artist matching since tracks aren't shown inline)

**Remove styles that are no longer needed:**
- `.chevron`, `.chevron.expanded`
- `.playlist-body`
- `.playlist-actions`, `.play-all-button`
- `.track-item` and all its variants (`.active`, `.selected`, `.phantom`)
- `.phantom-row`, `.phantom-caution`, `.phantom-path`, `.phantom-actions`, `.phantom-icon-btn`
- `.tracks-empty`
- Context menu styles for track-level menus

**Update `loadPlaylists`/`refreshPlaylists`:**
- Remove `expanded` from the mapped entries

**Remove the `contextMenuStyles` import** if no longer needed (check if playlist-level context menu uses it — it does, so keep it).

**Note:** The `PlaylistController`, `PlayerController`, `SearchController`, `FavoritesController` may still be needed. Keep `FavoritesController` for the favorites icon display. Remove `PlayerController` since active track highlighting is gone from the list. Keep `SearchController` for name-based search. Keep `PlaylistController` for data.
  </action>
  <verify>
`npx tsc --noEmit` passes. Verify playlist-view no longer has any `expanded`, `renderPlaylistBody`, `handleTrackClick`, `SelectionController`, or chevron references.
  </verify>
  <done>Playlist-view shows a clean list of playlists without inline track expansion. Plain-clicking a playlist navigates to playlist-details. Ctrl/Shift+Click still multi-selects. Right-click context menu still works for rename/delete/set-default. Drag-drop onto playlists still works. Create/import still works. Sort still works. Search filters by playlist name only.</done>
</task>

</tasks>

<verification>
1. `npx tsc --noEmit` — TypeScript compilation passes with no errors
2. Navigate to Playlists view — shows list of playlists without chevrons or expandable sections
3. Click a playlist — navigates to playlist-details with header (back button, icon, title, track count) and track list
4. Click back button — returns to playlist list
5. Double-click a track in playlist-details — plays the track
6. Right-click track in playlist-details — context menu works (play, queue, remove, etc.)
7. Right-click playlist in list — context menu works (rename, delete, set-default)
8. Ctrl+Click playlists in list — multi-select works for bulk delete
9. Drag tracks from another view onto a playlist in the list — adds tracks
10. Drag tracks from playlist-details to queue — works
11. Create new playlist — form still works
12. Import playlist — button still works
13. Sort playlists — toolbar still works
14. Search — filters playlist names in list view, filters tracks in detail view
</verification>

<success_criteria>
- Playlist navigation matches genre/artist pattern: list view → click → detail subpage → back button
- No inline track expansion/collapse in playlist-view
- All existing track interactions work in playlist-details (play, select, context menu, drag, phantom handling)
- All existing playlist management works in playlist-view (create, import, rename, delete, sort, drag-drop target)
- TypeScript compiles without errors
</success_criteria>

<output>
After completion, create `.planning/quick/17-refactor-playlist-view-to-use-subpages-l/17-SUMMARY.md`
</output>
