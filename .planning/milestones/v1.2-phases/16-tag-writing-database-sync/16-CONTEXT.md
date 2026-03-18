# Phase 16: Tag Writing & Database Sync - Context

**Gathered:** 2026-03-17
**Status:** Ready for planning

<domain>
## Phase Boundary

The backend can write metadata tags and cover art to MP3 and FLAC files, then synchronize all changes to the database and search index in a single atomic operation. This phase delivers the write pipeline that Phase 17 (Single Track Edit UI) and Phase 18 (Batch Edit) call into. No UI work in this phase.

Requirements: WRITE-01, WRITE-02, WRITE-04, WRITE-06, SYNC-01, SYNC-02, SYNC-03, SYNC-04

</domain>

<decisions>
## Implementation Decisions

### Tag writer API shape
- **Diff map for changes:** Callers specify changed fields as a map of field name to new value (e.g. `map[string]any{"artist": "New Name", "year": 2024}`). Only changed fields are sent — naturally supports partial edits and batch (Phase 18).
- **Single function call:** `WriteTrackTags(trackID, changes)` — one call does everything: write file tags, update DB entities, update FTS5 search index. No two-step prepare/commit.
- **Track ID input:** Accepts track ID (int64), not file path. The pipeline looks up the file path, format, and current metadata from the database. The UI only knows track IDs.
- **Single entry point, auto-dispatch:** One entry point detects MP3/FLAC from the file extension and routes to the appropriate format-specific writer internally. The caller never thinks about audio format.

### Cover art handling
- **No size/format constraints:** Accept any JPEG/PNG image as-is, embed without resizing or validation. The user chose the image — use it.
- **Immediate thumbnail regeneration:** After writing new cover art, regenerate all 3 thumbnail sizes (sm/md/lg) immediately so all views show updated art without delay.
- **Part of the diff map:** Cover art is a field in the same changes map as text fields (e.g. `{"cover_art": imageBytes}`). Keeps the single-call pipeline uniform.
- **Set, replace, and clear:** Support adding art to tracks with none, replacing existing art, and removing art entirely (clearing the embedded picture).

### Entity relinking behavior
- **Upsert-and-relink:** When an artist/album/genre name changes, find an existing entity with the new name or create one. Point the track at the new entity. Never mutate shared entity rows in-place. Matches the existing upsert-and-relink pattern from v1.1.
- **Immediate orphan cleanup:** After relinking, check if the old entity has zero remaining track references and delete it right away. No stale entities in browse views.
- **Album artist is a text field:** Album artist stays as a simple text field on the audio_files row — no new album_artist entity table. Edit it directly, no relinking needed.
- **Single DB transaction after file write:** File write (via AtomicWrite) happens first. On success, one database transaction handles: update audio_files row, upsert/relink entities, update FTS5 search index, cleanup orphans. If file write fails, DB is untouched. If DB transaction fails, file has new tags but DB is still consistent at old state (next scan would reconcile).

### Player safety coordination
- **Stop playback completely:** If the target file is currently playing, stop playback entirely (not pause). Release the file handle so the write can proceed.
- **Auto-stop in pipeline:** The write pipeline automatically checks if the target file is playing and stops the player. Callers don't need to handle player state.
- **Mutual exclusion with scan:** Scan pipeline and write pipeline share a mutex. If a scan is running, the write waits for it to finish (and vice versa). No concurrent modification of the same file.
- **Event-driven frontend notification:** After write + DB sync complete, emit an event (e.g. TrackMetadataChanged) so the frontend refreshes all views. Matches the existing event-driven sync architecture.

### Claude's Discretion
- Internal format-specific writer implementation details (ID3v2 frame handling, Vorbis Comment block management)
- Choice of Go libraries for tag writing (research phase will evaluate options)
- Exact field name strings in the diff map
- Error handling and error message wording
- Test file fixtures and test structure

</decisions>

<specifics>
## Specific Ideas

- The write pipeline should feel like a single atomic operation from the caller's perspective — "change these fields on this track" and everything Just Works
- Existing `upsert-and-relink` pattern (from v1.1 library scan) should be reused for entity management after tag edits — same code path, different trigger
- STATE.md flagged "album artist storage — not currently a separate entity; resolve during planning" — resolved: keep as text field, no new entity table
- STATE.md flagged "go-flac libraries (44 stars) — verify round-trip with edge-case FLAC files early" — research should prioritize this

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope

</deferred>

---

*Phase: 16-tag-writing-database-sync*
*Context gathered: 2026-03-17*
