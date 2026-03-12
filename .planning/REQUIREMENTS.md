# Requirements: YellowJacket

**Defined:** 2026-03-06
**Core Value:** The music player works reliably and feels solid — every interaction is correct, responsive, and trustworthy.

## v1.1 Requirements

Requirements for v1.1 Multi-Library Support milestone. Each maps to roadmap phases.

### Scan Cancellation (Phase 9 — Complete)

- [x] **SCAN-01**: User can cancel an in-progress library scan via a cancel button
- [x] **SCAN-02**: Cancelled scan stops gracefully without corrupting the database
- [x] **SCAN-03**: User can pause a library scan and resume it without re-scanning processed files

### Keyboard Shortcuts (Phase 9 — Complete)

- [x] **KEY-01**: Default keybindings work out of box (play/pause, next/prev, volume, search focus, queue toggle, shuffle, repeat)
- [x] **KEY-02**: User can customize all keyboard shortcuts via a visual settings UI
- [x] **KEY-03**: Shortcut conflicts are detected and warned about when rebinding
- [x] **KEY-04**: Shortcuts are scoped — different bindings apply based on focused component (track list vs player vs global)
- [x] **KEY-05**: Shortcuts are disabled when text input has focus (except Escape to blur)

### Library Management

- [x] **LIB-01**: User can add a new library directory via a folder picker dialog
- [x] **LIB-02**: User can rename a library (display name)
- [x] **LIB-03**: User can remove a library — tracks are deleted from DB, shared entities (artists, albums, genres) are cleaned up only if no other library references them
- [x] **LIB-04**: Libraries are stored in SQLite (not TOML config) with CRUD through the UI
- [x] **LIB-05**: Existing single-directory config is migrated seamlessly to the libraries table on first run after upgrade
- [ ] **LIB-06**: Library list is displayed in a management UI (settings or sidebar section)

### Library Scanning

- [x] **LSCAN-01**: User can trigger a scan for a specific library (not all-or-nothing)
- [x] **LSCAN-02**: Scanning is sequential — only one library scans at a time (SQLite single-writer)
- [x] **LSCAN-03**: Scan progress UI shows which library is being scanned
- [x] **LSCAN-04**: Existing scan cancellation and pause/resume work per-library
- [x] **LSCAN-05**: Audio files are associated with their library via `library_id` foreign key

### Unified Presentation

- [ ] **VIEW-01**: Default view shows tracks from all libraries merged (unified presentation)
- [ ] **VIEW-02**: User can filter the track list to show only tracks from a specific library
- [ ] **VIEW-03**: Browse views (albums, artists, genres) work across all libraries or filtered to one
- [ ] **VIEW-04**: Search (FTS5) searches across all libraries or respects the active library filter

### Playlists & Queue

- [ ] **PLAY-01**: Playlists can contain tracks from multiple libraries (cross-library playlists)
- [ ] **PLAY-02**: When a library is removed, playlist entries for that library's tracks become phantom tracks (preserved with cached metadata, not cascade-deleted)
- [ ] **PLAY-03**: Phantom tracks are visually distinguished in playlist views (e.g., greyed out, icon indicator)
- [x] **PLAY-04**: Queue tracks from a removed library are cascade-deleted (queue is ephemeral)

### Data Integrity

- [x] **DATA-01**: Schema migration adds `libraries` table and `library_id` FK on `audio_files`
- [x] **DATA-02**: Orphan cleanup after library removal: reference-counting bottom-up deletes for artists, albums, genres only referenced by removed library's tracks
- [x] **DATA-03**: FTS5 index entries for removed tracks are cleaned up (handling contentless table limitations)
- [x] **DATA-04**: All library operations are transactional — no partial state on failure

## Future Requirements

Deferred to future milestones. Tracked but not in current roadmap.

### Tag Editing (Deferred from v1.1)

- **TAG-01**: User can edit a single track's metadata (title, artist, album, genre, year, track number)
- **TAG-02**: User can batch edit multiple selected tracks' shared fields
- **TAG-03**: Tag changes are written to actual audio files (MP3 via ID3v2, FLAC via Vorbis Comments)
- **TAG-04**: Database and FTS5 search index update after tag writes without requiring a full rescan
- **TAG-05**: User can set or replace embedded cover art from an image file
- **TAG-06**: Tag writes use write-to-temp-then-rename to prevent file corruption
- **TAG-07**: Tag editing is blocked for currently-playing files (queued for after playback stops)

### Tag Editing (v2+)

- **TAG-F01**: Undo/redo for tag edits
- **TAG-F02**: Auto-capitalize and clean tag values
- **TAG-F03**: Filename-to-tag inference (parse "Artist - Title.mp3" patterns)
- **TAG-F04**: Tag-to-filename rename based on template

### Smart Playlists (Deferred from v1.1)

- **SMRT-01**: User can create a smart playlist with filter rules (genre, year, artist, album, title)
- **SMRT-02**: Multiple rules combine with AND logic
- **SMRT-03**: User can set random ordering and result limit ("Random 50 Jazz tracks")
- **SMRT-04**: Smart playlists appear in the sidebar alongside regular playlists
- **SMRT-05**: Smart playlist rules are persisted and survive app restart

### Smart Playlists (v2+)

- **SMRT-F01**: Play count tracking for smart playlist rules
- **SMRT-F02**: Rating system for smart playlist rules
- **SMRT-F03**: OR logic and nested boolean groups
- **SMRT-F04**: Sort order control in rule definition
- **SMRT-F05**: Auto-update smart playlists on library changes

### Gapless Playback (Deferred from v1.1)

