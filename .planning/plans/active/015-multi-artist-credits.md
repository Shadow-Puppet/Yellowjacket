# 015 — Multi-artist credits, navigable

## The problem

A track credited to more than one artist has exactly one navigable
artist in this app, and the others are punctuation.

`audio_files` carries `artist_credit` (the credit as tagged, for
display) and `artist_id` (one artist, for grouping and browsing).
`primaryArtist()` (`backend/library/artistcredit.go:53`) resolves that
one artist by *string-parsing* the credit: it strips a " feat. "
clause, and deliberately does not split on `&`, `x`, `with` or `,`
because those appear inside real artist names. So "Lana Del Rey ft.
Sean Lennon" stores Lana Del Rey and discards Sean Lennon entirely,
and "Alina Baraz & Galimatias" stores one artist whose name is the
whole credit.

### What the measurement says

Measured 2026-08-16 against a real 26,069-file library (19,840 mp3,
6,229 flac; 57 unreadable, m4a/ogg not examined), plus an 80+80
MusicBrainz `inc=artist-credits` sample.

- **13%** of a random sample of the library's recordings have more
  than one credited artist in MusicBrainz (10 of 79 resolved).
  Extrapolates to ~3,250 of the 24,989 files carrying a recording
  MBID.
- **0.86%** of files (224) carry any structured multi-artist signal in
  their own tags. mp3 carries **zero** files with multiple
  `MUSICBRAINZ_ARTISTID` values across 19,840 files; flac has 87.
- **1,286** files say "feat." in `ARTIST`; **1,159 of them (90%)**
  have nothing structured behind it. A sample of 80 such files was
  multi-artist in MB **80 of 80 times**.

CLAUDE.md currently justifies plan 013's removal of `artist_credit` /
`artist_credit_artist` with "3 credits of 2,823 listed more than one
artist". That figure measured **our own writer**, not the library:
`cachedLinkArtist` was called exactly once per credit
(`e7748f1^:backend/library/library.go:1842`), so a collaboration could
never have been recorded, and the three were resolution collisions on
shared credit text. Dropping the join table was still correct — it only
ever held one row, so it was pure join cost — but the stated evidence
does not support "multi-artist is rare". Correcting that claim is part
of this plan.

### Why the tags cannot answer it

Deriving the decomposition locally, with no network, works **79% of the
time** (169 of 215 files with a multi-value `ARTISTS` tag: mp3 69/105,
flac 100/110), and the failures are systematic rather than random:

```
ARTIST  = '2Pac feat. Snoop Dogg, Nate Dogg, Hussein Fatal & Yaki Kadafi'
ARTISTS = ['2Pac', 'Snoop Doggy Dogg', 'Nate Dogg', 'Fatal', 'Yaki Kadafi']
```

`ARTISTS` holds **canonical** artist names; `ARTIST` holds
**as-credited** names. Locating one inside the other fails on
"Snoop Doggy Dogg" vs "Snoop Dogg", on "Fatal" vs "Hussein Fatal", and
on Unicode (`Michel'le` vs `Michel’le`, `K-Ci` vs `K‐Ci` — U+2010, not
a hyphen). That distinction is precisely what a join phrase encodes,
and it is why this cannot be a tag-parsing feature.

Two format details that will mislead anyone re-running the probe:
Picard writes `ARTISTS` **slash-joined into one TXXX frame** on mp3 and
as **true repeated Vorbis keys** on flac, so a probe splitting only on
NUL undercounts mp3 to zero.

## The shape

MusicBrainz models a credit as ordered parts, and the credit *string*
is derived from them — `artist_credit.name` is a cached render, nothing
more. Each participant is `(position, artist, name, join_phrase)`,
where `artist` is the MBID (canonical, what you navigate to) and `name`
is the credited spelling (what you display).

**Join phrases are assembly instructions, not disassembly
instructions.** Rendering is a concatenation, never a search:

