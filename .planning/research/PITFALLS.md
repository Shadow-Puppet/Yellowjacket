# Domain Pitfalls: Tag Editing

**Domain:** Adding tag editing to an existing music player with normalized DB
**Researched:** 2026-03-16
**Confidence:** HIGH (based on codebase analysis + format specifications + SQLite FTS5 docs)

## Critical Pitfalls

Mistakes that cause data loss, file corruption, or require architectural rework.

### P1: FLAC Tag Writes Require Full File Rewrite

**What goes wrong:** FLAC stores Vorbis Comments in a METADATA_BLOCK after the STREAMINFO block. Unlike MP3 (which has padding in ID3v2 headers), FLAC metadata blocks are tightly packed with no padding by default. Changing a tag that increases the metadata size requires rewriting the entire file — moving every audio frame forward. A crash or power loss during this rewrite corrupts the file irrecoverably.

**Why it happens:** FLAC spec doesn't mandate padding blocks. Most FLAC files in the wild have zero padding. Even if padding exists, adding cover art (which can be 100KB+) almost always exceeds it.

**Consequences:** Corrupted FLAC files that won't play. Audio data intact on disk but offset table is wrong, so decoders can't find frames.

**Prevention:**
1. **Write-to-temp-then-rename (mandatory for all formats).** Write modified file to a temp file in the same directory (same filesystem), then `os.Rename()` atomically. This is already listed in PROJECT.md as a target feature.
2. For FLAC specifically: read entire file → write new metadata blocks → copy audio frames → rename. There is no in-place shortcut.
3. Verify the written file can be opened and has correct duration before replacing the original.
4. Consider adding a PADDING metadata block after writing (e.g. 8KB) so small future edits can be done in-place. This is what tools like `metaflac` do.

**Detection:** File size changes unexpectedly; beep decoder fails to open the file after write; duration changes after write (offset corruption).

**Phase:** File write layer (earliest phase)

---

### P2: Currently-Playing File Cannot Be Written On Windows (And Shouldn't On Any Platform)

**What goes wrong:** The player holds an `os.File` handle on the currently playing track (`p.currentFile` in `player.go:461`). On Windows, the OS enforces mandatory file locking — `os.Rename()` will fail with "The process cannot access the file because it is being used by another process." On Linux, the rename succeeds but the player continues reading the old inode (now unlinked), which works until something closes and reopens the path.

**Why it happens:** The player opens files with `os.Open()` and holds them open for the duration of playback (streaming audio data). The beep library reads from this file handle continuously.

**Consequences:** On Windows: tag write fails silently or with confusing error. On Linux: tag write succeeds but the player sees stale data, and if the user seeks, the streamer may read garbage from the new file at old offsets.

**Prevention:**
1. **Check if the target file is currently playing before writing.** Compare `player.currentTrackPath` against the edit target.
2. If the file IS playing: stop playback, close the file handle, perform the write, then reload and seek to the previous position. This creates a brief audio glitch but is the only safe approach.
3. For batch edits that include the current track: edit all other files first, handle the playing file last with the stop-write-reload dance.
4. Alternative (simpler): refuse to edit the currently playing file and show a user-facing message. Less ideal UX but avoids complexity.

**Detection:** `os.Rename()` returns error on Windows. On Linux, no error but playback becomes corrupted after seek.

**Phase:** File write layer + player integration

---

### P3: FTS5 Contentless Table Cannot UPDATE or DELETE Individual Rows

**What goes wrong:** The current `search_index` is a contentless FTS5 table (`content=''`). The existing `DeleteSearchIndex()` method is literally a no-op (see `search.go:120-127`). After editing a track's title from "Love Song" to "Heart Song", searching for "Love Song" still returns the track because the old FTS5 entry cannot be removed. The stale entry points to a valid rowid, and the JOIN against `track_metadata` will return the row (now with different data), so the user sees a search result that doesn't match their query.

**Why it happens:** Contentless FTS5 (`content=''`) stores only the index, not the original text. Without the original text, FTS5 can't compute what tokens to remove from the index. The current design relies on full rebuilds during rescan, which is fine for the read-only case but breaks for incremental edits.

