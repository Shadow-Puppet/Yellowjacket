# Refactoring Catalog

Prioritized list of architectural improvements identified during a full codebase audit (Feb 2026). Items are grouped by priority — tackle P1 before adding major features, P2 as convenient, P3 opportunistically.

---

## P1 — Should fix before adding major features

### 1. ~~Resolve `RequestPlay` dual-handler ambiguity~~ — solved

---

### 2. ~~Remove player from Wails `FEBindings` (or remove event handlers)~~ — solved

---

### 3. ~~Split `queue.go` (2254 lines)~~ — solved

---

### 4. ~~Split `cover-grid.ts` (3740 lines)~~ - solved

---

## P2 — Fix when convenient

### ~~5. Delete `backend/models/` package (dead code)~~ - solved

---

### ~~6. Extract `SizedFilename` to a shared utility package~~ — solved

Created `backend/coverart/` package with `SizedFilename`, a `ResolveURLs` helper (encapsulates the repeated pattern of resolving filesystem paths to all size-variant URL paths), a `URLs` struct, and a `PathPrefix` constant. Removed `SizedFilename` from `library/coverart.go`. Updated all four callers (`library/query.go`, `player/player.go`, `playlist/playlist.go`, `app.go`) to use `coverart.ResolveURLs`, eliminating the `player` -> `library` and `playlist` -> `library` coupling. Added tests for the new package.

---

### 7. ~~Consolidate `LibraryScanComplete` handling~~ — solved

---

### ~~8. Type the WebAwesome popup interactions (eliminate 49x `as any`)~~ — solved

---

### ~~9. Replace `GetCurrentTrackInfo` `map[string]interface{}` with a struct~~ — solved

---

### ~~10. Move `FullRescan` orchestration from library to app~~ — solved

Replaced `queueClearer`/`playlistRestorer` interfaces and `SetQueue`/`SetPlaylistRestorer` setters with a single `RescanHooks` struct containing `PreClear`/`PostScan` function callbacks. The app wires `queue.Clear` and `playlist.RestoreAllPlaylists` as hooks, so the library no longer has any knowledge of or dependency on those packages.

---

### ~~11. Fix double `LibraryScanStarted` event during FullRescan~~ — solved

Removed the `LibraryScanStarted` emission from `FullRescan` (resolved as part of item #10). The event is now only emitted from `Scan()`, giving exactly one emission per rescan.

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
