-- name: ExcludePath :exec
INSERT INTO excluded_paths (library_id, file_path)
VALUES (?, ?)
ON CONFLICT(library_id, file_path) DO NOTHING;

-- name: GetExcludedPathsByLibrary :many
SELECT file_path FROM excluded_paths
WHERE library_id = ?;

-- name: CountExcludedPathsByLibrary :one
SELECT COUNT(*) FROM excluded_paths
WHERE library_id = ?;

-- name: ClearExcludedPaths :exec
DELETE FROM excluded_paths;