**Consequences:** Search returns false positives after tag edits. The more edits the user makes, the worse search quality gets — until the next full rescan rebuilds the index.

**Prevention — Two Options:**

**Option A: Migrate to `contentless_delete=1` (Recommended)**
SQLite 3.43.0+ supports `contentless_delete=1` which enables DELETE and INSERT OR REPLACE. This requires a schema migration (drop + recreate the FTS5 table). The `modernc.org/sqlite` driver bundles SQLite 3.45+, so this is available. After migration, tag edit can do: DELETE the old row, INSERT the new row. This is the `DELETE + INSERT` pattern already noted in the milestone context.

**Option B: Rebuild the entire index after each edit session**
Call `RebuildSearchIndex()` after completing all tag writes. This is expensive (reads all tracks) but correct. Could be batched — rebuild once after a batch edit, not per-track.

**Recommendation:** Option A. The migration is straightforward and makes individual updates O(1) instead of O(n). The existing `RebuildSearchIndex()` becomes the migration step.

**Detection:** Search for old tag values — if they return results with the new values, the index is stale.

**Phase:** Schema migration (do first, before any tag write code)

---

### P4: Shared Entity Fan-Out — Editing Artist on One Track Affects Zero or Fifty Others

**What goes wrong:** The normalized schema shares entities across tracks. An `artist_credit` row with text "The Beatles" may be referenced by 200 recordings via `recordings.artist_credit_id`. If the user edits the artist field on one track from "The Beatles" to "Beatles, The", the system must decide: (a) update the shared `artist_credit` row (changing all 200 tracks), (b) create a new `artist_credit` and repoint only this track's recording, or (c) something else.

**Why it happens:** The MusicBrainz-inspired schema (`artists` → `artist_credit` → `recordings`) is designed for read-heavy workloads where entities are shared. Tag editing breaks this assumption by making per-track changes that may or may not be intended as global changes.

**Consequences:**
- If you update the shared row: user edits one track, 199 other tracks silently change. Terrifying.
- If you create new rows: orphaned entities accumulate (old `artist_credit` row with only 199 refs, then 198, etc.). The artist browse view shows "The Beatles" AND "Beatles, The" as separate entries.
- If you try to be smart about it: complex merge/split logic that's hard to get right.

**Prevention:**
1. **Tag editing always creates new entity rows for the edited track.** Create a new `recording`, new `artist_credit` (if changed), new `release_group_recordings` link, new `genre_recordings` links. Point the `audio_file.recording_id` at the new recording. This is the safest approach and matches what the scan pipeline already does (it always creates new recordings).
2. **Orphan cleanup after edit.** After repointing the audio_file, check if the old recording is still referenced by any audio_file. If not, delete it (and cascade to its genre links, release_group links). Same for artist_credit, artists, genres, release_groups.
3. **Never mutate shared entities in-place** during single-track or batch-within-same-album editing. The only exception is intentional "rename this artist across all tracks" which should be a separate, explicit feature (not part of v1.2).

**Detection:** After editing one track's artist, check if other tracks in the same album now show the wrong artist.

**Phase:** Database update layer (core architecture decision — must be settled before writing any DB update code)

---

### P5: Race Condition — Scan Runs While Tags Are Being Written

**What goes wrong:** User starts editing tags. While the edit is in progress (writing files, updating DB), a library scan starts (either from the scan queue, soft scan on launch, or user-initiated). The scan reads the file's tags (which may be half-written or already-written-but-DB-not-yet-updated), creates new entity rows, and overwrites the DB state that the tag editor just carefully set up.

**Why it happens:** The scan pipeline (`scanInternal`) and tag editing are independent operations. The scan loads existing files from DB, walks the filesystem, extracts metadata, and writes to DB. If a file's on-disk tags differ from the DB (because the edit just wrote new tags), the scan treats it as needing an update and overwrites the recording.

