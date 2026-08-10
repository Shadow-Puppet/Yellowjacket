-- name: CreateArtist :one
INSERT INTO artists (name) VALUES (?)
RETURNING *;

-- name: GetArtist :one
SELECT * FROM artists 
WHERE id = ? LIMIT 1;

-- name: GetArtistByName :one
SELECT * FROM artists
WHERE name = ? LIMIT 1;

-- name: UpsertArtist :one
INSERT INTO artists (name) VALUES (?)
ON CONFLICT(name) DO UPDATE SET name = excluded.name
RETURNING *;

-- name: UpdateArtist :exec
UPDATE artists 
SET name = ?
WHERE id = ?;

-- name: DeleteArtist :exec
DELETE FROM artists 
WHERE id = ?;

-- name: DeleteAllArtists :exec
DELETE FROM artists;

-- name: GetAllArtists :many
SELECT * FROM artists
ORDER BY name;

-- name: GetAlbumArtists :many
SELECT DISTINCT a.id, a.name, a.mbid
FROM artists a
JOIN artist_credit_artist aca ON aca.artist_id = a.id
JOIN artist_credit ac ON ac.id = aca.credit_id
JOIN release_groups rg ON rg.album_artist_credit_id = ac.id
ORDER BY a.name;

-- name: GetOrphanedArtistIDs :many
-- Artists no longer credited on any recording or release group - left
-- behind when a scan's orphan cleanup removes the audio_files that used
-- to justify them, since deleting an audio_files row doesn't cascade.
SELECT a.id FROM artists a
WHERE NOT EXISTS (
    SELECT 1 FROM artist_credit_artist aca WHERE aca.artist_id = a.id
);

-- name: GetAlbumArtistsByLibrary :many
SELECT DISTINCT a.id, a.name, a.mbid
FROM artists a
JOIN artist_credit_artist aca ON aca.artist_id = a.id
JOIN artist_credit ac ON ac.id = aca.credit_id
JOIN release_groups rg ON rg.album_artist_credit_id = ac.id
WHERE a.id IN (
    SELECT DISTINCT aca2.artist_id
    FROM artist_credit_artist aca2
    JOIN artist_credit ac2 ON ac2.id = aca2.credit_id
    JOIN release_groups rg2 ON rg2.album_artist_credit_id = ac2.id
    JOIN release_group_recordings rgr2 ON rgr2.release_group_id = rg2.id
    JOIN recordings r2 ON r2.id = rgr2.recording_id
    JOIN audio_files af2 ON af2.recording_id = r2.id
    WHERE af2.library_id = ?
)
ORDER BY a.name;
