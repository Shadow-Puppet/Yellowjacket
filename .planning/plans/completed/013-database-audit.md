# 013 — The database audit

**Status:** **complete** (2026-08-16). R1–R10 landed, the album page
that prompted the audit with them, and the one part of R5 that ships
*in the artifact* — a per-release-group track denominator — landed as
plan 014.
The audit below is unchanged from when it was written — the measurements
describe the *old* shape and are the reason for the new one.
**Branch:** none
**Created:** 2026-08-15
**Supersedes:** the four-part album-page fix sketched in conversation
(it survives, reduced, as R1 and R3 below)
**Related:** 010 (owned albums offline), 011 (owned artists'
discography), 012 (API call audit), 002 (data lifecycle)

---

## Method

Every number here is measured against the **real 25,966-track library**
at `~/.local/share/yellowjacket/yj.db` (copied read-only), not against
a fixture and not inferred from the code. Where a claim rests on a
capability rather than a count — "sqlc can do X" — it was executed, not
assumed.

The brief: *efficiency and simplicity — the minimum required to achieve
our featureset*, with fewer lines and a smaller database as evidence
rather than as the goal. Two named sources of confusion to resolve:
**local versus remote** versions of a thing, **files versus tracks**,
and **indexed versus live** lookups. One added constraint: **avoid
hitting APIs by storing intelligently, without a ridiculous base
install.**

---

## The measurements

### The database is 1.00 GB, and 78% of it is one table

| object | size | rows |
|---|---|---|
| `explore_index` | 383 MB | 2,052,200 |
| its five indexes + `UNIQUE(mbid)` | 395 MB | — |
| its two FTS tables | 85 MB | 2,052,200 + 96,451 |
| `recordings` | 38 MB (27 MB of it lyrics) | 26,778 |
| `lyrics_index` | 18 MB | 24,294 |
| `artist_metadata` | 12 MB | 7,673 |
| `http_cache` | 9 MB | 2,930 |
| `audio_files` | 5 MB | 25,966 |
| everything else | < 10 MB | — |

The local library — the part that is *the user's* — is about 50 MB.
The catalog and its indexes are 780 MB.

### Inside `explore_index`, half the bytes are three text columns

| column | bytes | note |
|---|---|---|
| `mbid` | 70 MB | 36-char text; 16 bytes as a blob |
| `artist_mbid` | 70 MB | same, and it is a foreign key in disguise |
| `caa_release_mbid` | 62 MB | same |
| `entity_type` | 18 MB | three distinct values, stored as words |
| `title` / `artist_name` / `release_name` | 74 MB | real data |

Five columns are declared, shipped in the artifact, selected in every
query, and **empty**: `aliases` (0 rows), `sort_name` (0),
`disambiguation` (0), `country` (69 rows of 2.05 M), `artist_type`
(72). `aliases` is additionally a column in *both* FTS tables, so the
tokenizer indexes nothing, twice.

### Two 50 MB indexes have a `WHERE` clause that excludes 0.3% of rows

`idx_explore_title_lower` (53 MB) and `idx_explore_artist_lower`
(48 MB) are `WHERE popularity > 0`. 2,046,645 of 2,052,200 rows satisfy
that. They are full indexes wearing a partial index's clothes, and they
exist to serve one exact-match tier (`ExactMatches`,
`searchindex.go:1298`) that the champion FTS — 96,451 rows, 2 MB —
already covers the popular half of.

### The local library models many-to-many relationships that are all 1:1

| claim | measured |
|---|---|
| recordings with more than one file | **0** |
| recordings in more than one release group | **0** |
| artist credits with more than one artist | **3** of 2,823 |
| files sharing a recording | **0** |

`recordings` (26,778) is one row per file. `release_group_recordings`
(26,778) is one row per file. `artist_credit` (2,823) and
`artist_credit_artist` (2,826) differ by three.

### …and it leaks rows that outlive the files

| orphan | count |
|---|---|
| `recordings` with no `audio_files` row | **812** (218 carry MBIDs) |
| `release_groups` with no file underneath | **216** |
| `artists` credited on no file | **260** |
| `explore_index` rows flagged **`in_library` with no file behind them** | **129** recordings, 2 release groups, 1 artist |

That last row is the bug reported today, in the user's own data.

### The query surface

