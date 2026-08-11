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

func (f fakeLibrary) GetAllAlbums() ([]library.Album, error) {
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

	exec := func(query string, args ...any) {
		t.Helper()

		if _, err := db.ExecContext(query, args...); err != nil {
			t.Fatalf("seed %q: %v", query, err)
		}
	}

	exec(`INSERT INTO artist_credit (text) VALUES (?)
	      ON CONFLICT DO NOTHING`, artist)
	exec(`INSERT INTO release_groups (name, album_artist_credit_id)
	      VALUES (?, (SELECT id FROM artist_credit WHERE text = ?))`,
		name, artist)
	exec(`INSERT INTO recordings (name, artist_credit_id)
	      VALUES (?, (SELECT id FROM artist_credit WHERE text = ?))`,
		name+" track", artist)
	exec(`INSERT INTO release_group_recordings (release_group_id, recording_id)
	      VALUES ((SELECT MAX(id) FROM release_groups),
	              (SELECT MAX(id) FROM recordings))`)
	exec(`INSERT INTO file_types (extension) VALUES ('mp3')
	      ON CONFLICT DO NOTHING`)
	exec(`INSERT INTO audio_files
	        (file_path, length_milliseconds, file_type_id, recording_id,
	         play_count, last_played)
	      VALUES (?, 1000,
	              (SELECT MAX(id) FROM file_types),
	              (SELECT MAX(id) FROM recordings),
	              ?, ?)`,
		"/music/"+name+".mp3", playCount, nullable(lastPlayed))

	if genre != "" {
		exec(`INSERT INTO genres (name) VALUES (?) ON CONFLICT DO NOTHING`, genre)
		exec(`INSERT INTO recording_genres (recording_id, genre_id)
		      VALUES ((SELECT MAX(id) FROM recordings),
		              (SELECT id FROM genres WHERE name = ?))`, genre)
	}

	var id int64
	if err := db.QueryRowWriter(
		`SELECT MAX(id) FROM release_groups`,
	).Scan(&id); err != nil {
		t.Fatalf("seed: read album id: %v", err)
	}

	return id
}

func nullable(s string) any {
	if s == "" {
		return nil
	}

	return s
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
