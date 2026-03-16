---
phase: 09-scan-cancellation-keyboard-shortcuts
plan: 02
subsystem: ui
tags: [keyboard-shortcuts, wails, lit, toml, config]

# Dependency graph
requires:
  - phase: 09-scan-cancellation-keyboard-shortcuts
    provides: ShortcutsConfigChanged event (added in 09-01 codegen)
provides:
  - Go shortcuts config package with defaults and validation
  - Wails binding methods for shortcut CRUD (GetShortcuts, SetShortcuts, SetShortcut, ResetShortcuts)
  - Frontend KeyboardShortcutService singleton with scope resolution
  - ShortcutsStore with Wails persistence and event sync
  - ShortcutsController for Lit component integration
  - buildKeyString utility for shortcut capture widget
affects: [09-04-shortcuts-settings-ui, 09-05-shortcuts-integration]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Keyboard shortcut service singleton pattern (document keydown listener)"
    - "Shadow DOM deep active element resolution for scope detection"
    - "Canonical key string format: Ctrl+Alt+Shift+Key"

key-files:
  created:
    - backend/shortcuts/config.go
    - frontend/src/services/keyboard-shortcut-service.ts
    - frontend/src/store/shortcuts-store.ts
    - frontend/src/store/controllers/shortcuts-controller.ts
  modified:
    - backend/config/config.go
    - backend/events/events.go
    - frontend/src/events.ts
    - frontend/src/store/index.ts
    - frontend/index.ts
    - frontend/wailsjs/go/config/Config.d.ts
    - frontend/wailsjs/go/config/Config.js
    - frontend/wailsjs/go/models.ts

key-decisions:
  - "Use ChangeVolume(delta) Wails binding for relative volume instead of reading state + SetVolume"
  - "Use CurrentPositionSeconds + Seek for relative seek (no delta API available)"
  - "Dispatch tracklist actions as CustomEvents on document for loose coupling"
  - "Remove hardcoded Ctrl+F handler in index.ts — keyboard shortcut service now handles it"

patterns-established:
  - "services/ directory for singleton services (first usage)"
  - "data-shortcut-scope attribute on elements for panel-specific shortcuts"
  - "shortcut: event prefix for panel-specific shortcut dispatch"

requirements-completed: [KEY-01, KEY-04, KEY-05]

# Metrics
duration: 35min
completed: 2026-03-07
---

# Phase 9 Plan 2: Keyboard Shortcuts Config & Service Summary

**Go shortcuts config with TOML persistence, frontend KeyboardShortcutService singleton with scope resolution, shadow DOM active element walking, and text input suppression**

## Performance

- **Duration:** 35 min
- **Started:** 2026-03-07T02:14:19Z
- **Completed:** 2026-03-07T02:49:20Z
- **Tasks:** 2
- **Files modified:** 12

## Accomplishments
- Go `shortcuts` package with 17 default bindings (player, nav, app, tracklist)
- Wails binding methods for shortcut CRUD: GetShortcuts, SetShortcuts, SetShortcut, ResetShortcuts
- Frontend KeyboardShortcutService with shadow DOM scope resolution and text input suppression
- ShortcutsStore syncs bindings via Wails events with queueMicrotask coalescing
- Replaced hardcoded Ctrl+F handler with service-based dispatch

## Task Commits

Each task was committed atomically:

1. **Task 1: Create backend shortcuts config package and wire into main config** - `6285ca9` (feat)
2. **Task 2: Create frontend keyboard shortcut service, store, and controller** - `40d4815` (feat)

## Files Created/Modified
- `backend/shortcuts/config.go` - Shortcuts config package with defaults, ApplyDefaults, Validate
- `backend/config/config.go` - Shortcuts field, getter/setter Wails bindings, event emission
- `backend/events/events.go` - ShortcutsConfigChanged event constant
- `frontend/src/events.ts` - Generated TypeScript event constant
- `frontend/src/services/keyboard-shortcut-service.ts` - Singleton keydown listener with scope resolution
- `frontend/src/store/shortcuts-store.ts` - Store with Wails persistence and event sync
- `frontend/src/store/controllers/shortcuts-controller.ts` - ReactiveController for Lit components
- `frontend/src/store/index.ts` - Added shortcuts store and controller exports
- `frontend/index.ts` - Removed hardcoded Ctrl+F, added service import
- `frontend/wailsjs/go/config/Config.d.ts` - Generated Wails TypeScript bindings
- `frontend/wailsjs/go/config/Config.js` - Generated Wails JavaScript stubs
- `frontend/wailsjs/go/models.ts` - Generated Wails model types

## Decisions Made
- Used `ChangeVolume(delta)` Wails binding for relative volume adjustment (cleaner than state read + SetVolume)
- Used `CurrentPositionSeconds() + Seek(target)` for relative seeking (no delta seek API exists)
- Panel-specific actions (tracklist.play, tracklist.delete) dispatch as CustomEvents on document for loose coupling — track-list component can listen without import dependency
- Removed the hardcoded Ctrl+F keydown handler from index.ts — the keyboard shortcut service now handles `nav.searchAlt` → Ctrl+F

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Fixed wsl lint error in library.go**
- **Found during:** Task 1 (pre-commit hook failure)
- **Issue:** `backend/library/library.go:593` had missing blank line before logger call (from Plan 01 commit)
- **Fix:** Added blank line before `l.logger.Info("scan cancelled, skipping orphan cleanup")`
- **Files modified:** backend/library/library.go
- **Verification:** golangci-lint passes with 0 issues
- **Committed in:** 6285ca9 (Task 1 commit)

---

**Total deviations:** 1 auto-fixed (1 blocking)
**Impact on plan:** Trivial lint fix required to unblock pre-commit hook. No scope creep.

## Issues Encountered
- Pre-commit hooks caused significant delays — `golangci-lint` runs on entire project and `codegen-check` verifies working tree cleanliness. Concurrent Plan 01 agent commits created race conditions with git staging. Resolved by stashing unrelated changes and ensuring clean working tree before commit.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- Shortcuts foundation complete — default bindings work out of the box
- Ready for Plan 03 (scan control UI) and Plan 04 (shortcuts settings UI)
- `data-shortcut-scope` attribute ready for track-list and queue-panel components to adopt
- `buildKeyString` utility exported for the shortcut capture widget in Plan 04

---
*Phase: 09-scan-cancellation-keyboard-shortcuts*
*Completed: 2026-03-07*
