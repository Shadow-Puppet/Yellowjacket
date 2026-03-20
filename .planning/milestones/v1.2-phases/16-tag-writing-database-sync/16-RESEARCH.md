# Phase 16: Tag Writing & Database Sync - Research

**Researched:** 2026-03-17
**Domain:** Audio metadata writing (ID3v2 for MP3, Vorbis Comments for FLAC) + database synchronization
**Confidence:** HIGH

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
- **Diff map for changes:** Callers specify changed fields as a map of field name to new value (e.g. `map[string]any{"artist": "New Name", "year": 2024}`). Only changed fields are sent — naturally supports partial edits and batch (Phase 18).
- **Single function call:** `WriteTrackTags(trackID, changes)` — one call does everything: write file tags, update DB entities, update FTS5 search index. No two-step prepare/commit.
- **Track ID input:** Accepts track ID (int64), not file path. The pipeline looks up the file path, format, and current metadata from the database. The UI only knows track IDs.
- **Single entry point, auto-dispatch:** One entry point detects MP3/FLAC from the file extension and routes to the appropriate format-specific writer internally. The caller never thinks about audio format.
- **No size/format constraints for cover art:** Accept any JPEG/PNG image as-is, embed without resizing or validation.
- **Immediate thumbnail regeneration:** After writing new cover art, regenerate all 3 thumbnail sizes (sm/md/lg) immediately.
- **Cover art as part of diff map:** Cover art is a field in the same changes map as text fields (e.g. `{"cover_art": imageBytes}`). Set, replace, and clear operations supported.
- **Upsert-and-relink:** When an artist/album/genre name changes, find an existing entity with the new name or create one. Point the track at the new entity. Never mutate shared entity rows in-place.
- **Immediate orphan cleanup:** After relinking, check if the old entity has zero remaining track references and delete it right away.
- **Album artist is a text field:** No new album_artist entity table. Edit directly, no relinking needed.
- **Single DB transaction after file write:** File write (via AtomicWrite) happens first. On success, one database transaction handles all DB changes. If file write fails, DB is untouched.
- **Stop playback completely:** If the target file is currently playing, stop playback entirely (not pause). Release the file handle so the write can proceed.
- **Auto-stop in pipeline:** The write pipeline automatically checks if the target file is playing and stops the player. Callers don't need to handle player state.
- **Mutual exclusion with scan:** Scan pipeline and write pipeline share a mutex. If a scan is running, the write waits for it to finish (and vice versa).
- **Event-driven frontend notification:** After write + DB sync complete, emit an event (e.g. TrackMetadataChanged) so the frontend refreshes all views.

### Claude's Discretion
- Internal format-specific writer implementation details (ID3v2 frame handling, Vorbis Comment block management)
- Choice of Go libraries for tag writing (research phase will evaluate options)
- Exact field name strings in the diff map
- Error handling and error message wording
- Test file fixtures and test structure