```
for each (position, artist_mbid, credited_name, join_phrase):
    emit link(credited_name -> artist_mbid)
    emit text(join_phrase)
```

The link positions are known **by construction**. This is load-bearing:
if we instead located each `credited_name` inside the stored
`artist_credit` text, we would reintroduce the mismatch above — the
stored string may have come from the tags while the parts come from the
catalog, and those **disagree for ~1 in 3 multi-artist files** (61 of
90 sampled credits rendered exactly equal to the tag string).
Divergences seen: `'Skrillex feat. Swae Lee'` tagged vs
`'Skrillex & Swae Lee'` in MB; `'STRFKR'` vs `'Starfucker'`;
`'Zedd feat. Hayley Williams'` vs `'... of Paramore'`. Either MB was
edited after tagging or Picard versions differ; either way the search
would miss or match the wrong span.

So `audio_files.artist_credit` stops being the source of truth and
becomes the **fallback**, used only where there are no parts.

## Where the data comes from

The catalog carries the decomposition; no user ever makes a
per-recording call. Two sources were ruled out first, both cheaply:

- **The canonical dump — which is what CI already pulls
  (`dumpimport.go:84-85`) — does not have it.**
  `canonical_musicbrainz_data.csv` gives `artist_mbids` (ordered list)
  and `artist_credit_name`, but that last column is the *rendered*
  string. Splitting it on CI needs the as-credited names, so CI would
  fail exactly the way a local parse does.
- **The JSON dumps do not cover the catalog.**
  `json-dumps/recording.tar.xz` is 31 MB / 368 MB uncompressed and
  holds **153,691 recordings**, not ~35M. Measured against the test
  library's 24,885 recording MBIDs: **0.00% overlap, zero rows**. It is
  some other subset and is not usable.

That leaves the core dump, **`mbdump.tar.bz2`** (7.1 GB compressed at
the 20260815 export), from
`https://data.metabrainz.org/pub/musicbrainz/data/fullexport/`. Four
members are needed:

| member | why | approx rows |
| --- | --- | --- |
| `mbdump/artist_credit_name` | `(artist_credit, position, artist, name, join_phrase)` — the payload | ~4M |
| `mbdump/artist` | `id -> gid`, since the above references artist *row ids* | ~2.6M |
| `mbdump/recording` | `gid -> artist_credit`, to key credits by recording MBID | ~35M |
| `mbdump/release_group` | same, for album credits | ~2M |

### Coverage is not a concern

Of 24,885 distinct recording MBIDs in the test library, **24,808
(99.7%)** already have an `explore_index` recording row, measured
against a database at 2,052,200 rows — i.e. shipped-artifact coverage,
not a local build's. The popularity filter does not strand the long
tail here.

## Status

- **Phase 1 — done.** `backend/explore/dumpcredits.go` +
  `dumpcreditswrite.go`, wired into `dumpimport.go`'s `run` behind its
  own `credits_import_done` marker.
- **Phase 2 — done.** `cmd/indexexport` writes the two tables;
  `artifactimport.go` reads them behind `artifactHasCredits()`.
- **Phase 4 — done, and it does not need Phase 3.** `explore.GetCredits`
  reads the catalog tables keyed on the *recording* MBID, which both
  sides of the app already carry — a catalog row has one and so does a
  local file (`library.Track.RecordingMBID`). So one binding serves the
  Explore pages and the library's own lists, and all ten artist-link
  call sites render credits today without a local table.
- **Phase 3 (`file_artists`) — not started, and now an
  offline-resilience task rather than a prerequisite.** The table is
  deliberately *not* declared yet: nothing writes or reads it, and a
  schema file plus a datamap note describing behaviour that does not
  exist is a claim the code cannot back. Its remaining
  value is that credits currently vanish when the catalog is absent or
  still downloading, which is precisely the `no-index` state
  `ShelfPage.State` exists to describe. Materialising into
  `file_artists` is what makes a library stand on its own.

