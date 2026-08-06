package database

import (
	"database/sql"
	"strings"
	"testing"

	"yellowjacket/backend/database/sql/sqlcgen"
)

// ---------------------------------------------------------------------------
// Migration 6 integration tests
// ---------------------------------------------------------------------------

func TestSchemaCreatesLibrariesTable(t *testing.T) {
	t.Parallel()

	db := NewTestDB(t)

	// Verify libraries table exists.
	var tableCount int64

	rows, err := db.QueryContext(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='libraries'",
	)
	if err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}

	if !rows.Next() {
		_ = rows.Close()

		t.Fatal("no row returned from sqlite_master query")
	}

	if err := rows.Scan(&tableCount); err != nil {
		_ = rows.Close()

		t.Fatalf("scan table count: %v", err)
	}

	_ = rows.Close()

	if tableCount != 1 {
		t.Errorf("libraries table count = %d, want 1", tableCount)
	}

	// Verify audio_files has library_id column.
	hasLibraryID := false

	afRows, err := db.QueryContext("PRAGMA table_info(audio_files)")
	if err != nil {
		t.Fatalf("PRAGMA table_info(audio_files): %v", err)
	}

	for afRows.Next() {
		var (
			cid       int64
			name      string
			colType   string
			notNull   int64
			dfltValue sql.NullString
			pk        int64
		)

		if err := afRows.Scan(
			&cid, &name, &colType, &notNull, &dfltValue, &pk,
		); err != nil {
			_ = afRows.Close()

			t.Fatalf("scan table_info row: %v", err)
		}

		if name == "library_id" {
			hasLibraryID = true
		}
	}

	_ = afRows.Close()

	if !hasLibraryID {
		t.Error("audio_files missing library_id column")
	}

	// Verify playlist_tracks has phantom columns and nullable
	// audio_file_id.
	phantomCols := map[string]bool{
		"phantom_title":          false,
		"phantom_artist":         false,
		"phantom_album":          false,
		"phantom_duration_ms":    false,
		"phantom_genre":          false,
		"phantom_cover_art_path": false,
		"phantom_file_path":      false,
	}

	audioFileIDNullable := false

	ptRows, err := db.QueryContext(
		"PRAGMA table_info(playlist_tracks)",
	)
	if err != nil {
		t.Fatalf("PRAGMA table_info(playlist_tracks): %v", err)
	}

	for ptRows.Next() {
		var (
			cid       int64
			name      string
			colType   string
			notNull   int64
			dfltValue sql.NullString
			pk        int64
		)

		if err := ptRows.Scan(
			&cid, &name, &colType, &notNull, &dfltValue, &pk,
		); err != nil {
			_ = ptRows.Close()

			t.Fatalf("scan playlist_tracks table_info: %v", err)
		}

		if _, ok := phantomCols[name]; ok {
			phantomCols[name] = true
		}

		if name == "audio_file_id" && notNull == 0 {
			audioFileIDNullable = true
		}
	}

	_ = ptRows.Close()

	for col, found := range phantomCols {
		if !found {
			t.Errorf("playlist_tracks missing column: %s", col)
		}
	}

	if !audioFileIDNullable {
		t.Error(
			"playlist_tracks.audio_file_id should be nullable",
		)
	}

	// Verify track_metadata VIEW includes library_id.
	viewRows, err := db.QueryContext(
		"SELECT sql FROM sqlite_master WHERE name='track_metadata'",
	)
	if err != nil {
		t.Fatalf("query track_metadata view: %v", err)
	}

	if !viewRows.Next() {
		_ = viewRows.Close()

		t.Fatal("track_metadata view not found")
	}

	var viewSQL string

	if err := viewRows.Scan(&viewSQL); err != nil {
		_ = viewRows.Close()

		t.Fatalf("scan view sql: %v", err)
	}

	_ = viewRows.Close()

	if !strings.Contains(viewSQL, "library_id") {
		t.Error("track_metadata VIEW does not contain library_id")
	}

	// Verify libraries table has only the sentinel row on fresh DB.
	count, err := db.Queries.CountLibraries(db.Ctx)
	if err != nil {
		t.Fatalf("CountLibraries: %v", err)
	}

	// Sentinel library at id=0 is inserted by NewTestDB.
	if count != 1 {
		t.Errorf(
			"CountLibraries on fresh DB = %d, want 1 (sentinel only)",
			count,
		)
	}
}

