package download

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeCatalog answers the reconciler's four questions from maps, so the
// wanted list can be tested without an explore index.
type fakeCatalog struct {
	mu sync.Mutex

	// discographies maps artist MBID to release groups.
	discographies map[string][]CatalogItem

	// tracklists maps an MBID to what it should contain.
	tracklists map[string][]ExpectedTrack

	// owned is the set of MBIDs the library has.
	owned map[string]bool

	// discographyErr is returned by ReleaseGroupsForArtist when set.
	discographyErr error
}

func newFakeCatalog() *fakeCatalog {
	return &fakeCatalog{
		discographies: map[string][]CatalogItem{},
		tracklists:    map[string][]ExpectedTrack{},
		owned:         map[string]bool{},
	}
}

func (c *fakeCatalog) ReleaseGroupsForArtist(
	_ context.Context,
	artistMBID string,
) ([]CatalogItem, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.discographyErr != nil {
		return nil, c.discographyErr
	}

	return c.discographies[artistMBID], nil
}

func (c *fakeCatalog) Tracklist(
	_ context.Context,
	_ Entity,
	mbid string,
) ([]ExpectedTrack, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.tracklists[mbid], nil
}

func (c *fakeCatalog) Owns(
	_ context.Context,
	_ Entity,
	mbid string,
) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.owned[mbid], nil
}

func (c *fakeCatalog) Describe(
	_ context.Context,
	_ Entity,
	_ string,
) (CatalogItem, bool) {
	return CatalogItem{}, false
}

// reconcileFixture is a manager fixture plus a wanted list over it.
type reconcileFixture struct {
	managerFixture

	catalog    *fakeCatalog
	reconciler *Reconciler
}

func newReconcileFixture(t *testing.T) reconcileFixture {
	t.Helper()

	mf := newManagerFixture(t)
	cat := newFakeCatalog()

	r := NewReconciler(slogDiscard(), mf.store, mf.manager, cat)

	return reconcileFixture{managerFixture: mf, catalog: cat, reconciler: r}
}

// An artist subscription becomes one want per album, and re-running
// adds nothing — which is what makes it a subscription rather than a
// one-time queue fill.
func TestExpandArtistIsIdempotent(t *testing.T) {
	t.Parallel()

	f := newReconcileFixture(t)
	ctx := context.Background()

	f.catalog.discographies["artist-1"] = []CatalogItem{
		{MBID: "rg-1", Title: "First", FirstReleaseDate: "2030-01-01"},
		{MBID: "rg-2", Title: "Second", FirstReleaseDate: "2030-06-01"},
	}

	if _, err := f.store.AddWant(ctx, Want{
		MBID:      "artist-1",
		Entity:    EntityArtist,
		LibraryID: 1,
		Artist:    "Radiohead",
		Scope:     ScopeAll,
	}); err != nil {
		t.Fatalf("AddWant: %v", err)
	}

	first, err := f.reconciler.expandArtists(ctx)
	if err != nil {
		t.Fatalf("expandArtists: %v", err)
	}

	if first != 2 {
		t.Fatalf("first pass created %d wants, want 2", first)
	}

	second, err := f.reconciler.expandArtists(ctx)
	if err != nil {
		t.Fatalf("expandArtists again: %v", err)
	}

	if second != 0 {
		t.Errorf("second pass created %d wants, want 0", second)
	}

	// A new album appearing later is picked up by the same pass.
	f.catalog.mu.Lock()
	f.catalog.discographies["artist-1"] = append(
		f.catalog.discographies["artist-1"],
		CatalogItem{MBID: "rg-3", Title: "Third", FirstReleaseDate: "2031-01-01"},
	)
	f.catalog.mu.Unlock()

	third, err := f.reconciler.expandArtists(ctx)
	if err != nil {
		t.Fatalf("expandArtists third: %v", err)
	}

	if third != 1 {
		t.Errorf("third pass created %d wants, want 1", third)
	}
}

