package maintenance

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"yellowjacket/backend/database"
)

// errTestJobFailed stands in for a job returning an error.
var errTestJobFailed = errors.New("job failed")

func testRunner() *Runner {
	return NewRunner(slog.Default())
}

func TestRunnerRunsRegisteredJobs(t *testing.T) {
	t.Parallel()

	r := testRunner()

	var ran int

	r.Register(Job{
		Name: "counter",
		Run: func(_ context.Context) (Result, error) {
			ran++

			return Result{RowsDeleted: 3}, nil
		},
	})

	total := r.RunDue(context.Background())

	if ran != 1 {
		t.Errorf("job ran %d times, want 1", ran)
	}

	if total.RowsDeleted != 3 {
		t.Errorf("RowsDeleted = %d, want 3", total.RowsDeleted)
	}
}

// A job must not run again before its interval has elapsed, so hooking
// the runner to a frequent trigger stays cheap.
func TestRunnerRespectsMinInterval(t *testing.T) {
	t.Parallel()

	r := testRunner()

	var ran int

	r.Register(Job{
		Name:        "throttled",
		MinInterval: time.Hour,
		Run: func(_ context.Context) (Result, error) {
			ran++

			return Result{}, nil
		},
	})

	r.RunDue(context.Background())
	r.RunDue(context.Background())
	r.RunDue(context.Background())

	if ran != 1 {
		t.Errorf("job ran %d times despite 1h interval, want 1", ran)
	}
}

// One failing job must not prevent the others from running — janitorial
// work is best-effort and the next run retries.
func TestRunnerContinuesAfterFailure(t *testing.T) {
	t.Parallel()

	r := testRunner()

	var secondRan bool

	r.Register(Job{
		Name: "failing",
		Run: func(_ context.Context) (Result, error) {
			return Result{}, errTestJobFailed
		},
	})
	r.Register(Job{
		Name: "healthy",
		Run: func(_ context.Context) (Result, error) {
			secondRan = true

			return Result{}, nil
		},
	})

	r.RunDue(context.Background())

	if !secondRan {
		t.Error("second job did not run after the first failed")
	}
}

// Registering the same name twice replaces the job rather than
// accumulating duplicates, so wiring code is safe to re-run.
func TestRunnerRegisterReplaces(t *testing.T) {
	t.Parallel()

	r := testRunner()

	noop := func(_ context.Context) (Result, error) { return Result{}, nil }

	r.Register(Job{Name: "dup", Run: noop})
	r.Register(Job{Name: "dup", Run: noop})

	if names := r.JobNames(); len(names) != 1 {
		t.Errorf("JobNames() = %v, want one entry", names)
	}
}

func TestRunnerStopsOnCancelledContext(t *testing.T) {
	t.Parallel()

	r := testRunner()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var ran bool

	r.Register(Job{
		Name: "should-not-run",
		Run: func(_ context.Context) (Result, error) {
			ran = true

			return Result{}, nil
		},
	})

	r.RunDue(ctx)

	if ran {
		t.Error("job ran despite cancelled context")
	}
}

// ---------------------------------------------------------------------------
// Sweeps
// ---------------------------------------------------------------------------

func TestExpiredHTTPCacheJob(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)

	if _, err := db.ExecContext(
		`INSERT INTO http_cache (url_key, response, expires_at)
		 VALUES ('stale', '{}', datetime('now', '-1 day')),
		        ('fresh', '{}', datetime('now', '+1 day'))`,
	); err != nil {
		t.Fatalf("seed http_cache: %v", err)
	}

	result, err := ExpiredHTTPCacheJob(db).Run(context.Background())
	if err != nil {
		t.Fatalf("run job: %v", err)
	}

	if result.RowsDeleted != 1 {
		t.Errorf("RowsDeleted = %d, want 1", result.RowsDeleted)
	}

	var remaining string

	rows, err := db.QueryContext("SELECT url_key FROM http_cache")
	if err != nil {
		t.Fatalf("query http_cache: %v", err)
	}

	defer func() { _ = rows.Close() }()

	if rows.Next() {
		_ = rows.Scan(&remaining)
	}

	if remaining != "fresh" {
		t.Errorf("remaining row = %q, want the unexpired one", remaining)
	}
}

// The covers sweep must delete files no cover_art row references while
// keeping the referenced original and every derived variant.
func TestOrphanedCoverFilesJob(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	dir := t.TempDir()

	// Stand-in for library.CoverArtFileSet.
	expand := func(original string) []string {
		base := filepath.Base(original)
		base = base[:len(base)-len(filepath.Ext(base))]

		return []string{
			original,
			filepath.Join(filepath.Dir(original), base+"_sm.jpg"),
			filepath.Join(filepath.Dir(original), base+"_md.jpg"),
		}
	}

	keep := []string{"live.jpg", "live_sm.jpg", "live_md.jpg"}
	drop := []string{"orphan.jpg", "orphan_sm.jpg", "stray_md.jpg"}

	for _, name := range slices.Concat(keep, drop) {
		if err := os.WriteFile(
			filepath.Join(dir, name), []byte("img"), 0o600,
		); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	if _, err := db.ExecContext(
		`INSERT INTO cover_art (is_embedded, file_path, mime_type)
		 VALUES (0, ?, 'image/jpeg')`,
		filepath.Join(dir, "live.jpg"),
	); err != nil {
		t.Fatalf("seed cover_art: %v", err)
	}

	result, err := OrphanedCoverFilesJob(db, dir, expand).
		Run(context.Background())
	if err != nil {
		t.Fatalf("run job: %v", err)
	}

	if result.FilesDeleted != int64(len(drop)) {
		t.Errorf("FilesDeleted = %d, want %d", result.FilesDeleted, len(drop))
	}

	for _, name := range keep {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("referenced file %s was deleted", name)
		}
	}

	for _, name := range drop {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("orphan %s survived the sweep", name)
		}
	}
}

