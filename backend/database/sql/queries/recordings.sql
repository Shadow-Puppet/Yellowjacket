-- name: CreateRecording :one
INSERT INTO recordings (name, artist_credit_id) VALUES (?, ?)
RETURNING *;

-- name: CreateRecordingFull :one
INSERT INTO recordings (
  name, artist_credit_id, track_number, disc_number,
  year, genre, composer, lyrics, comment
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetRecording :one
SELECT * FROM recordings 
WHERE id = ? LIMIT 1;

-- name: UpdateRecording :exec
UPDATE recordings 
SET name = ?, artist_credit_id = ?
WHERE id = ?;

-- name: UpdateRecordingFull :exec
UPDATE recordings
SET name = ?, artist_credit_id = ?, track_number = ?, disc_number = ?,
    year = ?, genre = ?, composer = ?, lyrics = ?, comment = ?
WHERE id = ?;

-- name: DeleteRecording :exec
DELETE FROM recordings 
WHERE id = ?;

-- name: DeleteAllRecordings :exec
DELETE FROM recordings;

-- name: GetAllRecordings :many
SELECT * FROM recordings
ORDER BY name;

-- name: CountRecordingsByArtistCredit :one
SELECT COUNT(*) FROM recordings WHERE artist_credit_id = ?;

-- name: GetOrphanedRecordingIDs :many
-- Recordings no longer backed by any audio_files row - left behind
-- when a scan's orphan cleanup deletes the file that used to own them,
-- since deleting audio_files doesn't cascade to recordings.
SELECT r.id FROM recordings r
LEFT JOIN audio_files af ON af.recording_id = r.id
WHERE af.id IS NULL;
