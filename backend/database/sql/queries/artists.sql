-- name: CreateArtist :one
INSERT INTO artists (name) VALUES (?)
RETURNING *;

-- name: GetArtist :one
SELECT * FROM artists 
WHERE id = ? LIMIT 1;

-- name: UpdateArtist :exec
UPDATE artists 
SET name = ?
WHERE id =?;

-- name: DeleteArtist :exec
DELETE FROM artists 
WHERE id =?;

