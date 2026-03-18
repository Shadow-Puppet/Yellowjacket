---
phase: 17-single-track-edit
plan: 02
subsystem: ui
tags: [tag-editing, cover-art, wails-bindings, lit-element, dialog, file-picker]

# Dependency graph
requires:
  - phase: 17-single-track-edit
    provides: WriteTrackTagsByPath, ImageFilePicker, TrackMetadataChanged handler, Track Details context menu
provides:
  - Complete single-track tag editor with save flow, cover art editing, and error handling
  - ReadFile Go method on FrontendUtil for reading cover art image bytes
  - DB sync for cover art (save to cache, thumbnail generation, release_group update)
  - Dialog data refresh after save (track + cover art URLs)
affects: [18-batch-edit]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Diff-only TagChanges map — only changed fields sent to backend, reducing unnecessary writes"
    - "Blob URL preview for cover art — instant client-side preview without server round-trip"
    - "Base64 decode for Go []byte return values — Wails serializes []byte as base64 JSON strings"
    - "asInt/asBytes helpers for Wails JSON deserialization — JavaScript numbers arrive as float64, arrays as []interface{}"

key-files:
  created: []
  modified:
    - frontend/src/components/track-details/track-details.ts
    - backend/frontendutil/frontendutil.go
    - backend/tagwriter/tagwriter.go
    - backend/tagwriter/dbsync.go
    - backend/tagwriter/mp3.go
    - backend/tagwriter/flac.go
    - frontend/wailsjs/go/frontendutil/FrontendUtil.js
    - frontend/wailsjs/go/frontendutil/FrontendUtil.d.ts

key-decisions:
  - "ReadFile Go method on FrontendUtil to return file bytes to frontend — needed because Wails native file dialog returns path, but frontend needs bytes for preview + save"
  - "asInt/asBytes type coercion helpers in tagwriter — Wails JSON deserialization sends all numbers as float64 and byte arrays as base64 strings"
  - "Cover art DB sync saves to covers cache directory with content-hash dedup and thumbnail generation"

patterns-established:
  - "Wails float64 coercion: always use asInt() helper for numeric TagChanges values, never direct .(int) assertion"
  - "Wails []byte handling: Go []byte serializes as base64 JSON string; frontend must atob() decode before use"

requirements-completed: [EDIT-02, EDIT-03, EDIT-04]

# Metrics
duration: 25min
completed: 2026-03-18
---

# Phase 17 Plan 02: Track Details Save Flow & Cover Art Editing Summary

**Diff-only tag save with cover art replace/remove via native file picker, inline error handling, and automatic dialog + view refresh after write**

## Performance

- **Duration:** ~25 min (implementation) + verification session with bug fixes
- **Started:** 2026-03-18T01:08:53Z
- **Completed:** 2026-03-18T14:54:17Z
- **Tasks:** 2 (1 auto + 1 human-verify)
- **Files modified:** 8

## Accomplishments
- Complete save flow: `saveEdit()` builds diff-only TagChanges map and calls `WriteTrackTagsByPath` — unchanged fields are never sent
- Cover art editing: native file picker for JPEG/PNG with instant blob preview via object URL; remove button (×) clears embedded art
- Inline error handling: errors display in the dialog action bar, edit mode stays active for retry or cancel
- Saving state indicator: Save button shows "Saving…" and both buttons disabled during write
- Dialog data refresh: after save, track data and cover art URLs are re-fetched from the library store
- Cover art DB sync: image saved to covers cache with content-hash dedup + thumbnail generation, release_group updated
- Wails JSON deserialization fixes: asInt/asBytes helpers handle float64 numbers and base64 byte arrays

## Task Commits

Each task was committed atomically:

1. **Task 1: Implement saveEdit, cover art editing, error handling, and saving state** - `265a9ea` (feat)
2. **Task 2: Verify complete single-track edit flow** — human-verify checkpoint, APPROVED

**Bug fixes during verification (committed by orchestrator):**
3. **Fix: refresh track-details dialog data after save** - `ffcdc41` (fix)
4. **Fix: handle float64 numeric values from Wails JSON deserialization** - `900db2e` (fix)
5. **Fix: cover art replace and remove (asBytes, DB sync, base64 decode)** - `d7c2965` (fix)
6. **Fix: refresh cover art URLs after save** - `8cd4914` (fix)

## Files Created/Modified
- `frontend/src/components/track-details/track-details.ts` - Complete save flow, cover art editing UI, error handling, saving state, dialog refresh
- `backend/frontendutil/frontendutil.go` - Added ReadFile method for reading cover art bytes
- `backend/tagwriter/tagwriter.go` - Added asInt/asBytes helpers for Wails JSON deserialization
- `backend/tagwriter/dbsync.go` - Cover art DB sync (save image, update release_group, orphan cleanup)
- `backend/tagwriter/mp3.go` - Use asInt/asBytes helpers for type coercion
- `backend/tagwriter/flac.go` - Use asInt/asBytes helpers for type coercion
- `frontend/wailsjs/go/frontendutil/FrontendUtil.js` - Wails binding for ReadFile
- `frontend/wailsjs/go/frontendutil/FrontendUtil.d.ts` - TypeScript declaration for ReadFile

