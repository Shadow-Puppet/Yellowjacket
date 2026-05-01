# YellowJacket — Roadmap

A cross-platform desktop music player. Plays local MP3 / FLAC / OGG / WAV; manages multiple library directories via SQLite; queue, playlists, smart playlists, play history, full-text search, cover art, MPRIS controls on Linux, full single + batch tag editing across all four formats, and a read-only MusicBrainz / ListenBrainz catalog browser.

The guiding rule for everything below is "the music player works reliably and feels solid" — every interaction should be correct, responsive, and trustworthy. New surface area only goes in once the foundation under it is stable.

## Capability set

- **Playback** — play / pause / seek / volume on MP3, FLAC, OGG, WAV. Queue with shuffle (Fisher-Yates), repeat (off / one / all), auto-advance, persistent across restarts.
- **Library** — multiple library directories, concurrent metadata extraction, adaptive scan concurrency by disk type, cancellable / pausable scans, cross-library playlists with phantom-track preservation when a library is removed.
- **Search & browse** — FTS5 across tracks/artists/albums/paths; browse by albums / artists / genres; library filter respected everywhere; virtual scrolling.
- **Playlists** — CRUD, M3U8 import/export, favorites, smart playlists (rule-based saved queries with combobox editor + live preview), default playlist.
- **Tag editing** — single + batch edit of 8 fields across all four formats; cover art embed/replace/remove; crash-safe atomic writes; instant DB + FTS5 sync, no rescan needed.
- **Play history** — natural-finish play counting, `last_played` timestamp, play-history log, integration into smart playlist rule fields.
- **Explore** — read-only MusicBrainz / ListenBrainz browser: search → artist page → album detail with release-version selection, with rate-limited APIs and SQLite-cached responses.
- **Configuration & system** — TOML config with live reload, configurable keyboard shortcuts (record-style capture, scope-aware dispatch), theme tokens (accent / background shade), MPRIS2 on Linux.

## Milestone sequence

| # | Milestone | Status | File |
|---|-----------|--------|------|
| 001 | v1.0 Consolidation | shipped 2026-03-05 | [completed/001-v1.0-consolidation.md](plans/completed/001-v1.0-consolidation.md) |
| 002 | v1.1 Multi-Library Support | shipped 2026-03-16 | [completed/002-v1.1-multi-library.md](plans/completed/002-v1.1-multi-library.md) |
| 003 | v1.2 Tag Editing (MP3 + FLAC) | shipped 2026-03-18 | [completed/003-v1.2-tag-editing.md](plans/completed/003-v1.2-tag-editing.md) |
| 004 | v1.2.1 Format Parity (OGG + WAV) | shipped 2026-03-21 | [completed/004-v1.2.1-format-parity.md](plans/completed/004-v1.2.1-format-parity.md) |
| 005 | Smart Playlists | shipped 2026-03-22 | [completed/005-smart-playlists.md](plans/completed/005-smart-playlists.md) |
| 006 | Play History & Play Count | shipped 2026-03-22 | [completed/006-play-history.md](plans/completed/006-play-history.md) |
| 007 | MusicBrainz/ListenBrainz Explore Browser | shipped 2026-03-24 | [completed/007-explore-browser.md](plans/completed/007-explore-browser.md) |
| 008 | Autotag — Schema & Grouping Foundation | shipped 2026-04-20 | [completed/008-autotag-schema-grouping.md](plans/completed/008-autotag-schema-grouping.md) |
| 009 | Autotag — Scoring Engine & MB Orchestration | shipped 2026-04-21 | [completed/009-autotag-scoring-engine.md](plans/completed/009-autotag-scoring-engine.md) |
| 010 | Autotag — Review UI & Apply Pipeline | **active** | [active/010-autotag-review-ui.md](plans/active/010-autotag-review-ui.md) |
| 011 | Autotag — Auto-Accept & Entry Points | pending | [pending/011-autotag-auto-accept.md](plans/pending/011-autotag-auto-accept.md) |
| 012 | Autotag — Settings & Polish | pending | [pending/012-autotag-settings-polish.md](plans/pending/012-autotag-settings-polish.md) |

The active phase is the **MusicBrainz autotagger** (collectively v1.3). It builds on the explore-browser API client + cache foundation from milestone 007. Plans 008-012 are sequential — each depends on the prior one. See `NOTES.md` for the API-call-minimization playbook and the design principles that constrain every Autotag plan.

## Beyond v1.3 (not yet planned)

Captured here so they don't get lost; not yet promoted to plan files.

- **ListenBrainz scrobbling.** Submit play data when a track crosses the scrobble threshold (`min(duration / 2, 4 minutes)`). Three submission types — `playing_now`, `single`, `import`. Hook slots in next to the existing play-history pipeline; MBIDs already flow through the stack thanks to v1.3.
- **Gapless playback + crossfade.** Seamless transitions with optional crossfade.
- **Layout customization system.** Section-based UI customization; components declare size constraints, users configure per-section.
- **Plugin system.** Full-access API for UI components and backend hooks.
- **AcoustID fingerprinting** via fpcalc — slots in behind the v1.3 `Identifier` interface seam.

## Out of scope

Things deliberately **not** going in. See `NOTES.md` for the reasoning behind each.

- Separate databases per library, auto-dedup across libraries, user access control per library.
- Parallel library scanning (SQLite single-writer).
- Cross-platform media controls beyond MPRIS on Linux.
- Database health checking / reconnection.
- ORM or query builder, connection pooling for SQLite.
- Parenthesized boolean logic in smart playlists; "is favorited" as a filter; queue that re-evaluates rules during playback.
- Playing audio from MusicBrainz remotely (it's a metadata catalog, not a streaming service).
- Integrating any service beyond MusicBrainz / ListenBrainz / Cover Art Archive without explicit user approval.
- Fuzzy auto-accept threshold sliders (strict all-match is the trustworthy default).
- Manual MB search UI (Paste-URL covers the escape-hatch case).
- Cover art replacement during auto-accept (highest-regret op — never automatic in v1).
