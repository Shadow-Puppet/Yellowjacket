package explore

import (
	"context"
	"log/slog"
	"testing"

	"yellowjacket/backend/database"
)

// The shelves are three queries and one rule about what to do when they
// all come back empty, and the second half is the part that matters:
// Explore's data is a downloaded artifact, so "no shelves" can mean the
// catalog is absent, mid-build, or simply has nothing the user does not
// already own — and rendering those identically is the blank panel this
// feature exists to remove.

// seedShelfRow writes one explore_index row. The parameter list is the
// real upsert's, so a schema change breaks these tests in the same
// place it breaks the app rather than leaving them passing against a
// shape nothing writes any more.
func seedShelfRow(
	t *testing.T,
	db *database.DB,
	entityType, mbid, title, artistName, artistMBID string,
	popularity int,
	inLibrary bool,
) {
	t.Helper()

	owned := 0
	if inLibrary {
		owned = 1
	}

	if _, err := db.ExecContext(
		upsertIndexSQL,
		entityType, mbid, title, artistName, artistMBID, "",
		popularity, popularity,
		0, "", "",
		"Album", "", "",
		"", "", "", "",
		owned, 0,
		0, 0, 0,
		0,
	); err != nil {
		t.Fatalf("seed explore_index row %q: %v", mbid, err)
	}
}

func newShelfService(t *testing.T) (*Service, *database.DB) {
	t.Helper()

	db := database.NewTestDB(t)
	index := NewSearchIndex(db, nil, nil, slog.Default())

	return &Service{
		index:  index,
		db:     db,
		logger: slog.Default(),
		ctx:    context.Background(),
	}, db
}

// buildShelves builds the page.
//
// Nothing has to be marked ready first, which is the point: the gate is
// a question asked of the database, so rows seeded after startup count.
// Both cached alternatives failed here first — one because it is only
// refreshed between build tiers, the other because it is set once at
// startup, when a test has seeded nothing yet.
func buildShelves(svc *Service) ShelfPage {
	return svc.GetExploreShelves()
}

func shelfByKind(page ShelfPage, kind ShelfKind) (Shelf, bool) {
	for _, shelf := range page.Shelves {
		if shelf.Kind == kind {
			return shelf, true
		}
	}

	return Shelf{}, false
}

func TestShelves_NoIndexSaysSoRatherThanShowingNothing(t *testing.T) {
	t.Parallel()

	svc, _ := newShelfService(t)

	page := buildShelves(svc)

	if page.State != ShelfStateNoIndex {
		t.Fatalf("state = %q, want %q", page.State, ShelfStateNoIndex)
	}

	if len(page.Shelves) != 0 {
		t.Fatalf("shelves = %d, want 0", len(page.Shelves))
	}
}

func TestShelves_PopularOrdersByPopularityAndExcludesOwned(t *testing.T) {
	t.Parallel()

	svc, db := newShelfService(t)

	seedShelfRow(t, db, "release_group", "rg-quiet", "Quiet", "A", "mbid-a", 10, false)
	seedShelfRow(t, db, "release_group", "rg-loud", "Loud", "B", "mbid-b", 900, false)
	seedShelfRow(t, db, "release_group", "rg-mine", "Mine", "C", "mbid-c", 5000, true)
	// Popularity 0 is the dump's "no listens", not a low score: those
	// rows have no defined order among themselves.
	seedShelfRow(t, db, "release_group", "rg-unheard", "Unheard", "D", "mbid-d", 0, false)

	page := buildShelves(svc)

	shelf, ok := shelfByKind(page, ShelfPopularAlbums)
	if !ok {
		t.Fatal("no popular-albums shelf")
	}

	var titles []string
	for _, album := range shelf.Albums {
		titles = append(titles, album.Title)
	}

	if len(titles) != 2 || titles[0] != "Loud" || titles[1] != "Quiet" {
		t.Fatalf("albums = %v, want [Loud Quiet]", titles)
	}
}

func TestShelves_ArtistsAreTheirOwnShelf(t *testing.T) {
	t.Parallel()

	svc, db := newShelfService(t)

	// Two different artists: the albums shelf takes one, and the
	// artists shelf must not simply repeat them (see the dedup test
	// below), so the shelf it gets is somebody else's.
	seedShelfRow(t, db, "release_group", "rg-1", "An Album", "Album Maker", "ar-album", 900, false)
	seedShelfRow(t, db, "artist", "ar-1", "Big Name", "Big Name", "ar-1", 800, false)

	page := buildShelves(svc)

	artists, ok := shelfByKind(page, ShelfPopularArtists)
	if !ok {
		t.Fatal("no popular-artists shelf")
	}

	// A shelf carries albums or artists, never both: the two render as
	// different cards and route to different pages.
	if len(artists.Artists) != 1 || len(artists.Albums) != 0 {
		t.Fatalf("artists shelf = %d artists / %d albums, want 1 / 0",
			len(artists.Artists), len(artists.Albums))
	}

	if artists.Artists[0].Name != "Big Name" {
		t.Fatalf("artist name = %q", artists.Artists[0].Name)
	}
}

func TestShelves_OmitsAShelfWithNothingBehindIt(t *testing.T) {
	t.Parallel()

	svc, db := newShelfService(t)

	// Albums only: the artists shelf has nothing and must not be
	// rendered empty, and nothing is owned so neither has the third.
	seedShelfRow(t, db, "release_group", "rg-1", "An Album", "A", "mbid-a", 100, false)

	page := buildShelves(svc)

	if len(page.Shelves) != 1 {
		var kinds []ShelfKind
		for _, shelf := range page.Shelves {
			kinds = append(kinds, shelf.Kind)
		}

		t.Fatalf("shelves = %v, want just popular-albums", kinds)
	}

	if page.State != ShelfStateReady {
		t.Fatalf("state = %q, want %q", page.State, ShelfStateReady)
	}
}

