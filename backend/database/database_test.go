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

func TestMigration6FreshDB(t *testing.T) {
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

	// Verify user_version >= 7.
	var version int

	verRows, err := db.QueryContext("PRAGMA user_version")
	if err != nil {
		t.Fatalf("PRAGMA user_version: %v", err)
	}

	if !verRows.Next() {
		_ = verRows.Close()

		t.Fatal("PRAGMA user_version: no row returned")
	}

	if err := verRows.Scan(&version); err != nil {
		_ = verRows.Close()

		t.Fatalf("scan user_version: %v", err)
	}

	_ = verRows.Close()

	if version < 7 {
		t.Errorf("user_version = %d, want >= 7", version)
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

func TestMigration6LibraryQueries(t *testing.T) {
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

func TestMigration6PhantomPlaylistTracks(t *testing.T) {
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

func TestMigration6AudioFilesLibraryFK(t *testing.T) {
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

func TestMigration6TrackMetadataViewHasLibraryID(t *testing.T) {
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

func TestMigration9SmartPlaylistColumns(t *testing.T) {
	t.Parallel()

	db := NewTestDB(t)

	// Verify user_version >= 9.
	var version int

	verRows, err := db.QueryContext("PRAGMA user_version")
	if err != nil {
		t.Fatalf("PRAGMA user_version: %v", err)
	}

	if !verRows.Next() {
		_ = verRows.Close()

		t.Fatal("PRAGMA user_version: no row returned")
	}

	if err := verRows.Scan(&version); err != nil {
		_ = verRows.Close()

		t.Fatalf("scan user_version: %v", err)
	}

	_ = verRows.Close()

	if version < 9 {
		t.Errorf("user_version = %d, want >= 9", version)
	}

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
