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

-- name: CountPlaylistsByName :one
SELECT COUNT(*) AS count FROM playlists WHERE name = ?;

-- name: AddPlaylistTrack :one
INSERT INTO playlist_tracks (
    playlist_id, audio_file_id, position,
    phantom_title, phantom_artist, phantom_album,
    phantom_duration_ms, phantom_genre, phantom_cover_art_path
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetPlaylistTracks :many
SELECT pt.id, pt.playlist_id, pt.audio_file_id, pt.position,
    COALESCE(af.file_path, '') AS file_path,
    pt.phantom_title, pt.phantom_artist, pt.phantom_album,
    pt.phantom_duration_ms, pt.phantom_genre, pt.phantom_cover_art_path
FROM playlist_tracks pt
LEFT JOIN audio_files af ON pt.audio_file_id = af.id
WHERE pt.playlist_id = ?
ORDER BY pt.position;

-- name: RemovePlaylistTrack :exec
DELETE FROM playlist_tracks WHERE id = ?;

-- name: ClearPlaylistTracks :exec
DELETE FROM playlist_tracks WHERE playlist_id = ?;

-- name: GetPlaylistTracksWithMetadata :many
SELECT
    pt.id,
    pt.playlist_id,
    pt.audio_file_id,
    pt.position,
    COALESCE(tm.file_path, '') AS file_path,
    COALESCE(tm.length_milliseconds, 0) AS length_milliseconds,
    COALESCE(tm.title, pt.phantom_title, '') AS title,
    COALESCE(tm.artist_name, pt.phantom_artist, '') AS artist,
    COALESCE(tm.album, pt.phantom_album, '') AS album,
    COALESCE(NULLIF(tm.cover_art_path, ''), pt.phantom_cover_art_path, '') AS cover_art_path,
    CASE WHEN pt.audio_file_id IS NULL THEN 1 ELSE 0 END AS is_phantom,
    CAST(COALESCE(tm.artist_mbid, '') AS TEXT) AS artist_mbid,
    COALESCE(tm.release_group_mbid, '') AS release_group_mbid,
    COALESCE(tm.recording_mbid, '') AS recording_mbid
FROM playlist_tracks pt
LEFT JOIN track_metadata tm ON tm.id = pt.audio_file_id
WHERE pt.playlist_id = ?
ORDER BY pt.position;

-- name: GetAllPlaylistTracksWithMetadata :many
SELECT
    pt.id,
    pt.playlist_id,
    pt.audio_file_id,
    pt.position,
    COALESCE(tm.file_path, '') AS file_path,
    COALESCE(tm.length_milliseconds, 0) AS length_milliseconds,
    COALESCE(tm.title, pt.phantom_title, '') AS title,
    COALESCE(tm.artist_name, pt.phantom_artist, '') AS artist,
    COALESCE(tm.album, pt.phantom_album, '') AS album,
    COALESCE(NULLIF(tm.cover_art_path, ''), pt.phantom_cover_art_path, '') AS cover_art_path,
    CASE WHEN pt.audio_file_id IS NULL THEN 1 ELSE 0 END AS is_phantom,
    CAST(COALESCE(tm.artist_mbid, '') AS TEXT) AS artist_mbid,
    COALESCE(tm.release_group_mbid, '') AS release_group_mbid,
    COALESCE(tm.recording_mbid, '') AS recording_mbid
FROM playlist_tracks pt
LEFT JOIN track_metadata tm ON tm.id = pt.audio_file_id
ORDER BY pt.playlist_id, pt.position;

-- name: DeleteAllPlaylistTracks :exec
DELETE FROM playlist_tracks;

-- name: GetNextPlaylistTrackPosition :one
SELECT COALESCE(MAX(position), -1) + 1 AS next_position
FROM playlist_tracks WHERE playlist_id = ?;

-- name: GetPlaylistTrackFilePaths :many
SELECT COALESCE(af.file_path, '') AS file_path
FROM playlist_tracks pt
LEFT JOIN audio_files af ON pt.audio_file_id = af.id
WHERE pt.playlist_id = ? AND pt.audio_file_id IS NOT NULL
ORDER BY pt.position;

-- name: IsTrackInPlaylist :one
SELECT EXISTS(
    SELECT 1 FROM playlist_tracks pt
    LEFT JOIN audio_files af ON pt.audio_file_id = af.id
    WHERE pt.playlist_id = ? AND af.file_path = ?
) AS in_playlist;

-- name: RemovePlaylistTrackByPath :exec
DELETE FROM playlist_tracks
WHERE playlist_id = ? AND audio_file_id = (
    SELECT id FROM audio_files WHERE file_path = ?
);

-- name: GetTrackPhantomMetadata :one
-- The display fields a playlist row keeps after its file goes away.
SELECT title, artist_name AS artist, album,
       length_milliseconds AS duration_ms, genre, cover_art_path
FROM track_metadata
WHERE id = ?;