### Deferred Ideas (OUT OF SCOPE)
None — discussion stayed within phase scope
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|-----------------|
| WRITE-01 | Write metadata tags to MP3 files via ID3v2 (title, artist, album, genre, year, track#, disc#, composer) | `n10v/id3v2` v2 library — full ID3v2.3/2.4 read+write support with typed setters, WriteTo for atomic write integration |
| WRITE-02 | Write metadata tags to FLAC files via Vorbis Comments | `go-flac/go-flac` v2 + `go-flac/flacvorbis` v2 — parse FLAC metadata blocks, modify Vorbis Comments, serialize back |
| WRITE-04 | Embed cover art image (JPEG/PNG) in MP3 and FLAC files | MP3: `id3v2.PictureFrame` with APIC; FLAC: `go-flac/flacpicture` v2 with PICTURE metadata block |
| WRITE-06 | Currently-playing file is stopped before writing (player safety) | `Player.UnloadTrack()` releases `currentFile *os.File`; pipeline checks `currentFile.Name()` match before writing |
| SYNC-01 | After tag write, update DB entities inline (upsert-and-relink for artist, album, genre) | Existing `cachedUpsertArtistCredit`, `UpsertArtist`, `UpsertGenre`, `UpsertReleaseGroup` patterns in library.go |
| SYNC-02 | After tag write, update FTS5 search index for affected tracks | `DB.DeleteSearchIndex(rowid)` + `DB.InsertSearchIndex(...)` — proven pattern from Phase 15 |
| SYNC-03 | Orphaned entities (artists, albums, genres no longer referenced) cleaned up | Query reference count for old entity ID after relink; DELETE if zero references remain |
| SYNC-04 | Scan pipeline paused during tag writes to prevent race conditions | `Library.mu sync.Mutex` already protects `scanActive bool`; extend to gate write pipeline entry |
</phase_requirements>

## Summary

Phase 16 implements the core tag writing pipeline that Phases 17 and 18 will call into. The pipeline accepts a track ID and a diff map of changed fields, writes tags to the audio file (MP3 or FLAC), then synchronizes all entity changes to the database and search index in a single transaction. This is a backend-only phase — no UI work.

The Go ecosystem has a clear standard stack for this: **`n10v/id3v2` v2** (formerly `bogem/id3v2`) for MP3 ID3v2 tag writing (359 stars, 57 importers, mature), and **`go-flac/go-flac` v2** with companion packages `flacvorbis` v2 and `flacpicture` v2 for FLAC metadata manipulation. Both support reading existing tags, modifying individual fields, and writing back — which is essential for the diff-based update model.

The key architectural challenge is integrating these libraries with the existing `AtomicWrite` utility. The `n10v/id3v2` library has `WriteTo(io.Writer)` which writes the complete tag to any writer, perfect for piping into AtomicWrite's temp file callback. For FLAC, `go-flac` provides `Save(filename)` which writes the complete file — we'll use its `Marshal()` method to serialize to bytes then write via AtomicWrite. Both approaches ensure the original file is never partially modified.

**Primary recommendation:** Use `n10v/id3v2` v2 for MP3 and `go-flac` ecosystem v2 for FLAC. Integrate both through the existing `AtomicWrite` utility. Build the pipeline as a new `backend/tagwriter` package with a single `WriteTrackTags` entry point.

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/bogem/id3v2/v2` (aka `n10v/id3v2`) | v2.1.4 | Read & write ID3v2.3/2.4 tags on MP3 files | 359 stars, 57 importers on pkg.go.dev, MIT license, pure Go, supports all frame types including APIC pictures |
| `github.com/go-flac/go-flac/v2` | v2.x | Parse/write FLAC file structure (metadata blocks + audio frames) | Only pure-Go FLAC metadata manipulation library; v2 module path available; 44 stars |
| `github.com/go-flac/flacvorbis/v2` | v2.x | Read/write Vorbis Comment metadata blocks in FLAC | Companion to go-flac for the specific metadata block type FLAC uses for tags |
| `github.com/go-flac/flacpicture/v2` | v2.x | Read/write PICTURE metadata blocks in FLAC | Companion to go-flac for embedded cover art in FLAC files |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `yellowjacket/backend/fileutil` | (internal) | `AtomicWrite` for crash-safe file writes | Every tag write operation — wraps both MP3 and FLAC writes |
| `yellowjacket/backend/database` | (internal) | `BeginTx`, `DeleteSearchIndex`, `InsertSearchIndex` | DB sync phase after successful file write |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `n10v/id3v2` | `mewkiz/flac` (already in go.mod as dep) | `mewkiz/flac` is for FLAC decoding, not ID3v2 — wrong format. Not applicable for MP3. |
| `go-flac/go-flac` | Manual FLAC block parsing | FLAC format is complex (variable-length metadata blocks, last-metadata-block flag, StreamInfo must be first). Hand-rolling this would be error-prone and pointless. |
| `n10v/id3v2` | `dhowden/tag` (already used for reading) | `dhowden/tag` is **read-only** — no write support at all. Cannot be used for tag writing. |

**Installation:**
```bash
go get github.com/bogem/id3v2/v2@latest
go get github.com/go-flac/go-flac/v2@latest
go get github.com/go-flac/flacvorbis/v2@latest
go get github.com/go-flac/flacpicture/v2@latest
```

## Architecture Patterns

### Recommended Project Structure
```
backend/
├── tagwriter/           # NEW — Phase 16 entry point
│   ├── tagwriter.go     # WriteTrackTags entry point, diff map types, format dispatch
│   ├── mp3.go           # MP3-specific ID3v2 writing via n10v/id3v2
│   ├── flac.go          # FLAC-specific Vorbis Comment + Picture writing via go-flac
│   └── tagwriter_test.go
├── fileutil/
│   └── atomicwrite.go   # Existing — used by tagwriter
├── library/
│   └── library.go       # Existing — extend mu for write/scan mutual exclusion
├── player/
│   └── player.go        # Existing — UnloadTrack() for file safety
└── events/
    └── events.go        # Existing — add TrackMetadataChanged event
```

### Pattern 1: Diff Map → Format-Specific Writer
**What:** A single `WriteTrackTags(ctx, trackID, changes)` function that:
1. Looks up track by ID (DB query for file_path, format, current metadata)
2. Checks player state, stops if needed
3. Acquires scan/write mutex
4. Dispatches to `writeMp3Tags()` or `writeFlacTags()` based on file extension
5. Uses `AtomicWrite` for crash-safe file writing
6. Runs DB sync in single transaction
7. Emits frontend event

**When to use:** Every tag edit operation (single track and batch).

**Example — MP3 write with AtomicWrite:**
```go
func writeMp3Tags(logger *slog.Logger, filePath string, changes map[string]any) error {
    // Open and parse existing tags
    tag, err := id3v2.Open(filePath, id3v2.Options{Parse: true})
    if err != nil {
        return fmt.Errorf("open mp3 for tag writing: %w", err)
    }
    defer tag.Close()

    // Apply changes from diff map
    if v, ok := changes["title"].(string); ok {
        tag.SetTitle(v)
    }
    if v, ok := changes["artist"].(string); ok {
        tag.SetArtist(v)
    }
    if v, ok := changes["album"].(string); ok {
        tag.SetAlbum(v)
    }
    if v, ok := changes["genre"].(string); ok {
        tag.SetGenre(v)
    }
    if v, ok := changes["year"].(string); ok {
        tag.SetYear(v)
    }
    // Track number: TRCK frame "3/12" format
    if v, ok := changes["track_number"].(int); ok {
        tag.AddTextFrame(tag.CommonID("Track number/Position in set"),
            id3v2.EncodingUTF8, strconv.Itoa(v))
    }
    // Disc number: TPOS frame
    if v, ok := changes["disc_number"].(int); ok {
        tag.AddTextFrame(tag.CommonID("Part of a set"),
            id3v2.EncodingUTF8, strconv.Itoa(v))
    }
    // Composer: TCOM frame
    if v, ok := changes["composer"].(string); ok {
        tag.AddTextFrame("TCOM", id3v2.EncodingUTF8, v)
    }

    // Cover art: APIC frame
    if imgData, ok := changes["cover_art"].([]byte); ok && len(imgData) > 0 {
        tag.DeleteFrames(tag.CommonID("Attached picture"))
        pic := id3v2.PictureFrame{
            Encoding:    id3v2.EncodingUTF8,
            MimeType:    detectMIME(imgData),
            PictureType: id3v2.PTFrontCover,
            Description: "Front cover",
            Picture:     imgData,
        }
        tag.AddAttachedPicture(pic)
    } else if _, clearArt := changes["cover_art"]; clearArt {
        // cover_art present but nil/empty = clear
        tag.DeleteFrames(tag.CommonID("Attached picture"))
    }

    // Write atomically: read original audio data, write new tag + audio to temp, rename
    return fileutil.AtomicWrite(logger, filePath, func(tmp *os.File) error {
        // WriteTo writes the complete ID3v2 tag
        if _, err := tag.WriteTo(tmp); err != nil {
            return fmt.Errorf("write id3v2 tag: %w", err)
        }
        // Copy audio frames from original file (after the tag)
        return copyAudioData(filePath, tag, tmp)
    })
}
```

### Pattern 2: FLAC Metadata Block Manipulation
**What:** Parse FLAC file into metadata blocks + audio frames, modify only the VorbisComment and Picture blocks, reassemble, write via AtomicWrite.

**Example — FLAC write with AtomicWrite:**
```go
func writeFlacTags(logger *slog.Logger, filePath string, changes map[string]any) error {
    f, err := flac.ParseFile(filePath)
    if err != nil {
        return fmt.Errorf("parse flac: %w", err)
    }

    // Find or create Vorbis Comment block
    var cmt *flacvorbis.MetadataBlockVorbisComment
    var cmtIdx int = -1
    for idx, meta := range f.Meta {
        if meta.Type == flac.VorbisComment {
            cmt, err = flacvorbis.ParseFromMetaDataBlock(*meta)
            if err != nil {
                return fmt.Errorf("parse vorbis comments: %w", err)
            }
            cmtIdx = idx
        }
    }
    if cmt == nil {
        cmt = flacvorbis.New()
    }

    // Apply changes — Vorbis Comments use uppercase field names
    if v, ok := changes["title"].(string); ok {
        replaceComment(cmt, flacvorbis.FIELD_TITLE, v)
    }
    if v, ok := changes["artist"].(string); ok {
        replaceComment(cmt, flacvorbis.FIELD_ARTIST, v)
    }
    // ... other fields ...

    // Marshal back to metadata block
    cmtMeta := cmt.Marshal()
    if cmtIdx >= 0 {
        f.Meta[cmtIdx] = &cmtMeta
    } else {
        f.Meta = append(f.Meta, &cmtMeta)
    }

    // Handle cover art — PICTURE metadata block
    if imgData, ok := changes["cover_art"].([]byte); ok && len(imgData) > 0 {
        removePictureBlocks(f)
        pic, _ := flacpicture.NewFromImageData(
            flacpicture.PictureTypeFrontCover,
            "Front cover", imgData, detectMIME(imgData),
        )
        picMeta := pic.Marshal()
        f.Meta = append(f.Meta, &picMeta)
    } else if _, clearArt := changes["cover_art"]; clearArt {
        removePictureBlocks(f)
    }

    // Write atomically
    return fileutil.AtomicWrite(logger, filePath, func(tmp *os.File) error {
        return f.Save(tmp.Name())
        // NOTE: go-flac's Save writes to a file path.
        // Alternative: f.Marshal() to get bytes, then tmp.Write(bytes)
    })
}
```

### Pattern 3: DB Sync Transaction
**What:** After successful file write, run a single DB transaction that updates audio_files, upserts/relinks entities, updates FTS5, and cleans up orphans.

```go
func (tw *TagWriter) syncDatabase(
    ctx context.Context,
    tx *sql.Tx,
    txq *sqlcgen.Queries,
    audioFileID int64,
    oldMeta, newMeta *metadata.TrackMetadata,
) error {
    // 1. Update recording fields (title, year, track#, disc#, composer)
    // 2. If artist changed: upsert new artist credit, relink recording, cleanup old
    // 3. If album changed: upsert new release group, relink, cleanup old
    // 4. If genre changed: unlink old genres, link new genres, cleanup orphans
    // 5. If cover art changed: save to covers dir, upsert cover_art record,
    //    update release_group, regenerate thumbnails
    // 6. Update FTS5: DeleteSearchIndex(rowid) + InsertSearchIndex(rowid, ...)
    return nil
}
```

### Pattern 4: Player Safety Check
**What:** Before writing, check if the target file is currently playing and stop the player.

```go
func (tw *TagWriter) ensureFileNotPlaying(filePath string) {
    info := tw.player.GetCurrentTrackInfo()
    if info.FilePath == filePath {
        tw.player.UnloadTrack() // stops playback, releases file handle
    }
}
```

### Anti-Patterns to Avoid
- **Mutating shared entity rows in-place:** When track A's artist changes from "Beatles" to "Stones", never UPDATE the artist_credit row "Beatles" to "Stones" — other tracks reference it. Always upsert-and-relink.
- **Writing tags without AtomicWrite:** Direct file modification risks corruption on crash. Always go through AtomicWrite (write temp, rename).
- **Running DB sync without a transaction:** The entity relink + FTS update + orphan cleanup must be atomic. If any step fails, the whole thing rolls back.
- **Holding the scan/write mutex during file I/O:** The mutex should gate entry to the pipeline, but file reads and library calls should not hold it for the duration. Use a "pipeline active" flag pattern.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| ID3v2 tag writing | Custom ID3v2 frame serializer | `n10v/id3v2` | ID3v2 has complex encoding rules (syncsafe integers, encoding byte per frame, unsynchronization), multiple versions (2.3 vs 2.4 with different frame IDs), and edge cases (padding, extended headers). 579 commits of battle-testing. |
| FLAC metadata block manipulation | Custom FLAC parser | `go-flac/go-flac` | FLAC has strict block ordering requirements (StreamInfo first, last-metadata-block flag), variable-length block headers, and audio frame integrity. The library handles reassembly correctly. |
| Vorbis Comment encoding | Custom key=value parser | `go-flac/flacvorbis` | Vorbis Comments use a specific binary encoding (vendor string, comment count, length-prefixed UTF-8 strings). Small but fiddly to get right. |
| FLAC PICTURE block encoding | Custom PICTURE block serializer | `go-flac/flacpicture` | PICTURE blocks have a specific binary format (picture type, MIME type length, description length, image dimensions, color depth, image data length). |

**Key insight:** Audio metadata formats are deceptively complex. ID3v2 and FLAC/Vorbis have decades of edge cases baked in. The libraries handle encoding details, version differences, and binary format requirements that would be error-prone to reimplement.

## Common Pitfalls

### Pitfall 1: n10v/id3v2 Save() Writes to Same File
**What goes wrong:** The `tag.Save()` method in `n10v/id3v2` writes directly back to the file it was opened from. This doesn't work with our AtomicWrite pattern.
**Why it happens:** The library was designed for simple "open, modify, save" workflows.
**How to avoid:** Use `tag.WriteTo(w io.Writer)` instead of `tag.Save()`. WriteTo writes the complete ID3v2 tag (header + frames) to any writer. Then manually copy the audio data (everything after the original tag) to the temp file. AtomicWrite handles the atomic rename.
**Warning signs:** If you call `tag.Save()`, it writes to the original file without atomic rename, defeating crash safety.

### Pitfall 2: MP3 Audio Data Offset
**What goes wrong:** After writing the new ID3v2 tag with `WriteTo`, you must copy the audio data from the original file. But the audio data starts at an offset that depends on the original tag size.
**Why it happens:** The ID3v2 tag sits at the beginning of an MP3 file, followed by audio frames. When the tag size changes (e.g., adding cover art), the audio data must be at the right offset.
**How to avoid:** The `n10v/id3v2` tag tracks the original tag size internally. After `Open()`, you can get the original size to know where audio frames start. Alternatively, use the library's internal mechanisms — `tag.Save()` handles this, so study its implementation for the copy logic needed with `WriteTo`.
**Warning signs:** Corrupted audio output, file plays with glitches, file size doesn't match expected.

### Pitfall 3: FLAC Full File Rewrite
**What goes wrong:** FLAC files require a complete rewrite when metadata blocks change size (which they always do when editing tags or cover art).
**Why it happens:** Unlike MP3 where the ID3v2 tag is a prefix, FLAC metadata blocks are integral to the file structure. There's no padding mechanism that's universally reliable.
**How to avoid:** Accept the full rewrite cost. `go-flac` reads the entire file (metadata + audio frames) into memory, modifies metadata blocks, and writes the complete file back. Use AtomicWrite to make this safe. For large FLAC files (hundreds of MB for high-res audio), this means significant memory usage — but it's the only correct approach.
**Warning signs:** Out-of-memory on very large FLAC files (24-bit/192kHz albums can be 1GB+). Consider streaming the audio frames rather than loading them entirely into memory.

### Pitfall 4: go-flac Save() File Path Issue with AtomicWrite
**What goes wrong:** `go-flac`'s `f.Save(filename)` writes to the given path. If we pass the temp file path from AtomicWrite, the metadata in the file may reference a different filename.
**Why it happens:** `go-flac`'s Save takes a filename and creates/truncates that file directly.
**How to avoid:** Two options: (a) Use `f.Save(tmp.Name())` within the AtomicWrite callback — the temp file was already created by AtomicWrite, so Save will overwrite it. Verify that Save truncates first. (b) Serialize the FLAC data to bytes in memory and write to the temp file via `tmp.Write()`. Option (b) is safer but uses more memory.
**Warning signs:** File permissions or ownership not matching after Save, or AtomicWrite's cleanup logic conflicting with Save's file creation.

### Pitfall 5: DeleteFrames Before AddFrame for Single-Value Fields
**What goes wrong:** ID3v2 allows multiple frames with the same ID (e.g., multiple APIC frames). If you call `AddAttachedPicture` without first calling `DeleteFrames("APIC")`, you'll accumulate duplicate pictures.
**Why it happens:** `n10v/id3v2` AddFrame appends to the frame list. It doesn't replace existing frames.
**How to avoid:** For fields that should be single-valued (title, artist, album, genre, year, cover art), always `DeleteFrames(id)` before `AddFrame` or use the convenience setters (`SetTitle`, `SetArtist`, etc.) which handle this internally. Check the library source to confirm which setters auto-replace.
**Warning signs:** File size growing on each edit, multiple artist names showing in players.

### Pitfall 6: Vorbis Comment Field Replacement
**What goes wrong:** Vorbis Comments can have duplicate keys. Adding "TITLE=New Title" without removing the old "TITLE=Old Title" results in two title entries.
**Why it happens:** The Vorbis Comment spec allows multiple values per key (used intentionally for multi-artist or multi-genre).
**How to avoid:** Implement a `replaceComment` helper that removes all existing entries for a key, then adds the new value. The `flacvorbis` library provides `Add()` but no `Set()` or `Replace()` — you must build this from `Get()` + removal + `Add()`.
**Warning signs:** Tags showing concatenated values, old values persisting after edit.

### Pitfall 7: Scan/Write Race Condition
**What goes wrong:** If a library scan is running while a tag write occurs, the scan might read stale data or the write might overwrite scan-imported data.
**Why it happens:** The scan pipeline walks files and imports metadata concurrently with the write pipeline modifying files.
**How to avoid:** Use `Library.mu` as mutual exclusion. Before writing, check `scanActive` — if true, wait (or return error). Set a `writeActive` flag while writing so scans wait. The decision says "If a scan is running, the write waits for it to finish (and vice versa)."
**Warning signs:** DB data reverting after edit, duplicate entities, stale search results.

## Code Examples

### MP3: Open, Modify, and Write to io.Writer

```go
// Source: n10v/id3v2 godoc + README
tag, err := id3v2.Open("file.mp3", id3v2.Options{Parse: true})
if err != nil {
    log.Fatal(err)
}
defer tag.Close()

// Set text fields
tag.SetArtist("New Artist")
tag.SetTitle("New Title")
tag.SetAlbum("New Album")
tag.SetGenre("Rock")
tag.SetYear("2024")

// Set track number (TRCK frame)
tag.AddTextFrame("TRCK", id3v2.EncodingUTF8, "3")

// Set disc number (TPOS frame)
tag.AddTextFrame("TPOS", id3v2.EncodingUTF8, "1")

// Set composer (TCOM frame)
tag.AddTextFrame("TCOM", id3v2.EncodingUTF8, "Composer Name")

// Write tag to an io.Writer (e.g., temp file from AtomicWrite)
n, err := tag.WriteTo(w)

// tag.Save() would write to the original file — don't use with AtomicWrite
```

### MP3: Embed Cover Art (APIC Frame)

```go
// Source: n10v/id3v2 godoc PictureFrame example
tag.DeleteFrames(tag.CommonID("Attached picture")) // remove existing

pic := id3v2.PictureFrame{
    Encoding:    id3v2.EncodingUTF8,
    MimeType:    "image/jpeg",  // or "image/png"
    PictureType: id3v2.PTFrontCover,
    Description: "Front cover",
    Picture:     imageBytes,
}
tag.AddAttachedPicture(pic)
```

### FLAC: Modify Vorbis Comments

```go
// Source: go-flac/flacvorbis README
f, err := flac.ParseFile(filePath)
if err != nil {
    return err
}

// Find existing Vorbis Comment block
var cmt *flacvorbis.MetadataBlockVorbisComment
var cmtIdx int = -1
for idx, meta := range f.Meta {
    if meta.Type == flac.VorbisComment {
        cmt, _ = flacvorbis.ParseFromMetaDataBlock(*meta)
        cmtIdx = idx
    }
}
if cmt == nil {
    cmt = flacvorbis.New()
}

// Replace a field (remove old + add new)
// flacvorbis field constants: FIELD_TITLE, FIELD_ARTIST, FIELD_ALBUM, etc.
cmt.Add(flacvorbis.FIELD_TITLE, []byte("New Title"))

// Marshal back
cmtMeta := cmt.Marshal()
if cmtIdx >= 0 {
    f.Meta[cmtIdx] = &cmtMeta
} else {
    f.Meta = append(f.Meta, &cmtMeta)
}

f.Save(filePath)
```

### FLAC: Embed Cover Art (PICTURE Block)

```go
// Source: go-flac/flacpicture README
picture, err := flacpicture.NewFromImageData(
    flacpicture.PictureTypeFrontCover,
    "Front cover",
    imageBytes,
    "image/jpeg",
)
if err != nil {
    return err
}

// Remove existing picture blocks first
newMeta := make([]*flac.MetaDataBlock, 0, len(f.Meta))
for _, meta := range f.Meta {
    if meta.Type != flac.Picture {
        newMeta = append(newMeta, meta)
    }
}
f.Meta = newMeta

// Add new picture
picMeta := picture.Marshal()
f.Meta = append(f.Meta, &picMeta)
```

### DB Sync: Upsert-and-Relink Pattern

```go
// Reuse existing pattern from library.go
// Within a transaction:
tx, err := db.BeginTx()
txq := db.Queries.WithTx(tx)

// Upsert new artist credit
newAC, err := txq.UpsertArtistCredit(ctx, newArtistName)
// Upsert artist
newArtist, err := txq.UpsertArtist(ctx, newArtistName)
// Link artist to credit
txq.CreateArtistCreditArtist(ctx, sqlcgen.CreateArtistCreditArtistParams{
    ArtistID: newArtist.ID,
    CreditID: newAC.ID,
})
// Update recording to point to new credit
txq.UpdateRecordingArtistCredit(ctx, ...) // may need new sqlc query

// Check if old credit is orphaned
count, _ := txq.CountRecordingsByArtistCredit(ctx, oldCreditID) // may need new sqlc query
if count == 0 {
    txq.DeleteArtistCredit(ctx, oldCreditID) // may need new sqlc query
}

// FTS5 update
db.DeleteSearchIndex(audioFileID)
db.InsertSearchIndex(audioFileID, filePath, title, artist, album)

tx.Commit()
```

### Event Emission

```go
// Add to backend/events/events.go:
const TrackMetadataChanged = "TrackMetadataChanged"

// Emit after successful write + sync:
runtime.EventsEmit(ctx, events.TrackMetadataChanged, map[string]any{
    "trackId":  trackID,
    "filePath": filePath,
})
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `bogem/id3v2` import path | `github.com/bogem/id3v2/v2` (module path) / `n10v/id3v2` (repo moved) | 2022 | Import as `github.com/bogem/id3v2/v2`, the go.mod still references bogem |
| `go-flac` v1 (flat import) | `go-flac/go-flac/v2` (v2 module path) | Recent | Use v2 import paths for all go-flac ecosystem packages |
| FLAC padding block optimization | Full file rewrite | N/A | go-flac does not optimize via padding — always rewrites. Acceptable for our use case since AtomicWrite handles safety. |

**Deprecated/outdated:**
- `n10v/id3v2` v1 (non-module path): Use v2 module path `github.com/bogem/id3v2/v2`
- `go-flac` v1 packages: Use v2 import paths

## Open Questions

1. **n10v/id3v2 WriteTo + audio data copying**
   - What we know: `WriteTo` writes the ID3v2 tag (header + frames) to an io.Writer. `Save()` handles writing the complete file (tag + audio).
   - What's unclear: The exact mechanism for copying audio data after the tag when using `WriteTo` instead of `Save()`. Need to examine the library source for `Save()` to understand how it locates the audio data start offset.
   - Recommendation: During implementation, read the `Save()` source code in `n10v/id3v2`. If `Save()` internally uses `WriteTo` + audio copy, replicate that logic. Alternatively, if the library provides the original tag size, calculate `audioOffset = originalTagSize + 10` (10 bytes for ID3v2 header) and copy from there. **This is the most important implementation detail to verify early.**

2. **go-flac memory usage for large files**
   - What we know: `go-flac` loads the entire file (metadata + audio frames) into memory via `ParseFile`.
   - What's unclear: Memory footprint for very large FLAC files (1GB+ for high-res audio albums).
   - Recommendation: For v1.2, accept the memory cost — most FLAC files are 20-100MB. Add a warning log if file size exceeds 500MB. Future optimization could use streaming if needed.

3. **go-flac Save() interaction with AtomicWrite temp file**
   - What we know: `go-flac` `Save(filename)` writes directly to a path. AtomicWrite creates a temp file and provides it.
   - What's unclear: Whether `Save()` creates a new file or expects the file to exist. Whether it conflicts with AtomicWrite's temp file management.
   - Recommendation: Test during implementation. If `Save()` conflicts with AtomicWrite, use the alternative approach: serialize the FLAC data to a `[]byte` buffer, then write that buffer to the AtomicWrite temp file.

4. **New sqlc queries needed for orphan cleanup**
   - What we know: Existing queries support upsert operations but not reference counting or targeted deletion of artist credits, artists, and genres by ID.
   - What's unclear: Exact set of new queries needed.
   - Recommendation: During planning, enumerate: `CountRecordingsByArtistCredit`, `DeleteArtistCredit`, `CountRecordingsByGenre`, `DeleteGenre`, `CountRecordingsByReleaseGroup`, `DeleteReleaseGroup`, `UpdateRecordingArtistCredit`, etc. These are simple queries that can be added to the existing sqlc schema.

## Sources

### Primary (HIGH confidence)
- [n10v/id3v2 GitHub](https://github.com/n10v/id3v2) — 359 stars, 60 forks, v2.1.4, MIT license. README confirms read/write API with Open/SetX/Save pattern. WriteTo(io.Writer) available on Tag.
- [n10v/id3v2 pkg.go.dev](https://pkg.go.dev/github.com/bogem/id3v2/v2) — Full API docs verified: PictureFrame, CommentFrame, TextFrame types. SetArtist/SetTitle/SetAlbum/SetGenre/SetYear convenience methods. AddTextFrame for arbitrary frame IDs. DeleteFrames for removal. 57 importers confirms community adoption.
- [go-flac/go-flac GitHub](https://github.com/go-flac/go-flac) — 44 stars, v2 module path. ParseFile/Save API. Metadata block manipulation via Meta slice.
- [go-flac/flacvorbis GitHub](https://github.com/go-flac/flacvorbis) — Vorbis Comment manipulation. New(), Add(), Get(), Marshal() API. Field constants (FIELD_TITLE, FIELD_ARTIST, etc.).
- [go-flac/flacpicture GitHub](https://github.com/go-flac/flacpicture) — PICTURE metadata block. NewFromImageData(), Marshal() API. PictureTypeFrontCover constant.
- Existing codebase: `backend/fileutil/atomicwrite.go`, `backend/library/library.go`, `backend/player/player.go`, `backend/database/search.go`, `backend/events/events.go` — all verified by reading source files.

### Secondary (MEDIUM confidence)
- FLAC format specification (xiph.org) — Referenced by go-flac README for metadata block ordering requirements (StreamInfo first).
- ID3v2.3/2.4 specifications — Referenced by n10v/id3v2 common_ids.go for frame ID mappings.

### Tertiary (LOW confidence)
- go-flac reliability with edge-case FLAC files — STATE.md flagged this at 44 stars. The library has only 33 commits and 5 forks. **Recommend early round-trip testing with diverse FLAC files during implementation (Wave 0 or first task).**

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — n10v/id3v2 is the clear standard for Go ID3v2 writing (no real alternatives). go-flac is the only option for FLAC metadata in pure Go.
- Architecture: HIGH — Pipeline design follows existing codebase patterns (AtomicWrite, upsert-and-relink, event emission). All building blocks verified in source.
- Pitfalls: HIGH — Key gotchas identified from library APIs (Save vs WriteTo, FLAC full rewrite, frame duplication, Vorbis Comment replacement). One MEDIUM-confidence area: exact WriteTo + audio copy mechanism for MP3.

**Research date:** 2026-03-17
**Valid until:** 2026-04-17 (stable libraries, no fast-moving changes expected)
