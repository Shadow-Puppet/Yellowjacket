# Project Research Summary

**Project:** YellowJacket v1.2.1 Format Parity
**Domain:** Audio metadata tag writing — OGG Vorbis and WAV format support
**Researched:** 2026-03-18
**Confidence:** HIGH

## Executive Summary

YellowJacket v1.2.1 adds OGG Vorbis and WAV tag writing to achieve full format parity across all four supported audio formats. The existing `tagwriter` pipeline was designed for format extension — a switch-case in `pipeline.go` dispatches to format-specific writer functions, and everything downstream (DB sync, events, UI) is format-agnostic. Adding OGG and WAV requires two new writer functions, zero new dependencies, and no frontend changes.

The recommended approach is **custom implementations for both formats**: a custom OGG page rewriter (~350 LOC) for OGG Vorbis, and a custom RIFF chunk parser (~200 LOC) wrapping the existing `bogem/id3v2` library for WAV. No pure-Go library exists for OGG Vorbis tag writing, and the Go WAV ecosystem recently lost its most popular library (`go-audio/wav` archived Feb 2026). The OGG container format and RIFF chunk format are both well-documented and simple enough to implement directly. Vorbis Comments (used by OGG) are the exact same metadata format already used in FLAC — field names, encoding, and comment structure are identical, enabling significant code reuse.

