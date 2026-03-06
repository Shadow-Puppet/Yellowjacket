# Phase 9: Scan Cancellation & Keyboard Shortcuts - Context

**Gathered:** 2026-03-06
**Status:** Ready for planning

<domain>
## Phase Boundary

Users can control library scans (cancel/pause/resume) and operate the entire app via configurable keyboard shortcuts. Scans stop gracefully without database corruption, paused scans resume without re-processing. Keyboard shortcuts work out of the box with sensible defaults, are fully customizable via a settings UI, context-aware across three scopes, and suppressed during text input.

</domain>

<decisions>
## Implementation Decisions

### Default key bindings
- Hybrid style: Space/arrows for player controls (no modifier), Ctrl+key for app actions
- Up/Down arrows adjust volume, Left/Right seek within track
- Both `/` and `Ctrl+F` focus the search box
- `Q` toggles the queue panel
- `S` for shuffle, `R` for repeat (single-key player controls)
- `Ctrl+A` for select-all in any multi-select context (track lists, etc.)
- All bindings are configurable — the above are defaults
- Claude fills in remaining defaults (mute, etc.) using common media player conventions

### Shortcut settings UI
- Record-style key capture: click a shortcut row, press the new key combo, it captures live
- Conflicts show a warning with the conflicting action — user chooses to overwrite (old becomes unbound) or cancel
- Shortcuts grouped by category (Player, Navigation, App) in the settings view
- "Reset to defaults" button resets all shortcuts; individual per-shortcut reset also available
- Lives as a "Keyboard Shortcuts" tab within the existing settings dialog

### Context scoping
- Three scopes: Global (always active), Panel-specific (when a panel has focus), Text Input (shortcuts suppressed)
- Global scope: player controls (Space, arrows, S, R, Q, etc.) fire regardless of which panel is focused
- Panel-specific scope: track list gets Enter-to-play and Delete-to-remove when focused
- Text Input scope: only Escape works (blurs the text input) — all other shortcuts suppressed
- No visual scope indicator — relies on natural browser focus behavior; users learn through use

### Scan control UX
- Pause and Cancel buttons placed next to the existing status label, above the existing progress bar in the scanner UI
- On cancel: prompt the user — "Keep X tracks found so far, or discard?" — gives user control over partial results
- On resume after pause: skip already-processed files and continue with remaining — no duplicate work
- Scan control is buttons-only — no keyboard shortcuts for cancel/pause (scans are infrequent)

### Claude's Discretion
- Remaining default key assignments not explicitly discussed (mute, volume step size, etc.)
- Scan progress detail level and error handling during scan
- Loading/disabled states for scan control buttons
- Visual design of the shortcut settings UI (spacing, grouping headers, etc.)
- How the cancel confirmation dialog looks and behaves

</decisions>

<specifics>
## Specific Ideas

- Hybrid key style inspired by media players (Foobar2000/Winamp feel for player controls, standard app conventions for Ctrl+key actions)
- Both `/` and `Ctrl+F` for search — power users get slash, everyone knows Ctrl+F
- Record-style key capture like VS Code's keybinding editor
- Cancel prompt on scan gives user control without losing work

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope

</deferred>

---

*Phase: 09-scan-cancellation-keyboard-shortcuts*
*Context gathered: 2026-03-06*
