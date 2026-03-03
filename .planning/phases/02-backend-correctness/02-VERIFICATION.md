---
phase: 02-backend-correctness
verified: 2026-03-03T00:30:00Z
status: passed
score: 5/5 must-haves verified
---

# Phase 2: Backend Correctness Verification Report

**Phase Goal:** All known error handling gaps are closed, configuration is secure, and the backend reports problems honestly instead of swallowing them
**Verified:** 2026-03-03T00:30:00Z
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | The package-level `startupErr` variable no longer exists; startup errors are stored in a YellowJacketApp struct field | ✓ VERIFIED | `grep "^var startupErr" backend/app.go` returns nothing; `startupErr error` at line 42 is a struct field; `yj.startupErr` used at lines 153, 154, 263, 264 |
| 2 | Config files are written with 0o644 permissions | ✓ VERIFIED | `os.WriteFile(c.filePath, confFileData, 0o644)` at line 152 of config.go; no `0o666` anywhere in the file |
| 3 | MPRIS lifecycle callback errors (Pause, Seek) appear in the application log instead of being silently discarded | ✓ VERIFIED | 4 `yj.logger.Warn("MPRIS ... failed"` calls at lines 184, 190, 198, 205 in app.go; no `_ = yj.player` anywhere in app.go |
| 4 | Artist credit link creation checks the actual error — only UNIQUE constraint violations are ignored, all other errors are surfaced | ✓ VERIFIED | `database.IsUniqueViolation(err)` check at line 1127 of library.go; non-unique errors logged and sent to `metrics.addWarning` at lines 1128-1141; no `_, _ = q.CreateArtistCreditArtist` remains |
| 5 | Library.Scan() returns warnings in ScanMetrics and fatal errors in the error return | ✓ VERIFIED | `scanErr` at line 225 only set from `commitBatch` fatal tx commit errors (line 391); 11 `metrics.addWarning` calls for walk/extraction/commit/orphan/variant paths; `handleConfigUpdate` at line 1365 captures `scanMetrics` and logs `scanMetrics.Warnings` count |

**Score:** 5/5 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `backend/app.go` | Startup error as struct field + MPRIS error logging | ✓ VERIFIED | `startupErr error` struct field line 42; 4 MPRIS Warn log calls |
| `backend/config/config.go` | Secure config file permissions | ✓ VERIFIED | `0o644` at line 152 |
| `backend/database/errors.go` | IsUniqueViolation helper | ✓ VERIFIED | 20 lines, exports `IsUniqueViolation`, uses `sqlite3.SQLITE_CONSTRAINT_UNIQUE` |
| `backend/database/database.go` | Migration 3: UNIQUE index on artist_credit_artist | ✓ VERIFIED | `version < 3` block at line 224; deduplicates then creates `idx_artist_credit_artist_unique` |
| `backend/library/metrics.go` | ScanWarning struct and addWarning method | ✓ VERIFIED | `ScanWarning` struct (lines 58-62) with FilePath/Phase/Err; `Warnings []ScanWarning` field (line 54); mutex-protected `addWarning` method (lines 94-103) |
| `backend/library/library.go` | Reclassified error paths + updated cachedLinkArtist | ✓ VERIFIED | 11 `metrics.addWarning` calls; `database.IsUniqueViolation` at line 1127; `handleConfigUpdate` captures scan metrics |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `app.go:OnStartup` | `app.go:OnDomReady` | `yj.startupErr` field | ✓ WIRED | Set at line 153, checked at line 263 — no package-level var involved |
| `app.go:MPRIS callbacks` | `yj.logger` | Warn log on Pause/Seek/Stop error | ✓ WIRED | 4 calls at lines 184, 190, 198, 205 |
| `library.go:cachedLinkArtist` | `database/errors.go:IsUniqueViolation` | Error check on CreateArtistCreditArtist | ✓ WIRED | `database.IsUniqueViolation(err)` at line 1127; import at line 22 |
| `library.go:Scan` | `metrics.go:addWarning` | Non-fatal errors reclassified | ✓ WIRED | 11 calls across walk, extraction, commit, orphan, variant, FTS paths |
| `database.go:runMigrations` | artist_credit_artist table | Migration 3 UNIQUE index | ✓ WIRED | `idx_artist_credit_artist_unique` at line 245; dedup + PRAGMA user_version = 3 |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|------------|-------------|--------|----------|
| CORR-05 | 02-01 | Package-level startupErr moved to struct field | ✓ SATISFIED | No `var startupErr` in app.go; `startupErr error` as struct field; all references use `yj.startupErr` |
| CORR-06 | 02-01 | Config file written with 0o644 permissions | ✓ SATISFIED | `0o644` at config.go:152; no `0o666` anywhere |
| CORR-07 | 02-01 | MPRIS callback errors logged instead of swallowed | ✓ SATISFIED | 4 Warn-level log calls for Pause, PlayPause(pause), Stop, Seek; no discarded `_ = yj.player` |
| CORR-08 | 02-02 | Artist credit link error properly checked | ✓ SATISFIED | `database.IsUniqueViolation` check; non-unique errors become warnings; migration 3 adds UNIQUE index |
| CORR-09 | 02-02 | Scan() separates warnings from fatal errors | ✓ SATISFIED | `scanErr` only for fatal tx commits; 11 `addWarning` calls; `handleConfigUpdate` logs warning count |

**Orphaned requirements:** None. All 5 requirement IDs (CORR-05 through CORR-09) from REQUIREMENTS.md Phase 2 are covered by plans 02-01 and 02-02.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| — | — | None found | — | — |

No TODOs, FIXMEs, placeholders, or empty implementations found in any modified files. `go vet ./backend/...` passes. `go build ./backend/...` compiles cleanly.

### Human Verification Required

### 1. MPRIS Error Logging Under Real Conditions

**Test:** Trigger MPRIS Pause/Stop/Seek while the player is in a state that causes failure (e.g., no audio loaded)
**Expected:** Warn-level log lines appear with "MPRIS Pause failed" / "MPRIS Stop failed" / "MPRIS Seek failed"
**Why human:** Requires a running Linux desktop with MPRIS-capable media key events and specific player error states

### 2. Config File Permissions on Disk

**Test:** After app writes config, run `stat -c '%a' ~/.config/yellowjacket/config.toml`
**Expected:** Shows `644`
**Why human:** Requires running the actual app to trigger config write; umask may interact

### 3. Scan Warning Accumulation End-to-End

**Test:** Scan a library with some corrupted/unreadable audio files
**Expected:** `Scan()` returns non-nil `ScanMetrics.Warnings` with entries for failed files, while the overall `error` return is nil (scan completed)
**Why human:** Requires crafted test files with specific corruption patterns

### Gaps Summary

No gaps found. All 5 success criteria from the ROADMAP are verified:

1. ✓ Package-level `startupErr` eliminated, struct field in place
2. ✓ Config written with `0o644`
3. ✓ All 4 MPRIS callbacks log errors at Warn level
4. ✓ `cachedLinkArtist` checks errors via `IsUniqueViolation`, surfaces non-unique failures
5. ✓ `Scan()` error return is fatal-only; warnings accumulated in `ScanMetrics.Warnings`; `handleConfigUpdate` logs warning count

All commits verified: `2a86408`, `0860b2f`, `e6866de` exist in git history.

---

_Verified: 2026-03-03T00:30:00Z_
_Verifier: Claude (gsd-verifier)_