**Nothing renders yet in practice**, because no published artifact
carries credit tables — every credit falls back to its single link
until an index build with Phase 1 runs and is exported.

**Column layouts are verified against the real 20260815 export**, not
taken from the schema docs — `artist(id, gid, …)`,
`artist_credit(id, name, artist_count, …)`,
`artist_credit_name(credit, position, artist, name, join_phrase)` and
`recording(id, gid, name, artist_credit, …)` were each read out of the
dump. `release_group` shares `recording`'s first four columns and is
the one layout still taken on trust; `ErrDumpShape` turns a wrong guess
into a loud failure rather than a quietly wrong catalog.

**Still unrun: the ingest against the real 7.1 GB dump.** Everything is
covered by tests over a synthetic tar, which cannot catch a surprise in
the other ~35M rows.

### Phase 1 — Ingest credits on CI

New dump stage in `cmd/indexbuild`, behind the `indexbuild` tag with
the rest of `dumpimport.go`'s stages.

**Constraint from `b98840e`:** `cmd/indexbuild` is built
`CGO_ENABLED=0` in a plain `golang` container and must not reach the
Wails `application` package — `TestIndexToolsDoNotImportWails` walks
`go list -deps -tags indexbuild`. Nothing here should need it, but a
new `ServiceStartup` hook on a package this imports is how it comes
back. Go's `compress/bzip2` is pure Go and decompress-only, which is
all this needs.

**Measured, 20260815 export.** Tar members are **alphabetical**, and
that is favourable: `artist` (435 MB), `artist_credit` (414 MB) and
`artist_credit_name` (237 MB) all fall inside the first ~900 MB
compressed, while `recording` and `release_group` come later. So the
maps are complete before the rows that consume them arrive, and no
recording data is ever buffered.

Pure-Go `compress/bzip2` decompresses at **26 MB/s uncompressed /
8.7 MB/s compressed** (measured on a 250 MB prefix, 3.01x ratio) —
**~13.7 min** for the whole file single-threaded, and less because the
stream can stop after `release_group` rather than reading the
`series`/`tag`/`track`/`url`/`work` tail. The 2 MB/s origin throttle
dominates, as it already does for every other dump here.

Do not, however, *depend* on the ordering: assert it and fall back to
buffering if a future export reorders, rather than silently emitting
nothing.

- `artist` -> `map[int32]uuid16` (~2.6M x ~20 B = ~60 MB)
- `artist_credit_name` -> `map[int32][]creditPart` (~4M x ~40 B =
  ~200 MB)
- `recording` / `release_group` -> emit `gid -> credit_id` **only for
  MBIDs already in `explore_index`** (the kept set is ~1.4M x 16 B =
  ~22 MB), which is what keeps 35M rows from being held

Peak ~300 MB, one sequential pass.

**Only multi-artist credits are stored.** A single-artist credit is
`(name, "")` and is already fully described by `explore_index`'s
`artist_name` / `artist_mbid`; storing it would triple the table for
nothing. Post-filter after loading, once the row count per credit is
known.

New tables (and `datamap` entries, or `TestCatalogCoversSchema` fails
the build — both are `Cache`, matching `explore_index`):

```
artist_credit_part(credit_id, position, artist_mbid, credited_name, join_phrase)
```

with `explore_index.artist_credit_id` as the link. Credits are
**shared** — an album's twelve tracks by one artist share one credit
row — which is the opposite of 013's local verdict, and correctly so:
1:1 in a local library, genuinely many-to-one at 2M-row catalog scale.

### Phase 2 — Ship them in the artifact

`cmd/indexexport` currently creates exactly two tables in the artifact
(`explore_index`, `artifact_meta`, at `cmd/indexexport/*.go:147,170`),
so this is a structural addition, not a column.

Estimated size: ~13% of 1.4M recordings, deduplicated by shared credit,
at ~2.3 parts each — order 400k rows, ~18 MB uncompressed. Against a
~0.6 GB install that is acceptable; it must be measured rather than
assumed before merge.

