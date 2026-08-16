-- One row per folder the user has added as a music library.
--
-- Everything else keyed by library_id means "which of these folders did
-- this come from"; a library_id of 0 in a query means "all of them".
--
-- autotag_warning_acked records that the user has been told what
-- autotagging will do to the files in this folder, which is a decision
-- they made and not something a rescan can rediscover.

CREATE TABLE IF NOT EXISTS libraries (
    id                     INTEGER PRIMARY KEY,
    name                   TEXT NOT NULL,
    path                   TEXT NOT NULL UNIQUE,
    created_at             DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    autotag_warning_acked  INTEGER NOT NULL DEFAULT 0
);
