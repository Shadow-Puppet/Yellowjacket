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

-- name: GetPlaylistTracksWithMetadata :many
SELECT
    pt.id,
    pt.playlist_id,
    pt.audio_file_id,
    pt.position,
    af.file_path,
    af.length_milliseconds,
    COALESCE(r.name, '') AS title,
    COALESCE(ac.text, '') AS artist,
    COALESCE(rg.name, '') AS album,
    COALESCE(ca.file_path, '') AS cover_art_path
FROM playlist_tracks pt
JOIN audio_files af ON pt.audio_file_id = af.id
LEFT JOIN recordings r ON af.recording_id = r.id
LEFT JOIN artist_credit ac ON r.artist_credit_id = ac.id
LEFT JOIN (
    SELECT recording_id, MIN(release_group_id) AS release_group_id
    FROM release_group_recordings
    GROUP BY recording_id
) rgr ON r.id = rgr.recording_id
LEFT JOIN release_groups rg ON rgr.release_group_id = rg.id
LEFT JOIN cover_art ca ON rg.cover_art_id = ca.id
WHERE pt.playlist_id = ?
ORDER BY pt.position;

-- name: GetAllPlaylistTracksWithMetadata :many
SELECT
    pt.id,
    pt.playlist_id,
    pt.audio_file_id,
    pt.position,
    af.file_path,
    af.length_milliseconds,
    COALESCE(r.name, '') AS title,
    COALESCE(ac.text, '') AS artist,
    COALESCE(rg.name, '') AS album,
    COALESCE(ca.file_path, '') AS cover_art_path
FROM playlist_tracks pt
JOIN audio_files af ON pt.audio_file_id = af.id
LEFT JOIN recordings r ON af.recording_id = r.id
LEFT JOIN artist_credit ac ON r.artist_credit_id = ac.id
LEFT JOIN (
    SELECT recording_id, MIN(release_group_id) AS release_group_id
    FROM release_group_recordings
    GROUP BY recording_id
) rgr ON r.id = rgr.recording_id
LEFT JOIN release_groups rg ON rgr.release_group_id = rg.id
LEFT JOIN cover_art ca ON rg.cover_art_id = ca.id
ORDER BY pt.playlist_id, pt.position;

-- name: DeleteAllPlaylistTracks :exec
DELETE FROM playlist_tracks;

-- name: GetNextPlaylistTrackPosition :one
SELECT COALESCE(MAX(position), -1) + 1 AS next_position
FROM playlist_tracks WHERE playlist_id = ?;

-- name: GetPlaylistTrackFilePaths :many
SELECT af.file_path
FROM playlist_tracks pt
JOIN audio_files af ON pt.audio_file_id = af.id
WHERE pt.playlist_id = ?
ORDER BY pt.position;

-- name: IsTrackInPlaylist :one
SELECT EXISTS(
    SELECT 1 FROM playlist_tracks pt
    JOIN audio_files af ON pt.audio_file_id = af.id
    WHERE pt.playlist_id = ? AND af.file_path = ?
) AS in_playlist;

-- name: RemovePlaylistTrackByPath :exec
DELETE FROM playlist_tracks
WHERE playlist_id = ? AND audio_file_id = (
    SELECT id FROM audio_files WHERE file_path = ?
);
