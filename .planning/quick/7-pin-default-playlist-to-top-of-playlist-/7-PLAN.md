---
phase: quick-7
plan: 1
type: execute
wave: 1
depends_on: []
files_modified:
  - backend/favorites/config.go
  - backend/config/config.go
  - frontend/src/store/favorites-store.ts
  - frontend/src/store/controllers/favorites-controller.ts
  - frontend/src/components/playlist-view/playlist-view.ts
  - frontend/src/components/config-page/config-page.ts
autonomous: true
requirements: [PIN-DEFAULT-01]

must_haves:
  truths:
    - "When pin is enabled, the default/favorites playlist always appears first in the playlist list regardless of sort field or direction"
    - "When pin is disabled, the default playlist sorts normally with all other playlists"
    - "The pin setting is toggleable from the config/settings page under the Favorites section"
    - "The pin preference persists across app restarts via config.toml"
  artifacts:
    - path: "backend/favorites/config.go"
      provides: "PinDefault bool field on Config struct"
      contains: "PinDefault"
    - path: "backend/config/config.go"
      provides: "GetPinDefaultPlaylist and SetPinDefaultPlaylist methods"
      exports: ["GetPinDefaultPlaylist", "SetPinDefaultPlaylist"]
    - path: "frontend/src/store/favorites-store.ts"
      provides: "pinDefault state, getter, setter, and event reactivity"
    - path: "frontend/src/components/playlist-view/playlist-view.ts"
      provides: "sortedEntries getter pins default playlist to top when enabled"
  key_links:
    - from: "frontend/src/components/playlist-view/playlist-view.ts"
      to: "frontend/src/store/controllers/favorites-controller.ts"
      via: "favCtrl.pinDefault and favCtrl.playlistId in sortedEntries"
      pattern: "this\\.favCtrl\\.pinDefault"
    - from: "frontend/src/store/favorites-store.ts"
      to: "backend/config/config.go"
      via: "GetPinDefaultPlaylist/SetPinDefaultPlaylist Wails bindings"
      pattern: "(Get|Set)PinDefaultPlaylist"
    - from: "backend/config/config.go"
      to: "frontend/src/store/favorites-store.ts"
      via: "FavoritesConfigChanged event includes PinDefault field"
      pattern: "PinDefault"
---

<objective>
Pin the default/favorites playlist to the top of the playlist view regardless of sort order, controlled by a toggleable config setting.

Purpose: Users who rely on a favorites playlist want instant access without scrolling/sorting to find it.
Output: Full-stack feature — config field, backend getter/setter, frontend store/controller, sort logic, and settings toggle.
</objective>

<execution_context>
@/home/caleb/.config/Claude/get-shit-done/workflows/execute-plan.md
@/home/caleb/.config/Claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@backend/favorites/config.go
@backend/config/config.go
@frontend/src/store/favorites-store.ts
@frontend/src/store/controllers/favorites-controller.ts
@frontend/src/components/playlist-view/playlist-view.ts
@frontend/src/components/config-page/config-page.ts

<interfaces>
<!-- Key types and contracts the executor needs -->

From backend/favorites/config.go:
```go
type Config struct {
    PlaylistID int64     `toml:"PlaylistID"`
    IconStyle  IconStyle `toml:"IconStyle"`
}
```

From backend/config/config.go:
```go
// Pattern for getter/setter — follow GetFavoritesPlaylistID / SetFavoritesPlaylistID exactly
func (c *Config) GetFavoritesPlaylistID() int64 { ... }
func (c *Config) SetFavoritesPlaylistID(id int64) error { ... }
func (c *Config) emitFavoritesChanged() {
    runtime.EventsEmit(c.ctx, events.FavoritesConfigChanged, map[string]any{
        "PlaylistID": c.Favorites.PlaylistID,
        "IconStyle":  string(c.Favorites.IconStyle),
    })
}
```

From frontend/src/store/favorites-store.ts:
```typescript
// Event handler in constructor:
EventsOn(Events.FavoritesConfigChanged, (data: {
    PlaylistID: number;
    IconStyle: string;
}) => { ... });

// loadConfig pattern:
private async loadConfig(): Promise<void> {
    const [id, style] = await Promise.all([
        GetFavoritesPlaylistID(),
        GetFavoritesIconStyle(),
    ]);
    ...
}
```

