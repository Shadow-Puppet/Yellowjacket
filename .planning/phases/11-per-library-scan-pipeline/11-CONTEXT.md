# Phase 11: Per-Library Scan Pipeline - Context

**Gathered:** 2026-03-09
**Status:** Ready for planning

<domain>
## Phase Boundary

Refactor the scan pipeline from scanning a single hardcoded directory to scanning individual libraries by ID. Add sequential scan coordination (queue) so only one library scans at a time. Update progress UI to identify which library is scanning. Existing cancel/pause/resume controls work per-library with clear scope when multiple scans are queued.

Library CRUD UI is Phase 12. Library-filtered views are Phase 13. This phase only changes how scans are triggered, coordinated, and displayed.

</domain>

<decisions>
## Implementation Decisions

### Concurrent scan policy
- Queue silently when a scan is requested while another is running — no confirmation dialog, no toast
- Ignore duplicate scan requests silently (if library is already scanning or already queued, no-op)
- Unbounded queue — no cap on queued scans (realistic library counts are low, 2-10)
- Seamless transition between queued scans — progress UI updates to next library name, no notification

### Scan trigger model
- Auto-scan all libraries on app launch (current single-directory behavior extended to all libraries)
- `ScanLibrary(id int64)` Wails-bound method — scans a specific library by database ID
- `ScanAllLibraries()` Wails-bound method — queries all libraries and queues them sequentially; used by both app startup and the UI "Scan All" button
- "Scan All Libraries" button in the UI in addition to per-library scan buttons

### Progress identification
- Library name shown in existing progress bar area: "Scanning: [Library Name] (245/1200 files)"
- When libraries are queued, show queue count: "N libraries queued" alongside the active scan progress
- Progress UI disappears/collapses when all scans complete (matches current behavior)

### Cancel/pause scope
- Cancel button during a queued multi-scan shows a **modal dialog** with two choices: "Cancel This Library" and "Cancel All Scanning" — no default, user must pick
- If user cancels just the current library, the next queued library starts automatically
- Pause freezes the current scan AND the queue — resume continues the paused library, then the queue proceeds
- No partial scan indication needed — partially-scanned library keeps whatever files were processed, user can re-scan later

### Claude's Discretion
- Event payload format (whether scan events include library name or just ID)
- Internal queue data structure implementation
- Exact progress bar label formatting and layout
- How "Scan All" button is placed in the UI (this phase focuses on the button existing; Phase 12 designs the full library management UI)

</decisions>

<specifics>
## Specific Ideas

- The scan queue coordinator should be a separate concern from the scan execution itself — clean separation between "what to scan next" and "how to scan"
- Cancel dialog should feel similar to the existing cancel confirmation from Phase 9, extended with the scope choice
- Auto-scan on launch should use the same `ScanAllLibraries()` codepath as the UI button — single implementation

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope

</deferred>

---

*Phase: 11-per-library-scan-pipeline*
*Context gathered: 2026-03-09*