| surface | count |
|---|---|
| sqlc queries | 235 (7,850 generated Go lines) |
| raw SQL call sites outside sqlc | 188 |
| bound IPC methods | 272 |
| `X` / `XByLibrary` query twins | 14 (8 of them exposed as separate bindings) |
| copies of the "one row per file with its metadata" projection | **9**, plus the view that already defines it |

`mapTrackRow` takes **22 positional arguments** and is called from 9
places, because each duplicated query generates its own row struct.

### The data directory is 8.5 GB — the database is the small part

| path | size | of which |
|---|---|---|
| `artist-images/` | 5.4 GB | **4,125 MB is candidate images no code path reads**; 1,222 MB is primaries + tiers for **5,770 artists** in a library with **1,301** |
| `covers/` | 1.4 GB | **1,134 MB is originals**; all three rendered tiers together are 110 MB |
| `ffmpeg/` | 283 MB | bundled binary |
| `yj.db` | 1.0 GB | above |
| `yj.db.bak` + `.bak.20260309` | 452 MB | nothing deletes these |
| art caches (`cover-art-cache`, `artist-image-cache`) | 81 MB | catalog art, fine |

The 4.1 GB of unreachable artist candidates is the bug `CLAUDE.md`
records as fixed; this install still carries it, so **the janitor jobs
have never run here**. Worth confirming they run at all before
declaring that one closed.

---

## The diagnosis

Everything below is downstream of one thing.

**There are three different notions of "a track" in this app, and the
code keeps asking the wrong one.**

1. **A file** — a row in `audio_files`. The only thing that is
   unambiguously *yours*: it has a path, it plays.
2. **A local entity** — a row in `recordings` / `release_groups` /
   `artists`. Created by a scan *from* a file, but with an independent
   lifetime: nothing deletes it when the file goes, and retagging a
   file **creates a new one and abandons the old**
   (`library.go:1722` repoints `audio_files.recording_id` at a fresh
   recording; `pruneOrphanedMetadata` only runs on the scan's
   *deleted-file* branch, `library.go:982`). This is where the 812
   orphans come from — and autotagging is the machine that makes them.
3. **A catalog entity** — a row in `explore_index`, downloaded, global,
   identical for every user.

"Is this mine" is asked of **(2)** almost everywhere, and answered by
**(1)** whenever the user actually does something:

- `LibraryMBIDIndex.CheckMBIDs` (`librarymbid.go:64`) is literally
  `SELECT mbid FROM recordings WHERE mbid IN (…)`. It sets `inLibrary`
  on every catalog tracklist.
- `pruneStaleLocalCrossReferences` (`searchindex.go:2480`) clears
  `explore_index.in_library` when the **`recordings` row** disappears —
  not when the file does. Hence 129 phantom "you own this" rows.
- `albumLibraryStatus()` in `explore-album-details.ts` ORs four claims
  of decreasing confidence, none of which is "a file exists".
- But `GetFilePathsByRecordingMBIDs`, which every *action* goes
  through, joins `audio_files`. It is the only one that tells the
  truth.

So a retagged file leaves behind a recording carrying the **old** MBID;
the catalog matches that MBID; the row renders owned, undimmed, with a
Play button; and every action on it fails with "could not be found in
your library" — on a fully-tagged library. The user's instinct that the
check is fragile is correct, and the fragility is not the live lookup.
**The live lookup is the only part that is right.**

The same confusion explains "files vs tracks" and "local vs remote":
tables (2) exist to be a local mirror of the catalog's shape, so a
"track" is sometimes a file, sometimes a mirror row, sometimes a
catalog row, and the three are joined by MBID — a key that **two of the
three can lack or lie about**.

---

## Findings and recommendations

### R1 — Ownership is "a file exists". Say it once, in SQL.

*Cheap, immediate, and it fixes the reported bug.*

- `CheckMBIDs`' `recordings` and `release_groups` branches gain a join
  to `audio_files`. (`artists` too, via credit.)
- `pruneStaleLocalCrossReferences` tests for a file, not for a local
  row.
- `pruneOrphanedMetadata` runs after the retag path as well as the
  delete path — or, better, is deleted along with the tables that need
  it (R2).
- One-shot cleanup of the 812/216/260 existing orphans at open.