// A default artist subscription takes new releases only, so subscribing
// does not silently queue a back catalogue.
func TestExpandArtistFutureScopeSkipsBackCatalogue(t *testing.T) {
	t.Parallel()

	f := newReconcileFixture(t)
	ctx := context.Background()

	f.catalog.discographies["artist-1"] = []CatalogItem{
		{MBID: "rg-old", Title: "Old", FirstReleaseDate: "1997-04-22"},
		{MBID: "rg-new", Title: "New", FirstReleaseDate: "2099-01-01"},
	}

	if _, err := f.store.AddWant(ctx, Want{
		MBID:      "artist-1",
		Entity:    EntityArtist,
		LibraryID: 1,
		Scope:     ScopeFuture,
	}); err != nil {
		t.Fatalf("AddWant: %v", err)
	}

	if _, err := f.reconciler.expandArtists(ctx); err != nil {
		t.Fatalf("expandArtists: %v", err)
	}

	wants, err := f.store.ListWants(ctx)
	if err != nil {
		t.Fatalf("ListWants: %v", err)
	}

	for _, w := range wants {
		if w.MBID == "rg-old" {
			t.Error("future scope queued a back-catalogue album")
		}
	}

	if len(wants) != 2 {
		t.Errorf("got %d wants (artist + new album), want 2", len(wants))
	}
}

// One artist whose discography will not resolve must not stop the rest
// of the list being expanded.
func TestExpandArtistToleratesCatalogFailure(t *testing.T) {
	t.Parallel()

	f := newReconcileFixture(t)
	ctx := context.Background()

	f.catalog.discographyErr = errors.New("index not ready") //nolint:err113 // test

	if _, err := f.store.AddWant(ctx, Want{
		MBID:      "artist-1",
		Entity:    EntityArtist,
		LibraryID: 1,
	}); err != nil {
		t.Fatalf("AddWant: %v", err)
	}

	created, err := f.reconciler.expandArtists(ctx)
	if err != nil {
		t.Fatalf("expandArtists returned an error for one bad artist: %v", err)
	}

	if created != 0 {
		t.Errorf("created %d wants from a failing catalog, want 0", created)
	}
}

// Something the library already owns is retired, however it got there.
func TestRetireOwnedSatisfiesWants(t *testing.T) {
	t.Parallel()

	f := newReconcileFixture(t)
	ctx := context.Background()

	id, err := f.store.AddWant(ctx, Want{
		MBID:      "rg-1",
		Entity:    EntityReleaseGroup,
		LibraryID: 1,
	})
	if err != nil {
		t.Fatalf("AddWant: %v", err)
	}

	f.catalog.owned["rg-1"] = true

	n, err := f.reconciler.retireOwned(ctx)
	if err != nil {
		t.Fatalf("retireOwned: %v", err)
	}

	if n != 1 {
		t.Fatalf("retired %d wants, want 1", n)
	}

	w, err := f.store.GetWant(ctx, id)
	if err != nil {
		t.Fatalf("GetWant: %v", err)
	}

	if w.State != WantStateSatisfied {
		t.Errorf("state = %q, want satisfied", w.State)
	}
}

// The end-to-end case: a want with a clear winner downloads without
// anyone watching, and is satisfied when the files land.
func TestReconcileDownloadsAndSatisfies(t *testing.T) {
	t.Parallel()

	f := newReconcileFixture(t)
	ctx := context.Background()

	provider := fakeWithAlbum(1, "source", ".flac")
	f.manager.installProvider(Config{ID: 1, Priority: 50}, provider)

	id, err := f.store.AddWant(ctx, Want{
		MBID:      "rg-1",
		Entity:    EntityReleaseGroup,
		LibraryID: 1,
		Artist:    "Radiohead",
		Title:     "OK Computer",
	})
	if err != nil {
		t.Fatalf("AddWant: %v", err)
	}

	f.catalog.tracklists["rg-1"] = fourTrackRequest().Expected

	summary, err := f.reconciler.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if summary.Attempted != 1 || summary.Started != 1 {
		t.Fatalf(
			"attempted=%d started=%d, want 1 and 1 (last error: see want)",
			summary.Attempted, summary.Started,
		)
	}

	waitFor(t, func() bool {
		w, err := f.store.GetWant(ctx, id)

		return err == nil && w.State == WantStateSatisfied
	}, "want was never satisfied after its download completed")

	if provider.GrabCalls != 1 {
		t.Errorf("grab calls = %d, want 1", provider.GrabCalls)
	}
}

