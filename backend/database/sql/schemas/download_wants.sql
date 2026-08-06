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


CREATE TABLE IF NOT EXISTS download_wants (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    mbid       TEXT    NOT NULL,
    entity     TEXT    NOT NULL
        CHECK(entity IN ('artist', 'release-group', 'release', 'recording')),
    library_id INTEGER NOT NULL,

    -- Display text, cached so the wanted list renders without touching
    -- the explore index.  Neither is authoritative; the MBID is.
    artist     TEXT    NOT NULL DEFAULT '',
    title      TEXT    NOT NULL DEFAULT '',

    scope      TEXT    NOT NULL DEFAULT 'future'
        CHECK(scope IN ('future', 'all')),

    -- secondary controls whether an artist want's expansion includes
    -- compilations, live albums and remixes.  Off by default: someone
    -- subscribing to an artist wants the albums, not six versions of
    -- the same greatest-hits package.
    secondary  INTEGER NOT NULL DEFAULT 0,

    state      TEXT    NOT NULL DEFAULT 'wanted'
        CHECK(state IN ('wanted', 'satisfied', 'paused')),

    -- parent_id links a want the reconciler derived from an artist
    -- want.  Deleting the artist takes its derived children with it,
    -- but children the user pinned themselves have no parent and stay.
    parent_id  INTEGER,

    -- Retry bookkeeping.  attempts drives the backoff; last_error is
    -- the most recent reason it did not work out, which for a wanted
    -- item is information rather than a failure.
    attempts     INTEGER  NOT NULL DEFAULT 0,
    last_error   TEXT     NOT NULL DEFAULT '',
    last_tried_at DATETIME,
    next_try_at  DATETIME,

    -- external_ids maps provider row ID to that provider's own
    -- identifier for this want, for clients that keep their own
    -- persistent list (a Lidarr artist ID).  JSON object.
    external_ids TEXT    NOT NULL DEFAULT '{}',

    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,

    -- One want per thing per library.  Asking twice is not two wants,
    -- and this is what lets an artist expansion re-run every reconcile
    -- without accumulating duplicates.
    UNIQUE(mbid, library_id),

    FOREIGN KEY(library_id) REFERENCES libraries(id) ON DELETE CASCADE,
    FOREIGN KEY(parent_id) REFERENCES download_wants(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_download_wants_due
    ON download_wants(next_try_at)
    WHERE state = 'wanted';

CREATE INDEX IF NOT EXISTS idx_download_wants_entity
    ON download_wants(entity, state);

CREATE INDEX IF NOT EXISTS idx_download_wants_parent
    ON download_wants(parent_id);
