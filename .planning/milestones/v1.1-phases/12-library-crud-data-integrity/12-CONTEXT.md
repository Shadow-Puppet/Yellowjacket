# Phase 12: Library CRUD & Data Integrity - Context

**Gathered:** 2026-03-12
**Status:** Ready for planning

<domain>
## Phase Boundary

Users can add, rename, and remove libraries through the UI with correct data lifecycle management. Tracks are created/deleted, shared entities (artists, albums, genres) are cleaned up only when orphaned, FTS5 search index stays consistent, queue tracks cascade-delete, and playlist tracks convert to phantoms. The library manager UI lives in settings alongside scan controls.

</domain>

<decisions>
## Implementation Decisions

### Library management UI
- Integrated library + scan section in the settings page — combine library management and scanning into one unified section
- Remove the libraries tab from the sidebar list entirely
- Each library row displays: name, directory path, track count in the main row; actions (rename, remove, rescan) hidden behind a `...` overflow menu
- Replace the old single-directory config UI (directory path field + rescan button) completely — the migrated library appears in the new list

### Add-library flow
- Click "Add Library" button in the library management section
- OS folder picker dialog opens
- Library auto-named from the folder name (editable later via rename)
- Scan starts automatically after adding
- Uses the existing per-library scan pipeline from Phase 11

### Removal confirmation & feedback
- Warning dialog with impact summary before removal: "Remove 'Jazz Collection'? This will delete 1,234 tracks, affect 2 playlists, and remove 15 queue items."
- If a track from the library being removed is currently playing, stop playback first, then proceed with removal; queue advances to next valid track if one exists
- Blocking operation with spinner on the dialog while cleanup runs (expected < 1 second for most libraries)
- Toast notification on completion: "Removed 'Jazz Collection' (1,234 tracks deleted)"

### Orphan cleanup behavior
- Immediate cleanup in the same database transaction — delete tracks, identify orphaned entities, delete orphans, convert playlist phantoms, all atomic
- Reference-counting bottom-up: only delete artists/albums/genres that have zero remaining track references after the library's tracks are removed
- Rebuild the entire FTS5 index from remaining tracks after library removal (handles contentless table limitation cleanly)
- Playlist phantom track conversion in the same transaction: copy track metadata to phantom columns on playlist_tracks, then SET NULL the audio_file_id
- Queue tracks cascade-delete (queue is ephemeral, not user-curated)
- Removal API endpoint returns cleanup summary: {tracks_deleted, artists_removed, albums_removed, genres_removed, playlists_affected, queue_items_removed} — feeds the toast notification

### Rename & display behavior
- Library names must be unique — validation error if user tries to use an existing name
- Inline edit on the list row: click name (or rename action from menu) turns it into an editable text field, Enter to save, Escape to cancel
- Name validation: 1-50 characters, non-empty
- Rename changes display name only — changing a library's directory path requires remove + add (no path editing)

### Claude's Discretion
- Exact layout/styling of the library management section within settings
- Loading skeleton design while library list loads
- Error state handling for failed operations
- Exact spinner implementation during removal
- Toast notification library/component choice
- API endpoint URL structure and HTTP methods
- SQL query optimization for orphan detection

</decisions>

<specifics>
## Specific Ideas

- Library management section should feel like a natural extension of the existing settings page — not a separate app within settings
- The impact summary in the removal dialog should use real counts from the database, not estimates
- The `...` overflow menu pattern keeps the list clean — same pattern used elsewhere in the app for action menus

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope

</deferred>

---

*Phase: 12-library-crud-data-integrity*
*Context gathered: 2026-03-12*
