-- Queries over artists.
--
-- An artist row is reachable two ways: as a file's primary artist
-- (audio_files.artist_id) and as an album's artist (albums.artist_id).
-- Both used to route through artist_credit + artist_credit_artist,
-- which is how "which album artists are in library 2" came to be a
-- five-join subquery inside a three-join query.

-- name: UpsertArtist :one
INSERT INTO artists (name, mbid) VALUES (?, ?)
ON CONFLICT(name) DO UPDATE SET
    mbid = COALESCE(excluded.mbid, artists.mbid)
RETURNING *;

-- name: GetArtist :one
SELECT * FROM artists WHERE id = ? LIMIT 1;

-- name: GetArtistByName :one
SELECT * FROM artists WHERE name = ? LIMIT 1;

-- name: SetArtistMBID :exec
UPDATE artists SET mbid = ? WHERE id = ?;

-- name: DeleteArtist :exec
DELETE FROM artists WHERE id = ?;

-- name: DeleteAllArtists :exec
DELETE FROM artists;

-- name: GetUnreferencedArtistIDs :many
-- Artists no file and no album points at any more.
SELECT id FROM artists a
WHERE NOT EXISTS (SELECT 1 FROM audio_files af WHERE af.artist_id = a.id)
  AND NOT EXISTS (SELECT 1 FROM albums al WHERE al.artist_id = a.id);

-- name: GetAllArtists :many
SELECT * FROM artists ORDER BY name;

-- name: GetAlbumArtists :many
SELECT DISTINCT a.id, a.name, a.mbid
FROM artists a
JOIN albums al ON al.artist_id = a.id
WHERE EXISTS (
    SELECT 1 FROM audio_files af
    WHERE af.album_id = al.id
      AND af.library_id = COALESCE(NULLIF(CAST(sqlc.arg(library_id) AS INTEGER), 0), af.library_id)
)
ORDER BY a.name;

-- name: GetArtistByFilePath :one
SELECT COALESCE(a.name, '') AS artist_name, COALESCE(a.mbid, '') AS artist_mbid
FROM audio_files af
LEFT JOIN artists a ON a.id = af.artist_id
WHERE af.file_path = ?
LIMIT 1;