// An empty live set means the query saw nothing, not that every cover is
// garbage.  The sweep must refuse to empty the directory in that case.
func TestOrphanedCoverFilesJob_EmptyLiveSetIsNoOp(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	dir := t.TempDir()

	path := filepath.Join(dir, "something.jpg")
	if err := os.WriteFile(path, []byte("img"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	expand := func(p string) []string { return []string{p} }

	result, err := OrphanedCoverFilesJob(db, dir, expand).
		Run(context.Background())
	if err != nil {
		t.Fatalf("run job: %v", err)
	}

	if result.FilesDeleted != 0 {
		t.Errorf("FilesDeleted = %d, want 0", result.FilesDeleted)
	}

	if _, err := os.Stat(path); err != nil {
		t.Error("sweep emptied the directory on an empty live set")
	}
}

// Artwork for an artist in the library is kept regardless of age;
// artwork for a browsed artist ages out.
func TestOrphanedArtistImagesJob(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	dir := t.TempDir()

	const (
		ownedMBID   = "11111111-1111-1111-1111-111111111111"
		browsedMBID = "22222222-2222-2222-2222-222222222222"
		recentMBID  = "33333333-3333-3333-3333-333333333333"
	)

	for _, mbid := range []string{ownedMBID, browsedMBID, recentMBID} {
		artistDir := filepath.Join(dir, mbid)
		if err := os.MkdirAll(artistDir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", mbid, err)
		}

		if err := os.WriteFile(
			filepath.Join(artistDir, "primary.jpg"), []byte("img"), 0o600,
		); err != nil {
			t.Fatalf("write image: %v", err)
		}
	}

	// The owned artist is in the library.
	if _, err := db.ExecContext(
		`INSERT INTO artists (name, mbid) VALUES ('Owned', ?)`, ownedMBID,
	); err != nil {
		t.Fatalf("seed artists: %v", err)
	}

	old := time.Now().Add(-200 * 24 * time.Hour)

	for _, tc := range []struct {
		mbid    string
		created time.Time
	}{
		{ownedMBID, old},
		{browsedMBID, old},
		{recentMBID, time.Now()},
	} {
		if _, err := db.ExecContext(
			`INSERT INTO artist_images
			   (artist_mbid, source, source_url, file_path, created_at)
			 VALUES (?, 'test', 'http://x', ?, ?)`,
			tc.mbid,
			filepath.Join(dir, tc.mbid, "primary.jpg"),
			tc.created,
		); err != nil {
			t.Fatalf("seed artist_images for %s: %v", tc.mbid, err)
		}
	}

	if _, err := OrphanedArtistImagesJob(db, dir).
		Run(context.Background()); err != nil {
		t.Fatalf("run job: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, ownedMBID)); err != nil {
		t.Error("artwork for a library artist was evicted")
	}

	if _, err := os.Stat(filepath.Join(dir, recentMBID)); err != nil {
		t.Error("recently fetched artwork was evicted")
	}

	if _, err := os.Stat(filepath.Join(dir, browsedMBID)); !os.IsNotExist(err) {
		t.Error("stale browsed artwork survived the sweep")
	}

	// The rows must go with the files.
	rows, err := db.QueryContext(
		"SELECT COUNT(*) FROM artist_images WHERE artist_mbid = ?",
		browsedMBID,
	)
	if err != nil {
		t.Fatalf("count rows: %v", err)
	}

	defer func() { _ = rows.Close() }()

	var n int

	if rows.Next() {
		_ = rows.Scan(&n)
	}

	if n != 0 {
		t.Errorf("artist_images rows for evicted artist = %d, want 0", n)
	}
}

func TestExpiredProxyCacheJob(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	stale := filepath.Join(dir, "stale.jpg")
	fresh := filepath.Join(dir, "fresh.jpg")

	for _, p := range []string{stale, fresh} {
		if err := os.WriteFile(p, []byte("img"), 0o600); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	old := time.Now().Add(-60 * 24 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	result, err := ExpiredProxyCacheJob(dir).Run(context.Background())
	if err != nil {
		t.Fatalf("run job: %v", err)
	}

	if result.FilesDeleted != 1 {
		t.Errorf("FilesDeleted = %d, want 1", result.FilesDeleted)
	}

	if _, err := os.Stat(fresh); err != nil {
		t.Error("recently written thumbnail was evicted")
	}

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("stale thumbnail survived the sweep")
	}
}

// A missing directory is normal on a fresh install and must not error.
func TestSweepMissingDirectory(t *testing.T) {
	t.Parallel()

	result, err := ExpiredProxyCacheJob(
		filepath.Join(t.TempDir(), "does-not-exist"),
	).Run(context.Background())
	if err != nil {
		t.Fatalf("missing directory returned an error: %v", err)
	}

	if result.FilesDeleted != 0 {
		t.Errorf("FilesDeleted = %d, want 0", result.FilesDeleted)
	}
}
