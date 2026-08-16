-- Queries over audio_files and the track_metadata view above it.
--
-- Every query that returns "a track" selects from `track_metadata`,
-- which is the one place the projection is defined.  The scoped and
-- unscoped variants that used to be written twice are one query now:
-- library_id 0 means "every library", and `(:id = 0 OR library_id = :id)`
-- costs nothing measurable (23 ms vs 21 ms over 26k rows) because these
-- queries scan either way.

-- ---------------------------------------------------------------------
-- Writes
-- ---------------------------------------------------------------------

-- name: CreateAudioFile :one
INSERT INTO audio_files (
  file_path, library_id, file_type_id,
  length_milliseconds, sample_rate, bit_depth, channels, bitrate, file_size,
  title, artist_credit, artist_id, album_id,
  track_number, disc_number, total_tracks, year, composer, comment,
  recording_mbid, basename, group_key, modified_at, tag_status
) VALUES (
  ?, ?, ?,
  ?, ?, ?, ?, ?, ?,
  ?, ?, ?, ?,
  ?, ?, ?, ?, ?, ?,
  ?, ?, ?, ?, ?
)
RETURNING *;

-- name: UpdateAudioFileTags :exec
-- A rescan of a file whose mtime moved: the tags are re-read and
-- written over the same row.  Under the old schema this created a
-- *new* recording and repointed the file at it, abandoning the old one
-- -- which is where 812 orphaned rows and every phantom "you own this"
-- came from.  There is nothing to orphan now.
UPDATE audio_files
SET title = ?, artist_credit = ?, artist_id = ?, album_id = ?,
    track_number = ?, disc_number = ?, total_tracks = ?, year = ?,
    composer = ?, comment = ?, recording_mbid = ?,
    sample_rate = ?, bit_depth = ?, channels = ?, bitrate = ?,
    file_size = ?, length_milliseconds = ?, modified_at = ?
WHERE id = ?;

-- name: SetAudioFileGroupKey :exec
UPDATE audio_files SET group_key = ? WHERE id = ?;

-- name: PromoteAudioFileTagStatusIfUntagged :exec
-- A rescan re-reads the tags of a file whose mtime moved, so a file
-- another tagger stamped with MBIDs since import arrives here still
-- carrying the 'untagged' status it was created with (only the insert
-- path sets it).  Promote it the same way saveAudioFile does.
-- Guarded on 'untagged' so it cannot overwrite a deliberate
-- 'user_skipped_permanent', and so a file losing its MBIDs is left
-- alone -- demotion is the scan's judgement, not this statement's.
UPDATE audio_files
SET tag_status = 'user_confirmed'
WHERE id = ? AND tag_status = 'untagged';

-- name: UpdateAudioFileStat :exec
-- Records the on-disk mtime/size without re-reading tags.  Used to
-- backfill the staleness baseline for files the scan skipped, and to
-- re-baseline after YellowJacket's own tag writer rewrites a file.
UPDATE audio_files
SET modified_at = ?, file_size = ?
WHERE id = ?;

-- name: SetAudioFileRecordingMBID :exec
UPDATE audio_files SET recording_mbid = ? WHERE id = ?;

-- name: DeleteAudioFile :exec
DELETE FROM audio_files WHERE id = ?;

-- name: DeleteAllAudioFiles :exec
DELETE FROM audio_files;

-- ---------------------------------------------------------------------
-- Reads: the file row itself
-- ---------------------------------------------------------------------

-- name: GetAudioFile :one
SELECT * FROM audio_files WHERE id = ? LIMIT 1;

-- name: GetAudioFileByPath :one
SELECT * FROM audio_files WHERE file_path = ? LIMIT 1;

-- name: GetAudioFileGroupKey :one
SELECT group_key FROM audio_files WHERE id = ? LIMIT 1;

-- name: GetAllAudioFilePaths :many
SELECT id, file_path FROM audio_files;

-- name: GetAudioFilesByPaths :many
SELECT id, library_id, file_path, group_key FROM audio_files
WHERE file_path IN (sqlc.slice('paths'));

-- name: GetRandomAudioFilePath :one
SELECT file_path FROM audio_files ORDER BY RANDOM() LIMIT 1;

-- name: CountAudioFiles :one
SELECT COUNT(*) AS count FROM audio_files
WHERE library_id = COALESCE(NULLIF(CAST(sqlc.arg(library_id) AS INTEGER), 0), library_id);

-- name: GetLibraryMaxModifiedAt :one
-- Newest recorded mtime in a library, for the startup soft scan.  0 when
-- the library is empty or no row has a baseline yet.
SELECT CAST(COALESCE(MAX(modified_at), 0) AS INTEGER) FROM audio_files
WHERE library_id = ?;

