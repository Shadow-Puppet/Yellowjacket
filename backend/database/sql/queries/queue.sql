-- name: GetQueueState :one
SELECT source_playlist_id, current_position, shuffle_mode, repeat_mode, shuffle_order
FROM queue WHERE id = 1;

-- name: UpdateQueueState :exec
UPDATE queue
SET source_playlist_id = ?, current_position = ?, shuffle_mode = ?, repeat_mode = ?, shuffle_order = ?
WHERE id = 1;

-- name: UpdateQueuePosition :exec
UPDATE queue
SET current_position = ?
WHERE id = 1;

-- name: GetQueueTracks :many
SELECT qt.id, qt.audio_file_id, qt.position, af.file_path,
    COALESCE(r.name, '') AS title,
    COALESCE(ac.text, '') AS artist
FROM queue_tracks qt
JOIN audio_files af ON qt.audio_file_id = af.id
LEFT JOIN recordings r ON af.recording_id = r.id
LEFT JOIN artist_credit ac ON r.artist_credit_id = ac.id
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
