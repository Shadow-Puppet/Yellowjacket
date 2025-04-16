-- name: CreateAudioFile :one
INSERT INTO audio_files (file_path, length_milliseconds, file_type_id, recording_id) VALUES (?, ?, ?, ?)
RETURNING *;

-- name: GetAudioFile :one
SELECT * FROM audio_files 
WHERE id = ? LIMIT 1;

-- name: UpdateAudioFile :exec
UPDATE audio_files 
SET file_path = ?, length_milliseconds = ?, file_type_id = ?, recording_id = ?
WHERE id =?;

-- name: DeleteAudioFile :exec
DELETE FROM audio_files 
WHERE id =?;

