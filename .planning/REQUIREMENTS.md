# Requirements: YellowJacket v1.2.1

**Defined:** 2026-03-18
**Core Value:** The music player works reliably and feels solid — every interaction is correct, responsive, and trustworthy.

## v1.2.1 Requirements

Requirements for Format Parity milestone. Each maps to roadmap phases.

### OGG Vorbis Tag Writing

- [ ] **OGG-01**: User can edit all 8 text metadata fields (title, artist, album, album_artist, genre, year, track#, disc#, composer) on OGG Vorbis files
- [ ] **OGG-02**: OGG tag writes preserve existing non-edited Vorbis Comment fields (ReplayGain, lyrics, etc.)
- [ ] **OGG-03**: OGG tag writes preserve audio data identically (lossless round-trip)
- [ ] **OGG-04**: User can embed, replace, and remove cover art in OGG Vorbis files via METADATA_BLOCK_PICTURE
- [ ] **OGG-05**: OGG tag writing uses crash-safe atomic writes (write-to-temp-then-rename)
- [ ] **OGG-06**: OGG writer round-trip tests verify all fields via dhowden/tag read-back

### WAV Tag Writing

- [x] **WAV-01**: User can edit all 8 text metadata fields on WAV files via ID3v2 chunk in RIFF container
- [x] **WAV-02**: WAV tag writes preserve existing RIFF INFO and other chunks (bext, cue, smpl, etc.) unchanged
- [x] **WAV-03**: WAV tag writes preserve audio data identically (lossless round-trip)
- [x] **WAV-04**: User can embed, replace, and remove cover art in WAV files via ID3v2 APIC frame
- [x] **WAV-05**: WAV tag writing uses crash-safe atomic writes (write-to-temp-then-rename)
- [ ] **WAV-06**: WAV writer round-trip tests verify all fields via dhowden/tag read-back

### Cleanup

- [ ] **CLEAN-01**: Fix pre-existing lint warnings (nlreturn/wsl) in dbsync.go and tagwriter.go
- [ ] **CLEAN-02**: General v1.2 cleanup sweep — any small issues that surfaced during tag editing milestone

## Future Requirements

Deferred to future milestones. Tracked but not in current roadmap.

### Format Extensions

- **FMT-01**: OGG Opus tag writing (.opus files — different header structure from OGG Vorbis)
- **FMT-02**: RF64/BWF64 tag writing (WAV files >4GB)
- **FMT-03**: RIFF INFO writing for WAV (dual-write alongside ID3v2 for legacy player compatibility)

### Tag Features

- **TAG-01**: Migrate legacy COVERART field to METADATA_BLOCK_PICTURE on OGG files
- **TAG-02**: ReplayGain tag editing

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
|---------|--------|
| OGG Opus tag writing | Different packet header structure (`OpusTags` vs `\x03vorbis`), no framing bit — separate effort |
| RIFF INFO as primary WAV write target | Cannot represent album_artist, disc_number, or cover art — ID3v2 is strictly superior |
| WAV RIFF INFO writing (dual-write) | Adds complexity for marginal benefit; preserve existing INFO but write ID3v2 only |
| BWF (bext) chunk writing | Broadcast metadata, not music metadata — preserve if present, don't write |
| In-place OGG page editing | Full rewrite is simpler and crash-safe; surgical editing is fragile for marginal I/O savings |
| Multi-stream OGG editing | Detect and reject; music files are single-stream |
| External CLI tools (vorbiscomment, ffmpeg) | Violates pure-Go constraint; distribution complexity |

## Traceability

Which phases cover which requirements. Updated during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| WAV-01 | Phase 19 | Complete |
| WAV-02 | Phase 19 | Complete |
| WAV-03 | Phase 19 | Complete |
| WAV-04 | Phase 19 | Complete |
| WAV-05 | Phase 19 | Complete |
| WAV-06 | Phase 19 | Pending |
| OGG-01 | Phase 20 | Pending |
| OGG-02 | Phase 20 | Pending |
| OGG-03 | Phase 20 | Pending |
| OGG-04 | Phase 20 | Pending |
| OGG-05 | Phase 20 | Pending |
| OGG-06 | Phase 20 | Pending |
| CLEAN-01 | Phase 21 | Pending |
| CLEAN-02 | Phase 21 | Pending |

**Coverage:**
- v1.2.1 requirements: 14 total
- Mapped to phases: 14
- Unmapped: 0 ✓

---
*Requirements defined: 2026-03-18*
*Last updated: 2026-03-18 — traceability updated with phase mappings*