func TestLibraryQueries(t *testing.T) {
	t.Parallel()

	db := NewTestDB(t)

	// Create a library.
	lib, err := db.Queries.CreateLibrary(
		db.Ctx,
		sqlcgen.CreateLibraryParams{
			Name: "Music",
			Path: "/home/user/Music",
		},
	)
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}

	if lib.Name != "Music" {
		t.Errorf("Name = %q, want %q", lib.Name, "Music")
	}

	if lib.Path != "/home/user/Music" {
		t.Errorf("Path = %q, want %q", lib.Path, "/home/user/Music")
	}

	if lib.ID <= 0 {
		t.Errorf("ID = %d, want > 0", lib.ID)
	}

	// Get by ID.
	got, err := db.Queries.GetLibrary(db.Ctx, lib.ID)
	if err != nil {
		t.Fatalf("GetLibrary: %v", err)
	}

	if got.Name != lib.Name || got.Path != lib.Path {
		t.Errorf(
			"GetLibrary = {%q, %q}, want {%q, %q}",
			got.Name, got.Path, lib.Name, lib.Path,
		)
	}

	// Get by path.
	gotByPath, err := db.Queries.GetLibraryByPath(
		db.Ctx, "/home/user/Music",
	)
	if err != nil {
		t.Fatalf("GetLibraryByPath: %v", err)
	}

	if gotByPath.ID != lib.ID {
		t.Errorf(
			"GetLibraryByPath ID = %d, want %d",
			gotByPath.ID, lib.ID,
		)
	}

	// Unique path constraint.
	_, err = db.Queries.CreateLibrary(
		db.Ctx,
		sqlcgen.CreateLibraryParams{
			Name: "Duplicate",
			Path: "/home/user/Music",
		},
	)

	if !IsUniqueViolation(err) {
		t.Errorf(
			"duplicate path insert: got %v, want unique violation",
			err,
		)
	}

	// List libraries (sentinel + Music).
	libs, err := db.Queries.GetAllLibraries(db.Ctx)
	if err != nil {
		t.Fatalf("GetAllLibraries: %v", err)
	}

	if len(libs) != 2 {
		t.Errorf("GetAllLibraries len = %d, want 2", len(libs))
	}

	// Update name.
	err = db.Queries.UpdateLibraryName(
		db.Ctx,
		sqlcgen.UpdateLibraryNameParams{
			Name: "My Music",
			ID:   lib.ID,
		},
	)
	if err != nil {
		t.Fatalf("UpdateLibraryName: %v", err)
	}

	updated, _ := db.Queries.GetLibrary(db.Ctx, lib.ID)

	if updated.Name != "My Music" {
		t.Errorf(
			"after update Name = %q, want %q",
			updated.Name, "My Music",
		)
	}

	// Delete.
	err = db.Queries.DeleteLibrary(db.Ctx, lib.ID)
	if err != nil {
		t.Fatalf("DeleteLibrary: %v", err)
	}

	count, _ := db.Queries.CountLibraries(db.Ctx)

	// Only sentinel remains.
	if count != 1 {
		t.Errorf("after delete CountLibraries = %d, want 1", count)
	}
}

