package download

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// slskd tests run against an httptest server shaped like the real API.
// No daemon, no Soulseek account, no network.

// slskdStub is a fake slskd daemon.
type slskdStub struct {
	server *httptest.Server

	mu sync.Mutex

	// responses is what a search returns.
	responses []slskdResponse

	// transfers is what the downloads endpoint reports, in order; the
	// last entry repeats.
	transfers [][]slskdTransfer
	pollCount int

	// enqueued records what was requested for download.
	enqueued []map[string]any

	// unauthorized makes every call return 401.
	unauthorized bool
}

func newSlskdStub(t *testing.T) *slskdStub {
	t.Helper()

	s := &slskdStub{}
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v0/application", func(w http.ResponseWriter, r *http.Request) {
		if s.reject(w, r) {
			return
		}

		writeJSON(t, w, map[string]any{"version": "0.21.0"})
	})

	mux.HandleFunc("/api/v0/searches", func(w http.ResponseWriter, r *http.Request) {
		if s.reject(w, r) {
			return
		}

		w.WriteHeader(http.StatusCreated)
	})

	mux.HandleFunc("/api/v0/searches/", func(w http.ResponseWriter, r *http.Request) {
		if s.reject(w, r) {
			return
		}

		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)

			return
		}

		s.mu.Lock()
		responses := s.responses
		s.mu.Unlock()

		writeJSON(t, w, slskdSearch{
			ID:         "search-1",
			IsComplete: true,
			Responses:  responses,
		})
	})

	mux.HandleFunc("/api/v0/transfers/downloads/", func(w http.ResponseWriter, r *http.Request) {
		if s.reject(w, r) {
			return
		}

		if r.Method == http.MethodPost {
			var body []map[string]any

			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode enqueue body: %v", err)
			}

			s.mu.Lock()
			s.enqueued = body
			s.mu.Unlock()

			w.WriteHeader(http.StatusCreated)

			return
		}

		s.mu.Lock()

		idx := s.pollCount
		if idx >= len(s.transfers) {
			idx = len(s.transfers) - 1
		} else {
			s.pollCount++
		}

		var batch []slskdTransfer
		if idx >= 0 && len(s.transfers) > 0 {
			batch = s.transfers[idx]
		}

		s.mu.Unlock()

		writeJSON(t, w, map[string]any{
			"directories": []map[string]any{{"files": batch}},
		})
	})

	s.server = httptest.NewServer(mux)
	t.Cleanup(s.server.Close)

	return s
}

// reject enforces API-key auth like the real daemon.
func (s *slskdStub) reject(w http.ResponseWriter, r *http.Request) bool {
	s.mu.Lock()
	unauthorized := s.unauthorized
	s.mu.Unlock()

	if unauthorized || r.Header.Get("X-Api-Key") != "test-key" {
		w.WriteHeader(http.StatusUnauthorized)

		return true
	}

	return false
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Errorf("encode response: %v", err)
	}
}

// newStubSlskd builds the provider pointed at the stub, with a real
// temp directory standing in for slskd's downloads folder.
func newStubSlskd(t *testing.T, stub *slskdStub) (*slskd, string) {
	t.Helper()

	downloads := t.TempDir()

	p, err := newSlskd(
		Config{
			ID:      1,
			Kind:    KindSlskd,
			Name:    "slskd",
			Enabled: true,
			Settings: map[string]string{
				"url":           stub.server.URL,
				"downloadsPath": downloads,
			},
		},
		func(string) (string, error) { return "test-key", nil },
		slogDiscard(),
	)
	if err != nil {
		t.Fatalf("newSlskd: %v", err)
	}

	s, ok := p.(*slskd)
	if !ok {
		t.Fatalf("provider is %T, want *slskd", p)
	}

	// Real intervals are tuned for Soulseek's pace; tests only care
	// about the state machine, so run it at full speed.
	s.searchPoll = time.Millisecond
	s.searchWait = 200 * time.Millisecond
	s.transferPoll = time.Millisecond

	return s, downloads
}

func TestSlskdCheck(t *testing.T) {
	t.Parallel()

	stub := newSlskdStub(t)
	s, _ := newStubSlskd(t, stub)

	if err := s.Check(context.Background()); err != nil {
		t.Errorf("Check: %v", err)
	}
}

func TestSlskdCheckRejectsBadKey(t *testing.T) {
	t.Parallel()

	stub := newSlskdStub(t)
	stub.unauthorized = true

	s, _ := newStubSlskd(t, stub)

	if err := s.Check(context.Background()); !errors.Is(err, ErrSlskdAuth) {
		t.Errorf("error = %v, want ErrSlskdAuth", err)
	}
}

