# 010 — Owned albums, offline

**Status:** not started — and **much smaller than when it was written**
**Branch:** none yet
**Created:** 2026-08-13
**Depends on:** nothing
**Related:** the `AlbumReleasesFailed` fix that prompted it, and the
tag-derived completeness that landed after it (same session)

---

## What already shipped, and what it leaves

The common case is solved without this plan. `GetAlbumCompleteness`
reads the "5/12" denominator off the files' own tags — persisted to
`release_group_recordings.total_tracks`, having been extracted at every
scan since forever and discarded — and an album that is **MBID-matched
and complete** now opens with **no catalog call at all**. Identity from
the MBID, tracklist from the tags; those were the two things the browse
was being spent on.

So the set this plan still has to serve is not "albums you own a track
of". It is:

- albums that are genuinely **incomplete** (the catalog is the only way
  to say *which* tracks are missing — tags give the count, not the
  names), and
- albums whose tags **never declared a total**, where completeness is
  unknowable locally and the catalog is the only source.

On a well-tagged library that is a small minority, which changes the
economics below considerably: the run is shorter, and the rate limiter
contention that dominates this design is proportionally less severe.
Re-measure before building — the answer may now be "the prefetch is
enough".

---

## The problem

Opening an album detail page for an album **you already own** hits
MusicBrainz. Every time it is not in the response cache, which for most
of a library is every time, because nothing warms that cache except a
capped prefetch on the artist page.

The user's framing: *this is a classic example of an album we should
have had locally.*

## Why we do not have it, despite the discography backfill

`BackfillLibraryDiscographies` / `EnsureArtistDiscography`
(`backend/explore/searchindex.go:301`, `:397`) do less than the name
suggests. Per artist, `indexOneArtist` fetches:

- `fetchTopReleaseGroups` — capped at `indexMaxRGs` (50)
- `fetchTopRecordings` — capped at `indexMaxRecs` (200)

and writes them as **flat `explore_index` rows**. There is no release
group → tracklist relation anywhere in the index, and no release-level
rows at all. `explore_index` recordings carry `caa_release_mbid` and
`release_name`, which name the release used for cover art — not a
tracklist.

So "we have full discographies for library artists" means *we know
which albums the artist made, offline*. It has never meant we know
what is on any of them.

The only store of release-level catalog data in the app is `http_cache`
under `mb:browse:releases:<rg>` (90-day TTL, `musicbrainz.go:27`),
populated **only** by a live `BrowseReleases` with
`Includes: ["recordings", "media"]` at `MaxLimit` — the most expensive
call the app makes to MusicBrainz. It is warmed by exactly one thing:
`PrefetchReleases` (`explore.go:746`), capped at 8, called only when an
artist page renders.

An album opened from the library grid therefore always browses live.

## What to build

**A post-scan backfill that warms the release cache for release groups
that are owned but not known-complete** — bounded, resumable, and
shaped exactly like `BackfillLibraryDiscographies`, which is the proven
pattern for this in the codebase.

The scoping rule is the user's and it is the right one: not "every
album by every artist in the library" (50 release groups per artist,
mostly never opened) but albums with owned tracks — narrowed further,
now, to the ones a local answer cannot already cover. The query gains
one clause: skip release groups whose `GetAlbumCompleteness` reports
`complete`.

Sketch:

1. A query for release groups with ≥1 owned track and no warm release
   cache entry. `release_groups.mbid` is the key; the owned-track join
   is `audio_files → recordings → release_group_recordings`, the same
   shape `unenrichedLibraryArtistMBIDs` already uses one table over.
2. Order by owned-track count descending, so the albums the user has
   most of are warmed first — same reasoning as the discography
   backfill's ordering, same benefit if a run is cut short.
3. Run through `releasesSF`, so it never double-fetches a release group
   an interactive open is already handling.
4. Bound a run (`discogBackfillMaxPerRun` has a value to copy) and make
   it resumable: the resume marker is the response cache itself —
   `BrowseReleasesCached` already answers "is this one done", so unlike
   the discography path this needs **no new flag column**.
5. Trigger it where `BackfillLibraryDiscographies` is triggered, and
   register it with `jobs` so it has progress, pause and cancel like
   every other long-running operation.

### The rate limiter is the whole design constraint

One shared `NewRateLimiter()` at 1 req/s (`explore.go:84`) serves this,
`PrefetchReleases`, and every interactive browse. A backfill over a
few thousand owned albums is *hours* of wall clock at that rate — which
is fine for a background job, and not fine if it starves the album page
the user is looking at right now.

That is the real work in this plan, and it is not the query:

- Interactive browses need to **jump the queue**. Today they cannot;
  there is one limiter and it is FIFO.
- `PrefetchReleases`' cap of 8 was sized when nothing else competed for
  the limiter. Revisit it in the same change.
- The 60 s fallback the `AlbumReleasesFailed` fix installed is sized
  for today's contention. If a backfill can queue behind it, that
  number is wrong again — which is an argument for priority, not for a
  bigger number.

Do not start the query until the priority question has an answer.

## The alternative that was considered and rejected

**Project release-group tracklists in the dump build and ship them in
the artifact.** The data is there: `canonical_musicbrainz_data.csv`
carries `release_mbid` *and* `recording_mbid`
(`dumpcatalog.go:520`), and `release_to_rg` already maps release →
release group. It is derivable from bytes the index build already
streams, with no new API surface at all, and it would work offline on
first launch with no per-user backfill.

It is rejected **for this plan** because the artifact is built
centrally and is byte-identical for every user, so "albums the user
owns a track of" cannot be a filter on it. Shipping tracklists for the
whole catalog means per-recording rows against a ~900 MB artifact
budget (~426 B/row measured), and gating on a popularity floor means it
is absent for exactly the obscure albums a local backfill would have
covered.

Worse than absent, in fact — and this is the argument that actually
kills it. The floor is not one number over artists; it is a **per
artist track budget** (`dumpcatalog.go:58-89`): 50 tracks for a tier-A
artist, 25 for tier B, 12 for tier C. A projected tracklist would
therefore be *whichever* of an album's tracks survived that budget,
with nothing marking the rest as absent — so the album page would count
owned against a truncated denominator and render "Play 7 of 9" for a
twelve-track album. That is a confident lie, where the honest states
this plan's alternative produces (complete / incomplete / unknown) are
at worst silent.

Note that `markLibraryArtists` (`dumpcatalog.go:246`) already grants
every library artist full coverage — 500 tracks, 100 release groups —
by reading the local library, so the per-user tailoring this option
supposedly cannot have does exist in code. It is a no-op in the CI
build (empty library), and reaching it means a **local** dump build:
the ~205 GB, half-a-day download the entire artifact design exists to
avoid. Whoever finds that function next should read this paragraph
before getting excited about it.

Worth revisiting if the artifact ever gains per-user tailoring, or if a
measurement shows the row count is smaller than feared. Note it also
yields the *canonical* tracklist rather than MusicBrainz's full version
list, so the versions dropdown would still browse live when opened.

## Done when

- Opening an owned album that has never been opened before renders its
  catalog tracklist with no network call, after one backfill run.
- An interactive browse issued while the backfill is running is not
  delayed by it.
- The backfill appears in the jobs indicator, and can be paused and
  cancelled there.
- A second run after a completed one does approximately nothing.
