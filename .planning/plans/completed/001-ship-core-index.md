# 001 — Ship a prebuilt "core" explore index

**Status:** complete
**Branch:** cleanup/fresh-start-schema
**Created:** 2026-07-25
**Completed:** 2026-07-30

## Outcome

A fresh install downloads a 70.6 MB artifact and merges 1,076,133 rows
in ~43 s, instead of streaming 205 GB over ~27 h. The dump importer that
produces the artifact left the app binary entirely — it is behind the
`indexbuild` build tag and runs only in CI.

Phase 5 landed differently than planned: rather than a user-facing
setting gating the deep import, the deep import is simply not in the
app. `deep_catalog_enabled` existed briefly and was removed with it.

Two things remain unverified or undone, both recorded in
`.planning/NOTES.md`: anonymous package download on git.ljones.me has
not been confirmed against a real published artifact, and installs whose
index was built by older code (no `listens_applied_series`) have no
rescue path — though with no migration chain, those databases are now
unsupported anyway.

## Problem

A fresh install has no explore index. `StartIndexBuild()` is called
unconditionally from two places in `app.go`, and `runDumpBuild` then
downloads gigabytes from `data.metabrainz.org` before Explore can return
anything beyond the user's own library:

| Stage | Source | Cost |
|---|---|---|
| Listen Counts | ListenBrainz spark full listens dump | **~205 GB streamed** — see below |
| Catalog Import | MusicBrainz canonical dump (~2 GB `.tar.zst`) | scan ~30M CSV rows, assemble to budget |
| Metadata Patch | MB/LB API | rate-limited at 3 req/s |
| Listener Counts | LB API | rate-limited |

Measured 2026-07-25 against the live dump
(`listenbrainz-spark-dump-2593-20260712-000004-full.tar`):

```
content-length: 205073162240   # 205 GB
accept-ranges: bytes
```

The stage-1 reader skips non-`.parquet` tar members
(`dumpcounts.go:317`), but a tar stream has no seek — skipped bytes
still transit the wire. **So a first run on a fresh install pulls
~205 GB.** Little of it touches disk (the counts map and checkpoint do,
not the dump), but the bandwidth is real and it is per-user.

Consequences today:

- Every install pulls ~205 GB to derive a catalog that is **identical
  for everyone**. On a metered or slow connection this is untenable, and
  it is unconditional on first run.
- **It refuses to start without 6 GB free** (`dumpMinStartFreeBytes`),
  and aborts below 2 GB (`dumpAbortFreeBytes`). This is what breaks
  `make fresh-install` on a tmpfs `/tmp`.
- First-run Explore is empty for the length of the import.

The catalog half is **the same for everyone**. Only the local half
(`PopulateLocalCrossReferences`, `BackfillLibraryDiscographies`) is
per-user. Deriving the shared half on each machine is the waste this
plan removes.

## Goal

Ship a prebuilt core index so a fresh install has a usable Explore
immediately, and the runtime build collapses to the local half plus
incremental refresh. The full dump import becomes an opt-in "deep
catalog" upgrade rather than a prerequisite.

## Sizing evidence

Measured 2026-07-25 with a synthetic harness against the real schema and
migrations (2.15M-row full run exceeded a 15-minute budget, so this is a
200K-row calibration, `VACUUM`ed):

| Metric | Value |
|---|---|
| 200,000 rows, with FTS | 85.2 MB |
| Cost per row | ~426 B |
| zstd -19 | 29.6 MB (2.9x) |

Extrapolating to the current budgets (`keepRecordings` 1.5M +
`keepReleaseGroup` 400K + `keepArtists` 250K = 2.15M rows):

| Tier | Rows | On disk | zstd -19 |
|---|---|---|---|
| Full budget | 2.15M | **~900 MB** | ~310 MB |
| Core (proposed) | 500K | ~210 MB | **~72 MB** |
| Minimal | 250K | ~105 MB | ~36 MB |

**This corrects an earlier figure.** A ~93 MB index was recorded in the
2026-07-16 audit note; that measured the *legacy tier-crawl* index, not
the dump-built one. The dump build targets an order of magnitude more
rows. Shipping the full index is not viable as a casual download —
which is exactly why this plan is scoped to a *core* subset.

⚠️ Two caveats on these numbers:

