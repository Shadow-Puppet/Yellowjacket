# Requirements: YellowJacket

**Defined:** 2026-03-06
**Core Value:** The music player works reliably and feels solid — every interaction is correct, responsive, and trustworthy.

## v1.1 Requirements

Requirements for v1.1 Features & Extensibility milestone. Each maps to roadmap phases.

### Scan Cancellation

- [ ] **SCAN-01**: User can cancel an in-progress library scan via a cancel button
- [ ] **SCAN-02**: Cancelled scan stops gracefully without corrupting the database
- [ ] **SCAN-03**: User can pause a library scan and resume it without re-scanning processed files

### Keyboard Shortcuts

- [ ] **KEY-01**: Default keybindings work out of box (play/pause, next/prev, volume, search focus, queue toggle, shuffle, repeat)
- [ ] **KEY-02**: User can customize all keyboard shortcuts via a visual settings UI
- [ ] **KEY-03**: Shortcut conflicts are detected and warned about when rebinding
- [ ] **KEY-04**: Shortcuts are scoped — different bindings apply based on focused component (track list vs player vs global)
- [ ] **KEY-05**: Shortcuts are disabled when text input has focus (except Escape to blur)

### Tag Editing

- [ ] **TAG-01**: User can edit a single track's metadata (title, artist, album, genre, year, track number)
- [ ] **TAG-02**: User can batch edit multiple selected tracks' shared fields
- [ ] **TAG-03**: Tag changes are written to actual audio files (MP3 via ID3v2, FLAC via Vorbis Comments)
- [ ] **TAG-04**: Database and FTS5 search index update after tag writes without requiring a full rescan
- [ ] **TAG-05**: User can set or replace embedded cover art from an image file
- [ ] **TAG-06**: Tag writes use write-to-temp-then-rename to prevent file corruption
- [ ] **TAG-07**: Tag editing is blocked for currently-playing files (queued for after playback stops)

### Smart Playlists

- [ ] **SMRT-01**: User can create a smart playlist with filter rules (genre, year, artist, album, title)
- [ ] **SMRT-02**: Multiple rules combine with AND logic
- [ ] **SMRT-03**: User can set random ordering and result limit ("Random 50 Jazz tracks")
- [ ] **SMRT-04**: Smart playlists appear in the sidebar alongside regular playlists
- [ ] **SMRT-05**: Smart playlist rules are persisted and survive app restart

### Gapless Playback

- [ ] **GAP-01**: Tracks transition seamlessly with no audible silence gap (gapless playback)
- [ ] **GAP-02**: Next track is pre-decoded before current track ends
- [ ] **GAP-03**: User can enable/disable crossfade with configurable duration (1-10 seconds)
- [ ] **GAP-04**: Crossfade only applies on auto-advance, not manual skip

### MusicBrainz Browser

- [ ] **MB-01**: User can search for artists by name and view results
- [ ] **MB-02**: User can browse an artist's discography (release groups — albums, EPs, singles)
- [ ] **MB-03**: User can view tracks on a specific release
- [ ] **MB-04**: User can view different editions of a release group (pressings, reissues)
- [ ] **MB-05**: API responses are cached in SQLite (24hr for searches, 7 days for entities)
- [ ] **MB-06**: Album cover art is displayed from the Cover Art Archive
- [ ] **MB-07**: Rate limiting (1 req/sec) is enforced with proper User-Agent header

### Layout Customization

- [ ] **LAYOUT-01**: User can resize sidebar and queue panels via drag handles
- [ ] **LAYOUT-02**: Panel sizes persist across app restarts
- [ ] **LAYOUT-03**: User can show/hide sidebar sections and queue panel
- [ ] **LAYOUT-04**: User can choose which component is displayed in each layout section (MusicBee-style)
- [ ] **LAYOUT-05**: Components declare size constraints (min/max dimensions, aspect ratio compatibility)
- [ ] **LAYOUT-06**: Layout presets available (Compact, Full, Mini player) with quick switch

### Plugin System

- [ ] **PLUG-01**: Plugin API is defined — plugins can access events, player state, queue, library data
- [ ] **PLUG-02**: JS/TS plugin bundles are loaded from user plugin directory at runtime
- [ ] **PLUG-03**: Plugins can register UI components into the layout system
- [ ] **PLUG-04**: Plugin manifest file defines name, version, permissions, hooks, and UI components
- [ ] **PLUG-05**: Plugins can have their own persistent configuration
- [ ] **PLUG-06**: One example plugin ships demonstrating the API

## Future Requirements

Deferred to future milestones. Tracked but not in current roadmap.

### Tag Editing (v2+)

