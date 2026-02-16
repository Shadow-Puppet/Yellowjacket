-- name: CreatePlaylist :one
INSERT INTO playlists (name) VALUES (?)
RETURNING *;

-- name: GetPlaylist :one
SELECT * FROM playlists WHERE id = ? LIMIT 1;

-- name: GetAllPlaylists :many
SELECT * FROM playlists ORDER BY updated_at DESC;

-- name: UpdatePlaylistName :exec
UPDATE playlists SET name = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?;

-- name: DeletePlaylist :exec
DELETE FROM playlists WHERE id = ?;

-- name: AddPlaylistTrack :one
INSERT INTO playlist_tracks (playlist_id, audio_file_id, position) VALUES (?, ?, ?)
RETURNING *;

-- name: GetPlaylistTracks :many
SELECT pt.id, pt.playlist_id, pt.audio_file_id, pt.position, af.file_path
FROM playlist_tracks pt
JOIN audio_files af ON pt.audio_file_id = af.id
WHERE pt.playlist_id = ?
ORDER BY pt.position;

-- name: RemovePlaylistTrack :exec
DELETE FROM playlist_tracks WHERE id = ?;

-- name: ClearPlaylistTracks :exec
DELETE FROM playlist_tracks WHERE playlist_id = ?;

-- name: GetNextPlaylistTrackPosition :one
SELECT COALESCE(MAX(position), -1) + 1 AS next_position
FROM playlist_tracks WHERE playlist_id = ?;
