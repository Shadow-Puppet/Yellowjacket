---
phase: 15-schema-migration-write-safety
plan: 02
subsystem: database
tags: [atomic-write, file-safety, os-rename, temp-file]

# Dependency graph
requires:
  - phase: none
    provides: standalone utility package
provides:
  - General-purpose AtomicWrite function for safe file modifications
  - ErrCrossDevice sentinel for cross-filesystem detection
  - Orphan .yj-tmp cleanup on each write operation
affects: [16-tag-writing-database-sync, 19-ogg-vorbis-tag-writing]

# Tech tracking
tech-stack:
  added: []
  patterns: [write-to-temp-then-rename, callback-API, deterministic-temp-suffix]

key-files:
  created:
    - backend/fileutil/atomicwrite.go
    - backend/fileutil/atomicwrite_test.go
  modified: []

key-decisions:
  - "Used *slog.Logger as first parameter for consistency with codebase conventions"
  - "Deterministic .yj-tmp suffix (not random) enables reliable orphan cleanup"
  - "Cross-device rejection via ErrCrossDevice sentinel wrapping syscall.EXDEV — no copy fallback"
  - "Default 0644 permissions for new files; stat-and-preserve for existing files"

patterns-established:
  - "AtomicWrite callback API: AtomicWrite(logger, path, func(tmp *os.File) error) error"
  - "Deterministic temp file suffix .yj-tmp for all atomic writes"

requirements-completed: [SCHEMA-02, WRITE-05]

# Metrics
duration: 16min
completed: 2026-03-16
---

# Phase 15 Plan 02: Atomic Write Utility Summary

**General-purpose AtomicWrite function with write-to-temp-then-rename, permission preservation, orphan cleanup, and cross-device rejection**

## Performance

- **Duration:** 16 min
- **Started:** 2026-03-16T21:57:34Z
- **Completed:** 2026-03-16T22:13:45Z
- **Tasks:** 2
- **Files modified:** 2

## Accomplishments
- Created `backend/fileutil` package with exported `AtomicWrite` function using callback API pattern
- Implemented deterministic `.yj-tmp` temp file suffix with automatic orphan cleanup
- Permission preservation (stat existing target, apply mode before rename) with 0644 default for new files
- Cross-device rejection via `ErrCrossDevice` sentinel wrapping `syscall.EXDEV`
- 7 comprehensive test functions covering success, new file, callback error rollback, orphan cleanup, same-dir constraint, permission preservation (3 modes), and 1MiB sync verification

## Task Commits

Each task was committed atomically:

1. **Task 1: Create backend/fileutil package with AtomicWrite** - `4d64b5d` (feat)
2. **Task 2: Add comprehensive tests for AtomicWrite** - `0cdfe48` (test)

## Files Created/Modified
- `backend/fileutil/atomicwrite.go` - AtomicWrite function with ErrCrossDevice sentinel, orphan cleanup, permission preservation, cross-device rejection
- `backend/fileutil/atomicwrite_test.go` - 7 test functions: success, new file, callback error, orphan cleanup, same-dir temp, permission preservation (table-driven), sync and close

## Decisions Made
- Used `*slog.Logger` as the first parameter for consistency with the codebase convention (all packages accept logger as first arg)
- Deterministic `.yj-tmp` suffix instead of random temp file names — enables reliable orphan cleanup without directory scanning
- Cross-device rejection wraps both `ErrCrossDevice` and `syscall.EXDEV` using Go 1.20+ multi-`%w` in `fmt.Errorf`
- Default 0644 permissions for new files (target doesn't exist); stat-and-preserve for existing files

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Fixed errorlint violation in cross-device error wrapping**
- **Found during:** Task 1 (AtomicWrite implementation)
- **Issue:** `fmt.Errorf("%w: %s", ErrCrossDevice, err)` used `%s` for the second error, violating the `errorlint` linter rule that requires `%w` for all error format verbs
- **Fix:** Changed to `fmt.Errorf("%w: %w", ErrCrossDevice, err)` using Go 1.20+ multi-wrapping
- **Files modified:** backend/fileutil/atomicwrite.go
- **Verification:** `golangci-lint run` passes with 0 issues
- **Committed in:** 4d64b5d (Task 1 commit)

**2. [Rule 1 - Bug] Fixed err113 lint violation in test code**
- **Found during:** Task 2 (test implementation)
- **Issue:** `errors.New("simulated write failure")` defined inline in test function violated `err113` linter (dynamic error creation)
- **Fix:** Extracted to package-level `var errSimulatedFailure = errors.New("simulated write failure")`
- **Files modified:** backend/fileutil/atomicwrite_test.go
- **Verification:** `golangci-lint run` passes with 0 issues
- **Committed in:** 0cdfe48 (Task 2 commit)

---

**Total deviations:** 2 auto-fixed (2 bugs — linter violations)
**Impact on plan:** Both auto-fixes necessary for lint compliance. No scope creep.

## Issues Encountered
None

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- AtomicWrite utility ready for Phase 16 tag writers to import
- No blockers — Phase 15 infrastructure complete (Plan 01: FTS5 migration, Plan 02: atomic write)

---
*Phase: 15-schema-migration-write-safety*
*Completed: 2026-03-16*
