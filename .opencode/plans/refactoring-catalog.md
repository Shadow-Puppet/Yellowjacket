# Refactoring Catalog

Prioritized list of architectural improvements identified during a full codebase audit (Feb 2026). Items are grouped by priority — tackle P1 before adding major new features, P2 as convenient, P3 opportunistically.

---

## P1 — Should fix before adding major features

### 1. ~~Resolve `RequestPlay` dual-handler ambiguity~~ — solved

---

### 2. ~~Remove player from Wails `FEBindings` (or remove event handlers)~~ — solved

---

### 3. ~~Split `queue.go` (2254 lines)~~ — solved

---

### 4. Split `cover-grid.ts` (3740 lines)

**Problem:** The largest frontend component by far. It likely handles album grid rendering, context menus, drag-and-drop, selection, sorting, resizing, and more — all in a single file.

**Why it matters:** Difficult to understand, modify, or review. Changes to context menu logic risk breaking grid rendering and vice versa.

**Approach:** Extract logical sections into separate files/components:

- Context menu logic into a shared utility or sub-component
- Selection logic already uses a `SelectionController` — verify it's fully extracted
- Drag-and-drop setup into the existing `DragController` if not already
- Grid rendering as the core component, delegating to these helpers

---

## P2 — Fix when convenient

### 5. Delete `backend/models/` package (dead code)

**Problem:** The `models` package (`files.go`, `music.go`, `art.go`) defines `AudioFile`, `AudioFileType`, `Album`, `Track`, `Artist`, and `Art` types. No package imports it anywhere.

**Why it matters:** Dead code creates confusion — new contributors may think these are the canonical domain types, but the actual types are in `library/`, `queue/`, `playlist/`, and `sqlcgen/`.

**Approach:** Delete the entire `backend/models/` directory.

---

### 6. Extract `SizedFilename` to a shared utility package

**Problem:** `library.SizedFilename()` is a small string utility for generating thumbnail filenames. Both `player/player.go` and `playlist/playlist.go` import the entire `library` package solely for this function.

**Why it matters:** Creates unnecessary coupling — `player` -> `library` and `playlist` -> `library` dependencies exist only for one utility function.

**Approach:** Move `SizedFilename` to a shared package (e.g., `backend/coverart/` or `backend/fileutil/`). Update the three callers: `library/`, `player/`, and `playlist/`.

---

### 7. Consolidate `LibraryScanComplete` handling

**Problem:** `LibraryScanComplete` is listened to directly in 10+ components (`genres-view.ts`, `artists-view.ts`, `cover-grid.ts`, `track-list.ts`, `playlist-view.ts`, `genre-details.ts`, `artist-details.ts`, `playlist-picker.ts`, `config-page.ts`, `library-manager.ts`) in addition to `library-store.ts` and `playlist-store.ts`. Each component independently re-fetches its data.

**Why it matters:** The stores already invalidate their caches and notify subscribers on this event. Components that use the store controllers should get re-rendered automatically. The direct listeners exist because many components load data independently from the stores (calling Go bindings directly), which means the stores aren't serving their full purpose as centralized data sources.

**Approach:** For components that already use `LibraryController`/`PlaylistController`, the store subscription should handle cache invalidation. The controller's `hostConnected` subscribes and `requestUpdate` triggers a re-render, which calls the async data getter, which will re-fetch since the cache was invalidated. Remove the redundant direct `EventsOn(LibraryScanComplete)` from components that go through stores. For components like `playlist-picker.ts` that call Go bindings directly (bypassing stores), either route them through the store or accept the direct listener as intentional.

---

### 8. Type the WebAwesome popup interactions (eliminate 49x `as any`)

**Problem:** Every component with a context menu uses `(popup as any).anchor = ...` and `(popup as any).active = true`. This pattern appears 49 times across `track-list.ts`, `cover-grid.ts`, `queue-panel.ts`, `playlist-view.ts`, `genres-view.ts`, `artists-view.ts`.

**Why it matters:** Type safety is completely bypassed for a core interaction pattern. Typos in property names (`actve` instead of `active`) would silently fail.

**Approach:** Create a type declaration for the WebAwesome popup element (or find one in their package). Alternatively, write a small typed utility:

```typescript
function openPopup(popup: Element, anchor: Element | VirtualAnchor): void
function closePopup(popup: Element): void
```

Replace all 49 `as any` casts with calls to these utilities.

---

### 9. Replace `GetCurrentTrackInfo` `map[string]interface{}` with a struct

**Problem:** `player.GetCurrentTrackInfo()` returns `map[string]interface{}` with stringly-typed keys (`"fileName"`, `"filePath"`, `"state"`, `"title"`, etc.). The `emitTrackChanged()` method mutates this map by adding keys after the fact.

**Why it matters:** No compile-time safety — typos in key names are silent bugs. The Wails binding generator would produce typed TypeScript if given a struct.

**Approach:** Define a `TrackInfo` struct in the player package with all the fields. Return it from `GetCurrentTrackInfo`. Update `emitTrackChanged` to build the struct directly instead of mutating a map.

---

### 10. Move `FullRescan` orchestration from library to app

