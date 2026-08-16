package playlist

import (
	"encoding/json"
	"log/slog"
	"testing"

	"yellowjacket/backend/database"
	"yellowjacket/backend/smartplaylist"
)

// ---------------------------------------------------------------------------
// Test helpers — seed data
// ---------------------------------------------------------------------------

// seedSmartTestTracks inserts a minimal set of tracks with the full
// FK chain required for smart playlist evaluation tests.
//
//	ID 1: "Electric Song"  by "Band A"  album "Album One" (2020) genre=Rock    duration=300000ms
//	ID 2: "Acoustic Vibes" by "Band B"  album "Album Two" (2015) genre=Jazz    duration=240000ms
//	ID 3: "Heavy Metal"    by "Band A"  album "Album One" (2020) genre=Metal   duration=420000ms
func seedSmartTestTracks(t *testing.T, db *database.DB) {
	t.Helper()

	type track struct {
		id       int64
		filePath string
		title    string
		artist   string
		album    string
		year     int64
		genre    string
		lenMs    int64
	}

	tracks := []track{
		{
			1, "/music/band_a/electric.mp3",
			"Electric Song", "Band A", "Album One",
			2020, "Rock", 300000,
		},
		{
			2, "/music/band_b/acoustic.flac",
			"Acoustic Vibes", "Band B", "Album Two",
			2015, "Jazz", 240000,
		},
		{
			3, "/music/band_a/heavy.mp3",
			"Heavy Metal", "Band A", "Album One",
			2020, "Metal", 420000,
		},
	}

	for _, tr := range tracks {
		database.InsertTestTrack(t, db, database.TestTrack{
			FilePath: tr.filePath,
			Title:    tr.title,
			Artist:   tr.artist,
			Album:    tr.album,
			Genres:   []string{tr.genre},
			Year:     tr.year,
			LengthMs: tr.lenMs,
		})
	}
}

// newTestService constructs a playlist.Service with only the
// fields needed for smart playlist operations (db and logger).
func newTestService(t *testing.T, db *database.DB) *Service {
	t.Helper()

	return &Service{
		db:              db,
		logger:          slog.Default(),
		dataDirOverride: t.TempDir(),
	}
}

