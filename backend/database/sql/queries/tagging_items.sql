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
  album_artist = CASE
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

-- name: GetTaggingItem :one
SELECT * FROM tagging_items
WHERE group_key = ?
LIMIT 1;

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
  ti.created_at
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
  ti.created_at
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
  COALESCE(r.mbid, '') AS recording_mbid
FROM audio_files af
LEFT JOIN recordings r ON af.recording_id = r.id
LEFT JOIN artist_credit ac ON r.artist_credit_id = ac.id
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
