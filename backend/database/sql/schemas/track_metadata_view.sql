CREATE VIEW IF NOT EXISTS track_metadata AS
SELECT
    af.id,
    af.file_path,
    af.length_milliseconds,
    COALESCE(r.name, '') AS title,
    COALESCE(ac.text, '') AS artist_name,
    r.track_number,
    r.disc_number,
    COALESCE(rg.name, '') AS album,
    CAST(COALESCE(
        (SELECT GROUP_CONCAT(g.name, '||')
         FROM recording_genres rg_sub
         JOIN genres g ON rg_sub.genre_id = g.id
         WHERE rg_sub.recording_id = r.id),
        ''
    ) AS TEXT) AS genre,
    COALESCE(r.year, 0) AS year,
    COALESCE(r.composer, '') AS composer,
    COALESCE(ft.extension, '') AS file_type,
    af.sample_rate,
    af.bit_depth,
    af.channels,
    af.bitrate,
    af.file_size,
    af.library_id,
    af.play_count,
    af.last_played
FROM audio_files af
LEFT JOIN recordings r ON af.recording_id = r.id
LEFT JOIN artist_credit ac ON r.artist_credit_id = ac.id
LEFT JOIN (
    SELECT recording_id,
        MIN(release_group_id) AS release_group_id
    FROM release_group_recordings
    GROUP BY recording_id
) rgr ON r.id = rgr.recording_id
LEFT JOIN release_groups rg ON rgr.release_group_id = rg.id
LEFT JOIN file_types ft ON af.file_type_id = ft.id;
