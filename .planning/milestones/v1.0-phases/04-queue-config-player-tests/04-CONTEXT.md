# Phase 4: Queue, Config & Player Tests - Context

**Gathered:** 2026-03-03
**Status:** Ready for planning

<domain>
## Phase Boundary

Write unit tests for three packages: queue operations (SetQueue, Next, Previous, shuffle, repeat, persistence), config roundtrip (load/save, validation, defaults), and player pure logic (volume conversion, state mapping). These tests characterize current behavior and serve as a safety net for Phase 6-7 refactoring. No production code changes except adding test files.

</domain>

<decisions>
## Implementation Decisions

### Test fixture strategy
- Per-test inline setup for queue — each test creates its own audio_file FK rows with minimal fields. Verbose but self-contained; a test failure tells you everything.
- t.TempDir() for config filesystem tests — real filesystem via Go's test temp dirs, auto-cleaned, tests actual TOML read/write.
- Simple mock TrackLoader struct defined locally in queue_test.go — only queue tests need it, keep it local.
- Player tests are pure logic only — no NewTestDB, no persistence round-trips. Volume conversion, clamp, state mapping only. Player persistence deferred to integration tests.

### Player logic extraction
- Test existing pure logic in place — volume.go (UserVolume, Volume, clampVolume) is already cleanly separated. Write volume_test.go against it. No extraction from player.go.
- Include stateToMediaControls() — it's pure and trivial but documents the state mapping. Characterization value.
- Format detection tested in metadata package, not player — the code lives in metadata/decoder.go, tests belong there (decoder_test.go or similar).
- Do NOT extract anything new from player.go — lock-sensitive code must not be touched. Test what's already pure.

### Coverage depth vs breadth
- Queue: edge cases first — empty queue, single track, last track, first track, remove current track. These are where bugs hide and refactoring breaks.
- Queue: dedicated move test cases — move forward, move backward, move current track, move to boundaries, move multiple tracks. MoveQueueTracks has the most complex index arithmetic.
- Queue: test InsertTracksAt index shifts — insert before/at/after current index, verify currentIndex adjusts correctly. Common off-by-one bug source.
- Queue: verify generateShuffleOrder() properties — all indices present, current track at index 0, no duplicates. Property-based validation.
- Queue: full persistence round-trip — SaveState → new Queue → RestoreState → verify all fields match (shuffle order, repeat mode, current index, track list). Critical for Phase 7 optimization safety.
- Config: test both sub-config validators independently AND the composed Config.Validate(). Pinpoints failures to specific validators.
- Config: include library.Config.Validate() path with t.TempDir() — test both valid directory (real temp dir) and invalid directory (nonexistent path).
- Player: 5-6 tests is sufficient — volume roundtrip, boundary values, clamp, state mapping. Quality over quantity.

### Test organization
- Internal test packages (package queue, package config, package player) — queue tests need access to unexported fields (shuffleOrder, currentIndex, tracks) for setup and assertions.
- Mirror source file names — navigation_test.go tests navigation.go, persistence_test.go tests persistence.go, queue_test.go tests queue.go. Easy to find tests for any function.
- Sub-config tests in their respective packages — theme/config_test.go, tracklist/config_test.go, favorites/config_test.go, library/config_test.go. Config package tests the composed Config.
- t.Parallel() everywhere — NewTestDB gives isolated DB instances, pure logic tests have no shared state. Matches existing coverart/metadata convention.

### Claude's Discretion
- Exact test case names and table-driven subtest structure
- How to organize table-driven tests vs individual test functions (per complexity)
- Specific assertion messages and error formatting
- Whether to use subtests within a single Test function or separate Test functions per behavior

</decisions>

<specifics>
## Specific Ideas

- Queue persistence round-trip is the highest-priority safety net — Phase 7 (PERF-01) will change queue persistence from full table rewrite to incremental INSERT/DELETE. These tests must catch any data loss.
- Mock TrackLoader should be minimal — just enough to satisfy the interface. LoadFile/Play/UnloadTrack can be no-ops, IsPlaying returns false, CurrentPositionSeconds returns 0.
- Queue tests need to insert audio_file rows before queue_tracks (FK constraint). Also need file_type rows since audio_files FKs to file_types.
- Player's existing player_test.go is an integration test guarded by YELLOWJACKET_INTEGRATION env var — new unit tests are separate and should always run.
- Existing test conventions: table-driven subtests with t.Run(), t.Parallel(), standard library testing only (no testify), no assertion libraries.

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope.

</deferred>

---

*Phase: 04-queue-config-player-tests*
*Context gathered: 2026-03-03*
