CREATE TABLE IF NOT EXISTS explore_index (
    id                       INTEGER PRIMARY KEY AUTOINCREMENT,
    entity_type              TEXT NOT NULL,
    mbid                     TEXT NOT NULL,
    title                    TEXT NOT NULL,
    artist_name              TEXT NOT NULL,
    artist_mbid              TEXT NOT NULL,
    aliases                  TEXT NOT NULL DEFAULT '',

    -- Popularity signals, derived from the ListenBrainz listens dump.
    popularity               INTEGER NOT NULL DEFAULT 0,
    listener_count           INTEGER NOT NULL DEFAULT 0,

    -- Recording-specific fields.
    duration                 INTEGER NOT NULL DEFAULT 0,
    caa_release_mbid         TEXT NOT NULL DEFAULT '',
    release_name             TEXT NOT NULL DEFAULT '',

    -- Release-group-specific fields.
    primary_type             TEXT NOT NULL DEFAULT '',
    secondary_types          TEXT NOT NULL DEFAULT '',
    release_date             TEXT NOT NULL DEFAULT '',

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

CREATE INDEX IF NOT EXISTS idx_explore_artist_lower
    ON explore_index(LOWER(artist_name))
    WHERE popularity > 0;

CREATE INDEX IF NOT EXISTS idx_explore_caa_release
    ON explore_index(caa_release_mbid)
    WHERE entity_type = 'release_group' AND caa_release_mbid != '';

CREATE INDEX IF NOT EXISTS idx_explore_index_artist_mbid
  ON explore_index(artist_mbid, entity_type, popularity DESC);

CREATE INDEX IF NOT EXISTS idx_explore_index_entity_pop
  ON explore_index(entity_type, popularity DESC);

CREATE INDEX IF NOT EXISTS idx_explore_title_lower
    ON explore_index(LOWER(title))
    WHERE popularity > 0;
