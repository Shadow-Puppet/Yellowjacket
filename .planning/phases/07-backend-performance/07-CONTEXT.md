# Phase 7: Backend Performance - Context

**Gathered:** 2026-03-04
**Status:** Ready for planning

<domain>
## Phase Boundary

Optimize queue persistence and library loading for speed — single-track queue changes should be O(1) instead of O(n), SetQueue Phase 2 should not re-resolve tracks already resolved in Phase 1, and the library store should not block app shell rendering with eager data fetches. This phase covers PERF-01, PERF-02, and PERF-03.

</domain>

<decisions>
## Implementation Decisions

### Queue persistence strategy
- Incremental INSERT/DELETE for single-track operations (AddTrack, RemoveTrack) and insert-at-position operations (InsertNext, InsertNextTracks, InsertTracksAt)
- Bulk operations (SetQueue, Clear, MoveQueueTracks) keep the existing full rewrite (DELETE ALL + batch INSERT) pattern
- Use existing sqlc-generated queries for incremental inserts — do not write new sqlc queries unless existing ones don't cover the case
- After incremental DELETE, UPDATE positions of subsequent tracks to keep positions contiguous (e.g., `UPDATE queue_tracks SET position = position - 1 WHERE position > N`)
- After incremental INSERT-at-position, UPDATE positions of subsequent tracks to shift them (e.g., `UPDATE queue_tracks SET position = position + N WHERE position >= insertPos`)

### SetQueue Phase 2 dedup
- Pass Phase 1's resolved paths as an exclusion set to Phase 2
- Phase 2 calls `lookupTrackMetaBatch` only for paths NOT in the exclusion set (avoiding redundant database lookups)
- Phase 2 receives the Phase 1 result map and merges it with its own results to build the complete track list
- Keep `initialBatchSize` at 50 — no changes to the Phase 1 window size

### Library store lazy loading (PERF-03 — revised scope)
- Remove `eagerFetch()` from the `LibraryStore` constructor — the constructor should not trigger data fetches
- Instead, trigger `eagerFetch()` after the DOM is ready (e.g., from a "ready" event or first connected callback) so the app shell renders instantly before data loads begin
- Still eagerly fetch ALL 4 data types (tracks, albums, artists, genres) once triggered — the intent is faster app shell render, NOT lazy per-view loading. User explicitly wants all views pre-loaded to avoid latency on first view switch
- Post-scan invalidation (`invalidate()`) keeps its current behavior: null all caches and eagerly re-fetch everything
- Use existing `isTracksLoading()`/`isAlbumsLoading()`/etc. flags for loading states — views should show loading state while data arrives

### Claude's Discretion
- Whether to add new sqlc queries for position-shift UPDATEs or use hand-crafted SQL with SAFETY comments
- Exact mechanism for deferring eagerFetch (Wails DOM ready event, Lit `connectedCallback`, or custom app-ready signal)
- Whether `lookupTrackMetaBatch` needs a new overload or if the exclusion set is handled by the caller filtering paths before calling it

</decisions>

<specifics>
## Specific Ideas

- The eager loading of all library views on startup was an intentional UX choice — every view should be pre-loaded so the first switch to a new view has no latency. PERF-03 is about deferring WHEN this happens (after DOM ready), not WHETHER it happens.
- Queue position contiguity matters — positions should not have gaps in the database after incremental operations.

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope.

</deferred>

---

*Phase: 07-backend-performance*
*Context gathered: 2026-03-04*
