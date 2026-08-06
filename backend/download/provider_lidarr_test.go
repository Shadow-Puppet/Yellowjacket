package download

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// lidarrStub is a fake Lidarr instance.
type lidarrStub struct {
	server *httptest.Server

	mu sync.Mutex

	// searchAlbum is what /api/v1/search returns.
	searchAlbum lidarrAlbum

	// album is what /api/v1/album/{id} returns, in order; the last
	// entry repeats.
	albumStates []lidarrAlbum
	pollCount   int

	// trackFiles is what /api/v1/trackfile returns.
	trackFiles []lidarrTrackFile

	// rootFolders is what /api/v1/rootfolder returns.
	rootFolders []lidarrRootFolder

	// commands records the commands that were issued.
	commands []string

	// monitorCalls records album-monitor toggles.
	monitorCalls []map[string]any

	// addedArtists records artist additions.
	addedArtists []map[string]any

	// artists is what a GET of /api/v1/artist returns, which is how the
	// Lister role looks up and enumerates monitored artists.
	artists []map[string]any

	unauthorized bool
}

func newLidarrStub(t *testing.T) *lidarrStub {
	t.Helper()

	s := &lidarrStub{
		rootFolders: []lidarrRootFolder{{ID: 1, Path: "/music"}},
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/system/status", func(w http.ResponseWriter, r *http.Request) {
		if s.reject(w, r) {
			return
		}

		writeJSON(t, w, map[string]any{"version": "2.0.0"})
	})

	mux.HandleFunc("/api/v1/rootfolder", func(w http.ResponseWriter, r *http.Request) {
		if s.reject(w, r) {
			return
		}

		s.mu.Lock()
		folders := s.rootFolders
		s.mu.Unlock()

		writeJSON(t, w, folders)
	})

	mux.HandleFunc("/api/v1/qualityprofile", func(w http.ResponseWriter, r *http.Request) {
		if s.reject(w, r) {
			return
		}

		writeJSON(t, w, []map[string]any{{"id": 7}})
	})

	mux.HandleFunc("/api/v1/metadataprofile", func(w http.ResponseWriter, r *http.Request) {
		if s.reject(w, r) {
			return
		}

		writeJSON(t, w, []map[string]any{{"id": 3}})
	})

	mux.HandleFunc("/api/v1/search", func(w http.ResponseWriter, r *http.Request) {
		if s.reject(w, r) {
			return
		}

		s.mu.Lock()
		album := s.searchAlbum
		s.mu.Unlock()

		writeJSON(t, w, []map[string]any{{"album": album}})
	})

	mux.HandleFunc("/api/v1/artist", func(w http.ResponseWriter, r *http.Request) {
		if s.reject(w, r) {
			return
		}

		if r.Method == http.MethodGet {
			s.mu.Lock()
			artists := s.artists
			s.mu.Unlock()

			if artists == nil {
				artists = []map[string]any{}
			}

			writeJSON(t, w, artists)

			return
		}

		var body map[string]any

		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode artist body: %v", err)
		}

		s.mu.Lock()
		s.addedArtists = append(s.addedArtists, body)
		s.mu.Unlock()

		writeJSON(t, w, map[string]any{"id": 42})
	})

	mux.HandleFunc("/api/v1/album/monitor", func(w http.ResponseWriter, r *http.Request) {
		if s.reject(w, r) {
			return
		}

		var body map[string]any

		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode monitor body: %v", err)
		}

		s.mu.Lock()
		s.monitorCalls = append(s.monitorCalls, body)
		s.mu.Unlock()

		w.WriteHeader(http.StatusAccepted)
	})

	mux.HandleFunc("/api/v1/album", func(w http.ResponseWriter, r *http.Request) {
		if s.reject(w, r) {
			return
		}

		s.mu.Lock()
		album := s.searchAlbum
		s.mu.Unlock()

		album.ID = 99

		writeJSON(t, w, []lidarrAlbum{album})
	})

	mux.HandleFunc("/api/v1/album/", func(w http.ResponseWriter, r *http.Request) {
		if s.reject(w, r) {
			return
		}

		s.mu.Lock()

		idx := s.pollCount
		if idx >= len(s.albumStates) {
			idx = len(s.albumStates) - 1
		} else {
			s.pollCount++
		}

		var album lidarrAlbum
		if idx >= 0 && len(s.albumStates) > 0 {
			album = s.albumStates[idx]
		}

		s.mu.Unlock()

		writeJSON(t, w, album)
	})

	mux.HandleFunc("/api/v1/trackfile", func(w http.ResponseWriter, r *http.Request) {
		if s.reject(w, r) {
			return
		}

		s.mu.Lock()
		files := s.trackFiles
		s.mu.Unlock()

		writeJSON(t, w, files)
	})

	mux.HandleFunc("/api/v1/command", func(w http.ResponseWriter, r *http.Request) {
		if s.reject(w, r) {
			return
		}

		var body map[string]any

		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode command body: %v", err)
		}

		name, _ := body["name"].(string)

		s.mu.Lock()
		s.commands = append(s.commands, name)
		s.mu.Unlock()

		writeJSON(t, w, map[string]any{"id": 1})
	})

	s.server = httptest.NewServer(mux)
	t.Cleanup(s.server.Close)

	return s
}

