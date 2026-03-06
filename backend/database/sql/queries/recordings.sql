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
