# 006 — Orientation fixes: knowing where you are and what you're looking at

**Status:** implemented
**Branch:** main
**Created:** 2026-08-11
**Follows:** 005-agent-development-harness

## Problem

Six reports from using the app, which turned out to be one theme with
six faces: **the UI knew things it did not say.**

1. Some track names in the track list were links, most were not. The
   rule (has both a release-group *and* a recording MBID) was invisible,
   so the list looked randomly broken.
2. Opening an album sometimes showed the full catalog tracklist and
   sometimes only the tracks the user owned, with nothing on screen
   distinguishing the two — or distinguishing either from "still
   fetching".
3. The same on artist pages: a discography that was the artist's, or a
   discography that was the user's shelf, rendered identically.
4. "Check now" on the requests list appeared to do nothing, because it
   honoured each request's retry backoff — a request searched an hour
   ago was not due, so a deliberate button press produced silence.
5. Pressing **M** muted playback and left the volume indicator
   unchanged, because mute does not change the volume *number* and
   `VolumeChanged` carried nothing else.
6. The home page did not exist. The sidebar had a Home item; it fell
   through to "Coming soon: home".

## What shipped

**Backend**

- `events.MuteChanged` (bool), emitted alongside `VolumeChanged` so the
  UI has something to react to when silence is the only thing that
  changed. `Player.Muted()` for symmetry; `MuteToggle` now takes the
  speaker lock and refuses politely when no streamer exists.
- `download.Reconciler.RunNow` — a forced pass that ignores backoff,
  backed by a new `ListWantedDownloadRequests` query. `RunOnce` (the
  loop) still honours it: the backoff is a promise to the providers,
  not to the user, and a person pressing a button *is* the schedule.
  `Summary` gained `Waiting` and `NoProviders` so "nothing happened"
  can be reported with a reason.
- `backend/home` — the shelf builder, with queries in
  `sql/queries/home.sql` that return album ids only, joined back to
  `GetAllAlbumsWithDetails` in Go rather than restating the album
  projection six times. A shelf with nothing behind it is omitted.

**Frontend**

- `explore-link.ts` rewritten: a name always goes somewhere. No MBID
  means the *library* page for the same album/artist (both detail views
  already accept a local id), resolved through the library store, with
  an untagged track highlighted by title instead of by recording MBID.
  Links now fire on a genuine single click only — see below.
- `<catalog-scope-notice>` — one banner, four states (`catalog`,
  `loading`, `library`, `unavailable`), used by both detail pages. The
  album and artist pages grew an explicit `catalogPending` /
  `catalogLoaded` pair, because `loadingReleases` already meant
  "something is renderable" and a library stand-in satisfies that.
- Artist page: an empty `BrowseReleaseGroups` no longer wipes the
  library-hydrated discography — an empty catalog answer means "not
  indexed yet", not "released nothing".
- Downloads: a no-client banner, per-request "next check in …", honest
  idle summaries, and copy that says the retry schedule exists.
- `<home-view>`: shelves as horizontal rows; a cover opens the album, a
  play button plays it.

## The one thing worth remembering

**Making every track name a link broke double-click-to-play**, and the
e2e playback suite caught it: the title is the widest thing in a row,
so the first click of the double-click landed on the link and navigated
away. Fixed in one place — `singleClick()` in `explore-link.ts` holds
the navigation for one double-click interval (250 ms) and drops it if a
`dblclick` arrives, while leaving the dblclick itself to bubble to the
row. Rows do not need to know links exist.

This is exactly the failure mode plan 005's e2e tier was built for; it
was invisible before the change because the seeded fixture library has
no MBIDs, so no track name was a link.

## Verification

`make lint` (3 configs), `make test` (3 passes), `make ui-test`
(329 passing, up from 313), `make e2e` (23 passing, up from 19 — four
new home-page specs), `tsc --noEmit`, and manual verification of all
six items in the running app via `make dev-headless` + `playwright-cli`.
