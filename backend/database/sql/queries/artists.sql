-- name: CreateArtist :one
INSERT INTO artists (name) VALUES (?)
RETURNING *;

-- name: GetArtist :one
SELECT * FROM artists 
WHERE id = ? LIMIT 1;

-- name: GetArtistByName :one
SELECT * FROM artists
WHERE name = ? LIMIT 1;

-- name: UpsertArtist :one
INSERT INTO artists (name) VALUES (?)
ON CONFLICT(name) DO UPDATE SET name = excluded.name
RETURNING *;

-- name: UpdateArtist :exec
UPDATE artists 
SET name = ?
WHERE id = ?;

-- name: DeleteArtist :exec
DELETE FROM artists 
WHERE id = ?;

-- name: GetAllArtists :many
SELECT * FROM artists
ORDER BY name;
