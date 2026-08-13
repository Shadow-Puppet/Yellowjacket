-- name: UpsertTaggingItemOnTrackAdd :exec
INSERT INTO tagging_items (
  group_key, library_id, track_count,
  album_name, album_artist, disc_number, status
)
VALUES (?, ?, 1, ?, ?, ?, 'pending')
ON CONFLICT(group_key) DO UPDATE SET
  track_count = tagging_items.track_count + 1,
  album_name = CASE
    WHEN tagging_items.album_name = '' THEN excluded.album_name
    ELSE tagging_items.album_name
  END,
  -- Tracks real consensus, not first-write-wins: stays set only
  -- while every track that has contributed a non-empty value agrees.
  -- A later track with a *different* non-empty value clears it back
  -- to '' and latches album_artist_conflict, since a single
  -- disagreeing tag means the folder no longer has one authoritative
  -- album-artist -- IsMixedBag (backend/autotag) treats a non-empty
  -- value here as trusted, so leaving a stale first-seen value in
  -- place would let one track's tag silently blind mixed-bag
  -- detection for the whole folder. The latch (rather than just
  -- clearing the text column) stops a later track from coincidentally
  -- repeating an already-disputed value and resurrecting trust in it.
  album_artist_conflict = CASE
    WHEN tagging_items.album_artist_conflict = 1 THEN 1
    WHEN tagging_items.album_artist != '' AND excluded.album_artist != ''
      AND tagging_items.album_artist != excluded.album_artist THEN 1
    ELSE 0
  END,
  album_artist = CASE
    WHEN tagging_items.album_artist_conflict = 1 THEN ''
    WHEN tagging_items.album_artist != '' AND excluded.album_artist != ''
      AND tagging_items.album_artist != excluded.album_artist THEN ''
    WHEN tagging_items.album_artist = '' THEN excluded.album_artist
    ELSE tagging_items.album_artist
  END;

-- name: DecrementTaggingItemTrackCount :exec
UPDATE tagging_items
SET track_count = track_count - 1
WHERE group_key = ?;

-- name: DeleteTaggingItemIfEmpty :exec
DELETE FROM tagging_items
WHERE group_key = ? AND track_count <= 0;

-- name: PruneOrphanedTaggingItems :exec
-- Self-healing sweep for rows whose track_count bookkeeping (scan
-- orphan cleanup, maybeRebindTaggingGroup, SplitMixedFolder) never
-- ran or drifted: a cancelled scan, a library move/rename the
-- SoftScanAllLibraries disk-count/mtime heuristic did not catch, or
-- a decrement that landed without its paired delete. Rather than
-- trust track_count, this checks the ground truth directly: any
-- group_key no audio_files row still points at is gone, and its
-- tagging_items row (and cascaded tagging_candidates) should be too.
-- Cheap: one indexed (idx_audio_files_group_key) existence check per
-- row. Called opportunistically wherever the pending list is read,
-- so stale entries cannot linger indefinitely between full rescans.
DELETE FROM tagging_items
WHERE NOT EXISTS (
  SELECT 1 FROM audio_files af WHERE af.group_key = tagging_items.group_key
);

-- name: MarkTaggingItemSynthetic :exec
-- Stamps a group as carved out of parent_group_key by
-- SplitMixedFolder.  Idempotent: safe to call every time a track
-- is migrated into the synthetic group, not just on first creation.
UPDATE tagging_items
SET synthetic = 1,
    parent_group_key = ?
WHERE group_key = ?;

-- name: GetTaggingItem :one
SELECT * FROM tagging_items
WHERE group_key = ?
LIMIT 1;

-- name: ListLikelyMixedBagGroupKeys :many
-- Cheap, whole-library triage pass for autotag.IsMixedBag: one
-- grouped scan over audio_files (indexed on group_key) rather than
-- hydrating every group's full track list in Go.  LOWER/TRIM is an
-- approximation of autotag.Normalize (no unicode fold, no qualifier
-- stripping) so this can flag a false positive Normalize would
-- clear, or miss a true one Normalize would catch.  Treat it as a
-- triage filter for which groups are worth a real autotag.
-- IsMixedBag check, or a badge at minimum, not the final word.
SELECT ti.group_key
FROM tagging_items ti
JOIN audio_files af ON af.group_key = ti.group_key
LEFT JOIN recordings r ON af.recording_id = r.id
LEFT JOIN artist_credit ac ON r.artist_credit_id = ac.id
LEFT JOIN release_group_recordings rgr ON rgr.recording_id = r.id
LEFT JOIN release_groups rg ON rg.id = rgr.release_group_id
WHERE ti.synthetic = 0
  AND ti.track_count >= 4
  AND (
    ti.album_artist = ''
    OR LOWER(TRIM(ti.album_artist)) IN ('various artists', 'various', 'va', 'v.a.', 'v a', 'unknown')
  )
