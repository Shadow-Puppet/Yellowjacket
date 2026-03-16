# Architecture Patterns: Tag Editing Integration

**Domain:** Audio metadata editing in existing music player
**Researched:** 2026-03-16
**Confidence:** HIGH (based on full codebase analysis of existing architecture)

## Recommended Architecture

Tag editing is a **cross-cutting operation** that touches files, database entities, the FTS5 search index, the cover art pipeline, and the frontend cache — all from a single user action. The architecture adds a new `backend/tageditor/` package that orchestrates the full write pipeline, keeping the existing `library`, `metadata`, and `database` packages focused on their current responsibilities.

### High-Level Data Flow

```
UI: track-details "Save" click
  → Wails binding: tageditor.EditTrack(filePath, changes)
    → 1. Validate input + resolve audio_file by path
    → 2. Write tags to temp file, rename over original (safe write)
    → 3. Update DB entities in single transaction:
         a. Upsert artist_credit + artist (if artist changed)
         b. Upsert release_group (if album changed)
         c. Update recording fields (title, year, track#, etc.)
         d. Update genre links (delete old, insert new)
         e. Update release_group_recordings link (if album changed)
         f. Handle cover art (if image provided)
    → 4. Update FTS5 search_index (re-insert with same rowid)
    → 5. Emit TagsUpdated event with affected file paths
  → Frontend: libraryStore receives event, patches cached tracks in-place
  → All views re-render with updated metadata
```

### Component Boundaries

| Component | Responsibility | Communicates With |
|-----------|---------------|-------------------|
| `backend/tageditor/` (NEW) | Orchestrates tag write pipeline: file write + DB update + FTS5 + events | `metadata/`, `database/`, `events/`, `coverart/`, Wails runtime |
| `backend/tageditor/writer.go` (NEW) | Format-specific tag writing (MP3/FLAC/OGG) via external libraries | File system, `bogem/id3v2`, `go-flac/go-flac` + `go-flac/flacvorbis` |
| `backend/metadata/tags.go` (EXISTING) | Tag reading via `dhowden/tag` — **no changes needed** | File system |
| `backend/library/library.go` (EXISTING) | Scan pipeline, entity upsert helpers — **reuse `processMetadata` pattern** | `database/`, `metadata/` |
| `backend/database/search.go` (EXISTING) | FTS5 index operations — **add `UpdateSearchIndex` method** | SQLite |
| `backend/events/events.go` (EXISTING) | Event constants — **add tag editing events** | Nothing |
| `frontend/src/components/track-details/` (EXISTING) | Edit UI — **wire Save to backend, add batch mode** | `tageditor` Wails binding |
| `frontend/src/store/library-store.ts` (EXISTING) | Track cache — **add event handler for in-place patch** | Wails events |

## New Package: `backend/tageditor/`

### Why a Separate Package

The tag editing flow does NOT fit cleanly into the existing `library` package because:

1. **Different lifecycle**: Scans are bulk, batch-oriented operations. Tag edits are individual, user-initiated, synchronous operations.
2. **Different entity update strategy**: Scans always CREATE new recordings. Tag edits must UPDATE existing recordings and handle shared entity reference changes.
3. **Different file I/O pattern**: Scans read files. Tag edits write files with safety guarantees (temp + rename).
4. **Wails binding boundary**: Tag editor needs its own binding registration for a clean API surface.

However, the tag editor REUSES logic from existing packages:
- Entity upsert helpers from `library` (either extracted to shared code or duplicated with attribution)
- FTS5 operations from `database/search.go`
- Cover art pipeline from `library/coverart.go` and `coverart/`

### Package Structure

```
backend/tageditor/
├── tageditor.go    # Service struct, EditTrack(), EditTracks(), SetCoverArt()
├── writer.go       # Format-specific tag writing (MP3, FLAC, OGG)
└── writer_test.go  # Tests for safe file write + tag round-trip
```

### Service API (Wails-Bound)

