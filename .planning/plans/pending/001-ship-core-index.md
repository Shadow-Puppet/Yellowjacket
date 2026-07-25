# 001 — Ship a prebuilt "core" explore index

**Status:** pending
**Branch:** wip
**Created:** 2026-07-25

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
3. ⬜ **One real build.** Run `indexbuild` against a persistent volume
   until it converges. This yields the first genuine dump-built index and
   with it true row counts, on-disk size, real FTS share, and
   `release_to_rg` size. **Every tier number above is still an
   extrapolation from synthetic rows until this exists.**
4. ⬜ **Import path.** First-run download + attach + upsert, with checksum
   verification, resumability, and clean degradation on failure. Report
   it as a job in the Jobs panel — the plumbing for that already exists.
5. ⬜ **Gate the dump build.** Add the setting that makes the full import
   opt-in, so a shipped core index isn't immediately followed by the
   multi-GB download it was meant to replace.
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
