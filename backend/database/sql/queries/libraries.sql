-- name: CreateLibrary :one
INSERT INTO libraries (name, path) VALUES (?, ?)
RETURNING *;

-- name: GetLibrary :one
SELECT * FROM libraries WHERE id = ? LIMIT 1;

-- name: GetLibraryByPath :one
SELECT * FROM libraries WHERE path = ? LIMIT 1;

-- name: GetAllLibraries :many
SELECT * FROM libraries ORDER BY name;

-- name: UpdateLibraryName :exec
UPDATE libraries SET name = ? WHERE id = ?;

-- name: DeleteLibrary :exec
DELETE FROM libraries WHERE id = ?;

-- name: CountLibraries :one
SELECT COUNT(*) AS count FROM libraries;

-- name: AckLibraryAutotagWarning :exec
UPDATE libraries SET autotag_warning_acked = 1 WHERE id = ?;
