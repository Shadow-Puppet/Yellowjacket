---
phase: quick-16
plan: 01
subsystem: frontend/keyboard-shortcuts
tags: [shortcuts, selection, ctrl-a, multi-select]
dependency_graph:
  requires: [selection-controller, keyboard-shortcut-service]
  provides: [select-all-shortcut]
  affects: [track-list, queue-panel, playlist-view]
tech_stack:
  added: []
  patterns: [CustomEvent broadcast for panel-agnostic shortcuts]
key_files:
  created: []
  modified:
    - frontend/src/utils/selection-controller.ts
    - frontend/src/services/keyboard-shortcut-service.ts
    - frontend/src/components/track-list/track-list.ts
    - frontend/src/components/queue-panel/queue-panel.ts
    - frontend/src/components/playlist-view/playlist-view.ts
decisions:
  - Broadcast shortcut:select-all to all connected components rather than routing to focused panel — harmless no-op on empty/inactive panels, consistent with existing tracklist.play pattern
metrics:
  duration: 2 min
  completed: "2026-03-07"
---

# Quick Task 16: Add Ctrl+A Hotkey to Multi-Select Views

SelectionController.selectAll() method with shortcut:select-all CustomEvent broadcast to track-list, queue-panel, and playlist-view

## What Changed

### Task 1: Add selectAll() to SelectionController and change app.selectAll dispatch
**Commit:** `f567762`

- Added `selectAll()` method to `SelectionController` that iterates all host items via `getItemCount()`/`getItemKey()` and builds a complete selection set
- Includes early-return guard when all items are already selected (prevents redundant updates)
- Sets `lastSelectedIndex` to the last item for consistent shift-click behavior after select-all
- Changed `app.selectAll` dispatch from `document.execCommand('selectAll')` (browser text selection) to `document.dispatchEvent(new CustomEvent('shortcut:select-all'))` following the existing shortcut event pattern

### Task 2: Wire select-all event listener in track-list, queue-panel, and playlist-view
**Commit:** `906ea28`

- Added `handleSelectAll` arrow method to all three components calling `this.selection.selectAll()`
- Registered `shortcut:select-all` event listener in `connectedCallback()` and cleaned up in `disconnectedCallback()` for all three components
- All three components may respond simultaneously but this is harmless — empty/inactive panels have 0 items and the selectAll early-return guard triggers immediately

## Deviations from Plan

None — plan executed exactly as written.

## Verification Results

| Check | Result |
|-------|--------|
| TypeScript compilation (`tsc --noEmit`) | Pass |
| Lint (`make lint`) | Pass (0 issues) |
| Pre-commit hooks | Pass (frontend-typecheck) |

## Commits

| Task | Commit | Description |
|------|--------|-------------|
| 1 | `f567762` | Add selectAll() to SelectionController and dispatch shortcut:select-all event |
| 2 | `906ea28` | Wire shortcut:select-all listener in track-list, queue-panel, and playlist-view |

## Self-Check: PASSED

- All 5 modified files exist on disk
- Both commits (f567762, 906ea28) verified in git log
- SUMMARY.md created at expected path
