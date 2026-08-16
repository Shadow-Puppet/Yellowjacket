-- The one definition of "a track, with everything a list needs".
--
-- A view is a definition, not data, so it is dropped and recreated on
-- every open rather than carrying a migration alongside it: CREATE VIEW
-- IF NOT EXISTS silently keeps an older database on the old definition,
-- and a migration file restating it would be the second description of
-- the schema the migration rules exist to prevent.
--
-- This projection used to exist **nine times** — four copies in
-- audio_files.sql, two in playlists.sql, two in genres.sql, one in
-- queue.sql — plus this view, which only the raw-SQL search paths used.
-- They had already drifted: this view preferred the album's
-- original_year for `year` and GetAllTracksWithFullMetadata used the
-- track's own, so the same library reported different years on
-- different screens.  Every query that wants a track row now selects
-- from here, which is also why there is one row type and one mapper on
-- the Go side instead of nine and a twenty-two-argument function.
DROP VIEW IF EXISTS track_metadata;

CREATE VIEW track_metadata AS
    SELECT
        af.id,
        af.file_path,
        af.length_milliseconds,
        af.title,
        af.artist_credit AS artist_name,
        af.track_number,
        af.disc_number,
        COALESCE(al.name, '') AS album,
        CAST(COALESCE(
            (SELECT GROUP_CONCAT(g.name, '||')
             FROM file_genres fg
             JOIN genres g ON g.id = fg.genre_id
             WHERE fg.audio_file_id = af.id),
            ''
        ) AS TEXT) AS genre,
        COALESCE(al.original_year, al.year, af.year, 0) AS year,
        COALESCE(al.year, af.year, 0) AS release_year,
        af.composer,
        COALESCE(ft.extension, '') AS file_type,
        af.sample_rate,
        af.bit_depth,
        af.channels,
        af.bitrate,
        af.file_size,
        af.library_id,
        af.play_count,
        af.last_played,
        COALESCE(ca.file_path, '') AS cover_art_path,
        COALESCE(ar.mbid, '') AS artist_mbid,
        COALESCE(al.mbid, '') AS release_group_mbid,
        COALESCE(af.recording_mbid, '') AS recording_mbid,
        af.album_id,
        af.artist_id
    FROM audio_files af
    LEFT JOIN albums al     ON al.id = af.album_id
    LEFT JOIN artists ar    ON ar.id = af.artist_id
    LEFT JOIN cover_art ca  ON ca.id = al.cover_art_id
    LEFT JOIN file_types ft ON ft.id = af.file_type_id;
