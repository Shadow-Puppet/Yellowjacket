-- Queries backing the dynamic-mix queue fallback (backend/explore/mix.go):
-- expanding a seed selection into a candidate pool by artist similarity
-- and genre overlap, restricted to what is actually in the library.

-- name: GetFilePathsByArtistMBID :many
SELECT DISTINCT af.file_path
FROM audio_files af
JOIN recordings r ON af.recording_id = r.id
JOIN artist_credit ac ON r.artist_credit_id = ac.id
JOIN artist_credit_artist aca ON aca.credit_id = ac.id
JOIN artists a ON a.id = aca.artist_id
WHERE a.mbid = ?;

-- name: GetGenreNamesByFilePath :many
SELECT DISTINCT g.name
FROM genres g
JOIN recording_genres rg ON g.id = rg.genre_id
JOIN recordings r ON rg.recording_id = r.id
JOIN audio_files af ON af.recording_id = r.id
WHERE af.file_path = ?;

-- name: GetArtistByFilePath :one
SELECT COALESCE(a.name, '') AS artist_name, COALESCE(a.mbid, '') AS artist_mbid
FROM audio_files af
JOIN recordings r ON af.recording_id = r.id
JOIN artist_credit ac ON r.artist_credit_id = ac.id
JOIN artist_credit_artist aca ON aca.credit_id = ac.id
JOIN artists a ON a.id = aca.artist_id
WHERE af.file_path = ?
LIMIT 1;
