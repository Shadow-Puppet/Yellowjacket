# 012 — Autotag: Settings & Polish

> Fifth and final v1.3 phase. Configuration surfaces in the existing settings system; the known sharp edges (rate-limit contention, VA compilations, singleton files) get sanded; the fingerprinting seam is in place for the future.

**Status:** pending · **Requirements:** CFG-01..06 · **Depends on:** 011

## Success criteria

1. Autotag settings panel accessible via the existing templ/HTMX settings UI. Exposes: enable/disable auto-accept, per-library file-write warning reset, default review filter, default sort order.
2. Shared rate limiter distinguishes interactive from background requests. User-initiated MB calls (paste-URL, opening a review, explore browsing) are never blocked behind a running auto-accept job.
3. VA compilation albums (per-track artist credits differ from album-artist) are detected. The auto-accept artist-match rule relaxes for them; the ranker prefers MB releases credited to "Various Artists".
4. Singleton files (`track_count = 1`, no sibling context) use a recording-level match path (`SearchRecordings` with title + artist + length filters). Lower confidence ceiling — never eligible for auto-accept regardless of confidence.
5. `type Identifier interface { Identify(path) ([]Candidate, error) }` exists with `MetadataIdentifier` as the v1 implementation. No fpcalc integration, but the seam is in place for a future `AcoustIDIdentifier`.
6. User-facing quickstart docs exist; CLAUDE.md gets a `backend/autotag/` package description; scoring-function dev notes are committed.

## Sub-plans

- Autotag settings panel (templ + HTMX).
- Rate-limiter priority support.
- VA compilation detection and scoring adjustments.
- Singleton-file match path.
- `Identifier` interface seam with `MetadataIdentifier`.
- Docs — user quickstart + dev notes + CLAUDE.md update.

## Ship criteria for v1.3 overall

- All 29 SCHEMA/MATCH/REVIEW/AUTO/CFG requirements complete.
- All five phases' success criteria verified end-to-end on a real library (10k+ tracks, mixed match quality).
- Auto-accept run against a well-tagged subset produces zero incorrect matches.
- Manual review workflow can process 100 albums in under 30 minutes without mouse use.
- Recap files moved to `.planning/plans/completed/`.
