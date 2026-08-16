-- The downloaded MusicBrainz/ListenBrainz catalog.
--
-- MusicBrainz ids are stored as their 16 raw bytes and entity types as
-- small integers, which is a size decision: on a real 2,052,200-row
-- catalog those four columns were 220 MB of a 383 MB table and were
-- carried again in every index keyed on them, and the conversion took
-- the table and its four indexes from 677 MB to 389 MB.  See
-- backend/explore/mbid.go, which is the only place that encoding is
-- known -- everything above it speaks dashed strings and entity names.
--
-- The CHECK constraints are what make a mistake loud.  SQLite does not
-- coerce between TEXT and BLOB, so a query comparing this column
-- against a 36-character string returns no rows rather than an error;
-- a *write* of one fails here instead, at the insert that made it.
CREATE TABLE IF NOT EXISTS explore_index (
    id                       INTEGER PRIMARY KEY AUTOINCREMENT,
    entity_type              INTEGER NOT NULL,
    mbid                     BLOB NOT NULL CHECK(length(mbid) = 16),
    title                    TEXT NOT NULL,
    artist_name              TEXT NOT NULL,
    artist_mbid              BLOB NOT NULL
        CHECK(length(artist_mbid) IN (0, 16)),
    aliases                  TEXT NOT NULL DEFAULT '',

    -- Popularity signals, derived from the ListenBrainz listens dump.
    popularity               INTEGER NOT NULL DEFAULT 0,
    listener_count           INTEGER NOT NULL DEFAULT 0,

    -- Recording-specific fields.
    duration                 INTEGER NOT NULL DEFAULT 0,
    caa_release_mbid         BLOB NOT NULL DEFAULT x''
        CHECK(length(caa_release_mbid) IN (0, 16)),
    release_name             TEXT NOT NULL DEFAULT '',

    -- Release-group-specific fields.
    primary_type             TEXT NOT NULL DEFAULT '',
    secondary_types          TEXT NOT NULL DEFAULT '',
    release_date             TEXT NOT NULL DEFAULT '',

    -- How many tracks the release group's canonical release has, so
    -- "do I have all of this" is answerable offline for an album the
    -- library holds no tags for.  Zero means the catalog does not say,
    -- which is the same third state the local answer has -- and is what
    -- every row carries until a central dump build fills it.
    total_tracks             INTEGER NOT NULL DEFAULT 0,

    -- Artist-specific fields.
    artist_type              TEXT NOT NULL DEFAULT '',
    country                  TEXT NOT NULL DEFAULT '',
    disambiguation           TEXT NOT NULL DEFAULT '',
    sort_name                TEXT NOT NULL DEFAULT '',

    -- Personalization flags.
    in_library               INTEGER NOT NULL DEFAULT 0,
    is_similar               INTEGER NOT NULL DEFAULT 0,

    -- Cross-reference to local library tables.  NULL when the
    -- entity has no corresponding row in the library.
    local_artist_id          INTEGER,
    local_release_group_id   INTEGER,
    local_recording_id       INTEGER,

    -- Set once an artist's full discography (release groups +
    -- recordings) has been fetched, so EnsureArtistDiscography can
    -- skip artists the catalog already covers.
    discog_fetched           INTEGER NOT NULL DEFAULT 0,


    UNIQUE(mbid)
  );

-- The exact-match tier's two indexes.
--
-- Their predicate is the champion set - the popular rows plus whatever
-- the user owns - and matching it to `ExactMatches`' own WHERE clause is
-- what makes them small.  They used to say `popularity > 0`, which on a
-- real 2,052,200-row catalog covered 2,046,645 of them: a full index
-- wearing a partial index's clothes, 101 MB for the pair.  Narrowed to
-- the set the tier can actually return, they are 3 MB and the query
-- plan is unchanged (measured, on that catalog).
CREATE INDEX IF NOT EXISTS idx_explore_artist_lower
    ON explore_index(LOWER(artist_name))
    WHERE popularity >= 10000 OR in_library = 1;

CREATE INDEX IF NOT EXISTS idx_explore_caa_release
    ON explore_index(caa_release_mbid)
    WHERE entity_type = 2 AND caa_release_mbid != x'';

CREATE INDEX IF NOT EXISTS idx_explore_index_artist_mbid
  ON explore_index(artist_mbid, entity_type, popularity DESC);

CREATE INDEX IF NOT EXISTS idx_explore_index_entity_pop
  ON explore_index(entity_type, popularity DESC);

CREATE INDEX IF NOT EXISTS idx_explore_title_lower
    ON explore_index(LOWER(title))
    WHERE popularity >= 10000 OR in_library = 1;