// makeRulesJSON is a helper that marshals rules into a valid JSON
// string for use in tests.
func makeRulesJSON(t *testing.T, rules smartplaylist.RuleSet) string {
	t.Helper()

	data, err := json.Marshal(rules)
	if err != nil {
		t.Fatalf("could not marshal rules: %v", err)
	}

	return string(data)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestSmartPlaylistCreateAndEvaluate(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	seedSmartTestTracks(t, db)

	svc := newTestService(t, db)

	rulesJSON := makeRulesJSON(t, smartplaylist.RuleSet{
		Rules: []smartplaylist.Rule{
			{Field: "artist", Operator: "is", Value: "Band A"},
		},
	})

	// Create smart playlist.
	summary, err := svc.CreateSmartPlaylist("My Smart PL", rulesJSON)
	if err != nil {
		t.Fatalf("CreateSmartPlaylist failed: %v", err)
	}

	if summary.Name != "My Smart PL" {
		t.Errorf("Name = %q, want %q", summary.Name, "My Smart PL")
	}

	if summary.ID <= 0 {
		t.Errorf("ID = %d, want > 0", summary.ID)
	}

	if summary.CreatedAt == "" {
		t.Error("CreatedAt is empty")
	}

	if summary.UpdatedAt == "" {
		t.Error("UpdatedAt is empty")
	}

	// Evaluate the smart playlist.
	tracks, err := svc.EvaluateSmartPlaylist(summary.ID)
	if err != nil {
		t.Fatalf("EvaluateSmartPlaylist failed: %v", err)
	}

	// Band A has tracks 1 and 3.
	if len(tracks) != 2 {
		t.Fatalf("got %d tracks, want 2", len(tracks))
	}

	// Verify tracks belong to Band A.
	for _, tr := range tracks {
		if tr.ArtistName != "Band A" {
			t.Errorf("track %q has artist %q, want Band A",
				tr.TrackName, tr.ArtistName)
		}
	}
}

func TestSmartPlaylistUpdateRules(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	seedSmartTestTracks(t, db)

	svc := newTestService(t, db)

	// Create with artist filter for Band A (2 tracks).
	initialRules := makeRulesJSON(t, smartplaylist.RuleSet{
		Rules: []smartplaylist.Rule{
			{Field: "artist", Operator: "is", Value: "Band A"},
		},
	})

	summary, err := svc.CreateSmartPlaylist("Update Test", initialRules)
	if err != nil {
		t.Fatalf("CreateSmartPlaylist failed: %v", err)
	}

	// Update to artist = Band B (1 track).
	newRules := makeRulesJSON(t, smartplaylist.RuleSet{
		Rules: []smartplaylist.Rule{
			{Field: "artist", Operator: "is", Value: "Band B"},
		},
	})

	if err := svc.UpdateSmartPlaylistRules(summary.ID, newRules); err != nil {
		t.Fatalf("UpdateSmartPlaylistRules failed: %v", err)
	}

	// Evaluate — should now return only Band B tracks.
	tracks, err := svc.EvaluateSmartPlaylist(summary.ID)
	if err != nil {
		t.Fatalf("EvaluateSmartPlaylist failed: %v", err)
	}

	if len(tracks) != 1 {
		t.Fatalf("got %d tracks, want 1", len(tracks))
	}

	if tracks[0].ArtistName != "Band B" {
		t.Errorf("artist = %q, want Band B", tracks[0].ArtistName)
	}
}

// TestSmartPlaylistPersistedSnapshot verifies that a smart playlist's
// membership is materialized and read from a stored snapshot: it is
// backfilled on first access, does NOT re-evaluate when the rules
// change out from under it, and only re-materializes on an explicit
// refresh.
func TestSmartPlaylistPersistedSnapshot(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	seedSmartTestTracks(t, db)

	svc := newTestService(t, db)

	// Band A → tracks 1 and 3.
	bandARules := makeRulesJSON(t, smartplaylist.RuleSet{
		Rules: []smartplaylist.Rule{
			{Field: "artist", Operator: "is", Value: "Band A"},
		},
	})

	summary, err := svc.CreateSmartPlaylist("Snapshot Test", bandARules)
	if err != nil {
		t.Fatalf("CreateSmartPlaylist failed: %v", err)
	}

	// First access backfills the snapshot (snapshot_at was NULL).
	tracks, err := svc.GetSmartPlaylistTracks(summary.ID)
	if err != nil {
		t.Fatalf("GetSmartPlaylistTracks failed: %v", err)
	}

	if len(tracks) != 2 {
		t.Fatalf("initial snapshot: got %d tracks, want 2", len(tracks))
	}

	for _, tr := range tracks {
		if tr.Artist != "Band A" {
			t.Errorf("snapshot track %q has artist %q, want Band A",
				tr.Title, tr.Artist)
		}
	}

	// Change the rules directly in the DB, bypassing
	// UpdateSmartPlaylistRules so no refresh is triggered. The stored
	// snapshot must be unaffected.
	bandBRules := makeRulesJSON(t, smartplaylist.RuleSet{
		Rules: []smartplaylist.Rule{
			{Field: "artist", Operator: "is", Value: "Band B"},
		},
	})

	if _, err := db.ExecContext(
		"UPDATE playlists SET smart_rules = ? WHERE id = ?",
		bandBRules, summary.ID,
	); err != nil {
		t.Fatalf("failed to rewrite rules: %v", err)
	}

	// Snapshot is served as-is: still Band A's two tracks, not Band B.
	tracks, err = svc.GetSmartPlaylistTracks(summary.ID)
	if err != nil {
		t.Fatalf("GetSmartPlaylistTracks (post rule change) failed: %v", err)
	}

	if len(tracks) != 2 {
		t.Fatalf("snapshot re-read: got %d tracks, want 2 (must not re-evaluate)",
			len(tracks))
	}

	// An explicit refresh re-materializes against the current rules.
	if err := svc.RefreshSmartPlaylist(summary.ID); err != nil {
		t.Fatalf("RefreshSmartPlaylist failed: %v", err)
	}

	tracks, err = svc.GetSmartPlaylistTracks(summary.ID)
	if err != nil {
		t.Fatalf("GetSmartPlaylistTracks (post refresh) failed: %v", err)
	}

	if len(tracks) != 1 {
		t.Fatalf("post-refresh snapshot: got %d tracks, want 1", len(tracks))
	}

	if tracks[0].Artist != "Band B" {
		t.Errorf("post-refresh artist = %q, want Band B", tracks[0].Artist)
	}
}

// TestSmartPlaylistSaveRulesRematerializes verifies that saving new
// rules through UpdateSmartPlaylistRules refreshes the stored snapshot.
func TestSmartPlaylistSaveRulesRematerializes(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	seedSmartTestTracks(t, db)

	svc := newTestService(t, db)

	bandARules := makeRulesJSON(t, smartplaylist.RuleSet{
		Rules: []smartplaylist.Rule{
			{Field: "artist", Operator: "is", Value: "Band A"},
		},
	})

	summary, err := svc.CreateSmartPlaylist("Save Refresh", bandARules)
	if err != nil {
		t.Fatalf("CreateSmartPlaylist failed: %v", err)
	}

	bandBRules := makeRulesJSON(t, smartplaylist.RuleSet{
		Rules: []smartplaylist.Rule{
			{Field: "artist", Operator: "is", Value: "Band B"},
		},
	})

	if err := svc.UpdateSmartPlaylistRules(summary.ID, bandBRules); err != nil {
		t.Fatalf("UpdateSmartPlaylistRules failed: %v", err)
	}

	// The stored snapshot should already reflect the new rules without
	// any manual refresh.
	tracks, err := svc.GetSmartPlaylistTracks(summary.ID)
	if err != nil {
		t.Fatalf("GetSmartPlaylistTracks failed: %v", err)
	}

	if len(tracks) != 1 {
		t.Fatalf("got %d tracks, want 1", len(tracks))
	}

	if tracks[0].Artist != "Band B" {
		t.Errorf("artist = %q, want Band B", tracks[0].Artist)
	}
}

func TestSmartPlaylistCreateInvalidJSON(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	svc := newTestService(t, db)

	_, err := svc.CreateSmartPlaylist("Bad", "not json")
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestSmartPlaylistCreateEmptyName(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	svc := newTestService(t, db)

	rulesJSON := makeRulesJSON(t, smartplaylist.RuleSet{
		Rules: []smartplaylist.Rule{
			{Field: "title", Operator: "contains", Value: "test"},
		},
	})

	_, err := svc.CreateSmartPlaylist("", rulesJSON)
	if err == nil {
		t.Fatal("expected error for empty name, got nil")
	}
}

func TestSmartPlaylistEvaluateNonSmartPlaylist(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	svc := newTestService(t, db)

	// Create a regular playlist via direct SQL.
	// An INSERT ... RETURNING is a write, so it needs the writer:
	// QueryContext routes to the query-only read pool.
	var regularID int64
	if err := db.QueryRowWriter(
		`INSERT INTO playlists (name) VALUES (?) RETURNING id`,
		"Regular PL",
	).Scan(&regularID); err != nil {
		t.Fatalf("insert regular playlist: %v", err)
	}

	// Evaluate should fail — not a smart playlist.
	_, err := svc.EvaluateSmartPlaylist(regularID)
	if err == nil {
		t.Fatal("expected error evaluating non-smart playlist, got nil")
	}
}

func TestSmartPlaylistEvaluateNonExistent(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	svc := newTestService(t, db)

	// Evaluate a playlist ID that doesn't exist.
	_, err := svc.EvaluateSmartPlaylist(99999)
	if err == nil {
		t.Fatal("expected error evaluating non-existent playlist, got nil")
	}
}

func TestSmartPlaylistUpdateNonSmartPlaylist(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	svc := newTestService(t, db)

	// Create a regular playlist.  An INSERT ... RETURNING is a write,
	// so it needs the writer: QueryContext routes to the read pool.
	var regularID int64
	if err := db.QueryRowWriter(
		`INSERT INTO playlists (name) VALUES (?) RETURNING id`,
		"Regular PL",
	).Scan(&regularID); err != nil {
		t.Fatalf("insert regular playlist: %v", err)
	}

	rulesJSON := makeRulesJSON(t, smartplaylist.RuleSet{
		Rules: []smartplaylist.Rule{
			{Field: "title", Operator: "contains", Value: "test"},
		},
	})

	// Update should fail — not a smart playlist.
	err := svc.UpdateSmartPlaylistRules(regularID, rulesJSON)
	if err == nil {
		t.Fatal("expected error updating non-smart playlist, got nil")
	}
}

func TestSmartPlaylistUpdateInvalidJSON(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	svc := newTestService(t, db)

	// Create a real smart playlist first.
	rulesJSON := makeRulesJSON(t, smartplaylist.RuleSet{
		Rules: []smartplaylist.Rule{
			{Field: "title", Operator: "contains", Value: "test"},
		},
	})

	summary, err := svc.CreateSmartPlaylist("Valid PL", rulesJSON)
	if err != nil {
		t.Fatalf("CreateSmartPlaylist failed: %v", err)
	}

	// Update with invalid JSON.
	err = svc.UpdateSmartPlaylistRules(summary.ID, "bad json")
	if err == nil {
		t.Fatal("expected error for invalid JSON update, got nil")
	}
}

func TestSmartPlaylistGenreEvaluation(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	seedSmartTestTracks(t, db)

	svc := newTestService(t, db)

	rulesJSON := makeRulesJSON(t, smartplaylist.RuleSet{
		Rules: []smartplaylist.Rule{
			{Field: "genre", Operator: "is", Value: "Rock"},
		},
	})

	summary, err := svc.CreateSmartPlaylist("Genre Test", rulesJSON)
	if err != nil {
		t.Fatalf("CreateSmartPlaylist failed: %v", err)
	}

	tracks, err := svc.EvaluateSmartPlaylist(summary.ID)
	if err != nil {
		t.Fatalf("EvaluateSmartPlaylist failed: %v", err)
	}

	// Only track 1 ("Electric Song") has genre exactly "Rock".
	if len(tracks) != 1 {
		t.Fatalf("got %d tracks, want 1", len(tracks))
	}

	if tracks[0].TrackName != "Electric Song" {
		t.Errorf("track = %q, want Electric Song", tracks[0].TrackName)
	}
}

func TestSmartPlaylistYearNumericFilter(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	seedSmartTestTracks(t, db)

	svc := newTestService(t, db)

	rulesJSON := makeRulesJSON(t, smartplaylist.RuleSet{
		Rules: []smartplaylist.Rule{
			{Field: "year", Operator: "greater_than", Value: "2019"},
		},
	})

	summary, err := svc.CreateSmartPlaylist("Year Test", rulesJSON)
	if err != nil {
		t.Fatalf("CreateSmartPlaylist failed: %v", err)
	}

	tracks, err := svc.EvaluateSmartPlaylist(summary.ID)
	if err != nil {
		t.Fatalf("EvaluateSmartPlaylist failed: %v", err)
	}

	// Tracks 1 and 3 have year=2020, track 2 has year=2015.
	if len(tracks) != 2 {
		t.Fatalf("got %d tracks, want 2", len(tracks))
	}

	for _, tr := range tracks {
		if tr.Year <= 2019 {
			t.Errorf("track %q has year %d, want > 2019",
				tr.TrackName, tr.Year)
		}
	}
}

// ---------------------------------------------------------------------------
// Preview and GetRules tests
// ---------------------------------------------------------------------------

func TestSmartPlaylistPreview(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	seedSmartTestTracks(t, db)

	svc := newTestService(t, db)

	rulesJSON := makeRulesJSON(t, smartplaylist.RuleSet{
		Rules: []smartplaylist.Rule{
			{Field: "artist", Operator: "is", Value: "Band A"},
		},
	})

	// Create and evaluate via saved playlist for comparison.
	summary, err := svc.CreateSmartPlaylist("Preview Compare", rulesJSON)
	if err != nil {
		t.Fatalf("CreateSmartPlaylist failed: %v", err)
	}

	savedTracks, err := svc.EvaluateSmartPlaylist(summary.ID)
	if err != nil {
		t.Fatalf("EvaluateSmartPlaylist failed: %v", err)
	}

	// Preview with same rules — should return same tracks.
	previewTracks, err := svc.PreviewSmartPlaylist(rulesJSON)
	if err != nil {
		t.Fatalf("PreviewSmartPlaylist failed: %v", err)
	}

	if len(previewTracks) != len(savedTracks) {
		t.Fatalf(
			"preview returned %d tracks, saved returned %d",
			len(previewTracks), len(savedTracks),
		)
	}

	// Verify all preview tracks are Band A.
	for _, tr := range previewTracks {
		if tr.ArtistName != "Band A" {
			t.Errorf(
				"preview track %q has artist %q, want Band A",
				tr.TrackName, tr.ArtistName,
			)
		}
	}
}

func TestSmartPlaylistPreviewInvalidRules(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	svc := newTestService(t, db)

	_, err := svc.PreviewSmartPlaylist("not valid json")
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestSmartPlaylistGetRules(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	seedSmartTestTracks(t, db)

	svc := newTestService(t, db)

	rulesJSON := makeRulesJSON(t, smartplaylist.RuleSet{
		Rules: []smartplaylist.Rule{
			{Field: "genre", Operator: "is", Value: "Rock"},
		},
	})

	summary, err := svc.CreateSmartPlaylist("Get Rules Test", rulesJSON)
	if err != nil {
		t.Fatalf("CreateSmartPlaylist failed: %v", err)
	}

	got, err := svc.GetSmartPlaylistRules(summary.ID)
	if err != nil {
		t.Fatalf("GetSmartPlaylistRules failed: %v", err)
	}

	if got != rulesJSON {
		t.Errorf(
			"GetSmartPlaylistRules = %q, want %q",
			got, rulesJSON,
		)
	}
}

func TestSmartPlaylistGetRulesNotFound(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	svc := newTestService(t, db)

	_, err := svc.GetSmartPlaylistRules(99999)
	if err == nil {
		t.Fatal(
			"expected error for non-existent playlist, got nil",
		)
	}

	if err.Error() != errNotSmartPlaylist.Error() {
		t.Errorf(
			"error = %q, want %q",
			err.Error(), errNotSmartPlaylist.Error(),
		)
	}
}

func TestSmartPlaylistGetRulesRegularPlaylist(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	svc := newTestService(t, db)

	// Create a regular playlist via direct SQL.
	// An INSERT ... RETURNING is a write, so it needs the writer:
	// QueryContext routes to the query-only read pool.
	var regularID int64
	if err := db.QueryRowWriter(
		`INSERT INTO playlists (name) VALUES (?) RETURNING id`,
		"Regular PL For GetRules",
	).Scan(&regularID); err != nil {
		t.Fatalf("insert regular playlist: %v", err)
	}

	// GetSmartPlaylistRules should fail — not a smart playlist.
	_, err := svc.GetSmartPlaylistRules(regularID)
	if err == nil {
		t.Fatal(
			"expected error for regular playlist, got nil",
		)
	}

	if err.Error() != errNotSmartPlaylist.Error() {
		t.Errorf(
			"error = %q, want %q",
			err.Error(), errNotSmartPlaylist.Error(),
		)
	}
}
