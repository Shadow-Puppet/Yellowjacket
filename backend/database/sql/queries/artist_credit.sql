-- name: CreateArtistCredit :one
INSERT INTO artist_credit (text) VALUES (?)
RETURNING *;

-- name: GetArtistCredit :one
SELECT * FROM artist_credit 
WHERE id = ? LIMIT 1;

-- name: GetArtistCreditByText :one
SELECT * FROM artist_credit
WHERE text = ? LIMIT 1;

-- name: UpsertArtistCredit :one
INSERT INTO artist_credit (text) VALUES (?)
ON CONFLICT(text) DO UPDATE SET text = excluded.text
RETURNING *;

-- name: UpdateArtistCredit :exec
UPDATE artist_credit 
SET text = ?
WHERE id = ?;

-- name: DeleteArtistCredit :exec
DELETE FROM artist_credit 
WHERE id = ?;
