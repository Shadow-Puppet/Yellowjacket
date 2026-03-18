---
phase: 16-tag-writing-database-sync
verified: 2026-03-17T15:01:08Z
status: passed
score: 5/5 must-haves verified
human_verification:
  - test: "Edit a track's metadata in the running app and verify all views update"
    expected: "Changed title/artist/album appear in track list, album view, now-playing bar without rescan"
    why_human: "Requires Wails runtime + full UI rendering; TrackMetadataChanged event can't be verified in isolation"
  - test: "Edit the currently-playing track and verify playback stops cleanly"
    expected: "Playback stops without crash/corruption, file writes succeed, player can resume another track"
    why_human: "Requires real audio hardware and player state management"
---

# Phase 16: Tag Writing & Database Sync Verification Report

**Phase Goal:** The backend can write metadata tags and cover art to MP3 and FLAC files, then synchronize all changes to the database and search index in a single atomic operation
**Verified:** 2026-03-17T15:01:08Z
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | A Go function can accept a track ID and a set of changed metadata fields, write those tags to an MP3 file (ID3v2), and the tags are readable back by the existing metadata reader — round-trip correctness verified by unit tests | ✓ VERIFIED | `WriteTrackTags` in pipeline.go dispatches to `writeMp3Tags` in mp3.go; 5 MP3 round-trip tests pass (text fields, cover art, clear art, partial update, atomic safety) using `metadata.ExtractTags` for readback |
| 2 | The same function works for FLAC files (Vorbis Comments) — including files with existing metadata blocks | ✓ VERIFIED | `writeFlacTags` in flac.go with 7 round-trip tests passing (text fields, cover art, clear art, partial update, StreamInfo preservation, comment replacement, atomic safety) |
| 3 | Cover art images (JPEG/PNG) can be embedded in both MP3 and FLAC files — the embedded image is readable back | ✓ VERIFIED | `TestWriteMp3Tags_CoverArt` and `TestWriteFlacTags_CoverArt` both embed a programmatically-generated 1×1 JPEG, read back with `metadata.ExtractTags`, and verify data + MIME type match. Clear tests also pass. |
| 4 | After a tag write, the database reflects the new metadata: artist/album/genre entities are created or relinked, orphaned entities cleaned up, FTS5 index updated — no rescan needed | ✓ VERIFIED | `syncDatabase` in dbsync.go runs entity relink + FTS5 delete/insert + orphan cleanup in a single transaction. 5 pipeline integration tests verify: recording updated, new artist_credit created, old orphans deleted, genre relink with multi-genre, FTS5 searchable with new values. `TestWriteTrackTags_DBSync` confirms full round-trip. |
| 5 | If the currently-playing track is being edited, playback is stopped before the file write begins | ✓ VERIFIED | `TestWriteTrackTags_PlayerSafety` uses mockPlayer to confirm `StopAndRelease()` is called when `CurrentFilePath()` matches the target file. Pipeline.go line 118: `if tw.player.CurrentFilePath() == audioFile.FilePath { tw.player.StopAndRelease() }` |

