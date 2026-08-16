package database_test

import (
	"strings"
	"testing"

	"yellowjacket/backend/autotag"
	"yellowjacket/backend/database"
	"yellowjacket/backend/database/sql/sqlcgen"
)

// ---------------------------------------------------------------------------
// Migration 31: tag_status column
// ---------------------------------------------------------------------------

func TestTagStatusDefaultAndCheck(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)

	seedAF(t, db, "/music/a.mp3", 0, 0, "", "")

	got := scalarString(t, db,
		`SELECT tag_status FROM audio_files WHERE file_path = ?`,
		"/music/a.mp3",
	)
	if got != "untagged" {
		t.Errorf("default tag_status = %q, want %q", got, "untagged")
	}

	// CHECK constraint is applied inline with ALTER TABLE ADD COLUMN
	// in migration 31, so both fresh and upgraded DBs enforce it.
	_, err := db.ExecContext(
		`UPDATE audio_files SET tag_status = 'bogus' WHERE file_path = ?`,
		"/music/a.mp3",
	)
	if err == nil {
		t.Error("expected CHECK constraint failure for invalid tag_status")
	} else if !strings.Contains(err.Error(), "CHECK") &&
		!strings.Contains(err.Error(), "constraint") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTagStatusBackfillFromRecordingMBID(t *testing.T) {
	t.Parallel()

	// Migration 31 runs against a DB that already exists — NewTestDB
	// creates a fresh DB and applies schemas + migrations.  To
	// exercise the backfill we seed audio_files with recordings whose
	// mbid field varies, then re-run the same UPDATE the migration
	// issues and verify each row lands on the expected status.
	db := database.NewTestDB(t)

	seedAF(t, db, "/music/no-mb.mp3", 0, 0, "Song A", "")
	seedAF(t, db, "/music/empty-mb.mp3", 0, 0, "Song B", "")
	seedAF(t, db, "/music/valid-mb.mp3", 0, 0, "Song C",
		"11111111-2222-3333-4444-555555555555",
	)

	// Clear any status first so the backfill has work to do.
	if _, err := db.ExecContext(
		`UPDATE audio_files SET tag_status = 'untagged'`,
	); err != nil {
		t.Fatalf("reset tag_status: %v", err)
	}

	if _, err := db.ExecContext(`
		UPDATE audio_files
		SET tag_status = 'user_confirmed'
		WHERE tag_status = 'untagged'
		  AND recording_mbid IS NOT NULL AND recording_mbid != ''
	`); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	cases := map[string]string{
		"/music/no-mb.mp3":    "untagged",
		"/music/empty-mb.mp3": "untagged",
		"/music/valid-mb.mp3": "user_confirmed",
	}

	for path, want := range cases {
		got := scalarString(t, db,
			`SELECT tag_status FROM audio_files WHERE file_path = ?`, path,
		)
		if got != want {
			t.Errorf("tag_status for %s = %q, want %q", path, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// Migration 32: tagging_items table + group_key column
// ---------------------------------------------------------------------------

func TestTaggingItemsAndGroupKey(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)

	// The schema already has group_key and tagging_items.  Simulate
	// the migration backfill by inserting a couple of audio_files
	// without group_key, then running the Go helper to set it, and
	// verify the aggregate-into-tagging_items step produces one row
	// per (group_key, library_id).
	seedAF(t, db, "/music/Artist/Album/01.mp3", 0, 1, "T1", "")
	seedAF(t, db, "/music/Artist/Album/02.mp3", 0, 1, "T2", "")
	seedAF(t, db, "/music/Artist/Other/01.mp3", 0, 1, "T3", "")

	// Clear any auto-populated group_key from CreateAudioFile.
	if _, err := db.ExecContext(
		`UPDATE audio_files SET group_key = ''`,
	); err != nil {
		t.Fatalf("reset group_key: %v", err)
	}

	// Run the same backfill logic inline.
	rows, err := db.QueryContext(
		`SELECT id, library_id, file_path FROM audio_files ORDER BY id`,
	)
	if err != nil {
		t.Fatalf("select rows: %v", err)
	}

	type afRow struct {
		id        int64
		libraryID int64
		path      string
	}

	var afs []afRow

	for rows.Next() {
		var r afRow
		if scanErr := rows.Scan(&r.id, &r.libraryID, &r.path); scanErr != nil {
			t.Fatalf("scan: %v", scanErr)
		}

		afs = append(afs, r)
	}

	_ = rows.Close()

	// Album for first two files, Other for third — shared parent dirs
	// produce shared group_keys.
	for _, r := range afs {
		key := autotag.GroupKey(r.libraryID, r.path, 0)
		if _, err := db.ExecContext(
			`UPDATE audio_files SET group_key = ? WHERE id = ?`,
			key, r.id,
		); err != nil {
			t.Fatalf("set group_key: %v", err)
		}
	}

	// Aggregate.
	if _, err := db.ExecContext(`
		INSERT INTO tagging_items (
		  group_key, library_id, track_count,
		  album_name, album_artist, disc_number, status
		)
		SELECT
		  af.group_key, af.library_id, COUNT(*),
		  '', '', 0,
		  CASE WHEN SUM(CASE WHEN af.tag_status = 'user_confirmed' THEN 0 ELSE 1 END) = 0
		       THEN 'confirmed' ELSE 'pending' END
		FROM audio_files af
		WHERE af.group_key != ''
		GROUP BY af.group_key, af.library_id
		ON CONFLICT(group_key) DO NOTHING
	`); err != nil {
		t.Fatalf("aggregate: %v", err)
	}

	got := scalarInt(t, db, `SELECT COUNT(*) FROM tagging_items`)
	if got != 2 {
		t.Errorf("tagging_items count = %d, want 2", got)
	}

	// The 2-track Album group should carry track_count = 2.
	albumKey := autotag.GroupKey(0, "/music/Artist/Album/01.mp3", 0)

	count := scalarInt(t, db,
		`SELECT track_count FROM tagging_items WHERE group_key = ?`, albumKey,
	)
	if count != 2 {
		t.Errorf("album track_count = %d, want 2", count)
	}
}

// ---------------------------------------------------------------------------
// 008.4 — sqlc queries, pagination, and partial-index usage
// ---------------------------------------------------------------------------

func TestTaggingItems_ListPendingAndCount(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)

	seedTaggingItem(t, db, "g1", 0, "Album A", "Artist A", 2, "pending")
	seedTaggingItem(t, db, "g2", 0, "Album B", "Artist B", 1, "pending")
	seedTaggingItem(t, db, "g3", 0, "Album C", "Artist C", 3, "confirmed")

	// A pending group is only listed while it still holds untagged
	// files — see TestTaggingItems_FullyTaggedGroupIsNotPending.
	seedGroupFile(t, db, "g1", "/music/a1.mp3", "untagged")
	seedGroupFile(t, db, "g2", "/music/b1.mp3", "untagged")
	seedGroupFile(t, db, "g3", "/music/c1.mp3", "user_confirmed")

	count, err := db.Queries.CountPendingTaggingItems(db.Ctx, 0)
	if err != nil {
		t.Fatalf("count: %v", err)
	}

	if count != 2 { //nolint:mnd
		t.Errorf("pending count = %d, want 2", count)
	}

	items, err := db.Queries.ListPendingTaggingItemsAlphabetical(
		db.Ctx,
		sqlcgen.ListPendingTaggingItemsAlphabeticalParams{
			LibraryID:    0,
			StatusFilter: "pending",
			RowLimit:     50,
			RowOffset:    0,
		},
	)
	if err != nil {
		t.Fatalf("list alphabetical: %v", err)
	}

	if len(items) != 2 { //nolint:mnd
		t.Fatalf("list len = %d, want 2", len(items))
	}

	if items[0].AlbumArtist != "Artist A" || items[1].AlbumArtist != "Artist B" {
		t.Errorf(
			"unexpected order: %q, %q",
			items[0].AlbumArtist, items[1].AlbumArtist,
		)
	}
}

// TestClearUnreviewedConfirmedTaggingItems mirrors migration 0007 the
// way TestTagStatusBackfillFromRecordingMBID mirrors the tag_status
// backfill: NewTestDB applies migrations to an empty database, so the
// only way to exercise one that rewrites existing rows is to seed the
// shapes and re-issue its statement.  Keep the two in step.
func TestClearUnreviewedConfirmedTaggingItems(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)

	// Stamped 'confirmed' by the old backfill: never scored, never
	// checked, never matched — the app has not touched it.
	seedTaggingItem(t, db, "g-backfill", 0, "Bulk", "Artist A", 2, "confirmed")

	// Confirmed by a real apply, which stamps last_checked_at.
	seedTaggingItem(t, db, "g-applied", 0, "Applied", "Artist B", 2, "confirmed")

	if _, err := db.ExecContext(
		`UPDATE tagging_items SET last_checked_at = CURRENT_TIMESTAMP, score = 0.98
		 WHERE group_key = 'g-applied'`,
	); err != nil {
		t.Fatalf("mark applied: %v", err)
	}

	// A skipped row is not confirmed and must be left alone.
	seedTaggingItem(t, db, "g-skipped", 0, "Skipped", "Artist C", 1, "skipped")

	if _, err := db.ExecContext(`
		UPDATE tagging_items
		SET cleared_at = CURRENT_TIMESTAMP
		WHERE status = 'confirmed'
		  AND cleared_at IS NULL
		  AND last_checked_at IS NULL
		  AND score IS NULL
		  AND (best_match_release_mbid IS NULL OR best_match_release_mbid = '')
	`); err != nil {
		t.Fatalf("migration: %v", err)
	}

	cases := map[string]bool{
		"g-backfill": true,
		"g-applied":  false,
		"g-skipped":  false,
	}

	for key, wantCleared := range cases {
		got := scalarInt(t, db,
			`SELECT cleared_at IS NOT NULL FROM tagging_items WHERE group_key = ?`,
			key,
		)
		if (got == 1) != wantCleared {
			t.Errorf("%s cleared = %v, want %v", key, got == 1, wantCleared)
		}
	}
}

func TestCountPendingTaggingItems_UsesPartialIndex(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)

	seedTaggingItem(t, db, "g1", 0, "Album A", "Artist A", 2, "pending")

	rows, err := db.QueryContext(`
		EXPLAIN QUERY PLAN
		SELECT COUNT(*) FROM tagging_items ti
		WHERE ti.status = 'pending'
		  AND (CAST(0 AS INTEGER) = 0 OR ti.library_id = 0)
		  AND EXISTS (
		    SELECT 1 FROM audio_files af
		    WHERE af.group_key = ti.group_key AND af.tag_status = 'untagged'
		  )
	`)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}

	defer func() { _ = rows.Close() }()

	var plan strings.Builder

	for rows.Next() {
		var id, parent, notused int

		var detail string

		if scanErr := rows.Scan(&id, &parent, &notused, &detail); scanErr != nil {
			t.Fatalf("scan: %v", scanErr)
		}

		plan.WriteString(detail)
		plan.WriteString("\n")
	}

	// Must show the partial index is being used — if a future schema
	// change drops or renames it, this assertion fires loudly.
	if !strings.Contains(plan.String(), "idx_tagging_items_status_pending") {
		t.Errorf(
			"badge query plan does not use idx_tagging_items_status_pending:\n%s",
			plan.String(),
		)
	}

	// The untagged-files existence check is asked once per candidate
	// row, so it has to be a seek.  idx_audio_files_tag_status_untagged
	// is keyed on library_id and cannot serve it; the group_key one
	// can, and covers the query outright.
	if !strings.Contains(plan.String(), "idx_audio_files_untagged_group_key") {
		t.Errorf(
			"untagged-files check does not use idx_audio_files_untagged_group_key:\n%s",
			plan.String(),
		)
	}
}