**Effect:** 129 lying rows in this library become honest; the class
cannot recur while (2) exists.

### R2 — Collapse the MusicBrainz-shaped local schema into a file-shaped one

*The big one. It is what makes R1 structural rather than a patch.*

The local model imitates MusicBrainz's normalization — `artist_credit`
is an MB concept — for a dataset in which **every relationship it
models is 1:1** (measured above). The cost of that imitation:

- 5 tables (`recordings`, `release_group_recordings`, `artist_credit`,
  `artist_credit_artist`, `release_to_rg` — the last has **0 rows** and
  no schema-file writer) and ~12 indexes.
- A 6-way join in every read, including a `MIN(release_group_id)`
  subquery repeated in **11 places** to undo a many-to-many that never
  happens, and a "first credited artist" subquery in **9** to undo
  another (the row-multiplication bug class documented at length in
  `CLAUDE.md`, which serves 3 rows).
- An orphan-cleanup subsystem (`GetOrphaned*IDs` ×3, `Count*References`
  ×2, `pruneOrphanedMetadata`) that exists only because these rows can
  outlive their file — and which does not actually work (812 orphans).
- The entire phantom-ownership class above.

Proposed shape:

```
audio_files   id, path, library_id, …, title, track_no, disc_no, year,
              composer, comment, artist_credit TEXT, artist_id→artists,
              album_id→albums, recording_mbid, modified_at, …
albums        id, name, artist_id, mbid, year, original_year,
              cover_art_id, total_tracks…      (genuinely many files→1)
artists       id, name, mbid                    (genuinely many→1)
genres + file_genres                            (genuinely many↔many:
                                                 107k rows / 26k files)
```