**Score:** 5/5 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `backend/tagwriter/tagwriter.go` | Package types, field constants, format detection, MIME detection | ✓ VERIFIED (108 lines) | TagChanges type, 10 field constants, DetectFormat, detectMIME, id3v2OriginalTagSize |
| `backend/tagwriter/mp3.go` | writeMp3Tags using id3v2 + AtomicWrite | ✓ VERIFIED (139 lines) | applyTextChanges, applyCoverArtChanges, copyAudioData, all 8 text fields + cover art |
| `backend/tagwriter/mp3_test.go` | Round-trip tests for MP3 tag writing | ✓ VERIFIED (248 lines) | 5 tests: TextFields, CoverArt, ClearCoverArt, PartialUpdate, AtomicSafety |
| `backend/tagwriter/flac.go` | writeFlacTags using go-flac + AtomicWrite | ✓ VERIFIED (186 lines) | applyFlacTextChanges, replaceVorbisComment, applyFlacCoverArt, 9 text fields + PICTURE blocks |
| `backend/tagwriter/flac_test.go` | Round-trip tests for FLAC tag writing | ✓ VERIFIED (467 lines) | 7 tests: TextFields, CoverArt, ClearCoverArt, PartialUpdate, PreservesStreamInfo, ReplaceComment, AtomicSafety |
| `backend/tagwriter/pipeline.go` | TagWriter struct with WriteTrackTags entry point | ✓ VERIFIED (175 lines) | PlayerStopper/PipelineLocker interfaces, NewTagWriter, SetContext, WriteTrackTags with 7-step pipeline |
| `backend/tagwriter/dbsync.go` | DB sync transaction: entity relink, FTS5, orphan cleanup | ✓ VERIFIED (394 lines) | syncDatabase with BeginTx, artist/album/genre relink, FTS5 delete+insert, orphan cleanup, SAFETY comments on all hand-crafted SQL |
| `backend/tagwriter/pipeline_test.go` | Integration tests for write pipeline | ✓ VERIFIED (480 lines) | 5 tests: PlayerSafety, ScanMutex, OrphanCleanup, GenreRelink, DBSync — all using in-memory test DB |
| `backend/events/events.go` | TrackMetadataChanged event constant | ✓ VERIFIED | Line 73: `TrackMetadataChanged = "TrackMetadataChanged"` |
| `frontend/src/events.ts` | Auto-generated TrackMetadataChanged | ✓ VERIFIED | Line 52: `TrackMetadataChanged: "TrackMetadataChanged"` |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `mp3.go` | `fileutil/atomicwrite.go` | `fileutil.AtomicWrite` call | ✓ WIRED | mp3.go:36 — `return fileutil.AtomicWrite(logger, filePath, func(tmp *os.File) error {...})` |
| `mp3.go` | `github.com/bogem/id3v2/v2` | `id3v2.Open` + `tag.WriteTo` | ✓ WIRED | mp3.go:10,26,38 — imports, opens, writes to temp file |
| `flac.go` | `fileutil/atomicwrite.go` | `fileutil.AtomicWrite` call | ✓ WIRED | flac.go:77 — `return fileutil.AtomicWrite(logger, filePath, func(tmp *os.File) error {...})` |
| `flac.go` | `go-flac/go-flac/v2` | `flac.ParseFile` + `f.WriteTo` | ✓ WIRED | flac.go:12,31,78 — imports, parses, writes to AtomicWrite callback |
| `pipeline.go` | `player.go` | `PlayerStopper` interface (CurrentFilePath + StopAndRelease) | ✓ WIRED | pipeline.go:118,121 — checks path match, calls StopAndRelease |
| `pipeline.go` | `library.go` | `PipelineLocker` (AcquirePipelineLock/ReleasePipelineLock) | ✓ WIRED | pipeline.go:114-115 — acquires lock, defers release |
| `dbsync.go` | `database` | `BeginTx` + `WithTx` for entity relink + FTS5 + orphans | ✓ WIRED | dbsync.go:35-42 — begins tx, creates txq, uses throughout |
| `pipeline.go` | `events.go` | `EventsEmit(TrackMetadataChanged)` | ✓ WIRED | pipeline.go:158 — `wailsruntime.EventsEmit(tw.ctx, events.TrackMetadataChanged, ...)` |
| `app.go` | `pipeline.go` | `tagwriter.NewTagWriter` + FEBindings | ✓ WIRED | app.go:121-126 — creates TagWriter, line 135 adds to FEBindings |
| `library.go` | `pipeline.go` | `pipelineMu` wraps scanInternal | ✓ WIRED | library.go:205-206 — `l.pipelineMu.Lock(); defer l.pipelineMu.Unlock()` in scanInternal |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|-----------|-------------|--------|----------|
| WRITE-01 | 16-01 | Write metadata tags to MP3 files via ID3v2 (title, artist, album, genre, year, track#, disc#, composer) | ✓ SATISFIED | `writeMp3Tags` in mp3.go handles all 8 text fields via `applyTextChanges`; `TestWriteMp3Tags_TextFields` verifies round-trip |
| WRITE-02 | 16-02 | Write metadata tags to FLAC files via Vorbis Comments | ✓ SATISFIED | `writeFlacTags` in flac.go handles all 9 fields (including album_artist); `TestWriteFlacTags_TextFields` verifies round-trip |
| WRITE-04 | 16-01, 16-02 | Embed cover art image (JPEG/PNG) in MP3 and FLAC files | ✓ SATISFIED | MP3: `applyCoverArtChanges` with APIC frame; FLAC: `applyFlacCoverArt` with PICTURE block. Both tested with round-trip readback. |
| WRITE-06 | 16-03 | Currently-playing file is stopped before writing (player safety) | ✓ SATISFIED | pipeline.go:118-121 checks `CurrentFilePath()` and calls `StopAndRelease()`; `TestWriteTrackTags_PlayerSafety` confirms |
| SYNC-01 | 16-03 | After tag write, update DB entities inline (upsert-and-relink for artist, album, genre) | ✓ SATISFIED | dbsync.go handles artist credit upsert+relink (§1), album/release_group upsert+relink (§2), genre delete+re-link (§3); `TestWriteTrackTags_DBSync` verifies |
| SYNC-02 | 16-03 | After tag write, update FTS5 search index for affected tracks | ✓ SATISFIED | dbsync.go:291-307 — FTS5 DELETE + INSERT within the same transaction; `TestWriteTrackTags_DBSync` queries FTS5 to verify "New Title" is searchable |
| SYNC-03 | 16-03 | Orphaned entities (artists, albums, genres no longer referenced) cleaned up | ✓ SATISFIED | dbsync.go §7: artist_credit orphan (CountArtistCreditReferences → DeleteArtistCredit), release_group orphan (CountReleaseGroupRecordings → DeleteReleaseGroup), genre orphan (global DELETE WHERE NOT IN); `TestWriteTrackTags_OrphanCleanup` and `TestWriteTrackTags_GenreRelink` verify |
| SYNC-04 | 16-03 | Scan pipeline paused during tag writes to prevent race conditions | ✓ SATISFIED | `pipelineMu sync.Mutex` on Library (library.go:117); write acquires via `AcquirePipelineLock` (pipeline.go:114); scan acquires at start of `scanInternal` (library.go:205); `TestWriteTrackTags_ScanMutex` confirms lock acquisition |

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| `dbsync.go` | 201-206 | Cover art DB sync skipped (comment says "no-op in the DB sync") | ⚠️ Warning | File-level embed works; DB cover_art table and thumbnails not updated after write. Next rescan would reconcile. Acceptable for Phase 16 scope — the requirement (WRITE-04) is about file embedding, which is satisfied. |

### Human Verification Required

### 1. Full UI Round-Trip

**Test:** Edit a track's metadata via the app and verify all views update
**Expected:** Changed title/artist/album appear in track list, album view, now-playing bar without rescan
**Why human:** Requires Wails runtime + full UI rendering; TrackMetadataChanged event propagation can't be verified in unit tests

### 2. Player Safety Under Real Playback

**Test:** Start playing a track, then edit its metadata
**Expected:** Playback stops cleanly without crash/corruption, file writes succeed, player can resume another track
**Why human:** Requires real audio hardware and player state; mock tests verify interface calls but not real audio stream behavior

### Gaps Summary

No gaps blocking goal achievement. All 5 success criteria from ROADMAP.md are verified. All 8 requirements (WRITE-01, WRITE-02, WRITE-04, WRITE-06, SYNC-01, SYNC-02, SYNC-03, SYNC-04) are satisfied with code evidence and passing tests.

**Minor note:** The cover art DB sync (updating `cover_art` table, `release_group.cover_art_id`, and thumbnail regeneration after a write) is deferred — the file-level embedding works for both MP3 and FLAC, but the database `cover_art` record is not updated post-write. This is acceptable within the phase goal since WRITE-04 specifically requires file embedding. The DB-level cover art sync can be added when the UI sends cover art data (Phase 17).

**Test results:** All 17 tests pass (5 MP3, 7 FLAC, 5 pipeline integration). Full backend compiles cleanly (`go build ./backend/...`).

---

_Verified: 2026-03-17T15:01:08Z_
_Verifier: Claude (gsd-verifier)_
