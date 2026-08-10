-- Repairs tagging_items rows left behind by a library-scan bug: the
-- rescan's orphan-cleanup phase deleted audio_files rows for files
-- removed from disk without decrementing/clearing their tagging
-- group, so a folder whose contents were fully replaced kept a
-- phantom entry (stale track_count, no matching audio_files) in the
-- autotag queue forever. The library scan code no longer has this
-- gap, but a database written before the fix still carries the
-- damage — this is a one-time repair, not ongoing bookkeeping.
--
-- Drop groups with no audio_files left at all.
DELETE FROM tagging_items
WHERE group_key NOT IN (
  SELECT DISTINCT group_key FROM audio_files WHERE group_key != ''
);

-- Reconcile track_count for groups that are still alive but drifted
-- (some, not all, of their tracks were removed without decrementing).
UPDATE tagging_items
SET track_count = (
  SELECT COUNT(*) FROM audio_files WHERE audio_files.group_key = tagging_items.group_key
)
WHERE track_count != (
  SELECT COUNT(*) FROM audio_files WHERE audio_files.group_key = tagging_items.group_key
);
