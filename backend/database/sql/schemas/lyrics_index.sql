-- Contentless FTS5 index over recording lyrics, enabling
-- "search by a lyric fragment → find the song".  The rowid is
-- recordings.id.  Only recordings with non-empty lyrics are indexed.
--
-- content='' means the lyric text itself is NOT stored a second time
-- (it already lives in recordings.lyrics); the index keeps only the
-- tokenised inverted index, so it stays compact even for large
-- libraries.  contentless_delete=1 lets us delete/reinsert a single
-- row when a track's lyrics change (scan update or LRCLIB backfill).


CREATE VIRTUAL TABLE IF NOT EXISTS lyrics_index USING fts5(
    lyrics,
    content='',
    contentless_delete=1,
    tokenize='unicode61 remove_diacritics 2'
);
