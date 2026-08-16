package explore

import (
	"context"
	"strings"
)

// Explore's shelves — the page's answer before anyone has typed.
//
// Every other view in this app answers "what have I got". Explore is
// the only one that answers "what exists", and until now it would not
// start: a search box over a 1.1 M-row local catalog, and a sentence
// telling the user to type into a catalog whose whole point is that
// they do not yet know what is in it (H-23).
//
// The convention is `backend/home`'s, deliberately: a shelf is a
// **reason**, not a filter, and it carries the sentence that says so; a
// shelf with nothing behind it is omitted rather than rendered empty.
// What differs is what happens when *every* shelf is empty. Home's
// answer is a shorter page, which is honest because a library with no
// history really has less to say. Explore's data is a downloaded
// artifact that can be absent, half-merged, or never fetched — so an
// empty page there is not a small library, it is a page that does not
// know yet, and it has to say which. That is what State is for.
//
// The plan named four candidate shelves and said to cut them down once
// they could be seen next to each other. Two of them could not be built
// at all, and the schema says so rather than the design:
//
//   - "Big in a genre you already have depth in" needs a genre on a
//     catalog row. `explore_index` has no genre or tag column, and
//     genre lives only in the library's own `recording_genres`. There
//     is nothing to join to. Dropped, not deferred.
//   - "Artists next to ones you own" needs `similar_artist_map`, which
//     `cmd/indexexport` does not ship (the artifact carries
//     `explore_index` and its metadata, nothing else) and which is
//     filled lazily by ListenBrainz calls from artist pages. It is
//     empty on a fresh install and empty offline, which is precisely
//     when this page most needs something to show.
//
// What is left is three, and only the first is guaranteed: the other
// two join back to the library through `in_library`, which is set by
// MBID and is therefore empty on an untagged library — including the
// fixture one, where these shelves correctly render as one.

// ShelfKind identifies what a shelf is built from, so the frontend can
// pick an icon and a spec can assert on a shelf without matching
// display copy.
type ShelfKind string

// Shelf kinds.
const (
	ShelfPopularAlbums  ShelfKind = "popular-albums"
	ShelfPopularArtists ShelfKind = "popular-artists"
	ShelfMoreFromOwned  ShelfKind = "more-from-owned"
)

// Shelf is one horizontal row on the Explore page.
//
// A shelf carries albums or artists, never both: they route to
// different pages and render as different cards, and a row that is
// sometimes one and sometimes the other is two components pretending to
// be one.
type Shelf struct {
	ID   string    `json:"id"`
	Kind ShelfKind `json:"kind"`

	// Title is the row heading.
	Title string `json:"title"`

	// Subtitle says why these are here. As on Home it is not
	// decoration: without it a shelf is indistinguishable from a
	// random grid.
	Subtitle string `json:"subtitle"`

	Albums  []MBReleaseGroup `json:"albums,omitempty"`
	Artists []MBArtist       `json:"artists,omitempty"`
}

// ShelfPage is what Explore renders before a query.
//
// State exists because "no shelves" has three different causes here and
// the page must not present them identically — a blank panel is the bug
// this whole feature is fixing, and a blank panel that says nothing
// about why is the same bug with more code behind it.
type ShelfPage struct {
	Shelves []Shelf `json:"shelves"`

	// State is one of:
	//
	//   "ready"     — the catalog is here and the shelves are below.
	//   "building"  — it is being fetched or built right now.
	//   "no-index"  — there is no catalog. Search still works over
	//                 whatever is in the index, which may be nothing;
	//                 the page says so and points at Settings.
	State string `json:"state"`
}

// Shelf page states.
const (
	ShelfStateReady    = "ready"
	ShelfStateBuilding = "building"
	ShelfStateNoIndex  = "no-index"
)

// shelfSize is how many cards one row holds. Matches `home`'s, for the
// same reason: wide enough to be worth scrolling, small enough that the
// row reads as a selection rather than a dump of the catalog.
const shelfSize = 12