// Nothing good enough is not a failure.  The want stays wanted, gains
// an attempt and a reason, and leaves no request row behind.
func TestReconcileKeepsWantingWhenNothingIsGoodEnough(t *testing.T) {
	t.Parallel()

	f := newReconcileFixture(t)
	ctx := context.Background()

	// A provider that finds only an unrelated album: enough to return
	// candidates, nowhere near enough to auto-pick.
	provider := NewFakeProvider(1, "weak", Caps{CanSearch: true, CanTransport: true})
	provider.Candidates = []Candidate{candidateFor(
		"weak-1", []string{"Something Else Entirely"}, ".mp3", 3_000_000,
	)}

	f.manager.installProvider(Config{ID: 1, Priority: 50}, provider)

	id, err := f.store.AddWant(ctx, Want{
		MBID:      "rg-1",
		Entity:    EntityReleaseGroup,
		LibraryID: 1,
		Artist:    "Radiohead",
		Title:     "OK Computer",
	})
	if err != nil {
		t.Fatalf("AddWant: %v", err)
	}

	f.catalog.tracklists["rg-1"] = fourTrackRequest().Expected

	summary, err := f.reconciler.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if summary.Started != 0 {
		t.Fatalf("started %d downloads, want 0", summary.Started)
	}

	w, err := f.store.GetWant(ctx, id)
	if err != nil {
		t.Fatalf("GetWant: %v", err)
	}

	if w.State != WantStateWanted {
		t.Errorf("state = %q, want it still wanted", w.State)
	}

	if w.Attempts != 1 {
		t.Errorf("attempts = %d, want 1", w.Attempts)
	}

	if w.LastError == "" {
		t.Error("no reason was recorded for the user")
	}

	if !w.NextTryAt.After(time.Now()) {
		t.Errorf("next try at %v, want it in the future", w.NextTryAt)
	}

	// The whole point of Attempt over Start: an unsuccessful pass
	// leaves no request row to clutter the downloads list.
	requests, err := f.store.ListRequests(ctx, 50)
	if err != nil {
		t.Fatalf("ListRequests: %v", err)
	}

	if len(requests) != 0 {
		t.Errorf("got %d request rows from a fruitless pass, want 0", len(requests))
	}
}

// A want with no resolvable tracklist waits rather than guessing.
func TestReconcileWaitsWithoutTracklist(t *testing.T) {
	t.Parallel()

	f := newReconcileFixture(t)
	ctx := context.Background()

	provider := fakeWithAlbum(1, "source", ".flac")
	f.manager.installProvider(Config{ID: 1, Priority: 50}, provider)

	if _, err := f.store.AddWant(ctx, Want{
		MBID:      "rg-1",
		Entity:    EntityReleaseGroup,
		LibraryID: 1,
		Artist:    "Radiohead",
		Title:     "OK Computer",
	}); err != nil {
		t.Fatalf("AddWant: %v", err)
	}

	summary, err := f.reconciler.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if summary.Started != 0 {
		t.Errorf("started %d downloads with no tracklist, want 0", summary.Started)
	}

	if provider.SearchCalls != 0 {
		t.Errorf(
			"searched %d times with no tracklist to verify against, want 0",
			provider.SearchCalls,
		)
	}
}

// Artist subscriptions are never attempted as downloads: they expand.
func TestArtistWantsAreNeverDue(t *testing.T) {
	t.Parallel()

	f := newReconcileFixture(t)
	ctx := context.Background()

	if _, err := f.store.AddWant(ctx, Want{
		MBID:      "artist-1",
		Entity:    EntityArtist,
		LibraryID: 1,
	}); err != nil {
		t.Fatalf("AddWant: %v", err)
	}

	due, err := f.store.ListDueWants(ctx, 10)
	if err != nil {
		t.Fatalf("ListDueWants: %v", err)
	}

	if len(due) != 0 {
		t.Errorf("got %d due wants, want 0 — artists expand, not download", len(due))
	}
}

// A pass attempts at most its batch size, so a large list is worked
// through steadily rather than in one flood.
func TestReconcileRespectsBatchSize(t *testing.T) {
	t.Parallel()

	f := newReconcileFixture(t)
	ctx := context.Background()

	f.reconciler.SetBatch(2)

	for _, mbid := range []string{"rg-1", "rg-2", "rg-3", "rg-4"} {
		if _, err := f.store.AddWant(ctx, Want{
			MBID:      mbid,
			Entity:    EntityReleaseGroup,
			LibraryID: 1,
			Title:     "Album " + mbid,
		}); err != nil {
			t.Fatalf("AddWant: %v", err)
		}

		f.catalog.tracklists[mbid] = fourTrackRequest().Expected
	}

	summary, err := f.reconciler.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if summary.Attempted != 2 {
		t.Errorf("attempted %d wants, want 2 (the batch size)", summary.Attempted)
	}
}

// waitFor polls a condition, failing the test if it never holds.  Used
// where the pipeline hands work to a goroutine.
func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		if cond() {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal(msg)
}
