# Project Research Summary

**Project:** YellowJacket v1.2 Tag Editing
**Domain:** Audio metadata editing in a desktop music player (Go/Wails)
**Researched:** 2026-03-16
**Confidence:** HIGH

## Executive Summary

Tag editing for YellowJacket is a cross-cutting feature that touches file I/O (three audio formats), a normalized relational database, an FTS5 search index, a cover art cache pipeline, and the frontend state — all from a single user action. Mature desktop music players (foobar2000, MusicBee, Mp3tag, Kid3) converge on a consistent pattern: modal dialog editing with atomic file writes, inline DB updates (no rescan), and batch editing with three-state field semantics (keep/set/clear). The existing codebase has substantial scaffolding already in place — the `track-details` component has an edit mode UI stub with a no-op save handler, multi-select works in the track list, and the cover art pipeline is fully operational.

The recommended approach uses **three external libraries** for tag writing — `bogem/id3v2/v2` for MP3, `go-flac/go-flac` + `go-flac/flacvorbis` + `go-flac/flacpicture` for FLAC — plus a **custom OGG page rewriter** (deferred to last, since no pure-Go OGG tag writing library exists). A new `backend/tageditor/` package orchestrates the full pipeline: validate → write tags to temp file → atomic rename → update DB entities in a single transaction → update FTS5 → emit event → frontend refreshes. This keeps the existing `library`, `metadata`, and `database` packages unchanged.

The dominant risks are: (1) **FLAC files require full rewrite** for tag changes (no in-place edit), making atomic write-to-temp-then-rename mandatory; (2) the **FTS5 contentless index cannot delete rows**, requiring a schema migration to `contentless_delete=1` before any tag writing code ships; (3) the **normalized schema shares entities** (artists, albums, genres) across tracks, so editing one track must create new entity rows and repoint references rather than mutating shared rows in-place; and (4) a **race condition** between tag editing and library scanning requires mutual exclusion. All four risks have well-understood mitigations documented in the research.

## Key Findings

### Recommended Stack

Pure-Go tag writing is well-supported for MP3 and FLAC via established libraries. OGG Vorbis tag writing requires custom implementation but shares the Vorbis Comment format with FLAC, so serialization code is reusable. No new dependencies beyond `golang.org/x/text` (already in go.mod) are pulled in transitively. The existing `dhowden/tag` library stays for all READ operations — no conflict with the new write libraries.

**Core technologies:**
- **`bogem/id3v2/v2`** (v2.1.4): MP3 ID3v2 read+write — 359 stars, 57 importers, handles encoding (UTF-8/UTF-16) correctly, supports picture frames. HIGH confidence.
- **`go-flac/go-flac/v2` + `go-flac/flacvorbis/v2` + `go-flac/flacpicture`**: FLAC metadata manipulation — copies audio frames as raw bytes (no re-encoding), ~50ms for metadata-only edits on large files. HIGH confidence.
- **Custom OGG page rewriter**: No pure-Go OGG tag writer exists. OGG page framing (CRC, segment tables) is the only new work — Vorbis Comment serialization is shared with FLAC. MEDIUM confidence.
- **stdlib `os.CreateTemp` + `os.Rename`**: Atomic file write pattern — temp file in same directory guarantees same-filesystem rename. No external dependency needed.

**Critical version requirements:**
- SQLite ≥ 3.43.0 for `contentless_delete=1` FTS5 support (bundled `modernc.org/sqlite` provides 3.45+, so already satisfied)

### Expected Features

