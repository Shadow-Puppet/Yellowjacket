-- Per-artist record of which catalog enrichment passes have completed,
-- so the owned-artist backfill knows what is left to do.
--
-- It is a table rather than more flag columns on `explore_index` for one
-- reason: the downloaded catalog artifact is merged into that table by
-- column list (see artifactimport.go), so a flag added there is a second
-- place to remember, and forgetting it silently wipes every mark on the
-- next catalog update.  These marks are about *this install's* fetching,
-- which the artifact knows nothing about.
--
-- Each column is a separate fetch with its own failure mode, which is
-- why they are not one boolean: an MB browse failing must not claim the
-- similar-artists fetch, or vice versa.  NULL means "not done" — the
-- timestamp is for debugging and for any future re-fetch policy, not
-- for expiry.  Nothing expires these today.
--
-- `explore_index.discog_fetched` is deliberately NOT duplicated here: it
-- means "this artist's top release groups and recordings are present",
-- which the artifact legitimately answers for artists it covers.

CREATE TABLE IF NOT EXISTS artist_enrichment (
  artist_mbid  TEXT PRIMARY KEY,

  -- The full MusicBrainz browse-by-artist landed: every release group,
  -- with primary and secondary types.  ListenBrainz's top-release-groups
  -- endpoint gives neither the tail nor the types.
  browsed_at   DATETIME,

  -- similar_artist_map has been filled for this artist.
  similar_at   DATETIME
);
