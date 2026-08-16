package home_test

import (
	"log/slog"
	"testing"

	"yellowjacket/backend/database"
	"yellowjacket/backend/home"
	"yellowjacket/backend/library"
)

// fakeLibrary answers the one question the home service asks of the
// library package, so these tests are about shelf selection rather than
// about album rendering.
type fakeLibrary struct {
	albums []library.Album
	err    error
}

func (f fakeLibrary) GetAlbums(int64) ([]library.Album, error) {
	return f.albums, f.err
}

// seed inserts one album with one played-or-not track, returning the
// release group id.  Written with raw SQL rather than the library
// scanner because the shelves are queries, and a query is best tested
// against rows it can be given precisely.
func seed(
	t *testing.T,
	db *database.DB,
	name, artist, genre string,
	playCount int,
	lastPlayed string,
) int64 {
	t.Helper()

	var genres []string
	if genre != "" {
		genres = []string{genre}
	}

	fileID := database.InsertTestTrack(t, db, database.TestTrack{
		FilePath:  "/music/" + name + ".mp3",
		Title:     name + " track",
		Artist:    artist,
		Album:     name,
		Genres:    genres,
		LengthMs:  1000,
		PlayCount: int64(playCount),
	})

	if lastPlayed != "" {
		if _, err := db.ExecContext(
			"UPDATE audio_files SET last_played = ? WHERE id = ?", lastPlayed, fileID,
		); err != nil {
			t.Fatalf("seed last_played: %v", err)
		}
	}

	var id int64
	if err := db.QueryRowWriter(
		"SELECT album_id FROM audio_files WHERE id = ?", fileID,
	).Scan(&id); err != nil {
		t.Fatalf("seed: read album id: %v", err)
	}

	return id
}

func shelfKinds(shelves []home.Shelf) []home.Kind {
	kinds := make([]home.Kind, 0, len(shelves))
	for _, s := range shelves {
		kinds = append(kinds, s.Kind)
	}

	return kinds
}

func hasKind(shelves []home.Shelf, kind home.Kind) bool {
	for _, s := range shelves {
		if s.Kind == kind {
			return true
		}
	}

	return false
}

func shelfFor(shelves []home.Shelf, kind home.Kind) home.Shelf {
	for _, s := range shelves {
		if s.Kind == kind {
			return s
		}
	}

	return home.Shelf{}
}

func TestGetShelvesEmptyLibraryHasNoShelves(t *testing.T) {
	db := database.NewTestDB(t)
	svc := home.NewService(slog.Default(), db, fakeLibrary{})

	shelves, err := svc.GetShelves()
	if err != nil {
		t.Fatalf("GetShelves: %v", err)
	}

	if len(shelves) != 0 {
		t.Fatalf("empty library produced shelves: %v", shelfKinds(shelves))
	}
}

func TestGetShelvesOmitsShelvesWithNothingBehindThem(t *testing.T) {
	// A library nothing has ever been played from must not claim to
	// know what is on repeat: an empty row labelled with a reason is a
	// worse answer than no row.
	db := database.NewTestDB(t)

	id := seed(t, db, "Quiet", "Nobody", "", 0, "")

	svc := home.NewService(slog.Default(), db, fakeLibrary{
		albums: []library.Album{{ID: id, Name: "Quiet", ArtistName: "Nobody"}},
	})

	shelves, err := svc.GetShelves()
	if err != nil {
		t.Fatalf("GetShelves: %v", err)
	}

	if hasKind(shelves, home.KindRecentlyPlayed) {
		t.Error("recently-played shelf built from no plays")
	}

	if hasKind(shelves, home.KindMostPlayed) {
		t.Error("most-played shelf built from no plays")
	}

	if !hasKind(shelves, home.KindUnplayed) {
		t.Errorf("expected an unplayed shelf, got %v", shelfKinds(shelves))
	}

	if !hasKind(shelves, home.KindRecentlyAdded) {
		t.Errorf("expected a recently-added shelf, got %v", shelfKinds(shelves))
	}
}

func TestGetShelvesRanksPlayHistory(t *testing.T) {
	db := database.NewTestDB(t)

	old := seed(t, db, "Old Favourite", "A", "", 20, "2020-01-01 00:00:00")
	recent := seed(t, db, "Last Night", "B", "", 3, "2999-01-01 00:00:00")

	svc := home.NewService(slog.Default(), db, fakeLibrary{
		albums: []library.Album{
			{ID: old, Name: "Old Favourite", ArtistName: "A"},
			{ID: recent, Name: "Last Night", ArtistName: "B"},
		},
	})

	shelves, err := svc.GetShelves()
	if err != nil {
		t.Fatalf("GetShelves: %v", err)
	}

	played := shelfFor(shelves, home.KindRecentlyPlayed)
	if len(played.Albums) == 0 || played.Albums[0].ID != recent {
		t.Errorf("recently played led with %v, want the newest play", played.Albums)
	}

	most := shelfFor(shelves, home.KindMostPlayed)
	if len(most.Albums) == 0 || most.Albums[0].ID != old {
		t.Errorf("most played led with %v, want the highest play count", most.Albums)
	}

	// Played long ago is "forgotten"; the never-played fallback must
	// not take over while there is real history to report.
	forgotten := shelfFor(shelves, home.KindStale)
	if len(forgotten.Albums) == 0 || forgotten.Albums[0].ID != old {
		t.Errorf("forgotten shelf = %v, want the album last played in 2020", forgotten.Albums)
	}
}

