-- name: CreateRecording :one
INSERT INTO recordings (name) VALUES (?)
RETURNING *;

-- name: GetRecording :one
SELECT * FROM recordings 
WHERE id = ? LIMIT 1;

-- name: UpdateRecording :exec
UPDATE recordings 
SET name = ?
WHERE id =?;

-- name: DeleteRecording :exec
DELETE FROM recordings 
WHERE id =?;