`artist_credit` survives as **text on the file** (display: "A feat.
B") plus `artist_id` (the primary artist, for grouping) — which is
everything the UI does with it today, minus the join that multiplies
rows.

**Effect:** a row exists iff a file exists, so R1 becomes a foreign key
rather than a rule anyone can forget. Removes 5 tables, ~12 indexes,
~30 sqlc queries, the orphan subsystem, both repeated subqueries, and
the `AUTOMATIC COVERING INDEX` SQLite builds on every library load.
Estimated −1,500 to −2,500 lines across `backend/library`,
`backend/database/sql/*` and `sqlcgen`.

**Cost:** one real migration of user data (not an `ADD COLUMN`), and it
touches autotag, tagwriter, playlist matching and the explore xref.
This is the item to sequence carefully; everything else is independent
of it.

### R3 — One projection, one row type, one mapper

`track_metadata` (the view) already *is* the canonical "one row per
file" definition, and **only the raw-SQL search paths use it**
(`search.go`, `lyrics_search.go`). Every sqlc query re-implements it —
9 copies, which have already drifted: the view prefers
`rg.original_year` for `year`, `GetAllTracksWithFullMetadata` uses
`r.year`. The same library shows a different year depending on which
screen you are on.

**Verified, not assumed:** sqlc generates cleanly against the view —
`SELECT * FROM track_metadata WHERE …` yields one `TrackMetadatum`
struct with correct types (run during this audit).

And the 14 `X`/`XByLibrary` twins collapse into one query each:

```sql
WHERE (CAST(sqlc.arg(library_id) AS INTEGER) = 0
       OR library_id = CAST(sqlc.arg(library_id) AS INTEGER))
```

**Measured cost of the collapse: none.** Scoped-with-OR 23 ms, scoped
direct 21 ms, unscoped 145 ms over the full 26k rows.

**Effect:** −14 queries, −8 bindings, −8 frontend branches, 9 row
structs → 1, 9 call sites of a 22-argument mapper → 1. Roughly −2,000
generated lines and −300 hand-written ones, and the year inconsistency
cannot exist.

### R4 — Put `explore_index` on a diet (~200 MB, no feature loss)

| change | saved |
|---|---|
| `mbid`, `artist_mbid`, `caa_release_mbid` as 16-byte blobs | ~110 MB in the table |
| …and the same keys in `UNIQUE(mbid)` (99 MB) and `idx_explore_index_artist_mbid` (131 MB) | ~70–100 MB |
| `entity_type` → INTEGER | 18 MB + index |
| drop `aliases`, `sort_name`, `disambiguation` (0 rows); reconsider `country`/`artist_type` (69/72 rows) | small bytes, real clarity — and one fewer empty FTS column |
| make the two `LOWER()` indexes' partial predicate *mean* something (`popularity >= championPopThreshold OR in_library`), or retire the tier onto the champion FTS | up to 101 MB |

Better still for `artist_mbid`: it is a foreign key spelled as text.
An integer reference to the artist row is 8 bytes instead of 36 and
makes the 131 MB index a fraction of its size.

**Also worth separating:** `in_library`, `local_*_id`, `is_similar` and
`discog_fetched` are *personalization* stored inside the *shipped
catalog* table, which is why the artifact import has to merge by
explicit column list and why `artist_enrichment` had to become its own
table for exactly this reason. Measured: `in_library` and
`local_*_id IS NOT NULL` agree on **every one of 2,052,200 rows** —
they are the same fact stored twice. A `library_xref(mbid, kind,
local_id)` side table would make the catalog table purely the artifact
and delete the merge-by-column-list rule.

### R5 — Ask the network less, without a bigger install

Present state (from `musicbrainz.go:17-27`): search 24 h, **entity 7
days**, releases 90 days. MusicBrainz entity data changes on the order
of *never* for the fields we read, and 251 of 2,930 cache rows are
already expired on this install — so a fully-populated artist page
re-fetches itself weekly, forever.

- **Raise `cacheTTLEntity` to a year** (or drop expiry and revalidate
  in the background). Cost: bytes already stored. Benefit: the
  steady-state network cost of browsing your own library goes to
  roughly zero.
- **Ship a per-release-group `total_tracks` in the artifact.** 010
  correctly rejects shipping *tracklists* (the per-artist track budget
  would truncate them, and "Play 7 of 9" for a twelve-track album is a
  confident lie). But the **denominator** is one small integer per
  release group — 400,677 rows, ~2 bytes — and it is exactly what
  `albumLibraryStatus`/`ownership()` needs to say complete /
  incomplete / unknown for a catalog album with no local tags. Tiny,
  honest, and it does not depend on coverage.
- **Keep 010's per-user backfill** for the tracklists themselves; this
  does not replace it, it shrinks what it has to cover.
- `http_cache` has no size bound and no vacuum beyond expiry. Give it a
  ceiling.

### R6 — The 5.3 GB on disk that no feature needs

- **4,125 MB of artist candidate images** that nothing reads (the
  documented bug — but the janitors have not run on this install;
  verify they run at all).
- Artist images exist for **5,770 artists** in a **1,301-artist**
  library. Fetching art for artists you do not own is the same
  "prefetch everything" instinct as the discography backfill 011
  corrected.
- **1,134 MB of cover originals** versus 110 MB for all three rendered
  tiers. Nothing renders the original; and it is re-derivable from the
  audio file itself, which is on disk by definition. Keep `_lg` as the
  largest and drop originals — that is 1.1 GB with no visible change.
- `yj.db.bak` (394 MB) and `yj.db.bak.20260309` (58 MB) accumulate with
  nothing to clean them.

This is the largest single win available and it does not touch the
schema.

### R7 — Redundant indexes and dead columns

Five indexes are prefixes of an existing UNIQUE/PK and can be dropped
outright (they cost write time on every insert):

`idx_recording_genres_recording_id` ⊂ `UNIQUE(recording_id, genre_id)` ·
`idx_similar_artist_map_source` ⊂ `PK(source, similar)` ·
`idx_artist_credit_artist_artist_id` ⊂ `UNIQUE(artist_id, credit_id)` ·
`idx_artist_metadata_mbid` ⊂ `PK(mbid, source)` ·
`idx_artist_images_mbid` ⊂ `UNIQUE(artist_mbid, source, source_url)`.

Dead data:

- **`recordings.genre`** — populated on 25,619 rows at every scan and
  **read by nothing**. Every genre read goes through
  `recording_genres` + `genres`. Write-only column.
- **`release_groups.total_tracks` / `total_discs`** — 0 rows populated;
  the feature that needed them put the number on
  `release_group_recordings` instead.
- **`release_to_rg`** — 0 rows, no writer in any schema file.
- `libraries.sql` carries a doc comment about `download_requests`,
  pasted from another file. Small, but it is the kind of drift the
  two-file schema rule exists to catch.

### R8 — One genuine N+1

`mixCandidates` (`explore/mix.go:181`) issues
`GetGenreNamesByFilePath` **per candidate path**, inside a loop over
similar artists, inside a loop over seed artists. Twenty seeds × twenty
similar × thirty paths is 12,000 single-row queries for one mix. It is
one query with an `IN` clause, or one query for the whole weighted set.
(`mixSeedProfile` above it is the same shape, bounded by seed size.)

Nothing else in the tree matches this pattern — a scan of every query
issued inside a loop turned up 72 candidates and this is the only real
one.

### R9 — The IPC surface has internals in it

Bound and reachable from the frontend today: `AcquirePipelineLock`,
`ReleasePipelineLock`, `SetJobRegistry`, `SetScanHooks`,
`SetRescanHooks`, `SetRemovalHooks`, `MusicBrainz`, `CAALimiter`,
`PopulateLocalCrossReferences`. v3's generator binds every exported
method; these want to be unexported or moved off the service type.
Free lines, and one less way to wedge the app from a console.

### R10 — The test DB is not the shape production runs

`NewTestDB` shares one in-memory connection and leaves `readDB` nil, so
`reader()` returns the writer. That is why the read-pool write bug
(documented in `CLAUDE.md`) reached a user, and why
`TestNoWritesOnTheReadPool` had to be a tree-walk instead of a test.
Giving the test DB two handles over one shared in-memory file would let
that be an ordinary test.

---

## What I recommend leaving alone

- **The download subsystem** (requests / downloads / items). Three
  tables, clean lifetimes, well argued in the schema comments. The
  `download_wants` table in this install is the pre-rename name; the
  rename migration will clear it on next launch.
- **The champion FTS.** 96k rows, 2 MB, a real latency tier.
- **The dual write/read handle**, WAL, and the persist-writer queues.
  These are recent, measured, and correct.
- **File paths as the frontend's identity for a track.** Integer ids
  would be cheaper over IPC, but `CLAUDE.md`'s argument (an index goes
  stale on re-sort/refilter, a path does not) is right, and the cost is
  bounded.
