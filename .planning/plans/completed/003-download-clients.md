# 003 — Download clients

**Status:** implemented (v1); follow-ups tracked below
**Branch:** main
**Created:** 2026-07-27

## Problem

YellowJacket can find music (`explore`), identify it (`autotag`), and
manage it (`library`) — but it can't acquire it. The one gap between
"you're missing this album" and "you own this album" is filled today by
the user alt-tabbing to some other tool.

The naive fix is an HTTP client for Soulseek and a shell-out to yt-dlp.
That produces two bespoke code paths with duplicated queueing, retry,
staging and import logic, and a third service means a third copy. The
services users want to connect are also not the same *kind* of thing —
some search, some transfer bytes, some are entire automation systems we
delegate to — so a single `DownloadClient` interface would be a lie that
every adapter partially implements.

## The role decomposition

Every candidate integration fills one or two of three roles:

| Service | Searches | Transports | Delegates |
|---|---|---|---|
| slskd (Soulseek) | ✅ | ✅ | |
| yt-dlp | ✅ | ✅ | |
| Lidarr | | | ✅ |
| Prowlarr | ✅ | | |
| qBittorrent / Transmission | | ✅ | |
| SABnzbd / NZBGet | | ✅ | |

So: three small interfaces, not one big one. A provider implements
whichever it supports and declares that in a capability struct, the same
way `jobs.Caps` lets the frontend render controls without switching on
`Kind`.

```go
// Searcher turns a request into ranked candidates.
type Searcher interface {
    Search(ctx context.Context, req Request) ([]Candidate, error)
}

// Transporter moves a candidate's bytes to a local staging directory.
type Transporter interface {
    Grab(ctx context.Context, c Candidate, dst string, p ProgressFunc) (Result, error)
    Cancel(ctx context.Context, grabID string) error
}

// Delegator hands the whole request to an external manager and
// reports back when files land.
type Delegator interface {
    Request(ctx context.Context, req Request) (string, error)
    Poll(ctx context.Context, externalID string) (DelegateStatus, error)
}
```

A `Provider` is the registry entry: identity, config, health check, caps,
plus whichever of the three it satisfies. Search-only providers
(Prowlarr) are paired with a transport at grab time by protocol match
(`torrent` → qBittorrent, `usenet` → SABnzbd); providers that do both
are self-pairing.

## v1 decisions (settled)

- **On-demand only.** User-initiated "find this album" from an Explore
  artist/album page or a missing-album row. No wanted list, no artist
  monitoring, no quality-cutoff upgrades. The queue and pipeline built
  here are exactly what monitoring would later sit on top of — see
  Deferred.
- **Soulseek via slskd's REST API**, not a native protocol client. Same
  adapter shape as everything else, no wire protocol, no credentials in
  our process, fully testable against an `httptest` server. A native
  provider can slot in behind `Searcher`/`Transporter` later with no
  pipeline changes.
- **Stage → autotag → import.** Downloads land in a staging directory,
  are matched against the intended release with the existing `autotag`
  scorer, tagged, then moved into the library and scanned. Never write
  into the library root directly.
- **All four provider families in v1**, sequenced so each phase proves a
  different role shape (see Phases).

## Pipeline

```
Request (MBID-anchored where possible)
  └─> fan-out Search across enabled providers (per-provider timeout)
       └─> merge + rank Candidates
            └─> user picks (or auto-pick above confidence threshold)
                 └─> Grab into staging/<request-id>/
                      └─> verify (audio decodes, expected track count)
                           └─> autotag against the intended release
                                └─> tagwriter writes tags
                                     └─> move into library layout
                                          └─> targeted incremental scan
```

The `Request` should carry a release-group or release MBID whenever the
user started from an Explore page, because that anchor is what makes the
autotag step reliable instead of a second guess. Free-text requests are
supported but flagged lower-confidence, and never auto-pick.

Staging lives under the user data dir, not the library. Partial grabs are
resumable where the provider supports it and swept on startup where it
doesn't.

