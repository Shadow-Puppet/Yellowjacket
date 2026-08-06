package download

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// qBittorrent
// ---------------------------------------------------------------------------

// qbitStub is a fake qBittorrent Web API.
type qbitStub struct {
	server *httptest.Server

	mu sync.Mutex

	// loginOK controls whether auth succeeds.
	loginOK bool

	// torrentStates is what the info endpoint returns, in order; the
	// last entry repeats.
	torrentStates [][]qbitTorrent
	pollCount     int

	// addedForm records the add-torrent parameters.
	addedForm url.Values
}

func newQbitStub(t *testing.T) *qbitStub {
	t.Helper()

	s := &qbitStub{loginOK: true}
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v2/auth/login", func(w http.ResponseWriter, _ *http.Request) {
		s.mu.Lock()
		ok := s.loginOK
		s.mu.Unlock()

		if ok {
			_, _ = w.Write([]byte("Ok."))

			return
		}

		// qBittorrent answers a bad login with 200 and "Fails.".
		_, _ = w.Write([]byte("Fails."))
	})

	mux.HandleFunc("/api/v2/app/version", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("v4.6.0"))
	})

	mux.HandleFunc("/api/v2/torrents/add", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse add form: %v", err)
		}

		s.mu.Lock()
		s.addedForm = r.PostForm
		s.mu.Unlock()

		_, _ = w.Write([]byte("Ok."))
	})

	mux.HandleFunc("/api/v2/torrents/info", func(w http.ResponseWriter, _ *http.Request) {
		s.mu.Lock()

		idx := s.pollCount
		if idx >= len(s.torrentStates) {
			idx = len(s.torrentStates) - 1
		} else {
			s.pollCount++
		}

		var batch []qbitTorrent
		if idx >= 0 && len(s.torrentStates) > 0 {
			batch = s.torrentStates[idx]
		}

		s.mu.Unlock()

		writeJSON(t, w, batch)
	})

	s.server = httptest.NewServer(mux)
	t.Cleanup(s.server.Close)

	return s
}

func newStubQbit(t *testing.T, stub *qbitStub) *qbittorrent {
	t.Helper()

	p, err := newQBittorrent(
		Config{
			ID:   1,
			Kind: KindQBittorrent,
			Name: "qbittorrent",
			Settings: map[string]string{
				"url":      stub.server.URL,
				"username": "admin",
			},
		},
		func(string) (string, error) { return "secret", nil },
		slogDiscard(),
	)
	if err != nil {
		t.Fatalf("newQBittorrent: %v", err)
	}

	q, ok := p.(*qbittorrent)
	if !ok {
		t.Fatalf("provider is %T, want *qbittorrent", p)
	}

	q.pollInterval = time.Millisecond

	return q
}

func TestQbitCheck(t *testing.T) {
	t.Parallel()

	stub := newQbitStub(t)
	q := newStubQbit(t, stub)

	if err := q.Check(context.Background()); err != nil {
		t.Errorf("Check: %v", err)
	}
}

// qBittorrent returns HTTP 200 with "Fails." for a bad password, so a
// status-code-only check would report success.
func TestQbitRejectsBadPassword(t *testing.T) {
	t.Parallel()

	stub := newQbitStub(t)
	stub.loginOK = false

	q := newStubQbit(t, stub)

	if err := q.Check(context.Background()); !errors.Is(err, ErrQbitAuth) {
		t.Errorf("error = %v, want ErrQbitAuth", err)
	}
}

// The transport must be a pure transport: no search role.
func TestQbitDeclaresTransportOnly(t *testing.T) {
	t.Parallel()

	stub := newQbitStub(t)
	q := newStubQbit(t, stub)

	caps := q.Info().Caps

	if caps.CanSearch || caps.CanDelegate {
		t.Errorf("qBittorrent should transport only, got %+v", caps)
	}

	if !caps.Handles(ProtocolTorrent) {
		t.Error("qBittorrent should handle the torrent protocol")
	}

	if caps.Handles(ProtocolUsenet) {
		t.Error("qBittorrent should not claim usenet")
	}
}

