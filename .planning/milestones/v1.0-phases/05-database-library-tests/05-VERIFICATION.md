---
phase: 05-database-library-tests
verified: 2026-03-04T16:48:00Z
status: passed
score: 25/25 must-haves verified
re_verification: false
---

# Phase 5: Database & Library Tests Verification Report

**Phase Goal:** Database queries (especially FTS5 search) and library scan logic have unit tests that lock down current behavior before SQL consolidation and performance optimization
**Verified:** 2026-03-04T16:48:00Z
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

#### Plan 05-01: FTS5 Search Tests (database package)

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | SearchFTS returns correct results for basic term queries | ✓ VERIFIED | TestSearchFTS_BasicTerm passes — searches "queen", asserts ≥2 results including "Bohemian Rhapsody" and "Another One Bites the Dust" |
| 2 | SearchFTS returns nil for empty queries | ✓ VERIFIED | TestSearchFTS_EmptyQuery passes — tests both "" and "   " (whitespace-only), asserts nil return |
| 3 | SearchFTS handles special characters (quotes, slashes like AC/DC) without error | ✓ VERIFIED | TestSearchFTS_SpecialCharacters passes — searches "AC/DC" and `back"in`, no errors, AC/DC track found |
| 4 | SearchFTS multi-word queries match across title/artist/album columns | ✓ VERIFIED | TestSearchFTS_MultiWord passes — "bohemian rhapsody" returns "Bohemian Rhapsody" as top result |
| 5 | SearchFTSByFilename scopes search to file_path column only | ✓ VERIFIED | TestSearchFTSByFilename passes — "bohemian_rhapsody.mp3" finds Bohemian Rhapsody; empty basename returns nil |
| 6 | SearchFTSTracks returns full 16-column track metadata | ✓ VERIFIED | TestSearchFTSTracks passes — validates all 16 fields: FilePath, LengthMilliseconds, Title, ArtistName, TrackNumber, DiscNumber, Album, Genre, Year, Composer, FileType, SampleRate, BitDepth, Channels, Bitrate, FileSize |
| 7 | FTS5 search ranking produces consistent BM25 ordering for known data | ✓ VERIFIED | TestSearchFTS_Ranking passes — "back in black" returns title+album match as top result |
| 8 | Diacritics search works (Beyonce finds Beyoncé) | ✓ VERIFIED | TestSearchFTS_Diacritics passes — "Beyonce" (no accent) finds Artist="Beyoncé" |
| 9 | RebuildSearchIndex repopulates the index from audio_files data | ✓ VERIFIED | TestRebuildSearchIndex passes — seeds data without search_index, calls RebuildSearchIndex(), SearchFTS then finds "Rebuild Track" |
| 10 | tokeniseForFTS and buildFTSQuery produce correct FTS5 query syntax | ✓ VERIFIED | TestTokeniseForFTS (9 subtests) and TestBuildFTSQuery (3 subtests) all pass — covers separators, quotes, empty strings |
| 11 | Schema migrations run successfully on a fresh database | ✓ VERIFIED | TestMigrationsApplied passes — user_version ≥ 3, UNIQUE constraint on artist_credit_artist enforced |
| 12 | All tests pass with -race flag | ✓ VERIFIED | `go test -race ./database/ -v -count=1` — all 15 top-level tests PASS (31 total including subtests) |

#### Plan 05-02: Library Scan Tests (library package)

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 13 | Entity cache returns cached value on second call (no DB hit) | ✓ VERIFIED | TestCachedUpsertArtistCredit passes — second call returns same ID, cache.artistCredits has 2 entries |
| 14 | cachedLinkArtist skips duplicate INSERT when linkedCredits cache hit | ✓ VERIFIED | TestCachedLinkArtist passes — second call same args, linkedCredits stays at 1 entry |
| 15 | cachedLinkArtist silently ignores UNIQUE constraint violations from DB | ✓ VERIFIED | TestCachedLinkArtist_MultiCredit passes — same artist linked to 2 credits, no errors |
| 16 | cachedUpsertGenre returns cached genre on repeated calls | ✓ VERIFIED | TestCachedUpsertGenre passes — second call returns same ID, cache.genres has 1 entry |
| 17 | resolveReleaseGroup returns cached release group and updates cover art if new art available | ✓ VERIFIED | TestResolveReleaseGroup passes — first call no art, second call adds cover art, CoverArtID updated on cached entry |
| 18 | getRecordingName falls back to filename when title is empty | ✓ VERIFIED | TestGetRecordingName passes — 3 subtests: title present, empty→filename sans extension, complex path |
| 19 | toNullInt64 treats 0 as null, non-zero as valid | ✓ VERIFIED | TestToNullInt64 passes — 0→{Valid:false}, 5→{Int64:5,Valid:true}, -1→{Int64:-1,Valid:true} |
| 20 | toNullString treats empty as null, non-empty as valid | ✓ VERIFIED | TestToNullString passes — ""→{Valid:false}, "rock"→{String:"rock",Valid:true} |
| 21 | splitGenres splits on \|\| delimiter correctly | ✓ VERIFIED | TestSplitGenres passes — 4 subtests: empty→nil, single, multiple, two genres |
| 22 | mapTrackRow maps all 16 columns correctly including NullInt64 fields | ✓ VERIFIED | TestMapTrackRow passes — validates all 16 fields plus NullInt64 Valid=false→0 case |
| 23 | Orphan deletion removes audio_file and search_index entries | ✓ VERIFIED | TestOrphanDeletion passes — DeleteAudioFile removes row; DeleteSearchIndex documents contentless FTS5 limitation |
| 24 | Entity cache functions work with plain context.Context (no Wails dependency) | ✓ VERIFIED | setupTestLibrary uses t.Context(), all 8 entity cache tests pass without Wails runtime |
| 25 | All tests pass with -race flag | ✓ VERIFIED | `go test -race ./library/ -v -count=1` — all 18 top-level tests PASS (33 total including subtests) |