GROUP BY ti.group_key
HAVING COUNT(DISTINCT CASE WHEN ac.text != '' THEN LOWER(TRIM(ac.text)) END) > 1
   AND COUNT(DISTINCT CASE WHEN rg.name != '' THEN LOWER(TRIM(rg.name)) END) > 1;

-- name: CountPendingTaggingItems :one
SELECT COUNT(*) FROM tagging_items
WHERE status = 'pending'
  AND (CAST(@library_id AS INTEGER) = 0 OR library_id = @library_id);

-- name: ListPendingTaggingItemsAlphabetical :many
SELECT
  ti.group_key,
  ti.library_id,
  COALESCE(lb.name, '') AS library_name,
  ti.track_count,
  ti.album_name,
  ti.album_artist,
  ti.disc_number,
  ti.best_match_release_mbid,
  ti.score,
  ti.last_checked_at,
  ti.status,
  ti.created_at
FROM tagging_items ti
LEFT JOIN libraries lb ON lb.id = ti.library_id
WHERE (CAST(@library_id AS INTEGER) = 0 OR ti.library_id = @library_id)
  AND (CAST(@status_filter AS TEXT) = 'all' OR ti.status = @status_filter)
  AND ti.cleared_at IS NULL
ORDER BY LOWER(ti.album_artist), LOWER(ti.album_name), ti.disc_number
LIMIT @row_limit OFFSET @row_offset;

-- name: ListPendingTaggingItemsByScore :many
SELECT
  ti.group_key,
  ti.library_id,
  COALESCE(lb.name, '') AS library_name,
  COALESCE(lb.path, '') AS library_path,
  CAST(COALESCE((SELECT af.file_path FROM audio_files af WHERE af.group_key = ti.group_key AND af.group_key != '' LIMIT 1), '') AS TEXT) AS sample_file_path,
  ti.track_count,
  ti.album_name,
  ti.album_artist,
  ti.disc_number,
  ti.best_match_release_mbid,
  ti.score,
  ti.last_checked_at,
  ti.status,
  ti.created_at,
  ti.synthetic
FROM tagging_items ti
LEFT JOIN libraries lb ON lb.id = ti.library_id
WHERE (CAST(@library_id AS INTEGER) = 0 OR ti.library_id = @library_id)
  AND (CAST(@status_filter AS TEXT) = 'all' OR ti.status = @status_filter)
  AND ti.cleared_at IS NULL
ORDER BY ti.score IS NULL, ti.score DESC, LOWER(ti.album_artist), LOWER(ti.album_name)
LIMIT @row_limit OFFSET @row_offset;

-- name: ClearCompletedTaggingItems :exec
UPDATE tagging_items
SET cleared_at = CURRENT_TIMESTAMP
WHERE status = 'confirmed'
  AND cleared_at IS NULL
  AND (CAST(@library_id AS INTEGER) = 0 OR library_id = @library_id);

-- name: GetPendingFolderDetail :one
SELECT
  ti.group_key,
  ti.library_id,
  COALESCE(lb.name, '') AS library_name,
  COALESCE(lb.path, '') AS library_path,
  CAST(COALESCE((SELECT af.file_path FROM audio_files af WHERE af.group_key = ti.group_key AND af.group_key != '' LIMIT 1), '') AS TEXT) AS sample_file_path,
  ti.track_count,
  ti.album_name,
  ti.album_artist,
  ti.disc_number,
  ti.best_match_release_mbid,
  ti.score,
  ti.last_checked_at,
  ti.status,
  ti.created_at,
  ti.synthetic
FROM tagging_items ti
LEFT JOIN libraries lb ON lb.id = ti.library_id
WHERE ti.group_key = ?
LIMIT 1;

-- name: ListPendingTaggingItemsByRecent :many
SELECT
  ti.group_key,
  ti.library_id,
  COALESCE(lb.name, '') AS library_name,
  ti.track_count,
  ti.album_name,
  ti.album_artist,
  ti.disc_number,
  ti.best_match_release_mbid,
  ti.score,
  ti.last_checked_at,
  ti.status,
  ti.created_at
FROM tagging_items ti
LEFT JOIN libraries lb ON lb.id = ti.library_id
WHERE (CAST(@library_id AS INTEGER) = 0 OR ti.library_id = @library_id)
  AND (CAST(@status_filter AS TEXT) = 'all' OR ti.status = @status_filter)
ORDER BY ti.created_at DESC, ti.group_key
LIMIT @row_limit OFFSET @row_offset;

