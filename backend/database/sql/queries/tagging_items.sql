-- name: UpsertTaggingItemOnTrackAdd :exec
INSERT INTO tagging_items (
  group_key, library_id, track_count,
  album_name, album_artist, disc_number, status
)
VALUES (?, ?, 1, ?, ?, ?, 'pending')
ON CONFLICT(group_key) DO UPDATE SET
  track_count = tagging_items.track_count + 1,
  album_name = CASE
    WHEN tagging_items.album_name = '' THEN excluded.album_name
    ELSE tagging_items.album_name
  END,
  -- Tracks real consensus, not first-write-wins: stays set only
  -- while every track that has contributed a non-empty value agrees.
  -- A later track with a *different* non-empty value clears it back
  -- to '' and latches album_artist_conflict, since a single
  -- disagreeing tag means the folder no longer has one authoritative
  -- album-artist -- IsMixedBag (backend/autotag) treats a non-empty
  -- value here as trusted, so leaving a stale first-seen value in
  -- place would let one track's tag silently blind mixed-bag
  -- detection for the whole folder. The latch (rather than just
  -- clearing the text column) stops a later track from coincidentally
  -- repeating an already-disputed value and resurrecting trust in it.
  album_artist_conflict = CASE
    WHEN tagging_items.album_artist_conflict = 1 THEN 1
    WHEN tagging_items.album_artist != '' AND excluded.album_artist != ''
      AND tagging_items.album_artist != excluded.album_artist THEN 1
    ELSE 0
  END,
  album_artist = CASE
    WHEN tagging_items.album_artist_conflict = 1 THEN ''
    WHEN tagging_items.album_artist != '' AND excluded.album_artist != ''
      AND tagging_items.album_artist != excluded.album_artist THEN ''
    WHEN tagging_items.album_artist = '' THEN excluded.album_artist
    ELSE tagging_items.album_artist
  END;

-- name: DecrementTaggingItemTrackCount :exec
UPDATE tagging_items
SET track_count = track_count - 1
WHERE group_key = ?;

-- name: DeleteTaggingItemIfEmpty :exec
DELETE FROM tagging_items
WHERE group_key = ? AND track_count <= 0;

-- name: PruneOrphanedTaggingItems :exec
-- Self-healing sweep for rows whose track_count bookkeeping (scan
-- orphan cleanup, maybeRebindTaggingGroup, SplitMixedFolder) never
-- ran or drifted: a cancelled scan, a library move/rename the
-- SoftScanAllLibraries disk-count/mtime heuristic did not catch, or
-- a decrement that landed without its paired delete. Rather than
-- trust track_count, this checks the ground truth directly: any
-- group_key no audio_files row still points at is gone, and its
-- tagging_items row (and cascaded tagging_candidates) should be too.
-- Cheap: one indexed (idx_audio_files_group_key) existence check per
-- row. Called opportunistically wherever the pending list is read,
-- so stale entries cannot linger indefinitely between full rescans.
DELETE FROM tagging_items
WHERE NOT EXISTS (
  SELECT 1 FROM audio_files af WHERE af.group_key = tagging_items.group_key
);

-- name: MarkTaggingItemSynthetic :exec
-- Stamps a group as carved out of parent_group_key by
-- SplitMixedFolder.  Idempotent: safe to call every time a track
-- is migrated into the synthetic group, not just on first creation.
UPDATE tagging_items
SET synthetic = 1,
    parent_group_key = ?
WHERE group_key = ?;

-- name: GetTaggingItem :one
SELECT * FROM tagging_items
WHERE group_key = ?
LIMIT 1;