// TestTaggingItems_FullyTaggedGroupIsNotPending pins the rule that
// decides what the autotag review page shows: every scanned folder
// gets a tagging_items row, so "pending" has to mean "still holds
// untagged files" rather than "has a row".  Without it a fully
// MB-tagged library queues its entire album count for review — and
// the background prefetch scores every one of them against
// MusicBrainz.
func TestTaggingItems_FullyTaggedGroupIsNotPending(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)

	seedTaggingItem(t, db, "g-partial", 0, "Half Tagged", "Artist A", 2, "pending")
	seedGroupFile(t, db, "g-partial", "/music/partial-1.mp3", "user_confirmed")
	seedGroupFile(t, db, "g-partial", "/music/partial-2.mp3", "untagged")

	seedTaggingItem(t, db, "g-done", 0, "Already Tagged", "Artist B", 2, "pending")
	seedGroupFile(t, db, "g-done", "/music/done-1.mp3", "user_confirmed")
	seedGroupFile(t, db, "g-done", "/music/done-2.mp3", "user_confirmed")

	// Reviewed rows are history, not work: an applied folder is fully
	// tagged by definition and must stay in the Completed section.
	seedTaggingItem(t, db, "g-applied", 0, "Applied Here", "Artist C", 1, "confirmed")
	seedGroupFile(t, db, "g-applied", "/music/applied-1.mp3", "user_confirmed")

	count, err := db.Queries.CountPendingTaggingItems(db.Ctx, 0)
	if err != nil {
		t.Fatalf("count: %v", err)
	}

	if count != 1 {
		t.Errorf("pending count = %d, want 1 (only the part-tagged folder)", count)
	}

	items, err := db.Queries.ListPendingTaggingItemsByScore(
		db.Ctx,
		sqlcgen.ListPendingTaggingItemsByScoreParams{
			LibraryID:    0,
			StatusFilter: "all",
			RowLimit:     50,
			RowOffset:    0,
		},
	)
	if err != nil {
		t.Fatalf("list by score: %v", err)
	}

	listed := make(map[string]bool, len(items))
	for _, it := range items {
		listed[it.GroupKey] = true
	}

	if !listed["g-partial"] {
		t.Error("expected g-partial (one untagged file left) to be listed")
	}

	if listed["g-done"] {
		t.Error("expected g-done (nothing left to tag) to be filtered out")
	}

	if !listed["g-applied"] {
		t.Error("expected g-applied (confirmed by a real apply) to stay listed")
	}

	next, err := db.Queries.GetNextPendingTaggingItem(
		db.Ctx,
		sqlcgen.GetNextPendingTaggingItemParams{LibraryID: 0, AfterGroupKey: ""},
	)
	if err != nil {
		t.Fatalf("next pending: %v", err)
	}

	// The cursor must not stop on a folder the sidebar no longer
	// shows: alphabetically g-done sorts before g-partial, so a
	// missing predicate here surfaces as "next" landing on nothing.
	if next.GroupKey != "g-partial" {
		t.Errorf("next pending = %q, want %q", next.GroupKey, "g-partial")
	}
}

