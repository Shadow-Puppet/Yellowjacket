package database

import (
	"database/sql"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync/atomic"
	"testing"

	_ "modernc.org/sqlite" // Register sqlite driver.

	"yellowjacket/backend/database/sql/sqlcgen"
)

// NewTestDB returns an in-memory SQLite database shaped like the real
// one: a single-writer handle and a separate query-only read pool over
// the same database, built by the same applySchema production uses.
//
// The two handles matter. The test DB used to be one shared connection
// with readDB nil, so `reader()` returned the *writer* — which is how a
// query-shaped write (`INSERT ... RETURNING` through QueryContext)
// passed every test and then failed for a user with "attempt to write a
// readonly database". `TestNoWritesOnTheReadPool` had to walk the source
// tree to catch what a test could not.
//
// A shared-cache in-memory database is what lets two handles see one
// database; the connections are capped the way production caps them.
// It is closed when the test completes via t.Cleanup.
func NewTestDB(t *testing.T) *DB {
	t.Helper()

	// A per-test name, so parallel tests do not share a database.
	dsn := fmt.Sprintf(
		"file:testdb%d?mode=memory&cache=shared&_pragma=busy_timeout(5000)",
		testDBSeq.Add(1),
	)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("could not open test database: %v", err)
	}

	db.SetMaxOpenConns(1)

	ctx := t.Context()

	if err := applyPRAGMAs(ctx, db); err != nil {
		t.Fatalf("could not apply PRAGMAs: %v", err)
	}

	// The same call production uses, so a test database and a real one
	// cannot diverge.
	if err := applySchema(ctx, db); err != nil {
		t.Fatalf("could not apply schema: %v", err)
	}

	// Insert a sentinel library row at id=0 so audio_files inserts
	// using the DEFAULT library_id=0 satisfy the FK constraint.
	if _, err := db.ExecContext(
		ctx,
		"INSERT INTO libraries (id, name, path) VALUES (0, 'Test', '/test')",
	); err != nil {
		t.Fatalf("could not insert test library: %v", err)
	}

	readDB, err := sql.Open("sqlite", dsn+"&_pragma=query_only(true)")
	if err != nil {
		t.Fatalf("could not open test read pool: %v", err)
	}

	readDB.SetMaxOpenConns(readPoolConns)

	t.Cleanup(func() {
		_ = readDB.Close()
		_ = db.Close()
	})

	return &DB{
		db:          db,
		readDB:      readDB,
		Ctx:         ctx,
		Queries:     sqlcgen.New(db),
		ReadQueries: sqlcgen.New(readDB),
		logger:      slog.Default(),
	}
}

// testDBSeq names each test database uniquely, so parallel tests do not
// share one through the shared cache.
var testDBSeq atomic.Int64

// NewTestDBWithLibrary returns a test DB with a library row
// pre-inserted. Returns the DB and the library ID.
func NewTestDBWithLibrary(
	t *testing.T,
	name, libPath string,
) (*DB, int64) {
	t.Helper()

	db := NewTestDB(t)

	lib, err := db.Queries.CreateLibrary(
		db.Ctx,
		sqlcgen.CreateLibraryParams{
			Name: name,
			Path: libPath,
		},
	)
	if err != nil {
		t.Fatalf("could not create test library: %v", err)
	}

	return db, lib.ID
}

// TestTrack describes one file to seed into a test database.  Zero
// values are fine: only FilePath is required.
type TestTrack struct {
	FilePath      string
	Title         string
	Artist        string
	ArtistMBID    string
	Album         string
	AlbumArtist   string
	AlbumMBID     string
	RecordingMBID string
	Genres        []string
	TrackNumber   int64
	DiscNumber    int64
	TotalTracks   int64
	Year          int64
	LengthMs      int64
	LibraryID     int64
	PlayCount     int64
	TagStatus     string
	GroupKey      string
	// SkipSearchIndex leaves the file out of the FTS index, for the
	// tests that assert on a rebuild putting it there.
	SkipSearchIndex bool
}