-- name: ListLikelyMixedBagGroupKeys :many
-- Cheap, whole-library triage pass for autotag.IsMixedBag: one
-- grouped scan over audio_files (indexed on group_key) rather than
-- hydrating every group's full track list in Go.  LOWER/TRIM is an
-- approximation of autotag.Normalize (no unicode fold, no qualifier
-- stripping) so this can flag a false positive Normalize would
-- clear, or miss a true one Normalize would catch.  Treat it as a
-- triage filter for which groups are worth a real autotag.
-- IsMixedBag check, or a badge at minimum, not the final word.
SELECT ti.group_key
FROM tagging_items ti
JOIN audio_files af ON af.group_key = ti.group_key
LEFT JOIN albums rg ON rg.id = af.album_id
WHERE ti.synthetic = 0
  AND ti.track_count >= 4
  AND (
    ti.album_artist = ''
    OR LOWER(TRIM(ti.album_artist)) IN ('various artists', 'various', 'va', 'v.a.', 'v a', 'unknown')
  )
GROUP BY ti.group_key
HAVING COUNT(DISTINCT CASE WHEN af.artist_credit != '' THEN LOWER(TRIM(af.artist_credit)) END) > 1
   AND COUNT(DISTINCT CASE WHEN rg.name != '' THEN LOWER(TRIM(rg.name)) END) > 1;

-- name: CountPendingTaggingItems :one
-- "Needs tagging" is a question about the files, not about the row:
-- every scanned folder gets a tagging_items row (see
-- UpsertTaggingItemOnTrackAdd), including one whose files all arrived
-- carrying a recording MBID.  Without the EXISTS a fully MB-tagged
-- library reports its entire album count as pending work.  See the
-- same predicate on the three list queries below.
SELECT COUNT(*) FROM tagging_items ti
WHERE ti.status = 'pending'
  AND (CAST(@library_id AS INTEGER) = 0 OR ti.library_id = @library_id)
  AND EXISTS (
    SELECT 1 FROM audio_files af
    WHERE af.group_key = ti.group_key AND af.tag_status = 'untagged'
  );

-- name: ListPendingTaggingItemsAlphabetical :many
SELECT
  ti.group_key,
  ti.library_id,
  COALESCE(lb.name, '') AS library_name,
  ti.track_count,
  ti.album_name,
  ti.album_artist,
  ti.disc_number,
  ti.best_match_release_mbid,
  ti.score,
  ti.last_checked_at,
  ti.status,
  ti.created_at
FROM tagging_items ti
LEFT JOIN libraries lb ON lb.id = ti.library_id
WHERE (CAST(@library_id AS INTEGER) = 0 OR ti.library_id = @library_id)
  AND (CAST(@status_filter AS TEXT) = 'all' OR ti.status = @status_filter)
  AND ti.cleared_at IS NULL
  -- Actionable rows must have something to act on: see
  -- CountPendingTaggingItems.  Reviewed rows (confirmed/skipped) are
  -- exempt because they are history, not work -- an applied folder is
  -- fully tagged by definition and would otherwise vanish from the
  -- sidebar's Completed section the instant it succeeded.
  AND (
    ti.status IN ('confirmed', 'skipped')
    OR EXISTS (
      SELECT 1 FROM audio_files af
      WHERE af.group_key = ti.group_key AND af.tag_status = 'untagged'
    )
  )
ORDER BY LOWER(ti.album_artist), LOWER(ti.album_name), ti.disc_number
LIMIT @row_limit OFFSET @row_offset;

-- name: ListPendingTaggingItemsByScore :many
SELECT
  ti.group_key,
  ti.library_id,
  COALESCE(lb.name, '') AS library_name,
  COALESCE(lb.path, '') AS library_path,
  CAST(COALESCE((SELECT af.file_path FROM audio_files af WHERE af.group_key = ti.group_key AND af.group_key != '' LIMIT 1), '') AS TEXT) AS sample_file_path,
  ti.track_count,
  ti.album_name,
  ti.album_artist,
  ti.disc_number,
  ti.best_match_release_mbid,
  ti.score,
  ti.last_checked_at,
  ti.status,
  ti.created_at,
  ti.synthetic
