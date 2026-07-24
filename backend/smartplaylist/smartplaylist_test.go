package smartplaylist

import (
	"strings"
	"testing"

	"yellowjacket/backend/database"
)

// ---------------------------------------------------------------------------
// Test helpers — seed data
// ---------------------------------------------------------------------------

// seedSmartPlaylistData inserts 8 tracks with the full FK chain
// required for smart playlist evaluation tests. Extends the
// seedSearchData pattern from search_test.go with a multi-genre
// track (ID 8) that has both "Rock" and "Alternative" genres.
//
// Track list:
//
//	ID 1: "Bohemian Rhapsody" by "Queen"   album "A Night at the Opera" (1975) genre=Rock
//	ID 2: "Halo"              by "Beyoncé" album "Lemonade"             (2008) genre=Pop
//	ID 3: "Back in Black"     by "AC/DC"   album "Back in Black"        (1980) genre=Hard Rock
//	ID 4: "Comfortably Numb"  by "Pink Floyd" album "The Dark Side"     (1979) genre=Progressive Rock
//	ID 5: "Another One"       by "Queen"   album "The Game"             (1980) genre=Funk Rock
//	ID 6: "Thunderstruck"     by "AC/DC"   album "The Razors Edge"      (1990) genre=Hard Rock
//	ID 7: "No One Knows"      by "QOTSA"   album "Rated R"             (2000) genre=Stoner Rock
//	ID 8: "Under the Bridge"  by "RHCP"    album "Blood Sugar"          (1991) genre=Rock+Alternative (multi-genre)
func seedSmartPlaylistData(t *testing.T, db *database.DB) {
	t.Helper()

	type track struct {
		id       int64
		filePath string
		title    string
		artist   string
		album    string
		trackNum *int64
		discNum  *int64
		year     int64
		genres   []string // supports multi-genre
		composer string
		lenMs    int64
		ftID     int64
		sr       int64
		bd       int64
		ch       int64
		br       int64
		fsize    int64
	}

	intPtr := func(v int64) *int64 { return &v }

	tracks := []track{
		{
			1, "/music/queen/bohemian_rhapsody.mp3",
			"Bohemian Rhapsody", "Queen",
			"A Night at the Opera", intPtr(11), intPtr(1),
			1975,
			[]string{"Rock"},
			"Freddie Mercury",
			354000, 0, 44100, 16, 2, 320000, 8500000,
		},
		{
			2, "/music/beyonce/halo.flac",
			"Halo", "Beyoncé", "Lemonade",
			intPtr(1), intPtr(1),
			2008,
			[]string{"Pop"},
			"Ryan Tedder",
			261000, 1, 96000, 24, 2, 1411000, 42000000,
		},
		{
			3, "/music/acdc/back_in_black.mp3",
			"Back in Black", "AC/DC", "Back in Black",
			intPtr(1), intPtr(1),
			1980,
			[]string{"Hard Rock"},
			"Angus Young",
			255000, 0, 44100, 16, 2, 320000, 6100000,
		},
		{
			4, "/music/pinkfloyd/comfortably_numb.flac",
			"Comfortably Numb", "Pink Floyd", "The Dark Side",
			intPtr(6), intPtr(1),
			1979,
			[]string{"Progressive Rock"},
			"David Gilmour",
			382000, 1, 96000, 24, 2, 1411000, 54000000,
		},
		{
			5, "/music/queen/another_one.mp3",
			"Another One Bites the Dust", "Queen", "The Game",
			intPtr(3), intPtr(1),
			1980,
			[]string{"Funk Rock"},
			"John Deacon",
			215000, 0, 44100, 16, 2, 320000, 5200000,
		},
		{
			6, "/music/acdc/thunderstruck.mp3",
			"Thunderstruck", "AC/DC", "The Razors Edge",
			intPtr(1), intPtr(1),
			1990,
			[]string{"Hard Rock"},
			"Angus Young",
			292000, 0, 44100, 16, 2, 320000, 7000000,
		},
		{
			7, "/music/qotsa/no_one_knows.mp3",
			"No One Knows", "QOTSA", "Rated R",
			intPtr(1), intPtr(1),
			2000,
			[]string{"Stoner Rock"},
			"Josh Homme",
			310000, 0, 44100, 16, 2, 320000, 7400000,
		},
		{
			8, "/music/rhcp/under_the_bridge.mp3",
			"Under the Bridge", "RHCP", "Blood Sugar",
			intPtr(2), intPtr(1),
			1991,
			[]string{"Rock", "Alternative"},
			"Anthony Kiedis",
			264000, 0, 44100, 16, 2, 320000, 6300000,
		},
	}

	// Build unique sets for artist_credit and release_groups.
	artistMap := map[string]int64{}
	albumMap := map[string]int64{}

	var artistID, albumID int64

	for _, tr := range tracks {
		if _, ok := artistMap[tr.artist]; !ok {
			artistID++
			artistMap[tr.artist] = artistID
		}

		if _, ok := albumMap[tr.album]; !ok {
			albumID++
			albumMap[tr.album] = albumID
		}
	}

	// Insert artist_credit rows.
	for text, id := range artistMap {
		_, err := db.ExecContext(
			"INSERT INTO artist_credit (id, text) VALUES (?, ?)",
			id, text,
		)
		if err != nil {
			t.Fatalf("insert artist_credit %q: %v", text, err)
		}
	}

	// Insert release_groups.
	for name, id := range albumMap {
		_, err := db.ExecContext(
			"INSERT INTO release_groups (id, name) VALUES (?, ?)",
			id, name,
		)
		if err != nil {
			t.Fatalf("insert release_group %q: %v", name, err)
		}
	}

	// Insert genres.
	genreMap := map[string]int64{}

	var genreID int64

	for _, tr := range tracks {
		for _, g := range tr.genres {
			if _, ok := genreMap[g]; !ok {
				genreID++
				genreMap[g] = genreID

				_, err := db.ExecContext(
					"INSERT INTO genres (id, name) VALUES (?, ?)",
					genreID, g,
				)
				if err != nil {
					t.Fatalf("insert genre %q: %v", g, err)
				}
			}
		}
	}

	// Insert tracks with full FK chain.
	for _, tr := range tracks {
		acID := artistMap[tr.artist]
		rgID := albumMap[tr.album]

		// Insert recording.
		_, err := db.ExecContext(
			"INSERT INTO recordings (id, name, artist_credit_id, "+
				"track_number, disc_number, year, composer) "+
				"VALUES (?, ?, ?, ?, ?, ?, ?)",
			tr.id, tr.title, acID, tr.trackNum, tr.discNum,
			tr.year, tr.composer,
		)
		if err != nil {
			t.Fatalf("insert recording %d %q: %v",
				tr.id, tr.title, err)
		}

		// Insert audio_files.
		_, err = db.ExecContext(
			"INSERT INTO audio_files (id, file_path, "+
				"length_milliseconds, file_type_id, recording_id, "+
				"sample_rate, bit_depth, channels, bitrate, file_size) "+
				"VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			tr.id, tr.filePath, tr.lenMs, tr.ftID, tr.id,
			tr.sr, tr.bd, tr.ch, tr.br, tr.fsize,
		)
		if err != nil {
			t.Fatalf("insert audio_file %d: %v", tr.id, err)
		}

		// Link recording to release_group.
		_, err = db.ExecContext(
			"INSERT INTO release_group_recordings "+
				"(release_group_id, recording_id, track_number, disc_number) "+
				"VALUES (?, ?, ?, ?)",
			rgID, tr.id, tr.trackNum, tr.discNum,
		)
		if err != nil {
			t.Fatalf("insert release_group_recordings %d→%d: %v",
				rgID, tr.id, err)
		}

		// Insert recording_genres links (supports multi-genre).
		for _, g := range tr.genres {
			gID := genreMap[g]

			_, err = db.ExecContext(
				"INSERT INTO recording_genres "+
					"(recording_id, genre_id) VALUES (?, ?)",
				tr.id, gID,
			)
			if err != nil {
				t.Fatalf(
					"insert recording_genres %d→%d: %v",
					tr.id, gID, err,
				)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// BuildWhereClause tests (pure — no DB needed)
// ---------------------------------------------------------------------------

func TestBuildWhereClause_TextIs(t *testing.T) {
	t.Parallel()

	clause, args, err := BuildWhereClause([]Rule{
		{Field: "artist", Operator: "is", Value: "Queen"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if clause != "artist_name = ? COLLATE NOCASE" {
		t.Errorf("clause = %q, want %q", clause, "artist_name = ? COLLATE NOCASE")
	}

	if len(args) != 1 || args[0] != "Queen" {
		t.Errorf("args = %v, want [Queen]", args)
	}
}

func TestBuildWhereClause_TextIsNot(t *testing.T) {
	t.Parallel()

	clause, args, err := BuildWhereClause([]Rule{
		{Field: "artist", Operator: "is_not", Value: "Queen"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if clause != "artist_name != ? COLLATE NOCASE" {
		t.Errorf("clause = %q, want %q",
			clause, "artist_name != ? COLLATE NOCASE")
	}

	if len(args) != 1 || args[0] != "Queen" {
		t.Errorf("args = %v, want [Queen]", args)
	}
}

func TestBuildWhereClause_TextContains(t *testing.T) {
	t.Parallel()

	clause, args, err := BuildWhereClause([]Rule{
		{Field: "title", Operator: "contains", Value: "Black"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if clause != "title LIKE ?" {
		t.Errorf("clause = %q, want %q", clause, "title LIKE ?")
	}

	if len(args) != 1 || args[0] != "%Black%" {
		t.Errorf("args = %v, want [%%Black%%]", args)
	}
}

func TestBuildWhereClause_TextDoesNotContain(t *testing.T) {
	t.Parallel()

	clause, args, err := BuildWhereClause([]Rule{
		{
			Field: "title", Operator: "does_not_contain",
			Value: "Black",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if clause != "title NOT LIKE ?" {
		t.Errorf("clause = %q, want %q",
			clause, "title NOT LIKE ?")
	}

	if len(args) != 1 || args[0] != "%Black%" {
		t.Errorf("args = %v, want [%%Black%%]", args)
	}
}

func TestBuildWhereClause_TextStartsWith(t *testing.T) {
	t.Parallel()

	clause, args, err := BuildWhereClause([]Rule{
		{Field: "title", Operator: "starts_with", Value: "Back"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if clause != "title LIKE ?" {
		t.Errorf("clause = %q, want %q", clause, "title LIKE ?")
	}

	if len(args) != 1 || args[0] != "Back%" {
		t.Errorf("args = %v, want [Back%%]", args)
	}
}

func TestBuildWhereClause_TextEndsWith(t *testing.T) {
	t.Parallel()

	clause, args, err := BuildWhereClause([]Rule{
		{Field: "title", Operator: "ends_with", Value: "Black"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if clause != "title LIKE ?" {
		t.Errorf("clause = %q, want %q", clause, "title LIKE ?")
	}

	if len(args) != 1 || args[0] != "%Black" {
		t.Errorf("args = %v, want [%%Black]", args)
	}
}

func TestBuildWhereClause_TextIsAnyOf(t *testing.T) {
	t.Parallel()

	clause, args, err := BuildWhereClause([]Rule{
		{
			Field: "artist", Operator: "is_any_of",
			Value: `["Queen","AC/DC"]`,
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if clause != "artist_name IN (? COLLATE NOCASE, ? COLLATE NOCASE)" {
		t.Errorf("clause = %q, want %q",
			clause, "artist_name IN (? COLLATE NOCASE, ? COLLATE NOCASE)")
	}

	if len(args) != 2 || args[0] != "Queen" || args[1] != "AC/DC" {
		t.Errorf("args = %v, want [Queen AC/DC]", args)
	}
}

func TestBuildWhereClause_NumericIs(t *testing.T) {
	t.Parallel()

	clause, args, err := BuildWhereClause([]Rule{
		{Field: "year", Operator: "is", Value: "1980"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if clause != "year = ?" {
		t.Errorf("clause = %q, want %q", clause, "year = ?")
	}

	if len(args) != 1 || args[0] != int64(1980) {
		t.Errorf("args = %v, want [1980]", args)
	}
}

func TestBuildWhereClause_NumericIsNot(t *testing.T) {
	t.Parallel()

	clause, args, err := BuildWhereClause([]Rule{
		{Field: "year", Operator: "is_not", Value: "1980"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if clause != "year != ?" {
		t.Errorf("clause = %q, want %q", clause, "year != ?")
	}

	if len(args) != 1 || args[0] != int64(1980) {
		t.Errorf("args = %v, want [1980]", args)
	}
}

func TestBuildWhereClause_NumericGreaterThan(t *testing.T) {
	t.Parallel()

	clause, args, err := BuildWhereClause([]Rule{
		{Field: "year", Operator: "greater_than", Value: "2000"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if clause != "year > ?" {
		t.Errorf("clause = %q, want %q", clause, "year > ?")
	}

	if len(args) != 1 || args[0] != int64(2000) {
		t.Errorf("args = %v, want [2000]", args)
	}
}

func TestBuildWhereClause_NumericLessThan(t *testing.T) {
	t.Parallel()

	clause, args, err := BuildWhereClause([]Rule{
		{Field: "year", Operator: "less_than", Value: "1980"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if clause != "year < ?" {
		t.Errorf("clause = %q, want %q", clause, "year < ?")
	}

	if len(args) != 1 || args[0] != int64(1980) {
		t.Errorf("args = %v, want [1980]", args)
	}
}

func TestBuildWhereClause_NumericBetween(t *testing.T) {
	t.Parallel()

	clause, args, err := BuildWhereClause([]Rule{
		{
			Field: "year", Operator: "between",
			Value: "1975,1985",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if clause != "year BETWEEN ? AND ?" {
		t.Errorf("clause = %q, want %q",
			clause, "year BETWEEN ? AND ?")
	}

	if len(args) != 2 || args[0] != int64(1975) || args[1] != int64(1985) {
		t.Errorf("args = %v, want [1975 1985]", args)
	}
}

func TestBuildWhereClause_NumericBetweenJSON(t *testing.T) {
	t.Parallel()

	clause, args, err := BuildWhereClause([]Rule{
		{
			Field: "year", Operator: "between",
			Value: `["1975","1985"]`,
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if clause != "year BETWEEN ? AND ?" {
		t.Errorf("clause = %q, want %q",
			clause, "year BETWEEN ? AND ?")
	}

	if len(args) != 2 || args[0] != int64(1975) || args[1] != int64(1985) {
		t.Errorf("args = %v, want [1975 1985]", args)
	}
}

func TestBuildWhereClause_GenreIsProducesSubquery(t *testing.T) {
	t.Parallel()

	clause, args, err := BuildWhereClause([]Rule{
		{Field: "genre", Operator: "is", Value: "Rock"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Must use subquery, NOT "genre = ?"
	if strings.Contains(clause, "genre =") {
		t.Errorf(
			"genre 'is' should use subquery, not direct column match: %q",
			clause,
		)
	}

	if !strings.Contains(clause, "recording_genres") {
		t.Errorf("genre 'is' should reference recording_genres: %q",
			clause)
	}

	if !strings.Contains(clause, "g.name = ? COLLATE NOCASE") {
		t.Errorf("genre 'is' should have g.name = ? COLLATE NOCASE: %q", clause)
	}

	if len(args) != 1 || args[0] != "Rock" {
		t.Errorf("args = %v, want [Rock]", args)
	}
}

func TestBuildWhereClause_GenreIsNotProducesSubquery(t *testing.T) {
	t.Parallel()

	clause, args, err := BuildWhereClause([]Rule{
		{Field: "genre", Operator: "is_not", Value: "Rock"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(clause, "NOT IN") {
		t.Errorf("genre 'is_not' should use NOT IN: %q", clause)
	}

	if !strings.Contains(clause, "recording_genres") {
		t.Errorf(
			"genre 'is_not' should reference recording_genres: %q",
			clause,
		)
	}

	if len(args) != 1 || args[0] != "Rock" {
		t.Errorf("args = %v, want [Rock]", args)
	}
}

func TestBuildWhereClause_GenreIsAnyOfProducesSubquery(t *testing.T) {
	t.Parallel()

	clause, args, err := BuildWhereClause([]Rule{
		{
			Field: "genre", Operator: "is_any_of",
			Value: `["Rock","Pop"]`,
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(clause, "recording_genres") {
		t.Errorf(
			"genre 'is_any_of' should reference recording_genres: %q",
			clause,
		)
	}

	if !strings.Contains(clause, "g.name IN (? COLLATE NOCASE, ? COLLATE NOCASE)") {
		t.Errorf(
			"genre 'is_any_of' should have g.name IN (? COLLATE NOCASE, ? COLLATE NOCASE): %q",
			clause,
		)
	}

	if len(args) != 2 || args[0] != "Rock" || args[1] != "Pop" {
		t.Errorf("args = %v, want [Rock Pop]", args)
	}
}

func TestBuildWhereClause_GenreContainsUsesSubquery(t *testing.T) {
	t.Parallel()

	clause, args, err := BuildWhereClause([]Rule{
		{Field: "genre", Operator: "contains", Value: "Rock"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Since the smart playlist query no longer projects a concatenated
	// genre column, "contains" filters genres via recording_genres
	// with g.name LIKE applied to individual genre rows.
	if !strings.Contains(clause, "recording_genres") {
		t.Errorf(
			"genre 'contains' should use recording_genres subquery: %q",
			clause,
		)
	}

	if !strings.Contains(clause, "g.name LIKE ?") {
		t.Errorf(
			"genre 'contains' should filter with g.name LIKE ?: %q",
			clause,
		)
	}

	if len(args) != 1 || args[0] != "%Rock%" {
		t.Errorf("args = %v, want [%%Rock%%]", args)
	}
}

func TestBuildWhereClause_MultipleRulesAND(t *testing.T) {
	t.Parallel()

	clause, args, err := BuildWhereClause([]Rule{
		{Field: "artist", Operator: "is", Value: "Queen"},
		{Field: "year", Operator: "greater_than", Value: "1975"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if clause != "artist_name = ? COLLATE NOCASE AND year > ?" {
		t.Errorf("clause = %q, want %q",
			clause, "artist_name = ? COLLATE NOCASE AND year > ?")
	}

	if len(args) != 2 || args[0] != "Queen" || args[1] != int64(1975) {
		t.Errorf("args = %v, want [Queen 1975]", args)
	}
}

func TestBuildWhereClause_SameFieldMultipleTimes(t *testing.T) {
	t.Parallel()

	clause, args, err := BuildWhereClause([]Rule{
		{Field: "genre", Operator: "contains", Value: "Rock"},
		{
			Field: "genre", Operator: "does_not_contain",
			Value: "Punk",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Genre text ops combine via AND across subqueries against
	// recording_genres; the exact SQL shape is asserted elsewhere.
	if !strings.Contains(clause, " AND ") {
		t.Errorf("clause should combine rules with AND: %q", clause)
	}

	if !strings.Contains(clause, "af.recording_id IN") {
		t.Errorf("clause should include positive IN subquery: %q", clause)
	}

	if !strings.Contains(clause, "af.recording_id NOT IN") {
		t.Errorf("clause should include NOT IN subquery: %q", clause)
	}

	if len(args) != 2 ||
		args[0] != "%Rock%" || args[1] != "%Punk%" {
		t.Errorf("args = %v, want [%%Rock%% %%Punk%%]", args)
	}
}

func TestBuildWhereClause_EmptyRules(t *testing.T) {
	t.Parallel()

	clause, args, err := BuildWhereClause(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if clause != "" {
		t.Errorf("clause = %q, want empty string", clause)
	}

	if args != nil {
		t.Errorf("args = %v, want nil", args)
	}
}

func TestBuildWhereClause_InvalidField(t *testing.T) {
	t.Parallel()

	_, _, err := BuildWhereClause([]Rule{
		{
			Field: "nonexistent", Operator: "is",
			Value: "anything",
		},
	})
	if err == nil {
		t.Fatal("expected error for invalid field, got nil")
	}

	if !strings.Contains(err.Error(), "invalid field") {
		t.Errorf(
			"error should mention 'invalid field': %v", err,
		)
	}

	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf(
			"error should include the field name: %v", err,
		)
	}
}

func TestBuildWhereClause_InvalidOperatorForNumeric(t *testing.T) {
	t.Parallel()

	_, _, err := BuildWhereClause([]Rule{
		{Field: "year", Operator: "contains", Value: "1980"},
	})
	if err == nil {
		t.Fatal(
			"expected error for text operator on numeric field",
		)
	}

	if !strings.Contains(err.Error(), "invalid operator") {
		t.Errorf(
			"error should mention 'invalid operator': %v", err,
		)
	}
}

func TestBuildWhereClause_InvalidOperatorForText(t *testing.T) {
	t.Parallel()

	_, _, err := BuildWhereClause([]Rule{
		{
			Field: "artist", Operator: "greater_than",
			Value: "Queen",
		},
	})
	if err == nil {
		t.Fatal(
			"expected error for numeric operator on text field",
		)
	}

	if !strings.Contains(err.Error(), "invalid operator") {
		t.Errorf(
			"error should mention 'invalid operator': %v", err,
		)
	}
}

// ---------------------------------------------------------------------------
// Evaluate tests (with DB)
// ---------------------------------------------------------------------------

func TestEvaluate_TextIs(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	seedSmartPlaylistData(t, db)

	tracks, err := Evaluate(db, RuleSet{
		Rules: []Rule{
			{Field: "artist", Operator: "is", Value: "Queen"},
		},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	if len(tracks) != 2 {
		t.Fatalf("got %d tracks, want 2", len(tracks))
	}

	for _, tr := range tracks {
		if tr.ArtistName != "Queen" {
			t.Errorf(
				"track %q has artist %q, want Queen",
				tr.TrackName, tr.ArtistName,
			)
		}
	}
}

func TestEvaluate_TextContains(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	seedSmartPlaylistData(t, db)

	tracks, err := Evaluate(db, RuleSet{
		Rules: []Rule{
			{
				Field: "title", Operator: "contains",
				Value: "Black",
			},
		},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	if len(tracks) != 1 {
		t.Fatalf("got %d tracks, want 1", len(tracks))
	}

	if tracks[0].TrackName != "Back in Black" {
		t.Errorf("got %q, want %q",
			tracks[0].TrackName, "Back in Black")
	}
}

func TestEvaluate_TextStartsWith(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	seedSmartPlaylistData(t, db)

	tracks, err := Evaluate(db, RuleSet{
		Rules: []Rule{
			{
				Field: "title", Operator: "starts_with",
				Value: "Back",
			},
		},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	if len(tracks) != 1 {
		t.Fatalf("got %d tracks, want 1", len(tracks))
	}

	if tracks[0].TrackName != "Back in Black" {
		t.Errorf("got %q, want %q",
			tracks[0].TrackName, "Back in Black")
	}
}

func TestEvaluate_TextEndsWith(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	seedSmartPlaylistData(t, db)

	tracks, err := Evaluate(db, RuleSet{
		Rules: []Rule{
			{
				Field: "title", Operator: "ends_with",
				Value: "Numb",
			},
		},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	if len(tracks) != 1 {
		t.Fatalf("got %d tracks, want 1", len(tracks))
	}

	if tracks[0].TrackName != "Comfortably Numb" {
		t.Errorf("got %q, want %q",
			tracks[0].TrackName, "Comfortably Numb")
	}
}

func TestEvaluate_TextDoesNotContain(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	seedSmartPlaylistData(t, db)

	tracks, err := Evaluate(db, RuleSet{
		Rules: []Rule{
			{
				Field: "title", Operator: "does_not_contain",
				Value: "the",
			},
		},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	// "the" appears in: "Another One Bites the Dust",
	// "Under the Bridge". The rest should be returned.
	for _, tr := range tracks {
		if strings.Contains(
			strings.ToLower(tr.TrackName), "the") {
			t.Errorf(
				"track %q should not contain 'the'",
				tr.TrackName,
			)
		}
	}
}

func TestEvaluate_TextIsAnyOf(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	seedSmartPlaylistData(t, db)

	tracks, err := Evaluate(db, RuleSet{
		Rules: []Rule{
			{
				Field: "artist", Operator: "is_any_of",
				Value: `["Queen","AC/DC"]`,
			},
		},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	if len(tracks) != 4 {
		t.Fatalf("got %d tracks, want 4 (2 Queen + 2 AC/DC)",
			len(tracks))
	}

	for _, tr := range tracks {
		if tr.ArtistName != "Queen" &&
			tr.ArtistName != "AC/DC" {
			t.Errorf(
				"unexpected artist %q", tr.ArtistName,
			)
		}
	}
}

func TestEvaluate_NumericGreaterThan(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	seedSmartPlaylistData(t, db)

	tracks, err := Evaluate(db, RuleSet{
		Rules: []Rule{
			{
				Field: "year", Operator: "greater_than",
				Value: "2000",
			},
		},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	// Only "Halo" (2008) has year > 2000.
	if len(tracks) != 1 {
		t.Fatalf("got %d tracks, want 1", len(tracks))
	}

	if tracks[0].TrackName != "Halo" {
		t.Errorf("got %q, want Halo", tracks[0].TrackName)
	}
}

func TestEvaluate_NumericLessThan(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	seedSmartPlaylistData(t, db)

	tracks, err := Evaluate(db, RuleSet{
		Rules: []Rule{
			{
				Field: "year", Operator: "less_than",
				Value: "1980",
			},
		},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	// Bohemian Rhapsody (1975) and Comfortably Numb (1979)
	if len(tracks) != 2 {
		t.Fatalf("got %d tracks, want 2", len(tracks))
	}

	for _, tr := range tracks {
		if tr.Year >= 1980 {
			t.Errorf("track %q year=%d should be < 1980",
				tr.TrackName, tr.Year)
		}
	}
}

func TestEvaluate_NumericBetween(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	seedSmartPlaylistData(t, db)

	tracks, err := Evaluate(db, RuleSet{
		Rules: []Rule{
			{
				Field: "year", Operator: "between",
				Value: "1975,1985",
			},
		},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	// 1975: Bohemian Rhapsody, 1979: Comfortably Numb,
	// 1980: Back in Black, 1980: Another One Bites the Dust
	if len(tracks) != 4 {
		t.Fatalf("got %d tracks, want 4", len(tracks))
	}

	for _, tr := range tracks {
		if tr.Year < 1975 || tr.Year > 1985 {
			t.Errorf(
				"track %q year=%d outside 1975-1985",
				tr.TrackName, tr.Year,
			)
		}
	}
}

func TestEvaluate_GenreIs_MultiGenreTrack(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	seedSmartPlaylistData(t, db)

	// Genre "is Rock" must match track 8 (Rock+Alternative) and
	// track 1 (Rock). This proves the subquery works correctly
	// with multi-genre tracks.
	tracks, err := Evaluate(db, RuleSet{
		Rules: []Rule{
			{Field: "genre", Operator: "is", Value: "Rock"},
		},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	if len(tracks) != 2 {
		names := make([]string, len(tracks))
		for i, tr := range tracks {
			names[i] = tr.TrackName
		}

		t.Fatalf(
			"genre 'is Rock' got %d tracks %v, want 2 "+
				"(Bohemian Rhapsody + Under the Bridge)",
			len(tracks), names,
		)
	}

	foundBR := false
	foundUTB := false

	for _, tr := range tracks {
		if tr.TrackName == "Bohemian Rhapsody" {
			foundBR = true
		}

		if tr.TrackName == "Under the Bridge" {
			foundUTB = true
		}
	}

	if !foundBR {
		t.Error("genre 'is Rock' missing Bohemian Rhapsody")
	}

	if !foundUTB {
		t.Error(
			"genre 'is Rock' missing Under the Bridge " +
				"(multi-genre track)",
		)
	}
}

func TestEvaluate_GenreIsNot_MultiGenreTrack(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	seedSmartPlaylistData(t, db)

	// Genre "is_not Rock" must NOT return tracks 1 or 8.
	tracks, err := Evaluate(db, RuleSet{
		Rules: []Rule{
			{
				Field: "genre", Operator: "is_not",
				Value: "Rock",
			},
		},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	for _, tr := range tracks {
		if tr.TrackName == "Bohemian Rhapsody" ||
			tr.TrackName == "Under the Bridge" {
			t.Errorf(
				"genre 'is_not Rock' should not return %q",
				tr.TrackName,
			)
		}
	}

	// Should return the other 6 tracks.
	if len(tracks) != 6 {
		t.Errorf("got %d tracks, want 6", len(tracks))
	}
}

func TestEvaluate_GenreContains(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	seedSmartPlaylistData(t, db)

	// "contains Rock" on the concatenated genre column should match
	// any track that has "Rock" anywhere in its genre string.
	tracks, err := Evaluate(db, RuleSet{
		Rules: []Rule{
			{
				Field: "genre", Operator: "contains",
				Value: "Rock",
			},
		},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	// Rock (1,8), Hard Rock (3,6), Progressive Rock (4),
	// Funk Rock (5), Stoner Rock (7) = 7 tracks
	if len(tracks) != 7 {
		names := make([]string, len(tracks))
		for i, tr := range tracks {
			names[i] = tr.TrackName
		}

		t.Fatalf(
			"genre 'contains Rock' got %d tracks %v, want 7",
			len(tracks), names,
		)
	}
}

func TestEvaluate_MultipleRulesAND(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	seedSmartPlaylistData(t, db)

	// genre contains "Rock" AND year > 1970 AND year < 1981
	tracks, err := Evaluate(db, RuleSet{
		Rules: []Rule{
			{
				Field: "genre", Operator: "contains",
				Value: "Rock",
			},
			{
				Field: "year", Operator: "greater_than",
				Value: "1970",
			},
			{
				Field: "year", Operator: "less_than",
				Value: "1981",
			},
		},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	// Bohemian Rhapsody (1975, Rock), Comfortably Numb (1979, Progressive Rock),
	// Back in Black (1980, Hard Rock), Another One (1980, Funk Rock)
	if len(tracks) != 4 {
		names := make([]string, len(tracks))
		for i, tr := range tracks {
			names[i] = tr.TrackName
		}

		t.Fatalf("got %d tracks %v, want 4", len(tracks), names)
	}
}

func TestEvaluate_EmptyRulesReturnsAll(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	seedSmartPlaylistData(t, db)

	tracks, err := Evaluate(db, RuleSet{})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	if len(tracks) != 8 {
		t.Fatalf("got %d tracks, want 8 (all seeded)",
			len(tracks))
	}
}

func TestEvaluate_Limit(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	seedSmartPlaylistData(t, db)

	tracks, err := Evaluate(db, RuleSet{Limit: 3})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	if len(tracks) != 3 {
		t.Fatalf("got %d tracks, want 3 (limited)",
			len(tracks))
	}
}

func TestEvaluate_SortByYearASC(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	seedSmartPlaylistData(t, db)

	tracks, err := Evaluate(db, RuleSet{
		SortField: "year",
		SortDir:   "ASC",
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	if len(tracks) < 2 {
		t.Fatalf("got %d tracks, want >= 2", len(tracks))
	}

	for i := 1; i < len(tracks); i++ {
		if tracks[i].Year < tracks[i-1].Year {
			t.Errorf(
				"sort ASC violated: track[%d].Year=%d < "+
					"track[%d].Year=%d",
				i, tracks[i].Year, i-1, tracks[i-1].Year,
			)
		}
	}
}

func TestEvaluate_SortByYearDESC(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	seedSmartPlaylistData(t, db)

	tracks, err := Evaluate(db, RuleSet{
		SortField: "year",
		SortDir:   "DESC",
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	if len(tracks) < 2 {
		t.Fatalf("got %d tracks, want >= 2", len(tracks))
	}

	for i := 1; i < len(tracks); i++ {
		if tracks[i].Year > tracks[i-1].Year {
			t.Errorf(
				"sort DESC violated: track[%d].Year=%d > "+
					"track[%d].Year=%d",
				i, tracks[i].Year, i-1, tracks[i-1].Year,
			)
		}
	}
}

func TestEvaluate_SortByRandom(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	seedSmartPlaylistData(t, db)

	// Just verify it doesn't error.
	tracks, err := Evaluate(db, RuleSet{
		SortField: "random",
	})
	if err != nil {
		t.Fatalf("Evaluate with random sort: %v", err)
	}

	if len(tracks) != 8 {
		t.Fatalf("got %d tracks, want 8", len(tracks))
	}
}

func TestEvaluate_InvalidField(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	seedSmartPlaylistData(t, db)

	_, err := Evaluate(db, RuleSet{
		Rules: []Rule{
			{Field: "bogus", Operator: "is", Value: "x"},
		},
	})
	if err == nil {
		t.Fatal("expected error for invalid field, got nil")
	}

	if !strings.Contains(err.Error(), "invalid field") {
		t.Errorf("error should mention 'invalid field': %v",
			err)
	}
}

// ---------------------------------------------------------------------------
// SQL injection tests
// ---------------------------------------------------------------------------

func TestSQLInjection_FieldName(t *testing.T) {
	t.Parallel()

	_, _, err := BuildWhereClause([]Rule{
		{
			Field:    "title; DROP TABLE playlists",
			Operator: "is", Value: "x",
		},
	})
	if err == nil {
		t.Fatal(
			"expected error for injected field name, got nil",
		)
	}

	if !strings.Contains(err.Error(), "invalid field") {
		t.Errorf("error should mention 'invalid field': %v",
			err)
	}
}

func TestSQLInjection_Value(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	seedSmartPlaylistData(t, db)

	// Value with SQL injection — should produce no error (safe
	// parameterization) and return 0 results.
	tracks, err := Evaluate(db, RuleSet{
		Rules: []Rule{
			{
				Field: "title", Operator: "is",
				Value: "'; DROP TABLE playlists; --",
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error with injection value: %v",
			err)
	}

	if len(tracks) != 0 {
		t.Errorf("got %d tracks, want 0", len(tracks))
	}
}

// ---------------------------------------------------------------------------
// ParseRuleSet tests
// ---------------------------------------------------------------------------

func TestParseRuleSet_Valid(t *testing.T) {
	t.Parallel()

	input := `{
		"rules": [
			{"field": "artist", "operator": "is", "value": "Queen"}
		],
		"limit": 50,
		"sort_field": "year",
		"sort_dir": "DESC"
	}`

	rs, err := ParseRuleSet(input)
	if err != nil {
		t.Fatalf("ParseRuleSet: %v", err)
	}

	if len(rs.Rules) != 1 {
		t.Fatalf("got %d rules, want 1", len(rs.Rules))
	}

	if rs.Rules[0].Field != "artist" {
		t.Errorf("Field = %q, want artist",
			rs.Rules[0].Field)
	}

	if rs.Limit != 50 {
		t.Errorf("Limit = %d, want 50", rs.Limit)
	}

	if rs.SortField != "year" {
		t.Errorf("SortField = %q, want year", rs.SortField)
	}

	if rs.SortDir != "DESC" {
		t.Errorf("SortDir = %q, want DESC", rs.SortDir)
	}
}

func TestParseRuleSet_Invalid(t *testing.T) {
	t.Parallel()

	_, err := ParseRuleSet("not json at all")
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}

	if !strings.Contains(err.Error(), "invalid smart playlist") {
		t.Errorf(
			"error should mention 'invalid smart playlist': %v",
			err,
		)
	}
}

func TestParseRuleSet_EmptyRules(t *testing.T) {
	t.Parallel()

	rs, err := ParseRuleSet(`{"rules":[]}`)
	if err != nil {
		t.Fatalf("ParseRuleSet: %v", err)
	}

	if len(rs.Rules) != 0 {
		t.Errorf("got %d rules, want 0", len(rs.Rules))
	}
}

// ---------------------------------------------------------------------------
// Track field mapping test
// ---------------------------------------------------------------------------

func TestEvaluate_TrackFieldMapping(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	seedSmartPlaylistData(t, db)

	// Fetch Bohemian Rhapsody and verify all library.Track fields
	// are correctly populated.
	tracks, err := Evaluate(db, RuleSet{
		Rules: []Rule{
			{
				Field: "title", Operator: "is",
				Value: "Bohemian Rhapsody",
			},
		},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	if len(tracks) != 1 {
		t.Fatalf("got %d tracks, want 1", len(tracks))
	}

	tr := tracks[0]

	if tr.TrackName != "Bohemian Rhapsody" {
		t.Errorf("TrackName = %q", tr.TrackName)
	}

	if tr.ArtistName != "Queen" {
		t.Errorf("ArtistName = %q", tr.ArtistName)
	}

	if tr.TrackLength != "354000" {
		t.Errorf("TrackLength = %q, want 354000",
			tr.TrackLength)
	}

	if tr.FilePath != "/music/queen/bohemian_rhapsody.mp3" {
		t.Errorf("FilePath = %q", tr.FilePath)
	}

	if tr.TrackNumber != 11 {
		t.Errorf("TrackNumber = %d, want 11", tr.TrackNumber)
	}

	if tr.DiscNumber != 1 {
		t.Errorf("DiscNumber = %d, want 1", tr.DiscNumber)
	}

	if tr.Album != "A Night at the Opera" {
		t.Errorf("Album = %q", tr.Album)
	}

	if len(tr.Genre) != 1 || tr.Genre[0] != "Rock" {
		t.Errorf("Genre = %v, want [Rock]", tr.Genre)
	}

	if tr.Year != 1975 {
		t.Errorf("Year = %d, want 1975", tr.Year)
	}

	if tr.Composer != "Freddie Mercury" {
		t.Errorf("Composer = %q", tr.Composer)
	}

	if tr.FileType != ".mp3" {
		t.Errorf("FileType = %q, want .mp3", tr.FileType)
	}

	if tr.SampleRate != 44100 {
		t.Errorf("SampleRate = %d", tr.SampleRate)
	}

	if tr.BitDepth != 16 {
		t.Errorf("BitDepth = %d", tr.BitDepth)
	}

	if tr.Channels != 2 {
		t.Errorf("Channels = %d", tr.Channels)
	}

	if tr.Bitrate != 320000 {
		t.Errorf("Bitrate = %d", tr.Bitrate)
	}

	if tr.FileSize != 8500000 {
		t.Errorf("FileSize = %d", tr.FileSize)
	}
}

// TestEvaluate_MultiGenreTrackGenreField verifies that a track with
// multiple genres has them split correctly into the []string field.
func TestEvaluate_MultiGenreTrackGenreField(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	seedSmartPlaylistData(t, db)

	tracks, err := Evaluate(db, RuleSet{
		Rules: []Rule{
			{
				Field: "title", Operator: "is",
				Value: "Under the Bridge",
			},
		},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	if len(tracks) != 1 {
		t.Fatalf("got %d tracks, want 1", len(tracks))
	}

	tr := tracks[0]

	if len(tr.Genre) != 2 {
		t.Fatalf("Genre = %v, want 2 genres", tr.Genre)
	}

	hasRock := false
	hasAlt := false

	for _, g := range tr.Genre {
		if g == "Rock" {
			hasRock = true
		}

		if g == "Alternative" {
			hasAlt = true
		}
	}

	if !hasRock || !hasAlt {
		t.Errorf(
			"Genre = %v, want [Rock, Alternative]", tr.Genre,
		)
	}
}

// TestEvaluate_YearUsesOriginalReleaseYear is a regression test for a
// bug where the smart-playlist year filter tested recordings.year (the
// file's ID3/reissue tag year) instead of the release group's original
// first-release year, the way the canonical track_metadata view and the
// UI do. That made a 1977 album owned as a 2010s reissue leak into a
// "2010s" year filter even though it displays as 1977.
func TestEvaluate_YearUsesOriginalReleaseYear(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)

	// One track: a 1977 album the user owns as a 2013 reissue. The file
	// tag / recording year is 2013, but the release group's original
	// (first-release) year is 1977.
	exec := func(query string, args ...any) {
		t.Helper()

		if _, err := db.ExecContext(query, args...); err != nil {
			t.Fatalf("exec %q: %v", query, err)
		}
	}

	exec("INSERT INTO artist_credit (id, text) VALUES (1, ?)", "The B-52's")
	exec(
		"INSERT INTO release_groups (id, name, year, original_year) "+
			"VALUES (1, ?, 2013, 1977)",
		"Reissue Compilation",
	)
	exec(
		"INSERT INTO recordings (id, name, artist_credit_id, year) "+
			"VALUES (1, ?, 1, 2013)",
		"Rock Lobster",
	)
	exec(
		"INSERT INTO audio_files (id, file_path, length_milliseconds, "+
			"file_type_id, recording_id) VALUES (1, ?, 300000, 1, 1)",
		"/music/b52s/rock_lobster.mp3",
	)
	exec(
		"INSERT INTO release_group_recordings " +
			"(release_group_id, recording_id) VALUES (1, 1)",
	)

	// A "2010s" filter must NOT match — the album is originally from 1977.
	tracks, err := Evaluate(db, RuleSet{
		Rules: []Rule{
			{Field: "year", Operator: "between", Value: "2010,2019"},
		},
	})
	if err != nil {
		t.Fatalf("Evaluate 2010s: %v", err)
	}

	if len(tracks) != 0 {
		t.Errorf(
			"2010s filter matched %d tracks, want 0 "+
				"(reissue year leaked in)", len(tracks),
		)
	}

	// A "1970s" filter must match — original_year is 1977.
	tracks, err = Evaluate(db, RuleSet{
		Rules: []Rule{
			{Field: "year", Operator: "between", Value: "1970,1979"},
		},
	})
	if err != nil {
		t.Fatalf("Evaluate 1970s: %v", err)
	}

	if len(tracks) != 1 {
		t.Fatalf(
			"1970s filter matched %d tracks, want 1", len(tracks),
		)
	}

	if tracks[0].Year != 1977 {
		t.Errorf("Year = %d, want 1977", tracks[0].Year)
	}

	// The release_year field, by contrast, tracks the specific release
	// owned (the 2013 reissue), so a 2010s filter on it MUST match.
	tracks, err = Evaluate(db, RuleSet{
		Rules: []Rule{
			{Field: "release_year", Operator: "between", Value: "2010,2019"},
		},
	})
	if err != nil {
		t.Fatalf("Evaluate release_year 2010s: %v", err)
	}

	if len(tracks) != 1 {
		t.Fatalf(
			"release_year 2010s filter matched %d tracks, want 1",
			len(tracks),
		)
	}

	// And a 1970s release_year filter must NOT match — the owned
	// release is from 2013.
	tracks, err = Evaluate(db, RuleSet{
		Rules: []Rule{
			{Field: "release_year", Operator: "between", Value: "1970,1979"},
		},
	})
	if err != nil {
		t.Fatalf("Evaluate release_year 1970s: %v", err)
	}

	if len(tracks) != 0 {
		t.Errorf(
			"release_year 1970s filter matched %d tracks, want 0",
			len(tracks),
		)
	}
}