func TestShelves_MoreFromOwnedNeedsExactlyOneOwnedAlbum(t *testing.T) {
	t.Parallel()

	svc, db := newShelfService(t)

	// One album owned by "Solo", so the rest of Solo's catalog is the
	// shelf. Two owned by "Complete", who is therefore not a gap.
	seedShelfRow(t, db, "release_group", "rg-solo-own", "Owned", "Solo", "mbid-solo", 100, true)
	seedShelfRow(t, db, "release_group", "rg-solo-a", "Second", "Solo", "mbid-solo", 90, false)
	seedShelfRow(t, db, "release_group", "rg-solo-b", "Third", "Solo", "mbid-solo", 80, false)
	seedShelfRow(t, db, "release_group", "rg-comp-1", "One", "Complete", "mbid-comp", 100, true)
	seedShelfRow(t, db, "release_group", "rg-comp-2", "Two", "Complete", "mbid-comp", 90, true)
	seedShelfRow(t, db, "release_group", "rg-comp-3", "Three", "Complete", "mbid-comp", 70, false)

	page := buildShelves(svc)

	shelf, ok := shelfByKind(page, ShelfMoreFromOwned)
	if !ok {
		t.Fatal("no more-from-owned shelf")
	}

	var titles []string
	for _, album := range shelf.Albums {
		titles = append(titles, album.Title)
	}

	if len(titles) != 2 || titles[0] != "Second" || titles[1] != "Third" {
		t.Fatalf("albums = %v, want [Second Third]", titles)
	}

	// A shelf that turns out to be about one artist says so by name.
	if shelf.Title != "More from Solo" {
		t.Fatalf("title = %q, want %q", shelf.Title, "More from Solo")
	}

	// It is the first row: it is the only one about this user.
	if page.Shelves[0].Kind != ShelfMoreFromOwned {
		t.Fatalf("first shelf = %q, want %q", page.Shelves[0].Kind, ShelfMoreFromOwned)
	}
}

func TestShelves_UntaggedLibraryProducesNoOwnershipShelf(t *testing.T) {
	t.Parallel()

	svc, db := newShelfService(t)

	// This is the fixture library's shape, and every untagged library's:
	// `in_library` is set from MusicBrainz IDs, so nothing is marked
	// owned however much music is on disk. The shelf must be absent
	// rather than wrong.
	seedShelfRow(t, db, "release_group", "rg-1", "An Album", "A", "mbid-a", 100, false)
	seedShelfRow(t, db, "release_group", "rg-2", "Another", "A", "mbid-a", 90, false)

	page := buildShelves(svc)

	if _, ok := shelfByKind(page, ShelfMoreFromOwned); ok {
		t.Fatal("more-from-owned shelf built from a library that owns nothing")
	}
}

func TestShelves_TheSecondRowIsNotTheFirstRowsArtists(t *testing.T) {
	t.Parallel()

	svc, db := newShelfService(t)

	// This is what the real catalog does. Ordered by raw ListenBrainz
	// listen count, the top albums are one act and its members, and the
	// artists shelf underneath was then the same people — one fandom
	// twice, on a page whose whole job is breadth. `home`'s duplicate
	// guard could not see it: the two shelves hold different entity
	// types, so no two rows share an id, and the page repeats itself
	// anyway because a person reads artists, not ids.
	seedShelfRow(t, db, "artist", "ar-huge", "Huge", "Huge", "ar-huge", 10000, false)
	seedShelfRow(t, db, "release_group", "rg-huge", "Hit", "Huge", "ar-huge", 9000, false)
	seedShelfRow(t, db, "artist", "ar-next", "Next", "Next", "ar-next", 500, false)

	page := buildShelves(svc)

	albums, ok := shelfByKind(page, ShelfPopularAlbums)
	if !ok {
		t.Fatal("no popular-albums shelf")
	}

	artists, ok := shelfByKind(page, ShelfPopularArtists)
	if !ok {
		t.Fatal("no popular-artists shelf")
	}

	if albums.Albums[0].ArtistMBID != "ar-huge" {
		t.Fatalf("albums shelf leads with %q, want ar-huge", albums.Albums[0].ArtistMBID)
	}

	for _, artist := range artists.Artists {
		if artist.MBID == "ar-huge" {
			t.Fatal("artists shelf repeats the artist the albums shelf just showed")
		}
	}
}

func TestShelves_OneAlbumPerArtist(t *testing.T) {
	t.Parallel()

	svc, db := newShelfService(t)

	// A shelf is a selection, not a leaderboard: twelve slots spent on
	// one artist is a row saying one thing twelve times.
	seedShelfRow(t, db, "release_group", "rg-a1", "First", "Prolific", "ar-a", 900, false)
	seedShelfRow(t, db, "release_group", "rg-a2", "Second", "Prolific", "ar-a", 800, false)
	seedShelfRow(t, db, "release_group", "rg-a3", "Third", "Prolific", "ar-a", 700, false)
	seedShelfRow(t, db, "release_group", "rg-b1", "Only", "Other", "ar-b", 100, false)

	page := buildShelves(svc)

	shelf, ok := shelfByKind(page, ShelfPopularAlbums)
	if !ok {
		t.Fatal("no popular-albums shelf")
	}

	var titles []string
	for _, album := range shelf.Albums {
		titles = append(titles, album.Title)
	}

	if len(titles) != 2 || titles[0] != "First" || titles[1] != "Only" {
		t.Fatalf("albums = %v, want [First Only]", titles)
	}
}
