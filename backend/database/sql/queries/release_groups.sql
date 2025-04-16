-- name: CreateReleaseGroup :one
INSERT INTO release_groups (name) VALUES (?)
RETURNING *;

-- name: GetReleaseGroup :one
SELECT * FROM release_groups 
WHERE id = ? LIMIT 1;

-- name: UpdateReleaseGroup :exec
UPDATE release_groups 
SET name = ?
WHERE id =?;

-- name: DeleteReleaseGroup :exec
DELETE FROM release_groups 
WHERE id =?;

