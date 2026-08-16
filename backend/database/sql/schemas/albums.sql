-- One row per album in the library.
--
-- This is `release_groups` renamed, and the rename is the point: a
-- release group is a *MusicBrainz* concept and the catalog still has
-- them (`explore_index.entity_type = 'release_group'`).  What this
-- table holds is the local thing — the album some files on disk belong
-- to — which may or may not have a catalog counterpart.  Calling both
-- of them "release group" is most of why "is this album mine" was a
-- question three different subsystems answered three different ways.
--
-- `artist_credit` is the album artist as tagged ("Various Artists",
-- "A & B"); `artist_id` is the primary artist it resolves to.  Album
-- identity is (name, artist_credit), which is what the old
-- UNIQUE(name, album_artist_credit_id) meant with a join in the way.
CREATE TABLE IF NOT EXISTS albums (
  id                   INTEGER PRIMARY KEY,
  name                 TEXT NOT NULL,
  artist_credit        TEXT NOT NULL DEFAULT '',
  artist_id            INTEGER,
  mbid                 TEXT,
  -- year is the tagged year of the copy on disk; original_year is
  -- MusicBrainz's first-release date when known.  For a 2010 remaster
  -- of a 1973 album: original_year 1973, year 2010.
  year                 INTEGER,
  original_year        INTEGER,
  cover_art_id         INTEGER,
  -- Set when the files carried a release MBID but no release-group
  -- MBID; a background pass resolves it and clears this.
  pending_release_mbid TEXT,

  FOREIGN KEY(cover_art_id) REFERENCES cover_art(id),
  FOREIGN KEY(artist_id)    REFERENCES artists(id),
  UNIQUE(name, artist_credit)
);

CREATE INDEX IF NOT EXISTS idx_albums_artist_id
  ON albums(artist_id);

CREATE INDEX IF NOT EXISTS idx_albums_cover_art_id
  ON albums(cover_art_id);

CREATE INDEX IF NOT EXISTS idx_albums_mbid
  ON albums(mbid) WHERE mbid IS NOT NULL;