**Problem:** `library.Library` holds references to the queue (`queueClearer` interface) and playlist service (`playlistRestorer` interface), set via `SetQueue()` and `SetPlaylistRestorer()`. The `FullRescan` method in `rescan.go` orchestrates clearing the queue and restoring playlists — cross-cutting concerns that aren't really library responsibilities.

**Why it matters:** The library package shouldn't know about queue clearing or playlist restoration. This creates a dependency web (`app` -> `library` -> `queue`, `app` -> `library` -> `playlist`).

**Approach:** Move the `FullRescan` orchestration to the `app` level. The app already has references to all three packages. The library would only expose `Scan()` and a `ClearAndRescan()` that handles only library concerns (clear DB, walk files, extract metadata). The app's `FullRescan` handler would call `queue.Clear()`, `library.ClearAndRescan()`, then `playlist.RestoreAll()`.

---

### 11. Fix double `LibraryScanStarted` event during FullRescan

**Problem:** `rescan.go:22` emits `LibraryScanStarted`, then calls `Scan()` which emits `LibraryScanStarted` again at `library.go:191`. The frontend receives two `LibraryScanStarted` events for a single full rescan.

**Why it matters:** Frontend components may show duplicate "scanning" UI state transitions or start/reset loading indicators twice.

**Approach:** Remove the `LibraryScanStarted` emission from either `FullRescan` or `Scan`. Since `Scan` is also called independently, keep it in `Scan` and remove it from `FullRescan`.

---

### 12. Inconsistent communication patterns: queue (events) vs playlist (bindings)

**Problem:** Queue operations use 14+ `Request*` events with manual `data[0].(type)` casting in ~300 lines of handler boilerplate. Playlist operations use direct Wails bindings with type-safe Go function signatures.

**Why it matters:** Inconsistency makes the codebase harder to learn. The queue's event-only approach requires substantial boilerplate that the playlist avoids. New features on the queue require touching 4 files (Go event constant, TS event constant, Go handler, TS store method) vs 1-2 files for the playlist.

**Approach:** This is a larger refactor. Two options:

1. **Move queue to bindings** (recommended): Add the queue to `FEBindings`, expose typed methods, call them directly from the frontend store. Remove the event handlers and the `Request*` events. Keep the backend-to-frontend events (`QueueChanged`, etc.) for state push.
2. **Accept the inconsistency**: Document the rationale (queue existed before playlists, events were the original pattern, bindings were adopted later). Add a comment in AGENTS.md.

---

## P3 — Fix opportunistically

### 13. Dead player methods: `ChangeVolume`, `MuteToggle`, `CurrentPosition`

**Problem:** `ChangeVolume()` (`player.go`), `MuteToggle()` (`player.go`), and `CurrentPosition()` (percentage-based, `player.go`) have zero callers anywhere in the codebase.

**Approach:** Delete them, or keep them if you plan to add keyboard shortcuts / media key support soon.

---

### 14. ~~Unused queue sentinels: `ErrEmptyQueue`, `ErrNoPlayer`~~ — solved

Removed during the queue.go split/rewrite (item #3).

---

### 15. `SeekFailed` event emitted but never listened to

**Problem:** `player.go` emits `SeekFailed` when seeking fails, but no frontend code subscribes to it. Users get no feedback on seek failure.

**Approach:** Either add a frontend listener that shows a brief notification/toast, or remove the event emission if seek failure feedback isn't needed.

---

### 16. `path.Join` instead of `filepath.Join` in config

**Problem:** `config/config.go:41` uses `path.Join` (POSIX paths) instead of `filepath.Join` (OS-aware paths) for constructing the config file path.

**Approach:** Replace with `filepath.Join`. Single-line change.

---

### 17. Replace 200ms sleep with frontend-ready handshake

**Problem:** `app.go:205-216` uses `time.Sleep(200 * time.Millisecond)` before emitting state to the frontend, assuming it will be ready by then.

**Approach:** Have the frontend emit a "ready" event when its stores have initialized. The backend listens for this event and then emits the current state. Eliminates the timing assumption.

---

### 18. ~~Custom `sortInts` in queue instead of `slices.Sort`~~ — solved

Replaced during the queue.go refactoring (item #3).

---

### 19. `playlist-picker.ts` bypasses `playlistStore`

**Problem:** `playlist-picker.ts` calls `GetAllPlaylists()` directly from the Go binding instead of going through `playlistStore`. It fetches only summaries (not `WithTracks`), which is why it doesn't use the store.

**Approach:** Either add a `getSummaries()` method to the playlist store that caches just the summary list, or accept this as intentional since the picker only needs summaries and the full `WithTracks` fetch would be wasteful for this use case.

---

### 20. `library-manager.ts` and `config-page.ts` overlap

**Problem:** Both components exist (different nav routes: "libraries" vs "settings"). `config-page.ts` has a comment saying scan metrics were "carried over from library-manager". They may have diverging copies of similar logic.

**Approach:** Audit both components for duplicated logic. If the library manager's functionality is fully subsumed by the config page, consider removing it and redirecting the "libraries" nav route.
