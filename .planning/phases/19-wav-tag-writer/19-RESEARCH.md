# Phase 19: WAV Tag Writer - Research

**Researched:** 2026-03-18
**Domain:** WAV RIFF container manipulation with ID3v2 tag embedding
**Confidence:** HIGH

## Summary

WAV files use the RIFF container format. Metadata is embedded as an ID3v2 tag inside a RIFF chunk with ID `id3 ` (lowercase, trailing space). The critical finding is that **neither `bogem/id3v2` nor `dhowden/tag` supports WAV/RIFF natively** — `bogem/id3v2` only handles raw ID3v2 streams (it looks for "ID3" magic bytes at position 0), and `dhowden/tag` only detects "fLaC", "OggS", "ftyp", "ID3", and "DSD " magic bytes but has no RIFF case. This means we need a **custom RIFF chunk parser/writer** for the container level, while still using `bogem/id3v2` for the ID3v2 tag payload itself.

The RIFF structure is simple: a sequence of 8-byte-header chunks (4-byte ID + 4-byte little-endian size) with optional padding bytes for odd-length alignment. The implementation approach is: read all RIFF chunks, preserve them byte-for-byte, extract existing ID3v2 data from the `id3 ` chunk (if present), build/modify the ID3v2 tag using `bogem/id3v2`, then write a new RIFF file with all original chunks plus the updated `id3 ` chunk appended at end. All wrapped in `fileutil.AtomicWrite` for crash safety.

For test read-back, since `dhowden/tag` cannot read WAV files, tests must use a custom RIFF parser to extract the `id3 ` chunk data and then pass it to either `bogem/id3v2.ParseReader()` or `tag.ReadID3v2Tags()` via a `bytes.Reader`.

**Primary recommendation:** Build a ~100-line custom RIFF chunk reader/writer in `wav.go`. Use `bogem/id3v2.NewEmptyTag()` + `ParseReader()` for ID3v2 manipulation, `tag.WriteTo()` for serialization. No new dependencies needed.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
- Preserve ALL non-ID3v2 chunks byte-for-byte when rewriting the WAV file (fmt, data, LIST INFO, bext, cue, smpl, iXML, and any unknown/proprietary chunks)
- Existing RIFF LIST INFO chunks are kept as-is, even if they contain stale metadata after an ID3v2 edit — our reader already prefers ID3v2, so stale INFO won't affect display
- Existing ID3v2 chunks in the WAV are replaced entirely — open with bogem/id3v2, apply changes, write fresh tag (same pattern as MP3 writer)
- ID3v2 chunk placed at end of file (after all other chunks) — most common convention, simplest implementation
- No size limit on embedded cover art — accept whatever the user provides, consistent with MP3/FLAC behavior
- JPEG and PNG only, detected by magic bytes via existing `detectMIME()` function — same as MP3
- Clearing cover art removes the APIC frame only; the ID3v2 chunk is kept even if only text frames remain
- Read and merge existing ID3v2 tags from WAV before applying changes — preserves unknown frames (lyrics, custom tags) added by other tools
- User-facing errors are friendly: "Could not write tags to [filename]: file appears to be damaged" — hide technical details
- Technical error details (chunk offsets, sizes, parse failures) logged via slog for debugging
- Permission-aware messages: distinguish "file is read-only", "file is in use", and generic "write failed"
- Batch error handling identical to MP3/FLAC — use existing BatchFailure struct, no WAV-specific categorization
- Reject RF64/BW64 files (magic bytes 'RF64' instead of 'RIFF') with clear error: "RF64 files are not yet supported"
- Accept BWF (Broadcast Wave) — it's standard RIFF with a bext chunk, which we preserve
- Accept multi-channel WAV — standard RIFF with WAVEFORMATEXTENSIBLE in fmt chunk, which we preserve
- Lenient read, strict write: accept minor spec violations on input (missing padding bytes, incorrect RIFF size), write spec-compliant output (correct padding, correct sizes)
- Warn (slog) above 500MB file size, same as FLAC writer threshold — proceed anyway
- Reject writes that would push output past 4GB (RIFF 32-bit size limit) with clear error: "File too large for WAV format (>4GB). No changes were made." Atomic write ensures original is untouched.