- The harness used a 14-word vocabulary, so its FTS measured only 7% of
  total size. Real titles have a far larger vocabulary and the real FTS
  share will be materially higher. **Treat the totals as a floor.**
- Row width was estimated from the schema (3 UUIDs at 36 chars dominate);
  `aliases` was left empty and is populated for real artists.

Re-measure against a genuine dump-built index before committing to a
tier size.

## What "core" should mean

`dumpcatalog.go` already has graded per-artist coverage (S2) —
`perArtistArtistBudget = 10_000` split into tiers A/B/C with per-tier
track and release-group caps. The core index should reuse that machinery
rather than invent a second notion of importance:

- **Artists:** top ~50K by listen count.
- **Release groups + recordings:** the S2 per-artist slice for those
  artists (tier A/B/C caps as they stand).
- **Excluded:** the global long tail below the per-artist selection.

Anything not covered still works — it just resolves through the existing
lazy paths (`EnsureArtistDiscography`, `AddFromCache`), which is the
behaviour non-covered artists already get today.

## Distribution: download on first run, not `go:embed`

**Both packaging paths build from source** — the Homebrew formula builds
from a release tarball, the Arch `PKGBUILD` clones the tag. So:

- Committing the artifact to git bloats the repo and every source tarball.
- `go:embed` makes a from-source build require the artifact at build
  time, so source builds would have to download it anyway — and
  `build-prod` runs UPX over the binary, which would be pathological
  with a 70 MB+ embedded blob.

So "ship with the app" should mean **fetch a prebuilt artifact on first
run** from a versioned URL. CI already publishes binary packages to the
Gitea package registry (`.gitea/workflows/arch-package.yml`), so there is
an existing place to host it.

Import path: download `.zst` → decompress → `ATTACH` → `INSERT INTO
explore_index SELECT ...` through the **existing** `upsertBatch` conflict
rules, which already do the right thing (non-empty wins, highest
popularity wins, never clobber a good value with an empty one).

## Artifact contents

Ship the global catalog columns only. These are **per-user** and must be
zeroed in the artifact, then recomputed locally by
`PopulateLocalCrossReferences`:

- `in_library`, `is_similar`
- `local_artist_id`, `local_release_group_id`, `local_recording_id`

`discog_fetched` should ship as `1` for artists whose S2 slice is
included, so the backfill doesn't redundantly re-fetch them.

Also decide per-table whether to include: `similar_artist_map`,
`artist_metadata`, `release_to_rg`. `release_to_rg` in particular may
rival the index in size — measure before including.

**Resolved: the artifact ships no FTS.** Rows are inserted into the
client's own `explore_index`, whose `AFTER INSERT` trigger populates
`explore_index_fts` as a side effect — so shipping a search index would
be pure redundant weight. `cmd/indexexport` builds the artifact without
FTS or triggers accordingly.

## Update strategy

- **Popularity drift** — `dumpincremental.go` already implements
  incremental listens-dump refresh (`RefreshListenCounts`, weekly
  cadence). It applies unchanged on top of a shipped baseline, provided
  `listens_applied_series` is stamped in the artifact so deltas resume
  from the right point.
- **Catalog additions** — new releases arrive via the existing lazy
  per-artist fetches. A refreshed artifact per app release is enough;
  no separate cadence needed.
- **Schema changes** — `schema_version` exists on `explore_index` but is
  noted as dead in the audit. Either wire it up or version the artifact
  filename against the migration number, so an old artifact can't be
  imported into a newer schema.

## Build pipeline: build and cache in Gitea CI

The import is unusually well suited to running as a **series of
time-boxed CI jobs against a persistent cache**, because the resumability
already exists:

- Stage 1 streams over a `resumableReader` that reconnects with HTTP
  `Range` requests, and the live dump advertises `accept-ranges: bytes`.
- `counts.bin` checkpoints `Offset` (absolute byte position) and
  `MemberIdx`, and the applier merges results **in member order** so
  "every checkpoint is a contiguous prefix of the stream"
  (`dumpcounts.go`).
- Stage 2's canonical scan is deliberately restartable wholesale — "cheap
  enough to simply restart after an interruption" (`dumpcatalog.go`).

So a job that hits a runner time limit resumes at its exact byte offset
on the next run. **No single multi-hour job is required** — schedule
N bounded runs and let them converge.

What it needs:

