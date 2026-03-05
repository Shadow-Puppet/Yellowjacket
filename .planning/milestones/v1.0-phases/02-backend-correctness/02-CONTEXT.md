# Phase 2: Backend Correctness - Context

**Gathered:** 2026-03-02
**Status:** Ready for planning

<domain>
## Phase Boundary

Fix all known error handling gaps in the backend: eliminate the package-level `startupErr` variable, secure config file permissions, log MPRIS callback errors, check artist credit link errors properly, and separate library scan warnings from fatal errors. The backend should report problems honestly instead of swallowing them. No new features — only correctness improvements to existing code.

Requirements: CORR-05, CORR-06, CORR-07, CORR-08, CORR-09

</domain>

<decisions>
## Implementation Decisions

### Startup error handling (CORR-05)
- Move the package-level `startupErr` variable (`backend/app.go:134`) to a private `startupErr error` field on the `YellowJacketApp` struct
- Keep the current behavior: `OnDomReady` checks the field, logs the error, and calls `Quit(ctx)` — the app exits on startup failure
- No public getter — the field is only accessed internally by `OnDomReady`
- Continue accumulating errors with `errors.Join` in `OnStartup` — run all initialization, collect all failures, report them together
- Log the error only in `OnDomReady` (not also in `OnStartup`) — avoid duplicate log lines

### Config file permissions (CORR-06)
- Change `os.WriteFile` permission from `0o666` to `0o644` in `backend/config/config.go:152`
- Straightforward one-line change — no design decisions needed

### MPRIS callback error logging (CORR-07)
- Log errors for ALL MPRIS callbacks that call fallible player methods, not just Pause and Seek — includes OnPause, OnPlayPause, OnStop, and OnSeek closures in `backend/app.go:181-203`
- Log and move on — no retry logic, no recovery attempts
- Claude decides: log level (Warn vs Error) and whether to keep inline closures or extract to named methods

### Artist credit link error checking (CORR-08)
- In `backend/library/library.go:1101`, `cachedLinkArtist` currently discards both return values from `CreateArtistCreditArtist` with `_, _`
- Check the actual error: only UNIQUE constraint violations should be silently ignored
- Use `sqlite3.ErrConstraintUnique` error code (2067) for detection — not string matching
- Create a shared `isUniqueViolation(err error) bool` helper in the `backend/database` package — reusable across the codebase for other upsert patterns
- Non-UNIQUE errors become scan warnings (log and continue) — the file still gets imported, it just won't have the artist-credit-artist link
- Claude decides: whether `cachedLinkArtist` should return an error or accept a warnings collector to report non-UNIQUE failures

### Scan error separation (CORR-09)
- Keep the existing `Scan() (*ScanMetrics, error)` signature — do not add a third return value
- Add a `Warnings []ScanWarning` field to the `ScanMetrics` struct in `backend/library/metrics.go`
- `ScanWarning` is a structured type with `FilePath string`, `Phase string` (extraction/commit/orphan), and `Err error` fields
- The `error` return from `Scan()` is reserved for fatal errors only — database connection loss, transaction commit failures, context cancellation
- Everything else is a warning: metadata extraction failures, individual file save failures, FTS indexing failures, orphan cleanup failures
- Directory walk failures (`WalkDir` returning an error) are warnings, not fatal — the scan can still process files already discovered
- Callers like `handleConfigUpdate` log warnings at Warn level and only propagate fatal errors
- No frontend notification for warnings — they stay in logs only

</decisions>

<specifics>
## Specific Ideas

No specific requirements — open to standard approaches. The success criteria in the roadmap are precise enough to guide implementation.

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope.

</deferred>

---

*Phase: 02-backend-correctness*
*Context gathered: 2026-03-02*