func (s *lidarrStub) reject(w http.ResponseWriter, r *http.Request) bool {
	s.mu.Lock()
	unauthorized := s.unauthorized
	s.mu.Unlock()

	if unauthorized || r.Header.Get("X-Api-Key") != "test-key" {
		w.WriteHeader(http.StatusUnauthorized)

		return true
	}

	return false
}

func newStubLidarr(t *testing.T, stub *lidarrStub) *lidarr {
	t.Helper()

	p, err := newLidarr(
		Config{
			ID:      1,
			Kind:    KindLidarr,
			Name:    "lidarr",
			Enabled: true,
			Settings: map[string]string{
				"url": stub.server.URL,
			},
		},
		func(string) (string, error) { return "test-key", nil },
		slogDiscard(),
	)
	if err != nil {
		t.Fatalf("newLidarr: %v", err)
	}

	l, ok := p.(*lidarr)
	if !ok {
		t.Fatalf("provider is %T, want *lidarr", p)
	}

	return l
}

func TestLidarrCheck(t *testing.T) {
	t.Parallel()

	stub := newLidarrStub(t)
	l := newStubLidarr(t, stub)

	if err := l.Check(context.Background()); err != nil {
		t.Errorf("Check: %v", err)
	}
}

func TestLidarrCheckRejectsBadKey(t *testing.T) {
	t.Parallel()

	stub := newLidarrStub(t)
	stub.unauthorized = true

	l := newStubLidarr(t, stub)

	if err := l.Check(context.Background()); !errors.Is(err, ErrLidarrAuth) {
		t.Errorf("error = %v, want ErrLidarrAuth", err)
	}
}

// Lidarr with nowhere to put music cannot fulfil anything, and that
// should be visible at configuration time.
func TestLidarrCheckRequiresRootFolder(t *testing.T) {
	t.Parallel()

	stub := newLidarrStub(t)
	stub.rootFolders = nil

	l := newStubLidarr(t, stub)

	if err := l.Check(context.Background()); !errors.Is(
		err, ErrLidarrNoRootFolder,
	) {
		t.Errorf("error = %v, want ErrLidarrNoRootFolder", err)
	}
}

// An album Lidarr already tracks only needs monitoring and a search.
func TestLidarrDelegateExistingAlbum(t *testing.T) {
	t.Parallel()

	stub := newLidarrStub(t)
	stub.searchAlbum = lidarrAlbum{
		ID:           55,
		Title:        "OK Computer",
		ForeignAlbum: "rg-mbid",
		Monitored:    false,
	}

	l := newStubLidarr(t, stub)

	externalID, err := l.Delegate(context.Background(), Request{
		ReleaseGroupMBID: "rg-mbid",
		Artist:           "Radiohead",
		Album:            "OK Computer",
	})
	if err != nil {
		t.Fatalf("Delegate: %v", err)
	}

	if externalID != "55" {
		t.Errorf("external id = %q, want 55", externalID)
	}

	stub.mu.Lock()
	commands := append([]string(nil), stub.commands...)
	monitors := len(stub.monitorCalls)
	added := len(stub.addedArtists)
	stub.mu.Unlock()

	if added != 0 {
		t.Errorf("added %d artists, want 0 for an album Lidarr already has", added)
	}

	if monitors != 1 {
		t.Errorf("monitor calls = %d, want 1", monitors)
	}

	if len(commands) != 1 || commands[0] != "AlbumSearch" {
		t.Errorf("commands = %v, want [AlbumSearch]", commands)
	}
}

// Adding an artist must not kick off their entire discography.
func TestLidarrDelegateNewArtistMonitorsNothingByDefault(t *testing.T) {
	t.Parallel()

	stub := newLidarrStub(t)
	stub.searchAlbum = lidarrAlbum{
		Title:        "OK Computer",
		ForeignAlbum: "rg-mbid",
	}
	stub.searchAlbum.Artist.ForeignArtistID = "artist-mbid"
	stub.searchAlbum.Artist.ArtistName = "Radiohead"

	l := newStubLidarr(t, stub)

	if _, err := l.Delegate(context.Background(), Request{
		ReleaseGroupMBID: "rg-mbid",
		Artist:           "Radiohead",
		Album:            "OK Computer",
	}); err != nil {
		t.Fatalf("Delegate: %v", err)
	}

	stub.mu.Lock()
	added := append([]map[string]any(nil), stub.addedArtists...)
	stub.mu.Unlock()

	if len(added) != 1 {
		t.Fatalf("added %d artists, want 1", len(added))
	}

	opts, ok := added[0]["addOptions"].(map[string]any)
	if !ok {
		t.Fatalf("addOptions missing from %v", added[0])
	}

	if opts["monitor"] != "none" {
		t.Errorf("monitor = %v, want none", opts["monitor"])
	}

	if opts["searchForMissingAlbums"] != false {
		t.Errorf(
			"searchForMissingAlbums = %v, want false",
			opts["searchForMissingAlbums"],
		)
	}
}

