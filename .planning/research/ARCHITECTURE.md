# Architecture Patterns: OGG Vorbis + WAV Tag Writer Integration

**Domain:** Adding OGG Vorbis and WAV tag writing to existing music player tag editing pipeline
**Researched:** 2026-03-18
**Confidence:** HIGH (based on full codebase analysis of existing architecture + container format research)

## Recommended Architecture

OGG Vorbis and WAV tag writing integrate into the existing `tagwriter` package by implementing two new format-specific writer functions (`writeOggTags` and `writeWavTags`) that follow the exact pattern established by `writeMp3Tags` and `writeFlacTags`. The existing pipeline (`WriteTrackTags` → file write → DB sync → event emission) requires only a switch-case extension — no new interfaces, no refactoring.

### High-Level Data Flow (Unchanged)

```
UI: track-details "Save" click
  → Wails binding: TagWriter.WriteTrackTagsByPath(filePath, changes)
    → 1. Resolve audio_file by path
    → 2. DetectFormat(filePath)  ← EXTEND: add .ogg, .wav cases
    → 3. AcquirePipelineLock
    → 4. PlayerStopper check
    → 5. Format switch:
         case FormatMP3:  writeMp3Tags(...)   ← existing
         case FormatFLAC: writeFlacTags(...)  ← existing
         case FormatOGG:  writeOggTags(...)   ← NEW
         case FormatWAV:  writeWavTags(...)   ← NEW
    → 6. syncDatabase(...)  ← NO CHANGES
    → 7. EventsEmit(TrackMetadataChanged)  ← NO CHANGES
```

**Key insight:** The existing architecture was designed for format extension. The `pipeline.go` switch statement is the single point of modification. Everything downstream (DB sync, FTS5, orphan cleanup, event emission, batch processing, cover art pipeline) is format-agnostic and works unchanged.

### Component Boundaries

| Component | Status | Changes |
|-----------|--------|---------|
| `backend/tagwriter/tagwriter.go` | MODIFY | Add `FormatOGG`, `FormatWAV` constants; extend `DetectFormat()` switch |
| `backend/tagwriter/pipeline.go` | MODIFY | Add two cases to format switch in `WriteTrackTags()` |
| `backend/tagwriter/ogg.go` | **NEW** | `writeOggTags()` — OGG container rewrite with Vorbis Comment manipulation |
| `backend/tagwriter/wav.go` | **NEW** | `writeWavTags()` — RIFF/WAV chunk manipulation with ID3v2 or LIST-INFO |
| `backend/tagwriter/ogg_test.go` | **NEW** | 7 round-trip tests following FLAC pattern |
| `backend/tagwriter/wav_test.go` | **NEW** | 7 round-trip tests following FLAC pattern |
| `backend/tagwriter/dbsync.go` | UNCHANGED | Format-agnostic entity sync |
| `backend/tagwriter/helpers_test.go` | UNCHANGED | Shared test helpers (`tinyJPEG`, `assertEqual`, etc.) |
| `backend/metadata/tags.go` | UNCHANGED | `dhowden/tag` already reads OGG and WAV tags |
| `backend/fileutil/atomicwrite.go` | UNCHANGED | Used by both new writers |
| `frontend/src/components/track-details/` | UNCHANGED | Format-agnostic edit UI |

## OGG Vorbis Writer: `writeOggTags()`

### Container Structure

An OGG Vorbis file consists of OGG pages containing three types of Vorbis packets:
1. **Identification header** (page 0, BOS flag) — audio parameters, must not be modified
2. **Comment header** (page 1) — Vorbis Comments (tags) + optional METADATA_BLOCK_PICTURE
3. **Setup header** (page 1 or 2) — codebooks, must not be modified
4. **Audio data** (remaining pages) — compressed audio, must not be modified

Writing tags means **replacing only the comment header packet** while preserving everything else exactly.

### Approach: Full-File Rewrite via AtomicWrite

