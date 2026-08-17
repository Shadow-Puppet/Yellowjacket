package download

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"yellowjacket/backend/database"
)

// managerFixture wires a manager over a real (in-memory) database, a
// temp staging area and a temp library root, with no network anywhere.
type managerFixture struct {
	manager *Manager
	store   *Store
	staging *Staging
	lib     *stubLibrary
	tags    *recordingTagWriter
	root    string
}

func newManagerFixture(t *testing.T) managerFixture {
	t.Helper()

	db := database.NewTestDB(t)
	seedLibrary(t, db)

	store := NewStore(db)
	staging := newTestStaging(t)
	tags := newRecordingTagWriter()
	root := t.TempDir()
	lib := &stubLibrary{path: root}

	imp := NewImporter(slogDiscard(), staging, tags, lib)

	m := NewManager(
		slogDiscard(), store, NewMemSecretStore(), staging, imp, lib,
	)
	m.SetImportOptions(ImportOptions{})

	return managerFixture{
		manager: m,
		store:   store,
		staging: staging,
		lib:     lib,
		tags:    tags,
		root:    root,
	}
}

// seedLibrary inserts the library row download_requests references.
func seedLibrary(t *testing.T, db *database.DB) {
	t.Helper()

	if _, err := db.ExecContext(
		`INSERT INTO libraries (id, name, path) VALUES (1, 'Test', '/music')`,
	); err != nil {
		t.Fatalf("seed library: %v", err)
	}
}

// fakeWithAlbum returns a fake provider that finds and can deliver the
// four-track reference album.
func fakeWithAlbum(id int64, name string, ext string) *FakeProvider {
	f := NewFakeProvider(id, name, Caps{CanSearch: true, CanTransport: true})

	titles := allTitles()
	c := candidateFor(name+"-cand", titles, ext, 30_000_000)
	c.ProviderID = id

	f.Candidates = []Candidate{c}

	for i, tt := range titles {
		f.Written[trackToken(i+1)+" - "+tt+ext] = []byte("audio-data")
	}

	return f
}

func TestManagerSearchRanksAcrossProviders(t *testing.T) {
	t.Parallel()

	f := newManagerFixture(t)

	mp3 := fakeWithAlbum(1, "mp3-source", ".mp3")
	flac := fakeWithAlbum(2, "flac-source", ".flac")

	f.manager.installProvider(Config{ID: 1, Priority: 50}, mp3)
	f.manager.installProvider(Config{ID: 2, Priority: 50}, flac)

	ranked, err := f.manager.Search(context.Background(), fourTrackDownload())
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(ranked) != 2 {
		t.Fatalf("got %d candidates, want 2", len(ranked))
	}

	if ranked[0].ProviderID != 2 {
		t.Errorf(
			"winner from provider %d, want 2 (FLAC)", ranked[0].ProviderID,
		)
	}

	if mp3.SearchCalls != 1 || flac.SearchCalls != 1 {
		t.Errorf(
			"search calls: mp3=%d flac=%d, want 1 each",
			mp3.SearchCalls, flac.SearchCalls,
		)
	}
}

// One broken provider must not take the others down with it.
func TestManagerSearchToleratesProviderFailure(t *testing.T) {
	t.Parallel()

	f := newManagerFixture(t)

	broken := NewFakeProvider(1, "broken", Caps{CanSearch: true})
	broken.SearchErr = errors.New("connection refused") //nolint:err113 // test

	working := fakeWithAlbum(2, "working", ".flac")

	f.manager.installProvider(Config{ID: 1, Priority: 50}, broken)
	f.manager.installProvider(Config{ID: 2, Priority: 50}, working)

	ranked, err := f.manager.Search(context.Background(), fourTrackDownload())
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(ranked) != 1 {
		t.Fatalf("got %d candidates, want 1 from the working provider", len(ranked))
	}
}

func TestManagerSearchNoProviders(t *testing.T) {
	t.Parallel()

	f := newManagerFixture(t)

	_, err := f.manager.Search(context.Background(), fourTrackDownload())
	if !errors.Is(err, ErrNoProviders) {
		t.Fatalf("error = %v, want ErrNoProviders", err)
	}
}

func TestManagerSearchNoCandidates(t *testing.T) {
	t.Parallel()

	f := newManagerFixture(t)

	empty := NewFakeProvider(1, "empty", Caps{CanSearch: true})
	f.manager.installProvider(Config{ID: 1, Priority: 50}, empty)

	_, err := f.manager.Search(context.Background(), fourTrackDownload())
	if !errors.Is(err, ErrNoCandidates) {
		t.Fatalf("error = %v, want ErrNoCandidates", err)
	}
}

