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


CREATE TABLE IF NOT EXISTS libraries (
    id                     INTEGER PRIMARY KEY,
    name                   TEXT NOT NULL,
    path                   TEXT NOT NULL UNIQUE,
    created_at             DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    autotag_warning_acked  INTEGER NOT NULL DEFAULT 0
);
