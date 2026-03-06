# Domain Pitfalls

**Domain:** Adding tag editing, scan cancellation, smart playlists, customizable keyboard shortcuts, gapless playback + crossfade, MusicBrainz browser, layout customization, and plugin system to an existing Go/Wails/Lit/SQLite desktop music player
**Researched:** 2026-03-06
**Confidence:** HIGH (based on deep codebase analysis, official MusicBrainz API docs, beep library docs, and established Go/Wails/SQLite patterns)

---

## Critical Pitfalls

These mistakes cause rewrites, data loss, or architectural dead ends.

---

### Pitfall 1: Tag Writing Corrupts Audio Files or Loses Data

**Feature area:** Tag editing
**What goes wrong:** Writing ID3/Vorbis tags corrupts the audio file — partial writes leave the file unplayable, or the tag library strips existing frames (cover art, replay gain, MusicBrainz IDs) that it doesn't understand. The user edits "Artist" and loses their embedded lyrics, custom TXXX frames, and cover art. Worse: if the file is currently being played by beep, simultaneous reads and writes corrupt both the playback stream and the tag data.

**Why it happens:**
- `github.com/dhowden/tag` (already in deps) is **read-only** — it does not support tag writing. The CONCERNS.md notes tag writing as a known gap (line 22).
- ID3v2 tag writing requires rewriting the file header. If the new tag is larger than the existing padding, the **entire file must be rewritten** — the audio data shifts. A crash or power loss during rewrite produces a corrupted file.
- FLAC uses Vorbis Comments in a METADATA_BLOCK. Rewriting this block similarly requires shifting the audio frame data if the block grows.
- The beep decoder holds an `*os.File` handle for the currently playing track. Writing to that same file while beep's read-ahead goroutine (`BufferedStreamer.readAhead`) is actively streaming from it will cause data corruption — the file offsets shift but the decoder's internal state doesn't update.

**Prevention:**
1. **Use `github.com/bogem/id3v2/v2` (n10v/id3v2)** for MP3 tag writing — it supports ID3v2.3 and v2.4 read/write with 359 stars and active maintenance. For FLAC, use `github.com/go-flac/flacvorbis` or a similar FLAC-specific writer. Keep `dhowden/tag` for read operations.
2. **Write-to-temp-then-rename pattern:** Write the modified file to a temp file in the same directory, then `os.Rename()` atomically. This ensures the original file is never partially written. On failure, the temp file is deleted and the original is untouched.
3. **Block tag writes on the currently playing file.** Before writing, check if `player.currentFile` points to the same path. If so, either: (a) stop playback, close the file, write, then reload; or (b) queue the write to execute after the track changes. Option (a) is simpler and more predictable.
4. **After writing tags, update the database.** The tag write changes the file on disk but the SQLite database still has the old metadata. You must: update the `recordings` table, update `artist_credit`/`release_groups` if changed, rebuild the FTS5 `search_index` entry for that track, and invalidate any entity cache.
5. **Preserve frames you don't edit.** When using id3v2, open with `Parse: true` to load all existing frames, modify only the ones the user changed, then save. Don't create a new tag from scratch.

**Detection:**
- Audio file won't play after tag edit
- Cover art disappears after editing title/artist
- Playback glitches or crashes during a tag write on the currently playing file
- FTS5 search returns stale metadata after edits

**Confidence:** HIGH — `dhowden/tag` being read-only is confirmed by its API (no `Save()` or `Write()` methods). File corruption from concurrent read/write is a fundamental OS-level concern.

---

### Pitfall 2: Gapless Playback Breaks the Existing Lock Ordering and Callback Contract

**Feature area:** Gapless playback + crossfade
**What goes wrong:** The current playback flow uses `beep.Seq(streamer, beep.Callback(func() { go p.onPlaybackFinished() }))` — when the stream ends, the callback fires (with speaker lock held), dispatches to a goroutine, which then tells the queue to advance, which calls `player.LoadFile()`. This produces an audible gap of 100-500ms (file open + decode + resample + buffer fill). Attempting to eliminate this gap by pre-decoding the next track while the current one plays introduces a new concurrent resource: two open decoders, two BufferedStreamers, two file handles, and a crossfade mixer that must be swapped into the speaker chain atomically.

**Why it happens:**
- **beep's `speaker.Play()` adds streamers to a global mix.** You can call it multiple times — new streamers are mixed with existing ones. But the Player struct assumes a single active streamer chain (`p.speakerStreamer`). Pre-loading a second track means two streamer chains are live simultaneously.
- **The lock ordering `p.mu → speaker.Lock()` assumes one-at-a-time.** With crossfade, you need to: (a) decode the next track under `p.mu`, (b) build its streamer chain, (c) under `speaker.Lock()`, splice the crossfade mixer into the active chain. If the existing track's `onPlaybackFinished` fires during this splice, you have a race between the callback goroutine (acquiring `p.mu`) and the pre-load logic (holding `p.mu` and needing `speaker.Lock()`).
- **The `BufferedStreamer` has its own goroutine.** With two tracks buffering simultaneously, you have two `readAhead()` goroutines competing for disk I/O. The `Close()` method must be called on the old BufferedStreamer at the right time — too early truncates audio, too late leaks goroutines.

