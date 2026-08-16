-- name: GetQueueState :one
SELECT current_position, shuffle_mode, repeat_mode, shuffle_order, source_type, source_id, source_label
FROM queue WHERE id = 1;

-- name: UpdateQueueState :exec
UPDATE queue
SET current_position = ?, shuffle_mode = ?, repeat_mode = ?, shuffle_order = ?, source_type = ?, source_id = ?, source_label = ?
WHERE id = 1;

-- name: UpdateQueuePosition :exec
UPDATE queue
SET current_position = ?
WHERE id = 1;

-- name: GetQueueTracks :many
-- The queue's rows, joined to the one track projection.
SELECT qt.id, qt.audio_file_id, qt.position, tm.file_path,
    tm.title, tm.artist_name AS artist, tm.album, tm.cover_art_path,
    tm.artist_mbid, tm.release_group_mbid, tm.recording_mbid
FROM queue_tracks qt
JOIN track_metadata tm ON tm.id = qt.audio_file_id
ORDER BY qt.position;

-- name: GetQueueTrackCount :one
SELECT count(*) FROM queue_tracks;

-- name: InsertQueueTrack :one
INSERT INTO queue_tracks (audio_file_id, position) VALUES (?, ?)
RETURNING *;

-- name: ClearQueueTracks :exec
DELETE FROM queue_tracks;

-- name: RemoveQueueTrack :exec
DELETE FROM queue_tracks WHERE id = ?;

-- name: RemoveQueueTrackByPosition :exec
DELETE FROM queue_tracks WHERE position = ?;

-- name: ShiftQueuePositionsDown :exec
UPDATE queue_tracks
SET position = position - 1
WHERE position > ?;

-- name: ShiftQueuePositionsUp :exec
UPDATE queue_tracks
SET position = position + 1
WHERE position >= ?;
