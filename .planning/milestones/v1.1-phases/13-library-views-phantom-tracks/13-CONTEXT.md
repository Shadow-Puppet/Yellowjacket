# Phase 13: Library Views & Phantom Tracks - Context

**Gathered:** 2026-03-16
**Status:** Ready for planning

<domain>
## Phase Boundary

Filtered presentation across all views, search, and phantom track display. The default view shows all libraries merged (unified). Users can filter to a specific library via a dropdown, and all browse views (tracks, albums, artists, genres) plus search respect that filter. Playlists can contain tracks from multiple libraries. When a library is removed, its playlist tracks become phantom entries using the existing phantom infrastructure.

</domain>

<decisions>
## Implementation Decisions

### Library filter placement & interaction
- Compact dropdown select control in the top bar, next to the search bar
- Shows current selection (default: "All Libraries"), click to open list of libraries
- Filter selection persists across view changes within a session (resets on app restart — no localStorage persistence)
- Search respects the active library filter — searching with a library selected returns only matches from that library; "All Libraries" searches everything

### Filter behavior across views
- Backend filtering — new SQL queries with library_id WHERE clauses, not frontend JS filtering (matches existing roadmap decision for 150K+ track collections)
- When filtering to one library, only show artists/albums/genres that have at least one track in that library — entities with zero tracks in the selected library are hidden
- Detail views (artist detail, album detail, genre detail) also respect the active library filter
- Switching the library filter triggers a backend re-fetch with brief loading state (existing loading skeleton) — no per-library caching
- Scroll positions reset when switching library filter (new data set)

### Phantom track appearance
- Use existing phantom track infrastructure already built in playlist-details component — no new visual treatment needed
- Existing `.track-item.phantom` CSS styling, phantom-resolver component, locate/remove actions all apply
- Full phantom resolver available for library-removal phantoms (users can re-locate/re-match)
- Preserve title, artist, album metadata in phantom columns (matches existing phantom_title, phantom_artist, phantom_album schema columns)
- Phantom tracks are included in playlist track counts (total count, not split)

### Cross-library playlist behavior
- Playlists always show all tracks regardless of the active library filter — playlists are user-curated, not filtered
- No per-track library indicator in playlist views — tracks look the same regardless of source library
- Adding tracks to a playlist works identically whether from a filtered or unfiltered view — no special confirmation
- Queue matches the filter context — playing from a filtered view populates the queue with only that library's tracks

### Claude's Discretion
- Exact dropdown styling and animation (should match existing design tokens)
- How "All Libraries" vs specific library queries are structured internally (separate SQL queries vs parameterized)
- Loading skeleton behavior during filter switch transitions
- How library filter state is stored in the frontend (new store, extension of existing store, etc.)

</decisions>

<specifics>
## Specific Ideas

- Phantom track infrastructure is already built in `playlist-details.ts` — phantom CSS classes, phantom-resolver component, locate/remove buttons. Library-removal phantoms should flow through the same rendering path (is_phantom = 1 with cached metadata columns).
- The existing `GetAudioFilesByLibrary` SQL query already exists but isn't wired into the main view data path. Backend filtering will need similar filtered queries for albums, artists, genres, and FTS search.
- The `track_metadata` VIEW already includes `library_id` — can JOIN on it for filtered queries.
- Phase 12 already pre-populates phantom metadata BEFORE cascade delete, so the phantom columns should already be filled correctly when a library is removed.

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope

</deferred>

---

*Phase: 13-library-views-phantom-tracks*
*Context gathered: 2026-03-16*
