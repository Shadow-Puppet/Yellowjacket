# 011 — An owned artist's discography, whole and offline

**Status:** built, **not yet verified against a real library**. Lint,
the three Go test configurations, `tsc` and the Vitest suite all pass;
what has *not* happened is a run against a seeded library with real
MusicBrainz traffic, which is the only thing that can show the pass
completing an artist end to end. Do that before moving this to
`completed/`.
**Branch:** main
**Created:** 2026-08-13
**Depends on:** nothing
**Related:** 010 (owned albums, offline) — the same rate limiter, the
next layer down. 010 warms *tracklists*; this warms the *list of
albums*. Read 010's "the rate limiter is the whole design constraint"
section before building either.

---

## The problem

`BackfillLibraryDiscographies` sounds like it does this and does not.
Per owned artist, `indexOneArtist` (`searchindex.go:1910`) fetches from
ListenBrainz:

- `fetchTopReleaseGroups` — capped at `indexMaxRGs` (50)
- `fetchTopRecordings` — capped at `indexMaxRecs` (200)

and both drop anything under `indexMinPopularity` (50 listens). So what
an owned artist's page shows offline is **their fifty most-listened
release groups**, not their discography. For an artist with a long tail
— early EPs, live albums, splits, anything regional — the missing rows
are precisely the ones a user who owns that artist is most likely to be
looking for.

**It is also untyped.** LB's `top-release-groups-for-artist` returns no
secondary types, so the first view of every backfilled artist has no
EP / Live / Compilation / Soundtrack distinction — the discography
renders as one undifferentiated list.

MusicBrainz's browse-by-artist has both the full list and the types,
and `BrowseReleaseGroups` (`explore.go:589`) already knows it: on
finding no secondary types on any indexed row it fires the browse **in
a goroutine, for next time**, and `AddFromCache` writes the result into
the index. So the fix is not new machinery. It is running that call
deliberately, once per owned artist, at scan time instead of
accidentally, on view, one artist at a time.

## What to build

Extend the existing post-scan pass — it is already bounded, resumable,
idempotent and ordered by owned-track count, which is the shape this
needs and the proven one in this codebase.

Per unenriched owned artist, in addition to today's LB fetches:

1. **`BrowseReleaseGroups`, paged to exhaustion.** `musicbrainz.go:318`
   issues a single `Paginator{Limit: MaxLimit}` with no offset loop, so
   a prolific artist is silently truncated at 100 release groups. Page
   until a short response. This is the one change that makes the word
   *full* honest, and it is a change to a function the interactive path
   also calls — which is a win, not a risk.
2. **`SimilarArtists`.** `similar_artist_map` is not in the shipped
   artifact and is filled lazily on view (`explore.go:905`), so it is
   empty for every artist nobody has opened. It is one LB labs call and
   already persists; folding it in here costs a request and removes the
   page's last routine network dependency.

Deliberately **not** in scope: cover art for non-owned release groups.
It is roughly *RGs per artist* fetches rather than one — an order of
magnitude more requests than everything else here combined — and a
missing thumbnail degrades to a placeholder, where a missing release
group degrades to a page that is quietly wrong. Covers stay lazy.

## Four things that bite

**`discog_fetched` is one boolean and would now cover three fetches
with different failure modes.** Today it is set only if an LB fetch
returned rows (`indexOneArtist:1962`), which is the right rule for one
call and useless for three — an MB failure would either permanently
claim the artist as done or force the LB fetches to repeat. Track the
facets separately. Prefer **a new table keyed by artist MBID** over new
`explore_index` columns: `artifactimport.go:95` enumerates the columns
the artifact merge preserves, so a flag column added there is a second
place to remember, and forgetting it silently wipes every mark on the
next artifact update. A new table is also the single-file schema case
(`CREATE TABLE IF NOT EXISTS`, no migration) and needs a `datamap`
entry — `Cache` / `Swept`, since it is re-derivable.

**The `hasSecondaryTypes` heuristic re-fires forever for an artist who
has none.** An artist whose discography is entirely plain albums writes
`secondary_types = ''` on every row, so the "we must be missing them"
test is true on every visit and browses again (cheaply — 7-day
`cacheTTLEntity` — but forever). An explicit per-artist "browsed at"
mark retires the heuristic, which is a second reason for the table
above.

**Popularity is safe, and only because of the upsert rule.**
`AddFromCache` writes `Popularity: 0` for every browsed release group;
`upsertIndexConflictSQL:2180` is "highest wins", so it cannot clobber
the LB figures. The consequence is one to state rather than fix:
`TopReleaseGroupsByArtist` orders by popularity descending, so the deep
cuts this plan adds sort below the top fifty. That is the correct
order.

**The MB limiter is shared — and the priority work this needed is
done.** ~~One `NewRateLimiter()` at 1 req/s serves this,
`PrefetchReleases`, and every interactive browse~~ — 010 says that and
it is wrong on the detail: `e.mb` runs on `mbSearchLimiter`,
`NewRateLimiterBurst(3, 1)`, while the 1/s `NewRateLimiter()` at
`explore.go:84` is the *artist image* limiter. Both were shared with
background work and both are FIFO, which was the real problem.

Shipped ahead of this plan (same session it was written):

- `RateLimiter.WithBackgroundLane(perSecond)` plus
  `WithBackgroundPriority(ctx)` — a marked caller yields entirely while
  any interactive wait is outstanding, and is paced at MB's own 1/s
  rather than the interactive burst rate. The marker is a context value
  so a backfill and a detail page can call the same
  `MusicBrainzClient` method and be treated differently.
- Both existing backfills mark their context, including the artist
  image resolution (`GetArtistImage` takes a `ctx` now for no reason
  other than carrying that marking).
- `jobs.KindCatalogEnrich` and `startBackfillJob` — both backfills are
  registered, cancellable, and show progress. No job is registered
  when there is nothing to do, which is every launch once the library
  is covered.

So this plan inherits the lane: mark the new fetches background and add
them to the existing job's progress. What it must **not** do is treat
"a backfill is now polite" as licence to widen it without measuring —
the yield gate protects latency, not the origin's patience.

## Done when

- An owned artist's page, opened for the first time after a scan,
  renders their complete typed discography with no network call —
  including release groups under the popularity floor and beyond the
  first 100.
- Similar artists render offline for an owned artist nobody has opened.
- An interactive browse issued while the backfill runs is not delayed
  by it.
- The backfill appears in the jobs indicator and can be paused and
  cancelled.
- A second run after a completed one does approximately nothing, and an
  artifact update does not undo a completed one.
