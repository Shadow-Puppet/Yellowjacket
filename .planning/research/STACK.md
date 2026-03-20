# Technology Stack: OGG Vorbis + WAV Tag Writing

**Project:** YellowJacket v1.2.1 Format Parity
**Researched:** 2026-03-18
**Scope:** Stack additions/changes needed ONLY for OGG Vorbis and WAV tag writing

> **Context:** MP3 writing (bogem/id3v2) and FLAC writing (go-flac ecosystem) are already
> implemented and validated in v1.2. This document focuses exclusively on what's needed
> for OGG Vorbis and WAV tag writing.

---

## OGG Vorbis Tag Writing

### The Problem

OGG Vorbis stores metadata as Vorbis Comments in the second header packet of the OGG
bitstream. Modifying these comments requires:

1. Parsing OGG pages to extract the three Vorbis header packets (identification, comment, setup)
2. Decoding and modifying the Vorbis Comment packet
3. Re-encoding the modified comment packet into new OGG pages (with correct segment tables, sequence numbers, and CRC32 checksums)
4. Copying all audio data pages unchanged
5. Writing the result atomically

No pure-Go library exists that provides this end-to-end. The recommendation is a **custom OGG page rewriter** using existing building blocks.

### Option Analysis

#### Option 1: Custom OGG Page Rewriter (RECOMMENDED)

| Component | Source | Purpose |
|-----------|--------|---------|
| OGG page read/write | Custom (~300-400 LOC) | Parse OGG pages, rewrite with new comment packet |
| Vorbis Comment encode/decode | Reuse `go-flac/flacvorbis` patterns | Same binary format as FLAC Vorbis Comments |
| CRC32 computation | Custom (~30 LOC, lookup table) | OGG uses CRC32 with polynomial 0x04c11db7 |
| METADATA_BLOCK_PICTURE | Reuse `go-flac/flacpicture` | Same binary format, base64-wrapped for OGG |

**Why this is the right approach:**

- The OGG container format is simple and well-documented (27-byte header + segment table + page data)
- `jfreymuth/oggvorbis` already has a working OGG page reader in `ogg.go` (~255 LOC) that can serve as reference
- The Vorbis Comment binary format is identical to what's used in FLAC — the existing `flacvorbis` library's comment manipulation code can be reused or adapted
- Audio data pages are copied byte-for-byte — no audio re-encoding
- Full control over the implementation with no external dependency risk

