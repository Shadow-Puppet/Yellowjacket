-- name: UpsertGenre :one
INSERT INTO genres (name) VALUES (?)
ON CONFLICT(name) DO UPDATE SET name = name
RETURNING *;

-- name: CreateRecordingGenre :exec
INSERT OR IGNORE INTO recording_genres (recording_id, genre_id)
VALUES (?, ?);

-- name: DeleteRecordingGenres :exec
DELETE FROM recording_genres
WHERE recording_id = ?;

-- name: GetGenresByRecordingID :many
SELECT g.*
FROM genres g
JOIN recording_genres rg ON g.id = rg.genre_id
WHERE rg.recording_id = ?;

-- name: DeleteAllRecordingGenres :exec
DELETE FROM recording_genres;

-- name: DeleteAllGenres :exec
DELETE FROM genres;
