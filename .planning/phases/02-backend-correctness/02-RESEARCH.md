# Phase 2: Backend Correctness - Research

**Researched:** 2026-03-02
**Domain:** Go backend error handling, SQLite constraint detection, file permissions, structured logging
**Confidence:** HIGH

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
- **CORR-05 (Startup error):** Move package-level `startupErr` to a private `startupErr error` field on `YellowJacketApp`. Keep `OnDomReady` check+quit behavior. No public getter. Continue `errors.Join` accumulation in `OnStartup`. Log only in `OnDomReady`.
- **CORR-06 (Config permissions):** Change `os.WriteFile` permission from `0o666` to `0o644` in `backend/config/config.go:152`. One-line change.
- **CORR-07 (MPRIS callbacks):** Log errors for ALL MPRIS callbacks that call fallible player methods — OnPause, OnPlayPause, OnStop, OnSeek (in `backend/app.go:181-203`). Log and move on, no retry logic.
- **CORR-08 (Artist credit link errors):** Check actual error in `cachedLinkArtist` (`backend/library/library.go:1101`). Only UNIQUE constraint violations are silently ignored. Use `sqlite3.ErrConstraintUnique` error code (2067) — not string matching. Create shared `isUniqueViolation(err error) bool` helper in `backend/database` package. Non-UNIQUE errors become scan warnings.
- **CORR-09 (Scan error separation):** Keep existing `Scan() (*ScanMetrics, error)` signature. Add `Warnings []ScanWarning` field to `ScanMetrics`. `ScanWarning` struct has `FilePath string`, `Phase string` (extraction/commit/orphan), `Err error`. Fatal errors only in error return (DB connection loss, tx commit failures, context cancellation). Everything else is a warning. Callers log warnings at Warn level and only propagate fatal errors. No frontend notification for warnings.

### Claude's Discretion
- **CORR-07:** Log level (Warn vs Error) for MPRIS callback errors; whether to keep inline closures or extract to named methods.
- **CORR-08:** Whether `cachedLinkArtist` should return an error or accept a warnings collector to report non-UNIQUE failures.

### Deferred Ideas (OUT OF SCOPE)
None — discussion stayed within phase scope.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|-----------------|
| CORR-05 | Package-level startupErr variable is moved to a YellowJacketApp struct field | Simple struct field addition + variable removal. Pattern: move `var startupErr error` (app.go:134) to `startupErr error` field on `YellowJacketApp` struct (app.go:28). Update OnStartup (app.go:154) and OnDomReady (app.go:252) references. |
| CORR-06 | Config file is written with 0o644 permissions instead of 0o666 | One-line change at config.go:152. Change `os.FileMode(int(0o666))` to `0o644`. |
| CORR-07 | MPRIS lifecycle callback errors are logged instead of silently swallowed | Replace `_ = yj.player.Pause()` and `_ = yj.player.Seek(...)` with error checks and `logger.Warn()` calls in MPRIS callback closures. See Architecture Patterns for recommended approach. |
| CORR-08 | Artist credit link creation error is checked; only UNIQUE constraint violations are ignored | Create `IsUniqueViolation(err error) bool` helper in `backend/database` using `errors.As` with `*sqlite.Error` and code comparison against `sqlite3.SQLITE_CONSTRAINT_UNIQUE` (2067). Add UNIQUE constraint to `artist_credit_artist` schema. Update `cachedLinkArtist` to check errors. |
| CORR-09 | Library.Scan() separates warnings from fatal errors | Add `ScanWarning` struct and `Warnings []ScanWarning` slice to `ScanMetrics`. Reclassify errors throughout Scan() — extraction failures, individual file save failures, FTS indexing failures, orphan cleanup failures become warnings. Only DB connection/transaction failures remain fatal. Update `handleConfigUpdate` caller. |
</phase_requirements>

## Summary

This phase addresses five discrete error handling gaps in the YellowJacket backend. All changes are correctness improvements to existing code — no new features, no new dependencies. The changes are well-scoped: each requirement maps to a specific file location and can be implemented independently.

