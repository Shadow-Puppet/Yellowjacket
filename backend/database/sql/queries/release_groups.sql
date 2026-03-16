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

-- name: GetReleaseGroupByNameAndArtist :one
SELECT * FROM release_groups
WHERE name = ? AND album_artist_credit_id = ? LIMIT 1;

-- name: UpsertReleaseGroup :one
INSERT INTO release_groups (name, album_artist_credit_id, year)
VALUES (?, ?, ?)
ON CONFLICT(name, album_artist_credit_id) DO UPDATE SET
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

-- name: DeleteAllReleaseGroups :exec
DELETE FROM release_groups;

-- name: GetAllReleaseGroups :many
SELECT * FROM release_groups
ORDER BY name;

-- name: GetAllAlbumsWithDetails :many
SELECT
    rg.id,
    rg.name,
    rg.year,
    COALESCE(ac.text, fallback_ac.text, '') as artist_name,
    COALESCE(ca.file_path, '') as cover_art_path
FROM release_groups rg
LEFT JOIN artist_credit ac ON rg.album_artist_credit_id = ac.id
LEFT JOIN cover_art ca ON rg.cover_art_id = ca.id
LEFT JOIN (
    SELECT rgr.release_group_id, ac2.text
    FROM release_group_recordings rgr
    JOIN recordings rec ON rec.id = rgr.recording_id
    JOIN artist_credit ac2 ON ac2.id = rec.artist_credit_id
    GROUP BY rgr.release_group_id
) fallback_ac ON fallback_ac.release_group_id = rg.id
ORDER BY rg.name;

-- name: GetAllAlbumsWithDetailsByLibrary :many
SELECT
    rg.id,
    rg.name,
    rg.year,
    COALESCE(ac.text, fallback_ac.text, '') as artist_name,
    COALESCE(ca.file_path, '') as cover_art_path
FROM release_groups rg
LEFT JOIN artist_credit ac ON rg.album_artist_credit_id = ac.id
LEFT JOIN cover_art ca ON rg.cover_art_id = ca.id
LEFT JOIN (
    SELECT rgr.release_group_id, ac2.text
    FROM release_group_recordings rgr
    JOIN recordings rec ON rec.id = rgr.recording_id
    JOIN artist_credit ac2 ON ac2.id = rec.artist_credit_id
    GROUP BY rgr.release_group_id
) fallback_ac ON fallback_ac.release_group_id = rg.id
WHERE rg.id IN (
    SELECT DISTINCT rgr2.release_group_id
    FROM release_group_recordings rgr2
    JOIN recordings r2 ON r2.id = rgr2.recording_id
    JOIN audio_files af2 ON af2.recording_id = r2.id
    WHERE af2.library_id = ?
)
ORDER BY rg.name;

-- name: GetAlbumsByArtist :many
SELECT
    rg.id,
    rg.name,
    rg.year,
    COALESCE(ac.text, fallback_ac.text, '') as artist_name,
    COALESCE(ca.file_path, '') as cover_art_path
FROM release_groups rg
JOIN artist_credit ac ON rg.album_artist_credit_id = ac.id
JOIN artist_credit_artist aca ON aca.credit_id = ac.id
LEFT JOIN cover_art ca ON rg.cover_art_id = ca.id
LEFT JOIN (
    SELECT rgr.release_group_id, ac2.text
    FROM release_group_recordings rgr
    JOIN recordings rec ON rec.id = rgr.recording_id
    JOIN artist_credit ac2 ON ac2.id = rec.artist_credit_id
    GROUP BY rgr.release_group_id
) fallback_ac ON fallback_ac.release_group_id = rg.id
WHERE aca.artist_id = ?
ORDER BY rg.name;

-- name: GetAlbumsByArtistByLibrary :many
SELECT
    rg.id,
    rg.name,
    rg.year,
    COALESCE(ac.text, fallback_ac.text, '') as artist_name,
    COALESCE(ca.file_path, '') as cover_art_path
FROM release_groups rg
JOIN artist_credit ac ON rg.album_artist_credit_id = ac.id
JOIN artist_credit_artist aca ON aca.credit_id = ac.id
LEFT JOIN cover_art ca ON rg.cover_art_id = ca.id
LEFT JOIN (
    SELECT rgr.release_group_id, ac2.text
    FROM release_group_recordings rgr
    JOIN recordings rec ON rec.id = rgr.recording_id
    JOIN artist_credit ac2 ON ac2.id = rec.artist_credit_id
    GROUP BY rgr.release_group_id
) fallback_ac ON fallback_ac.release_group_id = rg.id
WHERE aca.artist_id = ?
  AND rg.id IN (
    SELECT DISTINCT rgr2.release_group_id
    FROM release_group_recordings rgr2
    JOIN recordings r2 ON r2.id = rgr2.recording_id
    JOIN audio_files af2 ON af2.recording_id = r2.id
    WHERE af2.library_id = ?
)
ORDER BY rg.name;