FROM tagging_items ti
LEFT JOIN libraries lb ON lb.id = ti.library_id
WHERE (CAST(@library_id AS INTEGER) = 0 OR ti.library_id = @library_id)
  AND (CAST(@status_filter AS TEXT) = 'all' OR ti.status = @status_filter)
  AND ti.cleared_at IS NULL
  -- See ListPendingTaggingItemsAlphabetical.
  AND (
    ti.status IN ('confirmed', 'skipped')
    OR EXISTS (
      SELECT 1 FROM audio_files af
      WHERE af.group_key = ti.group_key AND af.tag_status = 'untagged'
    )
  )
ORDER BY ti.score IS NULL, ti.score DESC, LOWER(ti.album_artist), LOWER(ti.album_name)
LIMIT @row_limit OFFSET @row_offset;

-- name: ClearCompletedTaggingItems :exec
UPDATE tagging_items
SET cleared_at = CURRENT_TIMESTAMP
WHERE status = 'confirmed'
  AND cleared_at IS NULL
  AND (CAST(@library_id AS INTEGER) = 0 OR library_id = @library_id);

-- name: GetPendingFolderDetail :one
SELECT
  ti.group_key,
  ti.library_id,
  COALESCE(lb.name, '') AS library_name,
  COALESCE(lb.path, '') AS library_path,
  CAST(COALESCE((SELECT af.file_path FROM audio_files af WHERE af.group_key = ti.group_key AND af.group_key != '' LIMIT 1), '') AS TEXT) AS sample_file_path,
  ti.track_count,
  ti.album_name,
  ti.album_artist,
  ti.disc_number,
  ti.best_match_release_mbid,
  ti.score,
  ti.last_checked_at,
  ti.status,
  ti.created_at,
  ti.synthetic
FROM tagging_items ti
LEFT JOIN libraries lb ON lb.id = ti.library_id
WHERE ti.group_key = ?
LIMIT 1;

-- name: ListPendingTaggingItemsByRecent :many
SELECT
  ti.group_key,
  ti.library_id,
  COALESCE(lb.name, '') AS library_name,
  ti.track_count,
  ti.album_name,
  ti.album_artist,
  ti.disc_number,
  ti.best_match_release_mbid,
  ti.score,
  ti.last_checked_at,
  ti.status,
  ti.created_at
FROM tagging_items ti
LEFT JOIN libraries lb ON lb.id = ti.library_id
WHERE (CAST(@library_id AS INTEGER) = 0 OR ti.library_id = @library_id)
  AND (CAST(@status_filter AS TEXT) = 'all' OR ti.status = @status_filter)
ORDER BY ti.created_at DESC, ti.group_key
LIMIT @row_limit OFFSET @row_offset;

-- name: ListAudioFilesInTaggingGroup :many
-- album_name/album_artist are the PER-TRACK tags (each file's own
-- album link), not the folder-level tagging_items values.
-- SplitMixedFolder clusters on these to find sub-albums hiding inside
-- a folder full of unrelated tracks.
SELECT
  af.id,
  af.file_path,
  af.basename,
  af.length_milliseconds,
  af.tag_status,
  COALESCE(af.track_number, 0) AS track_number,
  COALESCE(af.disc_number, 0) AS disc_number,
  af.title,
  af.artist_credit AS artist_name,
  COALESCE(af.recording_mbid, '') AS recording_mbid,
  COALESCE(al.name, '') AS album_name,
  COALESCE(al.artist_credit, '') AS album_artist
FROM audio_files af
LEFT JOIN albums al ON al.id = af.album_id
WHERE af.group_key = ?
ORDER BY COALESCE(af.disc_number, 0),
         COALESCE(af.track_number, 0),
         af.file_path;

-- name: ListLocalAlbumCandidates :many
-- One row per (album, track) for any local album carrying an MBID.
-- Callers group these in Go and filter by normalized album-name match;
-- the join is case-insensitive on name to pre-filter cheaply.
SELECT
  al.id AS album_id,
  al.mbid AS album_mbid,
  al.name AS album_name,
  COALESCE(al.year, 0) AS year,
  al.artist_credit,
  COALESCE(af.track_number, 0) AS track_number,
  COALESCE(af.disc_number, 0) AS disc_number,
  af.title AS track_title,
  COALESCE(af.recording_mbid, '') AS recording_mbid,
  af.length_milliseconds