func TestQbitGrabCollectsCompletedTorrent(t *testing.T) {
	t.Parallel()

	stub := newQbitStub(t)

	// The torrent's content lands in a directory qBittorrent owns.
	content := t.TempDir()

	for _, name := range []string{"01 Airbag.flac", "02 Paranoid Android.flac"} {
		if err := os.WriteFile(
			filepath.Join(content, name), []byte("audio"), 0o600,
		); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	stub.torrentStates = [][]qbitTorrent{
		{{
			Hash: "abc123", State: "downloading",
			Progress: 0.4, Size: 1000, Completed: 400,
		}},
		{{
			Hash: "abc123", State: "uploading",
			Progress: 1.0, Size: 1000, Completed: 1000,
			ContentPath: content,
		}},
	}

	q := newStubQbit(t, stub)
	dst := t.TempDir()

	c := Candidate{
		ID:       "prowlarr:x",
		Protocol: ProtocolTorrent,
		Title:    "Radiohead - OK Computer",
		Payload: map[string]string{
			"link":     "magnet:?xt=urn:btih:abc123",
			"infoHash": "abc123",
		},
	}

	got, err := q.Grab(context.Background(), c, dst, nil)
	if err != nil {
		t.Fatalf("Grab: %v", err)
	}

	if len(got.Files) != 2 {
		t.Fatalf("collected %d files, want 2", len(got.Files))
	}

	for _, f := range got.Files {
		if !strings.HasPrefix(f, dst) {
			t.Errorf("file %s is outside the staging dir", f)
		}
	}

	// The torrent was saved into our staging directory and kept out of
	// qBittorrent's own move-on-completion rules.
	stub.mu.Lock()
	form := stub.addedForm
	stub.mu.Unlock()

	if form.Get("savepath") != dst {
		t.Errorf("savepath = %q, want the staging dir %q", form.Get("savepath"), dst)
	}

	if form.Get("autoTMM") != "false" {
		t.Errorf("autoTMM = %q, want false", form.Get("autoTMM"))
	}
}

func TestQbitGrabFailsOnErrorState(t *testing.T) {
	t.Parallel()

	stub := newQbitStub(t)
	stub.torrentStates = [][]qbitTorrent{
		{{Hash: "abc123", State: "error"}},
	}

	q := newStubQbit(t, stub)

	c := Candidate{
		Protocol: ProtocolTorrent,
		Payload: map[string]string{
			"link": "magnet:?xt=urn:btih:abc123", "infoHash": "abc123",
		},
	}

	_, err := q.Grab(context.Background(), c, t.TempDir(), nil)
	if !errors.Is(err, ErrQbitTransferFailed) {
		t.Errorf("error = %v, want ErrQbitTransferFailed", err)
	}
}

func TestInfoHashFromMagnet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{"magnet:?xt=urn:btih:ABC123&dn=x", "abc123"},
		{"magnet:?dn=x&xt=urn:btih:def456", "def456"},
		{"magnet:?dn=no-hash", ""},
		{"https://example.com/x.torrent", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()

			if got := infoHashFromMagnet(tt.in); got != tt.want {
				t.Errorf("infoHashFromMagnet(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// SABnzbd
// ---------------------------------------------------------------------------

// sabStub is a fake SABnzbd API.
type sabStub struct {
	server *httptest.Server

	mu sync.Mutex

	// queueSlots and historySlots are returned in order per mode; the
	// last entry repeats.
	queueSlots   [][]sabQueueSlot
	queuePolls   int
	historySlots []sabHistorySlot

	addStatus bool
	addError  string

	badKey bool
}

func newSabStub(t *testing.T) *sabStub {
	t.Helper()

	s := &sabStub{addStatus: true}

	s.server = httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			s.mu.Lock()
			badKey := s.badKey
			s.mu.Unlock()

			if badKey {
				// SABnzbd reports a bad key with HTTP 200 and a body.
				writeJSON(t, w, map[string]any{
					"status": false, "error": "API Key Incorrect",
				})

				return
			}

			switch r.URL.Query().Get("mode") {
			case "version":
				writeJSON(t, w, map[string]any{"version": "4.1.0"})
			case "addurl":
				s.mu.Lock()
				status, errMsg := s.addStatus, s.addError
				s.mu.Unlock()

				writeJSON(t, w, map[string]any{
					"status": status, "error": errMsg,
					"nzo_ids": []string{"SABnzbd_nzo_1"},
				})
			case "queue":
				s.mu.Lock()

				idx := s.queuePolls
				if idx >= len(s.queueSlots) {
					idx = len(s.queueSlots) - 1
				} else {
					s.queuePolls++
				}

				var slots []sabQueueSlot
				if idx >= 0 && len(s.queueSlots) > 0 {
					slots = s.queueSlots[idx]
				}

				s.mu.Unlock()

				writeJSON(t, w, map[string]any{
					"queue": map[string]any{"slots": slots},
				})
			case "history":
				s.mu.Lock()
				slots := s.historySlots
				s.mu.Unlock()

				writeJSON(t, w, map[string]any{
					"history": map[string]any{"slots": slots},
				})
			default:
				w.WriteHeader(http.StatusBadRequest)
			}
		},
	))

	t.Cleanup(s.server.Close)

	return s
}

func newStubSab(t *testing.T, stub *sabStub) *sabnzbd {
	t.Helper()

	p, err := newSABnzbd(
		Config{
			ID:       1,
			Kind:     KindSABnzbd,
			Name:     "sabnzbd",
			Settings: map[string]string{"url": stub.server.URL},
		},
		func(string) (string, error) { return "test-key", nil },
		slogDiscard(),
	)
	if err != nil {
		t.Fatalf("newSABnzbd: %v", err)
	}

	s, ok := p.(*sabnzbd)
	if !ok {
		t.Fatalf("provider is %T, want *sabnzbd", p)
	}

	s.pollInterval = time.Millisecond

	return s
}

func TestSabCheck(t *testing.T) {
	t.Parallel()

	stub := newSabStub(t)
	s := newStubSab(t, stub)

	if err := s.Check(context.Background()); err != nil {
		t.Errorf("Check: %v", err)
	}
}

func TestSabDeclaresUsenetOnly(t *testing.T) {
	t.Parallel()

	stub := newSabStub(t)
	s := newStubSab(t, stub)

	caps := s.Info().Caps

	if caps.CanSearch || caps.CanDelegate {
		t.Errorf("SABnzbd should transport only, got %+v", caps)
	}

	if !caps.Handles(ProtocolUsenet) {
		t.Error("SABnzbd should handle the usenet protocol")
	}

	if caps.Handles(ProtocolTorrent) {
		t.Error("SABnzbd should not claim torrents")
	}
}

// Completion is read from history, not from the queue emptying: a job
// leaves the queue before post-processing finishes.
func TestSabGrabWaitsForHistory(t *testing.T) {
	t.Parallel()

	stub := newSabStub(t)

	storage := t.TempDir()

	for _, name := range []string{"01 Airbag.flac", "02 Paranoid Android.flac"} {
		if err := os.WriteFile(
			filepath.Join(storage, name), []byte("audio"), 0o600,
		); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	stub.queueSlots = [][]sabQueueSlot{
		{{
			NzoID: "SABnzbd_nzo_1", Status: "Downloading",
			MB: "100.0", MBLeft: "60.0",
		}},
		// Second poll: gone from the queue.
		{},
	}
	stub.historySlots = []sabHistorySlot{{
		NzoID: "SABnzbd_nzo_1", Status: "Completed", Storage: storage,
	}}

	s := newStubSab(t, stub)
	dst := t.TempDir()

	c := Candidate{
		Protocol: ProtocolUsenet,
		Title:    "Radiohead - OK Computer",
		Payload:  map[string]string{"link": "https://example.com/x.nzb"},
	}

	got, err := s.Grab(context.Background(), c, dst, nil)
	if err != nil {
		t.Fatalf("Grab: %v", err)
	}

	if len(got.Files) != 2 {
		t.Fatalf("collected %d files, want 2", len(got.Files))
	}

	for _, f := range got.Files {
		if !strings.HasPrefix(f, dst) {
			t.Errorf("file %s is outside the staging dir", f)
		}
	}
}

func TestSabGrabFailsOnFailedJob(t *testing.T) {
	t.Parallel()

	stub := newSabStub(t)
	stub.queueSlots = [][]sabQueueSlot{{}}
	stub.historySlots = []sabHistorySlot{{
		NzoID:   "SABnzbd_nzo_1",
		Status:  "Failed",
		FailMsg: "Unpacking failed",
	}}

	s := newStubSab(t, stub)

	c := Candidate{
		Payload: map[string]string{"link": "https://example.com/x.nzb"},
	}

	_, err := s.Grab(context.Background(), c, t.TempDir(), nil)
	if !errors.Is(err, ErrSabTransferFailed) {
		t.Errorf("error = %v, want ErrSabTransferFailed", err)
	}
}

// A job that vanishes from both queue and history was removed out from
// under us, which must not look like success.
func TestSabGrabDetectsVanishedJob(t *testing.T) {
	t.Parallel()

	stub := newSabStub(t)
	stub.queueSlots = [][]sabQueueSlot{{}}
	stub.historySlots = nil

	s := newStubSab(t, stub)

	c := Candidate{
		Payload: map[string]string{"link": "https://example.com/x.nzb"},
	}

	_, err := s.Grab(context.Background(), c, t.TempDir(), nil)
	if !errors.Is(err, ErrSabNoJob) {
		t.Errorf("error = %v, want ErrSabNoJob", err)
	}
}

// An NZB URL is untrusted input from an indexer.
func TestSabGrabRejectsNonHTTPURL(t *testing.T) {
	t.Parallel()

	stub := newSabStub(t)
	s := newStubSab(t, stub)

	c := Candidate{Payload: map[string]string{"link": "file:///etc/passwd"}}

	_, err := s.Grab(context.Background(), c, t.TempDir(), nil)
	if !errors.Is(err, ErrUnsafeURL) {
		t.Errorf("error = %v, want ErrUnsafeURL", err)
	}
}

func TestParseMB(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want int64
	}{
		{"1.0", 1024 * 1024},
		{"0", 0},
		{"  2.5  ", int64(2.5 * 1024 * 1024)},
		{"garbage", 0},
		{"", 0},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()

			if got := parseMB(tt.in); got != tt.want {
				t.Errorf("parseMB(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Shared
// ---------------------------------------------------------------------------

// collectTree flattens whatever shape the transport produced, because
// the importer wants a flat set of paths inside staging.
func TestCollectTree(t *testing.T) {
	t.Parallel()

	t.Run("directory tree is flattened", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		nested := filepath.Join(root, "CD1")

		if err := os.MkdirAll(nested, 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		if err := os.WriteFile(
			filepath.Join(root, "a.flac"), []byte("x"), 0o600,
		); err != nil {
			t.Fatalf("write: %v", err)
		}

		if err := os.WriteFile(
			filepath.Join(nested, "b.flac"), []byte("y"), 0o600,
		); err != nil {
			t.Fatalf("write: %v", err)
		}

		dst := t.TempDir()

		got, err := collectTree(root, dst)
		if err != nil {
			t.Fatalf("collectTree: %v", err)
		}

		if len(got.Files) != 2 {
			t.Fatalf("collected %d files, want 2", len(got.Files))
		}
	})

	t.Run("single file", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		file := filepath.Join(root, "single.flac")

		if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}

		dst := t.TempDir()

		got, err := collectTree(file, dst)
		if err != nil {
			t.Fatalf("collectTree: %v", err)
		}

		if len(got.Files) != 1 {
			t.Fatalf("collected %d files, want 1", len(got.Files))
		}

		if filepath.Dir(got.Files[0]) != dst {
			t.Errorf("file landed at %s, want inside %s", got.Files[0], dst)
		}
	})

	t.Run("missing path errors", func(t *testing.T) {
		t.Parallel()

		if _, err := collectTree(
			filepath.Join(t.TempDir(), "nope"), t.TempDir(),
		); err == nil {
			t.Error("want an error for a missing content path")
		}
	})
}
