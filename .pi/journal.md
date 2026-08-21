# Journal

Running work log: what was done, what was verified, what was left open.
Newest entry last.

## 2026-08-21 — #58, the phone's progress line

Took #58 (Phase 3, item 4 of the roadmap in #73; #56 and #59 landed
before it and #64 after, so this was the phase's remaining item ahead of
#51's verification pass). Branch `feat/58-mini-player-progress-line`.

**What.** `<player-progress-line>` in the shell, with its own `auto`
grid row between `bottom-bar` and `bottom-nav` below 600px. 2px, the
fill is `scaleX()` off `PlaybackPositionChanged` with the seek bar's
`trackChangeId`/`seq` guards and an interval that only interpolates
between reports. `aria-hidden`, `pointer-events: none`, and it renders
nothing above 600px or with no track.

**Verified.**

- `make ui-test` — 992 passed, including 8 new in
  `frontend/test/components/progress-line.test.ts` (the matchMedia
  branch, the report/interpolation rules, paused, a stale report, and
  that it is neither announced nor touchable).
- `make e2e --project=chromium` — 223 passed, including 3 new in
  `e2e/specs/phone-progress-line.spec.ts` (the two adjacencies, that it
  takes no taps, that it is absent on a desktop). WebKit is CI's half;
  Arch cannot run Playwright's Linux WebKit.
- Read a screenshot at the reference device's 424x439 with a real track
  playing, and watched the real transform advance 0.288889 -> 0.377778
  over 8 s of a 90 s track. Also checked the line goes with the bar on
  `now-playing`.
- No Go changed, so `make lint`/`make test` were not run.

**Left open.** No device tier: this is a browser at 424x439, which
`CLAUDE.md` is explicit is not a phone. The line's *appearance* on
Chrome 113 is unverified; the CSS it uses (a transform and a flat
background) is nowhere near that engine's gaps.
