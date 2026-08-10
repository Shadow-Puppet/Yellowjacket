-- A Request is a persistent "I want this", stored as a MusicBrainz ID
-- and almost nothing else.  It outlives every download attempt made on
-- its behalf: nothing being findable today is the normal case for
-- obscure music, and the correct response is to try again next week,
-- not to show the user a failed row they have to remember to retry.
--
-- Because a Request is only an MBID, it stays true when everything
-- around it changes: the explore index is rebuilt, a provider is
-- swapped out, the release the user originally saw is superseded by a
-- remaster.  The display fields are a cache for the list view and are
-- never consulted for matching.


CREATE TABLE IF NOT EXISTS download_requests (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    mbid       TEXT    NOT NULL,
    entity     TEXT    NOT NULL
        CHECK(entity IN ('artist', 'release-group', 'release', 'recording')),
    library_id INTEGER NOT NULL,

    -- Display text, cached so the list renders without touching the
    -- explore index.  Neither is authoritative; the MBID is.
    artist     TEXT    NOT NULL DEFAULT '',
    title      TEXT    NOT NULL DEFAULT '',

    scope      TEXT    NOT NULL DEFAULT 'future'
        CHECK(scope IN ('future', 'all')),

    -- secondary controls whether an artist request's expansion includes
    -- compilations, live albums and remixes.  Off by default: someone
    -- subscribing to an artist wants the albums, not six versions of
    -- the same greatest-hits package.
    secondary  INTEGER NOT NULL DEFAULT 0,

    state      TEXT    NOT NULL DEFAULT 'wanted'
        CHECK(state IN ('wanted', 'satisfied', 'paused')),

    -- parent_id links a request the reconciler derived from an artist
    -- request.  Deleting the artist takes its derived children with
    -- it, but children the user pinned themselves have no parent and
    -- stay.
    parent_id  INTEGER,

    -- Retry bookkeeping.  attempts drives the backoff; last_error is
    -- the most recent reason it did not work out, which for a request
    -- is information rather than a failure.
    attempts     INTEGER  NOT NULL DEFAULT 0,
    last_error   TEXT     NOT NULL DEFAULT '',
    last_tried_at DATETIME,
    next_try_at  DATETIME,

    -- external_ids maps provider row ID to that provider's own
    -- identifier for this request, for clients that keep their own
    -- persistent list (a Lidarr artist ID).  JSON object.
    external_ids TEXT    NOT NULL DEFAULT '{}',

    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,

    -- One request per thing per library.  Asking twice is not two
    -- requests, and this is what lets an artist expansion re-run every
    -- reconcile without accumulating duplicates.
    UNIQUE(mbid, library_id),

    FOREIGN KEY(library_id) REFERENCES libraries(id) ON DELETE CASCADE,
    FOREIGN KEY(parent_id) REFERENCES download_requests(id) ON DELETE CASCADE
);

-- idx_download_requests_{due,entity,parent} are deliberately NOT
-- declared here. This table name is reused from the old one-shot
-- attempt table (also called download_requests before the Want/Request
-- rename), so on an existing database this CREATE TABLE is a no-op
-- against a table that, at schema-pass time, is still shaped like the
-- OLD attempts table and lacks these columns entirely — an inline
-- CREATE INDEX here would fail outright rather than just no-op. See
-- migrateDownloadRename/ensureDownloadIndexes in
-- backend/database/download_rename_migration.go, which create these
-- once the rename has actually happened (or immediately, on a fresh
-- database where the columns exist from the start).
