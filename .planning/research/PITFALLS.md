# Domain Pitfalls: OGG Vorbis + WAV Tag Writing

**Domain:** Adding OGG Vorbis and WAV tag writing to an existing tag writing pipeline (MP3 + FLAC already working)
**Researched:** 2026-03-18
**Confidence:** HIGH (OGG spec + RFC 3533 + codebase analysis + Xiph Vorbis comment spec + RIFF spec)

---

## Critical Pitfalls

Mistakes that cause file corruption, unplayable audio, or require significant rework.

### P1: OGG CRC32 Recalculation After Page Modification

**Severity:** BLOCKS SHIP — corrupted files won't play
**What goes wrong:** Every OGG page contains a CRC32 checksum (bytes 22–25 of the page header) computed over the entire page (header with CRC field zeroed + segment table + page data). The polynomial is `0x04c11db7` but uses a **non-standard bit ordering** — it's a direct algorithm (MSB-first), NOT the common CRC32 used in zlib/Ethernet (which is reflected/LSB-first). If you use Go's standard `hash/crc32` package with `crc32.MakeTable(0x04c11db7)`, you get the **wrong CRC** because `hash/crc32` uses reflected bit ordering.

**Why it happens:** Developers see "CRC32 with polynomial 0x04c11db7" and reach for the standard library. OGG uses a non-reflected (direct) CRC32 which is different from IEEE CRC32 despite sharing the same polynomial constant. The dhowden/tag source code (ogg.go) and jfreymuth/oggvorbis (crc.go) both implement custom CRC32 lookup tables for this reason.

**Consequences:** Every OGG decoder will reject pages with incorrect CRC. The file appears corrupted. Players show "codec error" or silence. Some players may try to resync and play garbled audio.

**Prevention:**
1. **Copy the CRC implementation from jfreymuth/oggvorbis/crc.go** — it's already an indirect dependency and has the correct OGG CRC32 table (MSB-first, polynomial 0x04c11db7).
2. **Never use `hash/crc32` from the standard library** for OGG pages.
3. Compute CRC with the checksum field set to zero in the header bytes, then write the computed CRC into bytes 22–25.
4. **Round-trip test:** write file → verify every page's CRC matches by re-reading with a CRC-checking reader.

**Detection:** After writing, re-read every page and verify CRC matches. This should be a unit test, not just manual verification.

**Phase:** OGG writer core (first thing to get right)

---

### P2: OGG Page Sequence Number Continuity

**Severity:** BLOCKS SHIP — seeking and error recovery break
**What goes wrong:** When rewriting OGG pages (because the comment header packet changed size), all subsequent page sequence numbers must be recalculated to remain strictly sequential (0, 1, 2, 3, ...). If you modify the comment header and it now spans a different number of pages, every page after it in the stream has a wrong sequence number. OGG decoders use sequence numbers for page loss detection and seeking.

**Why it happens:** The comment header is typically in page 1 (page 0 = BOS with codec identification). If the comment data grows large enough to span multiple pages, or shrinks from spanning multiple to fitting in one, the total page count changes, and all subsequent audio pages need renumbering.

**Consequences:** Players report "pages missing" or "page sequence gap." Seeking to specific positions fails. Some decoders abort playback entirely. Others may play but report errors in the log.

**Prevention:**
1. **Full-stream rewrite approach:** Read all pages → replace comment packet → regenerate all pages with correct sequence numbers → write as new file. This is the safest approach and matches how FLAC rewrite already works.
2. After rewriting, verify: page 0 has sequence 0, page 1 has sequence 1, etc.
3. The approach of "only rewrite the comment pages and shift the rest" is theoretically possible but fragile — the full rewrite is correct-by-construction.

**Detection:** After writing, scan through all pages and verify monotonically increasing sequence numbers starting from 0.

**Phase:** OGG writer core

---

### P3: OGG Granule Position Corruption

