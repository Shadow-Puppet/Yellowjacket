-- One row per "go find me this", from the moment a search is fired
-- until the files are in the library or the attempt is abandoned.  A
-- Download is one attempt: it searches, it grabs, it succeeds or
-- fails, and then it is history.  See download_requests.sql for the
-- durable record a Download may be attached to.
--
-- release_mbid / release_group_mbid are the anchor: a download that
-- carries one can be matched against a known tracklist at import time,
-- which is what makes unattended completion safe.  Free-text downloads
-- (both NULL) are always presented to the user for confirmation.
--
-- `expected` caches the anchor's tracklist as JSON so ranking and
-- import do not have to re-resolve it, and so a download survives the
-- explore index being rebuilt underneath it.


CREATE TABLE IF NOT EXISTS download_downloads (
    id                 TEXT PRIMARY KEY,
    library_id         INTEGER NOT NULL,
    -- source records where the download came from: 'explore-album',
    -- 'explore-artist', 'missing-album', 'wanted', 'manual'.
    source             TEXT    NOT NULL DEFAULT 'manual',
    -- request_id is set when this download is attached to a durable
    -- Request (see download_requests.sql), whether raised by the
    -- reconciler or attached to a manual anchored download.  NULL for
    -- a free-text download with nothing stable to attach to.
    -- Requests are durable and downloads are disposable, so the delete
    -- is a SET NULL rather than a cascade in either direction.
    request_id         INTEGER REFERENCES download_requests(id) ON DELETE SET NULL,
    release_mbid       TEXT,
    release_group_mbid TEXT,
    -- recording_mbid anchors a single-track download raised from a
    -- track-level request.
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

CREATE INDEX IF NOT EXISTS idx_download_downloads_created
    ON download_downloads(created_at DESC);

CREATE INDEX IF NOT EXISTS idx_download_downloads_state
    ON download_downloads(state);