func TestPhantomPlaylistTracksAreCleaned(t *testing.T) {
	t.Parallel()

	db, libID := NewTestDBWithLibrary(t, "Test", "/test/music")

	// Create prerequisite data: artist_credit, recording,
	// audio_file.
	_, err := db.ExecContext(
		"INSERT INTO artist_credit (id, text) VALUES (1, 'Test Artist')",
	)
	if err != nil {
		t.Fatalf("insert artist_credit: %v", err)
	}

	_, err = db.ExecContext(
		"INSERT INTO recordings (id, name, artist_credit_id) " +
			"VALUES (1, 'Test Song', 1)",
	)
	if err != nil {
		t.Fatalf("insert recording: %v", err)
	}

	_, err = db.ExecContext(
		"INSERT INTO audio_files "+
			"(id, file_path, length_milliseconds, file_type_id, "+
			"recording_id, library_id) "+
			"VALUES (1, '/test/music/song.mp3', 180000, 0, 1, ?)",
		libID,
	)
	if err != nil {
		t.Fatalf("insert audio_file: %v", err)
	}

	// Create playlist.
	playlist, err := db.Queries.CreatePlaylist(
		db.Ctx, "Test Playlist",
	)
	if err != nil {
		t.Fatalf("CreatePlaylist: %v", err)
	}

	// Add track with phantom metadata (eager population).
	track, err := db.Queries.AddPlaylistTrack(
		db.Ctx,
		sqlcgen.AddPlaylistTrackParams{
			PlaylistID: playlist.ID,
			AudioFileID: sql.NullInt64{
				Int64: 1,
				Valid: true,
			},
			Position: 0,
			PhantomTitle: sql.NullString{
				String: "Test Song",
				Valid:  true,
			},
			PhantomArtist: sql.NullString{
				String: "Test Artist",
				Valid:  true,
			},
			PhantomAlbum: sql.NullString{
				String: "Test Album",
				Valid:  true,
			},
			PhantomDurationMs: sql.NullInt64{
				Int64: 180000,
				Valid: true,
			},
			PhantomGenre: sql.NullString{
				String: "Rock",
				Valid:  true,
			},
		},
	)
	if err != nil {
		t.Fatalf("AddPlaylistTrack: %v", err)
	}

	if track.ID <= 0 {
		t.Errorf("track ID = %d, want > 0", track.ID)
	}

	// Delete the audio file — FK ON DELETE SET NULL should set
	// audio_file_id to NULL without deleting the playlist_track.
	_, err = db.ExecContext(
		"DELETE FROM audio_files WHERE id = 1",
	)
	if err != nil {
		t.Fatalf("delete audio_file: %v", err)
	}

	// Verify playlist track still exists with phantom data.
	tracks, err := db.Queries.GetPlaylistTracksWithMetadata(
		db.Ctx, playlist.ID,
	)
	if err != nil {
		t.Fatalf("GetPlaylistTracksWithMetadata: %v", err)
	}

	if len(tracks) != 1 {
		t.Fatalf(
			"tracks after delete = %d, want 1", len(tracks),
		)
	}

	row := tracks[0]

	if row.AudioFileID.Valid {
		t.Error(
			"audio_file_id should be NULL after delete",
		)
	}

	if row.Title != "Test Song" {
		t.Errorf(
			"phantom Title = %q, want %q",
			row.Title, "Test Song",
		)
	}

	if row.Artist != "Test Artist" {
		t.Errorf(
			"phantom Artist = %q, want %q",
			row.Artist, "Test Artist",
		)
	}

	if row.IsPhantom != 1 {
		t.Errorf("IsPhantom = %d, want 1", row.IsPhantom)
	}
}

func TestAudioFilesLibraryForeignKey(t *testing.T) {
	t.Parallel()

	db, libID := NewTestDBWithLibrary(t, "Test", "/test/fk-lib")

	// Insert prerequisite recording.
	_, err := db.ExecContext(
		"INSERT INTO artist_credit (id, text) VALUES (1, 'Test')",
	)
	if err != nil {
		t.Fatalf("insert artist_credit: %v", err)
	}

	_, err = db.ExecContext(
		"INSERT INTO recordings (id, name, artist_credit_id) " +
			"VALUES (1, 'Track', 1)",
	)
	if err != nil {
		t.Fatalf("insert recording: %v", err)
	}

	// Insert audio file with valid library_id — should succeed.
	_, err = db.ExecContext(
		"INSERT INTO audio_files "+
			"(id, file_path, length_milliseconds, file_type_id, "+
			"recording_id, library_id) "+
			"VALUES (1, '/test/song.mp3', 180000, 0, 1, ?)",
		libID,
	)
	if err != nil {
		t.Fatalf("insert audio_file with valid library: %v", err)
	}

	// Insert audio file with invalid library_id — should fail FK.
	_, err = db.ExecContext(
		"INSERT INTO audio_files " +
			"(id, file_path, length_milliseconds, file_type_id, " +
			"recording_id, library_id) " +
			"VALUES (2, '/test/song2.mp3', 200000, 0, 1, 999)",
	)
	if err == nil {
		t.Error(
			"insert with invalid library_id should fail FK check",
		)
	}

	// Count files by library.
	count, err := db.Queries.CountAudioFilesByLibrary(
		db.Ctx, libID,
	)
	if err != nil {
		t.Fatalf("CountAudioFilesByLibrary: %v", err)
	}

	if count != 1 {
		t.Errorf(
			"CountAudioFilesByLibrary = %d, want 1", count,
		)
	}
}

