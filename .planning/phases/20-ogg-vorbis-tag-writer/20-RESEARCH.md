# Phase 20: OGG Vorbis Tag Writer - Research

**Researched:** 2026-03-19
**Domain:** OGG container format, Vorbis Comment metadata, custom page-level binary I/O
**Confidence:** HIGH

## Summary

OGG Vorbis tag writing requires a custom OGG page parser/writer since no suitable Go library exists for page-level manipulation. The implementation reads all pages, modifies only the comment header packet (packet #2), then rewrites the entire file with correct CRC checksums and renumbered page sequence numbers.

The core technical challenges are: (1) OGG uses a non-standard CRC32 algorithm (MSB-first / "unreflected" with polynomial 0x04c11db7) that is incompatible with Go's `hash/crc32` package, requiring a custom implementation; (2) the Vorbis Comment packet inside OGG differs from FLAC by including a 7-byte header prefix (`\x03vorbis`) and a trailing framing bit; (3) cover art is embedded as base64-encoded FLAC PICTURE blocks inside Vorbis Comment fields (not as separate metadata blocks like FLAC).

The architecture follows the same full-rewrite pattern as the WAV writer: parse → modify → write atomically. The existing `replaceVorbisComment` pattern from `flac.go` and `AtomicWrite` from `fileutil` are directly reusable. Test read-back can use `dhowden/tag` via `metadata.ExtractTags` (unlike WAV, dhowden/tag supports OGG Vorbis).

**Primary recommendation:** Build a custom OGG page parser/writer in `ogg.go`, Vorbis Comment serializer in `ogg_vorbis.go`, and the pipeline entry point `writeOggTags` in `ogg.go` — all within `backend/tagwriter/`. Use a precomputed 256-entry CRC lookup table derived from the libogg reference implementation.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
- Normalize all Vorbis Comment field names to uppercase on write (consistent with FLAC writer pattern)
- Preserve duplicate field entries for non-edited fields as-is (multi-value fields are spec-legal); when editing a field, replace all entries with a single new value
- Preserve the original vendor string — we're a tag editor, not an encoder
- Preserve raw bytes for non-edited fields even if they contain invalid UTF-8 — don't break existing tags because another tool was sloppy
- When writing or removing cover art, also strip legacy COVERART and COVERARTMIME Vorbis Comment fields to prevent stale art from lingering
- "Clear cover art" removes ALL picture-related fields: METADATA_BLOCK_PICTURE, COVERART, and COVERARTMIME
- When setting cover art, replace all existing METADATA_BLOCK_PICTURE entries with a single front cover (same approach as FLAC writer)
- Write path only — dhowden/tag already handles reading OGG cover art for display
- Lenient read, strict write — accept pages with bad CRC on read (some tools produce wrong CRCs), always write correct CRCs (matches WAV parser lenient-read/strict-write philosophy)
- Reject truncated files — if we can't read the complete file structure, refuse the edit (can't guarantee audio preservation on a broken file)
- Reject non-Vorbis OGG — only support OGG Vorbis (check for `\x01vorbis` magic in identification header); OGG Opus, Theora, FLAC, etc. get a clear error
- User-friendly error messages with specific details ("Could not save tags to file.ogg: disk full"), technical info logged via slog
- Detect multi-stream early during parse (fail fast), before any write work begins
- Count unique serial numbers across pages — more than one means multi-stream
- Reject chained streams too (multiple sequential Vorbis streams in one file) — unusual for music libraries, each has its own comment header
- Clear rejection message: "This OGG file contains multiple streams and cannot be edited"

### Claude's Discretion
- OGG page size decisions (how to split large Vorbis Comment across pages)
- CRC32 implementation details (MSB-first bit ordering per the OGG spec warning)
- Page sequence number renumbering strategy when comment header page count changes
- Exact structure of the custom OGG page parser/writer
- Test file generation approach for round-trip tests

