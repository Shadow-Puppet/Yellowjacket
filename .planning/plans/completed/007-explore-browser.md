# 007 — MusicBrainz/ListenBrainz Explore Browser

> Read-only remote catalog browser: search MB/LB, browse artist pages with ListenBrainz-ranked top tracks and similar artists, view album detail with release-version selection. Rate-limited (1 req/sec), SQLite-cached, with cover art from CAA. Lays the API client + cache foundation that v1.3 Autotagger builds on.

**Shipped:** 2026-03-24 · **Slices:** S01-S04 (M004 in GSD)

## What landed

- **S01 — API clients, cache, explore shell.** `backend/explore/` — MusicBrainz client (wraps `musicbrainzws2`, which provides its own internal rate limiter), ListenBrainz client (custom `RateLimiter` via `golang.org/x/time/rate`, 1 req/sec, burst=1), Cover Art Archive client (URL builders only). All clients follow cache-first pattern: check cache → call API → wrap → cache → return. Migration 11 adds `explore_cache` table (URL key, JSON body, datetime expiry, nullable `mbid` + `entity_type` columns for future autotagger correlation). `idx_explore_cache_mbid` index. TTLs: 24 h for search, 7 d for entity lookups. Sidebar "Explore" entry with globe icon.
- **S02 — Smart search.** Unified `Search()` runs concurrent sub-searches with `WaitGroup` + `Mutex`; frontend renders Top Results / Artists / Albums / Tracks with 300 ms debounce and stale-response discarding via version counter. CAA thumbnails for all result types.
- **S03 — Remote artist page.** `explore-artist-details` (904 LOC). Four independent sections via `Promise.allSettled`: header, top tracks (ListenBrainz `TopRecordingsForArtist` with formatted listen counts), discography (grouped Album/EP/Single/Compilation/Other, newest-first), similar artists (recursive navigation). ListenBrainz Labs `SimilarArtists` failures degrade silently to console.error (it's an experimental endpoint).
- **S04 — Album detail & release versions.** `explore-album-details` (828 LOC). Release fingerprinting (sort tracks by `(discNumber, position)`, join MBIDs with `|`) collapses identical editions into one cluster; track-count min/max labelling shows differences across editions; default selection is the earliest-dated release. Disc separators when any track has `discNumber > 1`.

## Known gap (carried into v1.3 follow-up scope)

- **R032 — offline visual indicator** was in the milestone scope but never implemented. Cache infrastructure works (cached entries served when offline until TTL expires), but `Cache.Get()` doesn't propagate a "from cache" flag to the frontend, and no component renders a "Cached" badge. Functional offline mode works; the UI cue is missing.

## Key decisions retained

- **Wrapper types in `backend/explore/types.go` for Wails bindings**, not raw `musicbrainzws2` types. Avoids ugly TS namespaces and serialization issues with unexported fields.
- **Dual rate limiters** — accept it. The `musicbrainzws2` library has its own that can't easily be disabled or shared; our limiter governs LB + CAA. Both enforce 1 req/sec independently.
- **MBID + entity_type columns in `explore_cache`** — scaffolding for the v1.3 autotagger. No autotagging logic in M004, just storage shape.
- **`CoverArtGroupURL` is a synchronous inline string builder.** Pure deterministic template; making it async via Wails would add per-render round-trips for nothing.
- **Detail components share design tokens via `tokens.css.ts`** but are independent Lit components — no inheritance, no shared base. Data sources differ entirely (local SQLite vs remote API).
