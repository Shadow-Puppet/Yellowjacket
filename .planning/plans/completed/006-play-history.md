# 006 — Play History & Play Count

> Tracks every natural track completion. Powers smart-playlist rules like "most played", "never played", "not played in 30 days", and exposes play count as an optional track-list column.

**Shipped:** 2026-03-22 · **Slices:** S01-S03 (M003 in GSD)

## What landed

- **S01 — Schema, migration, recording.** Migration 10 adds `play_history` table and denormalized `play_count` + `last_played` columns on `audio_files`; `track_metadata` VIEW recreated to include them. `recordPlay()` pipeline: insert history row → increment count → update timestamp → emit event. `OnPlaybackFinished` hook unlocks the player mutex *before* the DB write (avoids SQLite deadlock).
- **S02 — Smart playlist integration.** `play_count` (numeric) and `days_since_played` (computed) added to the rule-engine field whitelist. Explicit NULL handling for never-played tracks in `days_since_played` conditions.
- **S03 — UI.** Optional play-count column in track lists; `PlayCount` / `LastPlayed` added to TypeScript models; editor dropdown entries for the new fields.

## Key decisions retained

- **Natural finish only** — no percentage threshold, no skip counting. (If you scrub past the end, that doesn't count.)
- **Denormalized `play_count`/`last_played` on `audio_files`** — the alternative was per-query aggregation against `play_history`; the cost wasn't worth it.
- **`days_since_played` is a computed field**, not raw timestamp comparison in the rule. Lets the rule engine speak the user's vocabulary.
- **"Recently played" is a smart playlist**, not a dedicated view. Reuses the existing rule engine instead of adding a parallel query path.
- **Mutex unlock before `recordPlay()`** is mandatory — locking around the DB write under `MaxOpenConns(1)` deadlocks.
