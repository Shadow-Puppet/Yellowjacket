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

-- name: DeleteAllArtists :exec
DELETE FROM artists;

-- name: GetAllArtists :many
SELECT * FROM artists
ORDER BY name;

-- name: GetAlbumArtists :many
SELECT DISTINCT a.id, a.name
FROM artists a
JOIN artist_credit_artist aca ON aca.artist_id = a.id
JOIN artist_credit ac ON ac.id = aca.credit_id
JOIN release_groups rg ON rg.album_artist_credit_id = ac.id
ORDER BY a.name;
