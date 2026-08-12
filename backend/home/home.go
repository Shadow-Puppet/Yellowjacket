// Package home builds the "start listening" shelves on the home page.
//
// The problem the home page solves is not "show the library" — four
// other views already do that, sorted and complete. It is the opposite:
// a complete, sorted library is exactly what gives you nothing to play,
// because every entry point into it is alphabetical and therefore
// identical every time you open the app.
//
// So a shelf here is a *reason*, not a filter. Each one answers a
// different question the user might be asking when they do not know
// what they want — what was I listening to, what is new, what do I keep
// coming back to, what have I forgotten, what fits the mood, what would
// I never pick myself — and each says which question it answered, since
// a row of covers with no explanation is just another grid.
//
// Two consequences of that framing show up throughout:
//
//   - Shelves are built from what the user actually did (play counts,
//     last played, import order) with random sampling only where there
//     is no signal to use. Randomness is the fallback, not the design.
//   - A shelf with nothing behind it is omitted rather than rendered
//     empty. A fresh library has no history, so its home page is
//     legitimately three shelves, and lying about that with empty rows
//     labelled "on repeat" would be worse than showing fewer.
package home

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"strings"

	"yellowjacket/backend/database"
	"yellowjacket/backend/database/sql/sqlcgen"
	"yellowjacket/backend/library"
)

// Kind identifies what a shelf is built from, so the frontend can pick
// an icon and the e2e suite can assert on a shelf without matching
// display copy.
type Kind string

// Shelf kinds.
const (
	KindRecentlyPlayed Kind = "recently-played"
	KindRecentlyAdded  Kind = "recently-added"
	KindMostPlayed     Kind = "most-played"
	KindUnplayed       Kind = "unplayed"
	KindStale          Kind = "stale"
	KindArtist         Kind = "artist"
	KindGenre          Kind = "genre"
	KindRandom         Kind = "random"
)

// Shelf is one horizontal row on the home page.
type Shelf struct {
	// ID is stable within a response, for list keying.
	ID string `json:"id"`

	Kind Kind `json:"kind"`

	// Title is the row heading.
	Title string `json:"title"`

	// Subtitle says why these albums are here.  It is not decoration:
	// without it a shelf is indistinguishable from a random grid.
	Subtitle string `json:"subtitle"`

	Albums []library.Album `json:"albums"`
}

// Shelf sizing.
const (
	// shelfSize is how many albums one row holds.  Wide enough to be
	// worth scrolling, small enough that every shelf is a considered
	// selection rather than a dump of the library.
	shelfSize = 12

	// maxGenreShelves bounds how many genre rows appear, so a
	// heavily-tagged library does not turn the home page into the
	// genres view.
	maxGenreShelves = 2

	// genreCandidates is how many top genres to draw the genre shelves
	// from.  Sampling from a pool rather than taking the top two is
	// what stops the same two genres appearing forever.
	genreCandidates = 8

	// staleWindow is how long an album must go unplayed to count as
	// forgotten.  Six months is past "I listened to that recently" for
	// almost everyone without reaching back to things they no longer
	// own.
	staleWindow = "-6 months"

	// artistShelfMin is the fewest albums by one artist worth a shelf
	// of their own.
	artistShelfMin = 3
)

// Library is the album data the shelves are rendered from.  Narrow on
// purpose: the home page needs one list of albums, not the library
// package.
type Library interface {
	GetAllAlbums() ([]library.Album, error)
}

// Service builds the home page's shelves.
type Service struct {
	logger *slog.Logger
	db     *database.DB
	lib    Library
}

// NewService builds the home service.
func NewService(
	logger *slog.Logger,
	db *database.DB,
	lib Library,
) *Service {
	return &Service{logger: logger, db: db, lib: lib}
}

// GetShelves returns the home page's rows, in display order.
//
// It is a single call rather than one per shelf because the shelves
// share an album lookup and because the page has nothing useful to
// render until it knows which rows exist — a page that pops rows in one
// at a time reflows under the user's cursor.
func (s *Service) GetShelves() ([]Shelf, error) {
	ctx := s.db.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	albums, err := s.lib.GetAllAlbums()
	if err != nil {
		return nil, err
	}

	if len(albums) == 0 {
		return []Shelf{}, nil
	}

	byID := make(map[int64]library.Album, len(albums))
	for _, album := range albums {
		byID[album.ID] = album
	}

	shelves := make([]Shelf, 0, 8) //nolint:mnd // rough capacity hint

	add := func(shelf Shelf, ok bool) {
		if !ok || len(shelf.Albums) == 0 {
			return
		}

		// A repeat is only a fault if a different row was possible. A
		// shelf showing the *entire* library answers every question with
		// the same albums because there are no others, and suppressing
		// those rows punishes a small library for being small — measured
		// against a fixed shelf size instead, this let the fixture
		// library keep three identical shelves while a library one album
		// larger lost them.
		if len(shelf.Albums) < len(albums) &&
			len(shelves) > 0 &&
			duplicates(shelf, shelves[len(shelves)-1]) {
			return
		}

		shelves = append(shelves, shelf)
	}

	add(s.recentlyPlayed(ctx, byID))
	add(s.recentlyAdded(ctx, byID))
	add(s.mostPlayed(ctx, byID))
	add(s.favouriteArtist(ctx, albums))

	for _, shelf := range s.genreShelves(ctx, byID) {
		add(shelf, true)
	}

	add(s.forgotten(ctx, byID))
	add(s.random(ctx, byID))

	return shelves, nil
}