// The whole pipeline: request → search → auto-pick → grab → verify →
// tag → import → library scan.
func TestManagerEndToEndAutoPick(t *testing.T) {
	t.Parallel()

	f := newManagerFixture(t)

	provider := fakeWithAlbum(1, "flac-source", ".flac")
	f.manager.installProvider(Config{ID: 1, Priority: 50}, provider)

	dl := fourTrackDownload()

	ranked, err := f.manager.Start(context.Background(), dl)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if !f.manager.AutoPickable(dl, ranked) {
		t.Fatalf(
			"expected a clear winner to auto-pick; best match %f quality %f",
			ranked[0].Match.Overall, ranked[0].Quality.Overall,
		)
	}

	waitForDownloadState(t, f.store, dl.ID, StateComplete)

	if provider.GrabCalls != 1 {
		t.Errorf("grab calls = %d, want 1", provider.GrabCalls)
	}

	// Files landed in the library, laid out by the template.
	want := filepath.Join(f.root, "Radiohead", "OK Computer", "01 Airbag.flac")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("expected imported file at %s: %v", want, err)
	}

	// Staging release and the rescan happen *after* the state is
	// recorded (manager.go sets StateComplete, then releases, then
	// scans), so waiting on the state is not waiting on these.  Under
	// load the worker is descheduled in between and asserting straight
	// away reads the world one step too early -- which is exactly how
	// this test failed on a busy machine while passing alone.
	waitFor(t, func() bool {
		entries, err := os.ReadDir(f.staging.Root())
		if err != nil || len(entries) != 0 {
			return false
		}

		f.lib.mu.Lock()
		defer f.lib.mu.Unlock()

		return len(f.lib.scanned) == 1
	}, "staging was never released, or the library was never rescanned")
}

// An ambiguous result set must park for the user rather than guess.
func TestManagerWaitsWhenAmbiguous(t *testing.T) {
	t.Parallel()

	f := newManagerFixture(t)

	a := fakeWithAlbum(1, "source-a", ".flac")
	b := fakeWithAlbum(2, "source-b", ".flac")

	f.manager.installProvider(Config{ID: 1, Priority: 50}, a)
	f.manager.installProvider(Config{ID: 2, Priority: 50}, b)

	dl := fourTrackDownload()

	ranked, err := f.manager.Start(context.Background(), dl)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if f.manager.AutoPickable(dl, ranked) {
		t.Fatal("two equivalent candidates must not auto-pick")
	}

	// Nothing was grabbed while waiting for the user.
	if a.GrabCalls != 0 || b.GrabCalls != 0 {
		t.Errorf(
			"grabs happened without a pick: a=%d b=%d",
			a.GrabCalls, b.GrabCalls,
		)
	}

	stored, err := f.store.GetDownload(context.Background(), dl.ID)
	if err != nil {
		t.Fatalf("GetDownload: %v", err)
	}

	if stored.ID != dl.ID {
		t.Errorf("stored request id = %s, want %s", stored.ID, dl.ID)
	}

	// The user picks the second one explicitly.
	if err := f.manager.Pick(
		context.Background(), dl.ID, ranked[1].ID,
	); err != nil {
		t.Fatalf("Pick: %v", err)
	}

	waitForDownloadState(t, f.store, dl.ID, StateComplete)
}

func TestManagerPickUnknownCandidate(t *testing.T) {
	t.Parallel()

	f := newManagerFixture(t)

	provider := fakeWithAlbum(1, "source", ".flac")
	f.manager.installProvider(Config{ID: 1, Priority: 50}, provider)

	dl := fourTrackDownload()
	dl.Expected = nil // free text: never auto-picks

	if _, err := f.manager.Start(context.Background(), dl); err != nil {
		t.Fatalf("Start: %v", err)
	}

	err := f.manager.Pick(context.Background(), dl.ID, "no-such-candidate")
	if !errors.Is(err, ErrCandidateGone) {
		t.Fatalf("error = %v, want ErrCandidateGone", err)
	}
}

// A failed grab must leave staging intact for retry and must not put
// anything in the library.
func TestManagerFailedGrabLeavesLibraryClean(t *testing.T) {
	t.Parallel()

	f := newManagerFixture(t)

	provider := fakeWithAlbum(1, "source", ".flac")
	provider.GrabErr = errors.New("peer went offline") //nolint:err113 // test

	f.manager.installProvider(Config{ID: 1, Priority: 50}, provider)

	dl := fourTrackDownload()

	if _, err := f.manager.Start(context.Background(), dl); err != nil {
		t.Fatalf("Start: %v", err)
	}

	waitForDownloadState(t, f.store, dl.ID, StateFailed)

	entries, err := os.ReadDir(f.root)
	if err != nil {
		t.Fatalf("read library root: %v", err)
	}

	if len(entries) != 0 {
		t.Errorf("failed grab wrote %d entries into the library", len(entries))
	}

	staged, err := os.ReadDir(f.staging.Root())
	if err != nil {
		t.Fatalf("read staging root: %v", err)
	}

	if len(staged) == 0 {
		t.Error("staging released after a failure; nothing left to retry")
	}
}

