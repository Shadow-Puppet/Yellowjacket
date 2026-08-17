-- The decomposition of a multi-artist credit, from the MusicBrainz
-- dump.  One row per credited artist, in credit order.
--
-- A credit is ordered parts, and the credit *string* is derived from
-- them -- MusicBrainz's own `artist_credit.name` is a cached render and
-- nothing more.  Rendering is a concatenation:
--
--     for each part in position order:
--         emit link(credited_name -> artist_mbid)
--         emit text(join_phrase)
--
-- so the link boundaries are known by construction.  That is the whole
-- reason this table exists, and it is why nothing may reconstruct a
-- credit by *searching* for a name inside a credit string: the stored
-- string may have come from a file's tags while the parts come from the
-- catalog, and measured on a real library those disagree for about one
-- in three multi-artist credits ("Skrillex feat. Swae Lee" tagged
-- against "Skrillex & Swae Lee" upstream).  A search would miss, or
-- match the wrong span.
--
-- `credited_name` is the name *as credited*, which is not the artist's
-- canonical name: MusicBrainz credits "Snoop Dogg" on a track by the
-- artist whose name is "Snoop Doggy Dogg".  It is stored per row rather
-- than joined from an artist table for exactly that reason.
--
-- Only *multi-artist* credits are stored.  A single-artist credit is
-- (name, "") and is already fully described by explore_index's
-- artist_name and artist_mbid; storing those would roughly triple the
-- table to say nothing new.
--
-- Credits are shared: an album's twelve tracks by one artist reference
-- one credit_id.  That is the opposite of the local library's verdict
-- in plan 013, and correctly so -- credit sharing is 1:1 in one
-- person's files and genuinely many-to-one across a 2M-row catalog.
--
-- MBIDs are the same 16 raw bytes explore_index stores, for the same
-- size reason and with the same CHECK, so a stringly write fails at the
-- insert that made it rather than reading back as no rows at all.  See
-- backend/explore/mbid.go.
CREATE TABLE IF NOT EXISTS artist_credit_part (
    credit_id     INTEGER NOT NULL,
    position      INTEGER NOT NULL,
    artist_mbid   BLOB NOT NULL CHECK(length(artist_mbid) = 16),

    -- The name as credited on this release, which may differ from the
    -- artist's canonical name.  Display uses this; navigation uses the
    -- MBID above.
    credited_name TEXT NOT NULL,

    -- The literal connector that follows this part -- " feat. ", " & ",
    -- ", ", or "" on the last part.  Rendered as plain text between two
    -- links.
    join_phrase   TEXT NOT NULL DEFAULT '',

    PRIMARY KEY (credit_id, position)
) WITHOUT ROWID;
