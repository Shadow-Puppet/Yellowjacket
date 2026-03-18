# Phase 15: Schema Migration & Write Safety - Context

**Gathered:** 2026-03-16
**Status:** Ready for planning

<domain>
## Phase Boundary

Migrate FTS5 search_index from `content=''` to `contentless_delete=1` so rows can be deleted/updated without dropping the entire index. Build a general-purpose atomic file write utility (write-to-temp-then-rename) that Phase 16+ tag writers will use to safely modify audio files. This phase is pure backend infrastructure — no UI, no tag writing, no format-specific code.

</domain>

<decisions>
## Implementation Decisions

### Migration experience
- Blocking startup migration — app waits for FTS5 rebuild to complete before showing UI
- Silent — no user-facing notification or progress indicator. For most libraries the rebuild is sub-second
- If migration fails (corrupted DB, disk full), fail startup with an error. Don't let the app run with a broken search index. Suggest "delete DB and rescan" as recovery
- Migration must be idempotent — safe to re-run if interrupted. Drop-and-rebuild is naturally idempotent. If app crashes mid-migration, next startup just re-runs it
- Follows the existing migration pattern (migration 2 already does FTS5 rebuild on startup)

### Temp file cleanup policy
- Temp files use `.yj-tmp` suffix — e.g., `song.mp3.yj-tmp`. App-specific suffix prevents accidental deletion of unrelated temp files
- Cleanup happens only during tag write operations — before writing a file, check for and remove any orphaned `.yj-tmp` file for that specific target. No global startup scan of library directories
- Cleanup logged at debug level only — not visible unless debug logging is enabled
- If an orphaned temp file can't be deleted (permissions, file lock), log a warning and continue. Don't block the write operation. Stale temp files are harmless (just wasted disk space)

### Atomic write scope
- General-purpose utility — not audio-file-specific. Standalone function that accepts any file path + writer function. Tag writers call it, but it could serve config files, playlists, etc. in the future
- Callback API pattern: `AtomicWrite(targetPath, func(tempFile) error)` — caller writes to the temp file via callback, utility handles create/rename/cleanup. Clean and hard to misuse
- Cross-filesystem writes rejected with a clear error — no fallback to copy-then-delete. The success criteria already require "cross-directory rejection" as a test case
- Preserve original file permissions — stat the target before writing, apply same mode to temp file. If target doesn't exist, use 0644

### Claude's Discretion
- Exact package location for the atomic write utility (likely a new package under `backend/` or added to an existing utility package)
- Migration version numbering — fits into the existing numbered migration sequence
- Internal implementation details of the FTS5 DELETE command after migration (standard `DELETE FROM search_index(search_index, rowid, ...)` syntax)
- Test file fixtures and test helper organization

</decisions>

<specifics>
## Specific Ideas

- The current `DeleteSearchIndex` is a no-op with a comment explaining the contentless FTS5 limitation — this becomes a real DELETE after migration
- The current `ClearSearchIndex` drops and recreates the table — after migration it can use `DELETE FROM search_index` instead (or keep drop/recreate for full rebuilds)
- Existing migration 2 (`applyMigration2`) already does FTS5 rebuild on startup — new migration follows the same pattern
- The `content=''` → `contentless_delete=1` migration requires the table to also have `content=''` (it's an addition, not a replacement). SQLite docs: both options are set together

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope

</deferred>

---

*Phase: 15-schema-migration-write-safety*
*Context gathered: 2026-03-16*