The most complex requirement is CORR-09 (scan error separation), which touches multiple phases of the `Scan()` function and requires reclassifying many error paths. The second most complex is CORR-08 (artist credit link errors), which requires adding a database helper, a schema migration, and modifying the `cachedLinkArtist` function. The remaining three (CORR-05, CORR-06, CORR-07) are straightforward mechanical changes.

A key discovery: the `artist_credit_artist` table currently has **no UNIQUE constraint** on `(artist_id, credit_id)`. The code relies on the in-memory `linkedCredits` cache to prevent duplicates within a scan, but across incremental scans, duplicate rows can be silently inserted. CORR-08 requires adding a UNIQUE constraint via a schema migration (migration 3) before the `isUniqueViolation` check becomes meaningful.

**Primary recommendation:** Implement in order CORR-06 → CORR-05 → CORR-07 → CORR-08 → CORR-09 (simplest first, building toward the most complex scan refactor last).

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `log/slog` | stdlib (Go 1.25) | Structured logging | Already used project-wide; all error logging should use this |
| `errors` | stdlib (Go 1.25) | Error wrapping, `errors.As`, `errors.Join` | Already used project-wide for error accumulation |
| `modernc.org/sqlite` | v1.45.0 | CGo-free SQLite driver | Already the project's database driver; provides `*sqlite.Error` with `.Code()` |
| `modernc.org/sqlite/lib` | (transitive) | SQLite constants | Provides `SQLITE_CONSTRAINT_UNIQUE = 2067` |

### Supporting
No additional libraries needed. All requirements are implementable with the existing stack.

### Alternatives Considered
None — all decisions are locked to existing project tooling.

## Architecture Patterns

### Pattern 1: SQLite Error Code Detection (CORR-08)
**What:** Type-assert the error to `*sqlite.Error` using `errors.As`, then check `.Code()` against the specific SQLite extended result code.
**When to use:** Any time the codebase needs to distinguish specific SQLite failure modes (UNIQUE violations, FOREIGN KEY violations, etc.)
**Why not string matching:** The `isDuplicateColumnErr` helper at `database.go:329` uses string matching (`strings.Contains(err.Error(), "duplicate column name")`). This is fragile — error messages can change across driver versions. The `*sqlite.Error` type with `.Code()` is the stable, correct approach for constraint violations.

```go
// backend/database/errors.go (new file)
package database

import (
    "errors"

    "modernc.org/sqlite"
    sqlite3 "modernc.org/sqlite/lib"
)

// IsUniqueViolation reports whether err is a SQLite UNIQUE
// constraint violation (extended result code 2067).
func IsUniqueViolation(err error) bool {
    var sqliteErr *sqlite.Error
    if errors.As(err, &sqliteErr) {
        return sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE
    }
    return false
}
```

**Confidence:** HIGH — verified from `modernc.org/sqlite@v1.45.0/error.go` source: `Error` struct has `Code() int` method, and `modernc.org/sqlite/lib` exports `SQLITE_CONSTRAINT_UNIQUE = 2067`.

### Pattern 2: MPRIS Callback Error Logging (CORR-07)
**What:** Replace discarded errors in MPRIS callback closures with log calls.
**When to use:** The four closures in `app.go:181-203` that call `player.Pause()` and `player.Seek()`.

