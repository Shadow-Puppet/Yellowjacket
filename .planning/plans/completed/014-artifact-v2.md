# 014 — The catalog's compact encoding, and the denominator it owed

**Status:** **complete** (2026-08-16). The encoding landed first; the
per-release-group `total_tracks` denominator landed with the album page
that spends it.
**Branch:** none
**Created:** 2026-08-16
**Depends on:** nothing
**Related:** 013 (the database audit, which measured all of this), 010
(owned albums offline), 001 (ship core index)

---

## The encoding

Measured on the real 2,052,200-row catalog, converting it through the
shipped schema (not a projection):

| object | before | after |
|---|---|---|
| `explore_index` | 383 MB | **242 MB** |
| `idx_explore_index_artist_mbid` | 131 MB | **65 MB** |
| `UNIQUE(mbid)` | 99 MB | **54 MB** |
| `idx_explore_index_entity_pop` | 47 MB | **28 MB** |
| `idx_explore_caa_release` | 17 MB | **11 MB** |
| the two `LOWER()` indexes | 101 MB | **3 MB** (013) |
| **total** | **780 MB** | **405 MB** |

Every row converted with the `CHECK` constraints live, which is also a
result: no MBID in a real 2 M-row catalog is malformed.

**No format bump, and no rebuilt artifact needed.** The importer asks
the artifact what encoding it carries (`typeof(mbid)`) and converts on
the way in if it is the old text form, so the artifact already
published keeps working and the exporter switches whenever CI next
runs. That is strictly better than the version negotiation this plan
originally proposed.

The silent-failure risk the plan was written around was handled by
making the failure loud instead of by avoiding the change: a `CHECK` on
the column turns a stringly write into an error at the insert, the
22-column projection became one constant and one scanner instead of
four copies, and `TestStoredEncodingRoundTrips` sweeps every read path
in the package. It found one real bug on its first run — the artifact
probe was asking the read pool, where the attached artifact does not
exist.

## The denominator

`total_tracks` on `explore_index`, ~2 bytes across 400,677 release
groups. It makes "do I have all of this" answerable offline for an
album whose **files declared no total**, which is a great deal of any
untagged library and the one thing `GetAlbumCompleteness` cannot answer
from tags. 010 rightly rejected shipping whole tracklists — the
per-artist track budget truncates them, and a truncated tracklist is a
confident lie about which tracks exist. A denominator has no such
problem, and the album page spends it as one: the numerator stays
local (distinct track numbers on disk), only the denominator is
borrowed, and only where the tags have none.

Four things about it are load-bearing.

**It is counted before the popularity filter.** `cmd/indexbuild` counts
the canonical dump's rows per kept release, which is that release's
track count because the dump carries one row per recording per
canonical release. Counting the *kept* recordings instead would say
"9" about a twelve-track album whose other three nobody has played —
worse than saying nothing, and the same class of lie as the truncated
tracklist. `TestDumpImportEndToEnd` has an unplayed track on a fixture
album for exactly this: three tracks in the total, two indexed as
recordings.

**Zero means "the catalog does not say"**, which is the same third
state the local answer already has. An album neither side can total
wears no ring rather than a wrong one.

**Adding a column to the importer's SELECT is how you break every
artifact already published.** `artifactHasTotals()` asks the attached
artifact whether the column exists, the same way and on the same handle
as `artifactStoresText()`, and selects a literal `0` when it does not.
Verified by forcing the probe true: the older shape then fails with
`no such column: total_tracks`, which is what a shipped build would
have done to a file nobody can re-cut retroactively.

**A test seeder that binds the upsert's parameters by hand is not
"breaking where the app breaks".** Three of them did, on the argument
that a schema change should fail the tests in the same place — and what
it actually produced was `missing argument with index 25`, three files
at a time, for a column none of them cares about. They go through
`upsertBatch` now, which is the one writer, and keep the property they
wanted: a field written to the wrong column still fails there.

## Done when

- [x] `GetAlbumCompleteness`'s gap is answerable for a catalog album the
      library has no tags for, with no network call.
- [x] The artifact grows by less than a megabyte (~800 kB at 400,677
      release groups).
- [x] An artifact published before the column still imports.
