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
- **WAV tracks scan in untitled.** `backend/tagwriter` writes WAV tags
  into a RIFF `id3 ` chunk and `dhowden/tag` has no RIFF parser, so
  there is no "Field Recordings" artist in the Artists view. This is a
  known open bug pinned by `TestWAVTagsAreNotReadableYet`; do not
  "fix" a spec by asserting the broken behaviour elsewhere.

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

Rebuild a seed after any schema change, or the restored database is
migrated on open in a way the seed's author never saw.
