# 004 — Wanted list

**Status:** implemented
**Branch:** main
**Created:** 2026-07-29
**Follows:** 003-download-clients

## Problem

Plan 003 shipped a request as a heavyweight row: library, anchors,
cached tracklist, state machine, error text, cascading items. That is
the right shape for *one attempt to acquire something* and the wrong
shape for *the user wanting something*, and 003 used it for both.

The consequences showed up immediately. A request that found nothing was
marked `failed`, which is a lie — the album exists, no source had it
today. Retrying meant the user remembering to press a button. Wanting an
artist's future releases was not expressible at all. And a user who
acquired an album by other means kept a failed row about it forever.

## The model

A **want** is an MBID, what that MBID names, and retry bookkeeping.
That is all.

```
download_wants(mbid, entity, library_id, scope, secondary, state,
               parent_id, attempts, last_error, next_try_at,
               external_ids)
```

`entity` is the only type distinction, and it carries all the policy:

| entity | meaning |
|---|---|
| `artist` | a subscription. Never satisfied; each pass expands the discography into child wants |
| `release-group` | an album in the abstract — any release satisfies it |
| `release` | one specific edition |
| `recording` | one track |

`UNIQUE(mbid, library_id)` is load-bearing: it is what makes artist
expansion idempotent, so a subscription can re-run every pass and add
only what is genuinely new.

Requests did not go away — they became what they always were, the
ephemeral record of one attempt, with a nullable `want_id` back-link.
The lifetimes are now opposite and explicit: **a request is history, a
want is intent.**

### Nothing here fails

There is no `failed` want state. An attempt can fail; a want cannot. A
want that found nothing gets `attempts + 1`, a reason the user can read,
and a longer backoff — 6h doubling to a 7-day ceiling, jittered so a
list added in one sitting does not come due in one burst.

### Satisfaction is ownership, not download

A want retires when the *library* owns what it names, however it got
there — bought, ripped, copied in. Inferring satisfaction from our own
completed downloads would keep hunting for music already on disk.

### Artist scope defaults to `future`

Following an artist takes new releases only, and skips compilations,
live albums and remixes. `all` backfills the discography, and the user
can widen it from the wanted list. Subscribing should not silently queue
forty albums.

## The reconciler

A 6-hourly loop (plus on-demand, plus a 3-minute startup delay so the
explore index has loaded). Four steps, in this order:

1. **Expand** artist subscriptions into album wants — first, so step 2
   sees them this pass rather than next.
2. **Retire** wants the library already owns.
3. **Sync** to clients that keep their own list.
4. **Attempt** a bounded batch (25) of due wants.

Everything the loop needs about music comes through a four-method
`CatalogPort`, adapted to the explore index in `backend/downloadcatalog.go`
— the composition root, so neither package learns about the other.

### Unattended grabs, and what stops them

`Manager.Attempt` is `Start` without the parking: it searches, and grabs
only if `AutoPickable` clears. When it does not, **nothing is
persisted** — no request row. A want retried weekly for a year would
otherwise leave fifty identical failed rows, none of them anything the
user can act on.

`AutoPickable` gained one condition: an anchored request with an empty
`Expected` is refused. An anchor with no tracklist behind it is an
anchor in name only, and match then rests on album/artist text — exactly
the evidence a wrong-album candidate also has. Nobody is watching a
reconcile pass.

## Per-provider concurrency

`Downloads.MaxConcurrent` was the only limit, and was never actually
applied (`SetMaxConcurrent` did not exist). Now:

- **slskd defaults to 1.** A Soulseek peer serves one file at a time
  from one person's upload slot; asking for more gets you queued behind
  everyone else at best. One is both the polite number and usually the
  fastest.
- yt-dlp 2, torrent/usenet clients 4, overridable per provider via a
  `maxConcurrent` field that `Register` appends automatically to any
  descriptor declaring `CanTransport`.
- A grab takes its **provider's** slot before the global one, so a queue
  on a busy slskd cannot sit on a global slot a usenet transfer could
  have used. The transport is resolved before either slot is taken;
  delegates take neither, since the transfer is happening inside another
  system that is doing its own limiting.

## The Lister role

The fourth role, alongside Searcher/Transporter/Delegator. Lidarr
already models a want — a monitored artist or album — and it is always
on, where a desktop player is not. A subscription mirrored there keeps
working while the app is closed.

- `artist` → Lidarr artist, `monitor: future|missing` per scope
- `release-group`/`release` → monitored album
- `recording` → not pushed. Lidarr cannot say "one track", and
  monitoring the album to get it downloads far more than was asked.

Sync is push-only in the loop; pulling happens only when the user
explicitly imports ("adopt the artists Lidarr already monitors", which
arrive at `future` scope). Removal **unmonitors**, never deletes — the
user's Lidarr may predate this app.

## Frontend

- `Wanted` view in the sidebar: Following / Looking for / Paused /
  Found, with pause, remove, scope toggle and "Check now".
- "Want this" on the album page, "Follow for new releases" on the artist
  page. The want button shows whether or not a client is connected —
  wanting is durable and stays queued until one exists.
- `WantedListChanged` event, since a background pass changes the list
  without the UI doing anything.

## Files

`backend/download/want.go`, `wantstore.go`, `reconcile.go`,
`provider_lidarr_list.go`; `backend/downloadcatalog.go`;
schema `download_wants.sql` + migration 48 for the two new
`download_requests` columns; `frontend/src/components/wanted-view/`.

## Deferred

- **Release-group wants are not retired by ownership of a specific
  release.** The library indexes release groups and recordings, not
  editions, so a `release` want is only satisfied by its own download
  completing.
- **No recording lookup on the explore index**, so a track want relies
  on the title the UI passed in. A want added as a bare recording MBID
  has no tracklist and waits.
- Quality profiles and upgrade-if-better (from 003).
- Resume across restart (from 003) — still the largest gap, and it now
  matters more: an unattended grab that dies on restart is retried by
  the reconciler, but from zero bytes.