## Decisions Made
- Added `ReadFile` Go method on FrontendUtil to bridge the gap between native file dialog (returns path) and frontend need for bytes (preview + save). Simplest approach that avoids additional Go-side image processing.
- Created `asInt()` and `asBytes()` type coercion helpers in tagwriter package — Wails JSON deserialization always sends JavaScript numbers as Go `float64` and `[]byte` as base64 strings. Direct `.(int)` assertions silently failed.
- Cover art DB sync saves the image to the covers cache directory using content-hash dedup with thumbnail generation, then updates `release_groups.cover_art_id`. Clear sets `cover_art_id` to NULL.

## Deviations from Plan

### Auto-fixed Issues (by orchestrator during verification)

**1. [Rule 1 - Bug] Dialog showed stale track data after save**
- **Found during:** Task 2 (human verification)
- **Issue:** After save, dialog returned to read-only mode but showed pre-edit values because `this.track` was the original snapshot passed via `show()`
- **Fix:** After successful `WriteTrackTagsByPath`, re-fetch tracks from library store and update `this.track` with fresh data
- **Files modified:** `frontend/src/components/track-details/track-details.ts`
- **Committed in:** `ffcdc41`

**2. [Rule 1 - Bug] Numeric fields silently ignored during save**
- **Found during:** Task 2 (human verification)
- **Issue:** Wails JSON deserialization sends all JavaScript numbers as Go `float64`. All `.(int)` type assertions on year, track_number, and disc_number silently failed (returned zero-value + false), meaning numeric edits were dropped
- **Fix:** Added `asInt()` helper that handles both `float64` and `int` types; replaced all direct `.(int)` assertions across tagwriter package
- **Files modified:** `backend/tagwriter/tagwriter.go`, `backend/tagwriter/dbsync.go`, `backend/tagwriter/mp3.go`, `backend/tagwriter/flac.go`
- **Committed in:** `900db2e`

**3. [Rule 1 - Bug] Cover art replace and remove did not work**
- **Found during:** Task 2 (human verification)
- **Issue:** Three related issues: (a) Cover art bytes from frontend arrived as `[]interface{}` of `float64` — same deserialization issue as numerics. (b) DB sync for cover art was a placeholder no-op — didn't save image to cache or update release_group. (c) Frontend `ReadFile` returns base64 string (Go `[]byte` JSON encoding), not `number[]` — preview blob was corrupted.
- **Fix:** Added `asBytes()` helper for `[]interface{}` → `[]byte` conversion. Implemented full cover art DB sync (save to covers cache with content-hash dedup + thumbnail generation, upsert cover_art row, update release_groups). Fixed frontend to decode base64 with `atob()` before creating `Uint8Array`.
- **Files modified:** `backend/tagwriter/tagwriter.go`, `backend/tagwriter/dbsync.go`, `backend/tagwriter/flac.go`, `backend/tagwriter/mp3.go`, `frontend/src/components/track-details/track-details.ts`
- **Committed in:** `d7c2965`

**4. [Rule 1 - Bug] Cover art image reverted to old after save**
- **Found during:** Task 2 (human verification)
- **Issue:** After save, dialog refreshed `this.track` but kept stale `this.coverArt` URLs pointing to old content-hash files. The image visually reverted until the dialog was closed and reopened.
- **Fix:** After save, re-fetch albums alongside tracks and re-resolve cover art URLs from updated album data
- **Files modified:** `frontend/src/components/track-details/track-details.ts`
- **Committed in:** `8cd4914`

---

**Total deviations:** 4 auto-fixed (all Rule 1 bugs discovered during human verification)
**Impact on plan:** All fixes were necessary for correct end-to-end functionality. The Wails JSON deserialization issues (float64 numbers, base64 bytes) were a systemic pattern not visible until real runtime testing. No scope creep — all fixes are within the plan's boundary.

## Issues Encountered
None beyond the deviations documented above. All issues were discovered and resolved during the human verification checkpoint.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- Phase 17 is now complete (2/2 plans done)
- Single-track editing works end-to-end: tag writes, cover art replacement/removal, error handling, view refresh
- Ready for Phase 18 (Batch Edit) which builds on this foundation
- The `asInt()`/`asBytes()` Wails deserialization helpers established in this plan will be essential for Phase 18

## Self-Check: PASSED

All 6 key files verified on disk. All 5 commits verified in git history.

---
*Phase: 17-single-track-edit*
*Completed: 2026-03-18*
