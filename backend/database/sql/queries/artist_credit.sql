-- name: CreateArtistCredit :one
INSERT INTO artist_credit (text) VALUES (?)
RETURNING *;

-- name: GetArtistCredit :one
SELECT * FROM artist_credit 
WHERE id = ? LIMIT 1;

-- name: UpdateArtistCredit :exec
UPDATE artist_credit 
SET text = ?
WHERE id =?;

-- name: DeleteArtistCredit :exec
DELETE FROM artist_credit 
WHERE id =?;

