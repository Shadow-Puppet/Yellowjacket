---
phase: 04-queue-config-player-tests
plan: 02
subsystem: testing
tags: [config, theme, tracklist, favorites, library, player, volume, validation, table-driven-tests]

# Dependency graph
requires:
  - phase: 03-test-infrastructure
    provides: Test infrastructure conventions (t.Parallel, table-driven, stdlib only)
provides:
  - Config roundtrip and validation tests for all sub-configs
  - Player volume conversion characterization tests
  - State mapping coverage for mediacontrols integration
affects: [05-database-query-tests, 06-sql-consolidation]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Internal package tests (same package) for unexported access"
    - "Float comparison with math.Abs tolerance for volume tests"
    - "Characterization roundtrip with ±1 tolerance for int-truncated conversions"

key-files:
  created:
    - backend/config/config_test.go
    - backend/theme/config_test.go
    - backend/tracklist/config_test.go
    - backend/favorites/config_test.go
    - backend/library/config_test.go
    - backend/player/volume_test.go
  modified: []

key-decisions:
  - "Roundtrip test uses ±1 tolerance: ToVolume/ToUserVolume uses int truncation not rounding, causing up to 1 unit drift"
  - "Empty AccentColor not tested as invalid: Validate() calls ApplyDefaults() first, filling in the default value"

patterns-established:
  - "Config validation tests: table-driven subtests for valid/invalid enum values"
  - "Volume characterization: boundary values exact, full-range roundtrip within tolerance"

requirements-completed: [TEST-04, TEST-05]

# Metrics
duration: 4min
completed: 2026-03-03
---

# Phase 04 Plan 02: Config & Player Tests Summary

**Unit tests for config load/save roundtrip, all sub-config validators (theme/tracklist/favorites/library), and player volume conversion + state mapping — 27 test cases across 6 packages, all passing with -race**

## Performance

- **Duration:** 4 min
- **Started:** 2026-03-03T21:57:19Z
- **Completed:** 2026-03-03T22:02:12Z
- **Tasks:** 2
- **Files modified:** 6

## Accomplishments
- Config load/save roundtrip test verifies all fields survive TOML serialization
- All 4 sub-config validators (theme, tracklist, favorites, library) tested for valid values, invalid values, and defaults
- Player volume conversion tested at all boundaries with full 0-100 roundtrip characterization
- stateToMediaControls mapping verified for all states including unknown fallback
- All tests pass with `-race` flag

## Task Commits

Each task was committed atomically:

1. **Task 1: Config and sub-config validation tests** - `f9b2ad9` (test)
2. **Task 2: Player volume and state mapping tests** - `294b629` (test)

## Files Created/Modified
- `backend/config/config_test.go` - Load/Save roundtrip, missing file, composed errors, nil defaults (227 lines)
- `backend/theme/config_test.go` - Hex color regex + background shade enum validation (83 lines)
- `backend/tracklist/config_test.go` - Column ID recognition + duplicate detection (73 lines)
- `backend/favorites/config_test.go` - Icon style enum validation (49 lines)
- `backend/library/config_test.go` - Directory existence + scan concurrency mode validation (83 lines)
- `backend/player/volume_test.go` - Volume conversion, clamp, state mapping (198 lines)

## Decisions Made
- **Roundtrip tolerance:** The `ToVolume`/`ToUserVolume` conversion uses `int()` truncation (not `math.Round`), so some values lose 1 unit in the roundtrip. The characterization test documents this with a ±1 tolerance, while verifying boundary values (0, 50, 100) are exact.
- **Empty AccentColor not invalid:** `Validate()` calls `ApplyDefaults()` first, which fills empty accent color with `#ffd43b`, so empty string is handled gracefully rather than being an error case.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Removed empty-string hex color from invalid test cases**
- **Found during:** Task 1 (theme validation tests)
- **Issue:** Plan listed empty string as invalid hex color, but `Validate()` calls `ApplyDefaults()` first which fills in the default color
- **Fix:** Removed empty string from invalid test cases — it's valid behavior by design
- **Files modified:** backend/theme/config_test.go
- **Verification:** All theme tests pass
- **Committed in:** f9b2ad9 (Task 1 commit)

**2. [Rule 1 - Bug] Changed roundtrip test from exact to ±1 tolerance**
- **Found during:** Task 2 (volume roundtrip test)
- **Issue:** Plan specified exact roundtrip match for all 0-100 values, but `ToUserVolume()` uses `int()` truncation causing up to 1 unit drift
- **Fix:** Changed to ±1 tolerance with separate exact checks for boundary values (0, 50, 100)
- **Files modified:** backend/player/volume_test.go
- **Verification:** All player tests pass with -race
- **Committed in:** 294b629 (Task 2 commit)

---

**Total deviations:** 2 auto-fixed (2 bugs — plan assumptions didn't match actual code behavior)
**Impact on plan:** Both fixes accurately characterize existing behavior rather than imposing incorrect expectations. No scope creep.

## Issues Encountered
None

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Config and player pure logic fully characterized with tests
- Ready for remaining Phase 4 plans (queue tests) or Phase 5 (database query tests)

## Self-Check: PASSED

All 6 created files verified on disk. Both commits (f9b2ad9, 294b629) verified in git log.

---
*Phase: 04-queue-config-player-tests*
*Completed: 2026-03-03*
