-- One row per grab attempt against one candidate.  A download can have
-- several: the first pick stalls, the user picks another, or a
-- search-only provider's candidate is fetched by a separate transport
-- (in which case provider_id is the searcher and transport_id is the
-- fetcher).
--
-- `candidate` is the full ranked Candidate as JSON.  It is stored
-- rather than re-derived because the provider's result set is
-- ephemeral — a Soulseek peer that had the files an hour ago may be
-- offline now, and the item still has to render in the UI and explain
-- why it was chosen.
--
-- external_id holds a delegating manager's own identifier (a Lidarr
-- queue id), which is how polling finds the record again after a
-- restart.


CREATE TABLE IF NOT EXISTS download_items (
    id           TEXT PRIMARY KEY,
    download_id  TEXT    NOT NULL,
    provider_id  INTEGER NOT NULL,
    transport_id INTEGER,
    external_id  TEXT    NOT NULL DEFAULT '',
    candidate    TEXT    NOT NULL DEFAULT '{}',
    state        TEXT    NOT NULL DEFAULT 'queued'
        CHECK(state IN ('searching', 'found', 'queued', 'grabbing',
                        'verifying', 'tagging', 'importing',
                        'complete', 'cancelled', 'failed')),
    staging_dir  TEXT    NOT NULL DEFAULT '',
    bytes_done   INTEGER NOT NULL DEFAULT 0,
    bytes_total  INTEGER NOT NULL DEFAULT 0,
    -- imported_paths is a JSON array of the library paths the files
    -- ended up at, so an import can be undone without guessing.
    imported_paths TEXT  NOT NULL DEFAULT '[]',
    error        TEXT    NOT NULL DEFAULT '',
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(download_id) REFERENCES download_downloads(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_download_items_live
    ON download_items(state)
    WHERE state NOT IN ('complete', 'cancelled', 'failed');

CREATE INDEX IF NOT EXISTS idx_download_items_state
    ON download_items(state);

-- idx_download_items_download is deliberately NOT declared here: on an
-- existing database this table already exists at schema-pass time with
-- its old column still named request_id, so an inline CREATE INDEX on
-- download_id would fail outright. See ensureDownloadIndexes in
-- backend/database/download_rename_migration.go.
