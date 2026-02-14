-- name: CreateCoverArt :one
INSERT INTO cover_art (is_embedded, file_path, mime_type) VALUES (?, ?, ?)
RETURNING *;

-- name: GetCoverArt :one
SELECT * FROM cover_art 
WHERE id = ? LIMIT 1;

-- name: GetCoverArtByPath :one
SELECT * FROM cover_art
WHERE file_path = ? LIMIT 1;

-- name: UpsertCoverArt :one
INSERT INTO cover_art (is_embedded, file_path, mime_type)
VALUES (?, ?, ?)
ON CONFLICT(file_path) DO UPDATE SET
  is_embedded = excluded.is_embedded,
  mime_type = excluded.mime_type
RETURNING *;

-- name: UpdateCoverArt :exec
UPDATE cover_art 
SET is_embedded = ?, file_path = ?, mime_type = ?
WHERE id = ?;

-- name: DeleteCoverArt :exec
DELETE FROM cover_art 
WHERE id = ?;
