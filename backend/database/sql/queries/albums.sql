-- Queries over albums (formerly release_groups).
--
-- The two-copy pattern is gone here too: one query answers both the
-- whole-library and the single-library case.  The `fallback_ac`
-- subquery every album read used to carry -- "if the album has no album
-- artist credit, borrow one from any of its recordings" -- is gone with
-- it, because the album carries its own credit text now.

-- name: UpsertAlbum :one
INSERT INTO albums (name, artist_credit, artist_id, year, cover_art_id)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(name, artist_credit) DO UPDATE SET
    artist_id    = COALESCE(excluded.artist_id, albums.artist_id),
    year         = COALESCE(excluded.year, albums.year),
    cover_art_id = COALESCE(excluded.cover_art_id, albums.cover_art_id)
RETURNING *;

-- name: GetAlbum :one
SELECT * FROM albums WHERE id = ? LIMIT 1;

-- name: SetAlbumMBID :exec
UPDATE albums SET mbid = ? WHERE id = ?;

-- name: SetAlbumOriginalYear :exec
UPDATE albums SET original_year = ? WHERE id = ?;

-- name: SetAlbumCoverArt :exec
UPDATE albums SET cover_art_id = ? WHERE id = ?;

-- name: SetAlbumPendingReleaseMBID :exec
UPDATE albums SET pending_release_mbid = ? WHERE id = ?;

-- name: ResolveAlbumPendingReleaseMBID :exec
-- Clears the pending marker once the release-group MBID it stood in for
-- has been resolved.  Guarded so a real MBID is never overwritten.
UPDATE albums
SET mbid = ?, pending_release_mbid = NULL
WHERE id = ? AND (mbid IS NULL OR mbid = '');

-- name: GetAlbumsWithPendingReleaseMBID :many
SELECT id, pending_release_mbid FROM albums
WHERE pending_release_mbid IS NOT NULL AND pending_release_mbid != ''
  AND (mbid IS NULL OR mbid = '');

-- name: DeleteAlbum :exec
DELETE FROM albums WHERE id = ?;

-- name: DeleteAllAlbums :exec
DELETE FROM albums;

-- name: GetEmptyAlbumIDs :many
-- Albums with no file left behind them.  Under the old schema this was
-- one of three orphan sweeps that had to run by hand and did not;
-- audio_files is the only thing that can leave an album empty now, so
-- this is the whole of it.
SELECT id FROM albums al
WHERE NOT EXISTS (
    SELECT 1 FROM audio_files af WHERE af.album_id = al.id
);

-- name: GetAlbums :many
SELECT
    al.id,
    al.name,
    COALESCE(al.original_year, al.year) AS year,
    COALESCE(al.year, 0) AS release_year,
    al.mbid,
    al.artist_credit AS artist_name,
    CAST(COALESCE(ar.mbid, '') AS TEXT) AS artist_mbid,
    COALESCE(ca.file_path, '') AS cover_art_path
FROM albums al
LEFT JOIN artists ar   ON ar.id = al.artist_id
LEFT JOIN cover_art ca ON ca.id = al.cover_art_id
WHERE EXISTS (
    SELECT 1 FROM audio_files af
    WHERE af.album_id = al.id
      AND af.library_id = COALESCE(NULLIF(CAST(sqlc.arg(library_id) AS INTEGER), 0), af.library_id)
)
ORDER BY al.name;

-- name: GetAlbumsByArtistName :many
SELECT
    al.id,
    al.name,
    COALESCE(al.original_year, al.year) AS year,
    COALESCE(al.year, 0) AS release_year,
    al.mbid,
    al.artist_credit AS artist_name,
    CAST(COALESCE(ar.mbid, '') AS TEXT) AS artist_mbid,
    COALESCE(ca.file_path, '') AS cover_art_path
FROM albums al
LEFT JOIN artists ar   ON ar.id = al.artist_id
LEFT JOIN cover_art ca ON ca.id = al.cover_art_id
WHERE (al.artist_credit = sqlc.arg(artist) OR ar.name = sqlc.arg(artist))
  AND EXISTS (
      SELECT 1 FROM audio_files af
      WHERE af.album_id = al.id
        AND af.library_id = COALESCE(NULLIF(CAST(sqlc.arg(library_id) AS INTEGER), 0), af.library_id)
  )