func TestTrackMetadataViewHasLibraryID(t *testing.T) {
	t.Parallel()

	db, libID := NewTestDBWithLibrary(t, "Test", "/test/view-lib")

	// Insert prerequisites.
	_, err := db.ExecContext(
		"INSERT INTO artist_credit (id, text) VALUES (1, 'View Artist')",
	)
	if err != nil {
		t.Fatalf("insert artist_credit: %v", err)
	}

	_, err = db.ExecContext(
		"INSERT INTO recordings (id, name, artist_credit_id) " +
			"VALUES (1, 'View Track', 1)",
	)
	if err != nil {
		t.Fatalf("insert recording: %v", err)
	}

	_, err = db.ExecContext(
		"INSERT INTO audio_files "+
			"(id, file_path, length_milliseconds, file_type_id, "+
			"recording_id, library_id) "+
			"VALUES (1, '/test/view.mp3', 200000, 0, 1, ?)",
		libID,
	)
	if err != nil {
		t.Fatalf("insert audio_file: %v", err)
	}

	// Query track_metadata VIEW and verify library_id is present
	// with the correct value.
	viewRows, err := db.QueryContext(
		"SELECT library_id FROM track_metadata WHERE id = 1",
	)
	if err != nil {
		t.Fatalf("query track_metadata: %v", err)
	}

	if !viewRows.Next() {
		_ = viewRows.Close()

		t.Fatal("track_metadata: no row returned")
	}

	var viewLibID int64

	if err := viewRows.Scan(&viewLibID); err != nil {
		_ = viewRows.Close()

		t.Fatalf("scan library_id from view: %v", err)
	}

	_ = viewRows.Close()

	if viewLibID != libID {
		t.Errorf(
			"track_metadata.library_id = %d, want %d",
			viewLibID, libID,
		)
	}
}

// ---------------------------------------------------------------------------
// Migration 9 integration tests
// ---------------------------------------------------------------------------

func TestSmartPlaylistColumns(t *testing.T) {
	t.Parallel()

	db := NewTestDB(t)

	// Verify playlists table has is_smart and smart_rules columns.
	hasSmart := false
	hasRules := false

	ptRows, err := db.QueryContext(
		"PRAGMA table_info(playlists)",
	)
	if err != nil {
		t.Fatalf("PRAGMA table_info(playlists): %v", err)
	}

	for ptRows.Next() {
		var (
			cid       int64
			name      string
			colType   string
			notNull   int64
			dfltValue sql.NullString
			pk        int64
		)

		if err := ptRows.Scan(
			&cid, &name, &colType, &notNull, &dfltValue, &pk,
		); err != nil {
			_ = ptRows.Close()

			t.Fatalf("scan playlists table_info: %v", err)
		}

		if name == "is_smart" {
			hasSmart = true
		}

		if name == "smart_rules" {
			hasRules = true
		}
	}

	_ = ptRows.Close()

	if !hasSmart {
		t.Error("playlists missing is_smart column")
	}

	if !hasRules {
		t.Error("playlists missing smart_rules column")
	}

	// Insert a smart playlist with rules.
	rulesJSON := `{"rules":[{"field":"genre","operator":"is","value":"Rock"}]}`

	_, err = db.ExecContext(
		"INSERT INTO playlists (name, is_smart, smart_rules) VALUES (?, 1, ?)",
		"Rock Songs", rulesJSON,
	)
	if err != nil {
		t.Fatalf("insert smart playlist: %v", err)
	}

	// Read it back and verify.
	rows, err := db.QueryContext(
		"SELECT is_smart, smart_rules FROM playlists WHERE name = ?",
		"Rock Songs",
	)
	if err != nil {
		t.Fatalf("query smart playlist: %v", err)
	}

	if !rows.Next() {
		_ = rows.Close()

		t.Fatal("smart playlist not found")
	}

	var (
		isSmart    int64
		smartRules sql.NullString
	)

	if err := rows.Scan(&isSmart, &smartRules); err != nil {
		_ = rows.Close()

		t.Fatalf("scan smart playlist: %v", err)
	}

	_ = rows.Close()

	if isSmart != 1 {
		t.Errorf("is_smart = %d, want 1", isSmart)
	}

	if !smartRules.Valid || smartRules.String != rulesJSON {
		t.Errorf(
			"smart_rules = %q, want %q",
			smartRules.String, rulesJSON,
		)
	}

	// Insert a regular playlist (default is_smart) and verify
	// it defaults to 0.
	_, err = db.ExecContext(
		"INSERT INTO playlists (name) VALUES (?)",
		"Regular Playlist",
	)
	if err != nil {
		t.Fatalf("insert regular playlist: %v", err)
	}

	regRows, err := db.QueryContext(
		"SELECT is_smart, smart_rules FROM playlists WHERE name = ?",
		"Regular Playlist",
	)
	if err != nil {
		t.Fatalf("query regular playlist: %v", err)
	}

	if !regRows.Next() {
		_ = regRows.Close()

		t.Fatal("regular playlist not found")
	}

	var (
		regSmart int64
		regRules sql.NullString
	)

	if err := regRows.Scan(&regSmart, &regRules); err != nil {
		_ = regRows.Close()

		t.Fatalf("scan regular playlist: %v", err)
	}

	_ = regRows.Close()

	if regSmart != 0 {
		t.Errorf("regular playlist is_smart = %d, want 0", regSmart)
	}

	if regRules.Valid {
		t.Errorf(
			"regular playlist smart_rules should be NULL, got %q",
			regRules.String,
		)
	}
}