**Why full rewrite, not in-place:** Changing the comment header changes its size. OGG pages have fixed-size segment tables (max 255 segments × 255 bytes = ~64KB per page). A larger comment header may require different page segmentation. Every subsequent page has a page sequence number and CRC-32 checksum that must be recalculated. In-place editing is impossible — the file must be rewritten.

**This is fine:** AtomicWrite already does full-file rewrite for MP3 and FLAC. OGG Vorbis files are typically 3-10MB (compressed audio). Memory usage is bounded because we stream page-by-page, not load the entire file.

### Implementation Strategy

```go
func writeOggTags(logger *slog.Logger, filePath string, changes TagChanges) error {
    // 1. Open and parse OGG file page-by-page
    // 2. Read the three header packets (identification, comment, setup)
    // 3. Parse existing Vorbis Comment from comment header packet
    // 4. Apply changes to Vorbis Comment fields (same field mapping as FLAC)
    // 5. If cover_art changed: encode METADATA_BLOCK_PICTURE and add/remove
    //    from Vorbis Comment as base64 field
    // 6. Re-serialize comment header packet
    // 7. AtomicWrite: stream all pages to temp file
    //    - Page 0 (BOS): rewrite with correct CRC (identification header unchanged)
    //    - Page 1+: rewrite with new comment header + setup header, recalculate
    //      segmentation and CRC
    //    - Remaining pages: copy byte-for-byte (page sequence numbers and CRCs
    //      only need recalculation if page boundaries shifted)
    // 8. Rename over original
}
```

### OGG Page Structure (For Implementation)

```
OGG Page Header (27 bytes + segment table):
  - Capture pattern: "OggS" (4 bytes)
  - Stream structure version: 0 (1 byte)
  - Header type flag: BOS/EOS/continued (1 byte)
  - Absolute granule position: int64 (8 bytes)
  - Stream serial number: uint32 (4 bytes)
  - Page sequence number: uint32 (4 bytes)
  - CRC-32 checksum: uint32 (4 bytes)
  - Number of page segments: uint8 (1 byte)
  - Segment table: [num_segments]uint8

OGG Page Body:
  - Raw data (sum of segment table values bytes)
```

### Vorbis Comment Format (Shared with FLAC)

The comment header packet uses the exact same Vorbis Comment format as FLAC's Vorbis Comment metadata block, with one difference:
- **In FLAC:** Vorbis Comments are a metadata block (type 4), binary data
- **In OGG:** Vorbis Comments are preceded by a 7-byte Vorbis packet header (`\x03vorbis`)

The field mapping is identical to `applyFlacTextChanges()`:

| Field | Vorbis Comment Key |
|-------|-------------------|
| title | TITLE |
| artist | ARTIST |
| album | ALBUM |
| album_artist | ALBUMARTIST |
| genre | GENRE |
| year | DATE |
| track_number | TRACKNUMBER |
| disc_number | DISCNUMBER |
| composer | COMPOSER |

### Code Reuse Opportunity

The `replaceVorbisComment()` function in `flac.go` operates on `*flacvorbis.MetaDataBlockVorbisComment` which is specific to the `go-flac/flacvorbis` library. However, the Vorbis Comment binary format is identical across FLAC and OGG. Two approaches:

1. **Build a minimal Vorbis Comment parser/serializer** (~80 lines) directly in `ogg.go` that reads/writes the `vendor_string + comments[]` binary format. This avoids pulling in FLAC dependencies for OGG files. The existing `parseVorbisComment()` helper in `flac_test.go` already demonstrates the parsing logic — promote it to a shared implementation.

2. **Reuse `go-flac/flacvorbis`** by constructing a fake `MetaDataBlock` from the OGG comment packet (strip the 7-byte `\x03vorbis` prefix). This is hacky and creates a false dependency.

**Recommendation:** Approach 1. Write a self-contained `vorbisComment` type in `ogg.go` with `parse(data []byte)` and `marshal() []byte` methods. The format is simple (little-endian length-prefixed strings) and the test already has the parser. This is ~80 lines and avoids coupling.

### Cover Art in OGG: METADATA_BLOCK_PICTURE

