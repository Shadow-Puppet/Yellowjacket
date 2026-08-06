package download

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// prowlarrStub is a fake Prowlarr instance.
type prowlarrStub struct {
	server *httptest.Server

	mu sync.Mutex

	results  []prowlarrResult
	indexers []map[string]any

	// lastQuery records the search query string for assertions.
	lastQuery string

	unauthorized bool
}

func newProwlarrStub(t *testing.T) *prowlarrStub {
	t.Helper()

	s := &prowlarrStub{
		indexers: []map[string]any{{"id": 1, "enable": true}},
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/system/status", func(w http.ResponseWriter, r *http.Request) {
		if s.reject(w, r) {
			return
		}

		writeJSON(t, w, map[string]any{"version": "1.0.0"})
	})

	mux.HandleFunc("/api/v1/indexer", func(w http.ResponseWriter, r *http.Request) {
		if s.reject(w, r) {
			return
		}

		s.mu.Lock()
		indexers := s.indexers
		s.mu.Unlock()

		writeJSON(t, w, indexers)
	})

	mux.HandleFunc("/api/v1/search", func(w http.ResponseWriter, r *http.Request) {
		if s.reject(w, r) {
			return
		}

		s.mu.Lock()
		s.lastQuery = r.URL.RawQuery
		results := s.results
		s.mu.Unlock()

		writeJSON(t, w, results)
	})

	s.server = httptest.NewServer(mux)
	t.Cleanup(s.server.Close)

	return s
}

func (s *prowlarrStub) reject(w http.ResponseWriter, r *http.Request) bool {
	s.mu.Lock()
	unauthorized := s.unauthorized
	s.mu.Unlock()

	if unauthorized || r.Header.Get("X-Api-Key") != "test-key" {
		w.WriteHeader(http.StatusUnauthorized)

		return true
	}

	return false
}

func newStubProwlarr(t *testing.T, stub *prowlarrStub, settings map[string]string) *prowlarr {
	t.Helper()

	if settings == nil {
		settings = map[string]string{}
	}

	settings["url"] = stub.server.URL

	p, err := newProwlarr(
		Config{ID: 1, Kind: KindProwlarr, Name: "prowlarr", Settings: settings},
		func(string) (string, error) { return "test-key", nil },
		slogDiscard(),
	)
	if err != nil {
		t.Fatalf("newProwlarr: %v", err)
	}

	pr, ok := p.(*prowlarr)
	if !ok {
		t.Fatalf("provider is %T, want *prowlarr", p)
	}

	return pr
}

func TestProwlarrCheck(t *testing.T) {
	t.Parallel()

	stub := newProwlarrStub(t)
	p := newStubProwlarr(t, stub, nil)

	if err := p.Check(context.Background()); err != nil {
		t.Errorf("Check: %v", err)
	}
}

// Prowlarr with every indexer disabled will silently return nothing
// forever, which is worth surfacing at configuration time.
func TestProwlarrCheckRequiresEnabledIndexer(t *testing.T) {
	t.Parallel()

	stub := newProwlarrStub(t)
	stub.indexers = []map[string]any{{"id": 1, "enable": false}}

	p := newStubProwlarr(t, stub, nil)

	if err := p.Check(context.Background()); !errors.Is(
		err, ErrProwlarrNoIndexers,
	) {
		t.Errorf("error = %v, want ErrProwlarrNoIndexers", err)
	}
}

