# Technology Stack: Tag Editing

**Project:** YellowJacket v1.2 Tag Editing
**Researched:** 2026-03-16

## Recommended Stack

### MP3 Tag Writing — `bogem/id3v2/v2`

| Technology | Version | Purpose | Why |
|------------|---------|---------|-----|
| `github.com/bogem/id3v2/v2` | v2.1.4 | ID3v2.3/v2.4 read + write for MP3 | Only mature pure-Go ID3v2 writing library. 359 stars, 57 importers, 579 commits. Supports SetTitle/SetArtist/SetAlbum/SetGenre/SetYear, AddAttachedPicture (cover art embedding), AddTextFrame (track/disc numbers, composer via TRCK/TPOS/TCOM), and tag.Save(). |

**Key capabilities verified (HIGH confidence — pkg.go.dev docs):**
- `tag.SetArtist()`, `tag.SetTitle()`, `tag.SetAlbum()`, `tag.SetGenre()`, `tag.SetYear()` — direct setters
- `tag.AddTextFrame("TRCK", id3v2.EncodingUTF8, "5/12")` — track number
- `tag.AddTextFrame("TPOS", id3v2.EncodingUTF8, "1/2")` — disc number
- `tag.AddTextFrame("TCOM", id3v2.EncodingUTF8, "Bach")` — composer
- `tag.AddAttachedPicture(PictureFrame{...})` — cover art embedding with MIME type, picture type (front cover), and raw image bytes
- `tag.Save()` — writes modified tag back to file
- `tag.DeleteFrames(id)` — remove specific frame types (needed for replacing cover art)
- ID3v2.3 and v2.4 version support with `tag.SetVersion()`
- UTF-8 encoding default for v2.4, ISO-8859-1 for v2.3
- `id3v2.Open(path, Options{Parse: true})` — open existing file, parse all frames, then modify and save

**Integration with existing dhowden/tag:**
- dhowden/tag stays for READ operations (already integrated in `backend/metadata/tags.go`)
- bogem/id3v2 used ONLY for WRITE operations
- No conflict: dhowden/tag reads from `io.ReadSeeker`, bogem/id3v2 reads from file path and writes back
- Read flow unchanged: `dhowden/tag.ReadFrom()` → `TrackMetadata` struct
- Write flow new: `id3v2.Open()` → modify → `tag.Save()` → close

**Dependency footprint:** Only dependency is `golang.org/x/text` (already in go.mod). Pure Go, no CGo.

### FLAC Tag Writing — `go-flac/go-flac` + `go-flac/flacvorbis` + `go-flac/flacpicture`

| Technology | Version | Purpose | Why |
|------------|---------|---------|-----|
| `github.com/go-flac/go-flac/v2` | v2.x | FLAC metadata block manipulation (parse + save) | Purpose-built for FLAC metadata manipulation. Parses metadata blocks separately from audio frames. `f.Save()` writes back metadata blocks + raw audio frames without re-encoding. 44 stars, clean API. |
| `github.com/go-flac/flacvorbis/v2` | v2.x | Vorbis Comment read/write for FLAC metadata blocks | Companion to go-flac. Provides `ParseFromMetaDataBlock()`, `Add()`, `Marshal()` for Vorbis Comment manipulation. Has field constants (`FIELD_TITLE`, `FIELD_ARTIST`, etc.). |
| `github.com/go-flac/flacpicture` | latest | PICTURE metadata block manipulation for FLAC | Companion to go-flac. `NewFromImageData()` creates PICTURE blocks, `Marshal()` serializes for embedding. |

**Why go-flac over mewkiz/flac for WRITING:**
- `mewkiz/flac` is primarily a FLAC **codec** (encoder/decoder). Its `Encode()` API re-encodes audio data, which is unacceptably slow and potentially lossy for metadata-only edits.
- `go-flac/go-flac` is specifically designed for **metadata manipulation**. It stores audio frames as raw bytes and copies them verbatim on save — no re-encoding.
- `mewkiz/flac` stays as an indirect dependency (via beep) for FLAC **decoding** during playback and duration extraction. No conflict.

**Key capabilities verified (HIGH confidence — GitHub README + examples):**
- `flac.ParseFile(fileName)` — returns `File` with `Meta` (metadata blocks) and `Frames` (raw audio data)
- `flacvorbis.ParseFromMetaDataBlock(*meta)` — parse existing Vorbis Comment block
- `cmts.Add(flacvorbis.FIELD_TITLE, "New Title")` — add/modify comment fields
- `cmts.Marshal()` — serialize back to MetaDataBlock
- `f.Meta[idx] = &cmtsmeta` — replace metadata block in-place
- `f.Save(fileName)` — write modified file (metadata blocks + raw audio frames, no re-encoding)
- `flacpicture.NewFromImageData(PictureTypeFrontCover, "Front cover", imgData, "image/jpeg")` — create picture block
- `picture.Marshal()` → append to `f.Meta` — embed cover art