```go
// Package tageditor provides audio file tag editing with safe
// file writes and inline database synchronization.
package tageditor

// EditRequest describes changes to apply to a single track.
type EditRequest struct {
    FilePath    string   `json:"filePath"`
    Title       *string  `json:"title,omitempty"`
    Artist      *string  `json:"artist,omitempty"`
    Album       *string  `json:"album,omitempty"`
    Genre       *string  `json:"genre,omitempty"`
    Year        *int     `json:"year,omitempty"`
    TrackNumber *int     `json:"trackNumber,omitempty"`
    DiscNumber  *int     `json:"discNumber,omitempty"`
    Composer    *string  `json:"composer,omitempty"`
    // CoverArt is set separately via SetCoverArt()
}

// EditResult reports the outcome of a tag edit operation.
type EditResult struct {
    FilePath string `json:"filePath"`
    Success  bool   `json:"success"`
    Error    string `json:"error,omitempty"`
}

// Service orchestrates tag editing operations.
type Service struct {
    ctx    context.Context
    logger *slog.Logger
    db     *database.DB
}

// EditTrack applies metadata changes to a single audio file.
func (s *Service) EditTrack(req EditRequest) EditResult

// EditTracks applies shared field changes to multiple files (batch).
func (s *Service) EditTracks(reqs []EditRequest) []EditResult

// SetCoverArt embeds an image file into one or more audio files.
func (s *Service) SetCoverArt(filePaths []string, imagePath string) []EditResult
```

Pointer fields (`*string`, `*int`) distinguish "not changed" (nil) from "set to empty/zero" (pointer to zero value). This is critical for batch editing where you only want to change shared fields.

### Two-Phase Initialization

Follows the existing `NewService()` + `SetContext()` pattern:

```go
// In NewYellowJacketApp():
yjApp.tagEditor = tageditor.NewService(logger, db)

// In OnStartup():
yj.tagEditor.SetContext(ctx)

// In FEBindings:
yjApp.FEBindings = []any{
    // ... existing bindings ...
    yjApp.tagEditor,
}
```

## File Writing Strategy

### Write-to-Temp-Then-Rename (Corruption Safety)

```
1. Write modified tags to temporary file in same directory:
   /music/track.mp3 → /music/.track.mp3.yjtmp
2. fsync the temp file
3. os.Rename temp file over original (atomic on same filesystem)
4. If any step fails, delete temp file and return error
```

Why same directory: `os.Rename` is atomic only within the same filesystem. Writing to a temp directory on a different mount would require a full copy.

### Format-Specific Writers

| Format | Library | Write Strategy |
|--------|---------|----------------|
| MP3 (ID3v2) | `github.com/bogem/id3v2/v2` (v2.1.4) | Open → parse existing → modify frames → Save() writes to same file. Use WriteTo() to write to temp file instead. |
| FLAC (Vorbis Comments) | `github.com/go-flac/go-flac/v2` + `github.com/go-flac/flacvorbis/v2` | ParseFile → find/create VorbisComment metablock → set fields → Save() to temp file |
| OGG (Vorbis Comments) | Custom or `dhowden/tag`-compatible approach | OGG Vorbis uses same comment format as FLAC. May need lower-level OGG page rewriting. **Needs deeper research at implementation time.** |

**Confidence notes:**
- MP3 via `bogem/id3v2`: HIGH — mature library (359 stars, v2.1.4, 57 importers), well-documented read+write API, supports ID3v2.3 and v2.4, picture frames, UTF-8 encoding.
- FLAC via `go-flac/go-flac` + `go-flac/flacvorbis`: MEDIUM — smaller community (12 stars on flacvorbis), but clean API for metadata block manipulation. `flac.Save(filename)` writes back to disk.
- OGG Vorbis: LOW — no well-established pure-Go OGG tag writing library. May need to shell out to a tool or implement custom OGG page rewriting. **Consider deferring OGG write support to a follow-up if complexity is high.**

### Cover Art Embedding

For cover art, the writer embeds the image data directly into the audio file:

- **MP3**: `id3v2.PictureFrame` with `PTFrontCover` type
- **FLAC**: `flac.MetaDataBlockPicture` (FLAC picture metadata block)

After writing to the audio file, the cover art pipeline also:
1. Saves the image to the covers directory (hash-based filename)
2. Generates size variants (sm/md/lg)
3. Upserts the `cover_art` DB record
4. Updates `release_groups.cover_art_id` if needed

## Database Update Strategy

### The Shared Entity Problem

The normalized schema means entities are shared across tracks:

```
artist_credit "The Beatles" ← referenced by 200 recordings
release_group "Abbey Road"  ← referenced by 17 recordings
genre "Rock"                ← referenced by 5000 recordings
```