## Candidate ranking

Two independent scores, kept separate:

1. **Match confidence** — does this candidate contain the release the
   user asked for? Reuse `autotag`'s distance/alignment machinery on the
   candidate's *filenames* (Soulseek gives paths, not tags), against the
   expected tracklist from the explore index.
2. **Source quality** — format (FLAC > V0 > 320 > lower), bitrate,
   completeness (file count vs. expected track count), source health
   (slskd queue length and upload slots; seeders for torrents), and a
   user-set per-provider priority.

Ranking presents both, because they trade off — a perfectly-matched
128kbps rip should lose to a well-matched FLAC, and the user should be
able to see why. Reusing `autotag.ScoreBreakdown`'s "explain the ranking"
pattern here is deliberate.

## Persistence

New tables (migration TBD, next free number):

- `download_providers` — id, kind, name, enabled, priority, config blob
  (JSON), `created_at`. Non-secret config only.
- `download_requests` — id, source (`explore-album`, `explore-artist`,
  `manual`), release_mbid / release_group_mbid, free-text query,
  requested_at, state, resolved_download_id.
- `download_items` — one row per grab attempt: request_id, provider_id,
  candidate JSON, state, bytes/total, staging path, error, timestamps.

**Secrets** (slskd API key, Lidarr/Prowlarr API keys, qBittorrent
password) do not go in the TOML config or the DB in plaintext. Use the OS
keyring where available with a clearly-labelled encrypted-file fallback,
and never log a config value from a provider's secret field. Open
question below on the exact library.

## Jobs integration

Add `jobs.KindDownload`. One job per request (not per file), with
`Stages` for search → grab → import so the existing detail panel renders
the pipeline for free. `Caps{Cancellable: true}`; pausable only for
providers that can resume. Per-provider concurrency caps and a global
cap, both configurable — hammering a Soulseek peer with eight parallel
transfers gets you queued or banned.

## Frontend

- New `download-providers` section in `config-page` (HTMX + templ, same
  as existing settings) for provider CRUD, test-connection, priority.
- New `download-picker` Lit component: the ranked-candidate dialog,
  invoked from Explore album/artist pages and from a missing-album row.
- `download-store.ts` subscribing to the existing `JobsChanged` event —
  no new event channel needed for progress.

## Phases

Each phase is independently shippable and proves a distinct role shape.

1. **Core.** Interfaces, registry, `Request`/`Candidate`/`Result` types,
   staging dir, ranking, the stage→autotag→import tail, jobs wiring,
   schema, secret storage. Ships with a fake provider and full test
   coverage of the pipeline. No real network.
2. **yt-dlp.** Subprocess provider: search + transport, no server for the
   user to run, so it's the fastest path to an end-to-end working
   feature. Proves the local-subprocess shape (binary discovery,
   version checks, stdout progress parsing, sandboxing the arg list).
3. **slskd.** Remote search + transport over REST. Proves the remote
   HTTP shape and is the highest-value source. This is where filename-
   based match confidence earns its keep.
4. **Lidarr.** Delegate. Proves the fire-and-poll shape, where we don't
   own the transfer and the "import" step is really "detect what Lidarr
   already imported and reconcile".
5. **Prowlarr + qBittorrent/SABnzbd.** Proves split search/transport
   pairing — the one case where two providers cooperate on a single
   request.

## Risks and constraints

- **No bundled credentials, no default-on providers, no preconfigured
  indexers.** Every provider is off until the user configures it. The
  app ships the ability to connect to services the user already runs.
- **yt-dlp is a moving target.** Pin a minimum version, check it at
  provider-enable time, and fail with a clear message rather than
  parsing garbage output.
- **Filename-only matching is genuinely hard.** Soulseek results are
  `\Music\Album (1997) [FLAC]\01 - Track.flac` at best. Budget real
  effort for the path-parsing heuristics; `autotag/normalize.go` is the
  starting point.
