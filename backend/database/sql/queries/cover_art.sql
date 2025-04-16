-- name: CreateCoverArt :one
INSERT INTO cover_art (is_embedded, file_path, file_type_id) VALUES (?, ?, ?)
RETURNING *;

-- name: GetCoverArt :one
SELECT * FROM cover_art 
WHERE id = ? LIMIT 1;

-- name: UpdateCoverArt :exec
UPDATE cover_art 
SET is_embedded = ?, file_path = ?, file_type_id = ?
WHERE id =?;

-- name: DeleteCoverArt :exec
DELETE FROM cover_art 
WHERE id =?;