**Severity:** BLOCKS SHIP — playback duration and seeking wrong
**What goes wrong:** Each OGG page has a 64-bit absolute granule position field (bytes 6–13) that encodes the total decoded samples up to and including the last completed packet on that page. If you rewrite pages and accidentally modify granule positions, seeking breaks and the reported duration is wrong. A special value of -1 (0xFFFFFFFFFFFFFFFF) means "no packets complete on this page."

**Why it happens:** When splitting or merging pages during rewrite, it's easy to accidentally assign granule positions to pages that contain the comment header (which should have granule position 0 for the first two pages per the Vorbis spec) or to shift granule positions of audio pages.

**Consequences:** Player reports wrong track duration. Seeking jumps to wrong positions. Progress bar is inaccurate.

**Prevention:**
1. **Header pages (BOS page + comment page(s)) MUST have granule position 0.** This is mandated by the Vorbis I spec.
2. **Audio page granule positions must be preserved exactly as-is** from the original file. Since we're only modifying the comment header, audio data doesn't change.
3. The full-rewrite approach: copy BOS page (granule=0), write new comment pages (granule=0), then copy all remaining audio pages verbatim (preserving their original granule positions but updating sequence numbers and CRCs).

**Detection:** Compare granule positions of audio pages before and after writing.

**Phase:** OGG writer core

---

### P4: OGG Vorbis Packet Structure — Three-Header Requirement

**Severity:** BLOCKS SHIP — file unplayable if headers are wrong
**What goes wrong:** A Vorbis stream in OGG has exactly three header packets in order: (1) identification header (starts with `\x01vorbis`), (2) comment header (starts with `\x03vorbis`), (3) setup header (starts with `\x05vorbis`). The identification header MUST be alone on the first page (BOS page). The comment and setup headers MUST appear before any audio data and MUST begin on the second page. If the rewriter corrupts the packet boundary between the comment and setup headers, the decoder can't initialize.

**Why it happens:** The comment header can span multiple pages (especially with large cover art). The setup header immediately follows in the same stream of pages. If you modify the comment header's size, the boundary between comment and setup packets shifts. If you re-segment into pages incorrectly, the setup header may be split wrong.

**Consequences:** Decoder fails to initialize. File appears to be an invalid Vorbis stream.

**Prevention:**
1. Parse the original stream to identify exactly where each header packet starts and ends.
2. Replace only the comment packet data. Preserve identification and setup packets byte-for-byte.
3. When re-assembling into pages: page 0 = BOS with identification header only. Page 1+ = comment header + setup header (they can share pages, but identification must be alone on page 0).
4. After writing, verify all three header packets are parseable.

**Phase:** OGG writer core

---

### P5: WAV ID3v2 vs RIFF INFO — Choosing Wrong Metadata Format

**Severity:** BLOCKS SHIP — metadata not readable by players or our own reader
**What goes wrong:** WAV files can contain metadata in multiple formats: RIFF INFO chunks (LIST/INFO), ID3v2 tags (`id3 ` or `ID3 ` chunk), or both. dhowden/tag reads **ID3v2 tags from WAV files** (it looks for the `ID3` marker inside RIFF chunks). If we write RIFF INFO tags but dhowden/tag reads ID3v2, our written tags won't round-trip through our own reader. If we write ID3v2 but the file already has RIFF INFO, players that prefer RIFF INFO will show old data.

**Why it happens:** There's no single WAV metadata standard. The music production world uses RIFF INFO (INAM, IART, etc.), while the consumer audio world often uses ID3v2 embedded in WAV. Different tools write different formats. dhowden/tag's WAV support reads ID3v2 tags.

**Consequences:** Tags appear written but don't show up when re-reading the file. Or worse, conflicting metadata between ID3v2 and RIFF INFO confuses players.

**Prevention:**
1. **Write ID3v2 tags in WAV files** because that's what dhowden/tag reads back. Use the same `bogem/id3v2/v2` library already used for MP3 tag writing.
2. WAV ID3v2 approach: the `id3 ` chunk in RIFF contains a complete ID3v2 tag. Write the ID3v2 tag into this chunk.
3. If the file has existing RIFF INFO tags, **leave them alone** — don't delete them, don't try to sync them. Only modify the ID3v2 chunk.
4. Round-trip test: write via our writer → read via `metadata.ExtractTags` → verify all fields match.