FROM albums al
JOIN audio_files af ON af.album_id = al.id
WHERE al.mbid IS NOT NULL
  AND al.mbid != ''
  AND af.recording_mbid IS NOT NULL
  AND af.recording_mbid != ''
  AND al.name = ? COLLATE NOCASE
ORDER BY al.id, af.disc_number, af.track_number;

-- name: SetTaggingItemBestMatch :exec
UPDATE tagging_items
SET best_match_release_mbid = ?,
    score = ?,
    status = ?,
    last_checked_at = CURRENT_TIMESTAMP
WHERE group_key = ?;

-- name: SetTaggingItemScore :exec
UPDATE tagging_items
SET best_match_release_mbid = ?,
    score = ?,
    last_checked_at = CURRENT_TIMESTAMP
WHERE group_key = ?;

-- name: SetTaggingItemStatus :exec
UPDATE tagging_items
SET status = ?,
    last_checked_at = CURRENT_TIMESTAMP
WHERE group_key = ?;

-- name: SetAudioFileTagStatus :exec
UPDATE audio_files SET tag_status = ? WHERE id = ?;

-- name: SetFileRecordingMBID :exec
UPDATE audio_files SET recording_mbid = ? WHERE id = ?;

-- name: SetFileAlbumMBID :exec
-- The album MBID for the album a file belongs to.  Keyed by file
-- because that is what the autotag apply path holds; under the old
-- schema it had to look the release group up through two join tables
-- first (GetRecordingReleaseGroupID), which is gone.
UPDATE albums SET mbid = ?
WHERE albums.id = (SELECT af.album_id FROM audio_files af WHERE af.id = ?);

-- name: GetNextPendingTaggingItem :one
SELECT
  ti.group_key,
  ti.library_id,
  COALESCE(lb.name, '') AS library_name,
  ti.track_count,
  ti.album_name,
  ti.album_artist,
  ti.disc_number,
  ti.best_match_release_mbid,
  ti.score,
  ti.last_checked_at,
  ti.status,
  ti.created_at
FROM tagging_items ti
LEFT JOIN libraries lb ON lb.id = ti.library_id
WHERE ti.status = 'pending'
  AND (CAST(@library_id AS INTEGER) = 0 OR ti.library_id = @library_id)
  AND ti.group_key > @after_group_key
  -- See CountPendingTaggingItems: the cursor must not stop on a
  -- folder the list query no longer shows, or "next" walks folders
  -- that are not in the sidebar.
  AND EXISTS (
    SELECT 1 FROM audio_files af
    WHERE af.group_key = ti.group_key AND af.tag_status = 'untagged'
  )
ORDER BY ti.group_key
LIMIT 1;

-- name: GetTaggingItemsForAlbum :many
-- Every tagging group holding a file of this album.
--
-- The join is `audio_files.group_key`, not a key derived from the
-- album's folder path: a group carved out of a mixed-bag folder by
-- SplitMixedFolder is keyed on its tags rather than on a directory,
-- so a path-derived key finds nothing for exactly the messiest
-- libraries this is meant to help.
--
-- Usually one row. A multi-disc album is one group per disc, which
-- the caller has to know about rather than average over -- applying
-- to "the album" would silently retag one disc of three.
SELECT
  ti.group_key,
  ti.status,
  ti.score,
  ti.best_match_release_mbid,
  ti.track_count,
  ti.album_name,
  ti.album_artist,
  ti.synthetic
FROM tagging_items ti
WHERE ti.group_key IN (
    SELECT DISTINCT af.group_key
    FROM audio_files af
    WHERE af.album_id = sqlc.arg(album_id) AND af.group_key != ''
  )
  AND ti.cleared_at IS NULL
-- Best first, with an unscored group last rather than first: NULL
-- sorts low in SQLite and DESC would put it at the top.
ORDER BY ti.score IS NULL, ti.score DESC, ti.group_key;