- **Storing lyrics locally** (27 MB + 18 MB index for 24k tracks). That
  is the API-avoidance trade working exactly as intended.

---

## What landed (2026-08-15 / 16)

### The third pass: the album page, which is where the report came from

The audit started from a user report — a fully-tagged library saying
"not in your library", on hover rather than on click — and R1 fixed the
half of that which lives in SQL. The other half was the page: ownership
was four claims OR'd into a tick, and the context menu asked the backend
per row, as the menu opened.

`explore-album-details` now resolves the displayed tracklist's file
paths **once**, from `updated()`, into one `filePaths` map that the
badge, the Play count, the dimmed rows and every menu item read. The
synthesised local tracks carry their own `FilePath`, so a library album
costs no lookup at all; a catalog tracklist costs one batched
`GetFilePathsByRecordingMBIDs`. `catalogScope()` no longer returns
`'library'` here — that was the second complaint in the same report, and
the artist page keeps it because a library-only *artist* really is
missing sections.

Two bugs fell out of doing it this way, and neither is the one that was
reported:

- The render loop. Guarding the lookup on `filePaths` (answered) rather
  than on `askedFor` (asked) re-requests every *unowned* MBID forever,
  because an unowned MBID never lands in the map.
- "No release data available" over a tracklist held in memory.
  `loadLocalTracks` rebuilt the version list only when catalog releases
  existed, but the "Your Library" entry is synthesised *from* the local
  tracks — so the no-releases case was the one case it skipped. Nothing
  caught it because the old ownership check answered from the local
  album id and never needed the tracklist to exist.

### The second pass: R5–R10

| | before | after |
|---|---|---|
| the two exact-match indexes | 101 MB | **3 MB** (predicate narrowed to the champion set; plan unchanged, measured) |
| cover art on disk | original + 3 tiers | **3 tiers** — 1,134 MB of a 1.4 GB directory was the original, and nothing rendered it |
| browsed artist art | 90-day expiry, no ceiling | expiry **plus a 256 MB budget**, oldest evicted first; owned artists never in it |
| MusicBrainz entity TTL | 7 days | **1 year**, with a 128 MB ceiling on the response cache |
| redundant indexes | 5 | **0** (3 dropped here, 2 went with their tables) |
| internal methods on the IPC surface | 24 | **0** (`//wails:ignore`; 272 → 248 bound methods) |
| test DB | one handle, `readDB` nil | **two handles**, the shape production runs |