When a user changes a track's artist from "The Beatles" to "The Beetles" (typo fix), we must NOT modify the existing `artist_credit` row — that would change the artist name for all 200 tracks.

### Update Rules

| Field Changed | DB Operation |
|---------------|-------------|
| Title | UPDATE `recordings.name` directly (recording is per-track) |
| Track Number | UPDATE `recordings.track_number` directly |
| Disc Number | UPDATE `recordings.disc_number` directly |
| Year | UPDATE `recordings.year` directly |
| Composer | UPDATE `recordings.composer` directly |
| Artist | Upsert new `artist_credit` + `artist`, UPDATE `recordings.artist_credit_id` to point to new credit. Old credit is NOT deleted (may be used by other recordings). |
| Album | Upsert new `release_group`, update `release_group_recordings` link. Old release group is NOT deleted. |
| Genre | Delete existing `recording_genres` links for this recording, upsert new genres, create new links. Old genres NOT deleted (shared). |
| Cover Art | Process through cover art pipeline, update `release_groups.cover_art_id` |

### Orphan Cleanup Strategy

After tag edits, orphaned entities (artist credits, release groups, genres with zero references) accumulate. Two options:

**Option A: Lazy cleanup (RECOMMENDED)**
- Orphans are harmless — they don't appear in queries because all views JOIN through `audio_files → recordings → ...`
- Clean up during the next library rescan (existing orphan cleanup phase)
- Zero additional complexity in the tag edit path

