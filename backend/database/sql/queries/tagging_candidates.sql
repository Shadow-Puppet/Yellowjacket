-- name: UpsertTaggingCandidates :exec
INSERT INTO tagging_candidates (group_key, candidates, computed_at)
VALUES (?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(group_key) DO UPDATE SET
  candidates = excluded.candidates,
  computed_at = excluded.computed_at;

-- name: GetTaggingCandidates :one
SELECT candidates FROM tagging_candidates
WHERE group_key = ?
LIMIT 1;

-- name: DeleteTaggingCandidates :exec
DELETE FROM tagging_candidates
WHERE group_key = ?;