// ownedArtistPool bounds how many owned artists the "you own one album
// by this artist" shelf considers. A library with 4 000 tagged artists
// does not need all of them ranked to fill twelve cards, and the bound
// is what keeps this query off the startup path's critical section.
const ownedArtistPool = 200

// GetExploreShelves builds the page Explore shows before a query.
//
// One call rather than one per shelf, for `home`'s reason: the shelves
// share nothing expensive, but the page has nothing useful to render
// until it knows which rows exist, and rows that pop in one at a time
// reflow under the cursor.
func (e *Service) GetExploreShelves() ShelfPage {
	ctx := e.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	// "Is there a catalog" is asked of the database, not of a flag.
	//
	// Two cached answers were tried first and both were wrong in the
	// same way. `GetIndexStatus().TotalRows` is an in-memory field
	// refreshed between build tiers, so on an ordinary launch — artifact
	// already merged, nothing building — it reads 0 beside a full
	// catalog, and gating on it hid every shelf. `IsReady()` is set once
	// at startup by counting rows, so it is right in the app and wrong
	// for anything that changes the table afterwards — including the
	// e2e suite staging a catalog, which is how this page gets tested
	// in CI, where the artifact URL points at a dead address on purpose.
	//
	// Both are the shape `emitStatus` warns about: a derived value with
	// nothing left polling behind it. One `SELECT 1 … LIMIT 1` against
	// an indexed table costs nothing and cannot be stale.
	status := e.index.GetIndexStatus()
	building := status.Building

	if !e.index.hasCatalogRows(ctx) {
		state := ShelfStateNoIndex
		if building {
			state = ShelfStateBuilding
		}

		return ShelfPage{Shelves: []Shelf{}, State: state}
	}

	shelves := make([]Shelf, 0, 3) //nolint:mnd // one per shelf kind

	// Which artists the page has already spent a row on.
	//
	// `home` suppresses a shelf that repeats the one above it by album
	// id. That test is useless here and reads as unnecessary: these
	// shelves hold different entity types, so their ids are disjoint by
	// construction and no overlap is possible. The page repeated itself
	// anyway. Ordered by raw listen count, a ListenBrainz-derived
	// catalog's top albums are seven records by one act and its members,
	// and the artists shelf underneath is then the same seven people —
	// visibly one fandom twice, with no two rows sharing an id. The
	// duplication is by *artist*, which is what a person sees.
	seen := make(map[string]struct{})

	add := func(shelf Shelf, ok bool) {
		if !ok || (len(shelf.Albums) == 0 && len(shelf.Artists) == 0) {
			return
		}

		for _, album := range shelf.Albums {
			if album.ArtistMBID != "" {
				seen[album.ArtistMBID] = struct{}{}
			}
		}

		for _, artist := range shelf.Artists {
			seen[artist.MBID] = struct{}{}
		}

		shelves = append(shelves, shelf)
	}

	add(e.moreFromOwnedArtists(ctx))
	add(e.popularAlbums(ctx, seen))
	add(e.popularArtists(ctx, seen))

	// A build in progress over a catalog that already has rows is still
	// "building" — the shelves below are real but incomplete, and a page
	// that will visibly gain rows should say so rather than let the user
	// wonder why it changed.
	state := ShelfStateReady
	if building {
		state = ShelfStateBuilding
	}

	return ShelfPage{Shelves: shelves, State: state}
}

// popularAlbums is the honest default for "what exists", and the only
// shelf that needs nothing from the user: no library, no tags, no
// network. If it is empty, there is no catalog, which the State
// already said.
func (e *Service) popularAlbums(
	ctx context.Context,
	seen map[string]struct{},
) (Shelf, bool) {
	rows := e.index.topByPopularity(ctx, "release_group", shelfSize, seen)
	if len(rows) == 0 {
		return Shelf{}, false
	}

	albums := make([]MBReleaseGroup, 0, len(rows))
	for _, row := range rows {
		albums = append(albums, releaseGroupFromIndex(row))
	}

	return Shelf{
		ID:       "popular-albums",
		Kind:     ShelfPopularAlbums,
		Title:    "Popular right now",
		Subtitle: "The most listened-to albums you don't already own",
		Albums:   albums,
	}, true
}

