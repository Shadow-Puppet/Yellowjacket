package explore

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"yellowjacket/backend/database"
)

// onePixelJPEG is the smallest thing image.Decode will accept, so
// setPrimary's thumbnail pass runs for real rather than bailing out.
//
//nolint:gochecknoglobals // fixture bytes, shared by the tests below
var onePixelJPEG = []byte{
	0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 0x4a, 0x46, 0x49, 0x46, 0x00, 0x01,
	0x01, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0xff, 0xdb, 0x00, 0x43,
	0x00, 0x08, 0x06, 0x06, 0x07, 0x06, 0x05, 0x08, 0x07, 0x07, 0x07, 0x09,
	0x09, 0x08, 0x0a, 0x0c, 0x14, 0x0d, 0x0c, 0x0b, 0x0b, 0x0c, 0x19, 0x12,
	0x13, 0x0f, 0x14, 0x1d, 0x1a, 0x1f, 0x1e, 0x1d, 0x1a, 0x1c, 0x1c, 0x20,
	0x24, 0x2e, 0x27, 0x20, 0x22, 0x2c, 0x23, 0x1c, 0x1c, 0x28, 0x37, 0x29,
	0x2c, 0x30, 0x31, 0x34, 0x34, 0x34, 0x1f, 0x27, 0x39, 0x3d, 0x38, 0x32,
	0x3c, 0x2e, 0x33, 0x34, 0x32, 0xff, 0xc0, 0x00, 0x0b, 0x08, 0x00, 0x01,
	0x00, 0x01, 0x01, 0x01, 0x11, 0x00, 0xff, 0xc4, 0x00, 0x1f, 0x00, 0x00,
	0x01, 0x05, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
	0x09, 0x0a, 0x0b, 0xff, 0xc4, 0x00, 0x14, 0x10, 0x01, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0xff, 0xda, 0x00, 0x08, 0x01, 0x01, 0x00, 0x00, 0x3f, 0x00, 0x37, 0xff,
	0xd9,
}

// newTestImageProvider returns a provider writing into a temp directory.
func newTestImageProvider(t *testing.T) *ArtistImageProvider {
	t.Helper()

	db := database.NewTestDB(t)

	return &ArtistImageProvider{
		db:      db,
		cache:   NewCache(db, slog.Default()),
		client:  http.DefaultClient,
		logger:  slog.Default(),
		baseDir: t.TempDir(),
	}
}

// candidateRows reports what fetchPrimary recorded, keyed by source URL.
func candidateRows(t *testing.T, p *ArtistImageProvider, mbid string) map[string]string {
	t.Helper()

	rows, err := p.db.QueryContext(
		"SELECT source_url, file_path FROM artist_images WHERE artist_mbid = ?",
		mbid,
	)
	if err != nil {
		t.Fatalf("query artist_images: %v", err)
	}

	defer func() { _ = rows.Close() }()

	out := map[string]string{}

	for rows.Next() {
		var url, path string
		if err := rows.Scan(&url, &path); err != nil {
			t.Fatalf("scan artist_images: %v", err)
		}

		out[url] = path
	}

	return out
}

// Exactly one candidate is downloaded, however many were offered — the
// rest are recorded as URLs so a later request is one fetch rather than
// a re-resolution of every upstream.
func TestFetchPrimaryDownloadsOnlyTheWinner(t *testing.T) {
	t.Parallel()

	var hits atomic.Int64

	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			hits.Add(1)

			_, _ = w.Write(onePixelJPEG)
		},
	))
	defer srv.Close()

	p := newTestImageProvider(t)

	const mbid = "11111111-1111-1111-1111-111111111111"

	candidates := []artistImageCandidate{
		{source: "fanart", url: srv.URL + "/a.jpg"},
		{source: "audiodb", url: srv.URL + "/b.jpg"},
		{source: "wikidata", url: srv.URL + "/c.jpg"},
	}

	p.fetchPrimary(mbid, candidates)

	if got := hits.Load(); got != 1 {
		t.Errorf("downloaded %d images, want 1", got)
	}

	dir := p.artistDir(mbid)

	// The portrait and its tiers, and nothing else: the winning
	// candidate is not also written under its own name.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read artist dir: %v", err)
	}

	for _, e := range entries {
		if !ArtistImageKeepNames()[e.Name()] {
			t.Errorf("unexpected file left on disk: %s", e.Name())
		}
	}

	if _, err := os.Stat(filepath.Join(dir, "primary.jpg")); err != nil {
		t.Errorf("no primary.jpg was written: %v", err)
	}

	recorded := candidateRows(t, p, mbid)
	if len(recorded) != len(candidates) {
		t.Errorf("recorded %d candidates, want %d", len(recorded), len(candidates))
	}

	if path := recorded[candidates[0].url]; path == "" {
		t.Error("the winning candidate has no file_path")
	}

	for _, c := range candidates[1:] {
		if path := recorded[c.url]; path != "" {
			t.Errorf("unfetched candidate %s recorded a path %q", c.source, path)
		}
	}
}

// A failing first candidate must fall through to the next.  The old loop
// keyed the primary on the index, so this left an artist with a stored
// image, no primary.jpg, and a .miss marker claiming it had no art.
func TestFetchPrimaryFallsThroughAFailure(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/dead.jpg" {
				w.WriteHeader(http.StatusNotFound)

				return
			}

			_, _ = w.Write(onePixelJPEG)
		},
	))
	defer srv.Close()

	p := newTestImageProvider(t)

	const mbid = "22222222-2222-2222-2222-222222222222"

	good := srv.URL + "/good.jpg"

	p.fetchPrimary(mbid, []artistImageCandidate{
		{source: "fanart", url: srv.URL + "/dead.jpg"},
		{source: "audiodb", url: good},
	})

	if _, err := os.Stat(p.primaryPath(mbid)); err != nil {
		t.Fatalf("no primary written after the first candidate failed: %v", err)
	}

	if p.isMiss(mbid) {
		t.Error("artist marked as having no artwork despite a successful fetch")
	}

	if path := candidateRows(t, p, mbid)[good]; path == "" {
		t.Error("the surviving candidate was not recorded as the stored one")
	}
}