ORDER BY year, al.name;

-- name: GetAlbumCompleteness :one
-- "Do I have all of this album", answered from the tags on disk.
--
-- The expectation is a **sum over discs**, not one number: totals are
-- declared per disc ("5/12" on disc 2 means 12 tracks on disc 2), so a
-- multi-disc album's expectation is the sum of each disc's declared
-- total.  A disc whose files declared nothing leaves the whole album
-- unknowable rather than being covered by the discs that did -- which is
-- what `known` reports.
--
-- Owned counts DISTINCT track numbers: this app detects duplicates, and
-- counting two files of track 3 twice would report a short album as
-- complete.
SELECT
    -- Distinct (disc, track) pairs: this app detects duplicates, and
    -- counting two files of track 3 twice would report a short album as
    -- complete.  A file with no track number falls back to its own id,
    -- because three untagged files are three tracks, not one.
    CAST(COUNT(DISTINCT CAST(COALESCE(a.disc_number, 1) AS TEXT) || ':' ||
                        COALESCE(CAST(a.track_number AS TEXT), 'f' || a.id)
    ) AS INTEGER) AS owned,
    CAST(COALESCE((
        SELECT SUM(per_disc.total)
        FROM (
            SELECT MAX(b.total_tracks) AS total
            FROM audio_files b
            WHERE b.album_id = sqlc.arg(album_id) AND b.total_tracks IS NOT NULL
            GROUP BY COALESCE(b.disc_number, 1)
        ) per_disc
    ), 0) AS INTEGER) AS expected,
    CAST((
        SELECT COUNT(*) = 0 FROM audio_files c
        WHERE c.album_id = sqlc.arg(album_id) AND c.total_tracks IS NULL
    ) AS INTEGER) AS known
FROM audio_files a
WHERE a.album_id = sqlc.arg(album_id);

-- name: GetAlbumsCompleteness :many
-- The same question as GetAlbumCompleteness, asked of a screenful of
-- albums at once.
--
-- A card grid cannot afford one query per card, and the answer it wants
-- is the one thing a badge cannot guess: an album held 9 tracks of 12
-- must show the count, never a bare tick.  So this is one query for the
-- whole grid, asked only of the cards that have a local album id.
--
-- It is two grouping levels rather than the single-album form's
-- correlated subqueries, because a correlated subquery in the FROM
-- clause is not something SQLite will reliably do -- and because the
-- slice may only be spelled once, or sqlc expands it twice with
-- independently numbered placeholders.
--
-- The per-disc level is where the meaning is, and it is the same
-- meaning as the single-album query.  `owned` counts DISTINCT track
-- numbers within a disc (this app detects duplicates, and counting two
-- files of track 3 twice would report a short album as complete), with
-- a file that declares no track number falling back to its own id
-- because three untagged files are three tracks and not one.
-- `expected` takes each disc's declared total and sums over discs,
-- since a total is declared per disc and a release total written on
-- every file of a two-disc album would double its expectation.  A disc
-- whose files declared nothing contributes a NULL that SUM ignores,
-- and `known` is what says the album is therefore unanswerable.
WITH per_disc AS (
    SELECT
        album_id AS album_id,
        COUNT(DISTINCT COALESCE(CAST(track_number AS TEXT), 'f' || id))
            AS owned_on_disc,
        MAX(total_tracks) AS disc_total,
        SUM(CASE WHEN total_tracks IS NULL THEN 1 ELSE 0 END)
            AS discs_without_a_total
    FROM audio_files
    WHERE album_id IN (sqlc.slice('album_ids'))
    GROUP BY album_id, COALESCE(disc_number, 1)
)
SELECT
    CAST(album_id AS INTEGER) AS album_id,
    CAST(SUM(owned_on_disc) AS INTEGER) AS owned,
    CAST(COALESCE(SUM(disc_total), 0) AS INTEGER) AS expected,
    CAST(SUM(discs_without_a_total) = 0 AS INTEGER) AS known
FROM per_disc
GROUP BY album_id;
