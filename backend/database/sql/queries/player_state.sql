-- name: GetPlayerState :one
SELECT volume, muted, last_track_path, last_position_seconds
FROM player_state WHERE id = 1;

-- name: UpdatePlayerState :exec
UPDATE player_state
SET volume = ?, muted = ?, last_track_path = ?, last_position_seconds = ?
WHERE id = 1;
