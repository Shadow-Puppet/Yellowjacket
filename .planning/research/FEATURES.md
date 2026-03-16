# Feature Landscape: Tag Editing

**Domain:** Metadata tag editing in desktop music players
**Researched:** 2026-03-16
**Confidence:** HIGH (based on analysis of MusicBee, foobar2000, Kid3, Mp3tag, Picard patterns + Hydrogenaudio tag standards + existing YellowJacket codebase)

## How Desktop Music Players Implement Tag Editing

### Reference Players Analyzed

| Player | Single Edit | Batch Edit | Cover Art Edit | Auto-Tag | Tag Format Handling |
|--------|------------|------------|---------------|----------|-------------------|
| foobar2000 | Properties dialog | Multi-select → Properties (shared fields) | Embed/remove from Properties | Via plugins | ID3v2, Vorbis, APEv2; configurable write format |
| MusicBee | Inline + dialog | Multi-select → Edit panel (keep/clear/set) | Drag-drop + file picker + paste | Built-in | ID3v2.3/2.4, Vorbis; auto-convert on write |
| Kid3 | Side panel + dialog | Multi-select → panel applies to all | File picker + paste + drag | MusicBrainz/Discogs | ID3v1/v2, Vorbis, APEv2; shows raw frames |
| Mp3tag | List view + panel | Inherent (panel always applies to selection) | Drag-drop + file picker + clipboard | Tag Sources | All formats; extended tag view |
| Picard | Panel per file/album | Album-level batch via MusicBrainz match | Automatic via MusicBrainz + manual | Core feature | All formats; submission to MusicBrainz |

### Common Patterns Across All Players