**FLAC tag writing approach — metadata block replacement:**
1. `flac.ParseFile(path)` — parses metadata blocks + stores audio frames as raw bytes
2. Find existing VorbisComment block in `f.Meta` slice, or create new via `flacvorbis.New()`
3. Modify/add comment fields via `cmts.Add()` (handles field replacement)
4. Marshal back: `f.Meta[idx] = &cmts.Marshal()`
5. For cover art: create via `flacpicture.NewFromImageData()`, append to `f.Meta`
6. `f.Save(tmpPath)` — writes "fLaC" + metadata blocks + raw audio frames to temp file
7. Atomic rename temp file over original

**Critical detail:** `go-flac/go-flac`'s `Save()` copies audio frames as raw bytes — no re-encoding. A metadata-only edit of a 50MB FLAC file takes ~50ms, not minutes.

### OGG Vorbis Tag Writing — Custom Implementation Required

| Technology | Version | Purpose | Why |
|------------|---------|---------|-----|
| Custom OGG page rewriter | n/a | OGG Vorbis Comment + Picture writing | No pure-Go OGG tag writing library exists. `jfreymuth/oggvorbis` is decode-only. OGG tag writing requires parsing OGG pages, modifying the Vorbis Comment header packet, and rewriting pages. |

**Why custom OGG writing is necessary:**
- `jfreymuth/oggvorbis` (existing indirect dep) is a **decoder only** — no write API
- No other pure-Go OGG Vorbis tag writer exists in the ecosystem
- OGG Vorbis comments are stored in the second header packet (comment header), which is an OGG page
- Modifying comments changes page sizes, requiring page-level rewriting

**OGG tag writing approach:**
1. Parse OGG pages to find the three Vorbis header packets (identification, comment, setup)
2. Decode existing Vorbis Comment from the comment header packet
3. Modify comment fields (same key=value format as FLAC Vorbis Comments)
4. Re-encode comment packet into new OGG pages
5. Copy identification and setup headers unchanged
6. Copy all audio data pages unchanged
7. Write to temp file, atomic rename

**Complexity assessment:** MEDIUM-HIGH. OGG page framing is well-documented but requires careful implementation. The Vorbis Comment format itself is simple (same as FLAC). The OGG page CRC and segment tables are the tricky parts.

**Cover art in OGG:** Stored as `METADATA_BLOCK_PICTURE` Vorbis Comment tag (base64-encoded FLAC Picture block). Same encoding as FLAC Picture but base64-wrapped in a comment field.

**Recommendation:** Implement OGG writing LAST. Start with MP3 and FLAC (libraries exist). OGG uses the same Vorbis Comment format as FLAC, so the comment serialization code is shared — only the OGG page framing is new work.

### Atomic File Writing — No New Dependency

| Technology | Version | Purpose | Why |
|------------|---------|---------|-----|
| `os.CreateTemp` + `os.Rename` (stdlib) | Go 1.25 | Write-to-temp-then-rename pattern | The stdlib approach is simpler and sufficient. `natefinch/atomic` is already an indirect dep but provides `WriteFile(filename, io.Reader)` which doesn't match our use case (we need to write to temp first, THEN rename). The stdlib pattern gives more control over temp file location (same directory as target for same-filesystem rename). |

**Pattern:**
```go
// Create temp file in same directory as target (ensures same filesystem for atomic rename)
dir := filepath.Dir(targetPath)
tmp, err := os.CreateTemp(dir, ".yj-tag-*.tmp")
// ... write tag data to tmp ...
tmp.Close()
// Atomic rename (POSIX guarantees atomicity for same-filesystem rename)
os.Rename(tmp.Name(), targetPath)
```

**Why NOT `natefinch/atomic`:** It's designed for `io.Reader` → file workflows. Our workflow is: read original → write modified to temp → rename. The stdlib `os.CreateTemp` + `os.Rename` is the right primitive. `natefinch/atomic` also uses `os.Rename` internally on Unix anyway.

**Why same-directory temp file matters:** `os.Rename` is only atomic when source and destination are on the same filesystem. Music files could be on any mount point. Creating the temp file in the same directory guarantees this.

## Alternatives Considered

| Category | Recommended | Alternative | Why Not |
|----------|-------------|-------------|---------|
| MP3 write | bogem/id3v2/v2 | dhowden/tag | dhowden/tag is read-only. No write API. Would require forking. |
| MP3 write | bogem/id3v2/v2 | go-id3 (mikkyang) | Dead project, archived, no v2 module support, last commit 2015 |
| FLAC write | go-flac/go-flac + flacvorbis | mewkiz/flac | mewkiz/flac is a codec (encoder/decoder); its Encode() re-encodes audio. go-flac is purpose-built for metadata manipulation — copies audio frames as raw bytes. |
| FLAC write | go-flac/go-flac + flacvorbis | Custom FLAC writer | go-flac handles the format correctly with proven Save(); reinventing would be fragile |
| OGG write | Custom | CGo (libvorbis) | Violates no-CGo constraint |
| OGG write | Custom | dhowden/tag fork | dhowden/tag OGG parsing is minimal, not designed for writing |
| Atomic write | stdlib os.CreateTemp+Rename | natefinch/atomic | Doesn't match our write pattern; stdlib is sufficient |
| Atomic write | stdlib os.CreateTemp+Rename | renameio | Unnecessary dep for a 5-line pattern |

