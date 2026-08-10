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

-- ---------------------------------------------------------------------
-- Downloads (one-shot search+grab attempts)
-- ---------------------------------------------------------------------

-- name: CreateDownload :exec
INSERT INTO download_downloads (
    id, library_id, source, request_id, release_mbid, release_group_mbid,
    recording_mbid, artist, album, query, expected, state
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetDownload :one
SELECT id, library_id, source, request_id, release_mbid, release_group_mbid,
       recording_mbid, artist, album, query, expected, state, error,
       created_at, updated_at
FROM download_downloads
WHERE id = ?;

-- name: ListDownloads :many
SELECT id, library_id, source, request_id, release_mbid, release_group_mbid,
       recording_mbid, artist, album, query, expected, state, error,
       created_at, updated_at
FROM download_downloads
ORDER BY created_at DESC
LIMIT ?;

-- name: ListLiveDownloads :many
SELECT id, library_id, source, request_id, release_mbid, release_group_mbid,
       recording_mbid, artist, album, query, expected, state, error,
       created_at, updated_at
FROM download_downloads
WHERE state NOT IN ('complete', 'cancelled', 'failed')
ORDER BY created_at;

-- name: SetDownloadState :exec
UPDATE download_downloads
SET state = ?, error = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: DeleteDownload :exec
DELETE FROM download_downloads
WHERE id = ?;

-- name: DeleteFinishedDownloads :exec
DELETE FROM download_downloads
WHERE state IN ('complete', 'cancelled', 'failed');

-- ---------------------------------------------------------------------
-- Items (transfer records within a download)
-- ---------------------------------------------------------------------

-- name: CreateDownloadItem :exec
INSERT INTO download_items (
    id, download_id, provider_id, transport_id, external_id,
    candidate, state, staging_dir, bytes_total
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetDownloadItem :one
SELECT id, download_id, provider_id, transport_id, external_id, candidate,
       state, staging_dir, bytes_done, bytes_total, imported_paths,
       error, created_at, updated_at
FROM download_items
WHERE id = ?;

-- name: ListDownloadItemsForDownload :many
SELECT id, download_id, provider_id, transport_id, external_id, candidate,
       state, staging_dir, bytes_done, bytes_total, imported_paths,
       error, created_at, updated_at
FROM download_items
WHERE download_id = ?
ORDER BY created_at;

-- name: ListLiveDownloadItems :many
SELECT id, download_id, provider_id, transport_id, external_id, candidate,
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

-- ---------------------------------------------------------------------
-- Requests (durable "I asked for this" records)
-- ---------------------------------------------------------------------

-- name: UpsertDownloadRequest :one
-- Adding something already requested is not an error and must not
-- reset the retry clock, so the conflict path only refreshes display
-- text and un-pauses nothing.  scope and secondary are updated because
-- asking again with a wider scope is a real change of intent.
INSERT INTO download_requests (
    mbid, entity, library_id, artist, title, scope, secondary,
    parent_id, next_try_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(mbid, library_id) DO UPDATE SET
    artist     = CASE WHEN excluded.artist <> '' THEN excluded.artist
                      ELSE download_requests.artist END,
    title      = CASE WHEN excluded.title <> '' THEN excluded.title
                      ELSE download_requests.title END,
    scope      = excluded.scope,
    secondary  = excluded.secondary,
    updated_at = CURRENT_TIMESTAMP
RETURNING id;

-- name: GetDownloadRequest :one
SELECT * FROM download_requests WHERE id = ?;

-- name: GetDownloadRequestByMBID :one
SELECT * FROM download_requests WHERE mbid = ? AND library_id = ?;

-- name: ListDownloadRequests :many
SELECT * FROM download_requests
ORDER BY
    CASE state WHEN 'wanted' THEN 0 WHEN 'paused' THEN 1 ELSE 2 END,
    artist, title;

-- name: ListDownloadRequestsByEntity :many
SELECT * FROM download_requests
WHERE entity = ? AND state = ?
ORDER BY id;

-- name: ListDueDownloadRequests :many
-- Everything the reconciler should act on this pass: wanted, not an
-- artist subscription (those expand rather than download), and either
-- never tried or past its backoff.
SELECT * FROM download_requests
WHERE state = 'wanted'
  AND entity <> 'artist'
  AND (next_try_at IS NULL OR next_try_at <= CURRENT_TIMESTAMP)
ORDER BY attempts, created_at
LIMIT ?;

-- name: ListChildDownloadRequests :many
SELECT * FROM download_requests WHERE parent_id = ? ORDER BY id;

-- name: SetDownloadRequestState :exec
UPDATE download_requests
SET state = ?, last_error = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: RecordDownloadRequestAttempt :exec
UPDATE download_requests
SET attempts      = attempts + 1,
    last_error    = ?,
    last_tried_at = CURRENT_TIMESTAMP,
    next_try_at   = ?,
    updated_at    = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: SatisfyDownloadRequest :exec
UPDATE download_requests
SET state = 'satisfied', last_error = '', next_try_at = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: SetDownloadRequestExternalIDs :exec
UPDATE download_requests
SET external_ids = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: DeleteDownloadRequest :exec
DELETE FROM download_requests WHERE id = ?;

-- name: DeleteSatisfiedDownloadRequests :exec
DELETE FROM download_requests WHERE state = 'satisfied';
