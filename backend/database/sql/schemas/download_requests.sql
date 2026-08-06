-- One row per "go find me this", from the moment the user asks until
-- the files are in the library or the attempt is abandoned.
--
-- release_mbid / release_group_mbid are the anchor: a request that
-- carries one can be matched against a known tracklist at import time,
-- which is what makes unattended completion safe.  Free-text requests
-- (both NULL) are always presented to the user for confirmation.
--
-- `expected` caches the anchor's tracklist as JSON so ranking and
-- import do not have to re-resolve it, and so a request survives the
-- explore index being rebuilt underneath it.


CREATE TABLE IF NOT EXISTS download_requests (
    id                 TEXT PRIMARY KEY,
    library_id         INTEGER NOT NULL,
    -- source records where the request came from: 'explore-album',
    -- 'explore-artist', 'missing-album', 'wanted', 'manual'.
    source             TEXT    NOT NULL DEFAULT 'manual',
    -- want_id is set when the reconciler raised this request from the
    -- wanted list, so the outcome can be written back to the want.
    -- NULL for one-off requests the user started by hand.  Requests are
    -- disposable and wants are not, so the delete is a SET NULL rather
    -- than a cascade in either direction.
    want_id            INTEGER REFERENCES download_wants(id) ON DELETE SET NULL,
    release_mbid       TEXT,
    release_group_mbid TEXT,
    -- recording_mbid anchors a single-track request raised from a
    -- track-level want.
    recording_mbid     TEXT,
    artist             TEXT    NOT NULL DEFAULT '',
    album              TEXT    NOT NULL DEFAULT '',
    query              TEXT    NOT NULL DEFAULT '',
    expected           TEXT    NOT NULL DEFAULT '[]',
    state              TEXT    NOT NULL DEFAULT 'searching'
        CHECK(state IN ('searching', 'found', 'queued', 'grabbing',
                        'verifying', 'tagging', 'importing',
                        'complete', 'cancelled', 'failed')),
    error              TEXT    NOT NULL DEFAULT '',
    created_at         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(library_id) REFERENCES libraries(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_download_requests_created
    ON download_requests(created_at DESC);

CREATE INDEX IF NOT EXISTS idx_download_requests_state
    ON download_requests(state);
