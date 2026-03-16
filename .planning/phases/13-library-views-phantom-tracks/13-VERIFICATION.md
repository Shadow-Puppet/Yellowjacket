---
phase: 13-library-views-phantom-tracks
verified: 2026-03-16T15:00:00Z
status: passed
score: 5/5 must-haves verified
re_verification: false
---

# Phase 13: Library Views & Phantom Tracks Verification Report

**Phase Goal:** Users experience a unified multi-library presentation with optional filtering and graceful playlist preservation
**Verified:** 2026-03-16T15:00:00Z
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths (from ROADMAP Success Criteria)

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | The default track list shows tracks from all libraries merged — the user sees their complete collection as one unified view | ✓ VERIFIED | `library-store.ts:41` — `selectedLibraryIdValue: number | null = null` (null = all). `getTracks()` at L133-136: when `id === null`, calls `GetAllTracks()` (unfiltered). Same pattern for albums (L162-164), artists (L190-192), genres (L218-220). |
| 2 | User can select a specific library from a filter control and all views (tracks, albums, artists, genres) show only that library's content | ✓ VERIFIED | `library-filter.ts` (107 lines) — `<select>` dropdown in top bar with "All Libraries" default + per-library options. `setSelectedLibrary()` calls `invalidate()` which clears all caches + resets scroll positions + triggers `eagerFetch()`. Each store method dispatches to `ByLibrary` variant when `selectedLibraryIdValue !== null`. Detail views wire through: `cover-grid.ts:980-987` (GetAlbumTracksByLibrary), `genre-details.ts:183-193` (GetTracksByGenreByLibrary), `genres-view.ts:737-743` (GetTracksByGenreByLibrary), `artists-view.ts:939-952` (GetAlbumTracksByLibrary). |
| 3 | Search results respect the active library filter | ✓ VERIFIED | Per SUMMARY key-decision: client-side `rankTracks()` operates on already-filtered track data from library store. When library filter is active, `getTracks()` returns library-scoped data, so search inherits the filter automatically without needing a separate `SearchTracksByLibrary` backend call. Backend `SearchTracksByLibrary` method exists (query.go:784-827, search.go:292-375) as a fallback capability. |
| 4 | Playlists can contain tracks from multiple libraries — adding tracks from different libraries to the same playlist works naturally | ✓ VERIFIED | `playlist-view/` and `playlist-details/` have zero imports of `selectedLibraryId` or `setSelectedLibrary`. playlist-details imports `libraryStore` only for `getCachedTracks()` and `getCachedAlbums()` (cover art resolution in track-details dialog, L433-454). Playlist data flows from `playlist.Service` (Go) which is library-agnostic — no library_id filtering. `playlist_tracks.sql` schema has nullable `audio_file_id` with `ON DELETE SET NULL` for phantom support. |
| 5 | When a library is removed, its tracks in playlists become phantom entries — visually distinguished with preserved metadata instead of disappearing | ✓ VERIFIED | **Schema:** `playlist_tracks.sql:6-11` — phantom_title, phantom_artist, phantom_album, phantom_duration_ms, phantom_genre, phantom_cover_art_path columns. `phantom_file_path` column (L12, migration 7). **Backend:** `crud.go:248` stores `phantom_file_path` on removal. `playlist.go:1497-1504` — `ResolvePhantomTracksAfterScan()` method. `app.go:176-178` — ScanHooks wiring. **Frontend:** `playlist-details.ts` has 41 phantom-related lines — `.phantom` CSS class, phantom-resolver component import, locate/remove actions. |

**Score:** 5/5 truths verified