// Prowlarr fills only the Searcher role, so its candidates must carry a
// protocol the pipeline can pair with a transport.
func TestProwlarrSearchMarksProtocols(t *testing.T) {
	t.Parallel()

	stub := newProwlarrStub(t)
	stub.results = []prowlarrResult{
		{
			GUID:      "a",
			Title:     "Radiohead - OK Computer [FLAC]",
			Indexer:   "SomeTracker",
			Protocol:  "torrent",
			Seeders:   50,
			Size:      400_000_000,
			MagnetURL: "magnet:?xt=urn:btih:abc123",
		},
		{
			GUID:        "b",
			Title:       "Radiohead - OK Computer [MP3]",
			Indexer:     "SomeUsenet",
			Protocol:    "usenet",
			Size:        90_000_000,
			DownloadURL: "https://example.com/x.nzb",
		},
	}

	p := newStubProwlarr(t, stub, nil)

	got, err := p.Search(context.Background(), Request{
		Artist: "Radiohead",
		Album:  "OK Computer",
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("got %d candidates, want 2", len(got))
	}

	byProtocol := map[Protocol]Candidate{}
	for _, c := range got {
		byProtocol[c.Protocol] = c
	}

	torrent, ok := byProtocol[ProtocolTorrent]
	if !ok {
		t.Fatal("no torrent candidate")
	}

	if torrent.Payload["link"] != "magnet:?xt=urn:btih:abc123" {
		t.Errorf("torrent link = %q, want the magnet", torrent.Payload["link"])
	}

	usenet, ok := byProtocol[ProtocolUsenet]
	if !ok {
		t.Fatal("no usenet candidate")
	}

	if usenet.Payload["link"] != "https://example.com/x.nzb" {
		t.Errorf("usenet link = %q, want the NZB URL", usenet.Payload["link"])
	}

	// Indexer results are opaque before fetching; claiming a file list
	// would be inventing information.
	if len(torrent.Files) != 0 {
		t.Errorf("torrent candidate has %d files, want none", len(torrent.Files))
	}
}

// A torrent nobody is seeding will never finish, so offering it wastes
// the user's choice.
func TestProwlarrFiltersDeadTorrents(t *testing.T) {
	t.Parallel()

	stub := newProwlarrStub(t)
	stub.results = []prowlarrResult{
		{
			GUID: "dead", Title: "Dead", Protocol: "torrent",
			Seeders: 0, MagnetURL: "magnet:?xt=urn:btih:dead",
		},
		{
			GUID: "alive", Title: "Alive", Protocol: "torrent",
			Seeders: 10, MagnetURL: "magnet:?xt=urn:btih:alive",
		},
	}

	p := newStubProwlarr(t, stub, map[string]string{"minSeeders": "1"})

	got, err := p.Search(context.Background(), Request{Query: "x"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("got %d candidates, want 1", len(got))
	}

	if got[0].Title != "Alive" {
		t.Errorf("kept %q, want the seeded torrent", got[0].Title)
	}
}

// Searching without a category constraint returns films and software
// alongside music.
func TestProwlarrSearchesMusicCategory(t *testing.T) {
	t.Parallel()

	stub := newProwlarrStub(t)
	p := newStubProwlarr(t, stub, nil)

	if _, err := p.Search(
		context.Background(), Request{Query: "radiohead"},
	); err != nil {
		t.Fatalf("Search: %v", err)
	}

	stub.mu.Lock()
	query := stub.lastQuery
	stub.mu.Unlock()

	if !strings.Contains(query, "categories="+prowlarrMusicCategory) {
		t.Errorf("query %q does not constrain to the music category", query)
	}
}

func TestSwarmHealth(t *testing.T) {
	t.Parallel()

	// More seeders is never worse.
	prev := -1.0

	for _, seeders := range []int{0, 1, 3, 10, 50, 500} {
		got := swarmHealth(ProtocolTorrent, seeders)

		if got < prev {
			t.Errorf("health fell at %d seeders: %f after %f", seeders, got, prev)
		}

		if got < 0 || got > 1 {
			t.Errorf("health %f out of range at %d seeders", got, seeders)
		}

		prev = got
	}

	// Usenet has no swarm, so seeder count is meaningless there.
	if a, b := swarmHealth(ProtocolUsenet, 0), swarmHealth(ProtocolUsenet, 99); a != b {
		t.Errorf("usenet health varied with seeders: %f vs %f", a, b)
	}
}

func TestProtocolFor(t *testing.T) {
	t.Parallel()

	tests := map[string]Protocol{
		"torrent": ProtocolTorrent,
		"Torrent": ProtocolTorrent,
		"usenet":  ProtocolUsenet,
		"USENET":  ProtocolUsenet,
		"weird":   ProtocolDirect,
		"":        ProtocolDirect,
	}

	for in, want := range tests {
		if got := protocolFor(in); got != want {
			t.Errorf("protocolFor(%q) = %q, want %q", in, got, want)
		}
	}
}