**Single-track editing:**
- Dialog/panel with labeled fields, plain text inputs
- Title, artist, album shown prominently (larger/bolder)
- Cover art displayed alongside fields (150-250px)
- Numeric fields (year, track #, disc #) use number inputs or constrained text
- Genre usually free-text (not dropdown — genre lists are opinionated and incomplete)
- Non-editable properties shown separately (bitrate, sample rate, file path, file size)
- Save button writes to file → updates database
- Cancel discards all changes

**Batch editing (the critical UX challenge):**
- Select multiple tracks → open editor
- Fields show current value if identical across selection, blank/placeholder if mixed
- A "keep" / "don't change" / "mixed" indicator distinguishes "empty because cleared" from "empty because mixed"
- User types a value → it applies to ALL selected tracks on save
- Fields left unchanged preserve each track's individual value
- Common pattern: three-state per field — "keep original" (default), "set to value", "clear"
- Track number is special: batch edit typically excludes it (each track needs unique number) OR offers auto-number (sequential from N)

**Cover art editing:**
- Display current embedded art (or "no cover" placeholder)
- Replace from file: file picker (JPEG, PNG)
- Remove embedded art (less common, but available in Kid3/Mp3tag)
- Cover art in batch edit: applies same image to all selected tracks (common for fixing an album)
- No crop/resize UI — users prepare images externally
- Players typically accept any size but recommend 500-1000px square

**File safety:**
- Write-to-temp-then-rename (atomic write) is universal best practice
- Some players (foobar2000) create backups before writing
- All players update their internal database after successful file write (no rescan)

### Universal Editable Fields (from Hydrogenaudio Tag Mapping + player analysis)

**Basic (ID3v1-level, universal compatibility):**
- Title, Artist, Album, Year, Genre, Track Number, Comment

**Standard (ID3v2/Vorbis, widely supported):**
- Album Artist, Composer, Disc Number, Track Total, Disc Total, Lyrics

**Extended (advanced users, format-dependent):**
- BPM, Initial Key, Mood, Label, Catalog Number, ISRC, MusicBrainz IDs

## Table Stakes

Features users expect. Missing = product feels incomplete.

| Feature | Why Expected | Complexity | Dependencies | Notes |
|---------|-------------|------------|--------------|-------|
| Single track tag editing (title, artist, album, genre, year, track#, disc#, composer) | Every player with tag editing supports these 8 fields minimum | Medium | Tag writing library, DB update queries, FTS5 reindex | Existing `track-details` dialog has edit mode UI scaffolded (save is no-op TODO) |
| Write tags to MP3 (ID3v2) | MP3 is the most common format; must-have | High | Need tag writing library (dhowden/tag is read-only) | Format-specific: must write ID3v2.3 or ID3v2.4 frames |
| Write tags to FLAC (Vorbis Comments) | FLAC is the standard lossless format | High | Same writing library | Vorbis comments in FLAC metadata block |
| Write tags to OGG (Vorbis Comments) | Already supported for reading | Medium | Same writing library | Same Vorbis comment format as FLAC |
| Write-to-temp-then-rename | File corruption on crash/power loss = unacceptable data loss | Low | `os.Rename` after writing to temp file | Universal best practice; Go stdlib handles this well |
| Inline DB + FTS5 update after tag write | Users expect immediate UI update; forcing rescan is unacceptable | Medium | UPDATE queries for recordings, artist_credit, release_groups, genres; FTS5 search_index rebuild for affected rows | Must update the `track_metadata` VIEW's source tables |
| Batch editing shared fields across multiple selected tracks | Every tag editor supports this; multi-select already exists in track list | High | Batch editor UI component, backend batch write endpoint, progress tracking | The hard UX problem: mixed-value indicators, three-state fields |
| Save confirmation / error feedback | User must know if write succeeded or failed | Low | Event emission, toast/notification UI | Especially important for read-only files or permission errors |
| Cover art set/replace from image file | Fundamental tag editing feature; cover art is visually prominent | Medium | File picker (already have `FrontendUtil.OpenFileDialog`), image embedding in tag write, cover art cache update | Must update both embedded tag and cover art cache + thumbnails |

## Differentiators

Features that set the product apart. Not expected, but valued.

| Feature | Value Proposition | Complexity | Dependencies | Notes |
|---------|------------------|------------|--------------|-------|
| Album artist field editing | Distinguishes VA compilations; power users expect it | Low | One additional field in edit form; already extracted by `dhowden/tag` | Not in PROJECT.md active list but low-hanging fruit |
| Lyrics field editing | Multi-line text editing for embedded lyrics | Low | Textarea in dialog; lyrics field already in `recordings` schema… wait, it's in `TrackMetadata` struct but not shown in track-details UI | Would need multiline input; niche but straightforward |
| Comment field editing | Standard tag field, some users store notes | Low | Already extracted, just needs UI input | Very low effort to include |
| Auto-number tracks in batch edit | Select album tracks → auto-assign sequential track numbers | Low | Frontend logic to generate sequential numbers, apply in batch write | Huge time-saver when retagging an album |
| Dirty indicator / unsaved changes warning | Prevent accidental dialog close with unsaved edits | Low | Track `editValues` diff vs original values | MusicBee and foobar2000 both do this |
| Undo last tag write (restore from backup) | Safety net for mistakes; builds user trust | Medium | Write original tag values to a backup store before overwriting | Most players don't do this — would be a genuine differentiator |
| Cover art remove (strip embedded art) | Some users want to remove bloated embedded art | Low | Write tags without picture data | Available in Kid3/Mp3tag but not most players |
| Cover art paste from clipboard | Quick workflow: copy image from browser → paste into editor | Medium | Clipboard API in WebView, image data extraction | MusicBee supports this; convenient for web-sourced art |
| Progress indicator for batch operations | Visual feedback during multi-file writes (batch of 20+ tracks) | Low | Progress event emission, progress bar in UI | Important when writing to many files (can take seconds per file for FLAC) |
| Total Tracks / Total Discs fields | Part of standard tag spec; power users tag these | Low | Two additional number fields; already in `TrackMetadata` struct | Mp3tag and Kid3 expose these; foobar2000 uses "X/Y" format |

## Anti-Features

Features to explicitly NOT build.

| Anti-Feature | Why Avoid | What to Do Instead |
|--------------|-----------|-------------------|
| Inline editing in track list columns | Extremely complex (virtual scrolling + inline inputs + focus management + multi-select conflicts); fragile UX | Use the existing modal dialog approach — click to open editor. This is what foobar2000 does. |
| MusicBrainz auto-tagging / lookup | Massive scope expansion (API integration, fuzzy matching, network dependency); separate milestone material | Defer to future "MusicBrainz browser" milestone already in PROJECT.md |
| Genre dropdown with predefined list | Genre lists are subjective, never complete, frustrate users who use custom genres | Free-text input with optional suggestions from existing genres in DB (future enhancement) |
| Tag format conversion (ID3v1→v2, strip APEv2) | Edge case tool feature; desktop tagger territory (Mp3tag) | Write the "correct" format for each file type; don't expose format internals to users |
| Raw tag frame editing | Power-user-only feature; complex UI for marginal value | Edit semantic fields (title, artist, etc.); abstract away ID3 frames vs Vorbis comments |
| Custom/arbitrary tag field editing | Requires extensible UI, arbitrary field names, format-specific storage concerns | Support the standard fields; users with custom tags use Mp3tag |
| Filename renaming from tags | Common in dedicated taggers (Mp3tag, Kid3) but orthogonal to tag editing; adds file system mutation risk | Out of scope; would need separate file operations system |
| ReplayGain scanning/writing | Separate audio analysis feature, not tag editing | Future milestone if ever; requires DSP analysis |
| Drag-and-drop cover art from external apps | Complex browser/WebView drag interop; unreliable across platforms | File picker is the reliable universal approach |
| Multi-value field editing (multiple artists/genres as separate entries) | ID3v2 and Vorbis support multiple values per field, but the UI complexity is enormous | Store as single string; genre already uses `||` separator internally |

## Feature Dependencies

```
Single Track Edit ──→ Tag Writing Library (MP3/FLAC/OGG)
                  ──→ DB Update Queries (recordings, artist_credit, release_groups, genres)
                  ──→ FTS5 Reindex (search_index)
                  ──→ Event Emission (UI refresh)

Batch Edit ────────→ Single Track Edit (batch = N × single with shared values)
           ────────→ Mixed-value UI (three-state field indicators)
           ────────→ Multi-select (already exists in track-list)

Cover Art Edit ───→ Tag Writing Library (picture frame embedding)
               ───→ Cover Art Cache Update (saveCoverArt + thumbnail generation)
               ───→ File Picker Dialog (already exists: FrontendUtil.OpenFileDialog)

Write Safety ─────→ Temp file + os.Rename (no dependencies on existing code)

DB Update ────────→ Existing schema: recordings, artist_credit, artists,
                    release_groups, release_group_recordings, genres, genre_recordings,
                    cover_art, audio_files
              ────→ FTS5 search_index rebuild for affected rows
              ────→ track_metadata VIEW reflects changes automatically (it's a VIEW)
```

### Critical Dependency Chain
```
Tag Writing Library → Single Track Edit → Batch Edit
                   → Cover Art Edit
```

The tag writing library choice gates everything. Until a library can write ID3v2 and Vorbis comments, no editing features can ship.

### Dependency on Existing Architecture

| Existing Feature | How Tag Editing Uses It |
|-----------------|----------------------|
| `track-details` component | Already has edit mode scaffolded with input fields, edit/save/cancel buttons, and `editValues` state. Save handler is a TODO stub. |
| Multi-select in track-list | Entry point for batch editing — selected file paths already accessible via `selection.getSelectedKeysOrdered()` |
| Context menu system | "Edit Tags" menu item for single or multi-select (currently shows "Track Details" for single) |
| `FrontendUtil.OpenFileDialog` | File picker for cover art image selection |
| `Library.saveCoverArt` + thumbnail pipeline | Reusable for cover art embedding — same hash-based cache, same thumbnail generation |
| Event system | New events needed: `TagsWritten`, `TagWriteProgress`, `TagWriteError` |
| `backend/metadata/tags.go` | `TrackMetadata` struct defines all writable fields; `ExtractTags` used for reading |

## Batch Editing UX Patterns (Deep Dive)

The batch editor is the highest-complexity feature. Here's how mature players handle it:

### Three-State Field Model

For each editable field in batch mode:
1. **Keep** (default): Shows "[Mixed]" or "[Various]" if values differ, shows the common value if all tracks share it. On save, each track retains its original value.
2. **Set**: User has typed a new value. On save, all selected tracks get this value.
3. **Clear**: User explicitly cleared the field. On save, all selected tracks have this field emptied.

**Implementation approach:**
```typescript
type FieldState = 'keep' | 'set' | 'clear';

interface BatchField {
  state: FieldState;
  value: string;           // The new value (only meaningful when state === 'set')
  commonValue: string;     // Value shared across all tracks (empty if mixed)
  isMixed: boolean;        // Whether tracks have different values
}
```

### Backend Batch Write Contract

```go
// TagEdits contains the fields to write. nil = don't change, empty string = clear.
type TagEdits struct {
    Title       *string
    Artist      *string
    Album       *string
    Genre       *string
    Year        *int
    TrackNumber *int
    DiscNumber  *int
    Composer    *string
    CoverArt    *CoverArtEdit // nil = keep, non-nil = set/remove
}

type CoverArtEdit struct {
    ImageData []byte // empty = remove cover art
    MIMEType  string
}
```

Using pointer fields: `nil` = keep original, non-nil = set to this value (empty string/zero = clear). This is the standard Go pattern for optional updates and maps directly to the three-state UI model.

### Batch Write Ordering

1. Validate all edits before writing any files (fail fast)
2. Write files sequentially (not concurrently — avoids disk thrashing and simplifies error handling)
3. For each file: read → modify → write-to-temp → rename
4. After ALL files written successfully: batch-update DB + FTS5
5. Emit success event with count
6. On error: stop, report which file failed, files already written are committed (no rollback — file writes are atomic individually)

## Cover Art Editing Workflow

### Set/Replace Cover Art (Table Stakes)

1. User clicks "Change Cover" in edit dialog
2. File picker opens (filter: `*.jpg, *.jpeg, *.png`)
3. User selects image file
4. Preview shown in dialog (replacing current art)
5. On save:
   a. Read image bytes from selected file
   b. Embed in audio file tag (APIC frame for ID3v2, METADATA_BLOCK_PICTURE for FLAC/OGG)
   c. Save to cover art cache (via existing `saveCoverArt` pipeline → hash, dedupe, thumbnails)
   d. Update `cover_art` table if hash changed
   e. Update UI with new cover art URLs

### Batch Cover Art (Same Image to All Selected Tracks)

Common use case: fixing an album where some tracks have wrong/missing cover art.
1. In batch editor, cover art section shows "[Mixed]" or common art
2. User selects new image → applies to ALL selected tracks on save
3. This is the same flow as single-track, just repeated N times

### What NOT to Build for Cover Art

- No crop/resize — users use external tools (GIMP, Preview, etc.)
- No web search — would require API integration (future MusicBrainz milestone could add this)
- No multiple picture types (front, back, booklet) — only front cover. ID3v2 supports picture types but the complexity isn't worth it for v1.

## Field Mapping: Tag Format → Database Schema

Understanding how edited fields map through the system:

| Edit Field | Tag (ID3v2) | Tag (Vorbis) | DB Table | DB Column | Notes |
|-----------|------------|-------------|----------|-----------|-------|
| Title | TIT2 | TITLE | `recordings` | `name` | |
| Artist | TPE1 | ARTIST | `artist_credit` → `artists` | `text` / `name` | May need to create new artist_credit + artist rows |
| Album | TALB | ALBUM | `release_groups` | `name` | May need to create new release_group row |
| Album Artist | TPE2 | ALBUMARTIST | (not currently stored separately) | — | Would need schema addition or use existing artist credit |
| Genre | TCON | GENRE | `genres` + `genre_recordings` | `name` | Multiple genres: split on `;` or `,` |
| Year | TYER/TDRC | DATE | `recordings` | `year` | |
| Track # | TRCK | TRACKNUMBER | `recordings` | `track_number` | |
| Disc # | TPOS | DISCNUMBER | `recordings` | `disc_number` | |
| Composer | TCOM | COMPOSER | `recordings` | `composer` | |
| Cover Art | APIC | METADATA_BLOCK_PICTURE | `cover_art` | `file_path` | Binary data; separate storage |
| Comment | COMM | COMMENT | `recordings` | `comment` | |
| Lyrics | USLT | LYRICS | `recordings` | `lyrics` | |

### Schema Update Complexity

Simple fields (title, year, track#, disc#, composer, comment, lyrics) → UPDATE `recordings` directly.

Relational fields (artist, album, genre) → must handle entity lifecycle:
- **Artist change:** Look up or create new `artists` + `artist_credit` rows, update `recordings.artist_credit_id`
- **Album change:** Look up or create new `release_groups` row, update `release_group_recordings` link
- **Genre change:** Parse genre string, look up or create `genres` rows, update `genre_recordings` links

This entity lookup logic already exists in `library.go`'s `processMetadata` / `saveAudioFile` pipeline — it should be extracted and reused.

## MVP Recommendation

**Prioritize (Phase 1 — Tag Editing Core):**
1. Tag writing library integration (MP3 + FLAC + OGG)
2. Single track editing (the 8 active fields from PROJECT.md)
3. Write-to-temp-then-rename safety
4. DB + FTS5 inline update
5. Cover art set/replace from file

**Prioritize (Phase 2 — Batch Editing):**
6. Batch editing with three-state field model
7. Progress feedback for batch operations
8. Error handling and partial-success reporting

**Defer:**
- Album artist editing (schema question, low priority)
- Lyrics/comment editing (easy to add later, niche)
- Auto-numbering tracks (convenience, not core)
- Undo/backup system (nice-to-have, not table stakes)
- Cover art paste from clipboard (WebView clipboard API complexity)

## Sources

- Hydrogenaudio Knowledgebase: Tag Mapping (https://wiki.hydrogenaud.io/index.php/Tag_Mapping) — HIGH confidence, authoritative tag format reference
- Hydrogenaudio Knowledgebase: foobar2000 Encouraged Tag Standards (https://wiki.hydrogenaud.io/index.php/Foobar2000:Encouraged_Tag_Standards) — HIGH confidence
- Hydrogenaudio Knowledgebase: Tag (metadata) (https://wiki.hydrogenaud.io/index.php/Tag) — HIGH confidence, basic/advanced/personalized field categorization
- YellowJacket codebase analysis: `track-details.ts`, `tags.go`, `coverart.go`, `library.go`, database schemas — PRIMARY source for dependency analysis
- MusicBee, foobar2000, Kid3, Mp3tag, Picard — feature set analysis from training data (MEDIUM confidence on specific UI details)