`artifactimport.go` must read it **only if present**, on the writer
handle where `core` is attached — the `artifactHasTotals()` /
`artifactStoresText()` pattern (`artifactimport.go:145-175`), one step
up from a column to a table. An artifact published before this exists
is still a perfectly good catalog and must import as one that declines
to answer. Adding this to the importer's SELECT list without the probe
is how every already-published artifact starts failing.

`artifactCatalogColumns` gains `artist_credit_id`; it is kept in sync
with the exporter by `TestArtifactColumnsMatchExporter`.

### Phase 3 — Materialize locally

```
file_artists(audio_file_id, position, artist_id, credited_name, join_phrase)
```

`credited_name` is stored **per row**, not looked up from
`artists.name` — that is the Snoop-Doggy-Dogg distinction, and it is
the whole point.

Filled at scan/import time by joining `audio_files.recording_mbid`
against the catalog. **Materialized rather than resolved live**,
because the catalog is a downloaded artifact that can be absent or
still arriving — that is why `ShelfPage.State` has a `no-index` value —
and a library whose track rows lose their artists when the catalog is
missing is worse than today.

That implies a backfill for the case where the catalog arrives *after*
the library was scanned. It registers with `jobs` (progress, cancel)
like every other long pass, and takes a **distinct kind** from
`index-build`, since `job-controls.ts` keys its "you will discard hours
of downloading" confirmation on that kind.

`artists` gains rows for guests who own no files. **This changes what
the artists grid shows** and is an open question below.

### Phase 4 — Render

`utils/explore-link.ts` gains a credit-rendering entry point taking
ordered parts and returning a `TemplateResult`. Every row and detail
view already renders artist names through it, so they inherit
multi-artist links without individually knowing credits exist — the
property that made centralising it worthwhile.

Its existing fallback philosophy already covers the no-parts case: "a
list where some rows are clickable and others silently are not reads as
a bug, not as a statement about metadata." Where there are no parts
(no recording MBID, or no catalog row — ~4% of the test library) render
today's behaviour: the flat `artist_credit` string with one link to the
primary artist. **Do not split the string there.** There is genuinely
no information to split on, and that is the one place the temptation
returns.

`primaryArtist()` stays exactly as it is. It remains the fallback and
is still what `artist_id` means.

## Open questions

1. **Catalog credit vs tagged credit, when they disagree** (~1 in 3
   multi-artist files). Rendering the catalog's decomposition is what
   makes names navigable; preserving the file's is what makes the app
   reflect the user's files. Leaning toward: render the catalog
   decomposition, keep `artist_credit` as the fallback string. Wants a
   deliberate decision, not an accident.
2. **Do guest artists appear in the artists grid?** Phase 3 creates
   `artists` rows for people who own no files. The grid currently means
   "artists in your library" and joins `audio_files`. A guest on one
   track is arguably in the library and arguably not. Whichever way,
   the ownership question stays "is there a file" — that rule does not
   bend.
3. **`release_group` credits** are ingested in the same pass for
   nearly nothing, but album-artist rendering is a separate surface.
   Ship the data in phase 1, render in a follow-up rather than widening
   phase 4.
4. **Our own `tagwriter`** does not write `ARTISTS` or multiple
   `MUSICBRAINZ_ARTISTID` frames, so autotagging a folder degrades the
   very field this rests on — the same shape as the existing
   track-totals note. Out of scope here; worth recording.

## Verification

- Coverage: re-run the library probe and assert `file_artists` is
  populated for ~13% of files, not ~0.9%.
- `TestCatalogCoversSchema` / `TestLifetimesMatchSchema` for the new
  tables.
- `TestIndexToolsDoNotImportWails` still passes with the new stage.
- An artifact **without** the credits table imports cleanly (the
  `artifactHasTotals` regression shape).
- Round-trip: a known multi-artist recording renders each name as a
  separate link with the correct join phrases between them.