## What NOT To Add

| Library | Why Avoid |
|---------|-----------|
| Any CGo-based tag library (taglib-go, etc.) | Violates pure-Go constraint from PROJECT.md |
| go-id3 (mikkyang/id3-go) | Archived, unmaintained since 2015, no module support |
| Any "universal tag writer" that wraps TagLib via CGo | Violates pure-Go constraint |
| natefinch/atomic as direct dep | Already indirect; stdlib pattern is more appropriate for this use case |
| goflac (CGo wrapper around libFLAC) | Violates pure-Go constraint |

## Existing Dependencies Leveraged (No Version Changes)

| Library | Current Use | New Use in Tag Editing |
|---------|------------|----------------------|
| `dhowden/tag` v0.0.0-20240417 | Tag reading during library scan | Unchanged — still used for all READ operations |
| `mewkiz/flac` v1.0.12 (indirect via beep) | FLAC audio decoding during playback + duration extraction | Unchanged — remains indirect for decoding only |
| `golang.org/x/image` v0.12.0 | Cover art thumbnail generation | Image validation before embedding (ensure valid JPEG/PNG) |
| `natefinch/atomic` v1.0.1 (indirect) | Not directly used | Remains indirect; not needed for our pattern |

## Installation

```bash
# New direct dependencies
go get github.com/bogem/id3v2/v2@v2.1.4
go get github.com/go-flac/go-flac/v2
go get github.com/go-flac/flacvorbis/v2
go get github.com/go-flac/flacpicture
```

## Format Coverage Matrix

| Format | Text Tags | Cover Art Embed | Library | Confidence |
|--------|-----------|-----------------|---------|------------|
| MP3 (ID3v2) | ✓ Full | ✓ APIC frame | bogem/id3v2/v2 | HIGH |
| FLAC | ✓ Full | ✓ Picture block | go-flac/go-flac + flacvorbis + flacpicture | HIGH |
| OGG Vorbis | ✓ Full | ✓ METADATA_BLOCK_PICTURE | Custom (built on Vorbis Comment format) | MEDIUM |
| WAV | ✗ Not supported | ✗ Not supported | n/a — WAV has no standard tag format | n/a |

**WAV exclusion rationale:** WAV files have no widely-adopted metadata standard. Some players use INFO chunks, some use ID3v2 headers prepended to WAV. The project already supports WAV playback but doesn't extract meaningful tags from WAV during scanning. Tag editing for WAV is out of scope.

## Vorbis Comment Field Mapping

Both FLAC and OGG use Vorbis Comments. Field names are standardized:

| YellowJacket Field | Vorbis Comment Key | ID3v2 Frame ID |
|--------------------|-------------------|----------------|
| Title | TITLE | TIT2 |
| Artist | ARTIST | TPE1 |
| Album | ALBUM | TALB |
| Album Artist | ALBUMARTIST | TPE2 |
| Genre | GENRE | TCON |
| Year | DATE | TDRC (v2.4) / TYER (v2.3) |
| Track Number | TRACKNUMBER | TRCK |
| Total Tracks | TRACKTOTAL | TRCK (as "N/Total") |
| Disc Number | DISCNUMBER | TPOS |
| Total Discs | DISCTOTAL | TPOS (as "N/Total") |
| Composer | COMPOSER | TCOM |
| Comment | COMMENT | COMM |
| Lyrics | LYRICS | USLT |

## Sources

- bogem/id3v2: https://pkg.go.dev/github.com/bogem/id3v2/v2 (HIGH confidence — official docs)
- bogem/id3v2 GitHub: https://github.com/n10v/id3v2 (HIGH confidence — 359 stars, v2.1.4 release Feb 2023)
- go-flac/go-flac GitHub: https://github.com/go-flac/go-flac (HIGH confidence — 44 stars, metadata manipulation library with Save())
- go-flac/flacvorbis GitHub: https://github.com/go-flac/flacvorbis (HIGH confidence — Vorbis Comment add/parse/marshal, v2 module)
- go-flac/flacpicture GitHub: https://github.com/go-flac/flacpicture (HIGH confidence — PICTURE block creation from image data)
- mewkiz/flac GitHub: https://github.com/mewkiz/flac (HIGH confidence — confirmed codec, not suitable for metadata-only writes)
- dhowden/tag GitHub: https://github.com/dhowden/tag (HIGH confidence — confirmed read-only, no write API)
- jfreymuth/oggvorbis GitHub: https://github.com/jfreymuth/oggvorbis (HIGH confidence — confirmed decode-only)
- natefinch/atomic GitHub: https://github.com/natefinch/atomic (HIGH confidence — confirmed API mismatch for our use case)
- Vorbis Comment spec: https://www.xiph.org/vorbis/doc/v-comment.html
- FLAC format spec: https://www.xiph.org/flac/format.html
- OGG framing spec: https://www.xiph.org/ogg/doc/framing.html
