---
phase: 08-frontend-performance-ux
plan: 04
subsystem: frontend
tags: [lit, design-tokens, css-custom-properties, px-spacing, icon-tokens, type-scale, visual-consistency]

# Dependency graph
requires:
  - phase: 08-frontend-performance-ux
    provides: "Design token CSS custom properties (tokens.css.ts) from Plan 01"
provides:
  - "All 15 components use design token CSS custom properties for icon sizing and type scale"
  - "Sidebar fully converted from em-based to px-based spacing"
  - "Cover-grid dynamic text sizing tiers mapped to type scale tokens"
  - "Consistent visual language across all views"
affects: []

# Tech tracking
tech-stack:
  added: []
  patterns: ["designTokens import + static styles array pattern applied across all components"]

key-files:
  created: []
  modified:
    - frontend/src/components/sidebar/app-sidebar.ts
    - frontend/src/components/now-playing/now-playing.ts
    - frontend/src/components/search-bar/search-bar.ts
    - frontend/src/components/audio-player/controls/player-controls.ts
    - frontend/src/components/audio-player/seekbar/seek-bar.ts
    - frontend/src/components/audio-player/volume-control/volume-control.ts
    - frontend/src/components/audio-player/audio-player.ts
    - frontend/src/components/cover-grid/cover-grid.ts
    - frontend/src/components/cover-grid/cover-grid-styles.ts
    - frontend/src/components/track-list/track-list.ts
    - frontend/src/components/queue-panel/queue-panel.ts
    - frontend/src/components/track-details/track-details.ts
    - frontend/src/components/track-info/track-info.ts
    - frontend/src/components/artist-details/artist-details.ts
    - frontend/src/components/genre-details/genre-details.ts

key-decisions:
  - "em→px conversion uses 16px base (standard browser default) for sidebar spacing"
  - "Icon tokens: --yj-icon-sm (14px) for small indicators, --yj-icon-md (18px) for sidebar/player controls, --yj-icon-lg (24px) for cover placeholders"
  - "Cover-grid dynamic text tiers mapped to --yj-text-xs/sm/md/lg tokens via updateSizeProperties()"

patterns-established:
  - "Design token adoption pattern: import designTokens, prepend to static styles array, replace ad-hoc px/em values with var(--yj-*) references"
  - "All font-size and icon font-size values use --yj-text-* and --yj-icon-* tokens respectively"

requirements-completed: [UX-01]

# Metrics
duration: 8min
completed: 2026-03-05
---

# Phase 8 Plan 04: Visual Consistency Audit & Token Application Summary

**Systematic em→px conversion and design token application across 15 components — sidebar spacing, icon sizing via --yj-icon-* tokens, and typography via --yj-text-* tokens for coherent visual language**

## Performance

- **Duration:** ~8 min (across sessions with checkpoint)
- **Started:** 2026-03-05T04:30:00Z
- **Completed:** 2026-03-05T14:13:19Z
- **Tasks:** 3 (2 auto + 1 human-verify checkpoint)
- **Files modified:** 15

## Accomplishments
- Sidebar fully converted from em-based spacing (padding: 1em, gap: 0.6em) to px-based values — eliminates compound inheritance issues
- All icon sizes across 15 components now use --yj-icon-sm/md/lg tokens instead of ad-hoc pixel or em values
- All text sizes use --yj-text-xs/sm/md/lg/xl tokens instead of hardcoded font-size values
- Cover-grid dynamic text sizing tiers in updateSizeProperties() mapped to type scale tokens
- Human-verified visual consistency across all views — sidebar, track list, cover grid, queue panel, now playing, search bar, audio player, and detail views

## Task Commits

Each task was committed atomically:

1. **Task 1: Convert sidebar em→px and apply icon/type tokens to sidebar, now-playing, search-bar, audio-player** - `aed90d7` (feat)
2. **Task 2: Apply design tokens to cover-grid, track-list, queue-panel, and detail components** - `1303422` (feat)
3. **Task 3: Visual consistency verification** - checkpoint:human-verify (approved, no commit)

**Hotfix during phase:** `72ef719` (fix) — revert repeat() inside lit-virtualizer, restore .renderItem + .keyFunction