// ---------------------------------------------------------------------------
// Migration 10 — play history tracking
// ---------------------------------------------------------------------------

func TestPlayHistoryTable(t *testing.T) {
	t.Parallel()

	db := NewTestDB(t)

	// Verify play_history table exists.
	var tableCount int64

	tblRows, err := db.QueryContext(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='play_history'",
	)
	if err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}

	if !tblRows.Next() {
		_ = tblRows.Close()

		t.Fatal("no row from sqlite_master query")
	}

	if err := tblRows.Scan(&tableCount); err != nil {
		_ = tblRows.Close()

		t.Fatalf("scan table count: %v", err)
	}

	_ = tblRows.Close()

	if tableCount != 1 {
		t.Errorf("play_history table count = %d, want 1", tableCount)
	}

	// Verify audio_files has play_count and last_played columns.
	hasPlayCount := false
	hasLastPlayed := false

	colRows, err := db.QueryContext("PRAGMA table_info(audio_files)")
	if err != nil {
		t.Fatalf("PRAGMA table_info(audio_files): %v", err)
	}

	for colRows.Next() {
		var (
			cid       int64
			name      string
			colType   string
			notNull   int64
			dfltValue sql.NullString
			pk        int64
		)

		if err := colRows.Scan(
			&cid, &name, &colType, &notNull, &dfltValue, &pk,
		); err != nil {
			_ = colRows.Close()

			t.Fatalf("scan audio_files table_info: %v", err)
		}

		if name == "play_count" {
			hasPlayCount = true
		}

		if name == "last_played" {
			hasLastPlayed = true
		}
	}

	_ = colRows.Close()

	if !hasPlayCount {
		t.Error("audio_files missing play_count column")
	}

	if !hasLastPlayed {
		t.Error("audio_files missing last_played column")
	}

	// Verify track_metadata VIEW includes play_count and last_played.
	viewCols := map[string]bool{}

	vcRows, err := db.QueryContext("PRAGMA table_info(track_metadata)")
	if err != nil {
		t.Fatalf("PRAGMA table_info(track_metadata): %v", err)
	}

	for vcRows.Next() {
		var (
			cid       int64
			name      string
			colType   string
			notNull   int64
			dfltValue sql.NullString
			pk        int64
		)

		if err := vcRows.Scan(
			&cid, &name, &colType, &notNull, &dfltValue, &pk,
		); err != nil {
			_ = vcRows.Close()

			t.Fatalf("scan track_metadata table_info: %v", err)
		}

		viewCols[name] = true
	}

	_ = vcRows.Close()

	if !viewCols["play_count"] {
		t.Error("track_metadata VIEW missing play_count column")
	}

	if !viewCols["last_played"] {
		t.Error("track_metadata VIEW missing last_played column")
	}

	// Round-trip: insert a play_history row and verify play_count update.
	// First, set up test data. The test DB already has library id=0.
	_, err = db.ExecContext(
		"INSERT OR IGNORE INTO artist_credit (id, text) VALUES (1, 'Test Artist')",
	)
	if err != nil {
		t.Fatalf("insert artist_credit: %v", err)
	}

	_, err = db.ExecContext(
		`INSERT OR IGNORE INTO recordings (id, name, artist_credit_id, track_number, disc_number)
		 VALUES (1, 'Test Track', 1, 1, 1)`,
	)
	if err != nil {
		t.Fatalf("insert recording: %v", err)
	}

	_, err = db.ExecContext(
		`INSERT INTO audio_files
		 (id, file_path, length_milliseconds, file_type_id, recording_id, library_id)
		 VALUES (1, '/test/track.mp3', 180000, 0, 1, 0)`,
	)
	if err != nil {
		t.Fatalf("insert audio_file: %v", err)
	}

	// Verify default play_count is 0.
	var playCount int64

	pcRows, err := db.QueryContext(
		"SELECT play_count FROM audio_files WHERE id = 1",
	)
	if err != nil {
		t.Fatalf("query play_count: %v", err)
	}

	if !pcRows.Next() {
		_ = pcRows.Close()

		t.Fatal("audio_file not found")
	}

	if err := pcRows.Scan(&playCount); err != nil {
		_ = pcRows.Close()

		t.Fatalf("scan play_count: %v", err)
	}

	_ = pcRows.Close()

	if playCount != 0 {
		t.Errorf("initial play_count = %d, want 0", playCount)
	}

	// Insert a play_history row and update play_count.
	_, err = db.ExecContext(
		"INSERT INTO play_history (audio_file_id) VALUES (1)",
	)
	if err != nil {
		t.Fatalf("insert play_history: %v", err)
	}

	_, err = db.ExecContext(
		`UPDATE audio_files
		 SET play_count = play_count + 1,
		     last_played = datetime('now')
		 WHERE id = 1`,
	)
	if err != nil {
		t.Fatalf("update play_count: %v", err)
	}

	// Verify play_count is now 1.
	pcRows2, err := db.QueryContext(
		"SELECT play_count, last_played FROM audio_files WHERE id = 1",
	)
	if err != nil {
		t.Fatalf("query play_count after update: %v", err)
	}

	if !pcRows2.Next() {
		_ = pcRows2.Close()

		t.Fatal("audio_file not found after update")
	}

	var (
		updatedCount int64
		lastPlayed   sql.NullString
	)

	if err := pcRows2.Scan(&updatedCount, &lastPlayed); err != nil {
		_ = pcRows2.Close()

		t.Fatalf("scan updated play_count: %v", err)
	}

	_ = pcRows2.Close()

	if updatedCount != 1 {
		t.Errorf("play_count after update = %d, want 1", updatedCount)
	}

	if !lastPlayed.Valid {
		t.Error("last_played should not be NULL after update")
	}

	// Verify track_metadata VIEW returns the play_count.
	tmRows, err := db.QueryContext(
		"SELECT play_count FROM track_metadata WHERE id = 1",
	)
	if err != nil {
		t.Fatalf("query track_metadata play_count: %v", err)
	}

	if !tmRows.Next() {
		_ = tmRows.Close()

		t.Fatal("track_metadata row not found")
	}

	var viewPlayCount int64

	if err := tmRows.Scan(&viewPlayCount); err != nil {
		_ = tmRows.Close()

		t.Fatalf("scan track_metadata play_count: %v", err)
	}

	_ = tmRows.Close()

	if viewPlayCount != 1 {
		t.Errorf("track_metadata play_count = %d, want 1", viewPlayCount)
	}
}

