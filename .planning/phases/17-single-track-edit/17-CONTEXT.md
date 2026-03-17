# Phase 17: Single Track Edit - Context

**Gathered:** 2026-03-17
**Status:** Ready for planning

<domain>
## Phase Boundary

End-to-end single track editing: user opens a tag editor dialog, edits metadata fields and/or cover art, saves changes which write tags to the audio file, update the database and FTS5 search index, and refresh all visible views immediately. The backend pipeline (WriteTrackTags) and tag writers (MP3, FLAC) are already built in Phase 16. This phase wires the existing track-details dialog's edit mode to the real backend and adds cover art replacement.

</domain>

<decisions>
## Implementation Decisions

### Cover art replacement flow
- In edit mode, clicking the cover art image opens a native file picker (Wails file dialog)
- A subtle edit/pencil icon overlays the artwork in edit mode to indicate clickability
- File picker filters to JPEG and PNG only (.jpg, .jpeg, .png)
- Selected image is previewed instantly in the dialog before saving (client-side preview via object URL or data URL)
- User can also remove existing cover art entirely (clear embedded art) — a small "remove" action (e.g., X button) appears on hover/in edit mode
- Cover art bytes are sent to WriteTrackTags via the `cover_art` field ([]byte for set, nil/sentinel for clear)

### Edit entry points
- Use the existing Track Details dialog which already has Edit/Save/Cancel buttons and edit mode inputs
- Entry is via right-click context menu → "Track Details" → click "Edit" button inside the dialog
- No separate "Edit Tags" context menu item — the existing flow is sufficient
- No keyboard shortcut for edit mode — context menu only
- "Track Details" should appear in the context menu when right-clicking any track, regardless of selection state (not just when exactly 1 track is selected)
- Minimal changes to the existing dialog layout — the UI scaffolding is already in place, wire the `saveEdit()` method to call `WriteTrackTags`

### View refresh after save
- On `TrackMetadataChanged` event, perform a full data reload from the database (invalidate library store caches, re-fetch tracks/albums/artists/genres)
- Full reload is acceptable because editing is a low-frequency operation
- Now-playing bar updates naturally as part of the store refresh
- After successful save, the dialog stays open and switches back to read-only view mode so the user can verify changes took effect
- Dialog re-fetches its own track data after save to show updated values

### Error handling
- If the file write fails (read-only file, unsupported format like WAV/OGG, other errors), show the error message inline inside the dialog
- Edit mode stays active on error so the user can retry or cancel
- No toast/snackbar needed — the dialog itself communicates the error

### Track ID resolution
- The frontend `library.Track` identifies tracks by `FilePath` but `WriteTrackTags` requires `trackID int64`
- Need a backend wrapper or lookup to bridge this gap (e.g., `WriteTrackTagsByPath(filePath, changes)` or expose a path→ID lookup)

### Claude's Discretion
- Exact error message wording and styling
- Loading/saving state indicator design (spinner, disabled button, etc.)
- How the "remove cover art" action is visually presented (X button placement, confirmation)
- Whether to add a saving indicator/disabled state while WriteTrackTags is in progress
- Implementation approach for the track ID resolution (wrapper vs lookup endpoint)

</decisions>

<specifics>
## Specific Ideas

- The track-details dialog (`frontend/src/components/track-details/track-details.ts`) already has full edit mode infrastructure: `editing` state, `editValues` record, input fields for all editable metadata, Edit/Save/Cancel buttons, and a `saveEdit()` TODO stub. The implementation work is wiring this to `WriteTrackTags`, not building UI from scratch.
- The `WriteTrackTags` Wails binding is already generated at `frontend/wailsjs/go/tagwriter/TagWriter.ts` — accepts `(trackID: number, changes: Record<string, any>)` and returns `Promise<void>`.
- The `TrackMetadataChanged` event is already defined in the events system with payload `{ trackId: number, filePath: string }`.
- Cover art is currently resolved from album cache (album → coverArtPath), not from individual tracks. After editing cover art, the album cache must also be refreshed.

</specifics>

<deferred>
## Deferred Ideas

- Multi-track details view showing shared fields and placeholders for differing values — Phase 18 (batch edit with three-state field model)
- Keyboard shortcut to open edit mode directly — revisit if users request it
- Auto-capitalize or clean tag values on save — future milestone (EDIT-F02)

</deferred>

---

*Phase: 17-single-track-edit*
*Context gathered: 2026-03-17*
