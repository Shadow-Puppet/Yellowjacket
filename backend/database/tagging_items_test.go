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
		  AND recording_id IN (
		    SELECT id FROM recordings
		    WHERE mbid IS NOT NULL AND mbid != ''
		  )
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

func TestCountPendingTaggingItems_UsesPartialIndex(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)

	seedTaggingItem(t, db, "g1", 0, "Album A", "Artist A", 2, "pending")

	rows, err := db.QueryContext(`
		EXPLAIN QUERY PLAN
		SELECT COUNT(*) FROM tagging_items
		WHERE status = 'pending'
		  AND (CAST(0 AS INTEGER) = 0 OR library_id = 0)
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

// seedAF inserts a minimal recording + audio_files pair and returns
// the new audio_files id.  All FK-satisfying rows (artist_credit,
// recordings, file_types[0]) are created inline.
func seedAF(
	t *testing.T,
	db *database.DB,
	filePath string,
	libraryID, discNumber int64,
	recordingName, recordingMBID string,
) int64 {
	t.Helper()

	ac, err := db.Queries.UpsertArtistCredit(db.Ctx, "Test Artist")
	if err != nil {
		t.Fatalf("upsert artist credit: %v", err)
	}

	rec, err := db.Queries.CreateRecordingFull(
		db.Ctx,
		sqlcgen.CreateRecordingFullParams{
			Name:           recordingName,
			ArtistCreditID: ac.ID,
		},
	)
	if err != nil {
		t.Fatalf("create recording: %v", err)
	}

	if recordingMBID != "" {
		if _, err := db.ExecContext(
			`UPDATE recordings SET mbid = ? WHERE id = ?`,
			recordingMBID, rec.ID,
		); err != nil {
			t.Fatalf("set mbid: %v", err)
		}
	}

	af, err := db.Queries.CreateAudioFile(
		db.Ctx,
		sqlcgen.CreateAudioFileParams{
			FilePath:           filePath,
			LengthMilliseconds: 1000,
			FileTypeID:         0,
			RecordingID:        rec.ID,
			Basename:           filePath,
			LibraryID:          libraryID,
		},
	)
	if err != nil {
		t.Fatalf("create audio file: %v", err)
	}

	_ = discNumber // reserved for callers that want specific disc values

	return af.ID
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