**Detection:** Write a tag, then immediately read it back with dhowden/tag. If any field doesn't round-trip, the format choice is wrong.

**Phase:** WAV writer core

---

### P6: WAV RIFF Chunk Size Updates

**Severity:** BLOCKS SHIP — file unplayable if sizes are wrong
**What goes wrong:** WAV is a RIFF container where every chunk has a 4-byte ID + 4-byte little-endian size. The outermost RIFF chunk's size field must equal the total file size minus 8 bytes. If you add or resize the `id3 ` chunk but don't update the outer RIFF size, the file appears truncated to parsers. Some players ignore the RIFF size and read to EOF, but others (including some decoders) stop at the declared size.

**Why it happens:** When adding an ID3v2 chunk to a WAV that didn't have one, or resizing an existing chunk, you need to update not just the chunk's own size but also the parent RIFF size and possibly the `data` chunk boundaries.

**Consequences:** File appears truncated. Some players play it fine (they read to EOF), others refuse to open it or play only partial audio.

**Prevention:**
1. **Full-file rewrite approach (like FLAC):** Read original → write RIFF header → write `fmt ` chunk → write `data` chunk → write other chunks → write `id3 ` chunk → fix RIFF size. AtomicWrite handles crash safety.
2. Calculate final RIFF size as sum of all chunk sizes + their 8-byte headers + 4 bytes for "WAVE" form type.
3. Place the `id3 ` chunk **after** the `data` chunk, not before it. Putting metadata before audio data means players must seek past it, and some naive parsers may not handle it.
4. Test with both small WAV files (~1KB) and medium WAV files (~50MB) to catch size calculation bugs.

**Detection:** After writing, verify: `file_size == RIFF_size + 8`. Open with beep's wav.Decode to verify playback still works.

**Phase:** WAV writer core

---

## Moderate Pitfalls

Mistakes that cause incorrect behavior, data loss in edge cases, or significant debugging time.

### P7: OGG Vorbis Comment Framing Bit

**Severity:** HIGH — file won't read if framing bit is wrong
**What goes wrong:** The Vorbis comment header (inside OGG Vorbis streams) ends with a mandatory **framing bit**. This is a single `1` bit at the end of the comment data, byte-aligned. If you construct the comment header packet without this framing bit, Vorbis decoders will reject the header. The Vorbis I spec says: "if framing_bit unset or end of packet then ERROR."

**Why it happens:** The OGG Opus format does NOT have this framing bit (OpusTags format omits it). If you're looking at Opus examples or generic Vorbis comment code, you might omit it. The framing bit is Vorbis-specific, not a general Vorbis Comment feature.

**Prevention:**
1. After writing vendor string + comment count + all comments, write a byte `0x01` (framing bit set in LSB position).
2. If reusing flacvorbis library to build the comment data: **FLAC Vorbis Comments do NOT have a framing bit** (FLAC metadata blocks have their own length framing). So you can't just take the FLAC Vorbis comment bytes and paste them into an OGG packet — you need to add the framing bit.
3. Test: write a file, then verify `oggvorbis.GetCommentHeader()` successfully parses it.

**Phase:** OGG writer

---

### P8: OGG Vorbis Comment Packet Prefix

**Severity:** HIGH — comment header not recognized
**What goes wrong:** In OGG Vorbis, the comment header packet must start with the 7-byte prefix `\x03vorbis`. In OGG Opus, it starts with `OpusTags` (8 bytes). If you omit this prefix or use the wrong one, the reader won't find the comment header. dhowden/tag checks for both prefixes (`\x03vorbis` and `OpusTags`) to dispatch to Vorbis comment parsing.

