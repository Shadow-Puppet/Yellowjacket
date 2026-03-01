---
phase: quick-5
plan: 1
type: execute
wave: 1
depends_on: []
files_modified:
  - backend/playlist/playlist.go
  - frontend/wailsjs/go/models.ts
  - frontend/src/components/playlist-view/playlist-view.ts
autonomous: true
requirements: [QUICK-5]
must_haves:
  truths:
    - "User sees a sort dropdown in the playlist view header"
    - "User can sort playlists by name (A-Z / Z-A)"
    - "User can sort playlists by date created"
    - "User can sort playlists by last modified (recent)"
    - "User can sort playlists by number of tracks"
    - "User can toggle ascending/descending direction"
    - "Sort preference persists across view switches"
    - "Default sort is 'Recent' (updated_at DESC) matching current DB order"
  artifacts:
    - path: "backend/playlist/playlist.go"
      provides: "Summary struct with CreatedAt and UpdatedAt fields"
    - path: "frontend/wailsjs/go/models.ts"
      provides: "TypeScript Summary class with CreatedAt and UpdatedAt"
    - path: "frontend/src/components/playlist-view/playlist-view.ts"
      provides: "Sort dropdown UI and client-side sorting logic"
  key_links:
    - from: "backend/playlist/playlist.go"
      to: "frontend/wailsjs/go/models.ts"
      via: "Wails bindings generation"
      pattern: "Summary.*CreatedAt.*UpdatedAt"
    - from: "frontend/src/components/playlist-view/playlist-view.ts"
      to: "playlist.Summary"
      via: "client-side sort using CreatedAt/UpdatedAt/Name/tracks.length"
      pattern: "sortEntries|sortField"
---

<objective>
Add a "sort" dropdown to the playlist view allowing users to sort playlists by name, date created, last modified, and number of tracks.

Purpose: Currently playlists are ordered by `updated_at DESC` from the database with no user control. Users need to organize playlists by different criteria.

Output: Sort dropdown in playlist header, client-side sorting with direction toggle, persisted preference via localStorage.
</objective>

<execution_context>
@/home/caleb/.config/Claude/get-shit-done/workflows/execute-plan.md
@/home/caleb/.config/Claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@frontend/src/components/playlist-view/playlist-view.ts — The main playlist view component (2798 lines). Sort dropdown goes in the header area.
@frontend/src/components/track-list/track-list.ts — Has an existing sort toolbar pattern to replicate (lines 729-830 for CSS, 1599-1696 for render methods, 1358-1510 for sort logic).
@frontend/src/store/playlist-store.ts — Playlist data store, provides `playlist.WithTracks[]`.
@frontend/src/store/controllers/playlist-controller.ts — Controller bridging store to component.
@backend/playlist/playlist.go — Go service; `Summary` struct (lines 43-47) needs `CreatedAt`/`UpdatedAt`. `GetAllPlaylistsWithTracks` (line 173) and `GetAllPlaylists` (line 147) construct Summary objects that need updating.
@backend/database/sql/sqlcgen/models.go — Sqlc model: `Playlist` struct already has `CreatedAt`/`UpdatedAt` fields (lines 67-72).
@frontend/wailsjs/go/models.ts — Auto-generated TypeScript models; `playlist.Summary` class (lines 305-318) will need `CreatedAt`/`UpdatedAt`.
@backend/database/sql/schemas/playlists.sql — Schema: `created_at` and `updated_at` columns already exist.

<interfaces>
<!-- Backend types the executor needs -->
From backend/playlist/playlist.go:
```go
type Summary struct {
	ID   int64  `json:"ID"`
	Name string `json:"Name"`
}

type WithTracks struct {
	Summary Summary `json:"Summary"`
	Tracks  []Track `json:"Tracks"`
}
```

From backend/database/sql/sqlcgen/models.go:
```go
type Playlist struct {
	ID        int64
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}
```

<!-- Frontend types -->
From frontend/wailsjs/go/models.ts:
```typescript
export class Summary {
    ID: number;
    Name: string;
    // CreatedAt and UpdatedAt NOT present yet — must be added
}
```