**Recommendation (Claude's Discretion):**
- **Log level: `Warn`** — these are non-fatal conditions where the player couldn't execute a command (e.g., no audio stream loaded when MPRIS sends Pause). They don't indicate bugs, but they're noteworthy for debugging.
- **Keep inline closures** — extracting to named methods would add indirection for simple one-line error checks. The closures are already short and clear.

```go
// Current (app.go:183):
OnPause: func() { _ = yj.player.Pause() },

// After:
OnPause: func() {
    if err := yj.player.Pause(); err != nil {
        yj.logger.Warn("MPRIS Pause failed", "err", err)
    }
},
```

**Confidence:** HIGH — direct code inspection of app.go confirms exactly four closures need this treatment.

### Pattern 3: Scan Warning Collection (CORR-09)
**What:** Accumulate non-fatal errors as structured warnings in `ScanMetrics.Warnings` instead of mixing them into the error return.
**When to use:** Throughout `Scan()` and its helper functions for non-fatal failures.

**Thread safety note:** `ScanMetrics` already has a `sync.Mutex` protecting worker-pool fields. The `Warnings` slice will be appended from multiple goroutines (extraction workers, DB writer, orphan cleanup), so additions must go through a mutex-protected method.

```go
// backend/library/metrics.go additions:

// ScanWarning represents a non-fatal issue encountered during scanning.
type ScanWarning struct {
    FilePath string `json:"filePath"`
    Phase    string `json:"phase"`    // "extraction", "commit", "orphan"
    Err      error  `json:"err"`
}

// addWarning records a non-fatal scan issue. Safe for concurrent use.
func (m *ScanMetrics) addWarning(filePath, phase string, err error) {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.Warnings = append(m.Warnings, ScanWarning{
        FilePath: filePath,
        Phase:    phase,
        Err:      err,
    })
}
```

**Confidence:** HIGH — the existing `ScanMetrics.mu` pattern is proven (used by `addExtraction` and `addThumbnailTier`).

### Pattern 4: Schema Migration for UNIQUE Constraint (CORR-08)
**What:** Add migration 3 to create a UNIQUE index on `artist_credit_artist(artist_id, credit_id)`.
**Why needed:** The `artist_credit_artist` table currently has NO UNIQUE constraint. Without it, the `isUniqueViolation` check would never trigger — the INSERT would always succeed (creating duplicates). The migration must also deduplicate existing rows.

```go
// Migration 3: add UNIQUE constraint to artist_credit_artist
if version < 3 {
    logger.Info("applying migration 3: artist_credit_artist unique constraint")

    // Remove duplicates first (keep lowest ID per pair).
    if _, err := db.ExecContext(ctx, `
        DELETE FROM artist_credit_artist
        WHERE id NOT IN (
            SELECT MIN(id)
            FROM artist_credit_artist
            GROUP BY artist_id, credit_id
        )
    `); err != nil {
        return fmt.Errorf("migration 3: could not deduplicate: %w", err)
    }

    if _, err := db.ExecContext(ctx, `
        CREATE UNIQUE INDEX IF NOT EXISTS idx_artist_credit_artist_unique
        ON artist_credit_artist(artist_id, credit_id)
    `); err != nil {
        return fmt.Errorf("migration 3: could not create unique index: %w", err)
    }

    if _, err := db.ExecContext(ctx, "PRAGMA user_version = 3"); err != nil {
        return fmt.Errorf("could not set user_version to 3: %w", err)
    }
}
```

**Confidence:** HIGH — follows the existing migration pattern in `database.go:156-224`. SQLite supports `CREATE UNIQUE INDEX` for adding uniqueness constraints after table creation.

### Anti-Patterns to Avoid
- **String matching for SQLite errors:** The existing `isDuplicateColumnErr` uses `strings.Contains(err.Error(), ...)`. Don't follow this pattern for CORR-08. Use `errors.As` + `.Code()` instead.
- **Mixing warnings and fatal errors in the same return:** The current `Scan()` accumulates everything into `scanErr` and returns it. After CORR-09, the error return must ONLY contain fatal errors; non-fatal issues go to `ScanMetrics.Warnings`.
- **Logging in multiple places:** CORR-05 specifies logging only in `OnDomReady`, not also in `OnStartup`. Don't add a second log call.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| SQLite error code detection | String matching on error messages | `errors.As` + `*sqlite.Error` + `.Code()` | Error messages are implementation details; codes are stable API |
| Error accumulation | Manual slice building | `errors.Join` (stdlib) | Already used in the project; handles nil correctly |

**Key insight:** The project already uses `errors.Join` (app.go:154, library.go:321) and `log/slog` consistently. No new patterns needed — just applying existing patterns to currently-unhandled error paths.

## Common Pitfalls

### Pitfall 1: Missing UNIQUE Constraint for CORR-08
**What goes wrong:** Adding `isUniqueViolation` without a UNIQUE constraint on `artist_credit_artist(artist_id, credit_id)` makes the check dead code — the INSERT never fails, duplicates silently accumulate.
**Why it happens:** The schema at `artist_credit_artist.sql` defines no uniqueness constraint. The code relies on the in-memory `linkedCredits` cache, which is per-scan.
**How to avoid:** Add migration 3 with a UNIQUE index AND deduplicate existing rows before creating the index.
**Warning signs:** If `isUniqueViolation` is never triggered in logs, the constraint is missing.

### Pitfall 2: Thread Safety for ScanWarnings
**What goes wrong:** Appending to `ScanMetrics.Warnings` from multiple goroutines without synchronization causes data races.
**Why it happens:** The extraction worker pool runs concurrently with the DB writer goroutine. Both may produce warnings.
**How to avoid:** Use the existing `ScanMetrics.mu` mutex via an `addWarning` method, following the pattern of `addExtraction`.
**Warning signs:** `go test -race` failures in library scan tests.

### Pitfall 3: Breaking the Fatal/Warning Boundary
**What goes wrong:** Reclassifying a fatal error as a warning causes the scan to "succeed" when it actually failed catastrophically (e.g., database connection lost).
**Why it happens:** Judgment call errors when categorizing error paths in CORR-09.
**How to avoid:** Strict rule: transaction begin/commit failures and context cancellation are ALWAYS fatal. Individual file operations (save, FTS index, orphan delete) are ALWAYS warnings.
**Warning signs:** `handleConfigUpdate` silently succeeding when the database is actually down.

### Pitfall 4: MPRIS Callback Logger Access
**What goes wrong:** The MPRIS callbacks in `OnStartup` capture `yj.logger` in closures. If logger is nil, the app panics.
**Why it happens:** It can't — `yj.logger` is set in `NewYellowJacketApp` before `OnStartup` runs. But worth noting this is a closure capture, not a method call.
**How to avoid:** No action needed; just verify logger is never nil when closures execute.

### Pitfall 5: cachedLinkArtist Warning Propagation
**What goes wrong:** If `cachedLinkArtist` returns an error, the caller (`processMetadata`) might abort the entire file import for a non-critical failure.
**Why it happens:** Artist-credit-artist linking is optional — the file should still be imported even if this link fails.
**How to avoid:** Per the CONTEXT.md decision, non-UNIQUE errors become scan warnings. The function should either accept a warnings collector or call `metrics.addWarning` directly. Given the function already has access to `l.logger` and logs warnings internally, the cleanest approach is to pass `metrics` and call `addWarning` for non-UNIQUE errors, keeping the existing "log and continue" pattern.

## Code Examples

### CORR-05: Startup Error Field Migration
```go
// backend/app.go — struct change
type YellowJacketApp struct {
    // ... existing fields ...
    startupErr error // replaces package-level var
}

// backend/app.go — OnStartup change (line ~154)
// Before:
//   startupErr = errors.Join(startupErr, ...)
// After:
//   yj.startupErr = errors.Join(yj.startupErr, ...)

// backend/app.go — OnDomReady change (line ~252)
// Before:
//   if startupErr != nil {
// After:
//   if yj.startupErr != nil {
```

### CORR-06: Config Permissions Fix
```go
// backend/config/config.go:152
// Before:
err = os.WriteFile(c.filePath, confFileData, os.FileMode(int(0o666)))
// After:
err = os.WriteFile(c.filePath, confFileData, 0o644)
```

### CORR-07: MPRIS Error Logging (all four closures)
```go
// backend/app.go — OnStartup MPRIS callbacks
OnPause: func() {
    if err := yj.player.Pause(); err != nil {
        yj.logger.Warn("MPRIS Pause failed", "err", err)
    }
},
OnPlayPause: func() {
    if yj.player.IsPlaying() {
        if err := yj.player.Pause(); err != nil {
            yj.logger.Warn("MPRIS PlayPause(pause) failed", "err", err)
        }
    } else {
        yj.queue.Play()
    }
},
OnStop: func() {
    if err := yj.player.Pause(); err != nil {
        yj.logger.Warn("MPRIS Stop failed", "err", err)
    }
},
OnSeek: func(positionSec int) {
    if err := yj.player.Seek(positionSec); err != nil {
        yj.logger.Warn("MPRIS Seek failed", "err", err)
    }
},
```

### CORR-08: cachedLinkArtist with Error Checking
```go
// backend/library/library.go — updated cachedLinkArtist
func (l *Library) cachedLinkArtist(
    q *sqlcgen.Queries,
    cache *entityCache,
    metrics *ScanMetrics,
    name string,
    creditID int64,
) {
    // ... existing artist upsert logic unchanged ...

    linkKey := fmt.Sprintf("%d:%d", artist.ID, creditID)
    if _, done := cache.linkedCredits[linkKey]; done {
        return
    }

    _, err = q.CreateArtistCreditArtist(
        l.ctx,
        sqlcgen.CreateArtistCreditArtistParams{
            ArtistID: artist.ID,
            CreditID: creditID,
        },
    )
    if err != nil {
        if !database.IsUniqueViolation(err) {
            l.logger.Warn(
                "could not link artist to credit",
                "artist", name,
                "creditID", creditID,
                "err", err,
            )
            metrics.addWarning(name, "commit", fmt.Errorf(
                "artist-credit link failed for %q: %w", name, err,
            ))
        }
        // UNIQUE violation: link already exists in DB, not an error
    }

    cache.linkedCredits[linkKey] = struct{}{}
}
```

### CORR-09: Error Reclassification in Scan()
```go
// Fatal errors (error return):
// - l.db.Queries.GetAllAudioFiles fails (line 199)
// - l.db.BeginTx fails (commitBatch, line 659)
// - tx.Commit fails (commitBatch, line 702)
// - l.ctx.Err() — context cancellation

// Warnings (ScanMetrics.Warnings):
// - metadata extraction failures (line 429-439)
// - individual file save failures (commitBatch, line 691-698)
// - FTS indexing failures (saveAudioFile line 787-798, updateAudioFile line 866-893)
// - orphan delete failures (line 484-495)
// - orphan FTS delete failures (line 498-505)
// - WalkDir errors (line 319-328)
// - missing variant generation (line 518-523)
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `strings.Contains(err.Error(), ...)` for SQLite errors | `errors.As` + `*sqlite.Error` + `.Code()` | Available since modernc.org/sqlite added `Error` type | Stable error detection, independent of message wording |
| Package-level error variables | Struct fields | Go best practice | Avoids global state, enables testing |
| `0o666` file permissions | `0o644` for config files | Unix convention | Prevents world-write on config files |

## Open Questions

1. **Should `isDuplicateColumnErr` be updated to use `*sqlite.Error`?**
   - What we know: The existing helper at `database.go:329` uses string matching. It only runs during migrations, not hot paths.
   - What's unclear: Whether to refactor it as part of this phase or leave it for a future cleanup.
   - Recommendation: Out of scope for this phase. Note it as a future cleanup item but don't touch it now — it works and isn't a correctness issue.

2. **Should `cachedLinkArtist` signature change?**
   - What we know: The CONTEXT.md leaves this as Claude's discretion — either return an error or accept a warnings collector.
   - Recommendation: **Pass `metrics *ScanMetrics` as an additional parameter** and call `metrics.addWarning()` directly. This avoids changing the return type (which would require updating all callers) and follows the existing pattern where `cachedLinkArtist` logs and continues. The function already has access to the logger — adding metrics access is the minimal change.

3. **Existing duplicate rows in `artist_credit_artist`?**
   - What we know: Without a UNIQUE constraint, duplicate `(artist_id, credit_id)` rows may exist from past incremental scans where the cache was reset.
   - Recommendation: Migration 3 must deduplicate before adding the UNIQUE index (see Architecture Pattern 4).

## Sources

### Primary (HIGH confidence)
- `modernc.org/sqlite@v1.45.0/error.go` — verified `Error` struct with `Code() int` method
- `modernc.org/sqlite/lib` — verified `SQLITE_CONSTRAINT_UNIQUE = 2067` constant
- Direct code inspection of all affected files in the repository

### Secondary (MEDIUM confidence)
- Go stdlib `errors.As` documentation — standard unwrapping pattern for type-asserting wrapped errors

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — all existing project dependencies, no new additions
- Architecture: HIGH — all patterns verified against actual source code in the repository
- Pitfalls: HIGH — identified through direct code inspection of thread safety, schema gaps, and error flow

**Research date:** 2026-03-02
**Valid until:** 2026-04-02 (stable — no external dependency changes expected)