// A downloads folder that is not readable from this machine is the
// classic slskd-on-a-NAS misconfiguration, and must surface at
// configuration time rather than after a long transfer.
func TestSlskdCheckRejectsUnreadableDownloadsPath(t *testing.T) {
	t.Parallel()

	stub := newSlskdStub(t)

	p, err := newSlskd(
		Config{
			ID: 1,
			Settings: map[string]string{
				"url":           stub.server.URL,
				"downloadsPath": "/definitely/not/a/real/path",
			},
		},
		func(string) (string, error) { return "test-key", nil },
		slogDiscard(),
	)
	if err != nil {
		t.Fatalf("newSlskd: %v", err)
	}

	if err := p.Check(context.Background()); !errors.Is(
		err, ErrSlskdDownloadsPath,
	) {
		t.Errorf("error = %v, want ErrSlskdDownloadsPath", err)
	}
}

// Soulseek has no album concept, so candidates are built by grouping a
// peer's files into the folders they live in.
func TestSlskdGroupsResultsByPeerAndFolder(t *testing.T) {
	t.Parallel()

	stub := newSlskdStub(t)
	stub.responses = []slskdResponse{
		{
			Username:          "peer-one",
			HasFreeUploadSlot: true,
			QueueLength:       0,
			UploadSpeed:       2_000_000,
			Files: []slskdFile{
				{Filename: `@@x\Music\OK Computer\01 Airbag.flac`, Size: 30_000_000},
				{Filename: `@@x\Music\OK Computer\02 Paranoid Android.flac`, Size: 40_000_000},
				{Filename: `@@x\Music\Kid A\01 Everything.flac`, Size: 30_000_000},
				{Filename: `@@x\Music\Kid A\02 Kid A.flac`, Size: 30_000_000},
			},
		},
		{
			Username:          "peer-two",
			HasFreeUploadSlot: false,
			QueueLength:       40,
			Files: []slskdFile{
				{
					Filename: `\share\OK Computer [320]\01 - Airbag.mp3`,
					Size:     8_000_000,
					BitRate:  320,
				},
				{
					Filename: `\share\OK Computer [320]\02 - Paranoid Android.mp3`,
					Size:     9_000_000,
					BitRate:  320,
				},
			},
		},
	}

	s, _ := newStubSlskd(t, stub)

	got, err := s.Search(context.Background(), Download{
		Artist: "Radiohead",
		Album:  "OK Computer",
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	// Two folders from peer-one, one from peer-two.
	if len(got) != 3 {
		t.Fatalf("got %d candidates, want 3", len(got))
	}

	byOrigin := map[string]int{}
	for _, c := range got {
		byOrigin[c.Origin]++
	}

	if byOrigin["peer-one"] != 2 {
		t.Errorf("peer-one folders = %d, want 2", byOrigin["peer-one"])
	}

	if byOrigin["peer-two"] != 1 {
		t.Errorf("peer-two folders = %d, want 1", byOrigin["peer-two"])
	}
}

// A busy peer behind a long queue is a worse bet than a free one, no
// matter how good the files look.
func TestSlskdPeerHealthReflectsAvailability(t *testing.T) {
	t.Parallel()

	free := peerHealth(slskdResponse{
		HasFreeUploadSlot: true,
		QueueLength:       0,
		UploadSpeed:       2_000_000,
	})

	busy := peerHealth(slskdResponse{
		HasFreeUploadSlot: false,
		QueueLength:       40,
	})

	if free <= busy {
		t.Errorf("free peer health %f should exceed busy peer %f", free, busy)
	}

	if free > 1 || busy < 0 {
		t.Errorf("health out of range: free=%f busy=%f", free, busy)
	}
}

// Folders with almost nothing in them are Soulseek noise, not albums.
func TestSlskdSkipsTinyFolders(t *testing.T) {
	t.Parallel()

	stub := newSlskdStub(t)
	stub.responses = []slskdResponse{{
		Username: "peer",
		Files: []slskdFile{
			{Filename: `\share\Random\one.mp3`, Size: 5_000_000},
		},
	}}

	s, _ := newStubSlskd(t, stub)

	got, err := s.Search(context.Background(), Download{Query: "x"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(got) != 0 {
		t.Errorf("got %d candidates, want 0 for a single-file folder", len(got))
	}
}

func TestSlskdGrabCollectsFromDownloadsFolder(t *testing.T) {
	t.Parallel()

	stub := newSlskdStub(t)
	stub.transfers = [][]slskdTransfer{
		{
			{
				Filename:         `\share\OK Computer\01 Airbag.flac`,
				State:            "InProgress",
				BytesTransferred: 100,
			},
			{
				Filename: `\share\OK Computer\02 Paranoid Android.flac`,
				State:    "InProgress",
			},
		},
		{
			{
				Filename:         `\share\OK Computer\01 Airbag.flac`,
				State:            "Completed, Succeeded",
				BytesTransferred: 500,
			},
			{
				Filename:         `\share\OK Computer\02 Paranoid Android.flac`,
				State:            "Completed, Succeeded",
				BytesTransferred: 500,
			},
		},
	}

	s, downloads := newStubSlskd(t, stub)

	// slskd writes into <downloads>/<folder>/<file>.
	folder := filepath.Join(downloads, "OK Computer")
	if err := os.MkdirAll(folder, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	for _, name := range []string{
		"01 Airbag.flac",
		"02 Paranoid Android.flac",
	} {
		if err := os.WriteFile(
			filepath.Join(folder, name), []byte("audio"), 0o600,
		); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	c := Candidate{
		ID:       "slskd:peer:OK Computer",
		Protocol: ProtocolDirect,
		Files: []CandidateFile{
			{Path: `\share\OK Computer\01 Airbag.flac`, Size: 500, IsAudio: true},
			{Path: `\share\OK Computer\02 Paranoid Android.flac`, Size: 500, IsAudio: true},
		},
		TotalSize: 1000,
		Payload:   map[string]string{"username": "peer"},
	}

	dst := t.TempDir()

	got, err := s.Grab(context.Background(), c, dst, nil)
	if err != nil {
		t.Fatalf("Grab: %v", err)
	}

	if len(got.Files) != 2 {
		t.Fatalf("collected %d files, want 2", len(got.Files))
	}

	for _, f := range got.Files {
		if !strings.HasPrefix(f, dst) {
			t.Errorf("file %s is not inside the staging dir %s", f, dst)
		}

		if _, err := os.Stat(f); err != nil {
			t.Errorf("collected file missing: %v", err)
		}
	}

	// The enqueue request named the files the candidate listed.
	stub.mu.Lock()
	enqueued := len(stub.enqueued)
	stub.mu.Unlock()

	if enqueued != 2 {
		t.Errorf("enqueued %d files, want 2", enqueued)
	}
}

// A peer that drops mid-folder is normal; partial results go forward
// and the importer's completeness check decides.
func TestSlskdGrabToleratesPartialFailure(t *testing.T) {
	t.Parallel()

	stub := newSlskdStub(t)
	stub.transfers = [][]slskdTransfer{{
		{
			Filename:         `\s\Album\01 A.flac`,
			State:            "Completed, Succeeded",
			BytesTransferred: 500,
		},
		{Filename: `\s\Album\02 B.flac`, State: "Completed, Errored"},
	}}

	s, downloads := newStubSlskd(t, stub)

	folder := filepath.Join(downloads, "Album")
	if err := os.MkdirAll(folder, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := os.WriteFile(
		filepath.Join(folder, "01 A.flac"), []byte("audio"), 0o600,
	); err != nil {
		t.Fatalf("write: %v", err)
	}

	c := Candidate{
		Files: []CandidateFile{
			{Path: `\s\Album\01 A.flac`, Size: 500, IsAudio: true},
			{Path: `\s\Album\02 B.flac`, Size: 500, IsAudio: true},
		},
		Payload: map[string]string{"username": "peer"},
	}

	got, err := s.Grab(context.Background(), c, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Grab: %v", err)
	}

	if len(got.Files) != 1 {
		t.Errorf("collected %d files, want the 1 that succeeded", len(got.Files))
	}
}

func TestSlskdGrabFailsWhenEverythingFails(t *testing.T) {
	t.Parallel()

	stub := newSlskdStub(t)
	stub.transfers = [][]slskdTransfer{{
		{Filename: `\s\Album\01 A.flac`, State: "Completed, Errored"},
	}}

	s, _ := newStubSlskd(t, stub)

	c := Candidate{
		Files:   []CandidateFile{{Path: `\s\Album\01 A.flac`, IsAudio: true}},
		Payload: map[string]string{"username": "peer"},
	}

	_, err := s.Grab(context.Background(), c, t.TempDir(), nil)
	if !errors.Is(err, ErrSlskdTransferFailed) {
		t.Errorf("error = %v, want ErrSlskdTransferFailed", err)
	}
}

func TestSlskdRequiresConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		settings map[string]string
		secrets  SecretLookup
	}{
		{
			name:     "no url",
			settings: map[string]string{"downloadsPath": "/tmp"},
			secrets:  func(string) (string, error) { return "k", nil },
		},
		{
			name:     "no downloads path",
			settings: map[string]string{"url": "http://localhost:5030"},
			secrets:  func(string) (string, error) { return "k", nil },
		},
		{
			name: "no api key",
			settings: map[string]string{
				"url": "http://localhost:5030", "downloadsPath": "/tmp",
			},
			secrets: func(string) (string, error) {
				return "", ErrSecretNotFound
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := newSlskd(
				Config{Settings: tt.settings}, tt.secrets, slogDiscard(),
			)

			if !errors.Is(err, ErrNotConfigured) {
				t.Errorf("error = %v, want ErrNotConfigured", err)
			}
		})
	}
}