- **GAP-01**: Tracks transition seamlessly with no audible silence gap (gapless playback)
- **GAP-02**: Next track is pre-decoded before current track ends
- **GAP-03**: User can enable/disable crossfade with configurable duration (1-10 seconds)
- **GAP-04**: Crossfade only applies on auto-advance, not manual skip

### Gapless Playback (v2+)

- **GAP-F01**: Per-album gapless (disable crossfade within albums)
- **GAP-F02**: ReplayGain normalization
- **GAP-F03**: Fade-in on play, fade-out on pause

### MusicBrainz Browser (Deferred from v1.1)

- **MB-01**: User can search for artists by name and view results
- **MB-02**: User can browse an artist's discography (release groups — albums, EPs, singles)
- **MB-03**: User can view tracks on a specific release
- **MB-04**: User can view different editions of a release group (pressings, reissues)
- **MB-05**: API responses are cached in SQLite (24hr for searches, 7 days for entities)
- **MB-06**: Album cover art is displayed from the Cover Art Archive
- **MB-07**: Rate limiting (1 req/sec) is enforced with proper User-Agent header

### Layout Customization (Deferred from v1.1)

- **LAYOUT-01**: User can resize sidebar and queue panels via drag handles
- **LAYOUT-02**: Panel sizes persist across app restarts
- **LAYOUT-03**: User can show/hide sidebar sections and queue panel
- **LAYOUT-04**: User can choose which component is displayed in each layout section (MusicBee-style)
- **LAYOUT-05**: Components declare size constraints (min/max dimensions, aspect ratio compatibility)
- **LAYOUT-06**: Layout presets available (Compact, Full, Mini player) with quick switch

### Layout Customization (v2+)

- **LAYOUT-F01**: Detachable panels (pop out to separate window)

### Plugin System (Deferred from v1.1)

- **PLUG-01**: Plugin API is defined — plugins can access events, player state, queue, library data
- **PLUG-02**: JS/TS plugin bundles are loaded from user plugin directory at runtime
- **PLUG-03**: Plugins can register UI components into the layout system
- **PLUG-04**: Plugin manifest file defines name, version, permissions, hooks, and UI components
- **PLUG-05**: Plugins can have their own persistent configuration
- **PLUG-06**: One example plugin ships demonstrating the API

### Plugin System (v2+)

- **PLUG-F01**: Plugin marketplace/registry for discovery and installation
- **PLUG-F02**: Dynamic Go plugin loading for backend extensions
- **PLUG-F03**: Plugin permissions and sandboxing model

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
|---------|--------|
| Separate databases per library | Defeats unified presentation, overly complex |
| Auto-dedup across libraries | Complex matching logic, not table stakes |
| User access control per library | Desktop app, single user |
| Parallel library scanning | SQLite single-writer makes it pointless |
| OGG Vorbis tag writing | No mature pure-Go write library exists |
| WAV metadata editing | Rarely needed, low priority |
| Auto-tag from MusicBrainz | Complex matching logic — Picard's domain |
| DSP effects chain (equalizer, reverb) | Scope explosion — separate feature area |
| Go `plugin` package for backend plugins | Linux-only, version-fragile, widely considered broken |
| Free-form drag-and-drop layout | Overwhelming complexity; section-based approach is better |
| Global OS-level hotkeys | Platform-specific, conflicts with OS shortcuts; MPRIS2 handles media keys |
| Mobile-responsive layout | Desktop app with fixed minimum size |

## Traceability

Which phases cover which requirements. Updated during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| SCAN-01 | Phase 9 | Complete |
| SCAN-02 | Phase 9 | Complete |
| SCAN-03 | Phase 9 | Complete |
| KEY-01 | Phase 9 | Complete |
| KEY-02 | Phase 9 | Complete |
| KEY-03 | Phase 9 | Complete |
| KEY-04 | Phase 9 | Complete |
| KEY-05 | Phase 9 | Complete |
| LIB-01 | Phase 12 | Complete |
| LIB-02 | Phase 12 | Complete |
| LIB-03 | Phase 12 | Complete |
| LIB-04 | Phase 10 | Complete |
| LIB-05 | Phase 10 | Complete |
| LIB-06 | Phase 12 | Pending |
| LSCAN-01 | Phase 11 | Complete |
| LSCAN-02 | Phase 11 | Complete |
| LSCAN-03 | Phase 11 | Complete |
| LSCAN-04 | Phase 11 | Complete |
| LSCAN-05 | Phase 10 | Complete |
| VIEW-01 | Phase 13 | Pending |
| VIEW-02 | Phase 13 | Pending |
| VIEW-03 | Phase 13 | Pending |
| VIEW-04 | Phase 13 | Pending |
| PLAY-01 | Phase 13 | Pending |
| PLAY-02 | Phase 13 | Pending |
| PLAY-03 | Phase 13 | Pending |
| PLAY-04 | Phase 12 | Complete |
| DATA-01 | Phase 10 | Complete |
| DATA-02 | Phase 12 | Complete |
| DATA-03 | Phase 12 | Complete |
| DATA-04 | Phase 10 | Complete |

**Coverage:**
- v1.1 requirements: 31 total (19 complete + 12 pending)
- Mapped to phases: 31/31 ✓ (Phase 9: 8, Phase 10: 5, Phase 11: 4, Phase 12: 6 of 7 — 12 pending in Phase 12-13)
- No orphaned requirements

---
*Requirements defined: 2026-03-06*
*Last updated: 2026-03-08 — restructured for multi-library support, deferred TAG/SMRT/GAP/MB/LAYOUT/PLUG*