// duplicateThreshold is the share of a shelf that has to be in the
// shelf above before the second one is not worth showing.  Two thirds:
// one album in common between two rows of four is a coincidence, three
// is the same row with a different heading.
const duplicateThreshold = 2.0 / 3.0

// duplicateMinimum is the shortest shelf this rule judges at all.
//
// Below it the ratio says nothing: two rows of one album overlap by
// 100% whenever they agree at all.  The first version of this had no
// floor and no library-size guard, and collapsed a four-album library
// to a single shelf — caught by the existing tests, not by the one
// written for the change.
const duplicateMinimum = 3

// duplicates reports whether a shelf is substantially the shelf above
// it wearing a different reason.
//
// A small library has one signal, not six: everything recently played
// is also everything most played is also everything recently added, so
// "Pick up where you left off" and "On repeat" render the same four
// covers in a different order and the page reads as repeating itself
// (H-9).  This is the same rule as omitting an empty shelf, one step
// further: a shelf has to be worth its own row.
//
// Only the shelf immediately above is compared, deliberately.  Two rows
// that share content are only jarring when they are adjacent, and
// comparing against everything already shown would delete the genre and
// random shelves on any library small enough to reach this at all.
func duplicates(shelf, previous Shelf) bool {
	if len(shelf.Albums) < duplicateMinimum {
		return false
	}

	above := make(map[int64]struct{}, len(previous.Albums))
	for _, album := range previous.Albums {
		above[album.ID] = struct{}{}
	}

	shared := 0

	for _, album := range shelf.Albums {
		if _, ok := above[album.ID]; ok {
			shared++
		}
	}

	return float64(shared)/float64(len(shelf.Albums)) >= duplicateThreshold
}

// resolve turns album ids into albums, dropping any the library no
// longer has (a scan can remove an album between the two queries).
func resolve(ids []int64, byID map[int64]library.Album) []library.Album {
	out := make([]library.Album, 0, len(ids))

	for _, id := range ids {
		if album, ok := byID[id]; ok {
			out = append(out, album)
		}
	}

	return out
}

func (s *Service) recentlyPlayed(
	ctx context.Context,
	byID map[int64]library.Album,
) (Shelf, bool) {
	ids, err := s.db.ReadQueries.HomeRecentlyPlayedAlbums(ctx, shelfSize)
	if err != nil {
		s.logger.Warn("home: recently played", "error", err)

		return Shelf{}, false
	}

	return Shelf{
		ID:       "recently-played",
		Kind:     KindRecentlyPlayed,
		Title:    "Pick up where you left off",
		Subtitle: "The last albums you played",
		Albums:   resolve(ids, byID),
	}, true
}

func (s *Service) recentlyAdded(
	ctx context.Context,
	byID map[int64]library.Album,
) (Shelf, bool) {
	ids, err := s.db.ReadQueries.HomeRecentlyAddedAlbums(ctx, shelfSize)
	if err != nil {
		s.logger.Warn("home: recently added", "error", err)

		return Shelf{}, false
	}

	return Shelf{
		ID:       "recently-added",
		Kind:     KindRecentlyAdded,
		Title:    "Fresh in your library",
		Subtitle: "Most recently added",
		Albums:   resolve(ids, byID),
	}, true
}

func (s *Service) mostPlayed(
	ctx context.Context,
	byID map[int64]library.Album,
) (Shelf, bool) {
	ids, err := s.db.ReadQueries.HomeMostPlayedAlbums(ctx, shelfSize)
	if err != nil {
		s.logger.Warn("home: most played", "error", err)

		return Shelf{}, false
	}

	return Shelf{
		ID:       "most-played",
		Kind:     KindMostPlayed,
		Title:    "On repeat",
		Subtitle: "What you play the most",
		Albums:   resolve(ids, byID),
	}, true
}

