-- name: CreateArtistCreditArtist :one
INSERT INTO artist_credit_artist (artist_id, credit_id) VALUES (?, ?)
RETURNING *;

-- name: GetArtistCreditArtist :one
SELECT * FROM artist_credit_artist 
WHERE id = ? LIMIT 1;

-- name: UpdateArtistCreditArtist :exec
UPDATE artist_credit_artist 
SET artist_id = ?, credit_id = ?
WHERE id =?;

-- name: DeleteArtistCreditArtist :exec
DELETE FROM artist_credit_artist 
WHERE id =?;