OGG Vorbis stores cover art as a Vorbis Comment field named `METADATA_BLOCK_PICTURE`. The value is a base64-encoded binary blob using the same FLAC PICTURE block format:

```
METADATA_BLOCK_PICTURE binary format:
  - Picture type: uint32 BE (3 = front cover)
  - MIME string length: uint32 BE
  - MIME string: UTF-8
  - Description length: uint32 BE
  - Description: UTF-8
  - Width: uint32 BE
  - Height: uint32 BE
  - Color depth: uint32 BE
  - Colors used: uint32 BE
  - Data length: uint32 BE
  - Data: raw image bytes
```

This is then base64-encoded and stored as: `METADATA_BLOCK_PICTURE=<base64 data>`

**Implementation:** Encode using `encoding/base64` and the same `detectMIME()` helper. Width/height/depth can be set to 0 (players derive them from the image data). This adds ~30 lines to the cover art handling.

**Read-back verification:** `dhowden/tag` already reads `METADATA_BLOCK_PICTURE` from OGG files and returns it via the `Picture()` method. Round-trip tests will work with `metadata.ExtractTags()` unchanged.

### No Existing Go Library

**Confirmed:** There is no pure-Go library for writing OGG Vorbis tags. The existing `jfreymuth/oggvorbis` library (already an indirect dependency via beep) is **read-only** — it provides `NewReader()`, `Read()`, `CommentHeader()`, and `GetCommentHeader()` but no write functionality. The `ogg.go` source reveals the page-level reading infrastructure (`page.read()`, `page.readHeader()`, `page.readContent()`) but no page writing.

**Impact:** We must implement OGG page writing ourselves. This is ~200 lines of code for the page serializer + CRC calculation. The `jfreymuth/oggvorbis` package's `crc.go` provides the CRC-32 lookup table (`crcUpdate()`) we can reference for the polynomial (0x04C11DB7, same as in the OGG spec). However, since it's unexported, we must either vendor the CRC table or compute it at init time.

### Risk Assessment

| Risk | Severity | Mitigation |
|------|----------|------------|
| OGG page re-segmentation correctness | HIGH | Comprehensive round-trip tests; verify with `dhowden/tag` read-back |
| CRC-32 calculation error | HIGH | Use the same polynomial as the OGG spec; test against known-good files |
| METADATA_BLOCK_PICTURE encoding | LOW | Well-documented format; base64 encoding is trivial |
| Large OGG files (>100MB) | LOW | OGG Vorbis files are typically <15MB; stream page-by-page |
| Multi-stream OGG files | LOW | Music files are single-stream; reject multi-stream as unsupported |

## WAV Writer: `writeWavTags()`

### Container Structure

WAV files use the RIFF (Resource Interchange File Format) container:

```
RIFF header:
  "RIFF" (4 bytes)
  File size - 8 (uint32 LE)
  "WAVE" (4 bytes)

Chunks (in any order):
  "fmt " chunk — audio format parameters (required)
  "data" chunk — raw PCM audio samples (required)
  "LIST" chunk (subtype "INFO") — metadata as sub-chunks
  "id3 " or "ID3 " chunk — embedded ID3v2 tag
  other chunks (fact, cue, etc.)
```

### Metadata Approach: ID3v2 in RIFF Chunk

WAV metadata can be stored in two ways:
1. **LIST-INFO chunks:** Simple key-value pairs (`INAM`=title, `IART`=artist, `IPRD`=album, etc.). No cover art support. Limited charset (originally ASCII, some tools use UTF-8).
2. **id3 chunk:** A full ID3v2 tag embedded in a RIFF chunk named `id3 ` or `ID3 `. Supports all fields including cover art. Most modern tools (foobar2000, MusicBee, TagLib) use this approach.

**Recommendation:** Use the **id3 chunk** approach because:
- Reuses the existing `bogem/id3v2` library already used by `writeMp3Tags()`
- Supports cover art embedding (LIST-INFO does not)
- `dhowden/tag` reads ID3v2 from WAV files, so round-trip tests work
- Consistent field semantics with MP3 tag writing

