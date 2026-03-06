# Phase 8: Frontend Performance & UX - Context

**Gathered:** 2026-03-04
**Status:** Ready for planning

<domain>
## Phase Boundary

Make the app feel smooth and visually consistent — large libraries (10k+ tracks) render without jank during scrolling, view switching, and search filtering, and the UI follows a coherent visual language across all components. This is the final phase of the consolidation milestone.

Performance work targets: Lit `repeat()` directive with stable keys for DOM reuse, `queueMicrotask()` debouncing for store notifications during rapid updates. Visual work targets: audit and fix spacing, colors, typography, and icon sizing inconsistencies.

</domain>

<decisions>
## Implementation Decisions

### Visual consistency scope
- Full audit of every component — check for hardcoded colors, inconsistent spacing, mismatched typography, and icon sizing
- Systematic pass, not just known issues

### Spacing units
- Converge all components to px-based spacing (not em/rem)
- The sidebar currently uses em-based spacing (padding: 0.5em, gap: 0.6em) — convert to px
- Track-list and cover-grid already use px — these are the reference pattern

### Icon sizing
- Define a CSS custom properties scale: --yj-icon-sm, --yj-icon-md, --yj-icon-lg (and apply consistently)
- Replace ad-hoc values (0.9em in sidebar, 12px in track-list favorites, 24px in now-playing) with scale tokens

### Typography
- Define a type scale via CSS custom properties (--yj-text-xs through --yj-text-lg)
- Apply everywhere — eliminate meaningless variations (e.g., 12px vs 13px in sort labels should pick one)
- Album name scaling with card size (11-16px tiers in cover-grid) should map to the type scale tokens

### Store notification debouncing
- Apply queueMicrotask() debouncing to library store only — it's the only store with rapid-fire updates (scan events)
- Queue, player, playlist stores stay with immediate synchronous notifications (user-driven, not rapid)
- Coalesce ALL library store notifications (data fetches, cover size changes, scroll position) through one debounced notify()
- Transparent to subscribers — same subscribe() API, debouncing is an internal optimization
- No partial progress during scan — one coalesced update after all data loads is acceptable

### Large library rendering
- Reference identity check is sufficient for detecting data changes (lastTracksRef !== cached pattern already exists)
- No deep equality checking
- Debounce search input ~150ms before triggering filter/rank computation on large datasets
- Aim for instant view switches — no loading skeletons needed (virtualizer only renders visible items, data is pre-cached via eagerFetch)
- Full optimization pass on per-row rendering: repeat() keys + reduce per-row allocations (cache class strings, pre-compute column values, minimize template computation in renderTrackRow)

### Rendering strategy
- Switch from .items/.renderItem pattern to repeat(items, keyFn, renderFn) directive in all virtualizer-based components
- Stable key strategy:
  - track-list: FilePath (unique per track)
  - cover-grid: album.ID (already has gridKeyFunction — convert to repeat())
  - queue-panel: QueueTrack.id (unique per queue entry, handles duplicate tracks)
  - playlist-view: uses track-list component (inherits FilePath key)
- Apply to ALL lit-virtualizer components, not just library views

### Claude's Discretion
- Exact px values for the icon scale (--yj-icon-sm: 14px? 16px? Claude decides)
- Exact px values for the type scale (--yj-text-xs through --yj-text-lg ranges)
- Which specific visual inconsistencies to fix during the audit — Claude identifies them
- Whether to extract CSS custom property definitions into a shared file or keep them in :root
- Search debounce exact timing (guideline: ~150ms, but Claude can adjust based on feel)
- How to handle cover-grid's dynamic text sizing tiers (size-small class, cardTextHeight) within the type scale

</decisions>

<specifics>
## Specific Ideas

- The cover-grid already has a gridKeyFunction using `a-${entry.album.ID}` — this should be migrated to the repeat() directive pattern rather than the .keyFunction property
- QueueTrack has an `id` field that uniquely identifies each queue entry even when the same track appears multiple times — use this as the queue repeat() key
- The library store's notify() currently does `this.subscribers.forEach((callback) => callback())` — the queueMicrotask wrapper should coalesce multiple notify() calls within the same microtask tick into a single subscriber notification round
- Track-list's renderTrackRow does class string concatenation and column mapping on every render call — the full optimization pass should address this

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope

</deferred>

---

*Phase: 08-frontend-performance-ux*
*Context gathered: 2026-03-04*