**Option B: Eager cleanup**
- After each edit, run reference-counting DELETE queries for affected entities
- Adds complexity and transaction time to every edit
- Only worthwhile if orphans cause visible problems (they don't)

**Decision: Option A.** The existing rescan orphan cleanup handles this. Tag editing should be fast and simple.

### Transaction Shape

Single transaction per track edit:

```sql
BEGIN;
-- 1. Upsert artist_credit (if artist changed)
INSERT INTO artist_credit(text) VALUES(?) ON CONFLICT(text) DO UPDATE SET text=text RETURNING *;
INSERT INTO artists(name) VALUES(?) ON CONFLICT(name) DO UPDATE SET name=name RETURNING *;
INSERT OR IGNORE INTO artist_credit_artist(artist_id, credit_id) VALUES(?, ?);

-- 2. Upsert release_group (if album changed)
INSERT INTO release_groups(name, album_artist_credit_id) VALUES(?, ?)
    ON CONFLICT(name, album_artist_credit_id) DO UPDATE SET name=name RETURNING *;

-- 3. Update recording
UPDATE recordings SET name=?, artist_credit_id=?, track_number=?, disc_number=?,
    year=?, genre=?, composer=? WHERE id=?;

-- 4. Update genre links (if genre changed)
DELETE FROM recording_genres WHERE recording_id = ?;
INSERT INTO genres(name) VALUES(?) ON CONFLICT(name) DO UPDATE SET name=name RETURNING *;
INSERT INTO recording_genres(recording_id, genre_id) VALUES(?, ?);

-- 5. Update release_group_recordings (if album changed)
DELETE FROM release_group_recordings WHERE recording_id = ?;
INSERT INTO release_group_recordings(release_group_id, recording_id, track_number, disc_number) VALUES(?, ?, ?, ?);

-- 6. FTS5 update (re-insert with same rowid)
INSERT INTO search_index(rowid, file_path, title, artist, album) VALUES(?, ?, ?, ?, ?);
COMMIT;
```

### FTS5 Update Pattern

The current `search_index` is contentless (`content=''`), which means:
- DELETE is not supported
- INSERT with an existing rowid adds a new entry; the old one becomes stale
- Stale entries are filtered out by the JOIN against `track_metadata` in search queries

This works correctly for tag editing: re-INSERT with the same `audio_files.id` as rowid. The stale entry for the old metadata is harmless and filtered by the VIEW JOIN.

**No FTS5 schema changes needed.**

## Events

### New Events

```go
// Tag editing events.
const (
    TagsUpdated   = "TagsUpdated"   // Single or batch edit complete
    TagEditFailed = "TagEditFailed" // Edit failed (file write error, etc.)
)
```

### Event Payloads

```go
// TagsUpdated payload:
type TagsUpdatedPayload struct {
    FilePaths []string `json:"filePaths"` // All affected file paths
}

// TagEditFailed payload:
type TagEditFailedPayload struct {
    FilePath string `json:"filePath"`
    Error    string `json:"error"`
}
```

### Frontend Event Handling

When `TagsUpdated` fires:
1. `libraryStore` re-fetches all data (simplest approach for v1)
2. OR `libraryStore` patches affected tracks in-place from the payload (more complex but avoids full reload)

**Recommendation:** Start with full re-fetch on `TagsUpdated`. Optimize to incremental patch later if performance is an issue. The existing `LibraryScanComplete` handler already does a full re-fetch, so this is consistent.

## Frontend Integration

### Existing `track-details` Component

The component already has:
- Edit mode toggle with input fields for all editable metadata
- `editValues` state tracking changes
- `saveEdit()` method (currently a no-op TODO)

Changes needed:
1. Wire `saveEdit()` to call `tageditor.EditTrack()` via Wails binding
2. Add loading/saving state for the save button
3. Add error display if the edit fails
4. Close dialog and emit refresh on success
5. Add cover art upload: file picker → `tageditor.SetCoverArt()`

### Batch Editing (Multi-Select)

The track list already has multi-select via `SelectionController`. Batch editing needs:

1. New context menu item: "Edit Tags" (when multiple tracks selected)
2. A batch edit dialog variant of `track-details` that:
   - Shows "Multiple Values" placeholder for fields that differ across selected tracks
   - Only sends changed fields (using the `*string`/`*int` nil-means-no-change pattern)
   - Calls `tageditor.EditTracks()` for all selected files

### Store Updates

`library-store.ts` needs:
```typescript
// In constructor, add event listener:
EventsOn(Events.TagsUpdated, () => {
    // Re-fetch all data to reflect changes
    this.eagerFetch();
});
```

This ensures all views (tracks, albums, artists, genres) reflect the updated metadata without manual cache invalidation.

## Integration Points Summary

| Existing Component | Change Type | What Changes |
|-------------------|-------------|-------------|
| `backend/app.go` | MODIFY | Add `tagEditor` field, wire in `NewYellowJacketApp`/`OnStartup`, add to `FEBindings` |
| `backend/events/events.go` | MODIFY | Add `TagsUpdated`, `TagEditFailed` constants |
| `frontend/src/events.ts` | MODIFY (auto-generated) | Mirror new event constants |
| `backend/database/search.go` | MINOR MODIFY | No changes needed — existing `InsertSearchIndex` works for re-insert |
| `backend/metadata/tags.go` | NO CHANGE | Read-only, continues to work as-is |
| `backend/library/library.go` | MINOR MODIFY | Extract `processMetadata` helpers to be reusable, or duplicate in tageditor with attribution |
| `backend/library/query.go` | NO CHANGE | Query methods work as-is |
| `frontend/src/components/track-details/` | MODIFY | Wire save to backend, add loading states, error handling |
| `frontend/src/store/library-store.ts` | MODIFY | Add `TagsUpdated` event listener for cache refresh |
| `go.mod` | MODIFY | Add `bogem/id3v2/v2`, `go-flac/go-flac/v2`, `go-flac/flacvorbis/v2` |

## Patterns to Follow

### Pattern 1: Pointer Fields for Optional Updates
**What:** Use `*string` and `*int` in `EditRequest` to distinguish "no change" from "set to empty/zero"
**When:** Any API that partially updates a record
**Example:**
```go
type EditRequest struct {
    Title *string `json:"title,omitempty"`
    Year  *int    `json:"year,omitempty"`
}

// nil = don't change, non-nil = set to this value
if req.Title != nil {
    recording.Name = *req.Title
}
```

### Pattern 2: Write-to-Temp-Then-Rename
**What:** Write to a temporary file in the same directory, then atomically rename
**When:** Any file modification that must not corrupt the original on failure
**Example:**
```go
tmpPath := filepath.Join(dir, "."+base+".yjtmp")
// Write to tmpPath...
if err := os.Rename(tmpPath, originalPath); err != nil {
    os.Remove(tmpPath)
    return err
}
```

### Pattern 3: Upsert-and-Relink for Shared Entities
**What:** Create new shared entity (artist/album/genre) and update the FK reference, rather than modifying the shared entity in place
**When:** Editing a field that maps to a shared/normalized entity
**Why:** Modifying a shared row would change data for all tracks referencing it

## Anti-Patterns to Avoid

### Anti-Pattern 1: Modifying Shared Entity Rows In-Place
**What:** `UPDATE artists SET name = ? WHERE id = ?` to change an artist name
**Why bad:** Changes the name for ALL tracks by that artist, not just the edited track
**Instead:** Upsert a new artist_credit, update the recording's FK to point to the new one

### Anti-Pattern 2: Full Library Rescan After Tag Edit
**What:** Triggering a library scan to pick up tag changes
**Why bad:** Scans take seconds to minutes. Creates new recordings instead of updating existing ones. Terrible UX.
**Instead:** Inline DB update in the same transaction as the file write

### Anti-Pattern 3: Frontend-Side Tag File Writing
**What:** Reading/writing audio files from TypeScript via File API
**Why bad:** Wails WebView doesn't have full filesystem access. Tag writing libraries are Go-native.
**Instead:** All file I/O happens in Go backend; frontend sends edit requests via Wails bindings

### Anti-Pattern 4: Deleting and Recreating Recordings on Edit
**What:** DELETE the old recording, CREATE a new one with updated metadata
**Why bad:** Changes the recording ID, breaking all references (audio_files.recording_id, release_group_recordings, recording_genres, queue, playlists referencing file paths)
**Instead:** UPDATE the existing recording row in place

## Scalability Considerations

| Concern | Single Track Edit | Batch Edit (100 tracks) | Batch Edit (1000 tracks) |
|---------|-------------------|------------------------|--------------------------|
| File I/O | ~50ms (one file read+write) | ~5s (sequential, safe) | ~50s (consider progress bar) |
| DB Transaction | <10ms | <100ms (single transaction) | <500ms (batch in groups of 100) |
| FTS5 Update | <1ms | <10ms | <50ms |
| Frontend Refresh | Instant (single event) | Single event, full re-fetch | Single event, full re-fetch |
| Memory | Negligible | ~100MB if all cover arts loaded | Consider streaming cover art |

For batch edits of >50 tracks, the UI should show a progress indicator. The backend should emit progress events similar to scan progress.

## Build Order (Dependency-Aware)

1. **Tag writing library integration** (`backend/tageditor/writer.go`)
   - Add dependencies to `go.mod`
   - Implement format-specific writers (MP3, FLAC)
   - Write-to-temp-then-rename safety wrapper
   - Unit tests with real audio files

2. **DB update logic** (`backend/tageditor/tageditor.go`)
   - Shared entity upsert (reuse or extract from library package)
   - Recording UPDATE query (existing `UpdateRecordingFull` in sqlc)
   - Genre re-linking
   - Release group re-linking
   - FTS5 re-index (existing `InsertSearchIndex`)
   - Transaction wrapper

3. **Events** (`backend/events/events.go`)
   - Add `TagsUpdated`, `TagEditFailed` constants
   - Run codegen to update `frontend/src/events.ts`

4. **Service wiring** (`backend/app.go`)
   - Create and bind `tageditor.Service`
   - Two-phase init (NewService + SetContext)

5. **Frontend: single track edit** (`frontend/src/components/track-details/`)
   - Wire `saveEdit()` to `tageditor.EditTrack()`
   - Loading/error states
   - `library-store` event handler for refresh

6. **Frontend: batch edit** (new or extended component)
   - Multi-select context menu action
   - Batch edit dialog
   - `tageditor.EditTracks()` call

7. **Cover art editing** (builds on phases 1-5)
   - File picker for image selection
   - `tageditor.SetCoverArt()` implementation
   - Cover art pipeline integration (save to disk, generate variants, update DB)

## Sources

- Codebase analysis: `backend/library/library.go` (scan pipeline, entity upsert pattern)
- Codebase analysis: `backend/database/search.go` (FTS5 contentless behavior)
- Codebase analysis: `backend/metadata/tags.go` (read-only tag extraction via dhowden/tag)
- Codebase analysis: `frontend/src/components/track-details/track-details.ts` (existing edit UI stub)
- `bogem/id3v2/v2`: https://pkg.go.dev/github.com/bogem/id3v2/v2 (v2.1.4, MIT, 359 stars, 57 importers)
- `go-flac/go-flac`: https://github.com/go-flac/go-flac (FLAC metadata manipulation)
- `go-flac/flacvorbis`: https://github.com/go-flac/flacvorbis (Vorbis comment read/write for FLAC)
- SQLite FTS5 contentless tables: https://www.sqlite.org/fts5.html#contentless_tables