**Consequences:** Tag edits silently reverted. Or worse: the scan creates duplicate recordings (one from the edit, one from the scan) because the scan's entity cache doesn't know about the edit's newly-created entities.

**Prevention:**
1. **Mutual exclusion between tag editing and scanning.** While tag writes are in progress, block scan start (or vice versa). The existing `l.mu` mutex protects scan state; extend it to cover "edit in progress" state.
2. **Simpler: Use the existing scan queue coordinator.** Tag edits happen on the main goroutine (via Wails binding). Scans run in background goroutines. Since SQLite has `SetMaxOpenConns(1)`, DB writes are already serialized. The risk is the scan re-reading the file AFTER the tag write but BEFORE the DB update. Solution: perform the file write and DB update atomically (in the same critical section), and have the scan skip files that were recently edited (timestamp check or "edited" flag).
3. **Best approach: Pause/cancel active scan during tag edit, resume after.** The existing `PauseScan()`/`ResumeScan()` mechanism can be leveraged. Pause the scan, do the edit (file write + DB update), resume the scan.

**Detection:** Edit a tag, immediately trigger a scan, check if the edit survives.

**Phase:** Tag write integration with scan pipeline

---

### P6: Temp File Rename Fails Across Filesystem Boundaries

**What goes wrong:** `os.Rename()` is atomic only when source and dest are on the same filesystem. If the temp file is created in `/tmp` (default `os.CreateTemp` behavior) but the music file is on `/mnt/music`, the rename becomes a copy+delete — no longer atomic, and if interrupted, you lose the file.

**Why it happens:** Many developers use `os.CreateTemp("", ...)` which defaults to the system temp directory, which is often a different filesystem/partition from where music files live.

**Consequences:** Non-atomic write. Power loss during copy = corrupted or missing file.

**Prevention:**
1. **Create the temp file in the same directory as the target file.** Use `os.CreateTemp(filepath.Dir(targetPath), ".yj-edit-*")` to ensure same-filesystem rename.
2. Clean up temp files on startup (find files matching `.yj-edit-*` pattern in library directories — these are orphaned from crashed edits).
3. Use the temp file pattern: `<dir>/.yj-edit-<random>` → write → `os.Rename()` → done. If rename fails, the temp file is deleted. The original is untouched.

**Detection:** Check if `os.Rename()` returns `EXDEV` (cross-device link) error.

**Phase:** File write layer (earliest phase)

## Moderate Pitfalls

Mistakes that cause bugs, degraded UX, or significant rework.

### P7: ID3v2 Encoding Mismatch — UTF-8 Written Where Latin-1 Expected

**What goes wrong:** ID3v2.3 (the most common version) defaults to ISO-8859-1 (Latin-1) encoding for text frames. If the tag writing library writes UTF-8 text into a Latin-1 frame without setting the encoding byte to UTF-8/UTF-16, players that strictly follow the spec will display garbled text (mojibake). Conversely, some players write Latin-1 tags that `dhowden/tag` reads as UTF-8, causing garbled reads.

**Why it happens:** ID3v2.3 only officially supports ISO-8859-1 and UTF-16. UTF-8 support was added in ID3v2.4. Many real-world files are ID3v2.3 with UTF-8 text (spec violation that most players tolerate). When writing tags, the library must match the encoding scheme to the ID3v2 version.

**Consequences:** Non-ASCII characters (accents, CJK, Cyrillic) display as garbage in other players after editing with YellowJacket.

**Prevention:**
1. **Use `bogem/id3v2` (aka `n10v/id3v2`) for MP3 tag writing.** This library handles encoding correctly — it auto-selects UTF-8 for v2.4 and UTF-16 for v2.3, or allows explicit control.
2. When writing ID3v2.3 tags with non-ASCII content, use UTF-16 encoding (the only Unicode encoding ID3v2.3 supports).
3. Consider upgrading all written tags to ID3v2.4 (which supports UTF-8 natively). This is what most modern taggers do.
4. **Read the existing tag version and preserve it** unless the user explicitly requests an upgrade.