-- ---------------------------------------------------------------------
-- Reads: tracks
-- ---------------------------------------------------------------------

-- name: GetTracks :many
SELECT * FROM track_metadata
WHERE library_id = COALESCE(NULLIF(CAST(sqlc.arg(library_id) AS INTEGER), 0), library_id);

-- name: GetTrackByPath :one
SELECT * FROM track_metadata WHERE file_path = ? LIMIT 1;

-- name: GetTracksByAlbum :many
SELECT * FROM track_metadata
WHERE album_id = sqlc.arg(album_id)
  AND library_id = COALESCE(NULLIF(CAST(sqlc.arg(library_id) AS INTEGER), 0), library_id)
ORDER BY disc_number, track_number;

-- name: GetTracksByGenre :many
SELECT tm.* FROM track_metadata tm
JOIN file_genres fg ON fg.audio_file_id = tm.id
JOIN genres g ON g.id = fg.genre_id
WHERE g.name = sqlc.arg(genre)
  AND tm.library_id = COALESCE(NULLIF(CAST(sqlc.arg(library_id) AS INTEGER), 0), tm.library_id);

-- name: LookupTrackMetaByPaths :many
SELECT id, file_path, title, artist_name, album, cover_art_path,
       artist_mbid, release_group_mbid, recording_mbid
FROM track_metadata
WHERE file_path IN (sqlc.slice('paths'));

-- name: SearchTracksByBasename :many
SELECT id, file_path, length_milliseconds, title, artist_name, album
FROM track_metadata
WHERE file_path IN (
    SELECT file_path FROM audio_files WHERE basename = sqlc.arg(basename)
)
LIMIT sqlc.arg(lim);

-- ---------------------------------------------------------------------
-- Reads: file paths, grouped by whatever the caller asked about
-- ---------------------------------------------------------------------
-- These answer "what can I play" and they all ask audio_files, because
-- that is the only table whose rows are files.  Grouped rather than
-- flattened because the caller owns the order.

-- name: GetFilePathsByAlbums :many
-- The library filter is applied in Go rather than here: sqlc numbers a
-- named parameter (?2) but expands a slice into N placeholders, so the
-- two together bind the wrong values - GetFilePathsByAlbums([1,2], 0)
-- read album id 2 as the library id.  Returning library_id and
-- filtering the (small) result is the version that cannot be wrong.
SELECT album_id, library_id, file_path FROM audio_files
WHERE album_id IN (sqlc.slice('album_ids'))
ORDER BY disc_number, track_number;

-- name: GetFilePathsByRecordingMBIDs :many
-- The ownership question in its only honest form: which of these
-- catalog recordings has a *file* behind it.  Asked of audio_files, so
-- a metadata row with no file cannot answer yes.
SELECT recording_mbid, library_id, file_path FROM audio_files
WHERE recording_mbid IN (sqlc.slice('mbids'))
ORDER BY file_path;

-- name: GetFilePathsByGenres :many
SELECT g.name AS genre, af.library_id, af.file_path
FROM audio_files af
JOIN file_genres fg ON fg.audio_file_id = af.id
JOIN genres g ON g.id = fg.genre_id
WHERE g.name IN (sqlc.slice('genres'))
ORDER BY af.disc_number, af.track_number;

-- name: GetFilePathsByArtistMBID :many
SELECT DISTINCT af.file_path
FROM audio_files af
JOIN artists a ON a.id = af.artist_id
WHERE a.mbid = ?;

-- ---------------------------------------------------------------------
-- Ownership, asked in bulk
-- ---------------------------------------------------------------------

-- name: OwnedRecordingMBIDs :many
-- Which of these recording MBIDs are actually in the library.  This is
-- what marks a catalog tracklist owned; it used to be
-- `SELECT mbid FROM recordings`, which answered yes for 129 tracks in a
-- real library that had no file at all.
SELECT DISTINCT recording_mbid FROM audio_files
WHERE recording_mbid IN (sqlc.slice('mbids'));

-- name: OwnedAlbumMBIDs :many
SELECT DISTINCT al.mbid FROM albums al
JOIN audio_files af ON af.album_id = al.id
WHERE al.mbid IN (sqlc.slice('mbids'));

-- name: OwnedArtistMBIDs :many
SELECT DISTINCT a.mbid FROM artists a
JOIN audio_files af ON af.artist_id = a.id
WHERE a.mbid IN (sqlc.slice('mbids'));

-- name: GetAudioFilesInLibrary :many
SELECT * FROM audio_files WHERE library_id = ?;