**Prevention:**
1. **Don't try to pre-decode inside the existing `LoadFile` flow.** Instead, build a separate pre-loading mechanism: when the current track reaches N seconds from the end (detectable by comparing `seeker.Position()` to `seeker.Len()`), start decoding the next track in a background goroutine. Store the pre-decoded streamer and format in a `nextTrack` field on the Player struct, protected by `p.mu`.
2. **For gapless (no crossfade): use `beep.Seq` with both streamers.** When the pre-decoded next track is ready, replace the current speaker chain with `beep.Seq(remainingCurrentTrack, nextTrackStreamer, beep.Callback(...))`. This lets beep handle the seamless transition without a gap. The key insight: you must resample both tracks to the same sample rate (the speaker rate, 44100) before sequencing them.
3. **For crossfade: build a custom `CrossfadeStreamer`.** This streamer reads from both the ending track and the starting track simultaneously, mixing their samples with a volume ramp. Register this single crossfade streamer with the speaker. It internally manages the two underlying streamers and their lifecycle.
4. **Never close the outgoing `BufferedStreamer` until the crossfade is complete.** The crossfade streamer should call `Close()` on the old track's BufferedStreamer only after it has drained all needed samples from it.
5. **The `onPlaybackFinished` callback must be suppressed during gapless/crossfade transitions.** If beep's `Seq` fires the callback for track A while you've already started track B, the queue will try to advance again. Use a "gapless transition in progress" flag, or change the callback to a no-op during transitions and notify the queue directly from the pre-load logic.

