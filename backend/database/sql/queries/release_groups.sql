-- name: CreateReleaseGroup :one
INSERT INTO release_groups (name) VALUES (?)
RETURNING *;

-- name: CreateReleaseGroupFull :one
INSERT INTO release_groups (
  name, cover_art_id, album_artist_credit_id, year, total_tracks, total_discs
) VALUES (?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetReleaseGroup :one
SELECT * FROM release_groups 
WHERE id = ? LIMIT 1;

-- name: GetReleaseGroupByName :one
SELECT * FROM release_groups
WHERE name = ? LIMIT 1;

-- name: UpsertReleaseGroup :one
INSERT INTO release_groups (name, album_artist_credit_id, year)
VALUES (?, ?, ?)
ON CONFLICT(name) DO UPDATE SET
  album_artist_credit_id = COALESCE(excluded.album_artist_credit_id, release_groups.album_artist_credit_id),
  year = COALESCE(excluded.year, release_groups.year)
RETURNING *;

-- name: UpdateReleaseGroup :exec
UPDATE release_groups 
SET name = ?
WHERE id = ?;

-- name: UpdateReleaseGroupCoverArt :exec
UPDATE release_groups
SET cover_art_id = ?
WHERE id = ?;

-- name: DeleteReleaseGroup :exec
DELETE FROM release_groups 
WHERE id = ?;

-- name: GetAllReleaseGroups :many
SELECT * FROM release_groups
ORDER BY name;

-- name: GetAllAlbumsWithDetails :many
SELECT 
    rg.id,
    rg.name,
    rg.year,
    COALESCE(ac.text, '') as artist_name,
    COALESCE(ca.file_path, '') as cover_art_path
FROM release_groups rg
LEFT JOIN artist_credit ac ON rg.album_artist_credit_id = ac.id
LEFT JOIN cover_art ca ON rg.cover_art_id = ca.id
ORDER BY rg.name;