### Implementation Strategy

```go
func writeWavTags(logger *slog.Logger, filePath string, changes TagChanges) error {
    // 1. Open WAV file, parse RIFF header
    // 2. Enumerate chunks: find existing "id3 " chunk (if any),
    //    locate "fmt " and "data" chunks
    // 3. If existing id3 chunk found:
    //    a. Parse existing ID3v2 tag
    //    b. Apply changes using same applyTextChanges()/applyCoverArtChanges()
    //       as MP3 writer
    //    c. Serialize updated tag
    // 4. If no existing id3 chunk:
    //    a. Create new ID3v2 tag
    //    b. Apply changes
    //    c. Serialize
    // 5. AtomicWrite: rebuild RIFF file
    //    a. Write RIFF header with new total size
    //    b. Copy all original chunks EXCEPT old id3 chunk
    //    c. Append new id3 chunk (with proper RIFF chunk header)
    //    d. Update RIFF file size in header
}
```

### Code Reuse with MP3 Writer

The MP3 writer's `applyTextChanges(tag, changes)` and `applyCoverArtChanges(tag, changes)` functions operate on `*id3v2.Tag` objects. These exact functions can be reused by the WAV writer since the ID3v2 tag format is identical:

```go
// In wav.go — reuse existing functions from mp3.go:
tag, err := id3v2.Open(...)  // or id3v2.ParseReader(...)
applyTextChanges(tag, changes)    // ← same function from mp3.go
applyCoverArtChanges(tag, changes) // ← same function from mp3.go
```

### RIFF Chunk Parser (~100 lines)

A minimal RIFF parser needs to:
1. Read 12-byte RIFF header (`"RIFF" + size + "WAVE"`)
2. Iterate chunks: 8-byte chunk header (`id[4] + size[4]`), skip body
3. Track positions and sizes of each chunk
4. Handle padding bytes (RIFF chunks are word-aligned — if data size is odd, a pad byte follows)

This is straightforward binary parsing. No external library needed.

### AtomicWrite for Large WAV Files

**Concern:** WAV files can be very large (uncompressed audio: a 60-minute CD-quality WAV is ~630MB). AtomicWrite creates a full copy in a temp file before renaming.

**Assessment:** This is acceptable because:
1. AtomicWrite already streams data via `io.Copy` — it doesn't load the file into memory
2. The WAV writer copies chunks sequentially: read chunk from source → write to temp file
3. Disk space for the temp file is the only cost (~2× file size temporarily)
4. The alternative (in-place chunk modification) risks corruption if the process is interrupted mid-write
5. The existing FLAC writer already handles large files this way (with a warning for >500MB)