**Detection:**
- App deadlocks when tracks transition (lock ordering violation)
- Two tracks play simultaneously (both speaker.Play'd without removing the old one)
- Goroutine leak (BufferedStreamer.readAhead never returns)
- Audio cuts out briefly then resumes (old track closed before crossfade samples drained)
- Queue advances twice (callback fires AND pre-load logic notifies queue)

**Confidence:** HIGH — lock ordering and callback contract are documented in player.go. The `go p.onPlaybackFinished()` goroutine dispatch pattern is explicitly commented as avoiding deadlock (lines 355-361).

---

### Pitfall 3: Scan Cancellation Leaves Database in Inconsistent State

**Feature area:** Scan cancellation
**What goes wrong:** User cancels a scan mid-way through Phase 4 (DB writer batching results). The current batch may be partially committed — 30 of 50 files written in a transaction that got rolled back, but the `added` counter was already incremented. Or worse: the orphan cleanup (Phase 5) runs on a partial scan, deleting files from the database that weren't visited because the walk was cancelled early, not because they were actually deleted from disk.

**Why it happens:**
- The scan pipeline has 6 phases running as communicating goroutines (walk → worker pool → DB writer → orphan cleanup → thumbnail generation). Cancellation must propagate cleanly through all of them.
- The `l.ctx.Done()` checks in the walk phase (lines 297, 324) use the Wails app context, which is only cancelled on shutdown. A user-triggered cancellation needs a separate `context.WithCancel()`.
- The `existingPaths` sync.Map is loaded in Phase 1 and entries are removed as files are found during the walk (Phase 2). Orphan cleanup (Phase 5) iterates remaining entries and deletes them. If the walk was cancelled early, many valid files remain in `existingPaths` and get incorrectly deleted as orphans.
- The DB writer's `flushBatch()` runs inside a transaction. If the context is cancelled between `BEGIN` and `COMMIT`, the transaction rolls back, but the import results have already been dequeued from `resultChan` — they're lost.

**Prevention:**
1. **Create a scan-specific context:** `scanCtx, scanCancel := context.WithCancel(l.ctx)`. Store `scanCancel` on the Library struct so the frontend can call a `CancelScan()` method.
2. **Skip orphan cleanup on cancelled scans.** Add a `cancelled bool` check before Phase 5. If the scan was cancelled, the `existingPaths` map is incomplete — orphan cleanup would delete valid files. Emit a `LibraryScanCancelled` event instead of `LibraryScanComplete`.
3. **Make the DB writer respect cancellation between batches, not mid-batch.** Check `scanCtx.Done()` in the `for result := range resultChan` loop, but let the current `flushBatch()` complete before stopping. This ensures each committed batch is complete.
4. **Drain channels on cancellation.** When the walk is cancelled, it closes `workChan`. Workers drain and close `resultChan`. The DB writer drains `resultChan` normally. But if workers are blocked sending to `resultChan` (buffer full), they need to select on `scanCtx.Done()` too. Ensure all goroutines can unblock.
5. **Report partial results.** The `ScanMetrics` should include a `Cancelled: true` flag. The frontend should show "Scan cancelled — X files processed" rather than treating it as a failure.

**Detection:**
- Files disappear from library after cancelling a scan (orphan cleanup ran on partial data)
- `ScanMetrics.Added` doesn't match actual DB row count (counter incremented but batch rolled back)
- App hangs on cancel (goroutines blocked on channel sends/receives)
- Subsequent scan adds files that were already in the library (previous scan's partial results lost)

**Confidence:** HIGH — confirmed by reading the scan pipeline code (library.go lines 175-540). The orphan cleanup problem is the most dangerous because it's a silent data loss.

---

### Pitfall 4: Plugin System Without Isolation Crashes the Host App

**Feature area:** Plugin system
**What goes wrong:** A plugin panics in a goroutine, and since Go panics are per-goroutine, the entire application crashes. Or a plugin holds the speaker lock for too long and audio glitches. Or a plugin writes to the SQLite database concurrently and hits `SQLITE_BUSY`. Or a plugin registers a Wails event handler that conflicts with core event names. The "full-access API" promised in the project requirements makes every component a potential victim of plugin misbehavior.

**Why it happens:**
- Go has no built-in process isolation for plugins. `plugin.Open()` loads shared objects into the same address space. Panics, goroutine leaks, and memory corruption in plugins affect the host.
- The SQLite single-writer constraint (`SetMaxOpenConns(1)`) means any plugin database access serializes with all core operations. A slow plugin query blocks library scans, queue persistence, and player state saves.
- The Wails event system is a global namespace. If a plugin emits `TrackChanged`, it could confuse the frontend. If it subscribes to `PlaybackFinished`, it runs in the same goroutine context as core handlers.

**Prevention:**
1. **Don't use Go's `plugin` package.** It requires matching Go versions between host and plugin, doesn't work on all platforms, and provides no isolation. Instead, use one of:
   - **Embedded scripting (Lua via `github.com/yuin/gopher-lua` or JavaScript via `github.com/nicholasgasior/goja`):** Run plugin code in an interpreter with controlled API exposure. Panics in the interpreter don't crash the host.
   - **Process-based plugins with gRPC/stdin-stdout RPC:** Like HashiCorp's `go-plugin` model. Full isolation but higher complexity and latency.
   - **WASM plugins (e.g., `github.com/tetratelabs/wazero`):** Good isolation, cross-platform, but limited Go interop.
   For a desktop music player, **embedded Lua or JS is the pragmatic choice** — it's fast enough for UI customization and event hooks, and panics are contained.
2. **Wrap all plugin API calls in recover().** If using native Go plugins or any host-side callback, wrap in `defer func() { if r := recover(); r != nil { log.Error(...) } }()`.
3. **Give plugins a read-only database view.** Open a second read-only SQLite connection (since WAL mode supports concurrent readers) for plugins. This doesn't compete with the single writer.
4. **Namespace plugin events.** All plugin-emitted events must be prefixed: `plugin:<pluginID>:<eventName>`. Core events cannot be emitted by plugins.
5. **Rate-limit plugin API calls.** A plugin calling `Player.Seek()` in a tight loop would create a cascade of mutex acquisitions, speaker locks, event emissions, and frontend updates. Apply a rate limiter (e.g., 10 calls/second per plugin per API surface).

**Detection:**
- App crashes with panic stack trace originating in plugin code
- Audio stutters when a plugin is active (speaker lock contention)
- Library scan takes 10x longer with plugins installed (SQLite writer contention)
- Frontend shows ghost events from plugin event namespace collisions

**Confidence:** MEDIUM — plugin architecture is a design decision with many valid approaches. The specific pitfalls around Go's `plugin` package and SQLite single-writer are HIGH confidence. The recommendation for embedded scripting is based on the "foundation, not feature-complete" goal stated in PROJECT.md.

---

## Moderate Pitfalls

These mistakes cause significant rework or user-facing bugs but not architectural collapse.

---

### Pitfall 5: MusicBrainz Rate Limiting Blocks the User or Gets the App Banned

**Feature area:** MusicBrainz browser
**What goes wrong:** The app fires burst requests to MusicBrainz when the user browses an artist's discography (artist lookup + release groups + releases + recordings = 4+ API calls per click). MusicBrainz enforces a **1 request per second per IP address** rate limit (confirmed from official docs). Exceeding this returns HTTP 503 for ALL subsequent requests until the rate drops. The user sees blank pages and errors. Worse: if the User-Agent string is missing or generic, the app falls into the "anonymous" throttle bucket with a shared 50 req/s global limit.

**Why it happens:**
- YellowJacket is currently a fully offline app (INTEGRATIONS.md: "No external API calls, cloud services"). Adding network requests is a new domain with no existing patterns for rate limiting, caching, or error handling.
- MusicBrainz API responses are richly linked — an artist has release groups, each release group has releases, each release has recordings. A naive "fetch everything on click" pattern generates a burst of requests.
- The `inc` parameter in the MusicBrainz API allows requesting related data in a single call (e.g., `?inc=release-groups+recordings`), but many combinations are not allowed together, forcing multiple requests anyway.

**Prevention:**
1. **Set a proper User-Agent:** `YellowJacket/<version> (https://github.com/your/repo)` — this is REQUIRED by MusicBrainz. Without it, the app is rate-limited as "anonymous" (official docs confirm).
2. **Implement a global HTTP rate limiter:** Use `golang.org/x/time/rate` with `rate.NewLimiter(1, 1)` — one request per second, burst of 1. All MusicBrainz API calls go through this limiter. This is the officially documented limit.
3. **Cache aggressively.** MusicBrainz data changes rarely. Cache responses in SQLite (a new `musicbrainz_cache` table with MBID as key, response JSON as value, and a TTL column). Artist data can be cached for days. This eliminates repeat API calls for the same artist/album.
4. **Use `inc` parameters to reduce request count.** Fetch `artist?inc=release-groups` in one call rather than artist + separate release-groups lookup. Check the MusicBrainz API docs for valid `inc` combinations.
5. **Show loading states, not blank pages.** While waiting for rate-limited responses, show skeleton UI with a "Loading from MusicBrainz..." indicator. Queue requests and process them sequentially.
6. **Handle 503 gracefully.** On 503, back off exponentially (2s, 4s, 8s). Show the user "MusicBrainz is rate limiting us, retrying in Xs..." Don't silently fail.

**Detection:**
- Blank artist/album pages in the MusicBrainz browser
- Console shows repeated 503 errors
- All MusicBrainz browsing stops working for ~10 seconds (IP-level block)
- MusicBrainz community reports your app as misbehaving

**Confidence:** HIGH — rate limiting rules confirmed from official MusicBrainz documentation at https://musicbrainz.org/doc/MusicBrainz_API/Rate_Limiting.

---

### Pitfall 6: Smart Playlists Trigger Expensive Full-Table Scans on Every Library Change

**Feature area:** Smart playlists
**What goes wrong:** A smart playlist with filter rules like "genre = 'Rock' AND year > 2000 AND playCount > 5" must be re-evaluated whenever the library changes (scan complete, tag edit, etc.). If evaluation queries the full `track_metadata` VIEW (which already joins 5 tables) with additional filter conditions, each smart playlist re-evaluation is a full table scan. With 10 smart playlists and a 50k-track library, a library scan completion triggers 10 expensive queries simultaneously, blocking the single SQLite writer for seconds and freezing the UI.

**Why it happens:**
- The `track_metadata` VIEW (schemas/track_metadata_view.sql) joins `audio_files`, `recordings`, `artist_credit`, `release_group_recordings`, `release_groups`, `genres`, and `file_types`. Adding smart playlist filters on top of this VIEW means SQLite can't use indexes effectively — VIEWs are expanded inline.
- SQLite's `SetMaxOpenConns(1)` means all these queries serialize. Even read queries block behind any pending write.
- Smart playlist re-evaluation is triggered by `LibraryScanComplete` events. If 10 smart playlists each take 200ms to evaluate, that's 2 seconds of blocked database access.

**Prevention:**
1. **Don't re-evaluate all smart playlists on every library change.** Instead, mark smart playlists as "stale" when the library changes, and only re-evaluate when the user views the playlist.
2. **Write dedicated sqlc queries for smart playlist evaluation** that target specific indexed columns directly on `audio_files` and `recordings` tables, rather than going through the `track_metadata` VIEW. For example, a "genre = Rock" filter should query `genre_recordings JOIN genres` directly with an index on `genres.name`.
3. **Add indexes for common smart playlist filter columns:** `recordings.year`, `genres.name`, `recordings.name` if not already indexed. Check existing indexes before adding.
4. **Batch evaluation.** If multiple smart playlists need re-evaluation, evaluate them in a single transaction to amortize the transaction overhead.
5. **Store smart playlist rules as JSON in a new `smart_playlists` table**, separate from the existing `playlists` table. Smart playlists don't have fixed track lists — they have rules. Mixing them into the same table complicates the playlist code.
6. **Consider a play_count column.** Smart playlists often filter by play count, but there's no `play_count` column in the current schema. This needs a schema migration adding it to `audio_files` or `recordings`, with an increment trigger on playback completion.

**Detection:**
- UI freezes for several seconds after library scan completes
- SQLite `busy_timeout` errors in logs during smart playlist evaluation
- Smart playlist contents don't update until app restart (stale evaluation)

**Confidence:** HIGH — the VIEW structure and single-writer constraint are confirmed from the codebase. The performance concern is proportional to library size.

---

### Pitfall 7: Keyboard Shortcut System Conflicts with Browser/WebView Defaults and Shadow DOM

**Feature area:** Customizable keyboard shortcuts
**What goes wrong:** The user configures Ctrl+L as "next track," but WebKitGTK intercepts Ctrl+L as "focus address bar" (or similar browser-internal shortcut). Or the user maps Space to "play/pause," but pressing Space while focused on a button triggers the button's click handler AND the global shortcut. Shadow DOM boundaries in Lit components further complicate event propagation — a keyboard event inside a component's shadow root may not bubble to the document-level shortcut handler.

**Why it happens:**
- Wails v2 uses a native WebView (WebKitGTK on Linux). The WebView has its own keyboard shortcut handling that runs before JavaScript event handlers. Some key combinations are intercepted before they reach the page.
- The existing Ctrl+F handler in `index.ts` (line 157) uses `document.addEventListener('keydown', ...)`. This works because it's at the document level. But components with shadow DOM (all Lit components in this project) create isolated event boundaries. A `keydown` event on an `<input>` inside a shadow root does bubble to the document, but `event.composedPath()` must be used to determine the actual target.
- Keyboard shortcuts that overlap with form controls (Space, Enter, arrow keys, Tab) interfere with normal text input, button interaction, and accessibility navigation.

**Prevention:**
1. **Use `document.addEventListener('keydown', ..., { capture: true })` for global shortcuts.** The capture phase fires before any component-level handlers can `stopPropagation()`. This is where the shortcut system should live.
2. **Skip shortcuts when focus is on an input/textarea.** Check `document.activeElement` (and use `event.composedPath()` to see through shadow DOM) — if the focused element is an input, text area, or contenteditable, don't handle the shortcut unless it uses a modifier key (Ctrl, Alt, Meta).
3. **Maintain a conflict list of reserved key combinations.** Some keys cannot be remapped because WebKitGTK intercepts them: Ctrl+C/V/X (copy/paste/cut), Ctrl+A (select all), Tab (focus navigation). Document these as non-configurable.
4. **Store shortcuts in the TOML config** under a `[Shortcuts]` section. Use the existing config event pattern — `ShortcutsConfigChanged` event triggers frontend re-registration. Don't store shortcuts in the frontend — the backend is the source of truth.
5. **Use the `key` property, not `keyCode`.** `keyCode` is deprecated and varies by keyboard layout. `event.key` is layout-aware and returns "a" regardless of whether the user has QWERTY or AZERTY.

**Detection:**
- Some key combinations "don't work" on Linux but work on macOS (WebView intercepts differently)
- Typing in a search or playlist name triggers shortcut actions
- Shortcut works when focus is on the track list but not when focus is inside a shadow DOM component

**Confidence:** HIGH — the shadow DOM event boundary behavior is fundamental to Lit/Web Components. WebView keyboard interception is platform-specific and confirmed by Wails community reports.

---

### Pitfall 8: Layout Customization Breaks Component Assumptions About Size and Context

**Feature area:** Layout customization system
**What goes wrong:** The current layout is hardcoded in `index.html` with a CSS Grid template: `"top-bar top-bar" 4em "sidebar main-panel" 1fr "bottom-bar bottom-bar" 4em`. Components assume their grid area and available space — `track-list` expects to fill the main panel, `now-playing` expects to be in the bottom bar with exactly 4em height. When users can rearrange sections, a component designed for a wide horizontal area (queue panel) gets placed in a narrow sidebar slot, or the `audio-player` component that assumes bottom-bar positioning gets placed in the sidebar where its progress bar layout breaks.

**Why it happens:**
- Components use CSS that assumes their container context. For example, `now-playing` uses `grid-template-columns: var(--now-playing-width, 200px) 1fr auto` in the bottom bar (index.css line 68). Moving it elsewhere breaks this layout.
- The `@lit-labs/virtualizer` used for large lists requires a fixed-height container to calculate visible items. If the track list is placed in a container without explicit height, virtual scrolling breaks — it either renders all items (defeating the purpose) or renders none.
- Navigation routing in `index.ts` uses `document.getElementById('main-content')` and replaces its `innerHTML`. Layout customization means there might be multiple content areas or the main content area might have a different ID.

**Prevention:**
1. **Components must declare size constraints.** Define a component metadata interface: `{ minWidth: number, minHeight: number, resizable: boolean, preferredArea: 'main' | 'sidebar' | 'footer' | 'any' }`. The layout system validates placements against constraints.
2. **Use CSS Container Queries for responsive components.** Instead of assuming "I'm in the sidebar" or "I'm in the main panel," components should use `@container` queries to adapt their layout based on available space. This requires adding `container-type: inline-size` to layout section containers.
3. **The layout system should operate at the section level, not the component level.** Sections have fixed roles (navigation, content, playback controls, queue). Users configure which components appear in each section and section sizes, but the section structure itself remains constrained. This is the MusicBee model.
4. **Don't refactor existing components for layout flexibility in the first pass.** Instead, build the layout configuration system that works with the current component set. Mark certain components as "fixed position" (audio-player must be in footer, sidebar must exist). Allow the content area to swap between different content components. Expand flexibility in later iterations.
5. **Virtual scrolling containers need explicit height.** Any section that hosts a virtualized list must provide a concrete CSS height (not `auto`). The layout system must enforce this for sections marked as "supports-virtualization."

**Detection:**
- Virtual scrolling breaks when components are moved to different sections
- Components render with broken layouts (overlapping, zero height, horizontal overflow)
- Navigation stops working because `main-content` element doesn't exist in the new layout

**Confidence:** HIGH — the hardcoded grid layout and component CSS assumptions are confirmed from index.html and index.css analysis.

---

### Pitfall 9: Tag Editing and Library Scan Compete for SQLite Writer and File Access

**Feature area:** Tag editing + library scanning interaction
**What goes wrong:** The user edits a track's tags while a library scan is in progress. The tag write modifies the file on disk, then tries to update the database. Simultaneously, the scan's DB writer goroutine is batching inserts in a transaction. The tag edit's UPDATE waits on `busy_timeout` (5000ms). Meanwhile, the scan discovers the same file during its walk — the file's modification time has changed (because of the tag write), so the scan processes it again, overwriting the just-saved tag edits with the data it reads from the file. But the file now has the NEW tags, so the scan reads the new data... unless the scan started reading the file before the tag write completed, in which case it reads a partially written file and gets corrupted metadata.

**Why it happens:**
- SQLite single-writer with WAL mode allows concurrent reads, but writes serialize. The scan's batch transaction holds the writer lock for the duration of each batch (50 files). A tag edit UPDATE must wait for the batch to commit.
- The scan pipeline's Phase 2 (walk) checks file existence and mod time against the `sync.Map` of existing files. If a tag write changes the file between the `sync.Map` population (Phase 1) and the walk (Phase 2), the file appears "modified" and gets reprocessed.
- The metadata extraction worker pool (Phase 3) reads the file concurrently with the user's tag write. There's no file-level locking.

**Prevention:**
1. **Block tag editing during active scans.** The simplest and most robust approach. Check `l.scanning` (add an atomic bool) before allowing tag writes. Return a user-friendly error: "Cannot edit tags while library scan is in progress."
2. **Alternatively, use a per-file advisory lock.** Before writing tags, acquire an in-memory lock for that file path. Before the scan processes a file, check the same lock. This is more complex but allows tag editing during scans for non-conflicting files.
3. **After a tag write, mark the file as "recently edited" with a timestamp.** The scan's walk phase should skip files edited within the last N seconds to avoid re-processing files that were just intentionally modified.
4. **The tag write endpoint should be a single Go method that coordinates all steps atomically:** stop playback if needed → write temp file → rename → update database → update FTS5 → emit events. Don't let the caller orchestrate these steps.

**Detection:**
- Tag edits "revert" after a library scan completes
- `SQLITE_BUSY` errors in the tag edit path during scans
- Corrupted metadata for files that were edited during a scan

**Confidence:** HIGH — the single-writer constraint and scan pipeline concurrency model are confirmed from the codebase.

---

## Minor Pitfalls

These cause developer frustration or minor user issues but are containable.

---

### Pitfall 10: MusicBrainz Data Model Mismatch with YellowJacket Schema

**Feature area:** MusicBrainz browser
**What goes wrong:** MusicBrainz uses a different data model than YellowJacket's schema. MusicBrainz has release groups (albums), releases (specific editions), and recordings (tracks). YellowJacket's schema already mirrors some of this (tables named `release_groups`, `recordings`, `artist_credit`), but the mapping isn't perfect — YellowJacket's `release_groups` are "albums" with a single name, while MusicBrainz release groups have types (Album, Single, EP, Compilation), dates, and disambiguation comments. Trying to merge MusicBrainz browsing data into the existing schema creates confusion about which data is "local library" and which is "MusicBrainz catalog."

**Prevention:**
1. **Keep MusicBrainz browser data completely separate from the library database.** Use a separate cache table (`musicbrainz_cache`) or even an in-memory map. The MusicBrainz browser is read-only catalog browsing — it shouldn't modify library data.
2. **Map MusicBrainz entities to display-only DTOs**, not to the existing sqlcgen types. Create separate TypeScript interfaces (`MBArtist`, `MBReleaseGroup`, `MBRecording`) that the MusicBrainz browser components consume.
3. **If linking local tracks to MusicBrainz IDs (for future features like automatic tagging), store MBIDs as optional columns** on existing tables (e.g., `recordings.musicbrainz_id TEXT`), not as foreign keys to MusicBrainz tables. This is a one-way link — local data points to MusicBrainz, not the reverse.

**Confidence:** MEDIUM — the schema naming overlap is confirmed, but the exact API response structure would need to be verified against the MusicBrainz API at implementation time.

---

### Pitfall 11: Crossfade Sample-Rate Mismatch Between Outgoing and Incoming Tracks

**Feature area:** Gapless playback + crossfade
**What goes wrong:** Track A is a 44.1kHz MP3 and Track B is a 96kHz FLAC. Both are resampled to the speaker rate (44100Hz), but the resampling happens in `updateStreamers()` which creates a new resample chain for each track. During crossfade, both tracks must produce samples at the same rate for mixing. If the crossfade streamer reads raw samples from pre-resample streamers, the mix produces garbage audio (different sample rates interpreted as the same).

**Prevention:**
1. **Always crossfade post-resample.** The crossfade mixer must receive samples that are already resampled to the speaker rate. Since `updateStreamers()` already handles resampling, ensure the crossfade operates on the resampled output, not the raw decoder output.
2. **The crossfade streamer should accept two `beep.Streamer` interfaces** (not `beep.StreamSeeker`), because the resampled streamers don't support seeking. This matches beep's design where resampled streamers lose the StreamSeeker interface.

**Confidence:** HIGH — the resample chain is confirmed in player.go lines 309-313. The speaker rate is hardcoded to 44100.

---

### Pitfall 12: Config TOML Backward Compatibility When Adding New Sections

**Feature area:** Keyboard shortcuts, layout customization
**What goes wrong:** Adding `[Shortcuts]` and `[Layout]` sections to config.toml works for new installations (defaults applied), but existing users have config files without these sections. The TOML decoder fills in zero values for missing sections. If the code checks `config.Shortcuts != nil` but TOML decoding creates an empty struct (not nil), the nil check passes but the struct has zero-value fields. The `applyDefaults()` function runs before decode (see CONCERNS.md line 168: "applyDefaults runs after decode which could overwrite valid zero values"), creating a timing issue.

**Prevention:**
1. **Follow the existing pattern:** `applyDefaults()` sets defaults, then TOML `Decode()` overwrites with user values. New sections get populated defaults even if the user's file doesn't contain them. This already works correctly for existing sections.
2. **Add defaults for ALL new fields in `applyDefaults()`.** For shortcuts, provide a complete default keybinding map. For layout, provide the default layout matching the current hardcoded grid.
3. **Test with an empty config file and an old-format config file.** The `config_test.go` should verify that loading a TOML file without `[Shortcuts]` or `[Layout]` produces valid defaults.
4. **Never use nil checks for TOML-decoded sections.** The TOML decoder creates zero-value structs, not nil pointers. Use a validation method that checks for meaningful content (e.g., "shortcuts map is empty" not "shortcuts is nil").

**Confidence:** HIGH — the config loading pattern is confirmed from config.go and CONCERNS.md.

---

### Pitfall 13: Wails Event Bridge Payload Size for MusicBrainz and Layout Data

**Feature area:** MusicBrainz browser, layout customization
**What goes wrong:** The Wails event system serializes payloads as JSON through the WebView bridge. A MusicBrainz artist response with full discography (release groups, releases with track listings) can be 100KB+ of JSON. Emitting this via `runtime.EventsEmit()` means serializing to JSON in Go, passing through the WebView bridge, and deserializing in JavaScript. For large payloads, this introduces noticeable latency. Similarly, saving/loading a complex layout configuration with per-component state creates large event payloads.

**Prevention:**
1. **Use Wails function bindings (direct calls) for large data transfers, not events.** Events are for notifications ("data changed"). Bindings are for data retrieval ("give me the data"). The frontend should call a Go binding method that returns the MusicBrainz data directly, not listen for an event with the data embedded.
2. **Paginate MusicBrainz results.** Don't load an artist's entire discography at once. Load release groups first (lightweight), then load releases for a specific release group on click (lazy loading).
3. **For layout config, store in the TOML file and load via the existing config binding pattern.** Don't emit the full layout through events — load it once at startup via `Config.GetLayoutConfig()` binding.

**Confidence:** MEDIUM — Wails event serialization overhead depends on WebView implementation. The recommendation to use bindings over events for data is based on Wails architecture best practices.

---

### Pitfall 14: Frontend Store Proliferation and Controller Explosion

**Feature area:** Smart playlists, MusicBrainz browser, layout customization, plugin system
**What goes wrong:** Each new feature area gets its own store and controller: `SmartPlaylistStore + SmartPlaylistController`, `MusicBrainzStore + MusicBrainzController`, `LayoutStore + LayoutController`, `PluginStore + PluginController`, `ShortcutStore + ShortcutController`. The project goes from 8 store/controller pairs to 13+. Each pair requires: a singleton store class, event subscriptions, a controller class with `hostConnected`/`hostDisconnected`, barrel file exports, and event name constants in both Go and TypeScript. The boilerplate adds up and the store/controller pattern becomes a maintenance burden.

**Prevention:**
1. **Not every feature needs its own store.** MusicBrainz data is view-local (only relevant when the user is browsing MusicBrainz) — it can live as component-local state in the MusicBrainz browser component, not a global store.
2. **Smart playlist rules are part of playlist data** — extend the existing `PlaylistStore` rather than creating a new store.
3. **Keyboard shortcuts and layout config are extensions of the existing config system.** Extend `Config` (backend) and load via the existing config binding. The frontend reads once at startup; changes are rare.
4. **Only create a new store when the data is: (a) shared across multiple components, (b) updated from backend events, AND (c) needed across different views.** If data is view-local or rarely changes, use component state or a simple module-level variable.

**Confidence:** HIGH — the store/controller pattern is confirmed from the codebase. The frontend already has 8 stores for ~15 components.

---

### Pitfall 15: Event Name Constants Drift with Many New Events

**Feature area:** All features (cross-cutting)
**What goes wrong:** Adding tag editing, scan cancellation, smart playlists, MusicBrainz, shortcuts, layout, and plugins requires ~15-20 new event names. Each must be added to both `backend/events/events.go` and `frontend/src/events.ts`. The AST-based codegen (`genevents`) generates TypeScript from Go, but only if you run `go generate`. Forgetting to regenerate after adding an event in Go leaves the TypeScript file stale. The pre-commit hook checks for codegen freshness, but a developer working in the frontend first (adding a TypeScript event) has no corresponding Go constant.

**Prevention:**
1. **Always add events in Go first.** The codegen flows Go → TypeScript. Never add events in TypeScript manually. This is already documented but worth reinforcing with 15+ new events being added.
2. **Run `make generate` as part of the development workflow,** not just before commit. The pre-commit hook is a safety net, not the primary mechanism.
3. **Group new events by feature area** in `events.go` with section comments, matching the existing pattern (Playback, Queue, Config, Playlist, Library). Add new groups: `Tag`, `SmartPlaylist`, `MusicBrainz`, `Layout`, `Plugin`, `Shortcuts`, `Scan`.
4. **Consider adding a build-time check** that counts events in both files and fails if they differ. The current codegen check verifies file freshness but not content correctness if someone manually edited the TypeScript.

**Confidence:** HIGH — the codegen pattern and its fragility are documented in CONCERNS.md.

---

## Phase-Specific Warnings

| Phase Topic | Likely Pitfall | Mitigation |
|-------------|---------------|------------|
| Tag editing | File corruption during write (P1) | Write-to-temp-then-rename; block writes on playing file |
| Tag editing | SQLite contention with scan (P9) | Block tag edits during active scans |
| Scan cancellation | Orphan cleanup on partial scan (P3) | Skip orphan cleanup when cancelled |
| Scan cancellation | Goroutine leaks on cancel (P3) | Drain all channels; use scan-specific context |
| Gapless playback | Lock ordering deadlock (P2) | Pre-decode in separate goroutine; suppress callback during transition |
| Crossfade | Sample rate mismatch (P11) | Always crossfade post-resample streamers |
| Smart playlists | Full-table scans (P6) | Lazy evaluation; dedicated indexed queries |
| Smart playlists | Missing play_count column (P6) | Schema migration with increment on playback |
| MusicBrainz browser | Rate limiting (P5) | 1 req/s rate limiter; aggressive caching; proper User-Agent |
| MusicBrainz browser | Schema confusion (P10) | Separate cache table; display-only DTOs |
| Keyboard shortcuts | Shadow DOM event boundaries (P7) | Capture phase listener; composedPath() for target detection |
| Keyboard shortcuts | WebView key interception (P7) | Document reserved keys; skip shortcuts on input focus |
| Layout customization | Component size assumptions (P8) | Container queries; component size constraints metadata |
| Layout customization | Virtual scrolling breakage (P8) | Explicit height enforcement for virtualized sections |
| Plugin system | Host crash from plugin panic (P4) | Embedded scripting runtime (not native Go plugins) |
| Plugin system | SQLite contention (P4) | Read-only connection for plugins; namespaced events |
| Config additions | Backward compatibility (P12) | Defaults for all new fields; test with old config files |
| All features | Event name drift (P15) | Go-first workflow; grouped event constants; codegen validation |
| All features | Store/controller proliferation (P14) | Extend existing stores; use component-local state where appropriate |
| MusicBrainz + Layout | Event payload size (P13) | Use bindings for data; events for notifications only |

---

## Feature Interaction Matrix

Some pitfalls emerge from the interaction between features, not from individual features:

| Feature A | Feature B | Interaction Pitfall |
|-----------|-----------|-------------------|
| Tag editing | Library scan | Writer contention + file access races (P9) |
| Tag editing | Gapless playback | Can't write tags on file being played or pre-decoded (P1) |
| Smart playlists | Tag editing | Smart playlists must re-evaluate after tag edits change matching criteria |
| Smart playlists | Library scan | Smart playlists must re-evaluate after scan adds/removes tracks (P6) |
| Gapless playback | Plugin system | Plugins must not interfere with speaker lock during transitions (P4 + P2) |
| Layout customization | Plugin system | Plugins may want to register custom layout sections — layout system must be extensible |
| Keyboard shortcuts | Plugin system | Plugins may want to register custom shortcuts — shortcut system must be extensible |
| MusicBrainz browser | Tag editing | Future feature: apply MusicBrainz metadata to local files (tag write from MB data) |

---

## Sources

- **Codebase analysis:** `backend/player/player.go` (lock ordering, callback pattern, BufferedStreamer), `backend/library/library.go` (scan pipeline phases, context cancellation), `backend/database/` (schema, single-writer), `frontend/index.ts` (keyboard handling, navigation), `frontend/index.html` + `index.css` (hardcoded grid layout)
- **MusicBrainz rate limiting:** https://musicbrainz.org/doc/MusicBrainz_API/Rate_Limiting — confirmed 1 req/s per IP, User-Agent requirement, 503 on violation
- **id3v2 Go library:** https://github.com/n10v/id3v2 — 359 stars, supports ID3v2.3/v2.4 read/write, last release v2.1.4 (Feb 2023)
- **beep wiki (composing/controlling):** https://github.com/gopxl/beep/wiki/Composing-and-controlling — confirmed speaker.Lock() usage, beep.Seq for chaining, beep.Ctrl for pause, effects.Volume for volume control
- **Project context:** `.planning/PROJECT.md`, `.planning/codebase/ARCHITECTURE.md`, `.planning/codebase/CONCERNS.md`, `.planning/codebase/INTEGRATIONS.md`

---

*Pitfalls research: 2026-03-06*
