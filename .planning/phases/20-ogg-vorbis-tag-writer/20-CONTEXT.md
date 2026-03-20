# Phase 20: OGG Vorbis Tag Writer - Context

**Gathered:** 2026-03-19
**Status:** Ready for planning

<domain>
## Phase Boundary

Extend the tag writing pipeline to write metadata and cover art to OGG Vorbis files via a custom OGG page rewriter. Users get the same edit experience as MP3/FLAC/WAV — single-track and batch editing both work. Full file rewrite (not in-place page editing). No OGG Opus, no multi-stream, no external tools.

</domain>

<decisions>
## Implementation Decisions

### Existing comment handling
- Normalize all Vorbis Comment field names to uppercase on write (consistent with FLAC writer pattern)
- Preserve duplicate field entries for non-edited fields as-is (multi-value fields are spec-legal); when editing a field, replace all entries with a single new value
- Preserve the original vendor string — we're a tag editor, not an encoder
- Preserve raw bytes for non-edited fields even if they contain invalid UTF-8 — don't break existing tags because another tool was sloppy

### Cover art edge cases
- When writing or removing cover art, also strip legacy COVERART and COVERARTMIME Vorbis Comment fields to prevent stale art from lingering
- "Clear cover art" removes ALL picture-related fields: METADATA_BLOCK_PICTURE, COVERART, and COVERARTMIME
- When setting cover art, replace all existing METADATA_BLOCK_PICTURE entries with a single front cover (same approach as FLAC writer)
- Write path only — dhowden/tag already handles reading OGG cover art for display

### Error behavior for malformed OGG
- Lenient read, strict write — accept pages with bad CRC on read (some tools produce wrong CRCs), always write correct CRCs (matches WAV parser lenient-read/strict-write philosophy)
- Reject truncated files — if we can't read the complete file structure, refuse the edit (can't guarantee audio preservation on a broken file)
- Reject non-Vorbis OGG — only support OGG Vorbis (check for `\x01vorbis` magic in identification header); OGG Opus, Theora, FLAC, etc. get a clear error
- User-friendly error messages with specific details ("Could not save tags to file.ogg: disk full"), technical info logged via slog

### Multi-stream detection
- Detect early during parse (fail fast), before any write work begins
- Count unique serial numbers across pages — more than one means multi-stream
- Reject chained streams too (multiple sequential Vorbis streams in one file) — unusual for music libraries, each has its own comment header
- Clear rejection message: "This OGG file contains multiple streams and cannot be edited"

### Claude's Discretion
- OGG page size decisions (how to split large Vorbis Comment across pages)
- CRC32 implementation details (MSB-first bit ordering per the OGG spec warning)
- Page sequence number renumbering strategy when comment header page count changes
- Exact structure of the custom OGG page parser/writer
- Test file generation approach for round-trip tests

</decisions>

<specifics>
## Specific Ideas

- Follow the same lenient-read/strict-write philosophy as the WAV RIFF parser — be forgiving about what we accept, strict about what we produce
- The pipeline integration is mechanical: add `FormatOGG` to `DetectFormat`, add `case FormatOGG: err = writeOggTags(...)` to the pipeline switch — same pattern as WAV
- Reuse the existing `replaceVorbisComment` pattern from `flac.go` for text field manipulation (field name uppercase, filter-then-add)
- OGG CRC32 uses non-standard MSB-first bit ordering — Go's `hash/crc32` produces wrong checksums (documented in STATE.md warnings)
- OGG page sequence numbers must be renumbered when comment header page count changes (documented in STATE.md warnings)

</specifics>

<deferred>
## Deferred Ideas

- Migrate legacy COVERART field to METADATA_BLOCK_PICTURE (TAG-01 — tracked in REQUIREMENTS.md future requirements)
- OGG Opus tag writing (FMT-01 — different header structure, `OpusTags` vs `\x03vorbis`, no framing bit)

</deferred>

---

*Phase: 20-ogg-vorbis-tag-writer*
*Context gathered: 2026-03-19*
