package database

import (
	"fmt"
	"testing"

	"yellowjacket/backend/database/sql/sqlcgen"
)

// seedSearchData inserts ~7 tracks with the full FK chain required for
// FTS5 search tests: artist_credit → recordings → audio_files →
// release_groups → release_group_recordings → search_index.
//
// Track list:
//
//	ID 1: "Bohemian Rhapsody" by "Queen" on "A Night at the Opera"
//	ID 2: "Halo" by "Beyoncé" on "Lemonade"
//	ID 3: "Back in Black" by "AC/DC" on "Back in Black"
//	ID 4: "Comfortably Numb" by "Pink Floyd" on "The Dark Side of the Moon"
//	ID 5: "Another One Bites the Dust" by "Queen" on "The Game"
//	ID 6: "Thunderstruck" by "AC/DC" on "The Razors Edge"
//	ID 7: "Queen of the Stone Age" by "Queens of the Stone Age" on "Rated R"
func seedSearchData(t *testing.T, db *DB) {
	t.Helper()

	type track struct {
		id       int64
		filePath string
		title    string
		artist   string // artist_credit text
		album    string // release_group name
		trackNum *int64 // recording track_number (nil = NULL)
		discNum  *int64 // recording disc_number (nil = NULL)
		year     int64  // recording year
		genre    string // genre name (empty = no genre)
		composer string // recording composer
		lenMs    int64  // audio_files length_milliseconds
		ftID     int64  // file_type_id
		sr       int64  // sample_rate
		bd       int64  // bit_depth
		ch       int64  // channels
		br       int64  // bitrate
		fsize    int64  // file_size
	}

	intPtr := func(v int64) *int64 { return &v }

	tracks := []track{
		{
			1, "/music/queen/bohemian_rhapsody.mp3", "Bohemian Rhapsody", "Queen",
			"A Night at the Opera", intPtr(11), intPtr(1), 1975, "Rock",
			"Freddie Mercury", 354000, 0, 44100, 16, 2, 320000, 8500000,
		},
		{
			2, "/music/beyonce/halo.flac", "Halo", "Beyoncé", "Lemonade",
			intPtr(1), intPtr(1), 2008, "Pop", "Ryan Tedder", 261000, 1,
			96000, 24, 2, 1411000, 42000000,
		},
		{
			3, "/music/acdc/back_in_black.mp3", "Back in Black", "AC/DC",
			"Back in Black", intPtr(1), intPtr(1), 1980, "Hard Rock",
			"Angus Young", 255000, 0, 44100, 16, 2, 320000, 6100000,
		},
		{
			4, "/music/pinkfloyd/comfortably_numb.flac", "Comfortably Numb",
			"Pink Floyd", "The Dark Side of the Moon", intPtr(6), intPtr(1),
			1979, "Progressive Rock", "David Gilmour", 382000, 1, 96000, 24,
			2, 1411000, 54000000,
		},
		{
			5, "/music/queen/another_one_bites_the_dust.mp3",
			"Another One Bites the Dust", "Queen", "The Game", intPtr(3),
			intPtr(1), 1980, "Funk Rock", "John Deacon", 215000, 0, 44100,
			16, 2, 320000, 5200000,
		},
		{
			6, "/music/acdc/thunderstruck.mp3", "Thunderstruck", "AC/DC",
			"The Razors Edge", intPtr(1), intPtr(1), 1990, "Hard Rock",
			"Angus Young", 292000, 0, 44100, 16, 2, 320000, 7000000,
		},
		{
			7, "/music/qotsa/queen_of_the_stone_age.mp3",
			"Queen of the Stone Age", "Queens of the Stone Age", "Rated R",
			intPtr(1), intPtr(1), 2000, "Stoner Rock", "Josh Homme", 310000,
			0, 44100, 16, 2, 320000, 7400000,
		},
	}

	for _, tr := range tracks {
		var genres []string
		if tr.genre != "" {
			genres = []string{tr.genre}
		}

		var trackNum, discNum int64
		if tr.trackNum != nil {
			trackNum = *tr.trackNum
		}

		if tr.discNum != nil {
			discNum = *tr.discNum
		}

		id := InsertTestTrack(t, db, TestTrack{
			FilePath:    tr.filePath,
			Title:       tr.title,
			Artist:      tr.artist,
			Album:       tr.album,
			Genres:      genres,
			TrackNumber: trackNum,
			DiscNumber:  discNum,
			Year:        tr.year,
			LengthMs:    tr.lenMs,
		})

		// The fixtures assert on audio properties and the composer,
		// which InsertTestTrack does not carry - they are not part of
		// what a seeder should have to know about a track.
		if _, err := db.ExecContext(
			`UPDATE audio_files
			 SET file_type_id = ?, sample_rate = ?, bit_depth = ?,
			     channels = ?, bitrate = ?, file_size = ?, composer = ?
			 WHERE id = ?`,
			tr.ftID, tr.sr, tr.bd, tr.ch, tr.br, tr.fsize, tr.composer, id,
		); err != nil {
			t.Fatalf("set audio properties for %q: %v", tr.filePath, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Pure helper tests (no database needed)
// ---------------------------------------------------------------------------

func TestTokeniseForFTS(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"simple word", "hello", []string{`"hello"`}},
		{"multiple words", "hello world", []string{`"hello"`, `"world"`}},
		{"hyphens split", "rock-pop", []string{`"rock"`, `"pop"`}},
		{"slashes split", "AC/DC", []string{`"AC"`, `"DC"`}},
		{"dots split", "01.track", []string{`"01"`, `"track"`}},
		{"underscores split", "my_song", []string{`"my"`, `"song"`}},
		{
			"double quotes escaped",
			`he"llo`,
			[]string{`"he""llo"`},
		},
		{"empty string", "", nil},
		{"only separators", "---", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tokeniseForFTS(tt.input)

			if len(got) != len(tt.want) {
				t.Fatalf(
					"tokeniseForFTS(%q): got %d tokens %v, want %d tokens %v",
					tt.input, len(got), got, len(tt.want), tt.want,
				)
			}

			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf(
						"tokeniseForFTS(%q)[%d] = %q, want %q",
						tt.input, i, got[i], tt.want[i],
					)
				}
			}
		})
	}
}

func TestBuildFTSQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"single word", "queen", `"queen"`},
		{"multi-word", "bohemian rhapsody", `"bohemian" "rhapsody"`},
		{"empty string returns original", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := buildFTSQuery(tt.input)
			if got != tt.want {
				t.Errorf(
					"buildFTSQuery(%q) = %q, want %q",
					tt.input, got, tt.want,
				)
			}
		})
	}
}

func TestStripExtForSearch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"mp3 extension", "song.mp3", "song"},
		{"double dot", "my.song.flac", "my.song"},
		{"no extension", "noextension", "noextension"},
		{"hidden file", ".hidden", ".hidden"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := stripExtForSearch(tt.input)
			if got != tt.want {
				t.Errorf(
					"stripExtForSearch(%q) = %q, want %q",
					tt.input, got, tt.want,
				)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// FTS5 search tests (require database + seeded data)
// ---------------------------------------------------------------------------

func TestSearchFTS_BasicTerm(t *testing.T) {
	t.Parallel()

	db := NewTestDB(t)
	seedSearchData(t, db)

	results, err := db.SearchFTS("queen", 10)
	if err != nil {
		t.Fatalf("SearchFTS(queen): %v", err)
	}

	// Should find at least "Bohemian Rhapsody" and "Another One Bites the
	// Dust" (artist=Queen) plus "Queen of the Stone Age" (title match).
	if len(results) < 2 {
		t.Fatalf("SearchFTS(queen): got %d results, want >= 2", len(results))
	}

	// Verify we got the expected Queen tracks by collecting titles.
	titles := map[string]bool{}
	for _, r := range results {
		titles[r.Title] = true
	}

	for _, want := range []string{"Bohemian Rhapsody", "Another One Bites the Dust"} {
		if !titles[want] {
			t.Errorf("SearchFTS(queen): missing expected title %q in results %v",
				want, titles)
		}
	}
}

func TestSearchFTS_EmptyQuery(t *testing.T) {
	t.Parallel()

	db := NewTestDB(t)
	seedSearchData(t, db)

	// Empty string.
	results, err := db.SearchFTS("", 10)
	if err != nil {
		t.Fatalf("SearchFTS(empty): %v", err)
	}

	if results != nil {
		t.Errorf("SearchFTS(empty): got %v, want nil", results)
	}

	// Whitespace-only.
	results, err = db.SearchFTS("   ", 10)
	if err != nil {
		t.Fatalf("SearchFTS(whitespace): %v", err)
	}

	if results != nil {
		t.Errorf("SearchFTS(whitespace): got %v, want nil", results)
	}
}

func TestSearchFTS_SpecialCharacters(t *testing.T) {
	t.Parallel()

	db := NewTestDB(t)
	seedSearchData(t, db)

	// "AC/DC" — the tokeniser splits on '/', so "AC" and "DC" both become
	// search tokens and match the AC/DC artist in the index.
	results, err := db.SearchFTS("AC/DC", 10)
	if err != nil {
		t.Fatalf("SearchFTS(AC/DC): %v", err)
	}

	if len(results) < 1 {
		t.Fatalf("SearchFTS(AC/DC): got 0 results, want >= 1")
	}

	// Verify at least one AC/DC track is present.
	found := false

	for _, r := range results {
		if r.Artist == "AC/DC" {
			found = true

			break
		}
	}

	if !found {
		t.Errorf("SearchFTS(AC/DC): no results with Artist='AC/DC'")
	}

	// Query with embedded double quote — should not error.
	results, err = db.SearchFTS(`back"in`, 10)
	if err != nil {
		t.Fatalf("SearchFTS(quote): %v", err)
	}

	// We don't assert exact results for the quote test, just no error.
	_ = results
}

func TestSearchFTS_MultiWord(t *testing.T) {
	t.Parallel()

	db := NewTestDB(t)
	seedSearchData(t, db)

	results, err := db.SearchFTS("bohemian rhapsody", 10)
	if err != nil {
		t.Fatalf("SearchFTS(multi-word): %v", err)
	}

	if len(results) == 0 {
		t.Fatal("SearchFTS(bohemian rhapsody): got 0 results")
	}

	// Top result should be the exact title match.
	if results[0].Title != "Bohemian Rhapsody" {
		t.Errorf(
			"SearchFTS(bohemian rhapsody): top result Title = %q, want %q",
			results[0].Title, "Bohemian Rhapsody",
		)
	}
}

func TestSearchFTS_Diacritics(t *testing.T) {
	t.Parallel()

	db := NewTestDB(t)
	seedSearchData(t, db)

	// Search without diacritic — should find "Beyoncé" due to
	// unicode61 remove_diacritics 2 tokeniser configuration.
	results, err := db.SearchFTS("Beyonce", 10)
	if err != nil {
		t.Fatalf("SearchFTS(Beyonce): %v", err)
	}

	if len(results) == 0 {
		t.Fatal("SearchFTS(Beyonce): got 0 results, want Beyoncé track")
	}

	found := false

	for _, r := range results {
		if r.Artist == "Beyoncé" {
			found = true

			break
		}
	}

	if !found {
		t.Error("SearchFTS(Beyonce): no result with Artist='Beyoncé'")
	}
}

func TestSearchFTS_Ranking(t *testing.T) {
	t.Parallel()

	db := NewTestDB(t)
	seedSearchData(t, db)

	// "Back in Black" appears as both title AND album for track ID 3,
	// so it should rank higher than tracks where "black" only appears
	// in one column.
	results, err := db.SearchFTS("back in black", 10)
	if err != nil {
		t.Fatalf("SearchFTS(ranking): %v", err)
	}

	if len(results) == 0 {
		t.Fatal("SearchFTS(back in black): got 0 results")
	}

	// First result should be the "Back in Black" track (title + album match).
	if results[0].Title != "Back in Black" {
		t.Errorf(
			"SearchFTS(ranking): top result = %q by %q, want %q",
			results[0].Title, results[0].Artist, "Back in Black",
		)
	}
}

func TestSearchFTSByFilename(t *testing.T) {
	t.Parallel()

	db := NewTestDB(t)
	seedSearchData(t, db)

	// Search by basename — extension is stripped, underscores split.
	results, err := db.SearchFTSByFilename("bohemian_rhapsody.mp3", 10)
	if err != nil {
		t.Fatalf("SearchFTSByFilename: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("SearchFTSByFilename(bohemian_rhapsody.mp3): got 0 results")
	}

	found := false

	for _, r := range results {
		if r.Title == "Bohemian Rhapsody" {
			found = true

			break
		}
	}

	if !found {
		t.Error("SearchFTSByFilename: Bohemian Rhapsody not found")
	}

	// Empty basename.
	results, err = db.SearchFTSByFilename("", 10)
	if err != nil {
		t.Fatalf("SearchFTSByFilename(empty): %v", err)
	}

	if results != nil {
		t.Errorf("SearchFTSByFilename(empty): got %v, want nil", results)
	}
}

func TestSearchFTSTracks(t *testing.T) {
	t.Parallel()

	db := NewTestDB(t)
	seedSearchData(t, db)

	results, err := db.SearchFTSTracks("queen", 0, 10)
	if err != nil {
		t.Fatalf("SearchFTSTracks: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("SearchFTSTracks(queen): got 0 results")
	}

	// Find the Bohemian Rhapsody result and verify all 16 fields.
	var br *sqlcgen.TrackMetadatum

	for i, r := range results {
		if r.Title == "Bohemian Rhapsody" {
			br = &results[i]

			break
		}
	}

	if br == nil {
		t.Fatal("SearchFTSTracks: Bohemian Rhapsody not found")

		return
	}

	// Verify all fields are populated.
	checks := []struct {
		field string
		got   any
		want  any
	}{
		{"FilePath", br.FilePath, "/music/queen/bohemian_rhapsody.mp3"},
		{"LengthMilliseconds", br.LengthMilliseconds, int64(354000)},
		{"Title", br.Title, "Bohemian Rhapsody"},
		{"ArtistName", br.ArtistName, "Queen"},
		{"Album", br.Album, "A Night at the Opera"},
		{"Year", br.Year, int64(1975)},
		{"Composer", br.Composer, "Freddie Mercury"},
		{"SampleRate", br.SampleRate, int64(44100)},
		{"BitDepth", br.BitDepth, int64(16)},
		{"Channels", br.Channels, int64(2)},
		{"Bitrate", br.Bitrate, int64(320000)},
		{"FileSize", br.FileSize, int64(8500000)},
	}

	for _, c := range checks {
		if fmt.Sprintf("%v", c.got) != fmt.Sprintf("%v", c.want) {
			t.Errorf("SearchFTSTracks: %s = %v, want %v", c.field, c.got, c.want)
		}
	}

	// TrackNumber and DiscNumber are sql.NullInt64.
	if !br.TrackNumber.Valid || br.TrackNumber.Int64 != 11 {
		t.Errorf("SearchFTSTracks: TrackNumber = %v, want 11", br.TrackNumber)
	}

	if !br.DiscNumber.Valid || br.DiscNumber.Int64 != 1 {
		t.Errorf("SearchFTSTracks: DiscNumber = %v, want 1", br.DiscNumber)
	}

	// Genre (via recording_genres + genres tables GROUP_CONCAT).
	if br.Genre != "Rock" {
		t.Errorf("SearchFTSTracks: Genre = %q, want %q", br.Genre, "Rock")
	}

	// FileType (from file_types table, id=0 → ".mp3").
	if br.FileType != ".mp3" {
		t.Errorf("SearchFTSTracks: FileType = %q, want %q", br.FileType, ".mp3")
	}
}

// ---------------------------------------------------------------------------
// Search index operation tests
// ---------------------------------------------------------------------------

func TestInsertAndDeleteSearchIndex(t *testing.T) {
	t.Parallel()

	db := NewTestDB(t)

	// Set up minimal FK chain for a single track.
	InsertTestTrack(t, db, TestTrack{
		FilePath: "/test/track.mp3",
		Title:    "Test Track",
		Artist:   "Test Artist",
		LengthMs: 180000,
	})

	// Insert into search index.
	if err := db.InsertSearchIndex(
		1, "/test/track.mp3", "Test Track", "Test Artist", "Test Album",
	); err != nil {
		t.Fatalf("InsertSearchIndex: %v", err)
	}

	// Verify it's findable.
	results, err := db.SearchFTS("Test Track", 10)
	if err != nil {
		t.Fatalf("SearchFTS after insert: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("SearchFTS after insert: got 0 results")
	}

	// DeleteSearchIndex now works with contentless_delete=1.
	err = db.DeleteSearchIndex(1)
	if err != nil {
		t.Fatalf("DeleteSearchIndex: %v", err)
	}

	// Verify the deleted row is no longer findable.
	results, err = db.SearchFTS("Test Track", 10)
	if err != nil {
		t.Fatalf("SearchFTS after delete: %v", err)
	}

	if len(results) != 0 {
		t.Fatalf(
			"SearchFTS after delete: got %d results, want 0",
			len(results),
		)
	}
}

func TestRebuildSearchIndex(t *testing.T) {
	t.Parallel()

	db := NewTestDB(t)

	// Seed the file WITHOUT putting it in search_index.
	InsertTestTrack(t, db, TestTrack{
		FilePath:        "/rebuild/track.mp3",
		Title:           "Rebuild Track",
		Artist:          "Rebuild Artist",
		Album:           "Rebuild Album",
		LengthMs:        200000,
		SkipSearchIndex: true,
	})

	// Search should return nothing before rebuild.
	results, err := db.SearchFTS("Rebuild", 10)
	if err != nil {
		t.Fatalf("SearchFTS before rebuild: %v", err)
	}

	if len(results) != 0 {
		t.Fatalf("SearchFTS before rebuild: got %d results, want 0", len(results))
	}

	// Rebuild search index.
	if err := db.RebuildSearchIndex(); err != nil {
		t.Fatalf("RebuildSearchIndex: %v", err)
	}

	// Search should now return the track.
	results, err = db.SearchFTS("Rebuild", 10)
	if err != nil {
		t.Fatalf("SearchFTS after rebuild: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("SearchFTS after rebuild: got 0 results, want >= 1")
	}

	if results[0].Title != "Rebuild Track" {
		t.Errorf(
			"SearchFTS after rebuild: Title = %q, want %q",
			results[0].Title, "Rebuild Track",
		)
	}

	if results[0].Album != "Rebuild Album" {
		t.Errorf(
			"SearchFTS after rebuild: Album = %q, want %q",
			results[0].Album, "Rebuild Album",
		)
	}
}

func TestClearSearchIndex(t *testing.T) {
	t.Parallel()

	db := NewTestDB(t)
	seedSearchData(t, db)

	// Verify data exists.
	results, err := db.SearchFTS("queen", 10)
	if err != nil {
		t.Fatalf("SearchFTS before clear: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("SearchFTS before clear: got 0 results")
	}

	// ClearSearchIndex drops and recreates the contentless FTS5
	// table, which is the only way to clear a content='' table.
	err = db.ClearSearchIndex()
	if err != nil {
		t.Fatalf("ClearSearchIndex: %v", err)
	}

	// Verify the index is empty after clear.
	results, err = db.SearchFTS("queen", 10)
	if err != nil {
		t.Fatalf("SearchFTS after clear: %v", err)
	}

	if len(results) != 0 {
		t.Fatalf("SearchFTS after clear: got %d results, want 0", len(results))
	}
}

// ---------------------------------------------------------------------------
// FTS5 row deletion and update cycle tests
// ---------------------------------------------------------------------------

func TestDeleteSearchIndex(t *testing.T) {
	t.Parallel()

	db := NewTestDB(t)
	seedSearchData(t, db) // 7 tracks

	tests := []struct {
		name        string
		rowid       int64
		searchTerm  string
		expectEmpty bool // true = search should return 0 results after delete
	}{
		{
			name:        "delete existing rowid removes it from search",
			rowid:       1, // "Bohemian Rhapsody"
			searchTerm:  "Bohemian Rhapsody",
			expectEmpty: true,
		},
		{
			name:        "delete non-existent rowid is a no-op",
			rowid:       9999,
			searchTerm:  "queen",
			expectEmpty: false, // other Queen tracks still present
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := db.DeleteSearchIndex(tt.rowid)
			if err != nil {
				t.Fatalf(
					"DeleteSearchIndex(%d): %v",
					tt.rowid, err,
				)
			}

			results, err := db.SearchFTS(tt.searchTerm, 10)
			if err != nil {
				t.Fatalf("SearchFTS(%q): %v", tt.searchTerm, err)
			}

			if tt.expectEmpty && len(results) != 0 {
				t.Errorf(
					"SearchFTS(%q) after delete: got %d results, want 0",
					tt.searchTerm, len(results),
				)
			}

			if !tt.expectEmpty && len(results) == 0 {
				t.Errorf(
					"SearchFTS(%q) after delete: got 0 results, want > 0",
					tt.searchTerm,
				)
			}
		})
	}

	// Verify other tracks are unaffected — "Thunderstruck" should
	// still be searchable after deleting rowid 1.
	results, err := db.SearchFTS("Thunderstruck", 10)
	if err != nil {
		t.Fatalf("SearchFTS(Thunderstruck): %v", err)
	}

	if len(results) == 0 {
		t.Error("SearchFTS(Thunderstruck): expected results, got 0")
	}
}

func TestSearchIndexUpdateCycle(t *testing.T) {
	t.Parallel()

	db := NewTestDB(t)

	// Set up minimal FK chain for a single track at rowid 100.
	id := InsertTestTrack(t, db, TestTrack{
		FilePath:        "/test/update_cycle.mp3",
		Title:           "Old Title",
		Artist:          "Old Artist",
		LengthMs:        200000,
		SkipSearchIndex: true,
	})

	// 1. Insert with old metadata.
	if err := db.InsertSearchIndex(
		id, "/test/update_cycle.mp3", "Old Title", "Old Artist", "Old Album",
	); err != nil {
		t.Fatalf("InsertSearchIndex (old): %v", err)
	}

	// Verify search for "Old Title" finds it.
	results, err := db.SearchFTS("Old Title", 10)
	if err != nil {
		t.Fatalf("SearchFTS(Old Title): %v", err)
	}

	if len(results) == 0 {
		t.Fatal("SearchFTS(Old Title): got 0 results after insert")
	}

	// 2. Delete the row.
	if err := db.DeleteSearchIndex(id); err != nil {
		t.Fatalf("DeleteSearchIndex(%d): %v", id, err)
	}

	// Verify "Old Title" no longer found.
	results, err = db.SearchFTS("Old Title", 10)
	if err != nil {
		t.Fatalf("SearchFTS(Old Title) after delete: %v", err)
	}

	if len(results) != 0 {
		t.Fatalf(
			"SearchFTS(Old Title) after delete: got %d results, want 0",
			len(results),
		)
	}

	// 3. Update the file's title in the DB to simulate a tag edit.
	_, err = db.ExecContext(
		"UPDATE audio_files SET title = 'New Title' WHERE file_path = '/test/update_cycle.mp3'",
	)
	if err != nil {
		t.Fatalf("update title: %v", err)
	}

	// 4. Re-insert the row with new metadata.
	if err := db.InsertSearchIndex(
		id, "/test/update_cycle.mp3", "New Title", "New Artist", "New Album",
	); err != nil {
		t.Fatalf("InsertSearchIndex (new): %v", err)
	}

	// 5. Verify "New Title" is now findable.
	results, err = db.SearchFTS("New Title", 10)
	if err != nil {
		t.Fatalf("SearchFTS(New Title): %v", err)
	}

	if len(results) == 0 {
		t.Fatal("SearchFTS(New Title): got 0 results after reinsert")
	}

	// 6. Verify "Old Title" still returns no results (no ghost entries).
	results, err = db.SearchFTS("Old Title", 10)
	if err != nil {
		t.Fatalf("SearchFTS(Old Title) after reinsert: %v", err)
	}

	if len(results) != 0 {
		t.Fatalf(
			"SearchFTS(Old Title) after reinsert: got %d results, want 0 (ghost entry)",
			len(results),
		)
	}
}

func TestClearSearchIndexPreservesSchema(t *testing.T) {
	t.Parallel()

	db := NewTestDB(t)
	seedSearchData(t, db) // 7 tracks

	// Verify data exists before clear.
	results, err := db.SearchFTS("queen", 10)
	if err != nil {
		t.Fatalf("SearchFTS before clear: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("SearchFTS before clear: got 0 results")
	}

	// Clear the search index.
	if err := db.ClearSearchIndex(); err != nil {
		t.Fatalf("ClearSearchIndex: %v", err)
	}

	// Verify all data is gone.
	results, err = db.SearchFTS("queen", 10)
	if err != nil {
		t.Fatalf("SearchFTS after clear: %v", err)
	}

	if len(results) != 0 {
		t.Fatalf(
			"SearchFTS after clear: got %d results, want 0",
			len(results),
		)
	}

	// Re-insert data and verify the recreated table supports
	// both insert and delete (contentless_delete=1 preserved).
	if err := db.InsertSearchIndex(
		1, "/music/queen/bohemian_rhapsody.mp3",
		"Bohemian Rhapsody", "Queen", "A Night at the Opera",
	); err != nil {
		t.Fatalf("InsertSearchIndex after clear: %v", err)
	}

	results, err = db.SearchFTS("Bohemian", 10)
	if err != nil {
		t.Fatalf("SearchFTS after reinsert: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("SearchFTS after reinsert: got 0 results")
	}

	// Verify delete still works on the recreated table.
	if err := db.DeleteSearchIndex(1); err != nil {
		t.Fatalf("DeleteSearchIndex after clear+reinsert: %v", err)
	}

	results, err = db.SearchFTS("Bohemian", 10)
	if err != nil {
		t.Fatalf("SearchFTS after clear+reinsert+delete: %v", err)
	}

	if len(results) != 0 {
		t.Fatalf(
			"SearchFTS after clear+reinsert+delete: got %d results, want 0",
			len(results),
		)
	}
}

// ---------------------------------------------------------------------------
// Schema constraint tests
// ---------------------------------------------------------------------------

func TestSearchIndexSchema(t *testing.T) {
	t.Parallel()

	db := NewTestDB(t)

	// Verify the UNIQUE index on artist_credit_artist exists by
	// attempting a duplicate insert. First, create the prerequisites.
	_, err := db.ExecContext(
		"INSERT INTO artists (id, name) VALUES (1, 'Test')",
	)
	if err != nil {
		t.Fatalf("insert artist: %v", err)
	}

	// The credit tables this used to assert a UNIQUE constraint on are
	// gone; a file names its artist directly, and artists are unique by
	// name, which is asserted below.
	_, err = db.ExecContext(
		"INSERT INTO artists (id, name) VALUES (2, 'Test')",
	)
	if err == nil {
		t.Error("duplicate artist name should fail, got nil error")
	}
}