// InsertTestTrack seeds one file, with the artist and album its tags
// name, and returns the audio_files id.
//
// There is one of these because there is one shape.  Twenty test files
// used to carry their own seeder, each inserting a recording, an artist
// credit, a credit-artist link and a release-group link in the right
// order - which is exactly the ceremony the schema change removed, and
// exactly why every one of those seeders was subtly different.
func InsertTestTrack(t *testing.T, db *DB, tr TestTrack) int64 {
	t.Helper()

	if tr.Title == "" {
		tr.Title = "Test Track"
	}

	if tr.Artist == "" {
		tr.Artist = "Test Artist"
	}

	if tr.TagStatus == "" {
		tr.TagStatus = "untagged"
	}

	artist, err := db.Queries.UpsertArtist(db.Ctx, sqlcgen.UpsertArtistParams{
		Name: tr.Artist,
		Mbid: nullString(tr.ArtistMBID),
	})
	if err != nil {
		t.Fatalf("seed artist: %v", err)
	}

	artistID := sql.NullInt64{Int64: artist.ID, Valid: true}
	albumID := sql.NullInt64{}

	if tr.Album != "" {
		credit := tr.AlbumArtist
		if credit == "" {
			credit = tr.Artist
		}

		album, albErr := db.Queries.UpsertAlbum(db.Ctx, sqlcgen.UpsertAlbumParams{
			Name:         tr.Album,
			ArtistCredit: credit,
			ArtistID:     artistID,
			Year:         nullInt64(tr.Year),
		})
		if albErr != nil {
			t.Fatalf("seed album: %v", albErr)
		}

		if tr.AlbumMBID != "" {
			if err := db.Queries.SetAlbumMBID(db.Ctx, sqlcgen.SetAlbumMBIDParams{
				Mbid: nullString(tr.AlbumMBID),
				ID:   album.ID,
			}); err != nil {
				t.Fatalf("seed album mbid: %v", err)
			}
		}

		albumID = sql.NullInt64{Int64: album.ID, Valid: true}
	}

	af, err := db.Queries.CreateAudioFile(db.Ctx, sqlcgen.CreateAudioFileParams{
		FilePath:           tr.FilePath,
		LibraryID:          tr.LibraryID,
		LengthMilliseconds: tr.LengthMs,
		Title:              tr.Title,
		ArtistCredit:       tr.Artist,
		ArtistID:           artistID,
		AlbumID:            albumID,
		TrackNumber:        nullInt64(tr.TrackNumber),
		DiscNumber:         nullInt64(tr.DiscNumber),
		TotalTracks:        nullInt64(tr.TotalTracks),
		Year:               nullInt64(tr.Year),
		RecordingMbid:      nullString(tr.RecordingMBID),
		Basename:           filepath.Base(tr.FilePath),
		GroupKey:           tr.GroupKey,
		TagStatus:          tr.TagStatus,
	})
	if err != nil {
		t.Fatalf("seed audio file %q: %v", tr.FilePath, err)
	}

	for _, name := range tr.Genres {
		g, gErr := db.Queries.UpsertGenre(db.Ctx, name)
		if gErr != nil {
			t.Fatalf("seed genre %q: %v", name, gErr)
		}

		if err := db.Queries.LinkFileGenre(db.Ctx, sqlcgen.LinkFileGenreParams{
			AudioFileID: af.ID,
			GenreID:     g.ID,
		}); err != nil {
			t.Fatalf("seed file genre: %v", err)
		}
	}

	if tr.PlayCount > 0 {
		if _, err := db.db.ExecContext(db.Ctx,
			"UPDATE audio_files SET play_count = ? WHERE id = ?",
			tr.PlayCount, af.ID,
		); err != nil {
			t.Fatalf("seed play count: %v", err)
		}
	}

	if !tr.SkipSearchIndex {
		if err := db.InsertSearchIndex(
			af.ID, tr.FilePath, tr.Title, tr.Artist, tr.Album,
		); err != nil {
			t.Fatalf("seed search index: %v", err)
		}
	}

	return af.ID
}

func nullString(v string) sql.NullString {
	if v == "" {
		return sql.NullString{}
	}

	return sql.NullString{String: v, Valid: true}
}

func nullInt64(v int64) sql.NullInt64 {
	if v == 0 {
		return sql.NullInt64{}
	}

	return sql.NullInt64{Int64: v, Valid: true}
}
