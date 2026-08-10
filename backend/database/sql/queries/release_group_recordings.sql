-- name: CreateReleaseGroupRecording :one
INSERT INTO release_group_recordings (release_group_id, recording_id, track_number, disc_number)
VALUES (?, ?, ?, ?)
RETURNING *;

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