// TestListPendingFolders_SampleFilePathUsesIndex guards the folder-list
// query's per-row sample_file_path subquery against regressing to a
// full table scan of audio_files.  The `AND af.group_key != ”` guard
// is load-bearing: without it SQLite can't prove the partial index
// idx_audio_files_group_key (WHERE group_key != ”) applies, and the
// subquery degrades to O(folders * audio_files) — the difference
// between the review list loading instantly and taking a minute.
func TestListPendingFolders_SampleFilePathUsesIndex(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)

	rows, err := db.QueryContext(`
		EXPLAIN QUERY PLAN
		SELECT
		  ti.group_key,
		  CAST(COALESCE((SELECT af.file_path FROM audio_files af
		    WHERE af.group_key = ti.group_key AND af.group_key != '' LIMIT 1), '') AS TEXT)
		FROM tagging_items ti
	`)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}

	defer func() { _ = rows.Close() }()

	var plan strings.Builder

	for rows.Next() {
		var id, parent, notused int

		var detail string

		if scanErr := rows.Scan(&id, &parent, &notused, &detail); scanErr != nil {
			t.Fatalf("scan: %v", scanErr)
		}

		plan.WriteString(detail)
		plan.WriteString("\n")
	}

	if !strings.Contains(plan.String(), "idx_audio_files_group_key") {
		t.Errorf(
			"sample_file_path subquery no longer uses idx_audio_files_group_key "+
				"(would full-scan audio_files per folder):\n%s",
			plan.String(),
		)
	}
}

