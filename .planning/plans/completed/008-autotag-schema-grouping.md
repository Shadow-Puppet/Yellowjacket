# 008 — Autotag: Schema & Grouping Foundation

> First v1.3 phase. Lays down the schema + bookkeeping for the MusicBrainz autotagger — no scoring, no UI, no MB calls yet. Subsequent autotag phases (009-012) build on this.

**Shipped:** 2026-04-20 · **Sub-plans:** 008.1-008.4

## What landed

- **008.1 — Migration 31: `audio_files.tag_status`.** Text column with inline `CHECK(tag_status IN (...))` constraint (SQLite `ALTER TABLE ADD COLUMN` supports column constraints, so both fresh and upgraded DBs enforce it). Partial index `idx_audio_files_tag_status_untagged ON audio_files(library_id) WHERE tag_status = 'untagged'` powers the pending badge. Backfill sets `user_confirmed` where the joined recording already has an MBID; everything else defaults to `untagged`.
- **008.2 — Migration 32: `tagging_items` + `group_key`.** New `tagging_items` table (PK `group_key`, `status CHECK IN ('pending','matched','confirmed','skipped')`, two indexes including the partial `WHERE status = 'pending'` for the badge). `audio_files.group_key TEXT NOT NULL DEFAULT ''` with partial index `WHERE group_key != ''`. Column-referencing indexes live in the migration, not the schema file, because `CREATE TABLE IF NOT EXISTS` is a no-op on existing tables and the partial-index predicate would reference a column that hasn't been added yet.
- **008.3 — `backend/autotag.GroupKey` + scan-path integration.** Lower-case hex SHA-1 over `libraryID || 0 || parentDirLower || 0 || albumTrimmed || 0 || discNumber` — intentionally shallow, deeper normalization is scoring territory (009). `saveAudioFile` switched to `CreateAudioFileWithGroupKey` + `UpsertTaggingItemOnTrackAdd`. `updateAudioFileMetadata` rebinds on key change: decrement old group → delete if empty → upsert new group → write new key onto the audio_files row. Migration 32 streams existing rows in batches of 500 and aggregates `tagging_items` at the end, defaulting status to `confirmed` when every track in the group is `user_confirmed`.
- **008.4 — Pending-queue sqlc queries.** `CountPendingTaggingItems`, three `ListPendingTaggingItems...` variants (alphabetical, by-score nulls-last, by-recent) — sqlc has no dynamic ORDER BY so each sort has its own query. `CAST(@param AS INTEGER|TEXT)` hints give typed params (otherwise sqlc emits `interface{}`). `GetTaggingItem` and `ListAudioFilesInTaggingGroup` round out the queue API. `EXPLAIN QUERY PLAN` test asserts the badge query uses `idx_tagging_items_status_pending` so a future schema change that breaks the partial index fails loudly.

## Key decisions retained

- **Hash algorithm in Go, not SQL.** `autotag.GroupKey` is the single source of truth for the key format so we can evolve it without coupling to SQLite functions.
- **SHA-1 over alternatives.** Matches the codebase's existing non-crypto deterministic-key convention. Collision risk at album-group cardinality (millions) is irrelevant.
- **Null-byte separators between hash inputs.** Prevents `a|b` vs `ab|` ambiguity.
- **Per-track integration inside `commitBatch`'s transaction**, not a post-scan callback — keeps `tagging_items` coherent after partial scans.
- **`best_match_release_mbid`, `score`, `last_checked_at` shapes fixed now** even though they stay NULL until 009-010. Avoids schema churn later.
