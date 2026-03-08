# Features Research: Multi-Library Support

**Researched:** 2026-03-08
**Confidence:** HIGH

## How Mature Players Handle Multiple Libraries

| Player | Model | Libraries Separate? | Cross-Library Playlists? |
|--------|-------|--------------------|-----------------------|
| foobar2000 | Multiple folders, one merged library | No — all folders merge | N/A (one library) |
| MusicBee | Multiple folders per library, separate library databases | Yes (separate DBs) | No |
| Plex | Separate typed libraries, multiple folders each | Yes (fully isolated) | No |
| Jellyfin | Virtual collections with multiple paths | Yes | No |
| Navidrome | Named libraries with user access control | Yes (with multi-select merge) | Yes |
| Roon | Watched folders, one unified library | No — all merge | N/A (one library) |

### Desktop Player Pattern (foobar2000, Roon)

All folders contribute to one unified library. No folder-level filtering in default UI. User never thinks about "which folder."

### Server Pattern (Plex, Jellyfin, Navidrome)

Separate libraries with access control. More suited to multi-user server apps.

### YellowJacket Fit

Desktop player = **merged by default**, with optional filter. Follows foobar2000/Roon pattern but adds Navidrome-style library selector for power users.

## Feature Classification

### Table Stakes (Must Have)

| Feature | Complexity | Dependencies |
|---------|-----------|-------------|
| Add multiple watched folders | Low | Config to DB migration |
| Unified merged view (default) | Medium | All browse views, search aggregate |
| Per-folder independent scan | Medium | Scan pipeline scoping |
| Remove folder without data loss | Low | Phantom tracks for playlists |
| Folder status indicators | Low | Scan event system |
| Graceful offline handling | Medium | Guard orphan cleanup |
| Existing playlists unaffected | Low | File-path references already work |

### Differentiators (Nice to Have)

| Feature | Complexity | Dependencies |
|---------|-----------|-------------|
| Filter/narrow by source folder | Medium | UI filter chip, query-level filtering |
| Named libraries | Low | DB stores name + path |
| Per-folder scan concurrency | Low | Extend existing ScanConcurrency per library |
| Folder health dashboard | Medium | Aggregate scan metrics |

### Anti-Features (Do NOT Build)

| Anti-Feature | Why Avoid |
|--------------|-----------|
| Separate databases per library | Breaks unified browse, doubles query logic |
| User/access control per library | Desktop app is single-user |
| Auto-merge/deduplicate across folders | Complex, error-prone, unexpected |
| Library-specific settings/themes | Over-engineering |

## Library Removal Patterns

All players that support library removal:
1. Show confirmation dialog
2. Remove tracks from DB (or mark as missing)
3. Handle playlist references (delete, mark phantom, or leave as-is)
4. Don't delete files from disk

**YellowJacket approach:** Phantom tracks (preserving metadata) for playlists. Queue tracks cascade-deleted (ephemeral). Orphan cleanup for shared entities via reference counting.

## Offline Handling

Universal pattern: Don't delete tracks when source goes offline. Mark as unavailable. Auto-recover on next scan when source returns. Never auto-delete on temporary unavailability.
