# Phase 18: Batch Edit - Context

**Gathered:** 2026-03-18
**Status:** Ready for planning

<domain>
## Phase Boundary

Multi-select batch editing of track metadata and cover art. Users select multiple tracks, open a batch editor (via the existing "Track Details" context menu which adapts for multi-select), view a summary of shared/differing field values, enter edit mode to make changes using implicit three-state field model, and save with progress feedback. The single-track edit pipeline from Phase 17 (WriteTrackTagsByPath, DB sync, view refresh) is the foundation — this phase adds multi-track field merging, batch write orchestration with progress, and the adapted dialog UI.

</domain>

<decisions>
## Implementation Decisions

### Three-state field model
- States are implicit from user action, NOT explicit UI controls:
  - **Keep original** = user doesn't touch the field (stays as-is)
  - **Set value** = user types a new value into the field
  - **Clear field** = user selects content and deletes it (empty string, distinct from "untouched")
- Dirty-tracking on the frontend: only fields the user interacted with are sent to the backend as TagChanges
- Fields with **shared values** across all selected tracks: pre-populated with the actual value (behaves like single-track edit)
- Fields with **mixed values** (different across tracks): input is empty with placeholder text like "Multiple values" in gray italic
- No per-field state toggle icons or dropdowns — the input behavior IS the state
- No warning when typing into a mixed-value field — the save confirmation handles this

### Selection & entry flow
- Same "Track Details" context menu item adapts for multi-select — NOT a separate "Batch Edit" menu entry
- When 2+ tracks are selected, "Track Details" opens a **read-only summary view first** showing:
  - Header: "N tracks selected"
  - Each field shows its shared value OR "N different values" indicator
  - Cover art area (see cover art section below)
- User clicks "Edit" button to enter edit mode (same pattern as single-track)
- Works from **all existing multi-select views** (track list, album detail, playlist detail) — wherever multi-select and context menu already exist

### Progress & error handling
- **Progress indicator** for batch writes: horizontal progress bar + "N of M tracks" counter text, shown inside the dialog
- **Partial failure handling:** continue processing all tracks, skip failures, then show results summary with success count and failure details (filename + reason for each failure)
- **Cancel button** visible during progress — already-written tracks keep changes, remaining tracks skipped, report what completed
- **After batch write completes:** dialog returns to the read-only summary view with updated values (re-fetched from DB)

### Save confirmation
- **Single confirmation dialog on save** that covers ALL pending changes — no separate warnings for different situations
- Confirmation shows what will change: e.g., "Apply changes to N tracks?" with a summary of which fields are being set/cleared and whether cover art is being replaced/removed
- This is the sole guard against accidental bulk overwrites — no other warning dialogs needed anywhere in the batch flow

### Batch cover art
- Same controls as single-track edit: click art area to pick new image (native file picker, JPEG/PNG), pencil overlay icon in edit mode, X button to remove
- **Mixed cover art display** (read-only summary): placeholder image indicating "multiple values" with small descriptive text showing count of how many different cover arts exist in the selection
- **Shared cover art display:** show the actual cover art (same as single-track)
- Picking a new image: same file picker, same preview in dialog. On save, embedded in every selected track.
- Clearing cover art: remove button applies to all tracks on save (covered by the single save confirmation dialog)

### Claude's Discretion
- Exact progress bar styling and animation
- How to display the failure details list (inline in dialog vs expandable section)
- The save confirmation dialog's exact layout and wording
- How the "N different values" placeholder is styled for mixed fields
- Cover art placeholder design for the mixed-art state
- Whether the summary view shows non-editable metadata (format, bitrate, duration) or only the editable fields
- Implementation approach for the batch write orchestration (sequential loop, backend endpoint, etc.)

</decisions>

<specifics>
## Specific Ideas

- The existing `track-details` component has full edit mode infrastructure from Phase 17 (editing state, editValues, save flow, cover art picker). The batch editor should extend or adapt this component rather than building from scratch.
- `WriteTrackTagsByPath` from Phase 16/17 processes one track at a time — the batch write loop calls it N times sequentially with progress events between each call.
- The `asInt()`/`asBytes()` Wails deserialization helpers from Phase 17 are already in place for the TagChanges payload.
- The `TrackMetadataChanged` event is already wired for view refresh — batch writes should emit this once after all writes complete (not per-track) to avoid N full reloads.

</specifics>

<deferred>
## Deferred Ideas

- Auto-completion for tag entry fields based on existing library metadata — new capability that benefits both single-track and batch editing, deserves its own phase
- Undo/redo for tag edits (EDIT-F01) — future milestone
- Auto-capitalize and clean tag values on save (EDIT-F02) — future milestone

</deferred>

---

*Phase: 18-batch-edit*
*Context gathered: 2026-03-18*