The catalog line is R4, finished the day after: MBIDs stored as 16 raw
bytes and entity types as codes, measured by converting the real
2,052,200-row catalog through the shipped schema. It needed no artifact
rebuild — the importer asks the artifact which encoding it carries and
converts the older text form on the way in. Plan 014 has the detail.

Two of those repaid immediately. Giving the test database its own
read pool **caught three tests writing through it** on the first run —
the exact bug class that reached a user as "attempt to write a readonly
database" and that `TestNoWritesOnTheReadPool` had to walk the source
tree to find. And the artist-image sweep's own test turned out to seed
an `artists` row with no file and call it owned: the phantom this whole
audit is about, sitting in the fixture of the test that guards it.

**One finding in this audit was wrong.** `aliases`, `sort_name`,
`disambiguation`, `country` and `artist_type` are not dead columns. They
are empty on that install because the artist-enrichment pass had barely
run (which is finding 011's subject), but `indexOneArtist` writes all
five, and `aliases` is an FTS column that makes an artist findable by
alias. They stay.

### The first pass: R2, carrying R1 and R3

R2 shipped with R1 and R3 inside it, because the collapse made them
free rather than separate work. No migration: fresh installs only, by
the user's decision, so `sql/migrations/` went with it.

| | before | after |
|---|---|---|
| local tables | 9 | 5 (`audio_files`, `albums`, `artists`, `genres`, `file_genres`) |
| sqlc queries | 235 | 185 |
| generated Go | 7,850 | 6,023 |
| bound IPC methods | 272 | 264 |
| copies of the track projection | 9 + the view | the view |
| `X`/`XByLibrary` query twins | 14 | 0 |
| migration files + runner | 7 + ~120 lines | 0 |
| **net** | | **−5,070 lines** across 122 files |

Gone: `recordings`, `release_group_recordings`, `artist_credit`,
`artist_credit_artist`, `pruneOrphanedMetadata`'s four sweeps,
`RemoveLibrary`'s eight, `mapTrackRow`'s 22 positional arguments, and
340 lines of `tagwriter/dbsync.go` that existed to relink and then
un-orphan those tables.

Ownership is now a file in every one of the places that used to ask a
metadata table: `CheckMBIDs`, `collectLibraryEntities`,
`pruneStaleLocalCrossReferences` and `GetFilePathsByRecordingMBIDs`.

Three things found on the way, each written down where it can be hit
again (`CLAUDE.md`, `references/schema-change.md`):

- **sqlc's parameter rewriter is byte-offset based**, so one em dash in
  a *query* comment corrupts generation into `SELECid`.
- **`sqlc.slice` and `sqlc.arg` do not compose** — slice expansion
  renumbers, so `GetFilePathsByAlbums([1,2], 0)` read album id 2 as the
  library id. Caught by a test, not by a type.
- **`release_to_rg` looked dead and was not**: 0 rows on any ordinary
  install, because only a local `indexbuild` fills it, and the daily
  incremental refresh reads it. Restored.

Verified: `make lint` (3 configurations), `go test ./...` plus the
`indexbuild` and `dev` tag passes, `tsc --noEmit`, `make ui-test`
(768), and a new end-to-end test that scans the real fixture library
and asserts no row outlives its file
(`TestScan_FixtureLibraryLeavesNothingBehind`).

---

## Sequence

**Revised 2026-08-15, after the compatibility constraint was lifted:**
breaking changes are acceptable and the schema may be squashed. That
inverts the order — R2 was last only because of the migration, and it
*subsumes* R1 (ownership becomes a foreign key) and reshapes R3 (the
projection is defined over the new tables). Doing R1 and R3 against the
old shape first would be work thrown away.

1. **R2** — the schema collapse, with the rebuild below. It carries R1
   and R3 with it.
2. **R6** — reclaim the 5.3 GB on disk; confirm the janitors run.
3. **R7 / R9 / R8 / R10** — the small correctness and hygiene items.
4. **R4** — the `explore_index` diet. Artifact rebuild + format bump.
5. **R5** — cache TTLs (trivial) and the shipped denominator (rides
   along with R4's artifact change).

### "Break everything" has a floor, and it is not the schema

Reshaping tables freely is fine. **Dropping the database is not**, and
the numbers say so — a wipe-and-rescan would destroy:

| | count | why a rescan does not restore it |
|---|---|---|
| files marked `user_confirmed` | **25,014** | the user's autotag review decisions |
| reviewed tagging folders (`confirmed`/`skipped`) | **2,109** | ditto, plus every `skipped` becomes pending again |
| rows in `recordings.lyrics` | **24,294** | an unknown share came from **LRCLIB**, not from tags — re-fetching them is precisely the API traffic we are trying to avoid |
| playlists / playlist tracks | 22 / 1,917 | `Authored`; nothing else has them |

So the change ships as a **one-shot in-place rebuild**: create the new
tables, `INSERT … SELECT` across, drop the old ones, in a single
transaction at open. Seconds on 26k rows, ~40 lines of SQL, no
migration *chain* and no rollback path — which is the freedom that was
actually being asked for. `sql/migrations/` gets squashed into
`sql/schemas/` at the same time (`NOTES.md` already blesses this
pre-1.0).

### Two tables are classified as one Kind and hold another

`backend/datamap` already encodes what is safe to lose (`Owned` and
`Derived` rebuild from the files; `Cache` is expensive; `Authored` is
irreplaceable). The audit found two places where the *column* disagrees
with the *table's* entry, which is exactly why a wipe looked cheaper
than it is:

- **`audio_files.tag_status`** — the table is `Owned` (a projection of
  the files), but `user_confirmed` / `user_skipped_permanent` are
  **`Authored`**: a decision the user made that exists nowhere else.
- **`recordings.lyrics`** — the table is `Owned`, but lyrics fetched by
  the LRCLIB backfill are **`Cache`**, and nothing records which of the
  24,294 rows came from a tag and which from the network.

The new schema fixes both by construction: lyrics move to their own
MBID-keyed table with a `source` column (so they survive any rebuild of
the owned tables, and the provenance question becomes answerable), and
`tag_status`' authored values are carried across explicitly rather than
recomputed.

**Expected outcome if all of it lands:** database ~1.0 GB → ~0.75 GB,
data directory 8.5 GB → ~2.5 GB, sqlc queries 235 → ~180, generated Go
7,850 → ~5,000, bound methods 272 → ~255, and — the part that matters —
one definition of "this is mine" that a file either satisfies or does
not.

## The open questions, answered

1. **R2's migration** — the user's call, and it was "just assume this
   new version will only be installed by a new user". So there is no
   in-place rebuild and no chain: `sql/schemas/` is the whole
   description. An existing `YJ_HOME` does not open (its `audio_files`
   has `recording_id` and none of the tag columns, and
   `CREATE TABLE IF NOT EXISTS` cannot add them) — delete and rescan,
   and rebuild any seed with `make sandbox-seed`.
2. **R4's artifact format** — no break was needed. The importer asks
   the artifact what it carries rather than trusting a version, so the
   published text-form artifact still imports. Plan 014 has it.
3. **Yes, the janitors run.** `Runner.Start` calls `RunDue` immediately
   and `lastRun` is in-memory, so every launch runs everything due.
   The 4.1 GB survived because `OrphanedArtistImagesJob` joined a bare
   MBID onto a *sharded* directory — deleting the rows and leaving the
   files, which is worse than not running — and because
   `StrayArtistImageFilesJob` did not exist. Both are fixed; it was a
   bug report, not a cleanup.

## Measured on the finished refactor

| | expected | actual |
|---|---|---|
| sqlc queries | ~180 | **185** |
| generated Go | ~5,000 | **6,024** |
| bound methods | ~255 | **248** |
| `explore_index` + indexes | — | **780 MB → 405 MB** |

## The one recommendation not taken

R4's "better still" for `artist_mbid`: an integer reference to the
artist row (8 bytes) rather than the 16 raw bytes it now stores. It is
a further ~30 MB on `idx_explore_index_artist_mbid`, and the reason to
stop short is that the *artifact* carries MBIDs and not local ids, so
the import would have to resolve every row against a table it is in the
middle of filling. Worth its own argument, not a footnote to this one.