- **TAG-F01**: Undo/redo for tag edits
- **TAG-F02**: Auto-capitalize and clean tag values
- **TAG-F03**: Filename-to-tag inference (parse "Artist - Title.mp3" patterns)
- **TAG-F04**: Tag-to-filename rename based on template

### Smart Playlists (v2+)

- **SMRT-F01**: Play count tracking for smart playlist rules
- **SMRT-F02**: Rating system for smart playlist rules
- **SMRT-F03**: OR logic and nested boolean groups
- **SMRT-F04**: Sort order control in rule definition
- **SMRT-F05**: Auto-update smart playlists on library changes

### Gapless Playback (v2+)

- **GAP-F01**: Per-album gapless (disable crossfade within albums)
- **GAP-F02**: ReplayGain normalization
- **GAP-F03**: Fade-in on play, fade-out on pause

### MusicBrainz Browser (v2+)

- **MB-F01**: Link local tracks to MusicBrainz recordings (MBID association)
- **MB-F02**: Search recordings (find specific songs across releases)

### Layout Customization (v2+)

- **LAYOUT-F01**: Detachable panels (pop out to separate window)

### Plugin System (v2+)

- **PLUG-F01**: Plugin marketplace/registry for discovery and installation
- **PLUG-F02**: Dynamic Go plugin loading for backend extensions
- **PLUG-F03**: Plugin permissions and sandboxing model

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
|---------|--------|
| OGG Vorbis tag writing | No mature pure-Go write library exists |
| WAV metadata editing | Rarely needed, low priority |
| Auto-tag from MusicBrainz | Complex matching logic — Picard's domain, not a browser feature |
| Write data to MusicBrainz | Requires OAuth and community guidelines compliance |
| DSP effects chain (equalizer, reverb) | Scope explosion — separate feature area |
| Go `plugin` package for backend plugins | Linux-only, version-fragile, widely considered broken |
| Free-form drag-and-drop layout | Overwhelming complexity; section-based approach is better |
| Global OS-level hotkeys | Platform-specific, conflicts with OS shortcuts; MPRIS2 handles media keys |
| Mobile-responsive layout | Desktop app with fixed minimum size |
| Plugin binary distribution | Source-based (JS bundles) is safer and more portable |

## Traceability

Which phases cover which requirements. Updated during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| SCAN-01 | Phase 9 | Pending |
| SCAN-02 | Phase 9 | Pending |
| SCAN-03 | Phase 9 | Pending |
| KEY-01 | Phase 9 | Pending |
| KEY-02 | Phase 9 | Pending |
| KEY-03 | Phase 9 | Pending |
| KEY-04 | Phase 9 | Pending |
| KEY-05 | Phase 9 | Pending |
| TAG-01 | Phase 10 | Pending |
| TAG-02 | Phase 10 | Pending |
| TAG-03 | Phase 10 | Pending |
| TAG-04 | Phase 10 | Pending |
| TAG-05 | Phase 10 | Pending |
| TAG-06 | Phase 10 | Pending |
| TAG-07 | Phase 10 | Pending |
| SMRT-01 | Phase 11 | Pending |
| SMRT-02 | Phase 11 | Pending |
| SMRT-03 | Phase 11 | Pending |
| SMRT-04 | Phase 11 | Pending |
| SMRT-05 | Phase 11 | Pending |
| GAP-01 | Phase 12 | Pending |
| GAP-02 | Phase 12 | Pending |
| GAP-03 | Phase 12 | Pending |
| GAP-04 | Phase 12 | Pending |
| MB-01 | Phase 13 | Pending |
| MB-02 | Phase 13 | Pending |
| MB-03 | Phase 13 | Pending |
| MB-04 | Phase 13 | Pending |
| MB-05 | Phase 13 | Pending |
| MB-06 | Phase 13 | Pending |
| MB-07 | Phase 13 | Pending |
| LAYOUT-01 | Phase 14 | Pending |
| LAYOUT-02 | Phase 14 | Pending |
| LAYOUT-03 | Phase 14 | Pending |
| LAYOUT-04 | Phase 14 | Pending |
| LAYOUT-05 | Phase 14 | Pending |
| LAYOUT-06 | Phase 14 | Pending |
| PLUG-01 | Phase 14 | Pending |
| PLUG-02 | Phase 14 | Pending |
| PLUG-03 | Phase 14 | Pending |
| PLUG-04 | Phase 14 | Pending |
| PLUG-05 | Phase 14 | Pending |
| PLUG-06 | Phase 14 | Pending |

**Coverage:**
- v1.1 requirements: 43 total
- Mapped to phases: 43
- Unmapped: 0 ✓

---
*Requirements defined: 2026-03-06*
*Last updated: 2026-03-06 — traceability updated with phase mappings*