<!-- Existing sort UI pattern from track-list.ts -->
Sort toolbar CSS classes: .sort-toolbar, .sort-anchor, .sort-label, .sort-dir-btn, .sort-dropdown-panel, .active-sort, #sort-dropdown
Sort state: sortField (string|null), sortDirection ('asc'|'desc'), sortDropdownOpen (boolean)
localStorage keys pattern: 'track-list-sort-field', 'track-list-sort-direction'
</interfaces>
</context>

<tasks>

<task type="auto">
  <name>Task 1: Add CreatedAt/UpdatedAt to playlist Summary struct and regenerate bindings</name>
  <files>
    backend/playlist/playlist.go
    backend/playlist/favorites.go
    frontend/wailsjs/go/models.ts
  </files>
  <action>
1. In `backend/playlist/playlist.go`, add `CreatedAt` and `UpdatedAt` fields to the `Summary` struct:

```go
type Summary struct {
	ID        int64  `json:"ID"`
	Name      string `json:"Name"`
	CreatedAt string `json:"CreatedAt"`
	UpdatedAt string `json:"UpdatedAt"`
}
```

Use `string` type (not `time.Time`) since Wails serializes time values as strings and the frontend only needs them for comparison sorting. Format as RFC3339 using `p.CreatedAt.Format(time.RFC3339)` and `p.UpdatedAt.Format(time.RFC3339)`.