// forgotten is two shelves' worth of intent in one row: albums played
// long ago, and — for a library with no history at all — albums never
// played.  Both answer "what am I ignoring", which is the shelf a large
// library benefits from most.
func (s *Service) forgotten(
	ctx context.Context,
	byID map[int64]library.Album,
) (Shelf, bool) {
	stale, err := s.db.ReadQueries.HomeStaleAlbums(
		ctx,
		sqlcgen.HomeStaleAlbumsParams{Datetime: staleWindow, Limit: shelfSize},
	)
	if err != nil {
		s.logger.Warn("home: stale albums", "error", err)

		stale = nil
	}

	if len(stale) > 0 {
		return Shelf{
			ID:       "forgotten",
			Kind:     KindStale,
			Title:    "You haven't played this in a while",
			Subtitle: "Last played over six months ago",
			Albums:   resolve(stale, byID),
		}, true
	}

	unplayed, err := s.db.ReadQueries.HomeUnplayedAlbums(ctx, shelfSize)
	if err != nil {
		s.logger.Warn("home: unplayed albums", "error", err)

		return Shelf{}, false
	}

	return Shelf{
		ID:       "forgotten",
		Kind:     KindUnplayed,
		Title:    "Never played",
		Subtitle: "In your library, still unheard",
		Albums:   resolve(unplayed, byID),
	}, true
}

func (s *Service) random(
	ctx context.Context,
	byID map[int64]library.Album,
) (Shelf, bool) {
	ids, err := s.db.ReadQueries.HomeRandomAlbums(ctx, shelfSize)
	if err != nil {
		s.logger.Warn("home: random albums", "error", err)

		return Shelf{}, false
	}

	return Shelf{
		ID:       "random",
		Kind:     KindRandom,
		Title:    "Take a chance",
		Subtitle: "A handful of albums at random",
		Albums:   resolve(ids, byID),
	}, true
}

// favouriteArtist builds a shelf around whoever the user plays most,
// which is the one recommendation here that reads as personal rather
// than statistical.
func (s *Service) favouriteArtist(
	ctx context.Context,
	albums []library.Album,
) (Shelf, bool) {
	rows, err := s.db.ReadQueries.HomeTopArtists(ctx, artistPoolSize)
	if err != nil {
		s.logger.Warn("home: top artists", "error", err)

		return Shelf{}, false
	}

	// Sampling from the top few rather than always taking first place
	// keeps the shelf from being a permanent fixture about one artist.
	rand.Shuffle(len(rows), func(i, j int) {
		rows[i], rows[j] = rows[j], rows[i]
	})

	for _, row := range rows {
		name := strings.TrimSpace(row.ArtistName)
		if name == "" {
			continue
		}

		byArtist := make([]library.Album, 0, shelfSize)

		for _, album := range albums {
			if strings.EqualFold(album.ArtistName, name) {
				byArtist = append(byArtist, album)
			}
		}

		if len(byArtist) < artistShelfMin {
			continue
		}

		if len(byArtist) > shelfSize {
			byArtist = byArtist[:shelfSize]
		}

		return Shelf{
			ID:       "artist",
			Kind:     KindArtist,
			Title:    "More from " + name,
			Subtitle: "One of your most played artists",
			Albums:   byArtist,
		}, true
	}

	return Shelf{}, false
}

// artistPoolSize is how many top artists the favourite-artist shelf
// picks from.
const artistPoolSize = 5

// genreShelves picks a couple of genres the library actually has depth
// in, sampled from the top handful so the page varies between visits.
func (s *Service) genreShelves(
	ctx context.Context,
	byID map[int64]library.Album,
) []Shelf {
	rows, err := s.db.ReadQueries.HomeTopGenres(ctx, genreCandidates)
	if err != nil {
		s.logger.Warn("home: top genres", "error", err)

		return nil
	}

	rand.Shuffle(len(rows), func(i, j int) {
		rows[i], rows[j] = rows[j], rows[i]
	})

	shelves := make([]Shelf, 0, maxGenreShelves)

	for _, row := range rows {
		if len(shelves) >= maxGenreShelves {
			break
		}

		genre := strings.TrimSpace(row.Genre)
		if genre == "" {
			continue
		}

		ids, err := s.db.ReadQueries.HomeAlbumsByGenre(
			ctx,
			sqlcgen.HomeAlbumsByGenreParams{Name: genre, Limit: shelfSize},
		)
		if err != nil {
			s.logger.Warn("home: albums by genre", "genre", genre, "error", err)

			continue
		}

		found := resolve(ids, byID)
		if len(found) == 0 {
			continue
		}

		shelves = append(shelves, Shelf{
			ID:       "genre-" + genre,
			Kind:     KindGenre,
			Title:    genre,
			Subtitle: "Because your library is full of it",
			Albums:   found,
		})
	}

	return shelves
}
