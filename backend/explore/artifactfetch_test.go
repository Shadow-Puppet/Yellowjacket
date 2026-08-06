package explore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"

	"yellowjacket/backend/database"
)

// artifactServer serves a compressed artifact and its checksum the way
// the Gitea generic package registry does.
func artifactServer(t *testing.T, body []byte) *httptest.Server {
	t.Helper()

	sum := sha256.Sum256(body)
	checksum := hex.EncodeToString(sum[:]) + "  " + coreArtifactFile + "\n"

	mux := http.NewServeMux()

	mux.HandleFunc("/"+coreArtifactChecksum,
		func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(checksum))
		})

	// ServeContent gives the stub real Range support, so the resume path
	// is exercised against the same semantics as the package registry.
	mux.HandleFunc("/"+coreArtifactFile,
		func(w http.ResponseWriter, r *http.Request) {
			http.ServeContent(
				w, r, coreArtifactFile, time.Time{}, bytes.NewReader(body))
		})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv
}

// compressArtifact zstd-compresses a file the way CI publishes it.
func compressArtifact(t *testing.T, path string) []byte {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}

	enc, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatalf("zstd writer: %v", err)
	}

	defer func() { _ = enc.Close() }()

	return enc.EncodeAll(raw, nil)
}

// withArtifactEnv points the fetcher at a stub server and gives it an
// isolated data directory.
func withArtifactEnv(t *testing.T, baseURL string) {
	t.Helper()

	t.Setenv(artifactURLEnv, baseURL)
	t.Setenv("YJ_HOME", t.TempDir())
}

func TestFetchAndMergeArtifactEndToEnd(t *testing.T) {
	src := writeTestArtifact(t, validMeta(), []artifactRow{
		{"artist", artA, "Artist A", "Artist A", artA, 5000},
		{"release_group", rgA, "Album A", "Artist A", artA, 3000},
		{"recording", recA, "Song A", "Artist A", artA, 2000},
	})

	srv := artifactServer(t, compressArtifact(t, src))
	withArtifactEnv(t, srv.URL+"/")

	db := database.NewTestDB(t)
	si := NewSearchIndex(db, nil, nil, testLogger())

	if err := si.tryCoreArtifact(context.Background()); err != nil {
		t.Fatalf("tryCoreArtifact: %v", err)
	}

	var got int
	if err := db.QueryRowWriter(
		"SELECT COUNT(*) FROM explore_index",
	).Scan(&got); err != nil {
		t.Fatalf("count rows: %v", err)
	}

	if got != 3 {
		t.Errorf("merged %d rows, want 3", got)
	}

	if !si.artifactAlreadyMerged() {
		t.Error("artifact merge not recorded; a restart would re-download it")
	}

	// Staging must not keep a few hundred MB around after success.
	staging := filepath.Join(os.Getenv("YJ_HOME"), "data", "explore-staging")
	for _, name := range []string{coreArtifactFile, "core-index.db"} {
		if _, err := os.Stat(filepath.Join(staging, name)); err == nil {
			t.Errorf("%s left behind in staging after import", name)
		}
	}
}

func TestFetchArtifactRejectsBadChecksum(t *testing.T) {
	src := writeTestArtifact(t, validMeta(), []artifactRow{
		{"artist", artA, "Artist A", "Artist A", artA, 5000},
	})

	body := compressArtifact(t, src)

	// Serve a checksum for different content.
	mux := http.NewServeMux()
	mux.HandleFunc("/"+coreArtifactChecksum,
		func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("a", 64) + "  " + coreArtifactFile))
		})
	mux.HandleFunc("/"+coreArtifactFile,
		func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(body)
		})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	withArtifactEnv(t, srv.URL+"/")

	db := database.NewTestDB(t)
	si := NewSearchIndex(db, nil, nil, testLogger())

	err := si.tryCoreArtifact(context.Background())
	if err == nil {
		t.Fatal("expected checksum rejection")
	}

	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("error = %v, want a checksum mismatch", err)
	}

	// A corrupt download must not leave the index claiming a catalog.
	if si.hasMeta(dumpImportDoneKey) || si.artifactAlreadyMerged() {
		t.Error("failed download still marked the catalog as imported")
	}
}

// A missing artifact is an ordinary outcome — the app has to keep
// working before the first one is ever published.
func TestFetchArtifactMissingIsUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	withArtifactEnv(t, srv.URL+"/")

	db := database.NewTestDB(t)
	si := NewSearchIndex(db, nil, nil, testLogger())

	err := si.tryCoreArtifact(context.Background())
	if !errors.Is(err, ErrArtifactUnavailable) {
		t.Errorf("error = %v, want ErrArtifactUnavailable", err)
	}
}

// The download resumes rather than restarting, which is what makes a
// large artifact survive a flaky connection.
func TestFetchArtifactResumesPartialDownload(t *testing.T) {
	src := writeTestArtifact(t, validMeta(), []artifactRow{
		{"artist", artA, "Artist A", "Artist A", artA, 5000},
	})

	body := compressArtifact(t, src)
	srv := artifactServer(t, body)
	withArtifactEnv(t, srv.URL+"/")

	db := database.NewTestDB(t)
	si := NewSearchIndex(db, nil, nil, testLogger())

	fetcher, err := newArtifactFetcher(si)
	if err != nil {
		t.Fatalf("newArtifactFetcher: %v", err)
	}

	// Pre-seed a truncated download.
	half := len(body) / 2
	if err := os.WriteFile(fetcher.compressedPath(), body[:half], 0o644); err != nil {
		t.Fatalf("seed partial download: %v", err)
	}

	if _, err := fetcher.fetch(context.Background()); err != nil {
		t.Fatalf("fetch after partial: %v", err)
	}
}