**Score:** 25/25 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `backend/database/search_test.go` | FTS5 search tests, pure helper tests, migration tests, rebuild tests (min 300 lines) | ✓ VERIFIED | 821 lines, 15 top-level test functions, 31 tests including subtests |
| `backend/library/scan_test.go` | Entity cache tests, pure helper tests, orphan cleanup tests (min 300 lines) | ✓ VERIFIED | 718 lines (new scan tests), 13 new test functions (18 total with pre-existing config tests) |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `search_test.go` | `search.go` | `SearchFTS\|SearchFTSByFilename\|SearchFTSTracks\|tokeniseForFTS\|buildFTSQuery\|stripExtForSearch` | ✓ WIRED | 73 matches — all 6 functions called directly in tests (same package, internal tests) |
| `search_test.go` | `testhelper.go` | `NewTestDB` | ✓ WIRED | 12 calls to NewTestDB(t) across 12 DB-backed test functions |
| `scan_test.go` | `library.go` | `cachedUpsertArtistCredit\|cachedLinkArtist\|cachedUpsertGenre\|resolveReleaseGroup\|getRecordingName\|toNullInt64\|toNullString` | ✓ WIRED | 35 matches — all 7 functions called directly (plus resolveAlbumArtistCredit, 4 matches) |
| `scan_test.go` | `query.go` | `splitGenres\|mapTrackRow` | ✓ WIRED | 6 matches — both functions called directly in tests |
| `scan_test.go` | `database/testhelper.go` | `database.NewTestDB(t)` | ✓ WIRED | 1 call in setupTestLibrary helper, used by all DB-backed tests |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|------------|-------------|--------|----------|
| TEST-03 | 05-01-PLAN | Database package has unit tests covering FTS5 search queries (basic, empty, special characters), search index rebuild, and schema migrations (~10-15 tests) | ✓ SATISFIED | 15 top-level test functions in search_test.go: 3 pure helper (tokenise, buildFTSQuery, stripExt), 7 FTS5 search (basic, empty, special chars, multi-word, diacritics, ranking, filename), 3 index ops (insert/delete, rebuild, clear), 1 migration, plus seedSearchData helper. All pass with -race. |
| TEST-06 | 05-02-PLAN | Library scan logic has unit tests covering metadata processing, entity cache behavior, and orphan cleanup (~10-15 tests) | ✓ SATISFIED | 13 new test functions in scan_test.go: 5 pure helpers (getRecordingName, toNullInt64, toNullString, splitGenres, mapTrackRow), 6 entity cache (upsertArtistCredit, linkArtist, linkArtist multi-credit, upsertGenre, resolveReleaseGroup, resolveReleaseGroup cache hit), 1 orphan deletion, 1 empty fields. All pass with -race. |

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| — | — | None found | — | — |

No TODO/FIXME/PLACEHOLDER markers, no empty implementations, no stub returns in either test file.

### Human Verification Required

None — all truths are programmatically verifiable via test execution and code inspection. Tests exercise real SQLite databases (in-memory via NewTestDB), real FTS5 queries with real BM25 ranking, and real entity cache operations.

### Gaps Summary

No gaps found. All 25 must-have truths verified across both plans:

- **15 database package tests** lock down FTS5 search behavior (basic term, empty query, special characters, multi-word, diacritics, ranking), search index operations (insert, rebuild, clear), pure helpers (tokenise, buildFTSQuery, stripExt), and schema migrations.
- **13 library package tests** lock down entity cache behavior (artist credit, link artist, genre, release group), pure helpers (getRecordingName, toNullInt64, toNullString, splitGenres, mapTrackRow), orphan cleanup, and empty metadata handling.
- All tests pass with `-race` flag.
- Both required artifacts exist and are substantive (821 and 718 lines respectively).
- All key links are wired — test functions call production functions directly via same-package internal tests.
- Both requirements (TEST-03, TEST-06) satisfied with no orphaned requirements.

---

_Verified: 2026-03-04T16:48:00Z_
_Verifier: Claude (gsd-verifier)_
