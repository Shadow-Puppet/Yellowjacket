package maintenance

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"yellowjacket/backend/database"
	"yellowjacket/backend/explore"
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
//
// The directories are laid out by explore.ArtistImageDir rather than by
// this test, which is the point: the job used to join the bare MBID,
// name a path that has never existed, delete the rows and leave every
// file on disk.  A test that invents its own flat layout agrees with
// the bug.
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
		artistDir := explore.ArtistImageDir(dir, mbid)
		if err := os.MkdirAll(artistDir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", mbid, err)
		}

		if err := os.WriteFile(
			filepath.Join(artistDir, "primary.jpg"), []byte("img"), 0o600,
		); err != nil {
			t.Fatalf("write image: %v", err)
		}
	}

	// The owned artist is in the library - which means a *file* says
	// so.  An artists row on its own is the phantom the file-shaped
	// schema removed, and it is not ownership.
	database.InsertTestTrack(t, db, database.TestTrack{
		FilePath:   "/music/owned.mp3",
		Artist:     "Owned",
		ArtistMBID: ownedMBID,
	})

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
			filepath.Join(explore.ArtistImageDir(dir, tc.mbid), "primary.jpg"),
			tc.created,
		); err != nil {
			t.Fatalf("seed artist_images for %s: %v", tc.mbid, err)
		}
	}

	if _, err := OrphanedArtistImagesJob(db, dir, explore.ArtistImageDir).
		Run(context.Background()); err != nil {
		t.Fatalf("run job: %v", err)
	}

	if _, err := os.Stat(explore.ArtistImageDir(dir, ownedMBID)); err != nil {
		t.Error("artwork for a library artist was evicted")
	}

	if _, err := os.Stat(explore.ArtistImageDir(dir, recentMBID)); err != nil {
		t.Error("recently fetched artwork was evicted")
	}

	if _, err := os.Stat(explore.ArtistImageDir(dir, browsedMBID)); !os.IsNotExist(err) {
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

// A directory holding an artist's portrait plus the candidates an older
// version downloaded keeps the portrait and loses the candidates.
func TestStrayArtistImageFilesJob(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	const mbid = "44444444-4444-4444-4444-444444444444"

	artistDir := explore.ArtistImageDir(dir, mbid)
	if err := os.MkdirAll(artistDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	keep := []string{
		"primary.jpg", "primary_sm.jpg", "primary_md.jpg",
		"primary_lg.jpg", ".miss",
	}
	strays := []string{"audiodb_0.jpg", "fanart_1.jpg", "wikidata_3.jpg"}

	for _, name := range append(append([]string{}, keep...), strays...) {
		if err := os.WriteFile(
			filepath.Join(artistDir, name), []byte("xx"), 0o600,
		); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	result, err := StrayArtistImageFilesJob(dir, explore.ArtistImageKeepNames()).
		Run(context.Background())
	if err != nil {
		t.Fatalf("run job: %v", err)
	}

	if result.FilesDeleted != int64(len(strays)) {
		t.Errorf("FilesDeleted = %d, want %d", result.FilesDeleted, len(strays))
	}

	for _, name := range keep {
		if _, err := os.Stat(filepath.Join(artistDir, name)); err != nil {
			t.Errorf("%s was swept and should not have been", name)
		}
	}

	for _, name := range strays {
		if _, err := os.Stat(
			filepath.Join(artistDir, name),
		); !os.IsNotExist(err) {
			t.Errorf("%s survived the sweep", name)
		}
	}
}

// An empty keep set would condemn every file, which is never what a
// caller means — it is a failed lookup, not an empty live set.
func TestStrayArtistImageFilesJobRefusesEmptyKeepSet(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	const mbid = "55555555-5555-5555-5555-555555555555"

	artistDir := explore.ArtistImageDir(dir, mbid)
	if err := os.MkdirAll(artistDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	primary := filepath.Join(artistDir, "primary.jpg")
	if err := os.WriteFile(primary, []byte("xx"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := StrayArtistImageFilesJob(dir, nil).
		Run(context.Background()); err != nil {
		t.Fatalf("run job: %v", err)
	}

	if _, err := os.Stat(primary); err != nil {
		t.Error("an empty keep set emptied the directory")
	}
}

// TestOrphanedArtistImagesJob_EvictsOverBudget pins the ceiling.
//
// Age alone bounds nothing: a browsing session fetches portraits for
// hundreds of artists in an afternoon and every one of them is inside
// the retention window. A real install held art for 5,770 artists in a
// 1,301-artist library. Art for an artist the user owns is outside the
// budget and must survive an eviction that takes everything else.
func TestOrphanedArtistImagesJob_EvictsOverBudget(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)
	dir := t.TempDir()

	const ownedMBID = "aaaaaaaa-0000-0000-0000-000000000000"

	database.InsertTestTrack(t, db, database.TestTrack{
		FilePath:   "/music/owned.mp3",
		Artist:     "Owned",
		ArtistMBID: ownedMBID,
	})

	// Each artist's art is a quarter of the budget, so four browsed
	// artists sit exactly on it and the fifth pushes it over.
	blob := make([]byte, browsedArtBudget/4)

	seed := func(mbid string, created time.Time) {
		t.Helper()

		artistDir := explore.ArtistImageDir(dir, mbid)
		if err := os.MkdirAll(artistDir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", mbid, err)
		}

		if err := os.WriteFile(
			filepath.Join(artistDir, "primary.jpg"), blob, 0o600,
		); err != nil {
			t.Fatalf("write image: %v", err)
		}

		if _, err := db.ExecContext(
			`INSERT INTO artist_images
			   (artist_mbid, source, source_url, file_path, created_at)
			 VALUES (?, 'test', 'http://x', ?, ?)`,
			mbid, filepath.Join(artistDir, "primary.jpg"), created,
		); err != nil {
			t.Fatalf("seed artist_images for %s: %v", mbid, err)
		}
	}

	// All inside the retention window, so only the budget can evict.
	now := time.Now()
	seed(ownedMBID, now)

	browsed := []string{
		"bbbbbbbb-0000-0000-0000-000000000000",
		"cccccccc-0000-0000-0000-000000000000",
		"dddddddd-0000-0000-0000-000000000000",
		"eeeeeeee-0000-0000-0000-000000000000",
		"ffffffff-0000-0000-0000-000000000000",
	}

	for i, mbid := range browsed {
		seed(mbid, now.Add(-time.Duration(len(browsed)-i)*time.Hour))
	}

	result, err := OrphanedArtistImagesJob(db, dir, explore.ArtistImageDir).
		Run(context.Background())
	if err != nil {
		t.Fatalf("run job: %v", err)
	}

	if result.FilesDeleted == 0 {
		t.Error("nothing was evicted despite being over budget")
	}

	// The oldest browsed artist goes first.
	if _, err := os.Stat(explore.ArtistImageDir(dir, browsed[0])); !os.IsNotExist(err) {
		t.Error("the least recently fetched art survived the budget pass")
	}

	// The owned artist is never in the budget.
	if _, err := os.Stat(explore.ArtistImageDir(dir, ownedMBID)); err != nil {
		t.Error("artwork for a library artist was evicted by the budget pass")
	}

	// And the newest browsed art survives, because eviction stops as
	// soon as the rest fits.
	if _, err := os.Stat(explore.ArtistImageDir(dir, browsed[len(browsed)-1])); err != nil {
		t.Error("eviction did not stop once under budget")
	}
}

// TestExpiredHTTPCacheJob_TrimsToBudget pins the ceiling that makes a
// year-long entity TTL safe: once answers stop expiring, expiry stops
// being a bound and something else has to be.
func TestExpiredHTTPCacheJob_TrimsToBudget(t *testing.T) {
	t.Parallel()

	db := database.NewTestDB(t)

	// Six rows of a third of the budget each: two fit, the rest go.
	blob := make([]byte, httpCacheBudget/3)

	for i := range 6 {
		if _, err := db.ExecContext(
			`INSERT INTO http_cache (url_key, response, expires_at)
			 VALUES (?, ?, ?)`,
			fmt.Sprintf("key-%d", i), blob,
			time.Now().Add(time.Duration(i+1)*24*time.Hour),
		); err != nil {
			t.Fatalf("seed http_cache: %v", err)
		}
	}

	if _, err := ExpiredHTTPCacheJob(db).Run(context.Background()); err != nil {
		t.Fatalf("run job: %v", err)
	}

	var total int64
	if err := db.QueryRowWriter(
		"SELECT COALESCE(SUM(LENGTH(response)), 0) FROM http_cache",
	).Scan(&total); err != nil {
		t.Fatalf("measure http_cache: %v", err)
	}

	if total > httpCacheBudget {
		t.Errorf("http_cache is %d bytes, over the %d budget", total, httpCacheBudget)
	}

	// The longest-lived answers are the ones kept.
	var kept string
	if err := db.QueryRowWriter(
		"SELECT url_key FROM http_cache ORDER BY expires_at DESC LIMIT 1",
	).Scan(&kept); err != nil {
		t.Fatalf("read surviving row: %v", err)
	}

	if kept != "key-5" {
		t.Errorf("kept %q, want the longest-lived row", kept)
	}
}
