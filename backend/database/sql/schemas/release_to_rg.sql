-- Release MBID -> release-group MBID, captured during a dump import.
--
-- It is empty on an ordinary install and looks droppable for that
-- reason: only a local dump build (`indexbuild`) fills it.  The daily
-- incremental refresh reads it to roll per-release listen counts up to
-- the release group they belong to, so an install that has built its
-- own index does need it.
CREATE TABLE IF NOT EXISTS release_to_rg (
  release_mbid TEXT PRIMARY KEY,
  rg_mbid      TEXT NOT NULL
) WITHOUT ROWID;
