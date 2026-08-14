# 012 — What we ask the network for, and what we already had

**Status:** all four findings fixed. Lint (3 configs), Go tests (3
configs), `tsc` and 752 Vitest tests pass; **not driven against the
real app**, so the numbers below are read off the code, not measured.

One claim in the audit was wrong and is corrected in finding 3:
`CheckLibraryMBIDs` is *not* dead — `downloadcatalog.go:152` calls it.
It has no *frontend* caller, which is what was checked and not what was
written.
**Branch:** none yet
**Created:** 2026-08-13
**Related:** 010 (owned albums offline), 011 (owned artists' discography)

---

## Scope

Every frontend call site that can reach the network, and the backend
method behind it. The question asked of each: *is there a local answer
first, and if we do go out, do we go out once for many things or many
times for one?*

## What is already right, and is the standard the rest is measured against

- **Every catalog read is index-first.** `LookupArtist`,
  `LookupReleaseGroup`, `BrowseReleaseGroups`,
  `TopRecordingsForArtist`, `TopReleaseGroupsForArtist`,
  `SimilarArtists` and `ResolveReleaseGroupMBIDs` all answer from
  `explore_index` / `similar_artist_map` and only fall through on a
  miss — several kick a background fetch and return empty rather than
  blocking, with a `*Ready` event to re-read.
- **Album art has the right shape:** seed from the library, one
  `GetThumbnails` batch that is *cached-only by contract*, then
  per-item `GetThumbnail` calls that stream in
  (`explore-view.ts:1445`). Nothing waits on a batch of network
  fetches.
- **Artist art has the right shape in exactly one place:**
  `seedSimilarArtistImagesFromLibrary`
  (`explore-artist-details.ts:1627`) — library store, then disk-only
  `GetArtistImageCachedPath`, fired in parallel, zero network calls.
  It is the model for finding 1.

## Finding 1 — Explore's artist images: no disk check, and serial

`explore-view.ts:1526-1546`. `loadArtistImages` seeds from
`libraryStore.cachedArtists` — i.e. **owned artists only**, which on a
catalog search is a small minority of results — and then, for every
remaining artist:

```ts
const url = await GetArtistImageURL(a.mbid);   // in a for loop
```

Two faults, both fixed by patterns already in the codebase:

- **No cached-path pass.** `GetArtistImageCachedPath` and
  `GetArtistImageCached` are disk-only and free, and neither is used
  here. An artist whose portrait is already on disk from a previous
  search still takes the resolution path.
- **`await` in a loop.** `GetArtistImageURL` is the *resolving* entry
  point: on a miss it does MB artist-rels (on the 1/s artist-image
  limiter) → Wikidata → Wikipedia → a Wikimedia image download. Serial
  awaits mean 8 unresolved artists are 8 of those end to end, each
  blocking the next, while the equivalent album-art path fires all of
  them at once.

The same "resolver used where a cache check belongs" appears at
`top-results-row.ts:218` and `artist-details.ts:207` (both fire in
parallel, so only the first fault applies, and both are small-N).

**Fix:** disk-cached pass first, then network in parallel. A
`GetArtistImagesCached(mbids []string) map[string]string` mirroring
`GetThumbnails` would make it one IPC call instead of N — see finding 4
for why that is not `GetArtistImages`.

## Finding 2 — The artist page prefetches tracklists twice, or four times

`prefetchReleases` (`explore-artist-details.ts:1531`) is called from
**both** `fetchTopReleaseGroups` (:1467) and `fetchReleaseGroups`
(:1506), and `PrefetchReleases` fires up to **8** `BrowseReleases` per
call — the most expensive request the app makes (every version of a
release group, with `recordings` and `media`).

The top release groups are a subset of the discography, so the two
calls are asking about overlapping sets; the backend's
`BrowseReleasesCached` guard stops a *literal* repeat, which means the
second call spends its 8 slots on the next 8 uncached albums rather
than doing nothing. One page view is therefore up to 16 browses — and
on a cold artist, `ArtistDiscographyReady` re-runs both fetchers
(:945, :948), taking it to 32.

Worse, some of that is now provably wasted: since tag-derived
completeness landed (`dcc40b1`), **a complete, MBID-matched album opens
with no catalog call at all**, so warming its tracklist buys nothing.

**Fix, in order of value:**

1. Prefetch once, from the union of both lists, after both resolve.
2. Skip release groups that are owned and complete —
   `GetAlbumCompleteness` already answers this locally.
3. Revisit the cap of 8 with the other two in place. Plan 010 flags
   the same number from the other direction.

## Finding 3 — Batch helpers with no caller (one of which was live)

`CheckLibraryMBIDs`, `GetPopularityBatch` and `GetArtistImages` are
bound to the frontend and have **no call site in `frontend/src`**.
They are the batch shapes a future N+1 would want, and their existence
is presumably why the N+1s above were not noticed.

**`CheckLibraryMBIDs` is not dead** — `downloadcatalog.go:152` calls
it from Go, one MBID at a time. Deleting it broke the build, which is
how that was found; it is kept, with a comment saying who its consumer
is. Read "no frontend caller" as exactly that, and grep both languages
before removing a bound method.

Note `GetArtistImages` is not the helper finding 1 needs: it resolves
names through `libMBID.AllArtistMBIDs()`, so it only answers for
artists **in the library** — the exact set Explore's search results are
not. Either give it an MBID-keyed sibling or replace it.

Also bound with no caller, and worth a separate decision about whether
the feature is live at all: `GetTrackLyrics`, `GenerateMix`,
`GetArtistPlayCount`, `GetLibrarySimilarArtists`,
`GetCandidateThumbnail`.

## Finding 4 — One more background pass with no job and no priority

`BackfillLibraryLyrics` (`lyrics.go:129`) is a bare `go` call: bounded
by passes and per-track (LRCLIB has no batch endpoint, so per-track is
correct), but with no `jobs` registration and no
`WithBackgroundPriority` marking. It runs on its own limiter, so it
starves nothing today — but it is invisible and uncancellable, which is
the gap 011 just closed for the other two backfills.

## Not a finding, recorded so it is not re-audited

- `GetThumbnails` returning only cached entries is deliberate and
  documented; the per-item follow-up is the streaming half, not an
  N+1.
- `explore-artist-details` calling both `TopReleaseGroupsForArtist`
  (50) and `BrowseReleaseGroups` (200) reads overlapping rows from the
  index twice, but both are local queries feeding two different
  sections. Not worth merging.
- The newest components (`home-view`, `catalog-scope-notice`,
  `page-header`, the notification stack, `shortcuts-overlay`) make no
  network calls at all. `home-view` is `GetShelves` + `GetAlbumTracks`,
  both local.

## Done when

- An Explore search with no owned artists in it makes zero artist-image
  network calls for portraits already on disk, and resolves the rest
  concurrently.
- Opening an artist page issues one prefetch pass, over albums that are
  not already fully owned.
- The bound-but-uncalled batch helpers are either wired or removed.