### Required Artifacts (Plan 13-01)

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `backend/database/sql/queries/audio_files.sql` | GetAllTracksWithFullMetadataByLibrary, GetAudioFilesByReleaseGroupByLibrary | ✓ VERIFIED | L139-169 (ByLibrary variant with `WHERE af.library_id = ?`), L204-235 (ByReleaseGroupByLibrary with `AND af.library_id = ?`) |
| `backend/database/sql/queries/release_groups.sql` | GetAllAlbumsWithDetailsByLibrary, GetAlbumsByArtistByLibrary | ✓ VERIFIED | L67-91 (ByLibrary with IN-subquery), L114-140 (ByArtistByLibrary with IN-subquery + artist filter) |
| `backend/database/sql/queries/artists.sql` | GetAlbumArtistsByLibrary | ✓ VERIFIED | L42-58 (ByLibrary with IN-subquery through artist_credit_artist → release_groups → recordings → audio_files) |
| `backend/database/sql/queries/genres.sql` | GetTracksByGenreByLibrary, GetAllGenresWithCountsByLibrary | ✓ VERIFIED | L66-104 (ByLibrary with `AND af.library_id = ?`), L113-121 (CountsByLibrary with JOIN through recordings → audio_files) |
| `backend/database/search.go` | SearchFTSTracksByLibrary | ✓ VERIFIED | L290-375 — full method with `AND tm.library_id = ?` in WHERE clause, SAFETY comment, parameterized query |
| `backend/library/query.go` | 8 ByLibrary methods on Library struct | ✓ VERIFIED | GetAllTracksByLibrary (L449-497), GetAllAlbumsByLibrary (L500-549), GetAllArtistsByLibrary (L553-587), GetAlbumsByArtistByLibrary (L591-646), GetAllGenresWithCountsByLibrary (L650-678), GetTracksByGenreByLibrary (L682-729), GetAlbumTracksByLibrary (L733-780), SearchTracksByLibrary (L784-827) — all exported, on exported struct, Wails-bindable |

### Required Artifacts (Plan 13-02)

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `frontend/src/components/library-filter/library-filter.ts` | Library filter dropdown (min 60 lines) | ✓ VERIFIED | 107 lines. Lit component with native `<select>`, design tokens, "All Libraries" default, per-library options, HTMLElementTagNameMap registration |
| `frontend/src/store/library-store.ts` | selectedLibraryId state + filtered fetch logic | ✓ VERIFIED | L41 `selectedLibraryIdValue`, L322-331 getter/setter, conditional dispatch in getTracks/getAlbums/getArtists/getGenres/getAlbumsByArtist, L333-344 getLibraries() |
| `frontend/src/store/controllers/library-controller.ts` | selectedLibraryId getter/setter pass-through | ✓ VERIFIED | L136-146 — `selectedLibraryId` getter, `setSelectedLibrary()`, `getLibraries()` |
| `frontend/index.html` | `<library-filter>` in top bar | ✓ VERIFIED | L18 — `<library-filter></library-filter>` between `<hgroup>` and `<search-bar>` |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `library-store.ts` | `@go/library/Library` | Conditional GetAllTracksByLibrary/GetAllTracks call | ✓ WIRED | L8-13 imports ByLibrary bindings; L133-136 dispatches based on `selectedLibraryIdValue` |
| `library-filter.ts` | `library-store.ts` | `libraryCtrl.setSelectedLibrary()` | ✓ WIRED | L75 calls `this.libraryCtrl.setSelectedLibrary(id)` on change event; L80 reads `this.libraryCtrl.selectedLibraryId` |
| `track-list.ts` / browse views | `library-store.ts` | `libraryCtrl.getTracks()` (library-aware) | ✓ WIRED | Store dispatches correct variant; all detail views (genre-details, cover-grid, artists-view, genres-view) check `libraryCtrl.selectedLibraryId` and call ByLibrary variants |
| `library/query.go` | `database/sql/queries/*.sql` | sqlc-generated Queries methods | ✓ WIRED | Each Go method calls `l.db.Queries.Get*ByLibrary(...)` — e.g., L452 `GetAllTracksWithFullMetadataByLibrary`, L503 `GetAllAlbumsWithDetailsByLibrary` |
| `library/query.go` | `database/search.go` | `l.db.SearchFTSTracksByLibrary` | ✓ WIRED | L787-788 calls `l.db.SearchFTSTracksByLibrary(query, searchTrackLimit, libraryID)` |
| `app.go` | `playlist/playlist.go` | ScanHooks.ResolvePhantoms | ✓ WIRED | L176-178 `yj.library.SetScanHooks(library.ScanHooks{ResolvePhantoms: yj.playlist.ResolvePhantomTracksAfterScan})` |
| `library/crud.go` | `playlist_tracks` | phantom_file_path storage on removal | ✓ WIRED | L248 `phantom_file_path = sub.file_path` in UPDATE during library removal |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|------------|-------------|--------|----------|
| VIEW-01 | 13-01, 13-02 | Default view shows tracks from all libraries merged | ✓ SATISFIED | `selectedLibraryIdValue` defaults to null; `GetAllTracks()` called when null |
| VIEW-02 | 13-01, 13-02 | User can filter to a specific library | ✓ SATISFIED | `<library-filter>` dropdown + conditional ByLibrary dispatch in all store methods |
| VIEW-03 | 13-01, 13-02 | Browse views (albums, artists, genres) work filtered | ✓ SATISFIED | 7 ByLibrary SQL queries + 8 Go methods + conditional frontend dispatch in all views |
| VIEW-04 | 13-01, 13-02 | Search respects active library filter | ✓ SATISFIED | Client-side search on filtered data; backend SearchFTSTracksByLibrary exists as capability |
| PLAY-01 | 13-02 | Cross-library playlists | ✓ SATISFIED | Playlist service is library-agnostic; playlist-view/playlist-details have no library filter dependency |
| PLAY-02 | 13-02 | Phantom tracks when library removed | ✓ SATISFIED | phantom columns in schema, phantom_file_path for resolution, ScanHooks wiring, ResolvePhantomTracksAfterScan method |
| PLAY-03 | 13-02 | Phantom tracks visually distinguished | ✓ SATISFIED | `.track-item.phantom` CSS styling in playlist-details, phantom-resolver component with locate/remove actions |

