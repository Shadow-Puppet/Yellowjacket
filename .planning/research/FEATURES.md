# Feature Landscape: OGG Vorbis + WAV Tag Writing

**Domain:** Format-specific metadata tag writing for OGG Vorbis and WAV audio files
**Researched:** 2026-03-18
**Confidence:** HIGH (OGG Vorbis) / MEDIUM (WAV — fragmented standards require approach decision)

**Sources:**
- Xiph.Org VorbisComment specification (https://xiph.org/vorbis/doc/v-comment.html) — HIGH confidence
- Xiph.Org Wiki VorbisComment page (https://wiki.xiph.org/VorbisComment) — HIGH confidence
- FLAC METADATA_BLOCK_PICTURE specification (http://flac.sourceforge.net/format.html#metadata_block_picture) — HIGH confidence
- Wikipedia WAV article, Metadata section — MEDIUM confidence
- go-flac/flacvorbis library (https://github.com/go-flac/flacvorbis) — already in use, HIGH confidence
- dhowden/tag library (https://github.com/dhowden/tag) — already in use for reading, HIGH confidence
- bogem/id3v2 library (https://github.com/bogem/id3v2) — already in use for MP3 writing, HIGH confidence
- Existing YellowJacket codebase analysis — HIGH confidence

---

## OGG Vorbis Tag Writing

### Field Mapping: Vorbis Comments → YellowJacket's 8 Fields

OGG Vorbis uses **Vorbis Comments** — the exact same metadata system used by FLAC. Field names are case-insensitive, stored as `FIELDNAME=value` pairs in UTF-8.

| YellowJacket Field | Vorbis Comment Field | Notes |
|---|---|---|
| `title` | `TITLE` | Standard recommended field |
| `artist` | `ARTIST` | Standard recommended field |
| `album` | `ALBUM` | Standard recommended field |
| `album_artist` | `ALBUMARTIST` | De facto standard, not in original spec but universally supported |
| `genre` | `GENRE` | Standard recommended field |
| `year` | `DATE` | Standard recommended field; spec says ISO 8601, most apps store just the year |
| `track_number` | `TRACKNUMBER` | Standard recommended field |
| `disc_number` | `DISCNUMBER` | De facto standard, universally supported |
| `composer` | `COMPOSER` | De facto standard, widely supported |

**Key insight:** These are the *exact same* field names already used in YellowJacket's FLAC writer (`flac.go` → `applyFlacTextChanges`). The existing `flacvorbis` constants (`FIELD_TITLE`, `FIELD_ARTIST`, etc.) and the manual strings (`ALBUMARTIST`, `DISCNUMBER`, `COMPOSER`) map identically. The Vorbis Comment format is format-agnostic; FLAC and OGG Vorbis share the same comment structure. The difference is the container (FLAC metadata blocks vs. OGG page structure).

### OGG Vorbis Cover Art: METADATA_BLOCK_PICTURE

Cover art in OGG Vorbis uses the `METADATA_BLOCK_PICTURE` Vorbis Comment field. The process:

1. Construct a binary FLAC picture block (same structure as FLAC's native PICTURE metadata block):
   - Picture type (3 = Front Cover)
   - MIME type string (e.g., `image/jpeg`)
   - Description string (UTF-8)
   - Width, height, color depth, number of colors (can all be 0 per spec)
   - Image data
2. Base64-encode the entire binary block
3. Store as `METADATA_BLOCK_PICTURE=<base64 string>` in Vorbis Comments

**Player compatibility for METADATA_BLOCK_PICTURE in OGG Vorbis:**

| Player | Reads | Writes | Notes |
|---|---|---|---|
| foobar2000 | YES | YES | Full support |
| MusicBee | YES | YES | Full support |
| Mp3tag | YES | YES | Full support (since 2.47b) |
| VLC | YES | NO | Displays embedded art |
| Audacious | YES | N/A | No issues |
| MediaMonkey | YES | YES | Full support |
| Windows Media Player | YES | N/A | No issues |
| Picard (MusicBrainz) | YES | YES | Full support |

**The deprecated `COVERART` field** (raw base64 without the FLAC picture block structure) should NOT be written. It lacks type/MIME info and may break some hardware players. If encountered when reading, it could optionally be migrated to `METADATA_BLOCK_PICTURE`, but that's beyond scope for this milestone.

**Complexity:** LOW — The `go-flac/flacpicture` library already creates the binary FLAC picture block structure (used in `applyFlacCoverArt`). For OGG, the same binary block just needs base64 encoding before being stored as a Vorbis Comment string.

### OGG Vorbis Writing: The Container Problem

**This is where OGG differs from FLAC.** In FLAC, Vorbis Comments live in a separate metadata block that can be replaced independently of the audio data. In OGG Vorbis:

- The Vorbis Comment packet is the **second header packet** in the OGG bitstream
- Header packets are stored in the first few OGG pages
- Audio data follows in subsequent OGG pages
- OGG pages have CRC32 checksums and sequence numbers

**To modify Vorbis Comments in an OGG file, the approach is:**
1. Parse the OGG page structure
2. Extract the three Vorbis header packets (identification, comment, setup)
3. Modify the comment packet
4. Re-serialize the header packets into OGG pages (with recalculated CRCs and sizes)
5. Write new header pages + copy audio pages unchanged

**There is no pure-Go OGG writing library.** The existing Go ecosystem for OGG:
- `jfreymuth/oggvorbis` — **decoder only** (reads OGG Vorbis, no writing)
- `jfreymuth/vorbis` — **raw Vorbis decoder** (no OGG container awareness)
- `go-flac/flacvorbis` — **FLAC metadata blocks only** (not OGG pages)
- `dhowden/tag` — **read-only** for all formats

**The implementation must operate at the OGG container level:**
- Parse OGG pages (each page: magic "OggS", version, header type, granule pos, serial, page seq, CRC, segments)
- Extract Vorbis header packets from initial pages
- Build new comment packet from modified Vorbis Comments
- Re-paginate headers and write out with audio pages

**Complexity: MEDIUM-HIGH.** The OGG page format is well-documented (https://xiph.org/ogg/doc/framing.html) and not complex per se, but implementing page parsing + repagination + CRC32 from scratch is non-trivial. However, only the header pages need to be re-written; audio pages can be copied byte-for-byte. This is the same pattern as the MP3 writer (new tag + copy audio data).

**Risk mitigation:** The existing AtomicWrite pattern provides crash safety. Round-trip tests (write → read back via dhowden/tag) will validate correctness, following the FLAC precedent (7 round-trip tests).

---

## WAV Tag Writing

WAV files have **no single dominant metadata standard**. There are three approaches, each with different tradeoffs.

### Approach 1: ID3v2 Chunk in WAV (RECOMMENDED)

An ID3v2 tag is stored as a RIFF chunk with FourCC `id3 ` (or `ID3 `) inside the WAV RIFF structure.

**How it works:**
1. Parse the WAV RIFF structure to find existing chunks
2. Build/modify an ID3v2 tag (reusing the existing `bogem/id3v2` library)
3. Write the RIFF header + fmt chunk + data chunk + id3 chunk (+ any other existing chunks to preserve)

**Player compatibility:**

| Player | Reads ID3v2 in WAV | Writes ID3v2 in WAV | Notes |
|---|---|---|---|
| foobar2000 | YES | YES | Primary WAV tag format |
| MusicBee | YES | YES | Preferred format |
| Mp3tag | YES | YES | Default for WAV |
| VLC | YES | NO | Reads for display |
| Picard | YES | YES | Default for WAV |
| Windows Media Player | PARTIAL | NO | May read title/artist |
| Audacity | YES | YES | Via metadata editor |

**Pros:**
- **Reuses existing `bogem/id3v2` library** — all 8 fields + cover art are already implemented in `mp3.go`
- Full field support: all our 8 fields map perfectly (same as MP3)
- Cover art works identically to MP3 (APIC frame)
- The dominant standard among music library managers
- UTF-8/UTF-16 support for international characters
- Well-tested library with 359 GitHub stars

**Cons:**
- Not the "original" WAV metadata mechanism (RIFF INFO is the native one)
- Some older/simpler players may not read it
- Requires RIFF chunk-level parsing to place the ID3v2 data correctly

**Cover art:** YES — same APIC frame mechanism as MP3, fully supported.

### Approach 2: RIFF INFO Chunks

The original RIFF metadata mechanism. Uses `LIST` chunk with type `INFO` containing sub-chunks with FourCC identifiers.

**Field mapping:**

| YellowJacket Field | RIFF INFO FourCC | Field Name | Notes |
|---|---|---|---|
| `title` | `INAM` | Name/Title | Supported |
| `artist` | `IART` | Artist | Supported |
| `album` | `IPRD` | Product (Album) | Supported |
| `album_artist` | — | — | **NO STANDARD FIELD** |
| `genre` | `IGNR` | Genre | Supported |
| `year` | `ICRD` | Creation Date | Supported (full date string, not just year) |
| `track_number` | `ITRK` | Track Number | Nonstandard/rare — some use `IPRT` |
| `disc_number` | — | — | **NO STANDARD FIELD** |
| `composer` | `IMUS` | Music By (Composer) | Rare, not universally read |

**Problems:**
- **Cannot represent album_artist or disc_number** — no RIFF INFO fields exist
- Track number support is inconsistent across players
- **ASCII/codepage encoding** — RIFF INFO predates Unicode; the `CSET` chunk exists but is rarely used; most implementations assume Windows codepage
- **No cover art support** — RIFF INFO has no image field
- Limited string length (some implementations cap at 255 bytes per field)

**Verdict:** NOT RECOMMENDED as primary format. Cannot represent our full 8-field model + cover art.

### Approach 3: BWF (Broadcast Wave Format)

Extension of WAV with a `bext` chunk for broadcast metadata (originator, description, date, time reference, etc.).

**Relevance to music tagging:** NONE. BWF metadata is about broadcast provenance (originator, coding history, loudness), not music metadata (artist, album, genre). No music player uses BWF fields for library management.

**Verdict:** OUT OF SCOPE. Not relevant for music tagging.

### WAV Approach Decision: ID3v2 Chunk

**Use ID3v2 in WAV because:**
1. Maps all 8 fields + cover art identically to MP3
2. Reuses existing `bogem/id3v2` library code
3. Is the approach used by foobar2000, MusicBee, Mp3tag, Picard — the dominant music library managers
4. YellowJacket already reads WAV metadata via `dhowden/tag`, which reads ID3v2 chunks in WAV

**Writing implementation:**
1. Parse WAV RIFF structure: read chunk headers sequentially (each chunk: FourCC + uint32 size + data)
2. Build ID3v2 tag using `bogem/id3v2` (same code path as MP3 minus the "copy audio data" step)
3. Write atomically: RIFF header → all existing chunks (fmt, data, any others) → new `id3 ` chunk
4. Any existing `id3 ` chunk is replaced; any existing `LIST INFO` chunk is preserved (don't destroy metadata we don't control)

**Complexity:** MEDIUM. The RIFF chunk structure is simple (FourCC + 4-byte LE size), but requires:
- Parsing all chunks to find/replace the `id3 ` chunk
- Recalculating the top-level RIFF size header
- Preserving chunk ordering and any padding (RIFF chunks must be word-aligned, i.e., even byte offsets)

**Cover art in WAV:** YES — via ID3v2 APIC frame, identical to MP3.

---

## Table Stakes

Features users expect when a music player claims to edit metadata for a format.

| Feature | Why Expected | Complexity | Format | Notes |
|---|---|---|---|---|
| OGG: Write all 8 text fields | Parity with MP3/FLAC editing | Low | OGG | Same Vorbis Comment fields as FLAC |
| OGG: Preserve existing non-edited comments | Users may have ReplayGain, lyrics, etc. | Low | OGG | Filter-and-keep pattern, same as FLAC |
| OGG: Preserve audio data perfectly | Users expect lossless round-trip | Low | OGG | Audio pages copied byte-for-byte |
| WAV: Write all 8 text fields | Parity with MP3/FLAC editing | Low-Med | WAV | Via ID3v2 chunk, reuse MP3 code |
| WAV: Preserve audio data perfectly | Users expect lossless round-trip | Low | WAV | Copy data chunk unchanged |
| Crash-safe writes (both formats) | Existing AtomicWrite pattern | Low | Both | Already implemented |
| Batch editing works for OGG + WAV | Batch editor already handles all formats | Low | Both | Just need format dispatch in pipeline |
| Single-track editing works for OGG + WAV | Track editor already handles all formats | Low | Both | Just need format dispatch in pipeline |

## Differentiators

Features that set the product apart. Not expected but valued.

| Feature | Value Proposition | Complexity | Format | Notes |
|---|---|---|---|---|
| OGG: Cover art embed/remove | Full parity with MP3/FLAC cover art | Medium | OGG | METADATA_BLOCK_PICTURE via base64; reuse flacpicture binary block |
| WAV: Cover art embed/remove | Full parity with MP3/FLAC cover art | Low | WAV | ID3v2 APIC frame, identical to MP3 |
| WAV: Preserve existing RIFF INFO chunks | Don't destroy metadata we didn't write | Low | WAV | Just copy LIST INFO chunk through |
| OGG: Preserve non-Vorbis OGG streams | Multi-stream OGG files exist (rare) | Low | OGG | Only modify Vorbis stream headers |
| Round-trip test coverage | Validates writes don't corrupt files | Medium | Both | dhowden/tag reads what we write, following FLAC precedent |

## Anti-Features

Features to explicitly NOT build.

| Anti-Feature | Why Avoid | What to Do Instead |
|---|---|---|
| WAV: RIFF INFO as primary write target | Cannot represent album_artist, disc_number, or cover art | Use ID3v2 chunk; preserve existing RIFF INFO if present |
| WAV: BWF bext chunk writing | Broadcast metadata, not music metadata | Ignore; preserve if present |
| OGG: Write deprecated COVERART field | Deprecated, inconsistent support, may break hardware players | Write only METADATA_BLOCK_PICTURE |
| OGG: Re-encode audio data | Must never touch the audio bitstream | Copy audio pages byte-for-byte |
| WAV: Delete RIFF INFO when writing ID3v2 | Would destroy existing metadata user may rely on | Preserve RIFF INFO chunks as-is |
| OGG: Custom OGG page library | Over-engineering for the scope needed | Minimal OGG page parser/writer sufficient for header rewrite |
| WAV: Write both ID3v2 and RIFF INFO | Dual-write is complex and the RIFF INFO mapping is lossy | Write ID3v2 only; preserve existing RIFF INFO |

## Feature Dependencies

```
Existing WriteTrackTags pipeline
├── DetectFormat (extend: add .ogg and .wav)
├── Format-specific writer dispatch (extend: add OGG and WAV cases)
│   ├── writeOggVorbisTags (NEW)
│   │   ├── OGG page parser (NEW)
│   │   ├── Vorbis Comment serializer (reuse flacvorbis patterns)
│   │   ├── METADATA_BLOCK_PICTURE builder (reuse flacpicture + base64)
│   │   ├── OGG page writer with CRC32 (NEW)
│   │   └── AtomicWrite (existing)
│   └── writeWavTags (NEW)
│       ├── RIFF chunk parser (NEW)
│       ├── ID3v2 tag builder (reuse bogem/id3v2, same as MP3)
│       ├── RIFF chunk writer with size recalculation (NEW)
│       └── AtomicWrite (existing)
├── DB sync (existing, unchanged)
└── Event emission (existing, unchanged)
```

## MVP Recommendation

**Phase 1 — OGG Vorbis (text fields only):**
1. OGG page parser + writer (the core new infrastructure)
2. Vorbis Comment extraction and modification (reuse flacvorbis patterns)
3. Write modified headers + copy audio pages
4. Round-trip tests via dhowden/tag

**Phase 2 — WAV (text fields + cover art):**
1. RIFF chunk parser
2. ID3v2 tag writing via bogem/id3v2 (reuse MP3 code paths)
3. RIFF reassembly with id3 chunk
4. Round-trip tests

**Phase 3 — OGG Vorbis cover art:**
1. METADATA_BLOCK_PICTURE encoding (flacpicture binary block → base64)
2. Cover art in Vorbis Comments alongside text fields
3. Cover art round-trip tests

**Rationale for this ordering:**
- OGG text fields first because they're needed by more users (OGG is more common in music libraries than WAV)
- WAV includes cover art from the start because it's trivial (same as MP3 APIC frame)
- OGG cover art is separated because it requires additional work (base64 encoding of FLAC picture blocks) and is less critical than basic text editing

**Defer:**
- RIFF INFO writing: Lossy mapping (can't represent all 8 fields), adds complexity for minimal user benefit
- Migrating legacy COVERART → METADATA_BLOCK_PICTURE on read: Nice to have but not required for writing
- WAV with both ID3v2 and RIFF INFO: Dual-write complexity not justified

## Edge Cases and Size Considerations

### OGG Vorbis: Large Cover Art

**Problem:** Vorbis Comments in OGG are stored in the comment header packet, which is part of the OGG page structure. Large cover art (e.g., a 5MB PNG) becomes ~6.7MB after base64 encoding. This is stored as a single Vorbis Comment value.

**Impact:** The Vorbis Comment packet may span multiple OGG pages. The OGG page writer must handle packets larger than a single page (max page size ~65KB). This is standard OGG behavior — pages have a segment table that spans packets across pages.

**Mitigation:**
- Xiph spec explicitly supports this
- Major players handle it fine (tested with foobar2000, MediaMonkey, etc.)
- YellowJacket could optionally warn on very large cover art (>2MB) but should not refuse
- Consider downscaling in the UI before embedding (existing cover art flow already handles this)

### WAV: Mixed ID3v2 and RIFF INFO

**Problem:** A WAV file may have both an existing RIFF INFO `LIST` chunk and an `id3 ` chunk.

**Solution:**
- When writing: replace `id3 ` chunk with new one; preserve `LIST INFO` chunk unchanged
- When reading: `dhowden/tag` already handles this (it reads ID3v2 from WAV if present, falls back to RIFF INFO)
- Never delete the user's RIFF INFO data

### WAV: RIFF Size Recalculation

**Problem:** The top-level RIFF chunk has a 32-bit size field. When adding/resizing the `id3 ` chunk, this must be updated.

**Mitigation:** Simple arithmetic: sum of all chunk sizes + headers. The 4GB RIFF limit is a WAV limitation in general, not specific to our tag writing.

### WAV: Chunk Alignment

**Problem:** RIFF chunks must start at even byte offsets. If a chunk has an odd data size, a padding byte must follow.

**Mitigation:** Standard RIFF handling. The chunk parser/writer must account for this.

### OGG: Multiple Logical Streams

**Problem:** OGG files can contain multiple multiplexed streams (e.g., Vorbis audio + cover art stream + metadata stream). Each stream has a unique serial number.

**Mitigation:** Identify the Vorbis stream by the "vorbis" identification header magic bytes. Only modify that stream's comment packet. Copy all other streams' pages unchanged. In practice, music OGG files almost always have a single Vorbis stream.

### OGG: Page Sequence Numbers and Granule Positions

**Problem:** Each OGG page has a sequence number and granule position. Rewriting header pages changes the page count.

**Mitigation:** Header pages (BOS page, comment pages, setup pages) have their own sequence numbers starting from 0. Audio pages continue from after the headers. If the number of header pages changes (because the new comment is larger/smaller), the audio page sequence numbers and granule positions are *not* affected — they reference the audio stream, not the page stream. However, the continued-page flags and sequence numbers must be correct for the rewritten header pages.

## Reuse Analysis

| Component | Existing Code | Reuse Level | Notes |
|---|---|---|---|
| Vorbis Comment field mapping | `flac.go:applyFlacTextChanges` | HIGH — extract shared helper | Same field names, same logic |
| FLAC picture block builder | `flacpicture.NewFromImageData` | HIGH — call directly | Same binary format for OGG |
| ID3v2 tag building | `mp3.go:applyTextChanges`, `applyCoverArtChanges` | HIGH — extract shared helper | Same API for WAV ID3v2 |
| Atomic file writing | `fileutil.AtomicWrite` | FULL — use as-is | No changes needed |
| DB sync pipeline | `dbsync.go:syncDatabase` | FULL — use as-is | Format-independent |
| Tag reader (round-trip tests) | `dhowden/tag` via metadata package | FULL — use as-is | Already reads OGG + WAV |
| MIME detection | `tagwriter.detectMIME` | FULL — use as-is | Same image formats |
| Type helpers (asInt, asBytes) | `tagwriter.go` | FULL — use as-is | Same TagChanges model |
| OGG page parser/writer | — | NEW | Must implement |
| RIFF chunk parser/writer | — | NEW | Must implement |

## Sources

- **Vorbis Comment specification:** https://xiph.org/vorbis/doc/v-comment.html — Official Xiph.Org spec defining field names and encoding (HIGH confidence)
- **VorbisComment wiki (cover art):** https://wiki.xiph.org/VorbisComment#Cover_art — METADATA_BLOCK_PICTURE standard, player compatibility tests (HIGH confidence)
- **FLAC picture block format:** http://flac.sourceforge.net/format.html#metadata_block_picture — Binary structure reused in OGG (HIGH confidence)
- **OGG framing specification:** https://xiph.org/ogg/doc/framing.html — Page structure, CRC, segmentation (HIGH confidence)
- **WAV/RIFF specification:** IBM & Microsoft, "Multimedia Programming Interface and Data Specifications 1.0", 1991 — RIFF chunk format, INFO chunk (HIGH confidence)
- **WAV metadata overview:** https://en.wikipedia.org/wiki/WAV#Metadata — ID3v2 in WAV, XMP in WAV (MEDIUM confidence)
- **bogem/id3v2 library:** https://github.com/bogem/id3v2 — Used for MP3 writing, can generate ID3v2 tags for WAV (HIGH confidence)
- **dhowden/tag library:** https://github.com/dhowden/tag — Used for reading all formats including OGG + WAV (HIGH confidence)
- **go-flac/flacvorbis:** https://github.com/go-flac/flacvorbis — Vorbis Comment manipulation, used in FLAC writer (HIGH confidence)
- **go-flac/flacpicture:** https://github.com/go-flac/flacpicture — FLAC picture block builder, usable for OGG METADATA_BLOCK_PICTURE (HIGH confidence)
- **YellowJacket codebase:** `backend/tagwriter/*.go` — Existing writer pipeline, field model, atomic write pattern (HIGH confidence)