// TestUpsertTaggingItemOnTrackAdd_AlbumArtistTracksConsensus guards
// against regressing to first-write-wins: a folder's album_artist
// must reflect whether every contributing track actually agreed, not
// just whichever track happened to be scanned first.  IsMixedBag
// (backend/autotag) trusts a non-empty album_artist unconditionally,
// so a stale first-seen value here would silently defeat mixed-bag
// detection for the rest of the folder's tracks.
func TestUpsertTaggingItemOnTrackAdd_AlbumArtistTracksConsensus(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)

	upsert := func(t *testing.T, groupKey, albumArtist string) {
		t.Helper()

		if err := db.Queries.UpsertTaggingItemOnTrackAdd(
			db.Ctx, sqlcgen.UpsertTaggingItemOnTrackAddParams{
				GroupKey:    groupKey,
				LibraryID:   0,
				AlbumName:   "",
				AlbumArtist: albumArtist,
				DiscNumber:  0,
			},
		); err != nil {
			t.Fatalf("upsert: %v", err)
		}
	}

	albumArtist := func(t *testing.T, groupKey string) string {
		t.Helper()

		return scalarString(t, db,
			`SELECT album_artist FROM tagging_items WHERE group_key = ?`, groupKey,
		)
	}

	// Every contributing track agrees: the value sticks.
	upsert(t, "agree", "Artist One")
	upsert(t, "agree", "Artist One")
	upsert(t, "agree", "Artist One")

	if got := albumArtist(t, "agree"); got != "Artist One" {
		t.Errorf("unanimous album_artist = %q, want %q", got, "Artist One")
	}

	// A later track disagrees: the value must clear, not freeze on
	// whichever track was scanned first.
	upsert(t, "disagree", "Artist One")
	upsert(t, "disagree", "Artist Two")
	upsert(t, "disagree", "Artist One")

	if got := albumArtist(t, "disagree"); got != "" {
		t.Errorf("disagreeing album_artist = %q, want empty (no consensus)", got)
	}

	// An untagged track (empty AlbumArtist) must not overwrite an
	// established consensus value, nor count as disagreement.
	upsert(t, "partial-tags", "Artist One")
	upsert(t, "partial-tags", "")
	upsert(t, "partial-tags", "Artist One")

	if got := albumArtist(t, "partial-tags"); got != "Artist One" {
		t.Errorf("partial-tags album_artist = %q, want %q", got, "Artist One")
	}

	// Once cleared by disagreement, a later untagged track must not
	// resurrect a stale value.
	upsert(t, "cleared-stays-cleared", "Artist One")
	upsert(t, "cleared-stays-cleared", "Artist Two")
	upsert(t, "cleared-stays-cleared", "")

	if got := albumArtist(t, "cleared-stays-cleared"); got != "" {
		t.Errorf("cleared-stays-cleared album_artist = %q, want empty", got)
	}
}