1. **A persistent volume for `explore-staging/` + the DB.** `act_runner`
   uses the Docker backend and job containers are ephemeral, so bind-mount
   a host path (or a named Docker volume) and point `YJ_HOME` at it.
   Prefer this over the Actions cache — cache entries are size-capped and
   awkward at GB scale, and this is a self-hosted runner anyway.
2. **A headless entrypoint** — currently the import only runs from the
   app lifecycle (`StartIndexBuild` via `OnDomReady`). This is a real gap,
   but a small one: `NewSearchIndex(db, lb, artistImg, logger)` takes no
   Wails dependency, and the single `runtime.EventsEmit` in
   `searchindex.go` sits inside `emitStatus`, which already early-returns
   when `runtimeCtx == nil`. A `cmd/indexbuild` that opens the DB and
   calls `StartBuild(context.Background())` — never `SetContext` — should
   work. Verify `scheduleChampionRebuild` in the `StartBuild` defer is
   also Wails-free.
3. **Triggers.** `indexbuild` decides its own mode from index state, so
   every trigger runs the same command: push to `main` and a weekly cron
   both land on a cheap refresh (which no-ops when nothing new is
   published), and the 3-month rebuild fires when the command notices the
   import has aged out.

Then export: subset to core, zero the personal columns, stamp
`dump_import_done` / `listens_applied_series` / schema version, `VACUUM`,
`zstd -19`, checksum, publish to the Gitea package registry (the Arch
workflow already authenticates against it with `PACKAGE_TOKEN`).

**Be a good citizen about the 205 GB.** Rebuild on the dump cadence
(the audit notes a 90-day re-import cadence), never per-commit. Once a
baseline exists, the ~180 MB daily incremental dumps already wired in
`dumpincremental.go` keep popularity fresh — so the 205 GB is genuinely
one-time per rebuild, not per refresh. Also check the runner's own
egress if it is self-hosted on a home connection.

## Licensing

- MusicBrainz canonical dump is **CC0** — redistribution fine.
- ListenBrainz-derived listen counts need their dump licence checked
  before redistribution, plus attribution in-app either way.
- Note the derived counts already differ from LB API values (no MLHD+
  history) — a known, accepted divergence, but worth stating wherever
  the numbers are surfaced.

## Risks

- **Artifact staleness vs app version** — a user on an old release gets
  an old catalog. Mitigated by incremental refresh + lazy fetches.
- **Download failure / offline install** — must degrade to today's
  behaviour (local library search), not a broken Explore. The failure is
  now visible in the Jobs panel, which helps.
- **Users who want the full catalog** — keep the existing dump import as
  an explicit opt-in, gated behind a setting. Note that no such setting
  exists today: `StartIndexBuild()` is unconditional, and Library Only
  mode is frontend-`localStorage` only with no backend wiring.

## Phasing

1. ✅ **Headless entrypoint.** `cmd/indexbuild` — resumable, budgeted
   (`-budget 3h`), signal-aware, exit 3 = "more work remains". Verified
   to run without Wails; builds with `CGO_ENABLED=0` and no build tags.
2. ✅ **Export tooling.** `cmd/indexexport` — top-N artists plus a
   per-artist window of their release groups and recordings, personal
   columns dropped, metadata stamped, vacuumed. Verified against a
   synthetic index: no personal columns leak, no orphaned rows, caps
   respected.
3. ✅ **One real build.** Superseded by a real dump-built index that
   already existed on the dev machine (`dump_import_done` 2026-07-17).
   Measured 2026-07-29 — these replace every extrapolation above:

   | | rows | on disk |
   |---|---|---|
   | `explore_index` | 2,052,168 (227,359 artists / 400,675 RGs / 1,424,134 recordings) | 383 MB |
   | its indexes | | 395 MB |
   | FTS | | 80 MB |

   187 B/row for the shippable table, 418 B/row all-in — so the ~900 MB
   full-budget estimate was right. Two real exports:

   | tier | rows | artifact | zstd -19 |
   |---|---|---|---|
   | 50K artists (default) | 1,076,133 | 191.5 MB | **70.6 MB** |
   | 25K artists / 10 RG / 20 rec | 620,973 | 110.6 MB | **37.8 MB** |

   `release_to_rg` was empty in that index — it predates the code that
   persists it — so its size is still unmeasured.
