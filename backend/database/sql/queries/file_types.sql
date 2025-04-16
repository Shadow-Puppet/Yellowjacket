-- name: CreateFileType :one
INSERT INTO file_types (extension) VALUES (?)
RETURNING *;

-- name: GetFileType :one
SELECT * FROM file_types 
WHERE id = ? LIMIT 1;

-- name: UpdateFileType :exec
UPDATE file_types 
SET extension = ?
WHERE id =?;

-- name: DeleteFileType :exec
DELETE FROM file_types 
WHERE id =?;