func TestGetTaggingItemAndListAudioFilesInGroup(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)

	seedTaggingItem(t, db, "g1", 0, "Album A", "Artist A", 2, "pending")

	// Seed two audio files pointing at the same group.
	id1 := seedAF(t, db, "/music/A/01.mp3", 0, 0, "T1", "")
	id2 := seedAF(t, db, "/music/A/02.mp3", 0, 0, "T2", "")

	if _, err := db.ExecContext(
		`UPDATE audio_files SET group_key = 'g1' WHERE id IN (?, ?)`,
		id1, id2,
	); err != nil {
		t.Fatalf("bind group_key: %v", err)
	}

	item, err := db.Queries.GetTaggingItem(db.Ctx, "g1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if item.AlbumName != "Album A" {
		t.Errorf("album_name = %q", item.AlbumName)
	}

	files, err := db.Queries.ListAudioFilesInTaggingGroup(db.Ctx, "g1")
	if err != nil {
		t.Fatalf("list files: %v", err)
	}

	if len(files) != 2 { //nolint:mnd
		t.Errorf("files in group = %d, want 2", len(files))
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// seedAF inserts one file and returns its audio_files id.
func seedAF(
	t *testing.T,
	db *database.DB,
	filePath string,
	libraryID, discNumber int64,
	recordingName, recordingMBID string,
) int64 {
	t.Helper()

	return database.InsertTestTrack(t, db, database.TestTrack{
		FilePath:      filePath,
		Title:         recordingName,
		RecordingMBID: recordingMBID,
		DiscNumber:    discNumber,
		LibraryID:     libraryID,
		LengthMs:      1000,
	})
}

// seedGroupFile attaches one file to a tagging group with an explicit
// tag_status - the thing the queue's "is there anything left to tag
// here" predicate reads.
func seedGroupFile(
	t *testing.T,
	db *database.DB,
	groupKey, filePath, tagStatus string,
) {
	t.Helper()

	database.InsertTestTrack(t, db, database.TestTrack{
		FilePath:  filePath,
		Title:     filePath,
		GroupKey:  groupKey,
		TagStatus: tagStatus,
	})
}

func seedTaggingItem(
	t *testing.T,
	db *database.DB,
	groupKey string,
	libraryID int64,
	album, artist string,
	trackCount int,
	status string,
) {
	t.Helper()

	if _, err := db.ExecContext(`
		INSERT INTO tagging_items (
		  group_key, library_id, track_count,
		  album_name, album_artist, disc_number, status
		) VALUES (?, ?, ?, ?, ?, 0, ?)
	`, groupKey, libraryID, trackCount, album, artist, status); err != nil {
		t.Fatalf("seed tagging_item: %v", err)
	}
}

func scalarString(t *testing.T, db *database.DB, query string, args ...any) string {
	t.Helper()

	rows, err := db.QueryContext(query, args...)
	if err != nil {
		t.Fatalf("query %q: %v", query, err)
	}

	defer func() { _ = rows.Close() }()

	var got string
	if rows.Next() {
		if scanErr := rows.Scan(&got); scanErr != nil {
			t.Fatalf("scan %q: %v", query, scanErr)
		}
	}

	return got
}

func scalarInt(t *testing.T, db *database.DB, query string, args ...any) int64 {
	t.Helper()

	rows, err := db.QueryContext(query, args...)
	if err != nil {
		t.Fatalf("query %q: %v", query, err)
	}

	defer func() { _ = rows.Close() }()

	var got int64
	if rows.Next() {
		if scanErr := rows.Scan(&got); scanErr != nil {
			t.Fatalf("scan %q: %v", query, scanErr)
		}
	}

	return got
}