func TestLidarrDelegateNoMatch(t *testing.T) {
	t.Parallel()

	stub := newLidarrStub(t)
	// searchAlbum stays zero-valued: no title means no match.

	l := newStubLidarr(t, stub)

	_, err := l.Delegate(context.Background(), Request{
		Artist: "Nobody",
		Album:  "Nothing",
	})

	if !errors.Is(err, ErrLidarrNoMatch) {
		t.Errorf("error = %v, want ErrLidarrNoMatch", err)
	}
}

// Completion is judged by imported files, not by an empty queue: the
// queue drains when the download finishes, which is before the import.
func TestLidarrPollWaitsForImportedFiles(t *testing.T) {
	t.Parallel()

	stub := newLidarrStub(t)
	stub.albumStates = []lidarrAlbum{
		{ID: 55, Statistics: lidarrAlbumStats{TrackCount: 12}},
		{ID: 55, Statistics: lidarrAlbumStats{TrackFileCount: 6, TrackCount: 12}},
		{ID: 55, Statistics: lidarrAlbumStats{TrackFileCount: 12, TrackCount: 12}},
	}
	stub.trackFiles = []lidarrTrackFile{
		{ID: 1, Path: "/music/Radiohead/OK Computer/01 Airbag.flac"},
		{ID: 2, Path: "/music/Radiohead/OK Computer/02 Paranoid Android.flac"},
	}

	l := newStubLidarr(t, stub)
	ctx := context.Background()

	// Nothing imported yet.
	first, err := l.Poll(ctx, "55")
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}

	if first.State != StateGrabbing {
		t.Errorf("state = %q, want grabbing", first.State)
	}

	// Half done.
	second, err := l.Poll(ctx, "55")
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}

	if second.State != StateGrabbing {
		t.Errorf("state = %q, want grabbing", second.State)
	}

	if second.Progress <= first.Progress {
		t.Errorf(
			"progress did not advance: %f then %f",
			first.Progress, second.Progress,
		)
	}

	// Complete, with the paths Lidarr imported to.
	third, err := l.Poll(ctx, "55")
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}

	if third.State != StateComplete {
		t.Fatalf("state = %q, want complete", third.State)
	}

	if len(third.ImportedPaths) != 2 {
		t.Errorf("imported paths = %v, want 2", third.ImportedPaths)
	}

	for _, p := range third.ImportedPaths {
		if !strings.HasPrefix(p, "/music/") {
			t.Errorf("path %q is not in Lidarr's library", p)
		}
	}
}

func TestLidarrPollRejectsBadExternalID(t *testing.T) {
	t.Parallel()

	stub := newLidarrStub(t)
	l := newStubLidarr(t, stub)

	if _, err := l.Poll(context.Background(), "not-a-number"); !errors.Is(
		err, ErrLidarrNoMatch,
	) {
		t.Errorf("error = %v, want ErrLidarrNoMatch", err)
	}
}

// Withdrawing stops monitoring; it must not delete the album, which may
// predate this request.
func TestLidarrWithdrawUnmonitorsOnly(t *testing.T) {
	t.Parallel()

	stub := newLidarrStub(t)
	l := newStubLidarr(t, stub)

	if err := l.Withdraw(context.Background(), "55"); err != nil {
		t.Fatalf("Withdraw: %v", err)
	}

	stub.mu.Lock()
	calls := append([]map[string]any(nil), stub.monitorCalls...)
	stub.mu.Unlock()

	if len(calls) != 1 {
		t.Fatalf("monitor calls = %d, want 1", len(calls))
	}

	if calls[0]["monitored"] != false {
		t.Errorf("monitored = %v, want false", calls[0]["monitored"])
	}
}

func TestLidarrRequiresConfiguration(t *testing.T) {
	t.Parallel()

	t.Run("no url", func(t *testing.T) {
		t.Parallel()

		_, err := newLidarr(
			Config{},
			func(string) (string, error) { return "k", nil },
			slogDiscard(),
		)

		if !errors.Is(err, ErrNotConfigured) {
			t.Errorf("error = %v, want ErrNotConfigured", err)
		}
	})

	t.Run("no api key", func(t *testing.T) {
		t.Parallel()

		_, err := newLidarr(
			Config{Settings: map[string]string{"url": "http://localhost:8686"}},
			func(string) (string, error) { return "", ErrSecretNotFound },
			slogDiscard(),
		)

		if !errors.Is(err, ErrNotConfigured) {
			t.Errorf("error = %v, want ErrNotConfigured", err)
		}
	})
}