### Deferred Ideas (OUT OF SCOPE)
- Migrate legacy COVERART field to METADATA_BLOCK_PICTURE (TAG-01 — tracked in REQUIREMENTS.md future requirements)
- OGG Opus tag writing (FMT-01 — different header structure, `OpusTags` vs `\x03vorbis`, no framing bit)
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|-----------------|
| OGG-01 | User can edit all 8 text metadata fields (title, artist, album, album_artist, genre, year, track#, disc#, composer) on OGG Vorbis files | Vorbis Comment format documented: `FIELD=value` pairs in comment header packet; field name mappings same as FLAC (TITLE, ARTIST, ALBUM, ALBUMARTIST, GENRE, DATE, TRACKNUMBER, DISCNUMBER, COMPOSER). Reuse `replaceVorbisComment` filter+add pattern. |
| OGG-02 | OGG tag writes preserve existing non-edited Vorbis Comment fields (ReplayGain, lyrics, etc.) | Comment serializer parses all existing fields into raw byte entries; only fields matching edited field names are filtered. Non-edited entries (including multi-value) preserved byte-for-byte. |
| OGG-03 | OGG tag writes preserve audio data identically (lossless round-trip) | Full-rewrite approach: identification header (page 0), setup header, and all audio pages are copied byte-for-byte. Only comment header pages are reconstructed. Audio page data never touched. |
| OGG-04 | User can embed, replace, and remove cover art in OGG Vorbis files via METADATA_BLOCK_PICTURE | Cover art stored as base64-encoded FLAC PICTURE block in `METADATA_BLOCK_PICTURE` Vorbis Comment field. Picture block format: 4-byte type + 4-byte MIME length + MIME + 4-byte desc length + desc + 4×4 dimension bytes + 4-byte data length + data (all big-endian). Existing `detectMIME` helper reusable. Legacy COVERART/COVERARTMIME fields stripped on write/clear. |
| OGG-05 | OGG tag writing uses crash-safe atomic writes (write-to-temp-then-rename) | `fileutil.AtomicWrite` already provides this pattern; OGG writer calls it same as FLAC and WAV writers. |
| OGG-06 | OGG writer round-trip tests verify all fields via dhowden/tag read-back | Unlike WAV (which requires bogem/id3v2 for read-back), dhowden/tag's `ReadFrom` supports OGG Vorbis. Tests can use `metadata.ExtractTags` for verification — same pattern as FLAC tests. |
</phase_requirements>

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| No external OGG library | N/A | Custom OGG page parser/writer | No Go library provides page-level write access needed for tag editing. `jfreymuth/oggvorbis` (already in go.mod) is a decoder, not a page editor. Custom is required and manageable (~300 lines). |
| `encoding/binary` | stdlib | Little-endian integer parsing for OGG headers and Vorbis Comment fields | Standard Go approach for binary format parsing |
| `encoding/base64` | stdlib | Base64 encode/decode for METADATA_BLOCK_PICTURE in Vorbis Comments | Required by the VorbisComment cover art spec |
| `fileutil.AtomicWrite` | internal | Crash-safe write-to-temp-then-rename | Already used by FLAC and WAV writers in this project |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `dhowden/tag` | (existing) | Read-back verification in tests | Test read-back via `metadata.ExtractTags` — confirms written OGG tags are valid |
| `github.com/go-flac/flacpicture/v2` | (existing) | May reuse `PictureTypeFrontCover` constant | Only if we want the constant; otherwise define locally |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Custom OGG parser | `jfreymuth/oggvorbis` | Decoder only — no write support, no page-level access. Not suitable. |
| Custom OGG parser | `github.com/jfreymuth/vorbis` | Lower-level but still no page write API. Would need heavy wrapping. |
| Custom CRC32 | `hash/crc32` with custom table | Go's `hash/crc32` uses reflected (LSB-first) algorithm internally. OGG requires unreflected (MSB-first). Cannot use `hash/crc32` even with `MakeTable` — the bit ordering is wrong. Must implement custom. |

## Architecture Patterns

### Recommended Project Structure
```
backend/tagwriter/
├── tagwriter.go          # FormatOGG constant, DetectFormat ".ogg" case
├── pipeline.go           # case FormatOGG: err = writeOggTags(...)
├── ogg.go                # OGG page parser/writer, CRC32, writeOggTags entry point
├── ogg_vorbis.go         # Vorbis Comment packet parse/serialize, cover art encoding
├── ogg_test.go           # Round-trip tests for OGG tag writing
├── ogg_crc_test.go       # CRC32 unit tests with known vectors
├── flac.go               # (existing)
├── mp3.go                # (existing)
├── wav.go                # (existing)
└── helpers_test.go       # (existing) testLogger, tinyJPEG, assertEqual
```

### Pattern 1: OGG Page Structure
**What:** Every OGG page has a fixed 27-byte header + variable segment table + page data.
**When to use:** Parsing and writing all pages.

```
Offset  Size  Field
0       4     Capture pattern: "OggS"
4       1     Version: 0x00
5       1     Header type flags: 0x01=continued, 0x02=bos, 0x04=eos
6       8     Granule position (int64 LE)
14      4     Serial number (uint32 LE)
18      4     Page sequence number (uint32 LE)
22      4     CRC checksum (uint32 LE) — zero during computation
26      1     Number of page segments (N)
27      N     Segment table (N lacing values, each 0-255)
27+N    ...   Page data (sum of lacing values bytes)
```
**Source:** RFC 3533 Section 6, xiph.org/ogg/doc/framing.html (both verified)

### Pattern 2: OGG Vorbis Stream Layout
**What:** A Vorbis stream has exactly 3 header packets, then audio packets.
**When to use:** Understanding which pages to modify.

```
Page 0:  [bos flag] Identification header packet (starts with \x01vorbis)
         - Always exactly 1 packet on 1 page (58 bytes total)
Page 1+: Comment header packet (starts with \x03vorbis) — may span pages
         Setup header packet (starts with \x05vorbis) — may span pages
         Comment + setup share pages; setup ENDS a page boundary
Page N+: Audio packets (start with bit 0 = 0, i.e. even first byte)
```
**Source:** Vorbis I Specification, Appendix A.2 "Encapsulation" (verified)

### Pattern 3: Full Rewrite Strategy
**What:** Read all pages into memory, modify comment header, write all pages back.
**When to use:** Every OGG tag write.
**Example:**
```go
// Pseudocode for writeOggTags
func writeOggTags(logger *slog.Logger, filePath string, changes TagChanges) error {
    // 1. Read and parse all OGG pages
    pages, err := parseOggPages(filePath)  // lenient CRC on read
    
    // 2. Validate: single stream, Vorbis identification
    validateOggVorbis(pages)
    
    // 3. Extract comment header packet (reassemble from pages)
    // 4. Parse Vorbis Comment fields from packet
    // 5. Apply text changes (filter+add pattern)
    // 6. Apply cover art changes (METADATA_BLOCK_PICTURE base64)
    // 7. Serialize new comment packet (with \x03vorbis prefix + framing bit)
    // 8. Split new packet into OGG pages (max 255 segments per page)
    // 9. Rebuild page list: page0 + new comment pages + setup pages + audio pages
    // 10. Renumber all page sequence numbers (0, 1, 2, ...)
    // 11. Compute CRC for each page
    // 12. AtomicWrite the complete file
    
    return fileutil.AtomicWrite(logger, filePath, func(tmp *os.File) error {
        return writeOggPages(tmp, rebuiltPages)
    })
}
```

### Pattern 4: OGG CRC32 Algorithm (MSB-first / Unreflected)
**What:** OGG uses CRC32 with polynomial 0x04c11db7 in unreflected mode.
**When to use:** Computing page checksums on write. Also verifying on read (lenient = log warning, don't reject).
**Critical detail:** Go's `hash/crc32` package uses reflected (LSB-first) bit ordering. Even with a custom polynomial table, it produces wrong results for OGG. Must implement from scratch.

Reference implementation from libogg `framing.c`:
```c
// Table generation (MSB-first / unreflected):
for (i = 0; i <= 0xFF; i++){
    crc = i << 24;
    for (j = 0; j < 8; j++)
        crc = (crc << 1) ^ (crc & (1 << 31) ? polynomial : 0);
    crc_lookup[0][i] = crc;
}

// Per-byte update:
crc = (crc << 8) ^ crc_lookup[0][((crc >> 24) & 0xff) ^ *buffer++];
```

**Go implementation approach:**
```go
// Pre-computed 256-entry lookup table (generated at init or as var)
var oggCRCTable [256]uint32

func init() {
    const poly = 0x04c11db7
    for i := 0; i < 256; i++ {
        crc := uint32(i) << 24
        for j := 0; j < 8; j++ {
            if crc&(1<<31) != 0 {
                crc = (crc << 1) ^ poly
            } else {
                crc <<= 1
            }
        }
        oggCRCTable[i] = crc
    }
}

func oggCRC(data []byte) uint32 {
    var crc uint32
    for _, b := range data {
        crc = (crc << 8) ^ oggCRCTable[(crc>>24)^uint32(b)]
    }
    return crc
}
```
**Source:** xiph.org libogg `src/framing.c` `_ogg_crc_init()` and `_os_update_crc()` (verified against reference source)

### Pattern 5: Vorbis Comment Packet Format (Inside OGG)
**What:** The comment header packet in OGG has a 7-byte prefix and trailing framing bit that FLAC's Vorbis Comment blocks do not.
**When to use:** Parsing and serializing the comment packet.

```
Bytes     Content
[0]       0x03 (packet type = comment header)
[1..6]    "vorbis" (6 ASCII bytes)
[7..10]   vendor_length (uint32 LE)
[11..N]   vendor_string (vendor_length bytes)
[N+1..4]  user_comment_list_length (uint32 LE)
           For each comment:
             [4 bytes] comment_length (uint32 LE)
             [N bytes] comment_string (comment_length bytes, "FIELD=value")
[last]    framing_bit: 0x01 (single bit set, byte-aligned since all prior data is byte-aligned)
```
**Source:** Vorbis I Specification, Section 5.2 and 4.2.3 (verified)

**Key difference from FLAC:** FLAC Vorbis Comment metadata blocks do NOT have the `\x03vorbis` prefix or the framing bit. The data starts directly at vendor_length. The framing bit is Vorbis-specific for packet boundary detection.

### Pattern 6: METADATA_BLOCK_PICTURE in Vorbis Comments
**What:** Cover art is stored as a base64-encoded binary FLAC PICTURE block within a Vorbis Comment field.
**When to use:** Writing cover art to OGG files.

```
The Vorbis Comment entry is:
  METADATA_BLOCK_PICTURE=<base64-encoded-data>

The base64-decoded data is the binary FLAC picture block:
  4 bytes: picture type (big-endian uint32) — 3 = front cover
  4 bytes: MIME type string length (big-endian uint32)
  N bytes: MIME type string (e.g., "image/jpeg")
  4 bytes: description string length (big-endian uint32)
  N bytes: description string (e.g., "Front cover")
  4 bytes: width (big-endian uint32) — 0 if unknown
  4 bytes: height (big-endian uint32) — 0 if unknown
  4 bytes: color depth (big-endian uint32) — 0 if unknown
  4 bytes: indexed colors (big-endian uint32) — 0 if unknown
  4 bytes: picture data length (big-endian uint32)
  N bytes: picture data (the JPEG/PNG bytes)
```
**Source:** XiphWiki METADATA_BLOCK_PICTURE section (verified), RFC 9639 Section 8.8 for FLAC PICTURE format

**Note:** No padding characters may be omitted in the base64 encoding. Standard base64 with `=` padding is required (RFC 4648 Section 4). Line feeds are not allowed.

### Anti-Patterns to Avoid
- **Using `hash/crc32` for OGG checksums:** Wrong bit ordering produces silent data corruption. Every page would have an invalid CRC, and strict decoders would reject the file.
- **In-place page editing:** Tempting for small changes, but when comment size changes, all subsequent page offsets shift. Full rewrite is simpler and crash-safe.
- **Forgetting the framing bit:** Omitting the trailing framing bit produces a technically corrupt Vorbis stream. Most decoders tolerate it but it's spec-violating.
- **Modifying raw bytes of non-edited fields:** User locked: preserve raw bytes for non-edited fields even with invalid UTF-8. Never re-encode existing comment values.
- **Forgetting to strip legacy COVERART:** User locked: when writing or removing cover art, also strip legacy COVERART and COVERARTMIME fields.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Atomic file writing | Custom temp file + rename logic | `fileutil.AtomicWrite` | Already handles orphan cleanup, permission preservation, cross-device detection |
| Cover art MIME detection | Custom MIME sniffing | `detectMIME` from `tagwriter.go` | Already handles JPEG/PNG magic bytes |
| Field name mappings | New mapping table | Same constants as FLAC writer (`TITLE`, `ARTIST`, etc.) | Vorbis Comment field names are identical for FLAC and OGG |
| Read-back in tests | Custom OGG parser for tests | `metadata.ExtractTags` (via dhowden/tag) | dhowden/tag fully supports OGG Vorbis including METADATA_BLOCK_PICTURE |
| ID3v2 tag handling | N/A | N/A | OGG does not use ID3v2 — this is Vorbis Comments only |

**Key insight:** The OGG page format itself is simple (27-byte header + segments). The complexity is in correctly implementing CRC32 and handling packet spanning across pages. A custom parser/writer is ~300 lines and avoids pulling in an external dependency for a format we fully understand.

## Common Pitfalls

### Pitfall 1: OGG CRC32 Bit Ordering
**What goes wrong:** Using Go's `hash/crc32.MakeTable(0x04c11db7)` produces reflected (LSB-first) checksums. OGG requires unreflected (MSB-first). The polynomial is the same but the algorithm differs.
**Why it happens:** Both use polynomial 0x04c11db7 (same as Ethernet CRC32), but OGG uses "direct algorithm" with MSB-first processing while `hash/crc32` uses the reflected variant.
**How to avoid:** Implement a custom 256-entry lookup table using the MSB-first generation algorithm from libogg. Validate with known test vectors from libogg's self-test data.
**Warning signs:** Written OGG files play in some players (VLC is lenient) but fail in strict decoders or produce "corrupt stream" warnings.

### Pitfall 2: Forgetting the Framing Bit
**What goes wrong:** The serialized Vorbis Comment packet is missing its trailing framing bit. Decoders may reject the comment header or misalign subsequent data.
**Why it happens:** FLAC Vorbis Comment blocks don't have a framing bit, so developers copying the FLAC pattern omit it.
**How to avoid:** After writing all comment fields, append a single byte `0x01` (the framing bit set). The framing bit is the LSB of the next byte after the last comment field.
**Warning signs:** Tags appear corrupt or missing when read back by strict parsers.

### Pitfall 3: Page Sequence Number Gaps
**What goes wrong:** After rewriting comment pages (which may now span more or fewer pages than before), audio pages retain their original sequence numbers, creating gaps or duplicates.
**Why it happens:** The comment header might originally fit in 1 page but after adding cover art requires 3 pages — pushing all subsequent sequence numbers up by 2.
**How to avoid:** After rebuilding the page list, renumber ALL page sequence numbers sequentially from 0. This is safe because the serial number identifies the stream and sequence numbers are only used for loss detection.
**Warning signs:** Players report "page sequence gap" warnings or skip audio.

### Pitfall 4: Comment Header Spanning Multiple Pages
**What goes wrong:** Large Vorbis Comments (especially with embedded cover art) exceed the ~64KB maximum page size. The writer tries to put all data in one page and hits the 255-segment limit.
**Why it happens:** Each OGG page can hold at most 255 segments × 255 bytes = 65,025 bytes of data. A JPEG cover art image can easily exceed this.
**How to avoid:** When serializing the comment packet into pages, split at 255-segment boundaries. Set the continuation flag (0x01) on continuation pages. The last segment of a packet must have a lacing value < 255 (use 0 for exact multiples of 255).
**Warning signs:** Crash or buffer overflow during page construction.

### Pitfall 5: Multi-Stream / Chained Stream Detection
**What goes wrong:** A file containing multiplexed streams (video+audio) or chained Vorbis streams gets partially rewritten, corrupting non-Vorbis data.
**Why it happens:** Multi-stream OGG files have pages with different serial numbers interleaved. Chained files have multiple bos pages.
**How to avoid:** During the initial parse pass, collect all unique serial numbers. If count > 1, reject immediately. Also check for multiple bos pages (chained streams).
**Warning signs:** Video files or internet radio recordings become unplayable after tag edit.

### Pitfall 6: Setup Header Packet Boundary
**What goes wrong:** The comment header and setup header share pages. When rewriting only the comment, the setup header bytes must be preserved exactly.
**Why it happens:** Per the Vorbis spec, the comment and setup packets share pages — the setup header finishes on the last header page. When we rewrite comment pages, we need to correctly handle the boundary.
**How to avoid:** Extract the raw setup header packet bytes during parse. When rebuilding header pages, the setup packet follows the comment packet. The spec requires the setup packet to end on a page boundary (next audio packet starts a fresh page).
**Warning signs:** "unrecoverable" decode errors, setup header appears corrupted.

## Code Examples

### Creating a Minimal Test OGG File

For round-trip tests, create a minimal valid OGG Vorbis file programmatically. The approach:

```go
// A minimal OGG Vorbis file needs:
// 1. Identification header page (page 0, bos flag)
//    - Packet: \x01 + "vorbis" + version(4B) + channels(1B) + samplerate(4B) 
//      + bitrate hints(12B) + blocksizes(1B) + framing(1B) = 30 bytes
// 2. Comment + Setup header page(s) (pages 1+)
//    - Comment packet: \x03 + "vorbis" + vendor + comments + framing bit
//    - Setup packet: \x05 + "vorbis" + minimal codebook data
// 3. At least one audio page (for a complete stream)
//    - Can use a minimal silent frame or synthetic data

// Alternative: Use a tiny pre-encoded OGG Vorbis file as test fixture
// (generated once with ffmpeg or oggenc, embedded as []byte literal)
```

The simplest approach for test fixtures is to encode a tiny silent OGG file with `ffmpeg -f lavfi -i anullsrc=r=44100:cl=mono -t 0.01 -c:a libvorbis silence.ogg` and embed the bytes, similar to how `tinyJPEG` works for cover art tests.

### Vorbis Comment Field Manipulation (Reuse Pattern)

```go
// Same filter+add pattern as flac.go replaceVorbisComment,
// but operating on raw []byte entries instead of string slices:

type vorbisComment struct {
    vendor   []byte     // raw vendor string bytes
    entries  [][]byte   // raw "FIELD=value" entries as bytes
}

func (vc *vorbisComment) replaceField(field string, value string) {
    prefix := []byte(strings.ToUpper(field) + "=")
    // Filter: keep entries that don't match this field (case-insensitive)
    filtered := make([][]byte, 0, len(vc.entries))
    for _, entry := range vc.entries {
        if !bytes.HasPrefix(bytes.ToUpper(entry), prefix) {
            filtered = append(filtered, entry)
        }
    }
    // Add: append new entry with uppercase field name
    filtered = append(filtered, []byte(strings.ToUpper(field)+"="+value))
    vc.entries = filtered
}
```

### Page Splitting for Large Packets

```go
func splitPacketIntoPages(packet []byte, serialNo uint32, startSeqNo uint32, 
    granulePos int64) []oggPage {
    // Each page can hold up to 255 segments
    // Each segment is up to 255 bytes
    // A packet is split into 255-byte segments + final shorter segment
    
    const maxSegments = 255
    const segmentSize = 255
    
    var pages []oggPage
    offset := 0
    seqNo := startSeqNo
    
    for offset < len(packet) || offset == 0 {
        var segments []byte
        var pageData []byte
        
        for len(segments) < maxSegments && offset < len(packet) {
            remaining := len(packet) - offset
            if remaining >= segmentSize {
                segments = append(segments, segmentSize)
                pageData = append(pageData, packet[offset:offset+segmentSize]...)
                offset += segmentSize
            } else {
                segments = append(segments, byte(remaining))
                pageData = append(pageData, packet[offset:offset+remaining]...)
                offset += remaining
            }
        }
        
        // If packet ends exactly on a 255 boundary, add a 0-length terminator
        if len(packet) > 0 && len(packet)%segmentSize == 0 && offset == len(packet) {
            if len(segments) < maxSegments {
                segments = append(segments, 0)
            }
            // else: terminator goes on next page (rare edge case)
        }
        
        pages = append(pages, buildPage(segments, pageData, serialNo, seqNo, granulePos))
        seqNo++
    }
    
    return pages
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Legacy COVERART base64 field | METADATA_BLOCK_PICTURE base64 field | ~2008-2012 | New code should write METADATA_BLOCK_PICTURE only; strip legacy on write/clear |
| vorbiscomment CLI tool | In-process editing via library APIs | Always | No external tools — pure Go constraint |
| Surgical in-place page editing | Full file rewrite | Project decision | Simpler, crash-safe, avoids offset tracking complexity |

**Deprecated/outdated:**
- `COVERART` / `COVERARTMIME` Vorbis Comment fields: Unofficial, deprecated in favor of `METADATA_BLOCK_PICTURE`. Still stripped on write/clear per user decision.

## Open Questions

1. **Test file generation approach**
   - What we know: Need a minimal valid OGG Vorbis file for round-trip tests.
   - What's unclear: Whether to build one programmatically (complex — need valid codebook data) or embed a pre-encoded tiny file as a byte literal.
   - Recommendation: Use a pre-encoded tiny silent OGG file generated with ffmpeg, embedded as `var tinyOGG = []byte{...}` in test code. This is simpler and more reliable than constructing valid Vorbis codebooks programmatically. The WAV tests use `createTestWAV` which builds files from scratch because WAV/RIFF is trivial to construct; OGG Vorbis audio frames are not.

2. **Maximum practical comment size**
   - What we know: OGG pages max out at ~64KB. Comments can span pages. Cover art images can be several MB.
   - What's unclear: Whether any practical limit should be imposed beyond what the format allows.
   - Recommendation: No artificial limit. Let the format handle it — a 5MB JPEG will produce ~80 continuation pages, which is fine. Log a warning for very large files (>500MB total, matching FLAC/WAV threshold).

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go testing (standard) |
| Config file | none |
| Quick run command | `go test ./backend/tagwriter/ -run TestOgg -count=1` |
| Full suite command | `go test ./backend/tagwriter/ -count=1` |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| OGG-01 | All 8 text fields writable | unit | `go test ./backend/tagwriter/ -run TestWriteOggTags_TextFields -count=1` | Wave 0 |
| OGG-02 | Non-edited fields preserved | unit | `go test ./backend/tagwriter/ -run TestWriteOggTags_PartialUpdate -count=1` | Wave 0 |
| OGG-03 | Audio data preserved | unit | `go test ./backend/tagwriter/ -run TestWriteOggTags_AudioPreservation -count=1` | Wave 0 |
| OGG-04 | Cover art embed/replace/remove | unit | `go test ./backend/tagwriter/ -run TestWriteOggTags_CoverArt -count=1` | Wave 0 |
| OGG-05 | Atomic writes | unit | `go test ./backend/tagwriter/ -run TestWriteOggTags_AtomicSafety -count=1` | Wave 0 |
| OGG-06 | Round-trip via dhowden/tag | unit | `go test ./backend/tagwriter/ -run TestWriteOggTags_ -count=1` | Wave 0 |

### Sampling Rate
- **Per task commit:** `go test ./backend/tagwriter/ -run TestOgg -count=1`
- **Per wave merge:** `go test ./backend/tagwriter/ -count=1`
- **Phase gate:** Full suite green before `/gsd-verify-work`

### Wave 0 Gaps
- [ ] `backend/tagwriter/ogg_test.go` — round-trip tests for all OGG requirements
- [ ] `backend/tagwriter/ogg_crc_test.go` — CRC32 correctness tests with known vectors from libogg self-test
- [ ] Tiny OGG test fixture (pre-encoded minimal silent file)

## Sources

### Primary (HIGH confidence)
- **Xiph.org OGG Framing Spec** — https://xiph.org/ogg/doc/framing.html — page header format, CRC polynomial, lacing values, continuation flags. Full spec read.
- **RFC 3533** — https://www.rfc-editor.org/rfc/rfc3533 — OGG page format (Section 6), encapsulation process (Section 5). Confirmed all field offsets and sizes.
- **Vorbis I Specification** — https://xiph.org/vorbis/doc/Vorbis_I_spec.html — Section 4.2 (header decode, packet types \x01/\x03/\x05 + "vorbis"), Section 5.2 (comment header structure with framing bit), Appendix A.2 (OGG encapsulation rules: bos page, header pages, audio pages).
- **Vorbis Comment Spec** — https://xiph.org/vorbis/doc/v-comment.html — Comment structure: vendor_length + vendor_string + count + entries + framing_bit. Field names case-insensitive, field=value format.
- **libogg source (framing.c)** — https://github.com/xiph/ogg/blob/master/src/framing.c — Reference CRC32 implementation (`_ogg_crc_init`, `_os_update_crc`, `ogg_page_checksum_set`). Confirmed MSB-first polynomial, init=0, final_xor=0. Also confirmed checksum field bytes 22-25 zeroed during computation and stored little-endian.
- **XiphWiki VorbisComment** — https://wiki.xiph.org/VorbisComment — METADATA_BLOCK_PICTURE encoding details, base64 rules (RFC 4648 Section 4), legacy COVERART deprecation.

### Secondary (MEDIUM confidence)
- **libogg self-test vectors** (in framing.c) — Known CRC values for specific test pages, usable for validating our Go implementation. Verified by reading source directly.

### Tertiary (LOW confidence)
- None needed — all critical details verified from primary sources.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — No external libraries needed; custom OGG parser verified against spec and reference implementation
- Architecture: HIGH — Pattern matches existing WAV/FLAC writers; full rewrite strategy is proven in this project
- Pitfalls: HIGH — CRC bit ordering, framing bit, and page sequence renumbering all verified from reference implementation source code
- Vorbis Comment format: HIGH — Verified against both formal spec and reference implementation

**Research date:** 2026-03-19
**Valid until:** Indefinite — OGG and Vorbis are stable, frozen specifications with no planned changes