// popularArtists is a different question, not the same one re-sorted:
// an artist card opens the artist page, and a page made only of albums
// offers no route to the half of Explore that is about people.
//
// It is *asked* differently too: it skips whoever the rows above
// already showed, because the shelves are all ordered by the same
// listen count and the top of that list is not a broad selection.
func (e *Service) popularArtists(
	ctx context.Context,
	seen map[string]struct{},
) (Shelf, bool) {
	rows := e.index.topByPopularity(ctx, "artist", shelfSize, seen)
	if len(rows) == 0 {
		return Shelf{}, false
	}

	artists := make([]MBArtist, 0, len(rows))
	for _, row := range rows {
		artists = append(artists, artistFromIndex(row))
	}

	return Shelf{
		ID:       "popular-artists",
		Kind:     ShelfPopularArtists,
		Title:    "Artists worth knowing",
		Subtitle: "Widely listened to, and not yet in your library",
		Albums:   nil,
		Artists:  artists,
	}, true
}

// moreFromOwnedArtists is the catalog answering a gap the library can
// already see: one album by an artist is usually an accident of how it
// arrived, not a considered stopping point.
//
// It is first on the page when it exists, because it is the only shelf
// about *this* user, and last to exist at all: it reads `in_library`,
// which `PopulateLocalCrossReferences` sets by MusicBrainz ID, so an
// untagged library produces nothing here however large it is.
func (e *Service) moreFromOwnedArtists(ctx context.Context) (Shelf, bool) {
	rows := e.index.unownedAlbumsBySinglyOwnedArtists(ctx, ownedArtistPool, shelfSize)
	if len(rows) == 0 {
		return Shelf{}, false
	}

	albums := make([]MBReleaseGroup, 0, len(rows))
	for _, row := range rows {
		albums = append(albums, releaseGroupFromIndex(row))
	}

	title := "You own one album by these artists"
	if names := distinctArtists(rows); len(names) == 1 {
		title = "More from " + names[0]
	}

	return Shelf{
		ID:       "more-from-owned",
		Kind:     ShelfMoreFromOwned,
		Title:    title,
		Subtitle: "The rest of what they made",
		Albums:   albums,
	}, true
}

// The two queries the shelves are built from.
//
// They return ids and nothing else, and are joined back to the card
// projection by `rowsByIDs` — `backend/home`'s arrangement, for its
// reason: there is one definition of an Explore card, and a shelf query
// that also selected columns would quietly become a second one. That
// `rowsByIDs` returns rows in the order it was given them is what lets
// the ordering live in SQL.

// topByPopularity is the catalog's own answer to "what exists", for one
// entity type.
//
// `in_library = 0` because Explore is the view that answers what the
// user does *not* have — every other view in the app already answers
// the other question, and a discovery row that opens with something
// they own has spent a slot saying nothing. `popularity > 0` drops the
// long tail the ListenBrainz dump had no listens for, which would
// otherwise be ordered arbitrarily among themselves.
//
// **One row per artist**, which is the difference between a shelf and a
// leaderboard. Ordered by raw listen count, the catalog's top twelve
// albums were seven records by one act and its members; a shelf is a
// selection, and twelve slots spent on one artist is the row saying one
// thing twelve times. `skip` then drops artists another shelf already
// showed, for the same reason one row further out.
//
// It over-fetches and filters in Go rather than passing the skip set to
// SQL: the set is a handful of MBIDs against a window function over an
// indexed scan, and an `artist_mbid NOT IN (?, ?, …)` would rebuild the
// statement per call for no measurable gain.
func (si *SearchIndex) topByPopularity(
	ctx context.Context,
	entityType string,
	limit int,
	skip map[string]struct{},
) []SearchIndexResult {
	// The artist rows *are* the artists, so they partition by their own
	// mbid; release groups partition by whoever made them.
	partition := "artist_mbid"
	if entityType == EntityArtist {
		partition = "mbid"
	}

	rows := si.rowsByIDs(ctx, si.shelfIDs(
		ctx,
		`SELECT id FROM (
		     SELECT id, popularity,
		            ROW_NUMBER() OVER (
		                PARTITION BY `+partition+`
		                ORDER BY popularity DESC
		            ) AS rank
		     FROM explore_index
		     WHERE entity_type = ? AND in_library = 0 AND popularity > 0
		 )
		 WHERE rank = 1
		 ORDER BY popularity DESC
		 LIMIT ?`,
		dbEntityType(entityType), limit+len(skip),
	))

	out := make([]SearchIndexResult, 0, limit)

	for _, row := range rows {
		key := row.ArtistMBID
		if entityType == EntityArtist {
			key = row.MBID
		}

		if _, ok := skip[key]; ok && key != "" {
			continue
		}

		out = append(out, row)

		if len(out) == limit {
			break
		}
	}

	return out
}