### Claude's Discretion
- RIFF parser implementation approach (custom vs. library)
- Chunk ordering for non-ID3v2 chunks (preserve original order or normalize)
- Exact padding byte handling for odd-length chunks
- Test fixture file construction approach
- Whether to use `album_artist` field mapping via TPE2 (match MP3 writer) or TPE1 variant

### Deferred Ideas (OUT OF SCOPE)
- RF64/BW64 support — future phase (FMT-02 in REQUIREMENTS.md)
- RIFF INFO dual-write alongside ID3v2 — explicitly deferred (FMT-03 in REQUIREMENTS.md)
- BWF bext chunk writing — out of scope (preserve only, not write)
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|-----------------|
| WAV-01 | User can edit all 8 text metadata fields on WAV files via ID3v2 chunk in RIFF container | Custom RIFF parser + `bogem/id3v2` for ID3v2 tag building; reuse `applyTextChanges()` from MP3 writer plus add TPE2 mapping for album_artist |
| WAV-02 | WAV tag writes preserve existing RIFF INFO and other chunks unchanged | RIFF chunk reader preserves all non-`id3 ` chunks byte-for-byte in original order; write them first, then append `id3 ` chunk at end |
| WAV-03 | WAV tag writes preserve audio data identically (lossless round-trip) | All chunks including `data` are copied byte-for-byte; only the `id3 ` chunk is replaced. Verified by comparing `data` chunk bytes before/after |
| WAV-04 | User can embed, replace, and remove cover art via ID3v2 APIC frame | Reuse `applyCoverArtChanges()` from MP3 writer — same APIC frame manipulation on the ID3v2 tag |
| WAV-05 | WAV tag writing uses crash-safe atomic writes | Wrap entire write operation in `fileutil.AtomicWrite()` — same pattern as MP3 and FLAC writers |
| WAV-06 | WAV writer round-trip tests verify all fields via read-back | Custom test helper extracts `id3 ` chunk from RIFF, passes to `tag.ReadID3v2Tags()` for verification; alternatively use `bogem/id3v2.ParseReader()` |
</phase_requirements>

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/bogem/id3v2/v2` | v2.1.4 | Build and serialize ID3v2 tags (frames, APIC, text fields) | Already used by MP3 writer; `NewEmptyTag()`, `ParseReader()`, `WriteTo()` provide full ID3v2 lifecycle |
| `encoding/binary` | stdlib | Read/write little-endian RIFF chunk headers (4-byte sizes) | RIFF is little-endian; stdlib covers all needs |
| `yellowjacket/backend/fileutil` | internal | `AtomicWrite()` for crash-safe write-to-temp-then-rename | Project standard; used by MP3 and FLAC writers |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `github.com/dhowden/tag` | v0.0.0-20240417 | Read-back verification in tests via `ReadID3v2Tags()` | Test assertion: extract `id3 ` chunk bytes, wrap in `bytes.Reader`, call `tag.ReadID3v2Tags()` |
| `io`, `os`, `bytes` | stdlib | File I/O, stream copying, buffer management | Throughout RIFF parsing and writing |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Custom RIFF parser (~100 lines) | `golang.org/x/image/riff` | Read-only (no writer); also read-only streaming API not suited to extracting/replacing individual chunks. Custom is better. |
| Custom RIFF parser | Third-party WAV library (e.g. `go-audio/wav`) | Adds dependency; most WAV libs focus on audio samples not metadata chunks; none handle ID3v2-in-RIFF. Custom is cleaner. |
| `tag.ReadID3v2Tags()` in tests | `bogem/id3v2.ParseReader()` in tests | Both work; `dhowden/tag` matches the project's existing `metadata.ExtractTags` verifier pattern but requires a RIFF extraction wrapper. Use whichever is cleaner. |

## Architecture Patterns

### Recommended Project Structure
```
backend/tagwriter/
├── tagwriter.go     # FormatWAV constant + DetectFormat ".wav" case (modify)
├── pipeline.go      # case FormatWAV: writeWavTags() dispatch (modify)
├── mp3.go           # Existing — applyTextChanges(), applyCoverArtChanges() reused
├── flac.go          # Existing
├── wav.go           # NEW — writeWavTags(), RIFF chunk reader/writer, applyTextChangesWav()
├── wav_test.go      # NEW — createTestWAV(), round-trip tests
└── helpers_test.go  # Existing — tinyJPEG(), testLogger() shared
```

### Pattern 1: RIFF Chunk Reader
**What:** Parse a WAV file into a slice of `riffChunk{id [4]byte, data []byte}` structs, preserving every chunk in order.
**When to use:** At the start of `writeWavTags()` to read the existing file.
**Example:**
```go
type riffChunk struct {
    id   [4]byte
    data []byte
}