**Detection:** Edit a track with non-ASCII characters, open in another player (VLC, foobar2000), check for garbled text.

**Phase:** Tag writing layer

---

### P8: Cover Art Embedding Size and Format Incompatibilities

**What goes wrong:**
- **JPEG vs PNG:** Both ID3v2 and FLAC Vorbis Comments support JPEG and PNG cover art. However, some older players only handle JPEG. If the user selects a PNG, it should work but may not display in all contexts.
- **Image size:** Users may select a 10MB PNG file as cover art. Embedding this in every track of a 50-track album creates 500MB of overhead. The file write becomes extremely slow, and the FLAC rewrite (P1) is even worse because the entire file must be rewritten.
- **FLAC cover art is stored as a PICTURE metadata block** with specific structure (picture type, MIME type, description, width, height, color depth, data). Getting any of these fields wrong causes players to not display the art.
- **Vorbis Comments in OGG:** Cover art in OGG Vorbis files is stored as a base64-encoded METADATA_BLOCK_PICTURE in a Vorbis Comment field. This is a different mechanism than FLAC's native PICTURE block, despite both using "Vorbis Comments."

**Consequences:** Cover art doesn't display in other players. Enormous file size increase. Slow writes.

**Prevention:**
1. **Resize cover art before embedding.** Cap at 800x800 or 1000x1000 pixels. Convert to JPEG (quality 90) for embedding — better compression than PNG for photos.
2. **Validate image before embedding.** Decode it, check dimensions, re-encode if needed. Use `image/jpeg` and `image/png` standard library packages (already in use for thumbnail generation via `golang.org/x/image`).
3. **For FLAC:** Populate ALL required PICTURE block fields (picture type=3 "front cover", MIME type, width, height, bit depth, data).
4. **For OGG:** Base64-encode the FLAC PICTURE block structure into a `METADATA_BLOCK_PICTURE` Vorbis Comment field.
5. **Show file size impact preview** in the UI before confirming cover art change on batch operations.

**Detection:** Embed cover art, open in another player, check if art displays. Check file size increase.

**Phase:** Cover art write layer

---

### P9: Batch Edit Creates Hundreds of Orphaned Entity Rows

**What goes wrong:** User selects 50 tracks from an album and changes the artist name. Following P4's approach (create new entities, repoint audio_file), this creates 50 new recordings, 1 new artist_credit, and 50 new release_group_recordings links. The old recording rows (and their genre links) are now orphaned — nothing references them. Without cleanup, the artists/albums/genres views show ghost entries.

**Why it happens:** The "always create new" approach from P4 is correct for safety but generates garbage. The existing scan pipeline never updates entities — it only creates them. There's no existing orphan cleanup for recordings/artists/genres (only for audio_files during scan).

**Consequences:** Ghost artists, albums, and genres appear in browse views. Database grows over time. Genre list fills with duplicates if genre spelling varies slightly across edits.

**Prevention:**
1. **Run entity orphan cleanup after every edit (or batch edit).** In a single transaction:
   - Delete recordings not referenced by any audio_file
   - Delete release_group_recordings referencing deleted recordings
   - Delete recording_genres referencing deleted recordings
   - Delete artist_credits not referenced by any recording or release_group
   - Delete artists not referenced by any artist_credit_artist
   - Delete genres not referenced by any recording_genres
   - Delete release_groups not referenced by any release_group_recordings
   - Delete cover_art not referenced by any release_groups
2. **Use `LEFT JOIN ... WHERE ... IS NULL` pattern** (same approach documented in P4 of the multi-library PITFALLS).
3. **Batch the cleanup** — run once per edit session, not per-track.

**Detection:** After batch edit, check that the old artist/album/genre no longer appears in browse views (unless other tracks still reference them).

