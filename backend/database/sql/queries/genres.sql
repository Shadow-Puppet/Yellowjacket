-- Queries over genres and file_genres.
--
-- The track-returning ones live in audio_files.sql with the rest of the
-- track_metadata reads; what is left here is the genre list itself and
-- the link table's writes.

-- name: UpsertGenre :one
INSERT INTO genres (name) VALUES (?)
ON CONFLICT(name) DO UPDATE SET name = excluded.name
RETURNING *;

-- name: LinkFileGenre :exec
INSERT OR IGNORE INTO file_genres (audio_file_id, genre_id) VALUES (?, ?);

-- name: DeleteFileGenres :exec
DELETE FROM file_genres WHERE audio_file_id = ?;

-- name: GetGenreNamesByFile :many
SELECT g.name FROM genres g
JOIN file_genres fg ON fg.genre_id = g.id
WHERE fg.audio_file_id = ?;

-- name: GetGenreNamesByFilePaths :many
-- Genres for many files at once.  The mix builder asked this one file
-- at a time, inside two nested loops -- twelve thousand single-row
-- queries to assemble one mix.
SELECT af.file_path, g.name
FROM audio_files af
JOIN file_genres fg ON fg.audio_file_id = af.id
JOIN genres g ON g.id = fg.genre_id
WHERE af.file_path IN (sqlc.slice('paths'));

-- name: DeleteGenre :exec
DELETE FROM genres WHERE id = ?;

-- name: DeleteAllGenres :exec
DELETE FROM genres;

-- name: GetUnusedGenreIDs :many
SELECT id FROM genres g
WHERE NOT EXISTS (SELECT 1 FROM file_genres fg WHERE fg.genre_id = g.id);

-- name: GetAllGenresWithCounts :many
SELECT g.name, COUNT(fg.audio_file_id) AS track_count
FROM genres g
JOIN file_genres fg ON fg.genre_id = g.id
JOIN audio_files af ON af.id = fg.audio_file_id
WHERE af.library_id = COALESCE(NULLIF(CAST(sqlc.arg(library_id) AS INTEGER), 0), af.library_id)
GROUP BY g.id, g.name
ORDER BY g.name;