The primary risk is the OGG page infrastructure: CRC32 checksums use a non-standard bit ordering (MSB-first, not the Go standard library's reflected CRC32), page sequence numbers must be strictly sequential, and granule positions must be preserved exactly. These are well-understood constraints with clear specifications, but incorrect implementation produces silently corrupted files. Round-trip testing (write → read back via `dhowden/tag`) is the primary mitigation, following the pattern established by the existing MP3 and FLAC writers.

## Key Findings

### Recommended Stack

Zero new dependencies. Both formats are implemented using Go standard library primitives (`encoding/binary`, `encoding/base64`, `bytes`, `io`) plus existing dependencies for shared functionality.

**Core technologies:**
- **Custom OGG page rewriter:** Parse/write OGG pages with CRC32 and segmentation — no pure-Go OGG writing library exists; `mccoyst/ogg` (37 stars, no semver) saves ~80 LOC but adds dependency risk
- **Custom RIFF chunk parser/writer:** Read/write WAV RIFF structure — `go-audio/wav` and `go-audio/riff` were archived Feb 2026; RIFF is simple enough for custom code
- **`bogem/id3v2` (existing):** Generate ID3v2 tags for WAV `id3 ` chunks — same library already used for MP3 writing, all 8 fields + cover art work identically
- **`go-flac/flacpicture` (existing):** Build METADATA_BLOCK_PICTURE binary blocks for OGG cover art — same binary format, just base64-wrapped for OGG
- **`fileutil.AtomicWrite` (existing):** Crash-safe file writes for both formats — proven pattern from MP3/FLAC writers

### Expected Features

**Must have (table stakes):**
- Write all 8 text fields for OGG Vorbis (same Vorbis Comment field names as FLAC)
- Write all 8 text fields for WAV (via ID3v2 in RIFF chunk)
- Preserve existing non-edited metadata (ReplayGain, lyrics, etc.)
- Preserve audio data byte-for-byte (no re-encoding)
- Crash-safe writes via AtomicWrite
- Batch and single-track editing work for both formats

**Should have (differentiators):**
- OGG cover art via METADATA_BLOCK_PICTURE (base64-encoded FLAC picture block)
- WAV cover art via ID3v2 APIC frame (identical to MP3)
- Preserve existing RIFF INFO chunks when writing ID3v2 to WAV
- Round-trip test coverage (7 tests per format, following FLAC precedent)

**Defer (v2+):**
- RIFF INFO writing (lossy — can't represent album_artist, disc_number, or cover art)
- Dual-write ID3v2 + RIFF INFO in WAV
- Migrating deprecated OGG `COVERART` field to `METADATA_BLOCK_PICTURE`
- RF64 (>4GB WAV) support
- OGG Opus tag writing (different header structure from Vorbis)

### Architecture Approach

Both writers integrate into the existing pipeline with minimal modification: add two format constants, extend the `DetectFormat()` switch, and add two cases to the `WriteTrackTags()` format dispatch. No new interfaces, no refactoring. The frontend is completely format-agnostic and requires zero changes.

**Major components:**
1. **`ogg.go`** (~350 LOC) — OGG page parser/writer, Vorbis Comment serializer, CRC32, METADATA_BLOCK_PICTURE encoding, `writeOggTags()` orchestrator
2. **`wav.go`** (~200 LOC) — RIFF chunk parser/writer, ID3v2 tag in `id3 ` chunk via `bogem/id3v2`, `writeWavTags()` orchestrator
3. **`tagwriter.go` + `pipeline.go`** (~15 LOC changes) — Format constants, detection, dispatch
4. **`ogg_test.go` + `wav_test.go`** (~600 LOC) — 7 round-trip tests each, following FLAC pattern

**Key code reuse:**
- Vorbis Comment field mapping: identical to FLAC (extract shared helpers from `flac.go`)
- ID3v2 tag building for WAV: identical to MP3 (`applyTextChanges()`, `applyCoverArtChanges()`)
- AtomicWrite: used as-is by both writers
- `dhowden/tag` for read-back verification in tests

### Critical Pitfalls

1. **OGG CRC32 non-standard bit ordering (P1)** — OGG uses MSB-first CRC32 with polynomial 0x04c11db7. Go's `hash/crc32` uses reflected (LSB-first) ordering and produces wrong checksums. Must implement custom CRC or port from `jfreymuth/oggvorbis/crc.go`.

2. **OGG page sequence number continuity (P2)** — Rewriting comment header may change the number of header pages, requiring all subsequent page sequence numbers to be renumbered. Use full-stream rewrite approach (correct by construction).

3. **OGG granule position preservation (P3)** — Header pages must have granule position 0; audio pages must preserve original granule positions exactly. Corruption here breaks seeking and duration reporting.

4. **WAV RIFF chunk size updates (P6)** — Adding or resizing the `id3 ` chunk requires updating the outer RIFF header size field. Wrong size makes the file appear truncated to some players.

5. **WAV chunk word alignment (P10)** — RIFF chunks must start at even byte offsets. Odd-length chunks need a padding byte that's NOT included in the chunk's size field but IS part of the physical file.

### WAV Metadata Approach Decision

Research revealed a tension between two approaches:
- **RIFF INFO:** Native WAV format, simple, but can't represent album_artist, disc_number, or cover art
- **ID3v2-in-WAV:** Reuses existing `bogem/id3v2`, full field + cover art support, read by `dhowden/tag`

**Decision: ID3v2-in-WAV.** This gives full field parity with MP3, enables cover art, reuses existing code, and round-trips through `dhowden/tag` (our reader). RIFF INFO is preserved when present but not written to.

## Implications for Roadmap

Based on research, suggested phase structure:

### Phase 1: WAV Tag Writer
**Rationale:** Lower risk, faster to implement. Reuses existing `bogem/id3v2` library and `applyTextChanges()`/`applyCoverArtChanges()` from MP3 writer. RIFF container is simpler than OGG (no checksums, no page segmentation). Building this first proves the pipeline extension pattern works before tackling the harder OGG format.
**Delivers:** WAV text tag writing (all 8 fields) + cover art + round-trip tests
**Addresses:** WAV table stakes + WAV cover art (differentiator, but trivial since it reuses MP3 APIC code)
**Avoids:** P5 (use ID3v2, not RIFF INFO), P6 (careful RIFF size bookkeeping), P10 (chunk alignment padding)
**New code:** ~200 LOC `wav.go` + ~250 LOC `wav_test.go` + ~15 LOC pipeline changes
**Estimated effort:** Small — RIFF parsing is straightforward binary parsing

### Phase 2: OGG Vorbis Text Tag Writer
**Rationale:** OGG requires the most new infrastructure (page parser, CRC32, segmentation). Text-only tag writing exercises all the hard parts (page rewrite, CRC, sequence numbers) without the added complexity of multi-page comment packets from large cover art. This is the riskiest phase and benefits from Phase 1 having proven the pipeline extension works.
**Delivers:** OGG Vorbis text tag writing (all 8 fields) + round-trip tests
**Addresses:** OGG text field table stakes, audio data preservation, existing comment preservation
**Avoids:** P1 (CRC32), P2 (sequence numbers), P3 (granule positions), P4 (three-header structure), P7 (framing bit), P8 (packet prefix)
**New code:** ~300 LOC `ogg.go` (page infra + text writer) + ~300 LOC `ogg_test.go`
**Estimated effort:** Medium — OGG page infrastructure is the hardest new code in this milestone

### Phase 3: OGG Vorbis Cover Art
**Rationale:** Separated from Phase 2 because it adds multi-page packet complexity (large base64-encoded images can exceed the ~64KB OGG page limit). Text fields exercise the page infrastructure with small comment packets; cover art stress-tests it with large ones. Can be deferred if Phase 2 runs long without blocking the milestone.
**Delivers:** OGG Vorbis cover art embed/remove via METADATA_BLOCK_PICTURE
**Addresses:** OGG cover art differentiator
**Avoids:** P9 (METADATA_BLOCK_PICTURE format), P15 (multi-page segmentation for large payloads)
**New code:** ~50 LOC additions to `ogg.go` + ~50 LOC additions to `ogg_test.go`
**Estimated effort:** Small if Phase 2's page infrastructure is solid; medium if multi-page edge cases surface

### Phase 4: Edge Cases and Cleanup
**Rationale:** Validation and hardening after core functionality works. Adds detection/rejection of unsupported edge cases, size warnings, and documentation updates.
**Delivers:** RF64 detection, multi-stream OGG detection, large file warnings, PROJECT.md updates
**Addresses:** P13 (multi-stream OGG), P16 (RF64 WAV), P12 (disk space for large files)
**New code:** ~30 LOC validation checks + documentation updates
**Estimated effort:** Small

### Phase Ordering Rationale

- **WAV before OGG:** WAV is lower risk (reuses existing ID3v2 library, simpler container) and proves the pipeline extension pattern. OGG requires all-new page infrastructure with correctness-critical CRC and sequencing.
- **OGG text before OGG cover art:** Text fields exercise the page rewrite with small comment packets. Cover art adds multi-page complexity that should only be attempted once the core page infrastructure is validated by round-trip tests.
- **Edge cases last:** Detection/rejection of unusual files (RF64, multi-stream) is low risk and low effort — just validation guards at file-open time.

### Research Flags

Phases likely needing deeper research during planning:
- **Phase 2 (OGG text writer):** The OGG page re-segmentation and CRC implementation is the most complex new code. The spec is clear, but implementation details (lacing values, continuation flags, packet splitting across pages) benefit from studying `jfreymuth/oggvorbis` source as reference. Phase-level research recommended.

Phases with standard patterns (skip research-phase):
- **Phase 1 (WAV writer):** RIFF parsing is trivial; ID3v2 tag generation reuses existing code. Well-documented, no unknowns.
- **Phase 3 (OGG cover art):** METADATA_BLOCK_PICTURE format is well-specified; base64 encoding is trivial. Only depends on Phase 2's page infrastructure being correct.
- **Phase 4 (edge cases):** Simple validation checks with clear specifications.

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | Zero new dependencies; all recommendations based on official specs and existing codebase analysis |
| Features | HIGH | Feature set derived from official format specs (Xiph.org, RIFF) and existing codebase field model |
| Architecture | HIGH | Full codebase analysis confirms pipeline was designed for format extension; minimal changes needed |
| Pitfalls | HIGH | Pitfalls sourced from official OGG/RIFF specs, cross-referenced with existing library implementations |

**Overall confidence:** HIGH

### Gaps to Address

- **`bogem/id3v2` WAV compatibility:** The library's `Open()`/`Save()` API expects MP3 file structure. For WAV, we'll need to use `ParseReader()` to read existing ID3v2 tags from a byte slice, and `WriteTo()` to serialize the tag to bytes for embedding in the RIFF chunk. This needs validation during Phase 1 implementation — if `ParseReader` doesn't work for standalone tag parsing, we may need to create tags from scratch (losing existing ID3v2 data in the WAV).
- **OGG test fixture creation:** Cannot programmatically generate a valid OGG Vorbis file (requires Vorbis codebook data in setup header). Need to embed a minimal OGG fixture via `//go:embed`. Can be created once with ffmpeg during Phase 2 setup.
- **Multi-stream OGG prevalence:** Research confirms multi-stream OGG music files are extremely rare, but we should detect and reject them rather than silently corrupting. Validation during Phase 2.

## Sources

### Primary (HIGH confidence)
- OGG framing specification: https://xiph.org/ogg/doc/framing.html
- OGG RFC 3533: https://xiph.org/ogg/doc/rfc3533.txt
- Vorbis I Specification (comment field): https://xiph.org/vorbis/doc/Vorbis_I_spec.html
- Vorbis Comment specification: https://xiph.org/vorbis/doc/v-comment.html
- METADATA_BLOCK_PICTURE: https://wiki.xiph.org/VorbisComment#METADATA_BLOCK_PICTURE
- FLAC Picture block format: https://xiph.org/flac/format.html#metadata_block_picture
- RIFF/WAV format: https://www.mmsp.ece.mcgill.ca/documents/AudioFormats/WAVE/WAVE.html

### Secondary (MEDIUM confidence)
- WAV metadata overview: https://en.wikipedia.org/wiki/WAV#Metadata
- RIFF tag reference: https://exiftool.org/TagNames/RIFF.html

### Libraries (HIGH confidence — direct code review)
- `jfreymuth/oggvorbis` v1.0.5: OGG page reader reference, CRC32 lookup table
- `dhowden/tag`: Reads OGG + WAV tags; validates round-trip correctness
- `bogem/id3v2` v2.1.4: ID3v2 tag generation for WAV `id3 ` chunks
- `go-flac/flacpicture` v2.0.2: FLAC picture block builder, reused for OGG METADATA_BLOCK_PICTURE
- `go-audio/wav` (ARCHIVED 2026-02-21): Evaluated and rejected
- `mccoyst/ogg` (37 stars, no semver): Evaluated and rejected — marginal benefit vs dependency risk

### Codebase (HIGH confidence — validated in v1.2)
- `backend/tagwriter/flac.go` — Vorbis Comment manipulation patterns to reuse
- `backend/tagwriter/mp3.go` — ID3v2 + AtomicWrite patterns to reuse for WAV
- `backend/tagwriter/pipeline.go` — Format dispatch switch to extend
- `backend/tagwriter/tagwriter.go` — TagChanges model, format detection, helpers
- `backend/fileutil/atomicwrite.go` — Crash-safe file write utility

---
*Research completed: 2026-03-18*
*Ready for roadmap: yes*