**Phase:** Database update layer (immediately after P4's approach is implemented)

---

### P10: Genre Storage Mismatch — Comma-Separated String vs Multi-Value

**What goes wrong:** The `recordings.genre` column stores genre as a free-text string. The existing `metadata.ParseGenres()` splits on `,` and `;` and normalizes to title case. But the `recording_genres` M:N junction table stores individual genre links. These two representations can diverge: the string says "Rock, Pop" but the junction table has links to "Rock" and "Pop" as separate genre entities. After a tag edit, if only the string is updated (or only the junction table), they fall out of sync.

**Why it happens:** Dual representation — the raw string in `recordings.genre` and the normalized M:N links in `recording_genres`. The scan pipeline populates both, but an edit might only update one.

**Consequences:** Genre filtering (which uses `recording_genres`) shows different results than the genre string displayed in the track list (which comes from `recordings.genre` via `track_metadata` VIEW).

**Prevention:**
1. **Always update both representations in the same transaction.** When the user sets genre to "Rock, Pop":
   - Update `recordings.genre` = "Rock, Pop"
   - Delete all `recording_genres` rows for this recording
   - Insert new `recording_genres` rows for "Rock" and "Pop" (via `ParseGenres()`)
2. **Use `ParseGenres()` consistently** for both display and storage.
3. **When writing to the audio file**, join the individual genre names with the format's conventional separator (`;` for Vorbis Comments multi-value, `,` for ID3v2 TCON frame).

**Detection:** Edit genre, verify both the displayed genre string and the genre filter show consistent results.

**Phase:** Database update layer

---

### P11: Database Update After File Write — Partial Failure Leaves Inconsistency

**What goes wrong:** The tag edit flow is: (1) write new tags to temp file, (2) rename temp to original, (3) update DB entities, (4) update FTS5 index. If step 2 succeeds but step 3 fails (e.g., SQLite busy, constraint violation), the file on disk has new tags but the DB shows old values. The next scan will "fix" this by re-reading the file, but until then the UI shows stale data.

**Why it happens:** File writes and DB writes can't be in the same transaction (they're different systems). The rename is the point of no return for the file.

**Consequences:** UI shows old metadata for edited tracks. Search returns old values. User thinks the edit failed and tries again (potentially fine since the file is already correct).

**Prevention:**
1. **DB update first approach:** Update the DB entities BEFORE writing the file. If DB update fails, don't write the file — clean rollback. If DB update succeeds but file write fails, revert the DB change. This makes the DB the "leader" and the file the "follower."
2. **Alternative: Accept eventual consistency.** Write file, update DB, if DB fails log a warning and mark the file for re-scan. The scan pipeline already handles files-on-disk-differ-from-DB.
3. **For batch edits:** Use a two-phase approach — first update all DBs in a transaction, then write all files. If any file write fails, the DB is already correct for the others. Report per-file errors to the user.
4. **Recommendation:** Option 1 (DB first) is simpler and more correct. The file write is the expensive/risky step; the DB update is fast and transactional.

**Detection:** Kill the app mid-edit (during file write), restart, verify DB and file are consistent.

**Phase:** Tag write integration layer

---

### P12: `dhowden/tag` Is Read-Only — Need Separate Write Libraries Per Format

**What goes wrong:** The existing `github.com/dhowden/tag` library is read-only. It extracts tags but cannot write them. Developers may assume the existing dependency can handle writes, waste time trying, then discover late that a separate library is needed.

**Why it happens:** `dhowden/tag` explicitly only supports reading. Its API has `ReadFrom()` but no `WriteTo()`.

**Consequences:** Need to add 1-2 new dependencies for tag writing, each with different APIs and behaviors per format.

**Prevention:**
1. **MP3 (ID3v2):** Use `github.com/bogem/id3v2/v2` (also available as `github.com/n10v/id3v2/v2`). 359 stars, actively maintained, supports read+write for ID3v2.3 and v2.4, handles encoding correctly, supports picture frames. Pure Go.
2. **FLAC:** Use `github.com/go-flac/flactag` or handle FLAC metadata blocks manually. FLAC's metadata format is simpler than ID3v2 (well-defined block structure). May need to write a thin wrapper that reads STREAMINFO + other blocks, modifies VORBIS_COMMENT block, and rewrites.
3. **OGG Vorbis:** Use `github.com/go-flac/go-ogg` or a Vorbis Comment library. OGG wraps Vorbis Comments in OGG pages, which adds framing complexity.
4. **WAV:** WAV tag support is minimal in practice. Defer WAV tag writing (not in v1.2 scope per PROJECT.md which lists MP3, FLAC, OGG only).
5. **Keep `dhowden/tag` for reading.** Don't replace it — use it alongside the write libraries.

