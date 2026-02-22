-- name: CreateAudioFile :one
INSERT INTO audio_files (file_path, length_milliseconds, file_type_id, recording_id) VALUES (?, ?, ?, ?)
RETURNING *;

-- name: GetAudioFile :one
SELECT * FROM audio_files 
WHERE id = ? LIMIT 1;

-- name: GetAudioFileByPath :one
SELECT * FROM audio_files
WHERE file_path = ? LIMIT 1;

-- name: UpdateAudioFile :exec
UPDATE audio_files 
SET file_path = ?, length_milliseconds = ?, file_type_id = ?, recording_id = ?
WHERE id = ?;

-- name: UpdateAudioFileRecording :exec
UPDATE audio_files
SET recording_id = ?
WHERE id = ?;

-- name: DeleteAudioFile :exec
DELETE FROM audio_files 
WHERE id = ?;

-- name: CountAudioFiles :one
SELECT count(*) FROM audio_files;

-- name: GetRandomAudioFilePath :one
SELECT file_path FROM audio_files
ORDER BY RANDOM()
LIMIT 1;

-- name: GetAllAudioFiles :many
SELECT * FROM audio_files;

-- name: GetAllAudioFilePaths :many
SELECT id, file_path FROM audio_files;

-- name: GetAudioFilesNeedingMetadata :many
SELECT * FROM audio_files
WHERE recording_id = 0;

-- name: GetAllAudioFilesWithArtist :many
SELECT 
    af.id,
    af.file_path,
    af.length_milliseconds,
    af.file_type_id,
    af.recording_id,
    COALESCE(ac.text, '') AS artist_name,
    COALESCE(r.name, '') AS title
FROM audio_files af
JOIN recordings r ON af.recording_id = r.id
JOIN artist_credit ac ON r.artist_credit_id = ac.id;

-- name: GetTrackMetadataByPath :one
SELECT 
    af.file_path,
    COALESCE(r.name, '') AS title,
    COALESCE(ac.text, '') AS artist,
    COALESCE(rg.name, '') AS album,
    COALESCE(ca.file_path, '') AS cover_art_path
FROM audio_files af
LEFT JOIN recordings r ON af.recording_id = r.id
LEFT JOIN artist_credit ac ON r.artist_credit_id = ac.id
LEFT JOIN release_group_recordings rgr ON r.id = rgr.recording_id
LEFT JOIN release_groups rg ON rgr.release_group_id = rg.id
LEFT JOIN cover_art ca ON rg.cover_art_id = ca.id
WHERE af.file_path = ?
LIMIT 1;

-- name: GetAllTracksWithFullMetadata :many
SELECT
    af.file_path,
    af.length_milliseconds,
    COALESCE(r.name, '') AS title,
    COALESCE(ac.text, '') AS artist_name,
    r.track_number,
    r.disc_number,
    COALESCE(rg.name, '') AS album,
    COALESCE(r.genre, '') AS genre,
    COALESCE(r.year, 0) AS year,
    COALESCE(r.composer, '') AS composer,
    COALESCE(ft.extension, '') AS file_type
FROM audio_files af
JOIN recordings r ON af.recording_id = r.id
JOIN artist_credit ac ON r.artist_credit_id = ac.id
LEFT JOIN release_group_recordings rgr ON r.id = rgr.recording_id
LEFT JOIN release_groups rg ON rgr.release_group_id = rg.id
LEFT JOIN file_types ft ON af.file_type_id = ft.id;

-- name: DeleteAllAudioFiles :exec
DELETE FROM audio_files;

-- name: GetAudioFilesByReleaseGroup :many
SELECT
    af.file_path,
    af.length_milliseconds,
    COALESCE(r.name, '') AS title,
    COALESCE(ac.text, '') AS artist_name,
    rgr.track_number,
    rgr.disc_number
FROM release_group_recordings rgr
JOIN recordings r ON rgr.recording_id = r.id
JOIN audio_files af ON af.recording_id = r.id
LEFT JOIN artist_credit ac ON r.artist_credit_id = ac.id
WHERE rgr.release_group_id = ?
ORDER BY rgr.disc_number, rgr.track_number;
