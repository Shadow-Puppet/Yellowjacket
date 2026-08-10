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

-- name: DeleteAllArtistCredits :exec
DELETE FROM artist_credit;

-- name: CountArtistCreditReferences :one
SELECT
  (SELECT COUNT(*) FROM recordings WHERE artist_credit_id = ?1) +
  (SELECT COUNT(*) FROM release_groups WHERE album_artist_credit_id = ?1)
AS total;

-- name: GetOrphanedArtistCreditIDs :many
-- Artist credits no longer used by any recording or release group - run
-- after orphaned recordings/release groups are deleted, so a credit
-- that only existed for now-removed tracks is cleaned up too.
SELECT ac.id FROM artist_credit ac
WHERE NOT EXISTS (SELECT 1 FROM recordings r WHERE r.artist_credit_id = ac.id)
  AND NOT EXISTS (SELECT 1 FROM release_groups rg WHERE rg.album_artist_credit_id = ac.id);