## Files Created/Modified
- `frontend/src/components/sidebar/app-sidebar.ts` - em→px spacing conversion, --yj-icon-md for nav icons, --yj-text-* for labels
- `frontend/src/components/now-playing/now-playing.ts` - --yj-icon-lg for cover placeholder, --yj-text-* for track info
- `frontend/src/components/search-bar/search-bar.ts` - --yj-icon-sm for search icon, --yj-text-md for input
- `frontend/src/components/audio-player/audio-player.ts` - designTokens import, type tokens
- `frontend/src/components/audio-player/controls/player-controls.ts` - --yj-icon-* for transport controls
- `frontend/src/components/audio-player/seekbar/seek-bar.ts` - --yj-text-* for time labels
- `frontend/src/components/audio-player/volume-control/volume-control.ts` - --yj-icon-* for volume icon
- `frontend/src/components/cover-grid/cover-grid.ts` - Dynamic text tiers mapped to --yj-text-xs/sm/md/lg
- `frontend/src/components/cover-grid/cover-grid-styles.ts` - Type token adoption in base styles
- `frontend/src/components/track-list/track-list.ts` - --yj-text-* for headers/cells, --yj-icon-sm for favorites
- `frontend/src/components/queue-panel/queue-panel.ts` - --yj-text-* and --yj-icon-* tokens
- `frontend/src/components/track-details/track-details.ts` - Type and icon tokens for detail layout
- `frontend/src/components/track-info/track-info.ts` - Type tokens for track metadata display
- `frontend/src/components/artist-details/artist-details.ts` - Type and icon tokens
- `frontend/src/components/genre-details/genre-details.ts` - Type and icon tokens

## Decisions Made
- **em→px conversion uses 16px base:** Standard browser default font size — 1em ≈ 16px, 0.5em ≈ 8px, 0.6em ≈ 10px. This eliminates compound inheritance issues where nested em values compound unexpectedly.
- **Icon token mapping:** --yj-icon-sm (14px) for small indicators like favorites star and search icon, --yj-icon-md (18px) for sidebar navigation and player controls, --yj-icon-lg (24px) for cover art placeholders.
- **Cover-grid dynamic tiers use tokens:** updateSizeProperties() maps card-size tiers to token values (small → --yj-text-xs, medium → --yj-text-sm, large → --yj-text-md/lg) instead of hardcoded pixel values.

## Deviations from Plan

None for the plan's own tasks — plan 04 executed exactly as written.

### Critical Hotfix (Plan 08-02 regression)

**[Rule 1 - Bug] repeat() directive inside lit-virtualizer defeated virtualization**
- **Found during:** Phase 8 execution (between plans 03 and 04)
- **Issue:** Plan 08-02 migrated all 7 lit-virtualizer instances to use repeat() as child content. However, repeat() renders ALL items as DOM children, bypassing lit-virtualizer's viewport-based rendering. This caused 2+ minute loading times and UI freezing with large libraries.
- **Root cause:** lit-virtualizer's .renderItem and .keyFunction properties integrate with its scroll-based viewport management. When content is provided as children (via repeat()), the virtualizer loses control of which items are rendered.
- **Fix:** Reverted all 7 virtualizer instances to use .renderItem + .keyFunction properties (the proper lit-virtualizer API). Removed repeat() from all virtualizer elements.
- **Files modified:** frontend/src/components/track-list/track-list.ts, frontend/src/components/queue-panel/queue-panel.ts, frontend/src/components/cover-grid/cover-grid.ts, frontend/src/components/artists-view/artists-view.ts, frontend/src/components/genres-view/genres-view.ts
- **Verification:** App loads instantly with large library, virtualization confirmed working (only visible items rendered)
- **Committed in:** `72ef719`

---

**Total deviations:** 1 hotfix (critical bug from prior plan)
**Impact on plan:** Hotfix was prerequisite for meaningful visual testing — without it, the app was unusable with real data.

## Issues Encountered
- The repeat() virtualizer regression from Plan 08-02 caused 2-minute load times with large libraries. This was a fundamental API misuse — lit-virtualizer requires .renderItem/.keyFunction for virtualization, not repeat() child content. Fixed before Plan 04 visual verification could proceed.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Phase 8 complete — all 4 plans executed
- All 26 consolidation milestone requirements delivered
- Ready for milestone completion

## Self-Check: PASSED

All 15 key files verified on disk. All 3 task/hotfix commits (aed90d7, 1303422, 72ef719) verified in git history.

---
*Phase: 08-frontend-performance-ux*
*Completed: 2026-03-05*
