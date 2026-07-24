-- name: CreateAudioFile :one
INSERT INTO audio_files (file_path, length_milliseconds, file_type_id, recording_id, sample_rate, bit_depth, channels, bitrate, file_size, basename, library_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: CreateAudioFileWithGroupKey :one
INSERT INTO audio_files (
  file_path, length_milliseconds, file_type_id, recording_id,
  sample_rate, bit_depth, channels, bitrate, file_size, basename,
  library_id, group_key, tag_status
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetAudioFileGroupKey :one
SELECT group_key FROM audio_files
WHERE id = ? LIMIT 1;

-- name: SetAudioFileGroupKey :exec
UPDATE audio_files SET group_key = ? WHERE id = ?;

-- name: GetAudioFile :one
SELECT * FROM audio_files 
WHERE id = ? LIMIT 1;

-- name: GetAudioFileByPath :one
SELECT * FROM audio_files
WHERE file_path = ? LIMIT 1;

-- name: UpdateAudioFile :exec
UPDATE audio_files 
SET file_path = ?, length_milliseconds = ?, file_type_id = ?, recording_id = ?, sample_rate = ?, bit_depth = ?, channels = ?, bitrate = ?, file_size = ?, basename = ?
WHERE id = ?;

-- name: UpdateAudioFileRecording :exec
UPDATE audio_files
SET recording_id = ?, sample_rate = ?, bit_depth = ?, channels = ?, bitrate = ?, file_size = ?
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
    af.length_milliseconds,
    COALESCE(r.name, '') AS title,
    COALESCE(ac.text, '') AS artist,
    COALESCE(rg.name, '') AS album,
    COALESCE(ca.file_path, '') AS cover_art_path,
    COALESCE(a.mbid, '') AS artist_mbid,
    COALESCE(rg.mbid, '') AS release_group_mbid,
    COALESCE(r.mbid, '') AS recording_mbid
FROM audio_files af
LEFT JOIN recordings r ON af.recording_id = r.id
LEFT JOIN artist_credit ac ON r.artist_credit_id = ac.id
LEFT JOIN artist_credit_artist aca ON aca.credit_id = ac.id
LEFT JOIN artists a ON a.id = aca.artist_id
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
    CAST(COALESCE(
        (SELECT GROUP_CONCAT(g.name, '||')
         FROM recording_genres rg_sub
         JOIN genres g ON rg_sub.genre_id = g.id
         WHERE rg_sub.recording_id = r.id),
        ''
    ) AS TEXT) AS genre,
    COALESCE(r.year, 0) AS year,
    COALESCE(r.composer, '') AS composer,
    COALESCE(ft.extension, '') AS file_type,
    af.sample_rate,
    af.bit_depth,
    af.channels,
    af.bitrate,
    af.file_size,
    af.play_count,
    af.last_played,
    COALESCE(ca.file_path, '') AS cover_art_path,
    COALESCE(a.mbid, '') AS artist_mbid,
    COALESCE(rg.mbid, '') AS release_group_mbid,
    COALESCE(r.mbid, '') AS recording_mbid
FROM audio_files af
JOIN recordings r ON af.recording_id = r.id
JOIN artist_credit ac ON r.artist_credit_id = ac.id
LEFT JOIN artist_credit_artist aca ON aca.credit_id = ac.id
LEFT JOIN artists a ON a.id = aca.artist_id
LEFT JOIN release_group_recordings rgr ON r.id = rgr.recording_id
LEFT JOIN release_groups rg ON rgr.release_group_id = rg.id
LEFT JOIN cover_art ca ON rg.cover_art_id = ca.id
LEFT JOIN file_types ft ON af.file_type_id = ft.id;

-- name: SearchAudioFilesByBasename :many
SELECT
    af.file_path,
    af.length_milliseconds,
    COALESCE(r.name, '') AS title,
    COALESCE(ac.text, '') AS artist,
    COALESCE(rg.name, '') AS album
FROM audio_files af
LEFT JOIN recordings r ON af.recording_id = r.id
LEFT JOIN artist_credit ac ON r.artist_credit_id = ac.id
LEFT JOIN (
    SELECT recording_id, MIN(release_group_id) AS release_group_id
    FROM release_group_recordings
    GROUP BY recording_id
) rgr ON r.id = rgr.recording_id
LEFT JOIN release_groups rg ON rgr.release_group_id = rg.id
WHERE af.basename = ?
LIMIT ?;

-- name: LookupTrackMetaByPaths :many
SELECT id, file_path, title, artist_name, album, cover_art_path, artist_mbid, release_group_mbid, recording_mbid
FROM track_metadata
WHERE file_path IN (sqlc.slice('paths'));

-- name: GetAudioFilesByLibrary :many
SELECT * FROM audio_files WHERE library_id = ?;

-- name: CountAudioFilesByLibrary :one
SELECT COUNT(*) AS count FROM audio_files WHERE library_id = ?;

-- name: DeleteAllAudioFiles :exec
DELETE FROM audio_files;

-- name: GetAllTracksWithFullMetadataByLibrary :many
SELECT
    af.file_path,
    af.length_milliseconds,
    COALESCE(r.name, '') AS title,
    COALESCE(ac.text, '') AS artist_name,
    r.track_number,
    r.disc_number,
    COALESCE(rg.name, '') AS album,
    CAST(COALESCE(
        (SELECT GROUP_CONCAT(g.name, '||')
         FROM recording_genres rg_sub
         JOIN genres g ON rg_sub.genre_id = g.id
         WHERE rg_sub.recording_id = r.id),
        ''
    ) AS TEXT) AS genre,
    COALESCE(r.year, 0) AS year,
    COALESCE(r.composer, '') AS composer,
    COALESCE(ft.extension, '') AS file_type,
    af.sample_rate,
    af.bit_depth,
    af.channels,
    af.bitrate,
    af.file_size,
    af.play_count,
    af.last_played,
    COALESCE(ca.file_path, '') AS cover_art_path,
    COALESCE(a.mbid, '') AS artist_mbid,
    COALESCE(rg.mbid, '') AS release_group_mbid,
    COALESCE(r.mbid, '') AS recording_mbid
FROM audio_files af
JOIN recordings r ON af.recording_id = r.id
JOIN artist_credit ac ON r.artist_credit_id = ac.id
LEFT JOIN artist_credit_artist aca ON aca.credit_id = ac.id
LEFT JOIN artists a ON a.id = aca.artist_id
LEFT JOIN release_group_recordings rgr ON r.id = rgr.recording_id
LEFT JOIN release_groups rg ON rgr.release_group_id = rg.id
LEFT JOIN cover_art ca ON rg.cover_art_id = ca.id
LEFT JOIN file_types ft ON af.file_type_id = ft.id
WHERE af.library_id = ?;

-- name: GetAudioFilesByReleaseGroup :many
SELECT
    af.file_path,
    af.length_milliseconds,
    COALESCE(r.name, '') AS title,
    COALESCE(ac.text, '') AS artist_name,
    rgr.track_number,
    rgr.disc_number,
    COALESCE(rg.name, '') AS album,
    CAST(COALESCE(
        (SELECT GROUP_CONCAT(g.name, '||')
         FROM recording_genres rg_sub
         JOIN genres g ON rg_sub.genre_id = g.id
         WHERE rg_sub.recording_id = r.id),
        ''
    ) AS TEXT) AS genre,
    COALESCE(r.year, 0) AS year,
    COALESCE(r.composer, '') AS composer,
    COALESCE(ft.extension, '') AS file_type,
    af.sample_rate,
    af.bit_depth,
    af.channels,
    af.bitrate,
    af.file_size,
    COALESCE(a.mbid, '') AS artist_mbid,
    COALESCE(rg.mbid, '') AS release_group_mbid,
    COALESCE(r.mbid, '') AS recording_mbid
FROM release_group_recordings rgr
JOIN recordings r ON rgr.recording_id = r.id
JOIN audio_files af ON af.recording_id = r.id
LEFT JOIN artist_credit ac ON r.artist_credit_id = ac.id
LEFT JOIN artist_credit_artist aca ON aca.credit_id = ac.id
LEFT JOIN artists a ON a.id = aca.artist_id
LEFT JOIN release_groups rg ON rgr.release_group_id = rg.id
LEFT JOIN file_types ft ON af.file_type_id = ft.id
WHERE rgr.release_group_id = ?
ORDER BY rgr.disc_number, rgr.track_number;

-- name: GetAudioFilesByReleaseGroupByLibrary :many
SELECT
    af.file_path,
    af.length_milliseconds,
    COALESCE(r.name, '') AS title,
    COALESCE(ac.text, '') AS artist_name,
    rgr.track_number,
    rgr.disc_number,
    COALESCE(rg.name, '') AS album,
    CAST(COALESCE(
        (SELECT GROUP_CONCAT(g.name, '||')
         FROM recording_genres rg_sub
         JOIN genres g ON rg_sub.genre_id = g.id
         WHERE rg_sub.recording_id = r.id),
        ''
    ) AS TEXT) AS genre,
    COALESCE(r.year, 0) AS year,
    COALESCE(r.composer, '') AS composer,
    COALESCE(ft.extension, '') AS file_type,
    af.sample_rate,
    af.bit_depth,
    af.channels,
    af.bitrate,
    af.file_size,
    COALESCE(a.mbid, '') AS artist_mbid,
    COALESCE(rg.mbid, '') AS release_group_mbid,
    COALESCE(r.mbid, '') AS recording_mbid
FROM release_group_recordings rgr
JOIN recordings r ON rgr.recording_id = r.id
JOIN audio_files af ON af.recording_id = r.id
LEFT JOIN artist_credit ac ON r.artist_credit_id = ac.id
LEFT JOIN artist_credit_artist aca ON aca.credit_id = ac.id
LEFT JOIN artists a ON a.id = aca.artist_id
LEFT JOIN release_groups rg ON rgr.release_group_id = rg.id
LEFT JOIN file_types ft ON af.file_type_id = ft.id
WHERE rgr.release_group_id = ? AND af.library_id = ?
ORDER BY rgr.disc_number, rgr.track_number;