// parseRIFF reads a RIFF WAVE file and returns all sub-chunks.
// Returns an error if the file is RF64 or not a valid RIFF WAVE.
func parseRIFF(r io.ReadSeeker) ([]riffChunk, error) {
    var magic [4]byte
    binary.Read(r, binary.LittleEndian, &magic)
    if magic == [4]byte{'R','F','6','4'} {
        return nil, errors.New("RF64 files are not yet supported")
    }
    if magic != [4]byte{'R','I','F','F'} {
        return nil, errors.New("not a RIFF file")
    }
    var riffSize uint32
    binary.Read(r, binary.LittleEndian, &riffSize)
    var formType [4]byte
    binary.Read(r, binary.LittleEndian, &formType)
    if formType != [4]byte{'W','A','V','E'} {
        return nil, errors.New("not a WAVE file")
    }
    // Read sub-chunks until EOF or riffSize exhausted
    var chunks []riffChunk
    for {
        var chunkID [4]byte
        if err := binary.Read(r, binary.LittleEndian, &chunkID); err != nil {
            break // EOF
        }
        var chunkSize uint32
        binary.Read(r, binary.LittleEndian, &chunkSize)
        data := make([]byte, chunkSize)
        io.ReadFull(r, data)
        chunks = append(chunks, riffChunk{id: chunkID, data: data})
        // Skip padding byte for odd-length chunks
        if chunkSize%2 != 0 {
            r.Read(make([]byte, 1))
        }
    }
    return chunks, nil
}
```

### Pattern 2: RIFF Chunk Writer (Write All + ID3v2 at End)
**What:** Write RIFF header, all preserved chunks in original order (excluding old `id3 ` chunk), then append new `id3 ` chunk at end.
**When to use:** Inside `fileutil.AtomicWrite()` callback.
**Example:**
```go
func writeRIFF(w io.Writer, chunks []riffChunk, id3Data []byte) error {
    // Calculate total data size: 4 (WAVE) + sum of (8 + chunkSize + padding) for each chunk
    totalDataSize := uint32(4)  // "WAVE" form type
    for _, c := range chunks {
        padded := c.paddedSize()
        totalDataSize += 8 + padded
    }
    // Add id3 chunk
    id3PaddedSize := uint32(len(id3Data))
    if id3PaddedSize%2 != 0 { id3PaddedSize++ }
    totalDataSize += 8 + id3PaddedSize

    // Check 4GB limit
    if uint64(totalDataSize) + 8 > 0xFFFFFFFF {
        return errors.New("file too large for WAV format (>4GB)")
    }

    // Write RIFF header
    w.Write([]byte("RIFF"))
    binary.Write(w, binary.LittleEndian, totalDataSize)
    w.Write([]byte("WAVE"))

    // Write preserved chunks
    for _, c := range chunks {
        w.Write(c.id[:])
        binary.Write(w, binary.LittleEndian, uint32(len(c.data)))
        w.Write(c.data)
        if len(c.data)%2 != 0 { w.Write([]byte{0}) }
    }

    // Write id3 chunk
    w.Write([]byte("id3 "))
    binary.Write(w, binary.LittleEndian, uint32(len(id3Data)))
    w.Write(id3Data)
    if len(id3Data)%2 != 0 { w.Write([]byte{0}) }
    return nil
}
```

### Pattern 3: Reuse MP3 ID3v2 Tag Manipulation
**What:** Extract existing ID3v2 bytes from RIFF `id3 ` chunk, parse with `bogem/id3v2.ParseReader()`, apply changes with existing `applyTextChanges()` + `applyCoverArtChanges()`, serialize with `tag.WriteTo()`.
**When to use:** Core of `writeWavTags()`.
**Example:**
```go
func writeWavTags(logger *slog.Logger, filePath string, changes TagChanges) error {
    // 1. Open and parse RIFF chunks
    f, _ := os.Open(filePath)
    defer f.Close()
    chunks, err := parseRIFF(f)
    // ... error handling, RF64 check, file size warning

    // 2. Find and extract existing id3 chunk
    var existingID3 []byte
    var preserved []riffChunk
    for _, c := range chunks {
        if isID3ChunkID(c.id) {
            existingID3 = c.data
        } else {
            preserved = append(preserved, c)
        }
    }

    // 3. Parse existing ID3v2 tag or create new empty one
    var tag *id3v2.Tag
    if len(existingID3) > 0 {
        tag, _ = id3v2.ParseReader(bytes.NewReader(existingID3), id3v2.Options{Parse: true})
    } else {
        tag = id3v2.NewEmptyTag()
    }

    // 4. Apply changes (reuse MP3 functions)
    applyTextChanges(tag, changes)
    applyCoverArtChanges(tag, changes)

    // 5. Serialize ID3v2 tag to bytes
    var id3Buf bytes.Buffer
    tag.WriteTo(&id3Buf)

    // 6. Atomic write new RIFF file
    return fileutil.AtomicWrite(logger, filePath, func(tmp *os.File) error {
        return writeRIFF(tmp, preserved, id3Buf.Bytes())
    })
}
```

### Pattern 4: Album Artist via TPE2
**What:** The MP3 writer's `applyTextChanges()` currently does NOT handle `FieldAlbumArtist`. The WAV writer needs it. Map `album_artist` to TPE2 ("Band/Orchestra/Accompaniment") — the de facto standard for album artist in ID3v2.
**When to use:** In a WAV-specific `applyTextChanges` wrapper, or better, fix the gap in the shared `applyTextChanges()`.
**Recommendation:** Add TPE2 mapping to the shared `applyTextChanges()` in mp3.go — this fixes the MP3 writer's missing album_artist support AND gives WAV the same behavior. The FLAC writer already maps this correctly to ALBUMARTIST.

```go
// Add to applyTextChanges() in mp3.go:
if v, ok := changes[FieldAlbumArtist].(string); ok {
    tpe2ID := tag.CommonID("Band/Orchestra/Accompaniment")
    tag.DeleteFrames(tpe2ID)
    tag.AddTextFrame(tpe2ID, id3v2.EncodingUTF8, v)
}
```

### Anti-Patterns to Avoid
- **Trying to use `id3v2.Open()` on WAV files:** It expects "ID3" magic at byte 0; a WAV file starts with "RIFF". This will return an empty tag with no error, silently discarding existing metadata.
- **Using `id3v2.Tag.Save()` for WAV:** Save() seeks past the original ID3v2 tag size and copies "audio data" — this makes no sense for RIFF containers where the ID3v2 tag is a sub-chunk, not a prefix.
- **Calling `dhowden/tag.ReadFrom()` on a WAV file:** Returns `ErrNoTagsFound` because it has no RIFF detection case. Must extract the `id3 ` chunk bytes first.
- **Forgetting the padding byte:** Odd-length RIFF chunks MUST be followed by a padding byte (0x00) for word alignment. Both reading and writing must account for this.
- **Using `ID3 ` (uppercase) chunk ID:** The de facto standard is `id3 ` (lowercase). Some tools write `ID3 ` — reading should accept both (case-insensitive), but writing should use `id3 ` (lowercase).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| ID3v2 tag encoding/decoding | Custom ID3v2 frame parser | `bogem/id3v2/v2` (`NewEmptyTag`, `ParseReader`, `WriteTo`, `AddAttachedPicture`) | ID3v2 has synchsafe integers, multiple encodings (UTF-8, UTF-16, ISO-8859-1), APIC frame structure — deceptively complex |
| Crash-safe file replacement | Manual temp file + rename | `fileutil.AtomicWrite()` | Already handles orphan cleanup, permission preservation, cross-device detection |
| JPEG/PNG detection | Custom image format sniffer | `detectMIME()` in tagwriter.go | Already exists, handles the JPEG (0xFF 0xD8) and PNG (0x89 PNG) magic bytes |

**Key insight:** The RIFF container parsing IS simple enough to hand-roll (~100 lines). The ID3v2 tag handling is NOT — it absolutely needs the library. The separation is: custom RIFF container, library ID3v2 payload.

## Common Pitfalls

### Pitfall 1: Padding Byte Alignment
**What goes wrong:** Odd-length chunks not followed by a padding byte cause subsequent chunks to be misaligned. Some tools are lenient; others break.
**Why it happens:** The RIFF spec requires chunks to start at even byte offsets, but the chunk size field reports the actual data length (not padded).
**How to avoid:** When reading: after reading `chunkSize` bytes of data, if `chunkSize` is odd, read and discard one additional byte. When writing: after writing chunk data, if data length is odd, write one zero byte.
**Warning signs:** Tests pass on files you create but fail on files from real-world tools.

### Pitfall 2: RIFF Size Field Calculation
**What goes wrong:** The RIFF header's size field must equal the total file size minus 8 (the "RIFF" ID + size field itself). Getting this wrong makes some players reject the file.
**Why it happens:** Easy to forget to include the 4-byte "WAVE" form type in the count, or to miscalculate padding.
**How to avoid:** Calculate total: `4 (WAVE) + Σ(8 + paddedChunkSize)` for all sub-chunks including the `id3 ` chunk.
**Warning signs:** File plays in some players but not others; hex editor shows mismatch between RIFF size and actual file size.

### Pitfall 3: Chunk ID Case Sensitivity for `id3 `
**What goes wrong:** The `id3 ` chunk ID is conventionally lowercase, but some tools (notably older versions of Windows Media Player, MediaMonkey) write `ID3 ` (uppercase). If you only look for one case, you miss existing tags.
**Why it happens:** There's no formal standard — the de facto convention varies.
**How to avoid:** Accept both `id3 ` and `ID3 ` on read (case-insensitive comparison for the first 3 bytes). Write `id3 ` (lowercase) — it's the more common convention used by most modern tools.
**Warning signs:** Existing metadata is duplicated instead of replaced on some WAV files.

### Pitfall 4: 4GB RIFF Size Limit
**What goes wrong:** RIFF uses a 32-bit unsigned size field. Files >4GB cannot be represented.
**Why it happens:** Adding a large cover art image to an already-large WAV file could push it over.
**How to avoid:** Calculate the total output size before writing. If it exceeds `0xFFFFFFFF` (4,294,967,295) bytes, return an error. The atomic write pattern ensures the original file is untouched.
**Warning signs:** Silently truncated files or integer overflow in the size field.

### Pitfall 5: `bogem/id3v2.ParseReader()` With Empty/No ID3v2 Data
**What goes wrong:** If there's no existing `id3 ` chunk, calling `ParseReader` with empty/nil data would fail.
**Why it happens:** Not all WAV files have an ID3v2 chunk.
**How to avoid:** Check if existing ID3v2 data exists. If not, use `id3v2.NewEmptyTag()` instead of `ParseReader()`.
**Warning signs:** Error on WAV files that have never been tagged.

### Pitfall 6: dhowden/tag Cannot Read WAV Files
**What goes wrong:** `metadata.ExtractTags()` calls `tag.ReadFrom()` which does NOT detect RIFF format — returns `ErrNoTagsFound` for any WAV file, even one with a valid `id3 ` chunk.
**Why it happens:** `dhowden/tag` v0.0.0-20240417 has no RIFF case in its `ReadFrom()` switch statement. It only checks for "fLaC", "OggS", "ftyp", "ID3", "DSD ".
**How to avoid:** Tests CANNOT use `metadata.ExtractTags()` for WAV read-back. Instead, write a test helper that: (1) parses RIFF chunks, (2) extracts `id3 ` chunk data, (3) passes it to `tag.ReadID3v2Tags()` via `bytes.NewReader()`. This gives the same `Metadata` interface used by other tests.
**Warning signs:** All WAV round-trip tests fail with "no tags found" even though the write succeeded.

## Code Examples

### Test WAV Fixture Construction
```go
// createTestWAV builds a minimal valid WAV file with optional initial
// ID3v2 metadata. The file contains a minimal PCM fmt chunk and a
// short silence data chunk, plus an id3 chunk if fields are provided.
func createTestWAV(t *testing.T, dir, name string, fields TagChanges) string {
    t.Helper()
    path := filepath.Join(dir, name)
    var buf bytes.Buffer

    // fmt chunk: PCM, mono, 44100 Hz, 16-bit
    fmtData := []byte{
        0x01, 0x00, // AudioFormat: PCM
        0x01, 0x00, // NumChannels: 1
        0x44, 0xAC, 0x00, 0x00, // SampleRate: 44100
        0x88, 0x58, 0x01, 0x00, // ByteRate: 88200
        0x02, 0x00, // BlockAlign: 2
        0x10, 0x00, // BitsPerSample: 16
    }

    // data chunk: 100 samples of silence (200 bytes)
    audioData := make([]byte, 200)

    // Build ID3v2 chunk if fields provided
    var id3Data []byte
    if len(fields) > 0 {
        tag := id3v2.NewEmptyTag()
        tag.SetDefaultEncoding(id3v2.EncodingUTF8)
        applyTextChanges(tag, fields)
        applyCoverArtChanges(tag, fields)
        var id3Buf bytes.Buffer
        tag.WriteTo(&id3Buf)
        id3Data = id3Buf.Bytes()
    }

    // Calculate total RIFF data size
    riffDataSize := uint32(4) // "WAVE"
    riffDataSize += 8 + uint32(len(fmtData))
    riffDataSize += 8 + uint32(len(audioData))
    if len(id3Data) > 0 {
        riffDataSize += 8 + uint32(len(id3Data))
        if len(id3Data)%2 != 0 { riffDataSize++ }
    }

    // Write RIFF header
    buf.Write([]byte("RIFF"))
    binary.Write(&buf, binary.LittleEndian, riffDataSize)
    buf.Write([]byte("WAVE"))

    // Write fmt chunk
    buf.Write([]byte("fmt "))
    binary.Write(&buf, binary.LittleEndian, uint32(len(fmtData)))
    buf.Write(fmtData)

    // Write data chunk
    buf.Write([]byte("data"))
    binary.Write(&buf, binary.LittleEndian, uint32(len(audioData)))
    buf.Write(audioData)

    // Write id3 chunk if present
    if len(id3Data) > 0 {
        buf.Write([]byte("id3 "))
        binary.Write(&buf, binary.LittleEndian, uint32(len(id3Data)))
        buf.Write(id3Data)
        if len(id3Data)%2 != 0 { buf.WriteByte(0) }
    }

    os.WriteFile(path, buf.Bytes(), 0o644)
    return path
}
```

### Test Read-Back Helper (Extract ID3v2 from WAV)
```go
// readWavID3Tags extracts the id3 chunk from a WAV file and parses
// it with dhowden/tag for test verification, returning the same
// Metadata interface used by other tagwriter tests.
func readWavID3Tags(t *testing.T, path string) *metadata.TrackMetadata {
    t.Helper()
    f, _ := os.Open(path)
    defer f.Close()

    chunks, err := parseRIFF(f)
    if err != nil { t.Fatalf("parseRIFF: %v", err) }

    for _, c := range chunks {
        if isID3ChunkID(c.id) {
            r := bytes.NewReader(c.data)
            m, err := tag.ReadID3v2Tags(r)
            if err != nil { t.Fatalf("ReadID3v2Tags: %v", err) }
            // Convert dhowden/tag Metadata to our TrackMetadata
            trackNum, _ := m.Track()
            discNum, _ := m.Disc()
            meta := &metadata.TrackMetadata{
                Title: m.Title(), Artist: m.Artist(),
                Album: m.Album(), AlbumArtist: m.AlbumArtist(),
                Genre: m.Genre(), Year: m.Year(),
                TrackNumber: trackNum, DiscNumber: discNum,
                Composer: m.Composer(),
            }
            if pic := m.Picture(); pic != nil {
                meta.Picture = &metadata.PictureData{
                    Data: pic.Data, MIMEType: pic.MIMEType, Ext: pic.Ext,
                }
            }
            return meta
        }
    }
    t.Fatal("no id3 chunk found in WAV file")
    return nil
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| RIFF INFO chunks for WAV metadata | ID3v2-in-RIFF via `id3 ` chunk | ~2010 era | ID3v2 supports album_artist, cover art, disc number — INFO cannot. All modern taggers (foobar2000, MusicBrainz Picard, Mp3tag) use ID3v2 in WAV. |
| Various case conventions for ID3 chunk ID | `id3 ` (lowercase) is de facto standard | Gradual convergence | Write lowercase; accept both on read for compatibility |

**Deprecated/outdated:**
- RIFF INFO as primary metadata target: Cannot represent album_artist, disc_number, or embedded cover art. Explicitly deferred (FMT-03).

## Open Questions

1. **Album artist in MP3 writer**
   - What we know: The MP3 writer's `applyTextChanges()` does NOT map `FieldAlbumArtist` to TPE2. The FLAC writer correctly maps it to ALBUMARTIST. The MP3 test suite doesn't test album_artist.
   - What's unclear: Is this an intentional omission (handled at a different layer) or a bug?
   - Recommendation: Add TPE2 mapping to `applyTextChanges()` in mp3.go as part of this phase. The WAV writer reuses this function, so it needs album_artist support. This also fixes a latent gap in MP3 writing.

2. **dhowden/tag WAV support in metadata.ExtractTags()**
   - What we know: `dhowden/tag` does not support WAV/RIFF. The project's `metadata.ExtractTags()` wraps `tag.ReadFrom()` which will return `ErrNoTagsFound` for WAV files.
   - What's unclear: Will the existing library scanner (`metadata.ExtractAllMetadata`) work for WAV tag reading during library scans? (This is an existing issue unrelated to writing, but worth noting.)
   - Recommendation: For this phase, only solve it in tests via a custom RIFF extraction helper. The broader metadata reading issue is separate.

## Sources

### Primary (HIGH confidence)
- `bogem/id3v2/v2` source code (v2.1.4) — verified: `Open()` calls `os.Open` + `ParseReader`; `ParseReader` calls `tag.parse()` which calls `parseHeader()` checking for "ID3" magic; `WriteTo()` writes raw ID3v2 bytes; `Save()` assumes MP3 structure (seeks past original tag, copies audio). **No WAV/RIFF support.**
- `dhowden/tag` source code (v0.0.0-20240417053706) — verified: `ReadFrom()` switch has no RIFF case; `ReadID3v2Tags()` accepts `io.ReadSeeker` starting with "ID3" magic. **No WAV/RIFF support, but ReadID3v2Tags works on extracted chunk data.**
- RIFF format specification (Multimedia Programming Interface and Data Specifications 1.0, IBM/Microsoft, August 1991) — chunk structure: 4-byte ID + 4-byte LE size + data + optional padding byte
- `golang.org/x/image/riff` package — confirmed read-only; `NewReader` returns streaming `Reader` with no write capability

### Secondary (MEDIUM confidence)
- Wikipedia RIFF article — confirms chunk structure, padding rules, RF64 extension mechanism
- Library of Congress WAV format description — confirms magic bytes `RIFF....WAVE`, RIFF size is 32-bit LE

### Tertiary (LOW confidence)
- Convention that `id3 ` (lowercase) is preferred over `ID3 ` — based on observed behavior of major taggers (foobar2000, MusicBrainz Picard, Mp3tag). No formal spec mandates case.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — verified by reading actual library source code; no WAV support in either id3v2 or dhowden/tag, confirmed by inspecting parse/detect logic
- Architecture: HIGH — RIFF format is well-understood, simple structure; implementation pattern directly mirrors existing MP3/FLAC writers
- Pitfalls: HIGH — padding byte rule verified in RIFF spec; 4GB limit is inherent to 32-bit size field; dhowden/tag WAV gap verified by source inspection

**Research date:** 2026-03-18
**Valid until:** 2026-04-18 (stable domain — RIFF spec unchanged since 1991, library versions pinned in go.mod)