**Orphaned requirements:** None. All 7 requirement IDs (VIEW-01 through VIEW-04, PLAY-01 through PLAY-03) appear in PLAN frontmatter and are traced in REQUIREMENTS.md to Phase 13.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| — | — | None found | — | No TODOs, FIXMEs, placeholders, empty implementations, or stub patterns detected in any key artifact |

### Human Verification Required

All automated checks pass. The following items require human testing for complete confidence:

### 1. Library Filter Visual Presentation

**Test:** Run app with `wails dev -tags webkit2_41`, verify dropdown appears in top bar between title and search bar
**Expected:** Compact 32px dropdown with "All Libraries" default, design-token styling
**Why human:** Visual appearance and positioning cannot be verified programmatically

### 2. Filter Responsiveness

**Test:** Select a specific library from the dropdown
**Expected:** All views (tracks, albums, artists, genres) immediately refresh with only that library's content; loading skeleton shows briefly
**Why human:** UI responsiveness, loading state timing, and data correctness require visual inspection

### 3. Phantom Track Display After Library Removal

**Test:** Add tracks from library A to a playlist, then remove library A
**Expected:** Tracks become phantom entries (greyed out, warning icon), metadata preserved, locate/remove buttons visible
**Why human:** Visual phantom styling and resolver behavior need interactive testing

### 4. Phantom Auto-Resolution After Re-Scan

**Test:** Remove library → re-add same library → scan → check playlists
**Expected:** Previously phantom tracks automatically resolve back to real tracks
**Why human:** End-to-end flow through ScanHooks callback and M3U8 path matching

### 5. Filter Reset on App Restart

**Test:** Select a library filter, restart the app
**Expected:** Filter resets to "All Libraries" (no persistence)
**Why human:** Requires app restart cycle

**Note:** Plan 13-02 included a human checkpoint (Task 2) that was marked APPROVED during execution. The SUMMARY documents comprehensive end-to-end verification was performed during development.

### Gaps Summary

No gaps found. All 5 ROADMAP success criteria are satisfied. All 7 requirement IDs (VIEW-01 through VIEW-04, PLAY-01 through PLAY-03) have implementation evidence in the codebase:

- **Backend:** 7 ByLibrary SQL queries + 8 Go wrapper methods + 1 FTS search method + phantom resolution infrastructure
- **Frontend:** Library filter dropdown component (107 lines) + store with conditional ByLibrary dispatch + controller pass-through + wiring in all browse views and detail views
- **Phantom tracks:** Schema columns + phantom_file_path storage on removal + ScanHooks-based auto-resolution + existing phantom UI in playlist-details
- **Playlists isolated:** playlist-view and playlist-details have no library filter dependency — confirmed by grep showing zero `selectedLibraryId`/`setSelectedLibrary` references
- **Scroll reset:** `invalidate()` resets all scrollPositions to 0 on filter change
- **No persistence:** `selectedLibraryIdValue` defaults to null, no localStorage/backend persistence code

All commits verified in git history: `5cc58ce`, `5f7de50`, `42b8cf9`, `f05d2bb`, `93262b9`, `9f595b7`.

---

_Verified: 2026-03-16T15:00:00Z_
_Verifier: Claude (gsd-verifier)_