**Implementation complexity:** MEDIUM. The OGG page format has exactly one tricky aspect:
packets can span multiple pages (via the continuation-of-packet flag and lacing values).
The comment packet is typically small enough to fit in one page, but the implementation
must handle the general case (especially when large cover art is embedded, inflating the
comment packet well beyond one page's ~65KB limit).

**Estimated LOC:** ~500-600 for a complete `ogg.go` writer in the tagwriter package.

#### Option 2: `mccoyst/ogg` for OGG Page Encoding

| Technology | Version | Stars | Purpose |
|------------|---------|-------|---------|
| `github.com/mccoyst/ogg` | latest (no semver tags) | 37 | OGG page encode/decode |

**What it provides:**
- `ogg.Decoder` — reads OGG pages, extracts packets
- `ogg.Encoder` — writes packets into OGG pages with correct framing, CRC, segment tables
- `EncodeBOS()`, `Encode()`, `EncodeEOS()` — page-type-aware encoding
- Handles packet splitting across pages automatically
- 100% test coverage claimed, 52 commits, MIT license

**Assessment:** This is the strongest external option. The Encoder handles the hardest parts
(lacing values, page splitting, CRC). However:

- **No semver releases** — importing is `github.com/mccoyst/ogg` with no version guarantee
- **37 stars, 1 open issue** — small community, low bus factor
- **Still requires Vorbis-specific logic** — mccoyst/ogg handles OGG pages but knows nothing about Vorbis header packets. We'd still need to:
  - Identify and extract the three Vorbis header packets
  - Parse/modify the Vorbis Comment packet
  - Handle the `\x03vorbis` packet type prefix
  - Re-encode everything in the correct order

**Verdict:** POSSIBLE but the marginal benefit over custom code is small. The OGG page format
is well-specified; the Encoder's value is mainly in lacing value calculation and CRC, which
are ~80 LOC total. Adding a dependency for ~80 LOC of saved work introduces a maintenance
and stability risk for a library with no tagged releases.

**Recommendation: Custom implementation.** If OGG page writing proves harder than expected
during implementation, `mccoyst/ogg` can be pulled in as a fallback.

#### Option 3: `jfreymuth/oggvorbis` (Already a Dependency)

| Technology | Version | Stars | Purpose |
|------------|---------|-------|---------|
| `github.com/jfreymuth/oggvorbis` | v1.0.5 | 75 | OGG Vorbis decoder |

**Assessment:** Decode-only. No write API. The `ogg.go` internal reader is useful as
**reference code** for understanding OGG page parsing, but the types are unexported
and the package provides no page-level write capability.

**Verdict:** NOT SUITABLE for writing. Useful as reference only.

#### Option 4: `dhowden/tag` Write Support

**Assessment:** dhowden/tag is read-only (642 stars, no write API). Its OGG parsing is
minimal — it extracts Vorbis Comments for reading but does not provide any mechanism
to modify or write back. Forking would require building the entire OGG page rewriter
anyway, plus inheriting maintenance burden.

**Verdict:** NOT SUITABLE.

#### Option 5: External CLI Tools (vorbiscomment, ffmpeg)

| Tool | What It Does | Issue |
|------|-------------|-------|
| `vorbiscomment` | CLI for reading/writing Vorbis Comments in OGG | Requires packaging/distributing a C binary |
| `ffmpeg` | Swiss-army multimedia tool | Massive binary (~100MB), CGo-equivalent burden |
| `opustags` | Opus metadata editor | Wrong codec (Opus, not Vorbis) |

**Assessment:** Shelling out to external CLI tools would work but violates the pure-Go
spirit of the project. It introduces:
- Distribution complexity (packaging native binaries per platform)
- Runtime dependency management (checking tool availability, version compat)
- Error handling complexity (parsing CLI output)
- No Windows/macOS availability guarantee without extra bundling

**Verdict:** NOT RECOMMENDED. Only consider as absolute last resort if custom Go
implementation proves infeasible (it won't — the format is well-understood).

### OGG Vorbis Writing: Recommended Stack

| Component | Approach | New Dependency? |
|-----------|----------|-----------------|
| OGG page parsing | Custom `oggRewriter` in tagwriter package | No |
| OGG page writing | Custom (header + segment table + CRC32) | No |
| Vorbis Comment manipulation | Reuse `flacvorbis` patterns + shared helpers | No (already a dep) |
| Cover art (METADATA_BLOCK_PICTURE) | `go-flac/flacpicture` for binary encoding + base64 wrapper | No (already a dep) |
| Atomic file write | Existing `fileutil.AtomicWrite` | No |

**Total new dependencies: ZERO.** Everything needed is either already in the dep tree
or implementable from well-documented specifications.

### OGG Vorbis Cover Art

**YES — OGG Vorbis can embed cover art.**

Cover art in OGG Vorbis uses the `METADATA_BLOCK_PICTURE` Vorbis Comment field:

1. Create a FLAC Picture binary block (same as used in FLAC files):
   - Picture type (3 = front cover)
   - MIME type string ("image/jpeg" or "image/png")
   - Description string ("Front cover")
   - Width, height, color depth, palette size (can be set to 0)
   - Raw image data
2. Base64-encode the entire binary block
3. Add as Vorbis Comment: `METADATA_BLOCK_PICTURE=<base64 data>`

**Integration with existing code:** The `go-flac/flacpicture` library already creates
the binary FLAC Picture block. For FLAC files, this block goes into a metadata block.
For OGG files, the same binary block gets base64-encoded and inserted as a Vorbis
Comment string. We can reuse `flacpicture.NewFromImageData()` and add a base64 wrapper.

**Confidence:** HIGH — This is the official Xiph.org recommendation per
https://wiki.xiph.org/VorbisComment#METADATA_BLOCK_PICTURE

### OGG Vorbis Format Details (Implementation Reference)

**OGG Page Structure (27 bytes header):**
```
Bytes 0-3:   "OggS" capture pattern
Byte 4:      Stream structure version (always 0)
Byte 5:      Header type flag (0x01=continued, 0x02=BOS, 0x04=EOS)
Bytes 6-13:  Absolute granule position (int64, little-endian)
Bytes 14-17: Stream serial number (uint32, little-endian)
Bytes 18-21: Page sequence number (uint32, little-endian)
Bytes 22-25: CRC32 checksum (computed with this field zeroed)
Byte 26:     Number of segments (0-255)
Bytes 27+:   Segment table (one byte per segment, lacing values)
             Page data follows immediately
```

**Vorbis Header Packets in OGG:**
- Packet 1: Identification header (starts with `\x01vorbis`)
- Packet 2: Comment header (starts with `\x03vorbis`) ← THIS IS WHAT WE MODIFY
- Packet 3: Setup header (starts with `\x05vorbis`)
- Packets 4+: Audio data

**Vorbis Comment Binary Format (within packet 2, after `\x03vorbis` prefix):**
```
[vendor_length: uint32 LE] [vendor_string: bytes]
[comment_count: uint32 LE]
for each comment:
  [length: uint32 LE] [comment: bytes]  // e.g. "ARTIST=Bob Dylan"
[framing_bit: 1 bit, must be 1]
```

**Algorithm for tag writing:**
1. Read all OGG pages from source file
2. Extract packets 1, 2, 3 from the first few pages (header pages)
3. Parse Vorbis Comment from packet 2 (skip `\x03vorbis` prefix)
4. Modify comments (add/replace/remove fields)
5. Re-serialize Vorbis Comment packet (with `\x03vorbis` prefix)
6. Write to temp file via AtomicWrite:
   a. Write packet 1 (identification) as BOS page
   b. Write modified packet 2 (comment) + packet 3 (setup) as continuation pages
   c. Copy all remaining audio pages, re-sequencing page numbers
7. Atomic rename over original

---

## WAV Tag Writing

### The Problem

WAV files use the RIFF container format. Metadata in WAV files can be stored in:

1. **RIFF INFO chunks** (`LIST`/`INFO` sub-chunks like `IART`, `INAM`, `IPRD`) — the oldest and most widely supported mechanism
2. **ID3v2 chunks** (`id3 ` RIFF chunk containing a full ID3v2 tag) — newer, more expressive, used by some modern tools
3. **BEXT chunks** (Broadcast Wave Extension) — professional/broadcast use only

The challenge is that no dominant pure-Go library exists for WAV metadata writing, and the ecosystem recently lost its most popular option.

### Option Analysis

#### Option 1: Custom RIFF Chunk Writer for LIST/INFO (RECOMMENDED)

| Component | Source | Purpose |
|-----------|--------|---------|
| RIFF chunk reader | Custom (~200 LOC) | Parse WAV RIFF structure, find/modify LIST INFO chunk |
| INFO field writing | Custom (~150 LOC) | Write INFO sub-chunks with metadata |
| Atomic file write | Existing `fileutil.AtomicWrite` | Crash-safe file operations |

**RIFF INFO Chunk Field Mapping:**

| YellowJacket Field | INFO Chunk ID | Description |
|--------------------|---------------|-------------|
| Title | `INAM` | Name/title of the work |
| Artist | `IART` | Artist name |
| Album | `IPRD` | Product/album name |
| Genre | `IGNR` | Genre |
| Year | `ICRD` | Creation date |
| Track Number | `ITRK` | Track number |
| Composer | `IMUS` | Composer/music by |
| Comment | `ICMT` | Comment |

**Why RIFF INFO over ID3v2-in-WAV:**
- RIFF INFO is the native WAV metadata format — it's part of the RIFF specification
- Universal player support (Windows Media Player, VLC, foobar2000, etc.)
- Simple format: 4-byte chunk ID + 4-byte size + null-terminated string
- dhowden/tag already reads RIFF INFO chunks, so round-trip works
- No additional dependencies needed

**Why NOT ID3v2-in-WAV:**
- The `id3 ` chunk is a de facto standard, not an official RIFF spec feature
- Not all players/tools support it
- dhowden/tag reads ID3v2 in WAV (it detects it), but our existing `bogem/id3v2` expects MP3 file structure (it calls `tag.Open()` which reads from position 0, expecting an ID3v2 header). Using bogem/id3v2 for WAV would require significant adaptation to handle the RIFF wrapper.
- More complex: we'd need to create an ID3v2 tag, serialize it, then embed it as a RIFF chunk

**Implementation approach:**
1. Read entire WAV file (parse RIFF chunks: `RIFF`, `fmt `, `data`, `LIST`, etc.)
2. Find or create `LIST`/`INFO` chunk
3. Write/replace INFO sub-chunks with new metadata values
4. Reassemble file: RIFF header → fmt chunk → data chunk → LIST/INFO chunk → other chunks
5. Update RIFF header size
6. Write via AtomicWrite

**Estimated LOC:** ~300-400 for RIFF INFO reading/writing.

#### Option 2: `go-audio/wav` + `go-audio/riff`

| Technology | Status | Stars |
|------------|--------|-------|
| `github.com/go-audio/wav` | **ARCHIVED Feb 21, 2026** | 383 |
| `github.com/go-audio/riff` | **ARCHIVED Feb 21, 2026** | 11 |

**Assessment:** Both libraries were archived less than one month ago. They provided WAV
encoding/decoding and RIFF chunk parsing, but:
- `go-audio/wav` is focused on audio encoding/decoding, not metadata manipulation
- `go-audio/riff` has only a parser (3 commits total), no writer
- Neither supports writing RIFF INFO metadata
- Both are now unmaintained/archived — using them would be a dead-end dependency

**Verdict:** NOT SUITABLE. Archived, no metadata write support.

#### Option 3: `bogem/id3v2` for ID3v2-in-WAV

**Assessment:** bogem/id3v2's `Tag.WriteTo(w io.Writer)` writes raw ID3v2 tag bytes to
any writer. In theory, we could:
1. Create an ID3v2 tag with bogem/id3v2
2. Serialize it to bytes via `WriteTo()`
3. Wrap it in a RIFF `id3 ` chunk
4. Insert the chunk into the WAV file

This is technically feasible but:
- Still requires custom RIFF chunk manipulation code
- ID3v2-in-WAV has worse player compatibility than RIFF INFO
- More complex than just writing RIFF INFO chunks directly
- `tag.Open("file.wav")` and `tag.Save()` won't work — those expect MP3 file structure

**Verdict:** NOT RECOMMENDED as primary approach. Could be added later as an enhancement
for richer metadata (cover art via APIC), but RIFF INFO should be the primary mechanism.

#### Option 4: External CLI Tools (ffmpeg, exiftool)

Same issues as OGG: distribution complexity, runtime dependencies, error handling overhead.

**Verdict:** NOT RECOMMENDED.

### WAV Writing: Recommended Stack

| Component | Approach | New Dependency? |
|-----------|----------|-----------------|
| RIFF container parsing | Custom RIFF reader in tagwriter package | No |
| RIFF INFO chunk writing | Custom (4-byte IDs + null-terminated strings) | No |
| Atomic file write | Existing `fileutil.AtomicWrite` | No |

**Total new dependencies: ZERO.**

### WAV Cover Art

**RIFF INFO: NO — cannot embed cover art.**

RIFF INFO chunks are limited to simple text key-value pairs. There is no standard
INFO sub-chunk for binary image data.

**ID3v2-in-WAV: YES — via APIC frame.**

If an `id3 ` RIFF chunk is present (or added), it can contain a full ID3v2 tag with
APIC (Attached Picture) frames, just like MP3 files.

**Recommendation for v1.2.1:** Do NOT implement WAV cover art. The RIFF INFO approach
gives us text tag support with zero dependencies, and WAV cover art support is rare
in the wild. Cover art for WAV can be deferred to a future enhancement using the
ID3v2-in-WAV approach if there's user demand.

### WAV RIFF Format Details (Implementation Reference)

**RIFF File Structure:**
```
"RIFF" [file_size: uint32 LE] "WAVE"
  "fmt " [chunk_size: uint32 LE] [format data...]
  "data" [chunk_size: uint32 LE] [audio samples...]
  "LIST" [chunk_size: uint32 LE] "INFO"
    "INAM" [size: uint32 LE] "Track Title\0"
    "IART" [size: uint32 LE] "Artist Name\0"
    ...
```

**Key implementation details:**
- All integers are little-endian (unlike OGG which is a mix)
- Chunk sizes must be even (pad with 0x00 byte if odd)
- Strings in INFO chunks are null-terminated
- The RIFF header size field = total file size - 8
- LIST/INFO chunk can appear anywhere after `fmt ` and `data`
- Multiple LIST chunks may exist; only `LIST`/`INFO` contains metadata

**Algorithm for tag writing:**
1. Parse RIFF chunks by reading 4-byte ID + 4-byte size pairs
2. Collect all chunks, preserving order
3. Find or create LIST/INFO chunk
4. Replace/add INFO sub-chunks for changed fields
5. Reassemble file via AtomicWrite:
   a. Write RIFF header with updated total size
   b. Write fmt chunk (unchanged)
   c. Write data chunk (unchanged — just copy bytes)
   d. Write LIST/INFO chunk with metadata
   e. Write any other chunks (unchanged)
6. Atomic rename

---

## Integration with Existing Pipeline

### Format Detection Changes

Current `tagwriter.DetectFormat()` supports MP3 and FLAC. Add OGG and WAV:

```go
const (
    FormatMP3  AudioFormat = "mp3"
    FormatFLAC AudioFormat = "flac"
    FormatOGG  AudioFormat = "ogg"   // NEW
    FormatWAV  AudioFormat = "wav"   // NEW
)

func DetectFormat(filePath string) (AudioFormat, error) {
    switch strings.ToLower(filepath.Ext(filePath)) {
    case ".mp3":  return FormatMP3, nil
    case ".flac": return FormatFLAC, nil
    case ".ogg":  return FormatOGG, nil     // NEW
    case ".wav":  return FormatWAV, nil     // NEW
    default:      return "", errUnsupportedFormat
    }
}
```

### Pipeline Dispatch Changes

Current `WriteTrackTags()` switch in `pipeline.go`:

```go
switch format {
case FormatMP3:  err = writeMp3Tags(tw.logger, audioFile.FilePath, changes)
case FormatFLAC: err = writeFlacTags(tw.logger, audioFile.FilePath, changes)
case FormatOGG:  err = writeOggTags(tw.logger, audioFile.FilePath, changes)  // NEW
case FormatWAV:  err = writeWavTags(tw.logger, audioFile.FilePath, changes)  // NEW
}
```

### Shared Code Reuse

**Vorbis Comment helpers** (already exist in `flac.go`):
- `replaceVorbisComment()` — remove existing field, add new value
- `applyFlacTextChanges()` — map TagChanges to Vorbis Comment fields
- Field mapping constants (`FIELD_TITLE`, `FIELD_ARTIST`, etc.)

These should be **extracted to a shared file** (e.g., `vorbis_comments.go`) and reused
by both `flac.go` and the new `ogg.go`. The logic is identical — both formats use Vorbis
Comments with the same field names.

**Cover art helpers** (already exist in `tagwriter.go`):
- `detectMIME()` — determine image MIME type from magic bytes
- `asBytes()` — extract byte slice from TagChanges value

**AtomicWrite** (already exists in `fileutil/atomicwrite.go`):
- Used identically for all formats: write to temp → sync → rename

---

## Dependency Summary

### New Dependencies Required

**NONE.** Both OGG Vorbis and WAV tag writing are implemented as custom code in the
tagwriter package, using only:
- Go standard library (`encoding/binary`, `encoding/base64`, `bytes`, `io`, `os`)
- Existing dependencies (`go-flac/flacpicture` for METADATA_BLOCK_PICTURE binary format)
- Existing utilities (`fileutil.AtomicWrite`)

### Existing Dependencies Unchanged

| Library | Current Use | Use in v1.2.1 |
|---------|------------|---------------|
| `dhowden/tag` v0.0.0-20240417 | Tag reading (all formats) | Unchanged — validates OGG/WAV round-trip |
| `go-flac/flacvorbis/v2` v2.0.2 | FLAC Vorbis Comment manipulation | Shared patterns for OGG comment encoding |
| `go-flac/flacpicture/v2` v2.0.2 | FLAC PICTURE block creation | Reused for OGG METADATA_BLOCK_PICTURE |
| `go-flac/go-flac/v2` v2.0.4 | FLAC metadata block manipulation | Unchanged |
| `bogem/id3v2/v2` v2.1.4 | MP3 ID3v2 tag writing | Unchanged |
| `jfreymuth/oggvorbis` v1.0.5 | OGG Vorbis decoding (via beep) | Reference for OGG page structure |

---

## What NOT To Add

| Library / Approach | Why Avoid |
|--------------------|-----------|
| `mccoyst/ogg` | No semver releases, 37 stars. The OGG page format is simple enough to implement directly (~80 LOC for the encoding part). Avoids dependency risk for minimal gain. |
| `go-audio/wav` or `go-audio/riff` | **Archived Feb 21, 2026.** Do not depend on abandoned libraries. |
| Any CGo-based library (taglib-go, etc.) | Violates pure-Go constraint |
| `bogem/id3v2` for WAV files | Its `Open()`/`Save()` API expects MP3 file structure. Would need significant wrapping for RIFF container. RIFF INFO is simpler and more compatible. |
| ID3v2-in-WAV for v1.2.1 | Adds complexity for marginal benefit. RIFF INFO covers the primary use case. Defer ID3v2-in-WAV to a future milestone if cover art in WAV is needed. |
| External CLI tools (vorbiscomment, ffmpeg) | Distribution complexity, runtime deps, violates pure-Go spirit |

---

## Format Coverage Matrix (v1.2.1 Target)

| Format | Text Tags | Cover Art | Approach | Complexity | Confidence |
|--------|-----------|-----------|----------|------------|------------|
| MP3 (ID3v2) | ✓ All fields | ✓ APIC frame | bogem/id3v2 (existing) | Done | HIGH |
| FLAC | ✓ All fields | ✓ PICTURE block | go-flac ecosystem (existing) | Done | HIGH |
| OGG Vorbis | ✓ All fields | ✓ METADATA_BLOCK_PICTURE | Custom OGG page rewriter | Medium | MEDIUM-HIGH |
| WAV | ✓ Core fields | ✗ Not in v1.2.1 | Custom RIFF INFO writer | Low-Medium | MEDIUM-HIGH |

**"Core fields" for WAV:** Title, Artist, Album, Genre, Year, Track Number, Composer.
Album Artist and Disc Number have no standard RIFF INFO chunk IDs. They can be omitted
or stored in non-standard INFO chunks if needed.

---

## Risk Assessment

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| OGG page splitting with large cover art | Medium | Medium | Test with cover art >64KB (forces multi-page comment packet) |
| WAV files with unusual RIFF chunk ordering | Low | Low | Parse all chunks generically, preserve unknown chunks |
| dhowden/tag can't read what we write (OGG) | Low | High | Round-trip tests: write with custom writer, read with dhowden/tag |
| dhowden/tag can't read what we write (WAV) | Low | High | Round-trip tests for RIFF INFO chunks |
| OGG CRC32 computation mismatch | Low | High | Use the exact polynomial from OGG spec (0x04c11db7), test against known files |
| WAV INFO chunk padding errors | Medium | Low | RIFF spec requires even-aligned chunks; easy to forget padding byte |

---

## Sources

### Official Specifications (HIGH confidence)
- OGG framing: https://xiph.org/ogg/doc/framing.html
- OGG RFC: https://xiph.org/ogg/doc/rfc3533.txt
- Vorbis I Spec (Section 5 — comment field): https://xiph.org/vorbis/doc/Vorbis_I_spec.html
- Vorbis Comment spec: https://xiph.org/vorbis/doc/v-comment.html
- METADATA_BLOCK_PICTURE: https://wiki.xiph.org/VorbisComment#METADATA_BLOCK_PICTURE
- FLAC Picture block format: https://xiph.org/flac/format.html#metadata_block_picture

### Libraries Evaluated (HIGH confidence — GitHub repos)
- jfreymuth/oggvorbis: https://github.com/jfreymuth/oggvorbis (75 stars, decode-only)
- mccoyst/ogg: https://github.com/mccoyst/ogg (37 stars, encode+decode, no semver)
- dhowden/tag: https://github.com/dhowden/tag (642 stars, read-only)
- go-audio/wav: https://github.com/go-audio/wav (383 stars, **ARCHIVED 2026-02-21**)
- go-audio/riff: https://github.com/go-audio/riff (11 stars, **ARCHIVED 2026-02-21**)
- bogem/id3v2: https://github.com/n10v/id3v2 (359 stars, MP3-focused API)

### Existing Codebase (HIGH confidence — already validated in v1.2)
- `backend/tagwriter/flac.go` — Vorbis Comment manipulation patterns
- `backend/tagwriter/mp3.go` — AtomicWrite integration pattern
- `backend/tagwriter/tagwriter.go` — TagChanges, format detection, helper functions
- `backend/fileutil/atomicwrite.go` — Crash-safe file write utility