**Must have (table stakes):**
- Single track tag editing (title, artist, album, genre, year, track#, disc#, composer)
- Write tags to MP3 (ID3v2) and FLAC (Vorbis Comments)
- Write-to-temp-then-rename corruption safety
- Inline DB + FTS5 update after tag write (no rescan)
- Batch editing with three-state field model (keep/set/clear)
- Cover art set/replace from image file
- Save confirmation and error feedback

**Should have (differentiators):**
- Progress indicator for batch operations (20+ files)
- Auto-number tracks in batch edit
- Cover art remove (strip embedded art)
- Dirty indicator / unsaved changes warning
- Album artist, comment, lyrics field editing (low-effort additions)
- Total tracks / total discs fields

**Defer (v2+):**
- MusicBrainz auto-tagging (separate milestone already in PROJECT.md)
- Undo/backup system for tag edits
- Cover art paste from clipboard
- Inline editing in track list columns (fragile UX, complex)
- Raw tag frame editing, custom fields, filename renaming
- OGG Vorbis tag writing (implement last due to custom work required)

### Architecture Approach

Tag editing is implemented as a new `backend/tageditor/` package that orchestrates the full write pipeline, keeping existing packages focused on their current responsibilities. The service exposes `EditTrack()`, `EditTracks()`, and `SetCoverArt()` as Wails bindings. It uses pointer fields (`*string`, `*int`) to distinguish "no change" (nil) from "set to empty" — mapping directly to the three-state UI model for batch editing.

**Major components:**
1. **`backend/tageditor/tageditor.go`** — Service orchestrator: validates input, coordinates file write → DB update → FTS5 → events
2. **`backend/tageditor/writer.go`** — Format-specific tag writing (MP3 via bogem/id3v2, FLAC via go-flac, OGG via custom)
3. **`backend/events/events.go`** (modified) — New `TagsUpdated` and `TagEditFailed` event constants
4. **`frontend/src/components/track-details/`** (modified) — Wire existing edit UI stub to backend, add batch edit variant
5. **`frontend/src/store/library-store.ts`** (modified) — Listen for `TagsUpdated` event, full re-fetch on change

**Key patterns:**
- Write-to-temp-then-rename (temp in same directory as target)
- Upsert-and-relink for shared entities (never mutate shared artist/album/genre rows)
- Pointer fields for optional partial updates
- Lazy orphan cleanup (defer to next rescan)

### Critical Pitfalls

1. **FTS5 contentless index can't delete rows (P3)** — Migrate to `contentless_delete=1` before writing any tag edit code. Without this, search returns stale results after every edit. This is a prerequisite schema migration.
2. **FLAC requires full file rewrite (P1)** — No in-place edit possible. Write-to-temp-then-rename is mandatory. Temp file must be in the same directory for atomic rename. Verify written file before replacing original.
3. **Shared entity fan-out (P4)** — Editing one track's artist must NOT modify the shared `artist_credit` row (would silently change 200 other tracks). Always create new entity rows and repoint the edited track's foreign keys.
4. **Currently-playing file lock (P2)** — On Windows, `os.Rename()` fails if the player holds an open file handle. Must check player state and stop playback before editing the current track.
5. **Scan/edit race condition (P5)** — A library scan running during tag editing can overwrite changes. Pause scan during edits using the existing `PauseScan()`/`ResumeScan()` mechanism.

## Implications for Roadmap

Based on research, suggested phase structure:

### Phase 1: Schema Migration & Write Safety Foundation

**Rationale:** The FTS5 migration (P3) is a hard prerequisite — without `contentless_delete=1`, tag edits degrade search quality. The atomic file write mechanism (P1, P6) is the foundation all tag writing depends on. These are small, testable, independent pieces that de-risk everything downstream.
**Delivers:** FTS5 schema migration; atomic write-to-temp-then-rename utility; temp file cleanup on startup
**Addresses:** Write-to-temp-then-rename (table stakes), FTS5 inline update capability
**Avoids:** P3 (stale search), P1 (file corruption), P6 (cross-filesystem rename failure)

### Phase 2: Tag Writing Library Integration

**Rationale:** With the write safety layer in place, integrate the format-specific tag writing libraries. MP3 first (most common format, best library), then FLAC. This phase is pure backend — no UI changes yet. Unit tests with real audio files validate round-trip correctness.
**Delivers:** `backend/tageditor/writer.go` with MP3 + FLAC tag writing; encoding handling (P7); cover art embedding capability
**Uses:** `bogem/id3v2/v2`, `go-flac/go-flac/v2` + `go-flac/flacvorbis/v2` + `go-flac/flacpicture`
**Avoids:** P7 (encoding mismatch), P8 (cover art format issues), P12 (dhowden/tag is read-only)

### Phase 3: Single Track Edit Pipeline

**Rationale:** Wire the full pipeline end-to-end for a single track: backend service → file write → DB entity update → FTS5 re-index → event emission → frontend refresh. This is the core loop that all other features build on. Includes the shared entity upsert-and-relink pattern (P4) and genre dual-representation sync (P10).
**Delivers:** `backend/tageditor/tageditor.go` service; `EditTrack()` Wails binding; wired `track-details` save handler; `TagsUpdated` event; library store refresh
**Implements:** Tageditor service, DB update logic, event system, frontend integration
**Avoids:** P4 (shared entity mutation), P5 (scan race), P10 (genre mismatch), P11 (partial failure), P17 (stale frontend cache)

### Phase 4: Cover Art Editing

**Rationale:** Cover art embedding builds on Phase 2's writer and Phase 3's pipeline but adds image validation, the cover art cache pipeline integration, and file picker UX. Separated because cover art has its own pitfalls (P8, P13) and is independently testable.
**Delivers:** `SetCoverArt()` binding; image resize/validation before embed; cover art cache invalidation and thumbnail regeneration; cover art remove capability
**Avoids:** P8 (oversized images, format issues), P13 (stale cached thumbnails)

### Phase 5: Batch Editing

**Rationale:** Batch editing is the highest-complexity UI feature (three-state field model, mixed-value indicators, progress tracking). It depends on the single-track pipeline being solid. The backend is straightforward (loop over `EditTrack()`), but the frontend UX is where the complexity lives.
**Delivers:** Batch edit dialog with three-state fields; `EditTracks()` binding; progress indicator; auto-number tracks; confirmation dialog for destructive batch operations
**Addresses:** Batch editing (table stakes), progress indicator (differentiator), auto-number (differentiator)
**Avoids:** P9 (orphan entity accumulation — run cleanup after batch), P14 (no undo — confirmation dialog)

### Phase 6: OGG Vorbis Tag Writing (Stretch)

**Rationale:** OGG tag writing requires a custom OGG page rewriter — MEDIUM-HIGH complexity with no library support. The Vorbis Comment serialization is shared with FLAC (Phase 2), so only the OGG page framing is new. This can ship after the core MP3/FLAC editing is stable.
**Delivers:** Custom OGG page rewriter; OGG Vorbis tag writing support; full format coverage (MP3 + FLAC + OGG)
**Avoids:** Scope creep — if OGG proves too complex, MP3 + FLAC cover the vast majority of user libraries

### Phase Ordering Rationale

- **Schema migration first** because FTS5 `contentless_delete=1` is a hard prerequisite that must be in place before any DB update code is written for tag editing.
- **Write safety before tag libraries** because the atomic write mechanism is tested independently of any format-specific code.
- **MP3 before FLAC before OGG** because library quality/maturity decreases in that order, and MP3 covers the largest user base.
- **Single track before batch** because batch editing is N × single with UI complexity on top — the underlying pipeline must be solid.
- **Cover art as a separate phase** because it has independent pitfalls (image validation, cache invalidation) and is testable in isolation.
- **OGG last** because it requires custom implementation and MP3 + FLAC cover the majority of use cases.

### Research Flags

Phases likely needing deeper research during planning:
- **Phase 2 (Tag Writing):** `go-flac` libraries have smaller communities (44 stars) — verify FLAC write round-trip with edge cases (large files, existing padding blocks, multiple PICTURE blocks) during implementation.
- **Phase 6 (OGG Writing):** Custom OGG page rewriter needs specification-level research (OGG framing RFC). Consider prototyping before committing to scope.

Phases with standard patterns (skip research-phase):
- **Phase 1 (Schema Migration):** Well-documented SQLite FTS5 migration. `contentless_delete=1` is a one-line schema change.
- **Phase 3 (Single Track Edit):** The architecture is fully designed — pointer fields, upsert-and-relink, event emission are all standard Go/Wails patterns.
- **Phase 5 (Batch Editing):** The three-state field model is well-understood from foobar2000/MusicBee analysis. Frontend-heavy but no novel backend work.

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | MP3 library (bogem/id3v2) verified via pkg.go.dev docs with 359 stars/57 importers. FLAC libraries verified via GitHub READMEs. OGG is the only gap (custom work). |
| Features | HIGH | Cross-referenced 5 desktop music players + Hydrogenaudio tag standards + existing codebase analysis. Table stakes are unambiguous. |
| Architecture | HIGH | Based on full codebase analysis — every integration point verified against actual source files (library.go, search.go, tags.go, track-details.ts, player.go). |
| Pitfalls | HIGH | All critical pitfalls derived from format specifications (FLAC, ID3v2, OGG, FTS5) and codebase analysis (shared entities, file locking, scan race). Mitigations are concrete. |

**Overall confidence:** HIGH

### Gaps to Address

- **OGG Vorbis tag writing:** No pure-Go library exists. Custom implementation complexity is estimated at MEDIUM-HIGH but not prototyped. Validate feasibility during Phase 6 planning — consider whether OGG support is worth the custom code, or whether to accept MP3+FLAC-only for v1.2.
- **`go-flac` edge cases:** The go-flac library has 44 stars and a small community. Round-trip testing with edge-case FLAC files (files with existing PADDING blocks, multiple PICTURE blocks, unusual metadata block orders) should be done early in Phase 2 to surface any library bugs.
- **Windows file locking behavior:** The currently-playing-file lock (P2) is well-understood conceptually but the exact interaction between Go's `os.Open`, beep's streamer, and Windows mandatory locking needs validation on a Windows build.
- **Album artist storage:** FEATURES.md notes album artist editing is low-hanging fruit, but ARCHITECTURE.md flags that album artist isn't currently stored as a separate entity. Schema implications should be resolved during Phase 3 planning.

## Sources

### Primary (HIGH confidence)
- `bogem/id3v2` (n10v/id3v2): https://pkg.go.dev/github.com/bogem/id3v2/v2 — API docs, v2.1.4, write support verified
- `go-flac/go-flac`: https://github.com/go-flac/go-flac — metadata manipulation, Save() copies audio frames as raw bytes
- `go-flac/flacvorbis`: https://github.com/go-flac/flacvorbis — Vorbis Comment add/parse/marshal
- `go-flac/flacpicture`: https://github.com/go-flac/flacpicture — PICTURE block creation from image data
- SQLite FTS5 docs: https://www.sqlite.org/fts5.html — contentless tables, contentless_delete=1
- FLAC format spec: https://www.xiph.org/flac/format.html — metadata block structure
- Vorbis Comment spec: https://www.xiph.org/vorbis/doc/v-comment.html — field format
- OGG framing spec: https://www.xiph.org/ogg/doc/framing.html — page structure
- Hydrogenaudio Tag Mapping: https://wiki.hydrogenaud.io/index.php/Tag_Mapping — field name standards
- YellowJacket codebase: library.go, search.go, tags.go, player.go, track-details.ts, schema files — architecture analysis

### Secondary (MEDIUM confidence)
- `dhowden/tag`: https://github.com/dhowden/tag — confirmed read-only, no write API
- `mewkiz/flac`: https://github.com/mewkiz/flac — confirmed codec (encoder/decoder), unsuitable for metadata-only writes
- `jfreymuth/oggvorbis`: https://github.com/jfreymuth/oggvorbis — confirmed decode-only
- MusicBee, foobar2000, Kid3, Mp3tag, Picard — feature pattern analysis

### Tertiary (LOW confidence)
- OGG Vorbis custom writer feasibility — estimated MEDIUM-HIGH complexity based on spec analysis, not prototyped

---
*Research completed: 2026-03-16*
*Ready for roadmap: yes*