**Practical consideration:** WAV files >500MB are rare in music libraries (they're typically ripped CDs at ~30-50MB per track, or high-resolution at ~150MB). Add the same size warning as the FLAC writer.

```go
if info.Size() > largeSizeThreshold {
    logger.Warn("large WAV file may take extra time/space for atomic write",
        slog.String("path", filePath),
        slog.Int64("size", info.Size()),
    )
}
```

### Cover Art in WAV

Since we're using the id3 chunk approach, cover art embedding uses the exact same APIC frame mechanism as MP3. The `applyCoverArtChanges()` function handles this already.

### Risk Assessment

| Risk | Severity | Mitigation |
|------|----------|------------|
| Large file temp space | MEDIUM | Log warning for >500MB; streaming copy avoids memory issues |
| RIFF chunk alignment (pad bytes) | MEDIUM | Follow spec: odd-sized chunks get 1 pad byte |
| Existing LIST-INFO metadata | LOW | Preserve LIST-INFO chunks as-is; only modify/add id3 chunk |
| `bogem/id3v2` reading from WAV | LOW | Library may need `ParseReader` instead of `Open` (investigate) |
| WAV files without existing tags | LOW | Create new id3 chunk; all other chunks preserved |

## Format Detection Extension

### Current Implementation (`tagwriter.go`)

```go
func DetectFormat(filePath string) (AudioFormat, error) {
    ext := strings.ToLower(filepath.Ext(filePath))
    switch ext {
    case ".mp3":
        return FormatMP3, nil
    case ".flac":
        return FormatFLAC, nil
    default:
        return "", fmt.Errorf("%w: %s", errUnsupportedFormat, ext)
    }
}
```

### Required Changes

```go
const (
    FormatMP3  AudioFormat = "mp3"
    FormatFLAC AudioFormat = "flac"
    FormatOGG  AudioFormat = "ogg"   // NEW
    FormatWAV  AudioFormat = "wav"   // NEW
)

func DetectFormat(filePath string) (AudioFormat, error) {
    ext := strings.ToLower(filepath.Ext(filePath))
    switch ext {
    case ".mp3":
        return FormatMP3, nil
    case ".flac":
        return FormatFLAC, nil
    case ".ogg":              // NEW
        return FormatOGG, nil // NEW
    case ".wav":              // NEW
        return FormatWAV, nil // NEW
    default:
        return "", fmt.Errorf("%w: %s", errUnsupportedFormat, ext)
    }
}
```

**Extension-based detection is sufficient.** The existing approach works because:
1. The metadata package already uses extension-based routing for decoding (`metadata/decoder.go`)
2. YellowJacket only indexes files with known extensions (`.mp3`, `.flac`, `.ogg`, `.wav`)
3. File magic-byte detection is unnecessary — if a file is in the DB, it was already validated during scan

### Pipeline Switch Extension

In `pipeline.go`, the format switch becomes:

```go
switch format {
case FormatMP3:
    err = writeMp3Tags(tw.logger, audioFile.FilePath, changes)
case FormatFLAC:
    err = writeFlacTags(tw.logger, audioFile.FilePath, changes)
case FormatOGG:
    err = writeOggTags(tw.logger, audioFile.FilePath, changes)
case FormatWAV:
    err = writeWavTags(tw.logger, audioFile.FilePath, changes)
default:
    err = fmt.Errorf("%w: %s", errUnsupportedFormat, format)
}
```

## Frontend Impact: None

The track-details dialog component is **completely format-agnostic**. It:
1. Shows the same 8 editable fields regardless of format
2. Shows the same cover art pick/replace/remove UI
3. Sends the same `TagChanges` diff map to `WriteTrackTagsByPath()`
4. Receives the same `TrackMetadataChanged` event
5. Uses the same three-state field model for batch editing

The backend handles all format-specific logic. The frontend never sees or cares about the audio format.

**Verified:** No frontend changes needed for OGG or WAV tag writing.

## Test Strategy

### Follow FLAC Round-Trip Pattern (7 Tests per Format)

The FLAC writer has 7 tests that verify the complete write→read cycle using `metadata.ExtractTags()` for read-back. The same test structure applies to OGG and WAV:

| Test | What It Verifies |
|------|-----------------|
| `TestWriteOggTags_TextFields` | All 9 text fields round-trip correctly |
| `TestWriteOggTags_CoverArt` | METADATA_BLOCK_PICTURE embedded and readable |
| `TestWriteOggTags_ClearCoverArt` | Cover art removal works |
| `TestWriteOggTags_PartialUpdate` | Unchanged fields preserved |
| `TestWriteOggTags_PreservesAudioData` | Audio stream intact after tag write |
| `TestWriteOggTags_ReplaceComment` | No duplicate Vorbis Comment entries |
| `TestWriteOggTags_AtomicSafety` | Failed write leaves file untouched |

Same 7 tests for WAV (`TestWriteWavTags_*`).

### Test Fixture Helpers

Each format needs a `makeMinimal*()` helper:

- **`makeMinimalOGG(t, path)`** — Creates a minimal valid OGG Vorbis file containing: BOS page with identification header, comment header page (empty Vorbis Comment), EOS page with setup header + minimal audio frame. This is complex (~60 lines) but required for round-trip testing.

- **`makeMinimalWAV(t, path)`** — Creates a minimal valid WAV file containing: RIFF header, `fmt ` chunk (PCM, 44100Hz, 16-bit, mono), `data` chunk (brief silence). This is simple (~30 lines) — just binary header construction.

### Read-Back Verification

Both formats use `metadata.ExtractTags()` (which uses `dhowden/tag`) for read-back verification:
- **OGG:** `dhowden/tag` reads Vorbis Comments from OGG files including `METADATA_BLOCK_PICTURE`. **Verified:** the library's `ogg.go` and `vorbis.go` handle this.
- **WAV:** `dhowden/tag` reads ID3v2 tags from WAV files (it detects the `id3 ` chunk in the RIFF container). **Verified:** the library handles this per its README (MP3/MP4/OGG/FLAC metadata parsing).

**Confidence:** HIGH — `dhowden/tag` is already the read-back library for MP3 and FLAC tests. It supports OGG and WAV reading.

## Suggested Build Order

Based on dependency analysis and risk levels:

### Phase 1: WAV Writer (Lower Risk, Faster)

**Rationale:** WAV writing reuses the existing `bogem/id3v2` library and the existing `applyTextChanges()`/`applyCoverArtChanges()` functions. The RIFF container is much simpler than OGG (no checksums, no page segmentation). This can be built and tested quickly, giving confidence in the pipeline extension pattern before tackling OGG.

1. Add `FormatWAV` constant and extend `DetectFormat()`
2. Write RIFF chunk parser (~100 lines in `wav.go`)
3. Implement `writeWavTags()` using id3v2 tag in RIFF chunk
4. Add pipeline switch case
5. Write `makeMinimalWAV()` test fixture
6. Write 7 round-trip tests
7. Verify with `make lint`

### Phase 2: OGG Vorbis Writer (Higher Risk, More Code)

**Rationale:** OGG requires implementing the page-level write infrastructure (CRC-32, segmentation, page serialization) from scratch. This is the riskiest part of the milestone and benefits from having the WAV writer already proving the pipeline extension pattern works.

1. Implement OGG CRC-32 calculation (~30 lines)
2. Implement OGG page serializer (~80 lines)
3. Implement Vorbis Comment parser/serializer (~80 lines)
4. Implement METADATA_BLOCK_PICTURE encoding (~40 lines)
5. Implement `writeOggTags()` orchestrator (~100 lines)
6. Add `FormatOGG` constant and pipeline switch case
7. Write `makeMinimalOGG()` test fixture (~60 lines)
8. Write 7 round-trip tests
9. Verify with `make lint`

### Phase 3: Cleanup

1. Remove OGG/WAV from "Out of Scope" in PROJECT.md
2. Update milestone status
3. Fix any lint warnings from v1.2

### Total New Code Estimate

| Component | Lines (approx) |
|-----------|----------------|
| `ogg.go` (writer + helpers) | ~350 |
| `wav.go` (writer + RIFF parser) | ~200 |
| `ogg_test.go` | ~350 |
| `wav_test.go` | ~250 |
| `tagwriter.go` changes | ~10 |
| `pipeline.go` changes | ~5 |
| **Total** | **~1,165** |

## Patterns to Follow

### Pattern 1: Format Writer Function Signature

**What:** All format writers follow the same signature: `func write*Tags(logger *slog.Logger, filePath string, changes TagChanges) error`

**Why:** Keeps the pipeline switch clean and uniform. No interface needed — package-internal functions with consistent signatures are simpler.

**Example:**
```go
func writeOggTags(logger *slog.Logger, filePath string, changes TagChanges) error { ... }
func writeWavTags(logger *slog.Logger, filePath string, changes TagChanges) error { ... }
```

### Pattern 2: AtomicWrite Integration

**What:** Every writer creates the complete output file inside the `AtomicWrite` callback, writing to `tmp *os.File`.

**Example (from existing FLAC writer):**
```go
return fileutil.AtomicWrite(logger, filePath, func(tmp *os.File) error {
    _, writeErr := f.WriteTo(tmp)
    return writeErr
})
```

### Pattern 3: Test Fixture + Round-Trip Verification

**What:** Each format has a `makeMinimal*()` helper that creates a valid file, and tests verify by writing tags then reading back with `metadata.ExtractTags()`.

**Why:** Tests validate the complete pipeline without external tools. The same `metadata.ExtractTags()` function used in production reads back the tags, ensuring what we write is what gets read.

## Anti-Patterns to Avoid

### Anti-Pattern 1: FormatWriter Interface

**What:** Creating a `FormatWriter` interface with `Write(path string, changes TagChanges) error`

**Why bad:** The package has only 4 format writers, all internal. An interface adds abstraction without value. The switch statement is clearer and the functions share a signature by convention.

**Instead:** Keep format writers as package-internal functions with matching signatures.

### Anti-Pattern 2: In-Place OGG Page Modification

**What:** Attempting to modify OGG pages in-place to avoid full file rewrite.

**Why bad:** OGG pages have checksums and sequence numbers. Changing the comment packet size shifts all subsequent page offsets. In-place modification requires recalculating every downstream page's CRC, which is effectively a full rewrite anyway — but without crash safety.

**Instead:** Full-file rewrite via AtomicWrite.

### Anti-Pattern 3: In-Place RIFF Chunk Insertion

**What:** Attempting to insert or resize RIFF chunks in-place.

**Why bad:** Inserting a new id3 chunk or resizing an existing one shifts all subsequent chunks. The RIFF header's total size must be updated. In-place modification risks corruption.

**Instead:** Full-file rewrite via AtomicWrite.

### Anti-Pattern 4: Shared Vorbis Comment Code via go-flac/flacvorbis

**What:** Importing `go-flac/flacvorbis` in the OGG writer to reuse Vorbis Comment parsing.

**Why bad:** Creates a dependency on a FLAC-specific library for OGG files. The `flacvorbis` types are coupled to FLAC `MetaDataBlock` structures. The binary format is simple enough to parse directly.

**Instead:** Self-contained Vorbis Comment parser in `ogg.go` (~80 lines).

## Scalability Considerations

| Concern | Typical Case | Edge Case | Approach |
|---------|--------------|-----------|----------|
| OGG file size | 3-10 MB | 50 MB live recording | Stream page-by-page, no full-file memory load |
| WAV file size | 30-50 MB (CD track) | 2 GB (24-bit/96kHz long recording) | Streaming copy via `io.Copy`; warn for >500MB |
| Temp disk space | 2× file size briefly | 2× 2GB = 4GB temp | Same concern as FLAC; document as known limitation |
| Batch edit 100 WAV files | Sequential, 30-50 MB each | 100 × 50 MB = 5 GB total I/O | Existing batch pipeline with progress events |

## Sources

- OGG container format specification: https://xiph.org/ogg/doc/rfc3533.txt (HIGH confidence)
- Vorbis Comment specification: https://xiph.org/vorbis/doc/v-comment.html (HIGH confidence)
- Vorbis I specification: https://xiph.org/vorbis/doc/Vorbis_I_spec.html (HIGH confidence)
- METADATA_BLOCK_PICTURE in Vorbis Comments: https://xiph.org/flac/format.html#metadata_block_picture (HIGH confidence)
- RIFF/WAV format: https://www.mmsp.ece.mcgill.ca/documents/AudioFormats/WAVE/WAVE.html (HIGH confidence)
- `jfreymuth/oggvorbis` package API: https://pkg.go.dev/github.com/jfreymuth/oggvorbis (HIGH confidence — verified read-only)
- `jfreymuth/vorbis` CommentHeader type: https://pkg.go.dev/github.com/jfreymuth/vorbis (HIGH confidence)
- `dhowden/tag` OGG/WAV reading: https://github.com/dhowden/tag (HIGH confidence — used in existing tests)
- `bogem/id3v2` for WAV id3 chunk: https://github.com/bogem/id3v2 (HIGH confidence — used in existing MP3 writer)
- Existing codebase: `backend/tagwriter/` package (analyzed in full)