// A search-only provider's candidate is fetched by whichever enabled
// transport handles its protocol.
func TestManagerPairsSearcherWithTransport(t *testing.T) {
	t.Parallel()

	f := newManagerFixture(t)

	searcher := NewFakeProvider(1, "indexer", Caps{CanSearch: true})

	titles := allTitles()
	c := candidateFor("torrent-cand", titles, ".flac", 30_000_000)
	c.Protocol = ProtocolTorrent
	searcher.Candidates = []Candidate{c}

	transport := NewFakeProvider(2, "torrent-client", Caps{
		CanTransport: true,
		Transports:   []Protocol{ProtocolTorrent},
	})

	for i, tt := range titles {
		transport.Written[trackToken(i+1)+" - "+tt+".flac"] = []byte("audio-data")
	}

	f.manager.installProvider(Config{ID: 1, Priority: 50}, searcher)
	f.manager.installProvider(Config{ID: 2, Priority: 50}, transport)

	dl := fourTrackDownload()

	if _, err := f.manager.Start(context.Background(), dl); err != nil {
		t.Fatalf("Start: %v", err)
	}

	waitForDownloadState(t, f.store, dl.ID, StateComplete)

	if transport.GrabCalls != 1 {
		t.Errorf("transport grabs = %d, want 1", transport.GrabCalls)
	}

	if searcher.GrabCalls != 0 {
		t.Errorf("searcher should not have grabbed, got %d", searcher.GrabCalls)
	}
}

// A candidate whose protocol nothing handles must fail loudly rather
// than being silently dropped.
func TestManagerNoTransportForProtocol(t *testing.T) {
	t.Parallel()

	f := newManagerFixture(t)

	searcher := NewFakeProvider(1, "indexer", Caps{CanSearch: true})

	c := candidateFor("usenet-cand", allTitles(), ".flac", 30_000_000)
	c.Protocol = ProtocolUsenet
	searcher.Candidates = []Candidate{c}

	f.manager.installProvider(Config{ID: 1, Priority: 50}, searcher)

	dl := fourTrackDownload()

	if _, err := f.manager.Start(context.Background(), dl); err != nil {
		t.Fatalf("Start: %v", err)
	}

	waitForDownloadState(t, f.store, dl.ID, StateFailed)
}

// waitForDownloadState polls until a download reaches the wanted state.
// The pipeline runs on its own goroutine, so tests observe it through
// the store rather than by reaching into the manager.
func waitForDownloadState(
	t *testing.T,
	store *Store,
	downloadID string,
	want State,
) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)

	var last State

	for time.Now().Before(deadline) {
		state, _, err := store.GetDownloadState(context.Background(), downloadID)
		if err == nil {
			last = state
			if last == want {
				return
			}
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("download state = %q after 5s, want %q", last, want)
}

// A delegate's files are already in the external manager's library,
// tagged by it and at paths it chose.  The pipeline must record them in
// place rather than moving them out from under a system that is still
// managing them.
func TestManagerDelegateReconcilesInPlace(t *testing.T) {
	t.Parallel()

	f := newManagerFixture(t)
	f.manager.delegatePoll = time.Millisecond

	delegate := NewFakeProvider(1, "lidarr", Caps{
		CanSearch:   true,
		CanDelegate: true,
	})

	c := candidateFor("delegate-cand", allTitles(), ".flac", 30_000_000)
	c.ProviderID = 1
	delegate.Candidates = []Candidate{c}

	external := []string{
		"/external/library/Radiohead/OK Computer/01 Airbag.flac",
		"/external/library/Radiohead/OK Computer/02 Paranoid Android.flac",
	}

	delegate.DelegateStatuses = []DelegateStatus{
		{State: StateGrabbing, Progress: 0.5},
		{State: StateComplete, Progress: 1, ImportedPaths: external},
	}

	f.manager.installProvider(Config{ID: 1, Priority: 50}, delegate)

	dl := fourTrackDownload()

	if _, err := f.manager.Start(context.Background(), dl); err != nil {
		t.Fatalf("Start: %v", err)
	}

	waitForDownloadState(t, f.store, dl.ID, StateComplete)

	if delegate.DelegateCalls != 1 {
		t.Errorf("delegate calls = %d, want 1", delegate.DelegateCalls)
	}

	// Nothing was tagged or written into our library root — the files
	// belong to the external manager.
	entries, err := os.ReadDir(f.root)
	if err != nil {
		t.Fatalf("read library root: %v", err)
	}

	if len(entries) != 0 {
		t.Errorf("delegate import wrote %d entries into our library", len(entries))
	}

	f.tags.mu.Lock()
	writes := len(f.tags.writes)
	f.tags.mu.Unlock()

	if writes != 0 {
		t.Errorf("tagged %d files, want 0 for a delegated import", writes)
	}

	// The external paths were recorded against the item.
	items, err := f.store.ListItemsForDownload(context.Background(), dl.ID)
	if err != nil {
		t.Fatalf("ListItemsForDownload: %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}

	if len(items[0].Imported) != len(external) {
		t.Errorf(
			"recorded %v, want the external manager's paths %v",
			items[0].Imported, external,
		)
	}
}
