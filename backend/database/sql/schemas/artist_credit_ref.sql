-- Which credit a catalog entity is credited to.  One row per recording
-- or release group whose credit names more than one artist.
--
-- This is a table rather than an `explore_index.artist_credit_id`
-- column, and that is a deliberate consequence of how this app applies
-- its schema.  `applySchema` is CREATE ... IF NOT EXISTS and there is
-- no migration chain (plan 013), so a *column* added to an existing
-- table never reaches a database that already has it -- while a new
-- *table* is created on every install, old or new, for free.
-- explore_index is the one table nobody can afford to drop and rebuild
-- on a schema change: it is the artifact users download rather than
-- derive.
--
-- Only multi-artist credits are referenced here, matching
-- artist_credit_part.  An entity with no row is credited to exactly one
-- artist, which explore_index's own artist_name and artist_mbid already
-- describe -- so absence is the common case and means "nothing to
-- decompose", not "unknown".
--
-- `credit_id` is opaque and is only meaningful against the
-- artist_credit_part rows built or imported alongside it.  The two are
-- always written together; nothing persists a credit_id anywhere else.
-- The local library stores resolved parts, never this id.
CREATE TABLE IF NOT EXISTS artist_credit_ref (
    mbid      BLOB NOT NULL PRIMARY KEY CHECK(length(mbid) = 16),
    credit_id INTEGER NOT NULL
) WITHOUT ROWID;

CREATE INDEX IF NOT EXISTS idx_artist_credit_ref_credit
    ON artist_credit_ref(credit_id);