2. Update ALL locations where `Summary{}` is constructed to include the new fields. Search the file for `Summary{` — there are ~16 occurrences across playlist.go and favorites.go. The main patterns:

   - `GetAllPlaylists` (line 162): Has access to `p.CreatedAt` and `p.UpdatedAt` from the sqlc `Playlist` struct
   - `GetAllPlaylistsWithTracks` (line 239): Same — `p` is `sqlcgen.Playlist`
   - `CreatePlaylist` / `CreatePlaylistWithTracks` / `ImportSingle`: After creating, the sqlc `CreatePlaylist` returns `*` (RETURNING *) so the result has `CreatedAt`/`UpdatedAt`
   - `RenamePlaylist` (line 677): Doesn't have access to full row — use empty strings or re-query. Since this is an event payload (not display), empty strings are fine.
   - `GetOrCreateFavoritesPlaylist` in favorites.go (line 141): Has `pl` from `GetPlaylist` which returns full row

   For Summary constructions in event emission contexts (where CreatedAt/UpdatedAt aren't critical): populate with empty strings `""` — the frontend ignores timestamps on event payloads.
   For Summary constructions returned to the frontend for display: populate with formatted time strings.

3. Run `wails generate` to regenerate the TypeScript bindings in `frontend/wailsjs/go/models.ts`. The `Summary` class should now have `CreatedAt: string` and `UpdatedAt: string`.

4. If `wails generate` isn't available or fails, manually add the fields to `frontend/wailsjs/go/models.ts` in the `Summary` class:
   - Add `CreatedAt: string;` and `UpdatedAt: string;` as properties
   - Add them to the constructor: `this.CreatedAt = source["CreatedAt"];` and `this.UpdatedAt = source["UpdatedAt"];`
  </action>
  <verify>
    <automated>cd backend && go build ./... && go vet ./...</automated>
  </verify>
  <done>Summary struct includes CreatedAt/UpdatedAt strings, all construction sites updated, TypeScript bindings have the new fields, backend compiles cleanly.</done>
</task>

<task type="auto">
  <name>Task 2: Add sort dropdown UI and client-side sorting to playlist-view</name>
  <files>frontend/src/components/playlist-view/playlist-view.ts</files>
  <action>
Add a sort dropdown to the playlist view, replicating the existing sort toolbar pattern from track-list.ts but adapted for playlist-level sorting.

**1. Add sort state and constants:**

Before the class definition, add:
```typescript
type PlaylistSortField = 'name' | 'created' | 'modified' | 'tracks';
type SortDirection = 'asc' | 'desc';

const PLAYLIST_SORT_KEY = 'playlist-view-sort-field';
const PLAYLIST_SORT_DIR_KEY = 'playlist-view-sort-direction';

const SORT_OPTIONS: { id: PlaylistSortField; label: string }[] = [
    { id: 'modified', label: 'Recent' },
    { id: 'name', label: 'Name' },
    { id: 'created', label: 'Date Created' },
    { id: 'tracks', label: 'Track Count' },
];
```

Inside the class, add state properties:
```typescript
@state() private sortField: PlaylistSortField = 'modified';
@state() private sortDirection: SortDirection = 'desc';
@state() private sortDropdownOpen = false;

@query('#sort-dropdown')
private sortDropdownPopup!: WaPopup;
```

**2. Add sort CSS (inside the static styles array):**

Copy the sort toolbar styles from track-list.ts (`.sort-toolbar`, `.sort-anchor`, `.sort-anchor:hover`, `.sort-anchor .sort-label`, `.sort-dir-btn`, `.sort-dir-btn:hover`, `.sort-dropdown-panel`, `.sort-dropdown-panel wa-dropdown-item`, `.sort-dropdown-panel wa-dropdown-item:hover`, `.sort-dropdown-panel wa-dropdown-item.active-sort`, `#sort-dropdown`). These are lines 731-830 of track-list.ts. Copy them verbatim — same CSS custom properties are used.

**3. Add sort logic methods:**

```typescript
private restoreSortPreferences() {
    try {
        const field = localStorage.getItem(PLAYLIST_SORT_KEY);
        if (field && SORT_OPTIONS.some(o => o.id === field)) {
            this.sortField = field as PlaylistSortField;
        }
        const dir = localStorage.getItem(PLAYLIST_SORT_DIR_KEY);
        if (dir === 'asc' || dir === 'desc') {
            this.sortDirection = dir;
        }
    } catch { /* localStorage unavailable */ }
}

private saveSortPreferences() {
    try {
        localStorage.setItem(PLAYLIST_SORT_KEY, this.sortField);
        localStorage.setItem(PLAYLIST_SORT_DIR_KEY, this.sortDirection);
    } catch { /* localStorage unavailable */ }
}

private get sortedEntries(): PlaylistEntry[] {
    const entries = this.filteredEntries;
    const dir = this.sortDirection === 'asc' ? 1 : -1;

    return [...entries].sort((a, b) => {
        let cmp = 0;
        switch (this.sortField) {
            case 'name':
                cmp = a.summary.Name.localeCompare(b.summary.Name);
                break;
            case 'created':
                cmp = (a.summary.CreatedAt || '').localeCompare(b.summary.CreatedAt || '');
                break;
            case 'modified':
                cmp = (a.summary.UpdatedAt || '').localeCompare(b.summary.UpdatedAt || '');
                break;
            case 'tracks':
                cmp = a.tracks.length - b.tracks.length;
                break;
        }
        return cmp * dir;
    });
}
```

**4. Add dropdown open/close/select methods** (same pattern as track-list.ts):

- `toggleSortDropdown()`, `openSortDropdown()`, `closeSortDropdown()` — same pattern as track-list.ts lines 1456-1490
- `onSortDropdownSelect(field: PlaylistSortField)` — sets `this.sortField = field`, calls `saveSortPreferences()`, `closeSortDropdown()`
- `toggleSortDirection()` — flips direction, saves
- `sortDropdownCloseHandler` — mousedown listener to close when clicking outside (same pattern as track-list.ts lines 1492-1510)

**5. Register/unregister the mousedown close handler** in `connectedCallback` and `disconnectedCallback`:
- In `connectedCallback()`: add `document.addEventListener('mousedown', this.sortDropdownCloseHandler);`
- Also call `this.restoreSortPreferences();` in `connectedCallback()`
- In `disconnectedCallback()`: add `document.removeEventListener('mousedown', this.sortDropdownCloseHandler);`

**6. Add sort toolbar rendering** as a private method `renderSortToolbar()`:

```typescript
private renderSortToolbar() {
    const activeOption = SORT_OPTIONS.find(o => o.id === this.sortField);
    const label = activeOption?.label ?? 'Recent';
    const dirIcon = this.sortDirection === 'asc'
        ? 'arrow-up-short-wide'
        : 'arrow-down-wide-short';

    return html`
        <div class="sort-toolbar">
            <span>Sort:</span>
            <button class="sort-anchor"
                @click=${() => this.toggleSortDropdown()}
            >
                <span class="sort-label">${label}</span>
                <wa-icon name="chevron-down"></wa-icon>
            </button>
            <button class="sort-dir-btn"
                title="${this.sortDirection === 'asc' ? 'Ascending' : 'Descending'}"
                @click=${() => this.toggleSortDirection()}
            >
                <wa-icon name=${dirIcon}></wa-icon>
            </button>
        </div>
        ${this.renderSortDropdownPopup()}
    `;
}

private renderSortDropdownPopup() {
    return html`
        <wa-popup id="sort-dropdown"
            placement="bottom-start" flip shift
            .active=${this.sortDropdownOpen}
        >
            ${this.sortDropdownOpen ? html`
                <div class="sort-dropdown-panel">
                    ${SORT_OPTIONS.map(opt => html`
                        <wa-dropdown-item
                            class=${this.sortField === opt.id ? 'active-sort' : ''}
                            @click=${() => this.onSortDropdownSelect(opt.id)}
                        >
                            ${opt.label}
                        </wa-dropdown-item>
                    `)}
                </div>
            ` : nothing}
        </wa-popup>
    `;
}
```

**7. Wire sort toolbar into the render method:**

In the `render()` method, insert the sort toolbar between the header `</div>` and the search indicator / create form. Specifically, after the `importError` block (after line 2124), add:
```typescript
${this.renderSortToolbar()}
```

**8. Replace `filteredEntries` with `sortedEntries` in the rendering path:**

In `renderPlaylistList()`, change line 2459 from:
```typescript
const visible = this.filteredEntries;
```
to:
```typescript
const visible = this.sortedEntries;
```

Also update the `originalIndex` lookup on line 2488-2489. Since `sortedEntries` may reorder entries, `this.entries.indexOf(entry)` still works correctly since it finds the entry in the original `this.entries` array — the reference identity is preserved because `sortedEntries` spreads `filteredEntries` which filters `this.entries`. VERIFY this is the case. If `filteredEntries` creates new objects (it does NOT — it just filters), then `indexOf` will still work.

**IMPORTANT:** The direction button should ALWAYS be visible (unlike track-list which hides it when no sort is active), since playlist sort always has an active field (no "Default" option — "Recent" is the default).
  </action>
  <verify>
    <automated>cd frontend && npx tsc --noEmit</automated>
  </verify>
  <done>Playlist view has a sort toolbar below the header with four options (Recent, Name, Date Created, Track Count), a direction toggle button, dropdown opens/closes correctly, sort preference saved to localStorage, playlists reorder when sort changes. Default is "Recent" descending (matching current behavior).</done>
</task>

</tasks>

<verification>
1. `cd backend && go build ./... && go vet ./...` — backend compiles
2. `cd frontend && npx tsc --noEmit` — frontend type-checks
3. Manual: Open playlist view, verify sort dropdown appears, try each sort option, toggle direction, verify playlists reorder correctly
4. Manual: Switch away from playlist view and back — sort preference persists
</verification>

<success_criteria>
- Sort dropdown visible in playlist view header area
- Four sort options: Recent (default), Name, Date Created, Track Count
- Ascending/descending toggle works
- Playlists visually reorder when sort or direction changes
- Sort preference persists in localStorage across view switches
- Backend compiles, frontend type-checks
- Default sort (Recent, desc) matches the existing behavior (updated_at DESC from DB)
</success_criteria>

<output>
After completion, create `.planning/quick/5-add-sort-dropdown-to-playlist-view/5-SUMMARY.md`
</output>
