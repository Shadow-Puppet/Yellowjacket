# 005 — Smart Playlists

> Rule-based dynamic playlists: saved queries that evaluate against the library on demand, with a typeable combobox rule editor, live preview, and queue-snapshot-on-play semantics. AND-only logic with multi-value "is any of" — no nested boolean groups.

**Shipped:** 2026-03-22 · **Slices:** S01-S04 (M002 in GSD)

## What landed

- **S01 — Schema & rule engine.** Migration 9 adds `is_smart` + `smart_rules` (JSON) columns to the existing `playlists` table — extends, doesn't duplicate. `backend/smartplaylist/` builds parameterized WHERE clauses via a hardcoded `fieldMap` whitelist (16 columns from `track_metadata`). 7 text operators (`is`, `is_not`, `contains`, `does_not_contain`, `starts_with`, `ends_with`, `is_any_of`) and 5 numeric (`is`, `is_not`, `greater_than`, `less_than`, `between`). 49 unit tests cover all operators, the genre dual-path, and SQL-injection rejection. Optional result limit + sort (whitelisted fields, `ORDER BY RANDOM()` allowed).
- **S02 — CRUD & sidebar.** sqlc regenerated for the new columns; `IsSmart bool` propagated through `playlist.Summary` to TypeScript models. Smart playlists render with a filter icon and "Smart" badge. New `smart-playlist-details` Lit element follows the `genre-details` pattern; refresh/play/shuffle wired. Drag-drop onto smart playlists blocked.
- **S03 — Rule editor UI.** Reusable `yj-combobox` LitElement (typeable input, ARIA, keyboard nav). `smart-playlist-editor` builds rules row-by-row with field combobox, operator switch (text/numeric auto), value input. Live preview at 300 ms debounce calling `PreviewSmartPlaylist`.
- **S04 — Integration.** Create → navigate → `auto-edit` flow — detail view awaits initial data load (avoids `MaxOpenConns(1)` deadlock) before opening the editor.

## Key decisions retained

- **One playlists table** — `is_smart` flag, not a parallel table. One code path for listing.
- **Saved queries, not saved track lists** — no `playlist_tracks` rows for smart playlists; results evaluated on demand.
- **Snapshot to queue on play.** Queue must be stable during playback, not dynamically re-evaluating.
- **Genre dual-path.** Exact ops (`is`, `is_not`, `is_any_of`) use a subquery against `recording_genres JOIN genres` because the `GROUP_CONCAT(g.name, '||')` column in `track_metadata` can't match individual names. LIKE-based ops use the concatenated column.
- **Wails TS bindings for the 5 smart-playlist methods are manually maintained** (`Service.d.ts` / `Service.js`). No build-time check catches drift if Go signatures change.

## Fragility notes

- **Combobox blur-vs-click race.** The `mousedown` + `preventDefault()` + `requestAnimationFrame` fallback in `combobox.ts` is battle-tested but brittle if anyone restructures option rendering into a separate shadow DOM. Re-verify click-to-select in any combobox refactor.
- **`MaxOpenConns(1)` deadlock.** Holding `*sql.Rows` open while calling functions that query the same `*database.DB` will deadlock. Close rows explicitly before downstream calls; never rely on `defer` here.