// unownedAlbumsBySinglyOwnedArtists finds albums by artists the library
// has exactly one album from.
//
// Both halves are `explore_index` rows: ownership is a column on the
// catalog, set by `PopulateLocalCrossReferences` from the library's
// MusicBrainz IDs, so this never touches the library tables and asks
// one query rather than one per artist.
//
// The artists are drawn most-popular-owned-album first, so a large
// library's pool is the part of it the user is likeliest to recognise
// rather than whichever artists sort first.
func (si *SearchIndex) unownedAlbumsBySinglyOwnedArtists(
	ctx context.Context,
	pool, limit int,
) []SearchIndexResult {
	return si.rowsByIDs(ctx, si.shelfIDs(
		ctx,
		`SELECT id FROM explore_index
		 WHERE entity_type = 2 /* release_group */
		   AND in_library = 0
		   AND artist_mbid IN (
		       SELECT artist_mbid FROM explore_index
		       WHERE entity_type = 2 /* release_group */
		         AND in_library = 1
		         AND artist_mbid != x''
		       GROUP BY artist_mbid
		       HAVING COUNT(*) = 1
		       ORDER BY MAX(popularity) DESC
		       LIMIT ?)
		 ORDER BY popularity DESC
		 LIMIT ?`,
		pool, limit,
	))
}

// hasCatalogRows reports whether there is a catalog at all.
//
// Deliberately "any row", not a count: the question is whether the page
// has a catalog to draw on, and a count of 1.1 M rows costs a scan to
// answer a yes/no.
func (si *SearchIndex) hasCatalogRows(ctx context.Context) bool {
	rows, err := si.db.QueryContextWith(ctx, "SELECT 1 FROM explore_index LIMIT 1")
	if err != nil {
		si.logger.Warn("explore shelves: catalog probe failed", "error", err)

		return false
	}

	defer func() { _ = rows.Close() }()

	return rows.Next()
}

// shelfIDs runs a shelf query that selects one id column.
func (si *SearchIndex) shelfIDs(
	ctx context.Context,
	query string,
	args ...any,
) []int64 {
	rows, err := si.db.QueryContextWith(ctx, query, args...)
	if err != nil {
		si.logger.Warn("explore shelves: query failed", "error", err)

		return nil
	}

	defer func() { _ = rows.Close() }()

	var ids []int64

	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}

	return ids
}

// distinctArtists lists the artist credits in a shelf, in order, once
// each — so a shelf that turns out to be about one artist can say so
// by name instead of using the plural heading.
func distinctArtists(rows []SearchIndexResult) []string {
	seen := make(map[string]struct{}, len(rows))
	names := make([]string, 0, len(rows))

	for _, row := range rows {
		name := strings.TrimSpace(row.ArtistName)
		if name == "" {
			continue
		}

		if _, ok := seen[name]; ok {
			continue
		}

		seen[name] = struct{}{}
		names = append(names, name)
	}

	return names
}
