-- name: ListDownloadProviders :many
SELECT id, kind, name, enabled, priority, settings, created_at
FROM download_providers
ORDER BY priority DESC, name;

-- name: GetDownloadProvider :one
SELECT id, kind, name, enabled, priority, settings, created_at
FROM download_providers
WHERE id = ?;

-- name: CreateDownloadProvider :one
INSERT INTO download_providers (kind, name, enabled, priority, settings)
VALUES (?, ?, ?, ?, ?)
RETURNING id;

-- name: UpdateDownloadProvider :exec
UPDATE download_providers
SET name = ?, enabled = ?, priority = ?, settings = ?
WHERE id = ?;

-- name: DeleteDownloadProvider :exec
DELETE FROM download_providers
WHERE id = ?;

-- name: CreateDownloadRequest :exec
INSERT INTO download_requests (
    id, library_id, source, want_id, release_mbid, release_group_mbid,
    recording_mbid, artist, album, query, expected, state
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetDownloadRequest :one
SELECT id, library_id, source, want_id, release_mbid, release_group_mbid,
       recording_mbid, artist, album, query, expected, state, error,
       created_at, updated_at
FROM download_requests
WHERE id = ?;

-- name: ListDownloadRequests :many
SELECT id, library_id, source, want_id, release_mbid, release_group_mbid,
       recording_mbid, artist, album, query, expected, state, error,
       created_at, updated_at
FROM download_requests
ORDER BY created_at DESC
LIMIT ?;

-- name: ListLiveDownloadRequests :many
SELECT id, library_id, source, want_id, release_mbid, release_group_mbid,
       recording_mbid, artist, album, query, expected, state, error,
       created_at, updated_at
FROM download_requests
WHERE state NOT IN ('complete', 'cancelled', 'failed')
ORDER BY created_at;

-- name: SetDownloadRequestState :exec
UPDATE download_requests
SET state = ?, error = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: DeleteDownloadRequest :exec
DELETE FROM download_requests
WHERE id = ?;

-- name: CreateDownloadItem :exec
INSERT INTO download_items (
    id, request_id, provider_id, transport_id, external_id,
    candidate, state, staging_dir, bytes_total
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetDownloadItem :one
SELECT id, request_id, provider_id, transport_id, external_id, candidate,
       state, staging_dir, bytes_done, bytes_total, imported_paths,
       error, created_at, updated_at
FROM download_items
WHERE id = ?;

-- name: ListDownloadItemsForRequest :many
SELECT id, request_id, provider_id, transport_id, external_id, candidate,
       state, staging_dir, bytes_done, bytes_total, imported_paths,
       error, created_at, updated_at
FROM download_items
WHERE request_id = ?
ORDER BY created_at;

-- name: ListLiveDownloadItems :many
SELECT id, request_id, provider_id, transport_id, external_id, candidate,
       state, staging_dir, bytes_done, bytes_total, imported_paths,
       error, created_at, updated_at
FROM download_items
WHERE state NOT IN ('complete', 'cancelled', 'failed')
ORDER BY created_at;

-- name: SetDownloadItemState :exec
UPDATE download_items
SET state = ?, error = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: SetDownloadItemProgress :exec
UPDATE download_items
SET bytes_done = ?, bytes_total = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: SetDownloadItemExternalID :exec
UPDATE download_items
SET external_id = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: SetDownloadItemImported :exec
UPDATE download_items
SET imported_paths = ?, state = 'complete', error = '',
    updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: DeleteFinishedDownloadRequests :exec
DELETE FROM download_requests
WHERE state IN ('complete', 'cancelled', 'failed');

-- ---------------------------------------------------------------------
-- Wants
-- ---------------------------------------------------------------------

-- name: UpsertDownloadWant :one
-- Adding something already wanted is not an error and must not reset
-- the retry clock, so the conflict path only refreshes display text and
-- un-pauses nothing.  scope and secondary are updated because asking
-- again with a wider scope is a real change of intent.
INSERT INTO download_wants (
    mbid, entity, library_id, artist, title, scope, secondary,
    parent_id, next_try_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(mbid, library_id) DO UPDATE SET
    artist     = CASE WHEN excluded.artist <> '' THEN excluded.artist
                      ELSE download_wants.artist END,
    title      = CASE WHEN excluded.title <> '' THEN excluded.title
                      ELSE download_wants.title END,
    scope      = excluded.scope,
    secondary  = excluded.secondary,
    updated_at = CURRENT_TIMESTAMP
RETURNING id;

-- name: GetDownloadWant :one
SELECT * FROM download_wants WHERE id = ?;

-- name: GetDownloadWantByMBID :one
SELECT * FROM download_wants WHERE mbid = ? AND library_id = ?;

-- name: ListDownloadWants :many
SELECT * FROM download_wants
ORDER BY
    CASE state WHEN 'wanted' THEN 0 WHEN 'paused' THEN 1 ELSE 2 END,
    artist, title;

-- name: ListDownloadWantsByEntity :many
SELECT * FROM download_wants
WHERE entity = ? AND state = ?
ORDER BY id;

-- name: ListDueDownloadWants :many
-- Everything the reconciler should act on this pass: wanted, not an
-- artist subscription (those expand rather than download), and either
-- never tried or past its backoff.
SELECT * FROM download_wants
WHERE state = 'wanted'
  AND entity <> 'artist'
  AND (next_try_at IS NULL OR next_try_at <= CURRENT_TIMESTAMP)
ORDER BY attempts, created_at
LIMIT ?;

-- name: ListChildDownloadWants :many
SELECT * FROM download_wants WHERE parent_id = ? ORDER BY id;

-- name: SetDownloadWantState :exec
UPDATE download_wants
SET state = ?, last_error = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: RecordDownloadWantAttempt :exec
UPDATE download_wants
SET attempts      = attempts + 1,
    last_error    = ?,
    last_tried_at = CURRENT_TIMESTAMP,
    next_try_at   = ?,
    updated_at    = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: SatisfyDownloadWant :exec
UPDATE download_wants
SET state = 'satisfied', last_error = '', next_try_at = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: SetDownloadWantExternalIDs :exec
UPDATE download_wants
SET external_ids = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: DeleteDownloadWant :exec
DELETE FROM download_wants WHERE id = ?;

-- name: DeleteSatisfiedDownloadWants :exec
DELETE FROM download_wants WHERE state = 'satisfied';