- **Partial and failed grabs must never reach the library.** The import
  step is the only writer into library paths, and it runs after
  verification. Staging sweep on startup.
- **Tests must not hit the network.** `httptest` servers for slskd/
  Lidarr/Prowlarr, a stub binary for yt-dlp.

## Deferred

- Wanted list with background retry (the natural next plan).
- Artist monitoring + auto-grab of new releases — cheap once the wanted
  list exists, because `explore`'s dump index already knows the full
  discography and `library` already knows what's owned.
- Quality profiles and upgrade-if-better.
- Native Soulseek protocol client.
- Transmission/Deluge/NZBGet (same shape as their shipped siblings —
  add on demand).
- Internet Archive / Bandcamp-collection providers: cheap REST adapters,
  worth adding once the core is proven.

## Resolved questions

1. **Secret storage.** No keyring dependency was added. Credentials go
   in a 0600 JSON file in the user data directory (`download-secrets.json`),
   keyed by provider row ID. This is deliberately *not* encryption — a
   key stored beside the data it unlocks protects nothing, and claiming
   otherwise would be worse than being clear about it. What the file
   mode buys is protection from other local users and from the config
   file being pasted into a bug report. `SecretStore` is an interface so
   an OS keyring backend can be added later without touching any
   provider.
2. **Auto-pick.** Implemented behind `Downloads.AutoPick`, default off.
   It requires an MBID-anchored request, match ≥ 0.85, quality ≥ 0.5,
   and ≥ 0.08 of daylight over second place. Free-text requests can
   never auto-pick, because there is no tracklist to be right about.
3. **Library layout.** Configurable path template, default
   `{albumartist}/{album}/{track} {title}`. Segments are sanitized for
   Windows-reserved characters and trailing dots/spaces so a library
   synced between platforms does not produce unopenable files. Existing
   files are never overwritten — a collision gets a numbered variant,
   because the file already there may be a better copy the user owns.
4. **Entry point.** "Find this album" on the Explore album page, shown
   only when a client is connected and the album is not already owned.
   The artist-discography right-click is not wired up yet.

## What shipped

All five phases, ~4,500 lines with tests, `make lint` clean and the full
backend suite green (including under `-race`).

**Core** (`backend/download/`): `Searcher`/`Transporter`/`Delegator`
interfaces with capability-driven composition; `Request`/`Candidate`/
`Result` types; provider registry with self-registering adapters;
two-axis ranking; staging area with escape-guards and startup sweep;
verify → tag → import tail; jobs integration under `KindDownload`;
three tables catalogued in `datamap`.

**Providers**: yt-dlp (subprocess; assembles albums from per-track
searches, since a "full album" video cannot be imported as tracks),
slskd (remote search + transport, peer-health scoring, collects from the
daemon's own downloads folder), Lidarr (delegate; reconciles in place
rather than moving files out from under a system still managing them),
Prowlarr (search-only) paired at grab time with qBittorrent or SABnzbd.

**Frontend**: `download-store.ts`, `download-picker` + `candidate-row`
(two meters, not one blended score), `download-clients` settings section
rendering its forms from backend descriptors so a new adapter needs no
frontend change.

## Follow-ups

- **Resume across restart.** Live transfers are currently marked failed
  on startup and their staging swept, because the transports do not
  survive the process. slskd and qBittorrent can both resume in
  principle; the item rows already carry what would be needed.
- ~~**Per-provider concurrency caps.**~~ Done in 004: per-kind defaults
  (slskd 1, yt-dlp 2, torrent/usenet 4) with a per-provider override,
  and the provider's slot is taken before the global one.
- **Prowlarr candidates score blind.** Indexer results carry no file
  list, so match scoring has only the release title. Fetching the
  torrent metadata before ranking would fix this and is the single
  biggest ranking improvement available.
- ~~Wanted list, artist monitoring~~ — done in 004. Quality profiles
  and upgrade-if-better remain deferred.