**Phase:** Stack decision (before implementation begins)

## Minor Pitfalls

Mistakes that cause minor issues, confusion, or suboptimal UX.

### P13: Cover Art Cache Invalidation After Embedded Art Change

**What goes wrong:** Cover art is cached by content hash in `~/.local/share/yellowjacket/covers/`. If the user replaces embedded cover art, the old cached thumbnails (sm/md/lg) still exist and may be served from cache. The cover art hash changes (new image = new hash), so a new cache entry is created, but the `release_groups.cover_art_id` must be updated to point to the new `cover_art` record.

**Why it happens:** The cover art system is designed for initial extraction during scan. It doesn't expect art to change after initial import.

**Prevention:**
1. After writing new cover art to the file, extract it back, compute the new hash, create the new `cover_art` record, update `release_groups.cover_art_id`, generate new thumbnails.
2. Delete old `cover_art` record and files only if no other release_group references them (same orphan cleanup as P9).
3. Emit an event so the frontend refreshes cover art display (invalidate any cached cover art URLs).

**Phase:** Cover art write layer

---

### P14: Undo/Redo Expectations — Users Expect to Revert Tag Edits

**What goes wrong:** User changes artist name, saves, realizes it was wrong, expects Ctrl+Z to work. But tag editing writes to the actual audio file — there's no undo buffer.

**Why it happens:** File writes are destructive. The temp-file-rename approach ensures atomicity but not reversibility.

**Prevention:**
1. **For v1.2: Don't implement undo.** It's complex (would need to store original tag values per-edit) and users of tag editors generally don't expect undo.
2. **Show a confirmation dialog before writing**, especially for batch edits. "You are about to modify 47 files. This cannot be undone."
3. **Log what changed.** Write structured log entries like `"tag edit: file=/path/to/song.mp3, field=artist, old=Beatles, new=The Beatles"`. This gives users a recovery path (manual).
4. **Future milestone consideration:** Backup original files before edit (copy to `.yj-backup/` directory). Add a "restore original" option.

**Phase:** UX design

---

### P15: Track Number and Disc Number Edge Cases

**What goes wrong:** Track number is stored as `sql.NullInt64` in the DB and as `int` in `TrackMetadata`. User enters "1/12" in the track number field (common display format). If parsed as a raw int, this fails. If split on `/`, `TotalTracks` must also be stored. The existing `toNullInt64()` treats 0 as NULL, so track 0 is impossible to store (rare but exists in some compilations).

**Consequences:** Track numbers display incorrectly or can't be set to certain values.

**Prevention:**
1. Parse "N/M" format: split on `/`, store track number and total separately.
2. Validate inputs: track number must be positive integer (or blank for null).
3. Consider whether `toNullInt64()` treating 0 as NULL is correct for the edit case. For display it's fine, but for editing, the user might explicitly set track number to 0. Probably not worth changing for v1.2.

**Phase:** Frontend input validation + backend write layer

---

### P16: Multiple Audio Files Sharing the Same Recording (1:1 Assumption)

**What goes wrong:** The scan pipeline creates a new `recordings` row for every audio file (see `processMetadata()` at `library.go:1178`). This means the relationship is effectively 1:1 (each audio_file has its own recording). But the schema allows N:1 (multiple audio_files can share a recording_id). If a future change or manual DB edit creates shared recordings, editing one track's metadata would affect the other track sharing that recording.

**Why it happens:** The schema was designed for MusicBrainz-style data where multiple releases of the same recording share a recording ID. The scan pipeline doesn't implement this sharing, but the schema allows it.

