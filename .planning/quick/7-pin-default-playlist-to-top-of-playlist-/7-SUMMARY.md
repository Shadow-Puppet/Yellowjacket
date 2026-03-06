---
phase: quick-7
plan: 1
subsystem: favorites
tags: [config, playlist, sort, favorites, full-stack]
dependency_graph:
  requires: []
  provides: [pin-default-playlist]
  affects: [playlist-view, config-page, favorites-store]
tech_stack:
  added: []
  patterns: [toggle-config-field, sort-pinning]
key_files:
  created: []
  modified:
    - backend/favorites/config.go
    - backend/config/config.go
    - frontend/src/store/favorites-store.ts
    - frontend/src/store/controllers/favorites-controller.ts
    - frontend/src/components/playlist-view/playlist-view.ts
    - frontend/src/components/config-page/config-page.ts
    - frontend/wailsjs/go/config/Config.js
    - frontend/wailsjs/go/config/Config.d.ts
decisions:
  - "Default PinDefault to true for new installs; existing configs without the field get Go zero-value (false) from TOML"
metrics:
  duration: 10 min
  completed: "2026-03-01"
---

# Quick Task 7: Pin Default Playlist to Top of Playlist View

Full-stack pin-to-top feature: PinDefault bool config field, Go getter/setter with event emission, frontend store/controller/view wiring, and config page toggle.

## Completed Tasks

| # | Task | Commit | Key Changes |
|---|------|--------|-------------|
| 1 | Add PinDefault to backend config and expose getter/setter | `6e123bd` | PinDefault field on favorites.Config, Get/SetPinDefaultPlaylist methods, event payload update, applyDefaults with true |
| 2 | Wire frontend store, controller, playlist-view sort logic, and config page toggle | `e6378e1` | favorites-store pinDefault state + getter/setter, controller delegation, sortedEntries pinning logic, config-page toggle, Wails bindings |

## Implementation Details

### Backend (Task 1)

- Added `PinDefault bool` with `toml:"PinDefault"` tag to `favorites.Config` struct
- Added `GetPinDefaultPlaylist() bool` — returns `true` when `Favorites` is nil (safe default)
- Added `SetPinDefaultPlaylist(pin bool) error` — follows existing setter pattern (nil guard, save, emit, log)
- Updated `emitFavoritesChanged()` to include `"PinDefault"` in the event payload map
- In `applyDefaults()`, new `favorites.Config` structs are created with `PinDefault: true`

### Frontend (Task 2)

- **favorites-store.ts**: Added `pinDefault` private field (default `true`), `getPinDefault()` getter, `setPinDefault()` action (optimistic update + backend call), included in `loadConfig()` Promise.all, and event handler reads `data.PinDefault`
- **favorites-controller.ts**: Added `get pinDefault(): boolean` and `async setPinDefault(pin)` delegating to store
- **playlist-view.ts**: Updated `sortedEntries` getter — when `this.favCtrl.pinDefault` is true, the playlist matching `this.favCtrl.playlistId` always sorts to index 0, regardless of sort field/direction
- **config-page.ts**: Added `<config-field type="toggle">` for "Pin to Top" in the Favorites section with `handlePinDefaultChange` handler
- **Wails bindings**: `GetPinDefaultPlaylist():Promise<boolean>` and `SetPinDefaultPlaylist(arg1:boolean):Promise<void>` (pre-generated)

## Deviations from Plan

None - plan executed exactly as written.

## Verification

- [x] `go build ./...` passes (backend compiles)
- [x] `npx tsc --noEmit` passes (frontend TypeScript compiles)
- [x] `config-field` supports `type: 'toggle'` (confirmed in config-field.ts)

## Self-Check: PASSED

All 8 modified files verified on disk. Both task commits (6e123bd, e6378e1) found in git history.
