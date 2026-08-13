-- name: CreateReleaseGroupRecording :one
INSERT INTO release_group_recordings (
  release_group_id, recording_id, track_number, disc_number, total_tracks
)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: GetAlbumCompleteness :one
WITH discs AS (
    SELECT
        COALESCE(rgr.disc_number, 1) AS disc,
        MAX(COALESCE(rgr.total_tracks, 0)) AS declared,
        COUNT(DISTINCT COALESCE(rgr.track_number, -rgr.recording_id)) AS owned
    FROM release_group_recordings rgr
    WHERE rgr.release_group_id = ?
    GROUP BY COALESCE(rgr.disc_number, 1)
)
SELECT
    CAST(COALESCE(SUM(owned), 0) AS INTEGER) AS owned,
    CAST(COALESCE(SUM(declared), 0) AS INTEGER) AS expected,
    CAST(COALESCE(SUM(CASE WHEN declared = 0 THEN 1 ELSE 0 END), 0) AS INTEGER) AS discs_untotalled
FROM discs;

-- name: GetReleaseGroupRecording :one
SELECT * FROM release_group_recordings 
WHERE id = ? LIMIT 1;

-- name: GetReleaseGroupRecordings :many
SELECT * FROM release_group_recordings
WHERE release_group_id = ?
ORDER BY disc_number, track_number;

-- name: GetRecordingReleaseGroups :many
SELECT * FROM release_group_recordings
WHERE recording_id = ?;

-- name: DeleteReleaseGroupRecording :exec
DELETE FROM release_group_recordings 
WHERE id = ?;

-- name: DeleteReleaseGroupRecordingByFK :exec
DELETE FROM release_group_recordings
WHERE release_group_id = ? AND recording_id = ?;

-- name: DeleteAllReleaseGroupRecordings :exec
DELETE FROM release_group_recordings;

-- name: DeleteReleaseGroupRecordingsByRecording :exec
DELETE FROM release_group_recordings
WHERE recording_id = ?;