func TestGetShelvesBuildsAGenreShelf(t *testing.T) {
	db := database.NewTestDB(t)

	albums := make([]library.Album, 0, 4)

	for _, name := range []string{"One", "Two", "Three", "Four"} {
		id := seed(t, db, name, "Various", "Doom Jazz", 1, "2024-01-01 00:00:00")
		albums = append(albums, library.Album{
			ID: id, Name: name, ArtistName: "Various",
		})
	}

	svc := home.NewService(slog.Default(), db, fakeLibrary{albums: albums})

	shelves, err := svc.GetShelves()
	if err != nil {
		t.Fatalf("GetShelves: %v", err)
	}

	genre := shelfFor(shelves, home.KindGenre)
	if genre.Title != "Doom Jazz" {
		t.Fatalf("genre shelf = %q, want the library's one genre", genre.Title)
	}

	if len(genre.Albums) != len(albums) {
		t.Errorf("genre shelf had %d albums, want %d", len(genre.Albums), len(albums))
	}
}

func TestGetShelvesSkipsAlbumsTheLibraryNoLongerHas(t *testing.T) {
	// The id queries and the album list are two reads, and a scan can
	// remove an album between them.  A stale id must vanish from the
	// shelf, not render as a blank card.
	db := database.NewTestDB(t)

	kept := seed(t, db, "Kept", "A", "", 5, "2024-01-01 00:00:00")
	seed(t, db, "Removed", "B", "", 5, "2024-01-02 00:00:00")

	svc := home.NewService(slog.Default(), db, fakeLibrary{
		albums: []library.Album{{ID: kept, Name: "Kept", ArtistName: "A"}},
	})

	shelves, err := svc.GetShelves()
	if err != nil {
		t.Fatalf("GetShelves: %v", err)
	}

	for _, shelf := range shelves {
		for _, album := range shelf.Albums {
			if album.ID != kept {
				t.Fatalf("shelf %q surfaced a removed album: %+v", shelf.ID, album)
			}
		}
	}
}

// A shelf has to be worth its own row.
//
// A library with one signal answers several questions with the same
// albums, so shelves render the same covers in different orders under
// different reasons and the page reads as repeating itself (H-9).
// Confirmed in the running app on the fixture library before this
// existed: "On repeat" was "Pick up where you left off" reordered.
//
// The library has to be bigger than one shelf for the rule to apply at
// all, which is the point: a repeat is only a fault if a different row
// was possible.
func TestGetShelvesOmitsAShelfThatRepeatsTheOneAboveIt(t *testing.T) {
	db := database.NewTestDB(t)

	albums := make([]library.Album, 0, 16)

	// Four albums carry the whole play history, so "what you played
	// last" and "what you play most" can only answer with those four.
	for i := range 4 {
		name := "Played " + string(rune('A'+i))
		id := seed(t, db, name, "Solo", "", 50+i, "2026-08-1"+string(rune('0'+i))+" 00:00:00")

		albums = append(albums, library.Album{ID: id, Name: name, ArtistName: "Solo"})
	}

	// …and ten more the user has never touched, so the library is
	// larger than a single shelf and a different row was possible.
	for i := range 10 {
		name := "Quiet " + string(rune('A'+i))
		id := seed(t, db, name, "Nobody", "", 0, "")

		albums = append(albums, library.Album{ID: id, Name: name, ArtistName: "Nobody"})
	}

	svc := home.NewService(slog.Default(), db, fakeLibrary{albums: albums})

	shelves, err := svc.GetShelves()
	if err != nil {
		t.Fatalf("GetShelves: %v", err)
	}

	// The first of a duplicate pair survives: the reason the user sees
	// is the one that came first, not the one built last.
	if !hasKind(shelves, home.KindRecentlyPlayed) {
		t.Fatalf("expected a recently-played shelf, got %v", shelfKinds(shelves))
	}

	// And the page still has somewhere to start.
	if len(shelves) < 2 {
		t.Fatalf("suppression left %d shelves: %v", len(shelves), shelfKinds(shelves))
	}

	for i := 1; i < len(shelves); i++ {
		above := shelves[i-1]
		shared := 0

		for _, a := range shelves[i].Albums {
			for _, b := range above.Albums {
				if a.ID == b.ID {
					shared++

					break
				}
			}
		}

		if len(shelves[i].Albums) < 3 {
			continue
		}

		if ratio := float64(shared) / float64(len(shelves[i].Albums)); ratio >= 2.0/3.0 {
			t.Errorf(
				"shelf %q repeats %q (%d of %d albums)",
				shelves[i].Kind, above.Kind, shared, len(shelves[i].Albums),
			)
		}
	}
}