// ---------------------------------------------------------------------------
// Migration 11 — explore_cache table
// ---------------------------------------------------------------------------

func TestHTTPCacheTable(t *testing.T) {
	t.Parallel()

	// explore_cache was split into http_cache + artist_metadata by
	// migration 27 and is dropped on fresh installs. This test covers
	// a table that no longer exists in a fresh DB; revisit once the
	// explore cache tests are rewritten against the new schemas.
	t.Skip("explore_cache dropped by migration 27; test is obsolete")

	db := NewTestDB(t)

	// Verify explore_cache table exists.
	var tableCount int64

	tblRows, err := db.QueryContext(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='explore_cache'",
	)
	if err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}

	if !tblRows.Next() {
		_ = tblRows.Close()

		t.Fatal("no row from sqlite_master query")
	}

	if err := tblRows.Scan(&tableCount); err != nil {
		_ = tblRows.Close()

		t.Fatalf("scan table count: %v", err)
	}

	_ = tblRows.Close()

	if tableCount != 1 {
		t.Errorf("explore_cache table count = %d, want 1", tableCount)
	}

	// Verify all expected columns exist.
	expectedCols := map[string]bool{
		"url_key":     false,
		"response":    false,
		"mbid":        false,
		"entity_type": false,
		"expires_at":  false,
		"created_at":  false,
	}

	colRows, err := db.QueryContext(
		"PRAGMA table_info(explore_cache)",
	)
	if err != nil {
		t.Fatalf("PRAGMA table_info(explore_cache): %v", err)
	}

	for colRows.Next() {
		var (
			cid       int64
			name      string
			colType   string
			notNull   int64
			dfltValue sql.NullString
			pk        int64
		)

		if err := colRows.Scan(
			&cid, &name, &colType, &notNull, &dfltValue, &pk,
		); err != nil {
			_ = colRows.Close()

			t.Fatalf("scan table_info row: %v", err)
		}

		if _, ok := expectedCols[name]; ok {
			expectedCols[name] = true
		}
	}

	_ = colRows.Close()

	for col, found := range expectedCols {
		if !found {
			t.Errorf("explore_cache missing column: %s", col)
		}
	}

	// Verify indexes exist.
	idxRows, err := db.QueryContext(
		"SELECT name FROM sqlite_master WHERE type='index' AND tbl_name='explore_cache'",
	)
	if err != nil {
		t.Fatalf("query indexes: %v", err)
	}

	indexes := map[string]bool{}

	for idxRows.Next() {
		var name string

		if err := idxRows.Scan(&name); err != nil {
			_ = idxRows.Close()

			t.Fatalf("scan index name: %v", err)
		}

		indexes[name] = true
	}

	_ = idxRows.Close()

	if !indexes["idx_explore_cache_expires"] {
		t.Error("missing index: idx_explore_cache_expires")
	}

	if !indexes["idx_explore_cache_mbid"] {
		t.Error("missing index: idx_explore_cache_mbid")
	}

	// Round-trip: insert and read back.
	_, err = db.ExecContext(
		`INSERT INTO explore_cache (url_key, response, mbid, entity_type, expires_at)
		 VALUES ('test-key', '{"data":"value"}', 'abc-123', 'artist', datetime('now', '+1 hour'))`,
	)
	if err != nil {
		t.Fatalf("insert explore_cache: %v", err)
	}

	rows, err := db.QueryContext(
		"SELECT url_key, response, mbid, entity_type FROM explore_cache WHERE url_key = 'test-key'",
	)
	if err != nil {
		t.Fatalf("query explore_cache: %v", err)
	}

	if !rows.Next() {
		_ = rows.Close()

		t.Fatal("explore_cache row not found")
	}

	var (
		urlKey     string
		response   string
		mbid       sql.NullString
		entityType sql.NullString
	)

	if err := rows.Scan(&urlKey, &response, &mbid, &entityType); err != nil {
		_ = rows.Close()

		t.Fatalf("scan explore_cache row: %v", err)
	}

	_ = rows.Close()

	if urlKey != "test-key" {
		t.Errorf("url_key = %q, want %q", urlKey, "test-key")
	}

	if response != `{"data":"value"}` {
		t.Errorf("response = %q, want %q", response, `{"data":"value"}`)
	}

	if !mbid.Valid || mbid.String != "abc-123" {
		t.Errorf("mbid = %v, want abc-123", mbid)
	}

	if !entityType.Valid || entityType.String != "artist" {
		t.Errorf("entity_type = %v, want artist", entityType)
	}
}
