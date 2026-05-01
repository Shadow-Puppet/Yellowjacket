# 010 — Autotag: Review UI & Apply Pipeline

> Third v1.3 phase. User reviews one album at a time, sees the diff clearly, and applies or skips — file tags get written, DB gets synced, cover art follows the never-replace-existing rule.

**Status:** pending · **Requirements:** REVIEW-01..07 · **Depends on:** 009 (needs candidates + scores)

## Success criteria

1. `/autotag` shows the next pending album with its top candidate as a field-by-field diff. Missing-from-local and extra-in-local tracks are shown explicitly.
2. Keyboard shortcuts work without the mouse: `A` apply, `M` more candidates, `S` skip, `L` leave-as-is, `U` paste URL, `→`/`←` navigate.
3. Apply writes tags to every track in the group via the existing format-specific writers + atomic write + DB sync + FTS5 sync. Whitelisted fields only: title, artist, album, album-artist, year, track#, track-total, disc#, disc-total, all MBIDs.
4. Cover art rule: embed only when the file has no existing art **and** CAA returns ≥500 px on the shortest side. Never replace existing embedded art (auto or manual).
5. First-ever apply per library shows an irreversibility warning. "Don't show again" sets a flag on the `libraries` row; never shows again for that library.
6. While the user reviews album N, candidates for album N+1 are prefetched into `http_cache` so advancing feels instant.
7. "Paste MB URL" dialog accepts a release URL, extracts the MBID, runs one `LookupRelease`, renders the diff against the current album.

## Sub-plans

- Wails bindings — `StartAutotagQueue`, `GetCurrentCandidate`, `GetCandidates(groupKey)`, `Apply(groupKey, releaseMBID)`, `Skip`, `LeaveAsIs(groupKey)`, `RetagGroup(groupKey)`.
- `/autotag` page layout — focused album header, diff table, candidate sidebar, missing/extra panel.
- Keyboard shortcut wiring through the existing scope-aware dispatch.
- Apply pipeline integration with existing tag writers + DB sync.
- Cover art apply rule + CAA fetch + 500 px minimum check.
- File-write warning dialog with per-library persistence.
- Prefetch-next-album goroutine, rate-limiter aware.
- Paste-MB-URL escape hatch.

## Risk callout

Every apply rewrites a file. The `AtomicWrite` pipeline mitigates corruption risk; the per-library warning mitigates surprise. Dry-run mode (from 009) lets developers validate scoring changes without file writes.
