-- name: GetAuthor :one
SELECT * FROM recordings
WHERE id = ? LIMIT 1;
