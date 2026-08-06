# Notes

Gotchas, measured facts, and things already considered and rejected.
Measurements carry the date they were taken — several of these are
properties of someone else's server and can change.

## MetaBrainz caps a client at ~2 MB/s (measured 2026-07-29)

`data.metabrainz.org` serves a single client at roughly 2.1 MB/s, and
**concurrency does not help**: one Range stream and four concurrent
lanes delivered 32 MB at 2,111,195 B/s and 2,209,000 B/s respectively,
while the same machine pulled 66.9 MB/s from a CDN. One of the four
lanes starved to 0.5 MB/s. The lanes divide a fixed cap; they do not
raise it.

Consequences:

- No client-side concurrency change will speed up a dump download.
  Pushing harder earns 503s (the reason `dumpLanes` is 4).
- Stage 1 of a full import costs ~11.8 h at best (89 GB after column
  projection). Before projection it was 205 GB — about 27 h.

This is the entire reason the catalog is built centrally and shipped as
an artifact rather than derived per install.

## Further stage-1 reductions, not yet taken

Both are CI-side options; neither is safe as a silent client default
because each changes *what gets counted*.

- **Project `recording_mbid` only** (24.1% of row-group bytes instead of
  43.4%): ~49 GB, ~6.5 h. `canonical_musicbrainz_data.csv` already
  carries `recording_mbid`, `release_mbid`, `artist_mbids` and
  `release_group_mbid`, so release/RG/artist counts can be rolled up
  locally. Cost: listens with no recording MBID are dropped, and artist
  totals become "sum of their recordings" rather than direct attribution.
- **Stride-sample members** (1-in-4): ~12 GB, ~1.6 h. The dump is flat
  numbered members (`0.parquet`, …). Sampling is viable because the
  counts only feed a *ranking* for a top-N cut. Must be a stride, never
  a prefix — if members are time-ordered a prefix biases hard toward one
  era.

## Incremental dump retention is 30 days (measured 2026-07-29)

The incremental directory held 30 dumps (series 2579–2610), and full
dumps land roughly monthly. An artifact older than ~30 days cannot be
topped up: the dailies bridging the gap are gone. That is a permanent
undercount of that window, not corruption — but it pins the artifact
republish cadence at monthly.

## Anonymous package download is UNVERIFIED

The client fetches the artifact from a fixed `latest` URL because Gitea's
package *listing* API requires a token while a plain file GET appears not
to — a probe of the not-yet-published artifact returned 404 rather than
401. **That is suggestive, not proof.** No artifact has been published
yet to test against. Confirm before relying on it.

Also worth deciding deliberately: every install pulling from a personal
Gitea makes its bandwidth and uptime a user-facing dependency.

## No migration chain

`applySchema` creates the whole schema from `sql/schemas/*.sql` on every
open; all DDL is `IF NOT EXISTS`. A database written by an older build is
not supported and there is no upgrade path by design.

Two things this replaced, worth not reintroducing:

- The 48-step chain was ~3,700 of `database.go`'s 4,061 lines, plus
  helpers that existed only to serve it (`backupDatabase`,
  `readLibraryDirFromTOML`, `isDuplicateColumnErr`, …).
- `sql/schemas/` had drifted badly from the real schema — it still
  described a `genre_recordings` table that migrations had renamed, and
  omitted `explore_index`, `http_cache`, `artist_images`,
  `similar_artist_map`, `release_to_rg`, `lyrics_index` and
  `artist_metadata` entirely. sqlc reads that directory, so it had been
  generating against a stale schema and silently missed columns such as
  `audio_files.modified_at`.

**When regenerating schema files from a live database, remember the seed
rows.** `file_types` (the four supported extensions), `player_state` and
`queue` each carry `INSERT OR IGNORE` rows that `sqlite_master` does not
contain. Dropping them breaks every audio-file foreign key.

## ANALYZE runs after the catalog merge, not at schema creation

The old migration 45 ran `ANALYZE` once. With the migration chain gone
there is no equivalent moment — an empty database has nothing to measure
— so it runs at the end of the artifact import instead
(`SearchIndex.analyzeIndex`). Without current statistics the planner
mis-estimates the partial expression indexes on `explore_index`
(`idx_explore_title_lower`, `idx_explore_artist_lower`) and scans a
million rows for queries that should seek.

If another path ever populates the catalog, it needs the same call.

## Writers, not readers, are responsible for name quality

`resolveArtistName` falls back to returning the artist MBID when it
cannot find a name. That is fine for a one-off render but must never be
persisted — an MBID stored as a title is unsearchable and shows as a
UUID in the UI.

This used to be defended at every read (`title != mbid` predicates) and
in the upsert's conflict rules. Those defenses are gone; `AddFromCache`
now refuses to write a name equal to the MBID and lets the upsert's
"non-empty wins" rule fill it in when a real name arrives.
`TestAddFromCacheNeverStoresMBIDAsName` guards this.

## Attached databases are invisible to the read pool

`database.DB` holds two handles: a single-writer connection and a
separate query-only pool. `ATTACH` binds to one connection, so anything
touching an attached database must use `ExecContext`/`QueryRowWriter`
(the writer) — `QueryContext` routes to the pool, where the attachment
does not exist and the query fails with "no such table".

## FTS triggers are defined in Go, not in the schema

`explore_index`'s three FTS sync triggers live in
`exploreIndexFTSTriggers` in `database.go` rather than in
`sql/schemas/explore_index.sql`, because the bulk-load path drops and
recreates them (`SuspendExploreIndexFTS`). Defining them in both places
would be two copies free to drift.

Bulk loads must suspend them: measured on a real import, assembly runs at
~31 rows/s with the triggers attached and ~4,700 rows/s without.

## Explore "library only" toggle was removed (2026-08-06)

The Explore UI used to have a "library only" mode toggle
(`frontend/src/store/explore-settings.ts`, `explore:libraryOnly` in
localStorage) that filtered the Explore UI to owned content only. It was
removed outright — the app now always shows full (network-enriched)
Explore data. If offline/library-only mode is wanted again, it should be
built from scratch rather than restored; the old implementation gated
several component code paths in ad hoc ways. A separate
`deep_catalog_enabled` backend flag briefly existed for the same idea and
was removed earlier, when the dump importer left the app binary.
