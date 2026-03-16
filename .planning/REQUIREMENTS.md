# Requirements: YellowJacket

**Defined:** 2026-03-16
**Core Value:** The music player works reliably and feels solid — every interaction is correct, responsive, and trustworthy.

## v1.2 Requirements

Requirements for v1.2 Tag Editing milestone. Each maps to roadmap phases.

### Schema & Safety

- [ ] **SCHEMA-01**: FTS5 search_index migrated to `contentless_delete=1` for safe row-level updates
- [ ] **SCHEMA-02**: Atomic file write utility (write-to-temp-then-rename in same directory)

### Tag Writing

- [ ] **WRITE-01**: Write metadata tags to MP3 files via ID3v2 (title, artist, album, genre, year, track#, disc#, composer)
- [ ] **WRITE-02**: Write metadata tags to FLAC files via Vorbis Comments
- [ ] **WRITE-03**: Write metadata tags to OGG Vorbis files via custom page rewriter
- [ ] **WRITE-04**: Embed cover art image (JPEG/PNG) in MP3 and FLAC files
- [ ] **WRITE-05**: All file writes use atomic write-to-temp-then-rename to prevent corruption
- [ ] **WRITE-06**: Currently-playing file is stopped before writing (player safety)

### Database Sync

- [ ] **SYNC-01**: After tag write, update DB entities inline (upsert-and-relink for artist, album, genre)
- [ ] **SYNC-02**: After tag write, update FTS5 search index for affected tracks
- [ ] **SYNC-03**: Orphaned entities (artists, albums, genres no longer referenced) cleaned up
- [ ] **SYNC-04**: Scan pipeline paused during tag writes to prevent race conditions

### Single Track Edit

- [ ] **EDIT-01**: User can open tag editor for a single track from context menu or detail view
- [ ] **EDIT-02**: Editor shows all 8 editable fields with current values pre-populated
- [ ] **EDIT-03**: Editor shows current cover art with option to replace from image file
- [ ] **EDIT-04**: Saving writes tags to file, updates DB, updates FTS5, and refreshes all views immediately

### Batch Edit

- [ ] **BATCH-01**: User can select multiple tracks and open batch editor
- [ ] **BATCH-02**: Batch editor uses three-state field model (keep original / set value / clear field)
- [ ] **BATCH-03**: Batch editor shows progress indicator for large selections
- [ ] **BATCH-04**: User can set cover art for all selected tracks at once

## Future Requirements

Deferred to future milestones. Tracked but not in current roadmap.

### Tag Editing (v2+)

- **EDIT-F01**: Undo/redo for tag edits
- **EDIT-F02**: Auto-capitalize and clean tag values
- **EDIT-F03**: Filename-to-tag inference (parse "Artist - Title.mp3" patterns)
- **EDIT-F04**: Tag-to-filename rename based on template
- **EDIT-F05**: WAV tag writing

### Smart Playlists (Deferred from v1.1)

- **SMRT-01**: User can create a smart playlist with filter rules (genre, year, artist, album, title)
- **SMRT-02**: Multiple rules combine with AND logic
- **SMRT-03**: User can set random ordering and result limit ("Random 50 Jazz tracks")

### Gapless Playback (Deferred from v1.1)

- **GAP-01**: Tracks transition seamlessly with no audible silence gap (gapless playback)
- **GAP-02**: Next track is pre-decoded before current track ends
- **GAP-03**: User can enable/disable crossfade with configurable duration

### Other Deferred

- **MB-01**: MusicBrainz artist/discography browser
- **LAYOUT-01**: Layout customization system (section-based UI)
- **PLUG-01**: Plugin system (extensibility foundation)

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
|---------|--------|
| OGG Vorbis tag writing (if infeasible) | No pure-Go library exists; custom OGG page rewriter may prove too complex — treat as stretch goal |
| WAV metadata editing | Rarely needed, low priority |
| Auto-tag from MusicBrainz | Complex matching logic — Picard's domain |
| Batch rename files from tags | High risk of data loss; defer to v2+ with undo support |
| Lossless audio re-encoding | Not a tag editor concern |
| Parallel library scanning | SQLite single-writer constraint |

## Traceability

Which phases cover which requirements. Updated during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| SCHEMA-01 | — | Pending |
| SCHEMA-02 | — | Pending |
| WRITE-01 | — | Pending |
| WRITE-02 | — | Pending |
| WRITE-03 | — | Pending |
| WRITE-04 | — | Pending |
| WRITE-05 | — | Pending |
| WRITE-06 | — | Pending |
| SYNC-01 | — | Pending |
| SYNC-02 | — | Pending |
| SYNC-03 | — | Pending |
| SYNC-04 | — | Pending |
| EDIT-01 | — | Pending |
| EDIT-02 | — | Pending |
| EDIT-03 | — | Pending |
| EDIT-04 | — | Pending |
| BATCH-01 | — | Pending |
| BATCH-02 | — | Pending |
| BATCH-03 | — | Pending |
| BATCH-04 | — | Pending |

**Coverage:**
- v1.2 requirements: 20 total
- Mapped to phases: 0
- Unmapped: 20

---
*Requirements defined: 2026-03-16*
*Last updated: 2026-03-16 after initial definition*
