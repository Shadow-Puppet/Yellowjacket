# Phase 19: WAV Tag Writer - Context

**Gathered:** 2026-03-18
**Status:** Ready for planning

<domain>
## Phase Boundary

Extend the existing tag writing pipeline so WAV files get the same metadata and cover art editing experience as MP3/FLAC. Pure backend — the single-track and batch edit UI is already built (Phases 17-18). This phase adds a `writeWavTags()` function, wires it into the format detection switch, and tests round-trip correctness via ID3v2-in-RIFF.

</domain>

<decisions>
## Implementation Decisions

### RIFF chunk preservation
- Preserve ALL non-ID3v2 chunks byte-for-byte when rewriting the WAV file (fmt, data, LIST INFO, bext, cue, smpl, iXML, and any unknown/proprietary chunks)
- Existing RIFF LIST INFO chunks are kept as-is, even if they contain stale metadata after an ID3v2 edit — our reader already prefers ID3v2, so stale INFO won't affect display
- Existing ID3v2 chunks in the WAV are replaced entirely — open with bogem/id3v2, apply changes, write fresh tag (same pattern as MP3 writer)
- ID3v2 chunk placed at end of file (after all other chunks) — most common convention, simplest implementation

### Cover art constraints
- No size limit on embedded cover art — accept whatever the user provides, consistent with MP3/FLAC behavior
- JPEG and PNG only, detected by magic bytes via existing `detectMIME()` function — same as MP3
- Clearing cover art removes the APIC frame only; the ID3v2 chunk is kept even if only text frames remain
- Read and merge existing ID3v2 tags from WAV before applying changes — preserves unknown frames (lyrics, custom tags) added by other tools

### Error messaging
- User-facing errors are friendly: "Could not write tags to [filename]: file appears to be damaged" — hide technical details
- Technical error details (chunk offsets, sizes, parse failures) logged via slog for debugging
- Permission-aware messages: distinguish "file is read-only", "file is in use", and generic "write failed"
- Batch error handling identical to MP3/FLAC — use existing BatchFailure struct, no WAV-specific categorization

### WAV variant handling
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

</decisions>

<specifics>
## Specific Ideas

- Follow existing MP3 writer pattern closely: read tag → apply changes → atomic write with audio data copy
- Use same `bogem/id3v2/v2` library for the ID3v2 portion — the tag format is identical to MP3's ID3v2, just wrapped in a RIFF chunk
- Test pattern should mirror `mp3_test.go` and `flac_test.go`: text fields round-trip, cover art round-trip, clear cover art, partial update, atomic safety
- STATE.md warning: "WAV RIFF chunks must start at even byte offsets — odd-length chunks need a padding byte" — this is already known

</specifics>

<deferred>
## Deferred Ideas

- RF64/BW64 support — future phase (FMT-02 in REQUIREMENTS.md)
- RIFF INFO dual-write alongside ID3v2 — explicitly deferred (FMT-03 in REQUIREMENTS.md)
- BWF bext chunk writing — out of scope (preserve only, not write)

</deferred>

---

*Phase: 19-wav-tag-writer*
*Context gathered: 2026-03-18*
