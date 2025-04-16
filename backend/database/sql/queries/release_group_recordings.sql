-- name: CreateReleaseGroupRecording :one
INSERT INTO release_group_recordings (release_group_id, recording_id) VALUES (?,?) 
RETURNING *;

-- name: GetReleaseGroupRecording :one
SELECT * FROM release_group_recordings 
WHERE id = ? LIMIT 1;

-- name: UpdateReleaseGroupRecording :exec
UPDATE release_group_recordings 
SET release_group_id = ?, recording_id = ?
WHERE id =?;

-- name: DeleteReleaseGroupRecording :exec
DELETE FROM release_group_recordings 
WHERE id =?;

