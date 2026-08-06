# 002 — Data lifecycle architecture

**Status:** completed (first tranche); follow-ups tracked below
**Branch:** main
**Created:** 2026-07-26

## Problem

An audit of asset and row cleanup found five leaks, four of which shared
one root cause: **deletion logic was hand-written per call site and lived
far from the thing being deleted.** `RemoveLibrary` knew about ten tables
because someone enumerated them once; migration 32 added an eleventh and
nothing noticed. Files written by `explore` had no cleanup counterpart
anywhere. A function that evicted expired cache rows was written and
never called.

Findings, in severity order:

1. **`RemoveLibrary` was broken for any scanned library.** `tagging_items`
   holds `FOREIGN KEY(library_id) REFERENCES libraries(id)` with no
   `ON DELETE` clause and was never cleared, so `DELETE FROM libraries`
   failed with `FOREIGN KEY constraint failed (787)` and rolled back the
   whole removal. Every scanned library has `tagging_items` rows (the
   scan upserts one per album folder), so this fired on essentially every
   real removal. `RemoveLibrary` had zero test coverage.
2. **Artist images were never deleted by anything.** No `os.Remove` in
   `explore`, no `DELETE FROM artist_images` in the codebase. Unbounded
   in the number of artists ever browsed in Explore, most of whom are not
   in the library.
3. **Cover art size variants leaked on removal.** Only the base
   `cover_art.file_path` was unlinked; the `_sm/_md/_lg` files beside it
   are derived filenames, not rows, so three files per cover survived.
4. **`http_cache` was never pruned.** `Cache.Evict()` existed with no
   callers. Reads filter on `expires_at`, so expired rows were inert but
   accumulated for the life of the install.
5. **Cover-art proxy cache was never pruned.** No eviction, no size cap.

## Approach

Rather than patch five holes, classify the data so the *class* of bug
becomes hard to write. Everything persisted falls on two axes —
regenerability and cost of regeneration — which collapse to four kinds:

| Kind | Regenerable? | Deletion policy |
|---|---|---|
| **Owned** — projection of the user's files | Yes, by rescan | Follows the files |
| **Authored** — user-created, no other copy | **No** | Explicit user action only |
| **Derived** — computed from owned | Yes, cheaply | Free; must never block owned deletion |
| **Cache** — network or dump sourced | Yes, expensively | TTL/age eviction, never cascade |

The classification is not just vocabulary — it produces the right fix for
each finding. Finding 1 is derived data acting as a referential parent of
owned data, which the taxonomy makes categorically illegal. Finding 2 is
cache data that never needed owner-linked cleanup at all; it wants age
eviction. Finding 3 is derived data that must be swept against a live set
rather than tracked individually.

A Go interface was considered and rejected: the only polymorphic consumer
is the janitor, the substrates have nothing in common (SQL rows, an FTS
virtual table, a view, three directories of JPEGs, a 900 MB index), and
provenance is a static fact better enforced by package boundaries than by
methods an implementation may lie about. A declarative catalog gets the
same benefit for a tenth of the cost.

## What shipped

**`backend/datamap`** — the catalog. Every table, view, and asset
directory declared with its `Kind`, its `Lifetime` (`cascade`, `set-null`,
`swept`, `retained`), and a note explaining the classification. Plain data
with no service dependencies, so tests can assert it against a live
schema. FTS5 shadow tables resolve to their parent.

Tests that give it teeth (`backend/datamap/datamap_test.go`):

- `TestCatalogCoversSchema` — every table in `sqlite_master` is claimed by
  exactly one entry. **A new table fails the build until somebody states
  what it is and how it dies.**
- `TestCatalogHasNoStaleEntries` — the reverse, catching drift.
- `TestNoActionForeignKeysAreDeclaredSwept` — a `NO ACTION` foreign key
  blocks its parent's deletion, so its table must declare `swept`. This is
  the exact shape of finding 1, now caught at CI time.
- `TestLifetimesMatchSchema` — declared cascade/set-null must match what
  SQLite actually enforces.
- `TestAuthoredCascadesAreDeliberate` — authored data is unrecoverable, so
  a cascade onto it needs an explicit exemption.

**`backend/maintenance`** — the janitor. A registry of named jobs with
per-job minimum intervals, run at startup-idle and on a 6h tick. Policies
follow the taxonomy: derived data sweeps against a live set, cache data
ages out. Registered in one place (`app.go: startJanitor`) so the full set
of janitorial work is a single visible list.

Jobs: `http-cache-evict` (6h), `covers-sweep` (24h, live set from
`cover_art` expanded via `CoverArtFileSet`), `artist-images-sweep` (24h,
keeps art for library artists indefinitely, evicts browsed-artist art
after 90d), `cover-art-proxy-sweep` (24h, 30d age eviction).

The covers sweep refuses to act on an empty live set — that means the
query failed to see the table, not that every cover is garbage.

**Leak tests** (`backend/library/leak_test.go`) — driven by the catalog
rather than a hardcoded list, so new tables are covered the moment they
are catalogued:

- `TestRemoveLibraryLeavesNoOwnedOrDerivedRows` — removing the only
  library leaves no owned or derived rows, except those in
  `staleTolerated` with a written reason.
- `TestRemoveLibraryPreservesAuthoredData` — authored data survives.
- `TestSweptTablesAreActuallySwept` — a table declaring `swept` that
  nothing sweeps is caught.

All three were verified to fail when the finding-1 fix is reverted.

**Fixes** — `tagging_items` cleared inside the removal transaction
(`crud.go` step 17); `CoverArtFileSet` expands originals to variants and
the legacy `_thumb` name; `Cache.Evict` logic moved into a registered job.

**Incidental:** `Library.emit` — `runtime.EventsEmit` calls `log.Fatalf`
on a context without a Wails runtime, which killed the test binary and
made the whole package untestable. All ten emits in the package now route
through a nil-safe helper. This also removes a real crash risk for
background workers that outlive their context.

## Follow-ups

**`audio_files` is a mixed-kind table.** `play_count`, `last_played`, and
`tag_status` are *authored* data living in an *owned* table. Orphan
cleanup treats the whole row as regenerable, which is why renaming a file
destroys its play count — the row is deleted and re-imported fresh. This
is the strongest argument for splitting authored per-track state into its
own table keyed by something more stable than a path. Related: an
audio-stream content hash (excluding tag blocks, so it survives
retagging) would let a rename be recognised as the same file. Deliberately
out of scope here; it is a schema change plus a rename-detection pass, not
a cleanup fix.

**Cascade adoption.** Fourteen of nineteen foreign keys are `NO ACTION`.
Converting them to `CASCADE` would delete a lot of hand-written orphan
sweeps, but SQLite cannot add `ON DELETE` via `ALTER TABLE` — each needs
the 12-step table rebuild. Note the ordering constraint: cascades delete
rows silently, so any code that collects file paths *before* deleting rows
(as `RemoveLibrary` does for cover art) breaks under cascade. Mark-and-
sweep must land first; the two compose, cascade plus path-collection does
not.

**Consolidate the ten orphan sweeps.** `DELETE ... WHERE id NOT IN (...)`
appears ten times across `crud.go`, `dbsync.go`, `smartplaylist.go`, and
`database.go`. One shared `sweepOrphans(tx)` would shrink the surface where
a new table can be forgotten. Worth doing opportunistically rather than as
a big-bang refactor.

**Storage settings pane.** The catalog knows every table and directory and
its kind; the janitor already computes bytes freed. A settings pane showing
per-kind disk usage with "clear cache" and "rebuild derived data" buttons
is now mostly a UI job.