From frontend/src/components/playlist-view/playlist-view.ts:
```typescript
private get sortedEntries(): PlaylistEntry[] {
    const entries = this.filteredEntries;
    const dir = this.sortDirection === 'asc' ? 1 : -1;
    return [...entries].sort((a, b) => { ... });
}
```

From frontend/src/components/config-page/config-page.ts:
```typescript
// Favorites section uses config-field with type: 'select'
// Pattern for toggle: use type: 'toggle' with boolean value
private renderFavoritesSection() { ... }
```
</interfaces>
</context>

<tasks>

<task type="auto">
  <name>Task 1: Add PinDefault to backend config and expose getter/setter</name>
  <files>
    backend/favorites/config.go
    backend/config/config.go
  </files>
  <action>
1. In `backend/favorites/config.go`, add `PinDefault bool` field to the `Config` struct with TOML tag `"PinDefault"`. Default should be `true` (pin enabled by default). Update `ApplyDefaults()` — since Go zero-value for bool is false, add a separate mechanism: add a `pinDefaultSet bool` unexported field (no toml tag) to track if PinDefault was explicitly set, OR simpler: just document that the default is applied in `config.go`'s `applyDefaults`. Actually, simplest approach: since `bool` zero-value is `false`, and we want default `true`, handle this in `config.go`'s `applyDefaults()` method by setting `c.Favorites.PinDefault = true` when initializing a new Favorites config. No validation needed for a bool field.

2. In `backend/config/config.go`:
   - Add `GetPinDefaultPlaylist() bool` method following the exact pattern of `GetFavoritesPlaylistID()`:
     ```go
     func (c *Config) GetPinDefaultPlaylist() bool {
         if c.Favorites == nil {
             return true // default: pinned
         }
         return c.Favorites.PinDefault
     }
     ```
   - Add `SetPinDefaultPlaylist(pin bool) error` method following the pattern of `SetFavoritesPlaylistID()`:
     - Ensure `c.Favorites` is initialized (same nil guard pattern)
     - Set `c.Favorites.PinDefault = pin`
     - Call `c.Save()`, return error if save fails
     - Call `c.emitFavoritesChanged()`
     - Log the change
   - Update `emitFavoritesChanged()` to include `"PinDefault": c.Favorites.PinDefault` in the event payload map
   - In the `applyDefaults()` method, ensure when creating a new `favorites.Config{}`, `PinDefault` is set to `true`

NOTE: The Wails bindings (`frontend/wailsjs/go/config/Config.js` and `.d.ts`) are auto-generated by `wails generate module`. Run `wails generate module` after making Go changes, or if not available, manually add the binding stubs to match the pattern of existing bindings.
  </action>
  <verify>
    Run `go build ./...` from the backend directory to verify compilation. Grep for `PinDefault` in `backend/` to confirm it appears in both files.
  </verify>
  <done>
    - `favorites.Config` has `PinDefault bool` field with TOML tag
    - `config.Config` has `GetPinDefaultPlaylist()` and `SetPinDefaultPlaylist()` methods
    - `emitFavoritesChanged` includes `PinDefault` in event payload
    - Default value is `true` (pin enabled)
    - Code compiles without errors
  </done>
</task>

<task type="auto">
  <name>Task 2: Wire frontend store, controller, playlist-view sort logic, and config page toggle</name>
  <files>
    frontend/src/store/favorites-store.ts
    frontend/src/store/controllers/favorites-controller.ts
    frontend/src/components/playlist-view/playlist-view.ts
    frontend/src/components/config-page/config-page.ts
    frontend/wailsjs/go/config/Config.js
    frontend/wailsjs/go/config/Config.d.ts
  </files>
  <action>
1. **Wails bindings** — Add `GetPinDefaultPlaylist` and `SetPinDefaultPlaylist` to `frontend/wailsjs/go/config/Config.js` and `.d.ts` following the exact pattern of the existing exports (e.g. `GetFavoritesPlaylistID`/`SetFavoritesPlaylistID`):
   - In `.d.ts`: `export function GetPinDefaultPlaylist():Promise<boolean>;` and `export function SetPinDefaultPlaylist(arg1:boolean):Promise<void>;`
   - In `.js`: Follow the exact `window['go']['config']['Config']['MethodName']` pattern used by other exports

