-- Queries behind the home page's "start listening" shelves.
--
-- Every one of these returns album ids and nothing else.  The display
-- columns (cover art, artist credit, year) already have exactly one
-- correct expression of them, in GetAlbums, and a second
-- copy per shelf would be six more places for that to drift.  The home
-- service joins the ids back to that one album list in Go.

-- name: HomeRecentlyPlayedAlbums :many
-- Albums with the most recent play, newest first.
SELECT rg.id AS album_id
FROM albums rg
JOIN audio_files af ON af.album_id = rg.id
WHERE af.last_played IS NOT NULL
GROUP BY rg.id
ORDER BY MAX(af.last_played) DESC
LIMIT ?;

-- name: HomeRecentlyAddedAlbums :many
-- Newest albums.  audio_files has no import timestamp, so the row id
-- stands in for one: it is monotonic and assigned at import, which is
-- the same ordering an added_at column would give.
SELECT rg.id AS album_id
FROM albums rg
JOIN audio_files af ON af.album_id = rg.id
GROUP BY rg.id
ORDER BY MAX(af.id) DESC
LIMIT ?;

-- name: HomeMostPlayedAlbums :many
-- Albums by total plays across their tracks.
SELECT rg.id AS album_id
FROM albums rg
JOIN audio_files af ON af.album_id = rg.id
GROUP BY rg.id
HAVING SUM(af.play_count) > 0
ORDER BY SUM(af.play_count) DESC
LIMIT ?;

-- name: HomeUnplayedAlbums :many
-- Albums nothing on has ever been played, sampled at random so the
-- shelf is a different suggestion each time rather than the same
-- alphabetical head of the list forever.
SELECT rg.id AS album_id
FROM albums rg
JOIN audio_files af ON af.album_id = rg.id
GROUP BY rg.id
HAVING SUM(af.play_count) = 0
ORDER BY RANDOM()
LIMIT ?;

-- name: HomeStaleAlbums :many
-- Played before, but not for a long while.
SELECT rg.id AS album_id
FROM albums rg
JOIN audio_files af ON af.album_id = rg.id
WHERE af.last_played IS NOT NULL
GROUP BY rg.id
HAVING MAX(af.last_played) < datetime('now', ?)
ORDER BY MAX(af.last_played) ASC
LIMIT ?;

-- name: HomeRandomAlbums :many
SELECT rg.id AS album_id
FROM albums rg
JOIN audio_files af ON af.album_id = rg.id
GROUP BY rg.id
ORDER BY RANDOM()
LIMIT ?;

-- name: HomeAlbumsByGenre :many
-- A random sample of albums carrying a genre, so the same genre shelf
-- is not the same ten albums every time the page opens.
SELECT rg.id AS album_id
FROM albums rg
JOIN audio_files af ON af.album_id = rg.id
JOIN file_genres fg ON fg.audio_file_id = af.id
JOIN genres g ON g.id = fg.genre_id
WHERE g.name = ?
GROUP BY rg.id
ORDER BY RANDOM()
LIMIT ?;

-- name: HomeTopGenres :many
-- Genres ranked by how much of the library carries them, restricted to
-- ones with at least a few albums: a shelf built from a genre one
-- album carries is a shelf about that one album.
SELECT
    g.name AS genre,
    COUNT(DISTINCT af.album_id) AS album_count
FROM genres g
JOIN file_genres fg ON fg.genre_id = g.id
JOIN audio_files af ON af.id = fg.audio_file_id
GROUP BY g.id
HAVING album_count >= 3
ORDER BY album_count DESC
LIMIT ?;

-- name: HomeTopArtists :many
-- Artists by total plays, as the album-artist credit text the album
-- list already displays.
SELECT
    rg.artist_credit AS artist_name,
    SUM(af.play_count) AS plays
FROM albums rg
JOIN audio_files af ON af.album_id = rg.id
WHERE rg.artist_credit <> ''
GROUP BY rg.artist_credit
HAVING plays > 0
ORDER BY plays DESC
LIMIT ?;