**Why it happens:** Confusing OGG Vorbis with OGG Opus. Or confusing FLAC Vorbis Comments (which have no packet prefix, they're identified by metadata block type) with OGG Vorbis Comments.

**Prevention:**
1. When building the comment packet for OGG Vorbis: prepend `\x03vorbis` (7 bytes) before the Vorbis Comment data.
2. The comment data itself (vendor string, comment list) is identical in format to what FLAC uses.
3. **Scope decision:** For v1.2.1, only support `.ogg` files containing Vorbis (prefix `\x01vorbis` on identification header). Do NOT attempt to handle Opus files (`.opus`) — that's a different format with different header structure.

**Phase:** OGG writer

---

### P9: Cover Art in OGG Vorbis — METADATA_BLOCK_PICTURE

**Severity:** MEDIUM-HIGH — cover art either too large or not recognized
**What goes wrong:** Vorbis Comments don't have a dedicated picture block like FLAC. Instead, cover art is stored as a `METADATA_BLOCK_PICTURE` comment field whose value is a **base64-encoded** FLAC PICTURE block. A typical 500KB JPEG becomes ~680KB of base64 text in a single Vorbis Comment entry. This dramatically increases the comment header size, which means the comment header may span many OGG pages (each page is max ~64KB). Additionally, some players only support this format while others look for a `COVERART` field (deprecated).

**Why it happens:** The Vorbis Comment spec has no native image support. The community adopted the FLAC PICTURE structure encoded as base64 in a comment field, but it's cumbersome and space-inefficient.

**Consequences:**
- A 1MB cover image becomes ~1.36MB of base64, which needs ~21 OGG pages just for the comment header.
- Large comment headers stress the page segmentation code — more pages means more CRC calculations, more sequence number management.
- If you use the deprecated `COVERART` field instead, dhowden/tag won't read it.

**Prevention:**
1. Use the `METADATA_BLOCK_PICTURE` field name.
2. Value format: base64-encode a FLAC PICTURE structure (picture type + MIME type + description + width/height/depth/colors + image data). The `go-flac/flacpicture` library already knows this format — marshal a `flacpicture.MetadataBlockPicture`, then base64-encode the result.
3. **Consider a max cover art size limit** (e.g., 2MB) to avoid pathological page counts. Log a warning for large images.
4. Test round-trip: write cover art → read back with dhowden/tag → verify bytes match.

**Phase:** OGG writer (possibly deferred from initial implementation if too complex)

---

### P10: WAV Chunk Word Alignment

**Severity:** MEDIUM — some parsers fail on unaligned chunks
**What goes wrong:** The RIFF specification requires chunks to start at even byte offsets (word-aligned). If a chunk has an odd-length data section, a padding byte (`0x00`) must be added after the data before the next chunk begins. This padding byte is NOT included in the chunk's size field. If you write an `id3 ` chunk with odd-length data and don't add the pad byte, the next chunk's header will be misaligned and parsers will fail to find it.

**Why it happens:** Easy to forget the padding byte, especially since the size field doesn't include it. Many existing WAV files have this bug (written by buggy software), so you might test with files that happen to have even-length chunks and never notice.

**Consequences:** Parsers following the misaligned chunk will read garbage as the next chunk ID. Some parsers handle this gracefully (try realignment), others crash or report corruption.

**Prevention:**
1. After writing each chunk's data, check if `chunk_data_length % 2 != 0`. If so, write one zero byte.
2. Don't count this byte in the chunk's size field.
3. DO count it when calculating the outer RIFF size (since it's part of the physical file).
4. Test with an `id3 ` chunk that has odd-length data.

**Phase:** WAV writer

---

### P11: Reading/Writing Library Asymmetry

**Severity:** MEDIUM — data loss or failure if libraries disagree on format
**What goes wrong:** The reading path uses `dhowden/tag` for metadata extraction. The writing path uses different libraries: `bogem/id3v2` for MP3, `go-flac/go-flac` for FLAC, and will use custom code for OGG. If these libraries disagree on field encoding, round-trip testing will fail. For example, dhowden/tag may normalize certain fields that the writer preserves verbatim, or vice versa.

**Why it happens:** No single Go library handles both reading and writing for all formats. Each library has its own interpretation of edge cases.

**Consequences:** Tags appear to be saved but read back differently. Or dhowden/tag can't parse what the writer produces.

**Prevention:**
1. **Round-trip testing is mandatory for every format.** Write tags → read back with dhowden/tag → assert all 8 fields match.
2. For OGG: since we're writing the Vorbis Comment bytes ourselves, we control the exact format. Use the same field names dhowden/tag expects (TITLE, ARTIST, ALBUM, ALBUMARTIST, GENRE, DATE, TRACKNUMBER, DISCNUMBER, COMPOSER).
3. For WAV: use `bogem/id3v2` (same as MP3) for the ID3v2 chunk content. The round-trip behavior should match MP3 tests.
4. Watch for Vorbis Comment field name case sensitivity: field names are case-insensitive per spec, but dhowden/tag may return them in a specific case. Our writer should use UPPERCASE field names (Vorbis convention).

**Phase:** All writer implementations

---

### P12: AtomicWrite Disk Space for Large Files

**Severity:** MEDIUM — write fails on low disk space
**What goes wrong:** AtomicWrite creates a temp file alongside the original, writes the complete new file, then renames. For a 700MB WAV file, this requires 700MB of free disk space. If the disk is nearly full, the temp file write will fail partway through, and the deferred cleanup removes the partial temp file — no data loss, but the user gets an error.

**Why it happens:** WAV files can be gigabytes (24-bit, 96kHz stereo recordings). OGG files are typically much smaller (compressed), but large FLAC→OGG conversions at high quality can still be hundreds of MB.

**Consequences:** Write fails with "no space left on device." AtomicWrite correctly cleans up — no corruption. But the user can't save tags without freeing disk space.

**Prevention:**
1. **Pre-flight check:** Before starting the write, check available disk space. If free space < file size + some margin (10MB), return a clear error: "insufficient disk space for atomic write."
2. Log the file size at INFO level when writing large files (>100MB) so users understand why it takes time.
3. The existing `largeSizeThreshold` warning in `writeFlacTags` (500MB) should be applied to all format writers.
4. For WAV specifically, where we're rewriting a potentially multi-GB file: consider whether the tag data can be appended at the end without rewriting the audio data. If the `id3 ` chunk is placed after `data`, and the RIFF header size is updated, this is theoretically possible — but tricky to get right and incompatible with the current AtomicWrite pattern.

**Phase:** WAV writer, integration testing

---

### P13: OGG Multi-Stream Files (Chained and Multiplexed)

**Severity:** MEDIUM — silent data loss or crash on unusual files
**What goes wrong:** OGG supports two types of multiplexing: **chaining** (sequential streams, like concatenated songs in internet radio recordings) and **grouping** (interleaved streams, like video + audio). A chained OGG file has multiple logical bitstreams end-to-end, each with its own BOS/EOS pages. If our writer assumes a single logical bitstream, it will either only modify the first stream's tags (losing all subsequent streams) or crash when it encounters unexpected BOS pages.

**Why it happens:** Most `.ogg` music files contain a single Vorbis stream. But files from internet radio captures, or OGG files with embedded lyrics/subtitles, may have multiple streams.

**Consequences:** Chained streams after the first are silently dropped. File plays only the first few seconds/minutes. Data loss that the user may not notice immediately.

**Prevention:**
1. **Detect multi-stream files early:** Check for a second BOS page (after the first EOS). If found, either:
   - (a) Return an error: "multi-stream OGG files are not supported for tag editing"
   - (b) Only modify the first stream and pass through all subsequent streams byte-for-byte
2. Option (a) is safer for v1.2.1. Option (b) is more user-friendly but requires more careful implementation.
3. **Detect multiplexed streams:** If pages with different serial numbers appear interleaved, reject the file. YellowJacket is a music player — multiplexed OGG (video+audio) is out of scope.
4. Test with a chained OGG file to verify graceful handling.

**Phase:** OGG writer (validation at file open time)

---

### P14: Round-Trip Data Loss — Fields Present in File But Not in Our Model

**Severity:** MEDIUM — data loss for power users
**What goes wrong:** Our model has 8 editable fields + cover art. Vorbis Comments can contain arbitrary fields (LYRICS, COMMENT, PERFORMER, ORGANIZATION, REPLAYGAIN_*, etc.). When we rewrite the OGG file, if we rebuild the Vorbis Comment block from scratch using only our 8 fields, all other comment fields are lost. The same applies to WAV — an ID3v2 tag may have many frames beyond our 8 fields.

**Why it happens:** The simplest implementation is "build new comment block from scratch." This is what go-flac/flacvorbis does for FLAC — and it works for FLAC because the library preserves the comment block and we only replace specific fields.

**Consequences:** ReplayGain data lost (affects volume normalization in other players). Custom fields from other tools lost. Lyrics lost. Users who also use other tagging software will lose data.

**Prevention:**
1. **For OGG:** Parse the existing Vorbis Comment block. Replace/add only the fields we're changing. Preserve all other comment entries verbatim. This is the same approach already used in `replaceVorbisComment()` for FLAC — port that logic.
2. **For WAV (ID3v2):** The `bogem/id3v2` library already handles this — opening with `Parse: true` reads all existing frames, and writing only replaces/adds what we change.
3. **Cover art special case:** When setting cover art in OGG, remove existing `METADATA_BLOCK_PICTURE` entries but preserve all other fields. When clearing cover art, only remove `METADATA_BLOCK_PICTURE`.
4. Test: create a file with extra fields (e.g., REPLAYGAIN_TRACK_GAIN), edit one of our fields, verify the extra fields survive.

**Phase:** OGG writer, WAV writer

---

## Minor Pitfalls

Mistakes that cause edge-case bugs, poor UX, or unexpected behavior.

### P15: OGG Page Segmentation for Large Comment Headers

**Severity:** LOW-MEDIUM — fails for large cover art
**What goes wrong:** OGG pages have a max segment table of 255 entries, each representing up to 255 bytes, giving a max page data size of 65,025 bytes (~63.5KB). A comment header with embedded cover art can easily exceed this. The header packet must then span multiple pages, using the continued-packet mechanism (header type flag 0x01 on continuation pages). If the page segmentation code doesn't handle multi-page packets, writing large cover art will fail or produce corrupt files.

**Why it happens:** Most comment headers fit in a single page. The multi-page case only triggers with cover art or very long field values.

**Prevention:**
1. Implement proper packet-to-page segmentation: split packets into 255-byte segments, fill pages up to 255 segments each, set continuation flag on subsequent pages.
2. Test with a comment header that's exactly 65,025 bytes (fits in one page), 65,026 bytes (requires two pages), and ~200KB (requires multiple pages).
3. Consider implementing cover art writing as a second phase after text fields are working and tested.

**Phase:** OGG writer (advanced)

---

### P16: WAV Files >4GB (RF64 Format)

**Severity:** LOW — rare in music player context
**What goes wrong:** Standard WAV uses 32-bit chunk sizes, limiting files to ~4GB. WAV files larger than 4GB use the RF64 extension (magic `RF64` instead of `RIFF`, with a `ds64` chunk for 64-bit sizes). If we encounter an RF64 file and treat it as standard WAV, we'll misparse the chunk sizes and produce a corrupt file.

**Why it happens:** RF64 is common in professional audio (long multi-channel recordings). Most music collections won't have these, but a user with high-resolution recordings might.

**Consequences:** File corruption for files >4GB. Audio data shifted due to wrong chunk size calculation.

**Prevention:**
1. **Check for RF64 magic** (`RF64` at offset 0 instead of `RIFF`). If found, return an error: "RF64 WAV files are not supported for tag editing."
2. This is acceptable for v1.2.1 — RF64 support can be added later if users report needing it.
3. Also check: if the calculated file size would exceed 4GB after adding/expanding the ID3v2 chunk, warn that the file may be problematic.

**Phase:** WAV writer (validation at file open time)

---

### P17: Test Fixture Creation for OGG and WAV

**Severity:** LOW-MEDIUM — blocks test coverage
**What goes wrong:** The existing FLAC and MP3 tests create minimal valid files programmatically (see `makeMinimalFLAC` and `createTestMP3`). OGG Vorbis files are harder to create programmatically because you need valid identification, comment, and setup header packets. WAV files are simpler but still require correct RIFF structure.

**Why it happens:** OGG Vorbis requires three header packets where the setup header contains Vorbis codebook data. You can't easily generate a valid setup header from scratch without a Vorbis encoder.

**Prevention for OGG:**
1. **Embed a minimal OGG fixture as `//go:embed`** in the test file. Create it once with an encoder (e.g., `ffmpeg -f lavfi -i "sine=frequency=440:duration=0.1" -c:a libvorbis -q:a 0 minimal.ogg`). ~5KB file.
2. Copy this fixture to a temp directory in each test, then modify it.
3. Alternatively, use `jfreymuth/oggvorbis` (already an indirect dep) to understand the page structure and craft test files with known structure.

**Prevention for WAV:**
1. **Generate minimal WAV programmatically** — it's much simpler than OGG:
   - RIFF header (12 bytes): "RIFF" + size + "WAVE"
   - fmt chunk (24 bytes): "fmt " + 16 + PCM format data
   - data chunk (8+ bytes): "data" + size + silence samples
   This is similar to `makeMinimalFLAC` in complexity.
2. Generate fixtures with and without existing ID3v2 chunks to test both "add new" and "replace existing" paths.

**Phase:** Test infrastructure (before format writers)

---

### P18: Empty/Missing Tags vs Zero-Length Strings

**Severity:** LOW — cosmetic but confusing
**What goes wrong:** When a user clears a field (sets it to empty string), should we write an empty comment entry (`TITLE=`) or omit the field entirely? Different readers handle these differently. dhowden/tag returns empty string for both missing and empty fields, so the distinction is invisible on read. But other tools may show "unknown" for missing fields vs blank for empty fields.

**Why it happens:** The Vorbis Comment spec says nothing about whether empty values are allowed (they are — any UTF-8 string including empty is valid).

**Prevention:**
1. **Match FLAC behavior:** The existing `replaceVorbisComment` function writes the new value regardless of whether it's empty. An empty string results in `TITLE=`. This is fine.
2. For OGG, use the same approach: write `FIELD=value` for all fields in the diff map, whether value is empty or not.
3. For WAV ID3v2: an empty string text frame is valid. The bogem/id3v2 library handles this correctly.

**Phase:** All writers

---

### P19: Unicode in Vorbis Comment Field Values

**Severity:** LOW — most data is ASCII but international users need this
**What goes wrong:** Vorbis Comment values MUST be valid UTF-8. Go strings are inherently UTF-8, so this is mostly automatic. However, if the user pastes text from a non-UTF-8 source (e.g., Windows-1252), the bytes won't be valid UTF-8 and could cause downstream parsing issues.

**Prevention:**
1. Validate that all string values are valid UTF-8 before writing: `utf8.ValidString(value)`.
2. This is unlikely to be an issue since Wails serializes strings as JSON (which mandates UTF-8), but a defensive check is cheap.

**Phase:** All writers (validation layer)

---

### P20: Existing BWF (Broadcast Wave Format) Chunks in WAV

**Severity:** LOW — professional users may care
**What goes wrong:** Some WAV files contain `bext` (Broadcast Extension) chunks with professional metadata (origination date, time reference, loudness info). Our writer should not delete or corrupt these chunks.

**Prevention:**
1. **Preserve all unrecognized chunks verbatim.** When rewriting the WAV file, copy all chunks we don't modify byte-for-byte to the output.
2. Only modify the `id3 ` chunk (add, replace, or update). Leave `fmt `, `data`, `bext`, `cue `, `smpl`, and all other chunks untouched.
3. The full-rewrite approach: iterate chunks in the source file, copy each to the output, replacing `id3 ` with our new version (or appending it at the end if it didn't exist).

**Phase:** WAV writer

---

## Phase-Specific Warnings

| Phase Topic | Likely Pitfall | Severity | Mitigation |
|------------|---------------|----------|------------|
| OGG page infrastructure | P1 (CRC32), P2 (seq numbers) | CRITICAL | Port CRC from oggvorbis/crc.go; full-stream rewrite |
| OGG Vorbis comment writer | P4 (three headers), P7 (framing bit), P8 (prefix) | HIGH | Follow Vorbis I spec exactly; round-trip test |
| OGG cover art | P9 (base64 PICTURE), P15 (large pages) | MEDIUM-HIGH | METADATA_BLOCK_PICTURE format; multi-page support |
| OGG edge cases | P13 (multi-stream), P3 (granule positions) | MEDIUM | Detect and reject multi-stream; preserve granule positions |
| WAV writer core | P5 (ID3v2 vs RIFF INFO), P6 (RIFF size) | CRITICAL | Write ID3v2; careful size bookkeeping |
| WAV chunk handling | P10 (alignment), P16 (RF64), P20 (BWF) | MEDIUM | Pad to even; detect RF64; preserve all chunks |
| Integration | P11 (read/write asymmetry), P14 (data loss) | MEDIUM | Round-trip tests; preserve unknown fields |
| Large files | P12 (disk space) | MEDIUM | Pre-flight size check; AtomicWrite handles crash safety |
| Testing | P17 (fixtures) | LOW-MEDIUM | Embed OGG fixture; generate WAV programmatically |

## Implementation Order Recommendation

Based on pitfall severity and dependency chain:

1. **OGG page infrastructure** (P1, P2, P3) — must be correct before anything else works
2. **OGG comment writer** (P7, P8, P4, P14) — build on page infrastructure
3. **WAV writer** (P5, P6, P10, P20) — independent of OGG, can parallelize
4. **OGG cover art** (P9, P15) — can be deferred if text fields work
5. **Edge case handling** (P13, P16, P12) — validation and error reporting
6. **Round-trip testing** (P11, P17) — continuous throughout, but formalize at end

## Key Decision Points

### OGG: Full Rewrite vs Surgical Edit
**Recommendation: Full rewrite.** The approach of "read all pages → replace comment packet → rewrite all pages" is simpler, more correct, and matches the FLAC precedent. The performance cost is acceptable because OGG files are compressed (typically 3-10MB for a song). A surgical edit (only rewriting comment pages and adjusting subsequent pages) saves I/O but dramatically increases complexity for CRC, sequence numbers, and page boundary management.

### WAV: Where to Place ID3v2 Chunk
**Recommendation: After the `data` chunk.** This ensures naive parsers that stop reading after `data` still play the file. More sophisticated parsers (like dhowden/tag) scan the entire RIFF structure and will find the `id3 ` chunk wherever it is.

### Cover Art in OGG: v1.2.1 or Defer?
**Recommendation: Implement text fields first, add cover art as stretch goal.** Cover art in OGG (P9, P15) adds significant complexity (base64 encoding, multi-page packets, FLAC PICTURE structure). Text-only tag editing is valuable on its own. If cover art doesn't make v1.2.1, it's a clean addition in a follow-up.

## Sources

- OGG Framing Specification: https://xiph.org/ogg/doc/framing.html (HIGH confidence — canonical spec)
- RFC 3533 — The Ogg Encapsulation Format: https://www.rfc-editor.org/rfc/rfc3533 (HIGH confidence — IETF RFC)
- Vorbis I Comment Spec: https://xiph.org/vorbis/doc/v-comment.html (HIGH confidence — canonical spec)
- OGG Opus Mapping: https://wiki.xiph.org/OggOpus (HIGH confidence — Xiph wiki)
- jfreymuth/oggvorbis source (crc.go, ogg.go): https://github.com/jfreymuth/oggvorbis (HIGH confidence — direct code review)
- dhowden/tag source (ogg.go): https://github.com/dhowden/tag/blob/master/ogg.go (HIGH confidence — direct code review)
- RIFF tag reference: https://exiftool.org/TagNames/RIFF.html (MEDIUM confidence — ExifTool documentation)
- YellowJacket codebase analysis: tagwriter/, fileutil/, metadata/ packages (HIGH confidence — direct code review)