2. **favorites-store.ts**:
   - Import `GetPinDefaultPlaylist` and `SetPinDefaultPlaylist` from `@go/config/Config`
   - Add `private pinDefault = true;` field (default true)
   - Add `getPinDefault(): boolean` getter
   - Add `async setPinDefault(pin: boolean): Promise<void>` action (same pattern as `setIconStyle`)
   - In `loadConfig()`: add `GetPinDefaultPlaylist()` to the `Promise.all` call, store result in `this.pinDefault`
   - In the `FavoritesConfigChanged` event handler: read `data.PinDefault` (as `boolean`) and store in `this.pinDefault`, then notify

3. **favorites-controller.ts**:
   - Add `get pinDefault(): boolean` getter that delegates to `favoritesStore.getPinDefault()`
   - Add `async setPinDefault(pin: boolean): Promise<void>` that delegates to `favoritesStore.setPinDefault(pin)`

4. **playlist-view.ts** — Update `sortedEntries` getter to pin default playlist when enabled:
   ```typescript
   private get sortedEntries(): PlaylistEntry[] {
       const entries = this.filteredEntries;
       const dir = this.sortDirection === 'asc' ? 1 : -1;

       const sorted = [...entries].sort((a, b) => {
           // Pin default playlist to top when enabled
           if (this.favCtrl.pinDefault) {
               const aIsDefault = a.summary.ID === this.favCtrl.playlistId;
               const bIsDefault = b.summary.ID === this.favCtrl.playlistId;
               if (aIsDefault && !bIsDefault) return -1;
               if (!aIsDefault && bIsDefault) return 1;
           }

           let cmp = 0;
           switch (this.sortField) {
               // ... existing sort cases unchanged
           }
           return cmp * dir;
       });

       return sorted;
   }
   ```

5. **config-page.ts** — Add a toggle in `renderFavoritesSection()` AFTER the existing Icon Style field:
   ```typescript
   <config-field
       .schema=${{
           key: 'pinDefaultPlaylist',
           label: 'Pin to Top',
           description: 'Always show the default playlist first, regardless of sort order.',
           type: 'toggle' as const,
       }}
       .value=${this.favCtrl.pinDefault}
       @config-change=${this.handlePinDefaultChange}
   ></config-field>
   ```
   - Add handler `private handlePinDefaultChange`:
     ```typescript
     private handlePinDefaultChange = (
         e: CustomEvent<ConfigFieldChangeEvent>,
     ): void => {
         const pin = Boolean(e.detail.value);
         this.favCtrl
             .setPinDefault(pin)
             .catch((err: unknown) => {
                 console.error('Failed to set pin default:', err);
             });
     };
     ```

IMPORTANT: Check if `config-field` supports `type: 'toggle'`. If not, check what boolean toggle type it supports (could be `'switch'` or `'checkbox'`). Look at the config-field component to determine the correct type string. If `toggle` isn't supported, use whatever boolean field type the component supports.
  </action>
  <verify>
    Run `npm run build` (or the project's frontend build command) from the frontend directory to verify TypeScript compilation. Visually verify by launching the app that: (1) The favorites playlist appears at the top of the playlist list regardless of sort, (2) The setting toggle appears in Settings > Favorites, (3) Disabling the toggle causes the favorites playlist to sort normally.
  </verify>
  <done>
    - Favorites store exposes `pinDefault` state with getter/setter
    - FavoritesController exposes `pinDefault` getter and `setPinDefault` action
    - `sortedEntries` in playlist-view pins default playlist to index 0 when `pinDefault` is true
    - Config page shows "Pin to Top" toggle in Favorites section
    - Toggling the setting immediately updates the playlist view (reactive via store subscription)
    - Setting persists across app restarts (saved to config.toml via backend)
    - Frontend builds without TypeScript errors
  </done>
</task>

</tasks>

<verification>
1. `go build ./...` passes (backend compiles)
2. Frontend build passes (TypeScript compiles)
3. App launches; default playlist appears pinned to top regardless of sort field/direction
4. Settings > Favorites shows "Pin to Top" toggle (default: on)
5. Disabling the toggle causes the default playlist to sort normally
6. Re-enabling the toggle immediately pins the default playlist back to the top
7. Restarting the app preserves the pin preference
</verification>

<success_criteria>
- Default playlist pinned to top of playlist view when setting enabled (default: enabled)
- Toggle in Settings > Favorites controls the behavior
- Setting persists in config.toml across restarts
- All other sort functionality (field + direction) works normally for non-default playlists
- No regressions to existing playlist features (sorting, filtering, drag-drop, context menu)
</success_criteria>

<output>
After completion, create `.planning/quick/7-pin-default-playlist-to-top-of-playlist-/7-SUMMARY.md`
</output>