-- name: ListAudioFilesInTaggingGroup :many
-- album_name/album_artist are the PER-TRACK tags (via each track's
-- own release_group link), not the folder-level tagging_items
-- values.  SplitMixedFolder clusters on these to find sub-albums
-- hiding inside a folder full of unrelated tracks.
SELECT
  af.id,
  af.file_path,
  af.basename,
  af.length_milliseconds,
  af.tag_status,
  COALESCE(r.track_number, 0) AS track_number,
  COALESCE(r.disc_number, 0) AS disc_number,
  COALESCE(r.name, '') AS title,
  COALESCE(ac.text, '') AS artist_name,
  COALESCE(r.mbid, '') AS recording_mbid,
  COALESCE(rg.name, '') AS album_name,
  COALESCE(rgac.text, '') AS album_artist
FROM audio_files af
LEFT JOIN recordings r ON af.recording_id = r.id
LEFT JOIN artist_credit ac ON r.artist_credit_id = ac.id
LEFT JOIN release_group_recordings rgr ON rgr.recording_id = r.id
LEFT JOIN release_groups rg ON rg.id = rgr.release_group_id
LEFT JOIN artist_credit rgac ON rg.album_artist_credit_id = rgac.id
WHERE af.group_key = ?
ORDER BY COALESCE(r.disc_number, 0),
         COALESCE(r.track_number, 0),
         af.file_path;

-- name: ListLocalReleaseGroupCandidates :many
-- Returns one row per (release_group, track) combination for any
-- local release_group that has an MBID.  Callers group these in Go
-- and filter by normalized album-name match.  Joined case-insensitive
-- on name to pre-filter cheaply; Go does the real normalization.
SELECT
  rg.id AS release_group_id,
  rg.mbid AS release_group_mbid,
  rg.name AS album_name,
  COALESCE(rg.year, 0) AS year,
  COALESCE(ac.text, '') AS artist_credit,
  COALESCE(rgr.track_number, 0) AS track_number,
  COALESCE(rgr.disc_number, 0) AS disc_number,
  COALESCE(r.name, '') AS track_title,
  COALESCE(r.mbid, '') AS recording_mbid,
  COALESCE(local_af.length_milliseconds, 0) AS length_milliseconds
FROM release_groups rg
JOIN release_group_recordings rgr ON rgr.release_group_id = rg.id
JOIN recordings r ON r.id = rgr.recording_id
LEFT JOIN artist_credit ac ON rg.album_artist_credit_id = ac.id
LEFT JOIN audio_files local_af ON local_af.recording_id = r.id
WHERE rg.mbid IS NOT NULL
  AND rg.mbid != ''
  AND r.mbid IS NOT NULL
  AND r.mbid != ''
  AND rg.name = ? COLLATE NOCASE
ORDER BY rg.id, rgr.disc_number, rgr.track_number;

-- name: SetTaggingItemBestMatch :exec
UPDATE tagging_items
SET best_match_release_mbid = ?,
    score = ?,
    status = ?,
    last_checked_at = CURRENT_TIMESTAMP
WHERE group_key = ?;

-- name: SetTaggingItemScore :exec
UPDATE tagging_items
SET best_match_release_mbid = ?,
    score = ?,
    last_checked_at = CURRENT_TIMESTAMP
WHERE group_key = ?;

-- name: SetTaggingItemStatus :exec
UPDATE tagging_items
SET status = ?,
    last_checked_at = CURRENT_TIMESTAMP
WHERE group_key = ?;

-- name: SetAudioFileTagStatus :exec
UPDATE audio_files SET tag_status = ? WHERE id = ?;

-- name: SetRecordingMBID :exec
UPDATE recordings SET mbid = ? WHERE id = ?;

-- name: SetReleaseGroupMBID :exec
UPDATE release_groups SET mbid = ? WHERE id = ?;

-- name: GetRecordingReleaseGroupID :one
SELECT COALESCE(rgr.release_group_id, 0) AS release_group_id
FROM release_group_recordings rgr
WHERE rgr.recording_id = ?
LIMIT 1;

-- name: GetNextPendingTaggingItem :one
SELECT
  ti.group_key,
  ti.library_id,
  COALESCE(lb.name, '') AS library_name,
  ti.track_count,
  ti.album_name,
  ti.album_artist,
  ti.disc_number,
  ti.best_match_release_mbid,
  ti.score,
  ti.last_checked_at,
  ti.status,
  ti.created_at
FROM tagging_items ti
LEFT JOIN libraries lb ON lb.id = ti.library_id
WHERE ti.status = 'pending'
  AND (CAST(@library_id AS INTEGER) = 0 OR ti.library_id = @library_id)
  AND ti.group_key > @after_group_key
ORDER BY ti.group_key
LIMIT 1;