4. ✅ **Import path.** `backend/explore/artifactfetch.go` (download,
   Range-resume, sha256, zstd) and `artifactimport.go` (validate, ATTACH,
   batched merge, FTS rebuild, meta stamping). Reported in the Jobs panel
   under its own two stages. Measured end to end on the real 50K-artist
   artifact against a disk-backed DB: **1,076,133 rows merged in 43.2s**
   (24,900 rows/s), yielding a 455 MB `yj.db`, FTS populated and
   searchable. In-memory the same merge runs in 28.3s.
5. ✅ **Gate the dump build.** `deep_catalog_enabled` in
   `explore_index_meta` (beside `index_build_paused` — it is build state,
   read at one decision point). Off by default; exposed as
   `DeepCatalogEnabled` / `SetDeepCatalogEnabled` on the explore Service.
   An interrupted dump import resumes regardless of the setting, so the
   gate never discards a checkpoint that already cost hours.

## Measured 2026-07-29: why the client cannot fix this itself

`data.metabrainz.org` caps a client at ~2.1 MB/s. One Range stream and
four concurrent Range lanes both delivered 32 MB at the same aggregate
rate (2,111,195 B/s vs 2,209,000 B/s) while the same machine pulled
66.9 MB/s from a CDN. **Parallelism buys nothing** — the four lanes just
divide the same cap, and one of them starved to 0.5 MB/s.

So stage 1 costs, unavoidably:

| | bytes | wall clock |
|---|---|---|
| Whole tar (what shipped before column projection) | 205 GB | ~27 h |
| Column projection, 3 columns (43.4%) | 89 GB | ~11.8 h |
| `recording_mbid` only (24.1%), rolled up via canonical | 49 GB | ~6.5 h |
| + 1-in-4 member stride sample | 12 GB | ~1.6 h |

The last two are CI-side options, not client defaults: recording-only
drops listens carrying no recording MBID and re-derives artist totals as
a sum over recordings, and sampling trades exact counts for a ranking.
Both are only safe because the selection they feed is a top-N cut.

## Distribution: the "latest" version trick

The client cannot enumerate package versions — Gitea's package listing
API requires a token, while an anonymous file GET does not (a probe of a
non-existent artifact returns 404, not 401). So `index-artifact.yml`
publishes each artifact twice: under a dated version for history, and
under a fixed `latest` version that the client fetches from a
predictable URL. Generic packages reject overwriting an existing
filename, so `latest` is DELETEd before each rewrite.

⚠️ **Unverified:** that anonymous package *download* is actually enabled
on git.ljones.me. The 404-vs-401 probe is suggestive, not proof — no
artifact has been published yet to test against. Confirm before relying
on it, and note that every install pulling from a personal Gitea makes
its bandwidth and uptime a user-facing dependency.

## Incremental retention bounds artifact staleness

The incremental dump directory holds 30 dumps (series 2579–2610 as of
2026-07-29) and full dumps land roughly monthly. An artifact older than
~30 days therefore cannot be topped up: the dailies bridging the gap are
gone. That is a permanent undercount of that window's listens, not
corruption — but it pins the republish cadence at monthly.

## Upgrade path for indexes built by older code

The dev machine's index has `dump_import_done` set but **no**
`listens_applied_series` and an empty `release_to_rg`, because it was
built before the code that writes them. That combination is a dead end:
`RefreshListenCounts` bails with "no baseline series recorded", and
`runDumpBuild` short-circuits on the done marker, so popularity can
never update again. Current code writes both, so this affects only
pre-existing installs — but the artifact import is the natural place to
rescue them, since merging one stamps a fresh baseline series.
6. ✅ **CI wiring.** `.gitea/workflows/index-artifact.yml` — push +
   weekly cron + manual, concurrency-guarded, publishes only when
   `complete && changed` so identical artifacts don't accumulate.
   Runner-side prerequisites are in place (cache dir + `valid_volumes`
   on the VPS runner).

Step 3 is the gate on everything downstream — and it is worth doing
regardless of whether the artifact ever ships, since it is the only way
to get real numbers for the index.

## Related

- `backend/explore/dumpimport.go` — stage orchestration, disk floors
- `backend/explore/dumpcatalog.go` — budgets, S2 per-artist tiers
- `backend/explore/dumpincremental.go` — incremental refresh (update path)
- `backend/explore/searchindex.go` — `upsertBatch` conflict rules,
  `PopulateLocalCrossReferences`
- Migration 26 in `backend/database/database.go` — `explore_index` schema
