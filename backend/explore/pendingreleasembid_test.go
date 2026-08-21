package explore

import (
	"database/sql"
	"log/slog"
	"strconv"
	"testing"

	"yellowjacket/backend/database"
	"yellowjacket/backend/database/sql/sqlcgen"
)

// The release-group MBID backfill queried `release_groups`, a table
// plan 013 renamed to `albums`, so it failed on its first statement on
// every launch from e7748f1 until #189 -- and the pass swallowed that,
// because a query error and an empty library are the same early
// return. Nothing noticed for two reasons worth keeping in mind:
//
//   - the statement was **raw SQL**, so sqlc never read it. Every other
//     statement in the repo was renamed by the same change because sqlc
//     reads sql/schemas/ and cannot generate against a table that is not
//     declared. The two sqlc queries this now calls were written by 013
//     and left uncalled.
//   - it needs no network and no fixture library to reproduce. The
//     failure is at prepare time.

// seedPendingAlbum inserts an album whose files carried a release MBID
// but no release-group MBID, which is what `library.updateMBIDs`
// leaves behind for this pass to resolve.
func seedPendingAlbum(
	t *testing.T,
	db *database.DB,
	name, pendingMBID string,
) int64 {
	t.Helper()

	res, err := db.ExecContext(
		"INSERT INTO albums (name, artist_credit, pending_release_mbid) "+
			"VALUES (?, ?, ?)",
		name, "Test Artist", pendingMBID,
	)
	if err != nil {
		t.Fatalf("insert albums row: %v", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}

	return id
}

func newPendingTestService(db *database.DB) *Service {
	return &Service{db: db, logger: slog.Default()}
}

// TestPendingReleaseMBIDsRunsAgainstTheRealSchema is the regression.
//
// It asserts the statement *runs*, which is the whole of what was
// broken: against the old raw SQL this returns
// "no such table: release_groups" rather than a row.
func TestPendingReleaseMBIDsRunsAgainstTheRealSchema(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	e := newPendingTestService(db)

	want := seedPendingAlbum(t, db, "Pending Album", "release-mbid-1")

	pending, err := e.pendingReleaseMBIDs(db.Ctx)
	if err != nil {
		t.Fatalf("the backfill's query failed: %v", err)
	}

	if len(pending) != 1 {
		t.Fatalf("got %d pending albums, want 1", len(pending))
	}

	if pending[0].ID != want {
		t.Errorf("got album id %d, want %d", pending[0].ID, want)
	}

	if got := pending[0].PendingReleaseMbid.String; got != "release-mbid-1" {
		t.Errorf("got pending mbid %q, want %q", got, "release-mbid-1")
	}
}

// TestOnlyUnresolvedAlbumsAreReturned pins the two conditions that make
// the pass idempotent, since between them they are what stops it doing
// the same MusicBrainz lookups on every launch forever.
func TestOnlyUnresolvedAlbumsAreReturned(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	e := newPendingTestService(db)

	pendingID := seedPendingAlbum(t, db, "Still Pending", "release-mbid-1")

	// Already resolved: it has a real MBID, so there is nothing to
	// look up even though a marker is still sitting on it.
	resolved := seedPendingAlbum(t, db, "Already Resolved", "release-mbid-2")
	if err := db.Queries.SetAlbumMBID(db.Ctx, sqlcgen.SetAlbumMBIDParams{
		Mbid: sql.NullString{String: "rg-mbid", Valid: true},
		ID:   resolved,
	}); err != nil {
		t.Fatalf("set album mbid: %v", err)
	}

	// Never had a release MBID to resolve in the first place, which is
	// most of a library.
	seedPendingAlbum(t, db, "Nothing Pending", "")

	pending, err := e.pendingReleaseMBIDs(db.Ctx)
	if err != nil {
		t.Fatalf("the backfill's query failed: %v", err)
	}

	if len(pending) != 1 || pending[0].ID != pendingID {
		t.Fatalf(
			"got %d albums %v, want only the unresolved one (%d)",
			len(pending), pending, pendingID,
		)
	}
}

// TestResolvingClearsTheMarker is the other half: once the lookup has
// answered, the album must stop being a candidate, or the pass repeats
// the same live MusicBrainz call on every launch.
func TestResolvingClearsTheMarker(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	e := newPendingTestService(db)

	id := seedPendingAlbum(t, db, "Pending Album", "release-mbid-1")

	// The writer, deliberately: this is an UPDATE, and the read pool
	// would refuse it at runtime.
	if err := db.Queries.ResolveAlbumPendingReleaseMBID(
		db.Ctx,
		sqlcgen.ResolveAlbumPendingReleaseMBIDParams{
			Mbid: sql.NullString{String: "resolved-rg-mbid", Valid: true},
			ID:   id,
		},
	); err != nil {
		t.Fatalf("resolve pending release mbid: %v", err)
	}

	pending, err := e.pendingReleaseMBIDs(db.Ctx)
	if err != nil {
		t.Fatalf("the backfill's query failed: %v", err)
	}

	if len(pending) != 0 {
		t.Fatalf("a resolved album is still a candidate: %v", pending)
	}

	album, err := db.ReadQueries.GetAlbum(db.Ctx, id)
	if err != nil {
		t.Fatalf("get album: %v", err)
	}

	if album.Mbid.String != "resolved-rg-mbid" {
		t.Errorf("album mbid = %q, want the resolved one", album.Mbid.String)
	}

	if album.PendingReleaseMbid.Valid &&
		album.PendingReleaseMbid.String != "" {
		t.Errorf(
			"the pending marker survived as %q",
			album.PendingReleaseMbid.String,
		)
	}
}

// TestAResolvedMBIDIsNeverOverwritten covers the guard in the UPDATE.
//
// The pass runs against rows it read earlier, and a rescan can resolve
// an album from its tags in between -- a real MBID from the file must
// win over one this pass inferred from a release.
func TestAResolvedMBIDIsNeverOverwritten(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)

	id := seedPendingAlbum(t, db, "Pending Album", "release-mbid-1")

	if err := db.Queries.SetAlbumMBID(db.Ctx, sqlcgen.SetAlbumMBIDParams{
		Mbid: sql.NullString{String: "from-the-tags", Valid: true},
		ID:   id,
	}); err != nil {
		t.Fatalf("set album mbid: %v", err)
	}

	if err := db.Queries.ResolveAlbumPendingReleaseMBID(
		db.Ctx,
		sqlcgen.ResolveAlbumPendingReleaseMBIDParams{
			Mbid: sql.NullString{String: "from-the-backfill", Valid: true},
			ID:   id,
		},
	); err != nil {
		t.Fatalf("resolve pending release mbid: %v", err)
	}

	album, err := db.ReadQueries.GetAlbum(db.Ctx, id)
	if err != nil {
		t.Fatalf("get album: %v", err)
	}

	if album.Mbid.String != "from-the-tags" {
		t.Errorf(
			"album mbid = %q, want the tagged one to have won",
			album.Mbid.String,
		)
	}
}

// TestThePassIsBounded checks the LIMIT.
//
// Each row costs a live MusicBrainz lookup on a 1 req/s limiter shared
// with every page the user can open, so an unbounded read is a run that
// lasts as long as the library is untagged. The sqlc query 013 wrote
// had no LIMIT; the raw statement it was replacing did.
func TestThePassIsBounded(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	e := newPendingTestService(db)

	for i := range releaseGroupMBIDBackfillMaxPerRun + 10 {
		seedPendingAlbum(
			t, db,
			"Album "+string(rune('A'+i%26))+strconv.Itoa(i),
			"release-mbid-"+strconv.Itoa(i),
		)
	}

	pending, err := e.pendingReleaseMBIDs(db.Ctx)
	if err != nil {
		t.Fatalf("the backfill's query failed: %v", err)
	}

	if len(pending) != releaseGroupMBIDBackfillMaxPerRun {
		t.Errorf(
			"got %d albums, want the run bounded at %d",
			len(pending), releaseGroupMBIDBackfillMaxPerRun,
		)
	}
}