**Prevention:**
1. **Before editing a recording, check how many audio_files reference it.** If more than one, create a new recording for this audio_file (fork the entity).
2. This is already handled by P4's "always create new" approach, but worth calling out as a specific guard.

**Phase:** Database update layer

---

### P17: Frontend Store Refresh After Tag Edit

**What goes wrong:** After a tag edit updates the DB, the frontend `libraryStore` still holds the old cached data (tracks, albums, artists, genres). Without a refresh, the UI shows stale values until the user navigates away and back, or triggers a full reload.

**Why it happens:** The `libraryStore.eagerFetch()` loads all data at startup. There's no mechanism for partial updates — the store either shows cached data or refetches everything.

**Prevention:**
1. **Emit a `TagsUpdated` event** from the backend after successful tag edit, with the list of affected file paths.
2. The frontend store listens for this event and either:
   - (a) Refetches the full data (simple but expensive for large libraries), or
   - (b) Patches the affected rows in-place (more complex but instant)
3. **Recommendation for v1.2:** Option (a) — full refetch. The existing `eagerFetch()` path is proven. Optimize to partial updates in a future milestone if performance is an issue.
4. Also update: search results (refetch if search is active), queue track metadata (emit `QueueTracksModified`), now-playing display (emit `TrackChanged` if the edited track is playing).

**Phase:** Frontend integration (last phase)

## Phase-Specific Warnings

| Phase Topic | Likely Pitfall | Mitigation |
|-------------|---------------|------------|
| Schema migration | P3: FTS5 contentless can't delete | Migrate to `contentless_delete=1` first |
| File write layer | P1: FLAC full rewrite, P6: temp file same dir | Write-to-temp-then-rename in same directory |
| File write layer | P2: Currently playing file | Check player state before write, stop if needed |
| Tag library selection | P12: dhowden/tag is read-only | Use bogem/id3v2 for MP3, format-specific libs for FLAC/OGG |
| DB update design | P4: Shared entities, P9: Orphan cleanup | Always create new entities, clean up orphans per-edit |
| DB update design | P10: Genre dual representation | Update both recordings.genre and recording_genres atomically |
| Write + DB integration | P5: Scan race condition | Pause scan during edit, or mutual exclusion |
| Write + DB integration | P11: Partial failure | DB update first, then file write |
| Cover art writes | P8: Size/format compat, P13: Cache invalidation | Resize before embed, invalidate cache after write |
| Encoding | P7: ID3v2 Latin-1 vs UTF-8 | Use UTF-16 for v2.3, UTF-8 for v2.4 |
| Frontend | P17: Stale cache after edit | Emit event, full refetch |
| UX design | P14: No undo for file writes | Confirmation dialog, structured logging |

## Ordering Implications

The pitfalls strongly suggest this phase ordering:

1. **FTS5 migration first** (P3) — enables all subsequent DB updates to be clean
2. **File write layer** (P1, P2, P6) — the atomic write-to-temp-rename mechanism, independent of DB
3. **Tag library integration** (P7, P8, P12) — per-format write support using new dependencies
4. **DB update design** (P4, P9, P10, P11, P16) — entity creation, orphan cleanup, genre sync
5. **Scan pipeline integration** (P5) — mutual exclusion between edit and scan
6. **Frontend** (P14, P15, P17) — UI, events, cache refresh

## Sources

- SQLite FTS5 documentation: contentless tables section (sqlite.org/fts5.html#contentless_tables) — HIGH confidence
- SQLite FTS5 contentless_delete: sqlite.org/fts5.html#contentless_delete_tables — HIGH confidence
- YellowJacket codebase analysis: search.go, library.go, player.go, schema files — HIGH confidence
- FLAC format spec: metadata block structure, PICTURE block format — HIGH confidence (well-established spec)
- ID3v2.3/2.4 spec: encoding requirements for text frames — HIGH confidence
- `bogem/id3v2` GitHub (n10v/id3v2): read+write ID3v2 library, 359 stars — MEDIUM confidence (verified repo exists and has write support)
- `dhowden/tag` API: read-only confirmed from codebase usage — HIGH confidence
