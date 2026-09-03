# The fixture library

`test_data/music_library_test/` is **generated, not committed**:
`make testdata` (~1 s) builds 31 deterministic tracks across MP3, FLAC,
Ogg Vorbis and WAV. `make testdata-force` rebuilds unconditionally,
`make testdata-clean` deletes it. `make test` and `make sandbox-seed`
depend on it, so it is rarely run by hand.

Tests that need it fetch it through `internal/testfixtures` and skip
themselves when it has not been generated.

## Select by case, never by path

```go
m := testfixtures.Load(t)
paths := m.Case(t, testfixtures.CaseCoverDedup)
track := m.Track(t, rel)
```

Cases: `cover-dedup`, `multi-disc`, `various-artists`, `flac-album`,
`ogg-album`, `wav-tracks`, `partial-tags`, `unicode`, `duplicates`,
`edge-lengths`, `broken`.

Two invariants worth not breaking:

- **The clean library is exactly 31 tracks**, because `sandbox-seed`
  verifies the scan against that count. Deliberately malformed files
  live in a *sibling* root, `test_data/music_library_broken/`
  (`m.BrokenPath()`), so the scanner never sees them.
- **Tags are written by `backend/tagwriter`, not by ffmpeg** (which
  encodes with `-map_metadata -1`). Fixture and reader therefore cannot
  drift into agreeing with each other and disagreeing with reality.

The manifest (`test_data/music_library_test.manifest.json`, outside the
scanned root) hashes the *spec* — paths, formats, durations, tags,
cover identity — not the bytes, because ffmpeg stamps encoder version
strings and identical specs produce different bytes on different builds.

## In e2e specs

- **Every fixture except one is 2–6 seconds.** A spec that starts
  playback and then clicks pause races the track ending and fails
  against a correct UI. Use `LONG_TRACK` (90 s, `edge-lengths`) exported
  from `e2e/support/fixtures.ts`.
- **WAV tracks scan like every other format.** #104 added
  `backend/riff`, so the scan reads the `id3 ` chunk `backend/tagwriter`
  writes and both WAVs come in fully tagged: "Field Recordings" is an
  ordinary artist in the Artists view, with a "Test Tones" album and a
  cover. They are therefore not an example of an untitled or albumless
  track — the only two tracks with no album are
  `unsorted/no-tags-at-all.mp3` and `unsorted/title-only.mp3`. Prose
  written before #104 says the opposite and names
  `TestWAVTagsAreNotReadableYet`, a test that change deleted; that is
  dated history rather than a description of the app.

## Seeds

```bash
make sandbox-seed NAME=default   # build (boots a fresh YJ_HOME and drives the app)
make sandbox-seeds               # list
make dev-headless SEED=default   # restore into a run
```

A seed is a tarred `YJ_HOME` produced by *running the app*: fresh home →
real `AddLibrary` binding → poll until the real scan reports the
manifest's track count → SIGTERM so shutdown hooks persist state → tar.
Never hand-write one. Seeding points `YJ_CORE_INDEX_URL` at a dead
address on purpose, so no seed depends on what the explore artifact
server happened to be serving.

Rebuild a seed after any schema change. Nothing migrates a restored
database: `applySchema` is `CREATE TABLE IF NOT EXISTS`, so an old seed
keeps its old columns, the app starts, and the first query dies on
`no such column`.

**Restoring the seed does not disable the artifact fetch — only
*building* it does.** `dev-headless` leaves `YJ_CORE_INDEX_URL` alone,
so on a developer machine the restored app immediately downloads and
imports the real ~1.1M-row catalog, through the one writer connection,
while whatever you started it for is running. A full `make e2e` against
that reported **14 failures** that were all contention; the same suite
against the same seed with

```bash
YJ_CORE_INDEX_URL='http://127.0.0.1:1/none.tar.zst' make dev-headless SEED=default
```

is the configuration CI runs (`ci.yml` sets exactly that address) and is
what to use before believing a failure. The tell is in `.dev/app.log` —
an import logging progress — and in how the failures look: timeouts
spread across unrelated specs rather than one surface being wrong.
