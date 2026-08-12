-- name: UpsertGenre :one
INSERT INTO genres (name) VALUES (?)
ON CONFLICT(name) DO UPDATE SET name = name
RETURNING *;

-- name: CreateRecordingGenre :exec
INSERT OR IGNORE INTO recording_genres (recording_id, genre_id)
VALUES (?, ?);

-- name: DeleteRecordingGenres :exec
DELETE FROM recording_genres
WHERE recording_id = ?;

-- name: GetGenresByRecordingID :many
SELECT g.*
FROM genres g
JOIN recording_genres rg ON g.id = rg.genre_id
WHERE rg.recording_id = ?;

-- name: DeleteAllRecordingGenres :exec
DELETE FROM recording_genres;

-- name: DeleteAllGenres :exec
DELETE FROM genres;

-- name: GetTracksByGenre :many
SELECT
    af.file_path,
    af.length_milliseconds,
    COALESCE(r.name, '') AS title,
    COALESCE(ac.text, '') AS artist_name,
    r.track_number,
    r.disc_number,
    COALESCE(rlg.name, '') AS album,
    CAST(COALESCE(
        (SELECT GROUP_CONCAT(g2.name, '||')
         FROM recording_genres rg2
         JOIN genres g2 ON rg2.genre_id = g2.id
         WHERE rg2.recording_id = r.id),
        ''
    ) AS TEXT) AS genre,
    COALESCE(r.year, 0) AS year,
    COALESCE(r.composer, '') AS composer,
    COALESCE(ft.extension, '') AS file_type,
    af.sample_rate,
    af.bit_depth,
    af.channels,
    af.bitrate,
    af.file_size
FROM genres g
JOIN recording_genres rg ON g.id = rg.genre_id
JOIN recordings r ON rg.recording_id = r.id
JOIN audio_files af ON af.recording_id = r.id
JOIN artist_credit ac ON r.artist_credit_id = ac.id
LEFT JOIN (
    SELECT recording_id,
        MIN(release_group_id) AS release_group_id
    FROM release_group_recordings
    GROUP BY recording_id
) rgr ON r.id = rgr.recording_id
LEFT JOIN release_groups rlg ON rgr.release_group_id = rlg.id
LEFT JOIN file_types ft ON af.file_type_id = ft.id
WHERE g.name = ?
ORDER BY r.name;

-- name: GetTracksByGenreByLibrary :many
SELECT
    af.file_path,
    af.length_milliseconds,
    COALESCE(r.name, '') AS title,
    COALESCE(ac.text, '') AS artist_name,
    r.track_number,
    r.disc_number,
    COALESCE(rlg.name, '') AS album,
    CAST(COALESCE(
        (SELECT GROUP_CONCAT(g2.name, '||')
         FROM recording_genres rg2
         JOIN genres g2 ON rg2.genre_id = g2.id
         WHERE rg2.recording_id = r.id),
        ''
    ) AS TEXT) AS genre,
    COALESCE(r.year, 0) AS year,
    COALESCE(r.composer, '') AS composer,
    COALESCE(ft.extension, '') AS file_type,
    af.sample_rate,
    af.bit_depth,
    af.channels,
    af.bitrate,
    af.file_size
FROM genres g
JOIN recording_genres rg ON g.id = rg.genre_id
JOIN recordings r ON rg.recording_id = r.id
JOIN audio_files af ON af.recording_id = r.id
JOIN artist_credit ac ON r.artist_credit_id = ac.id
LEFT JOIN (
    SELECT recording_id,
        MIN(release_group_id) AS release_group_id
    FROM release_group_recordings
    GROUP BY recording_id
) rgr ON r.id = rgr.recording_id
LEFT JOIN release_groups rlg ON rgr.release_group_id = rlg.id
LEFT JOIN file_types ft ON af.file_type_id = ft.id
WHERE g.name = ? AND af.library_id = ?
ORDER BY r.name;

-- name: CountGenreReferences :one
SELECT COUNT(*) FROM recording_genres WHERE genre_id = ?;

-- name: DeleteGenre :exec
DELETE FROM genres WHERE id = ?;

-- name: GetAllGenresWithCounts :many
SELECT g.name, COUNT(rg.recording_id) AS track_count
FROM genres g
JOIN recording_genres rg ON g.id = rg.genre_id
GROUP BY g.id, g.name
ORDER BY g.name;

-- name: GetAllGenresWithCountsByLibrary :many
SELECT g.name, COUNT(rg.recording_id) AS track_count
FROM genres g
JOIN recording_genres rg ON g.id = rg.genre_id
JOIN recordings r ON rg.recording_id = r.id
JOIN audio_files af ON af.recording_id = r.id
WHERE af.library_id = ?
GROUP BY g.id, g.name
ORDER BY g.name;

-- Same as GetFilePathsByReleaseGroups, for "play these genres" (perf.m2):
-- one query instead of one per genre, and file paths instead of whole
-- track rows, which was 6 MB over the IPC for five genres.

-- name: GetFilePathsByGenres :many
SELECT g.name AS genre_name, af.file_path
FROM genres g
JOIN recording_genres rg ON g.id = rg.genre_id
JOIN recordings r ON rg.recording_id = r.id
JOIN audio_files af ON af.recording_id = r.id
WHERE g.name IN (sqlc.slice('genre_names'))
ORDER BY r.name;

-- name: GetFilePathsByGenresByLibrary :many
SELECT g.name AS genre_name, af.file_path
FROM genres g
JOIN recording_genres rg ON g.id = rg.genre_id
JOIN recordings r ON rg.recording_id = r.id
JOIN audio_files af ON af.recording_id = r.id
WHERE g.name IN (sqlc.slice('genre_names'))
  AND af.library_id = ?
ORDER BY r.name;
