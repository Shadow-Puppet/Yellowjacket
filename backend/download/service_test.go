package download

import (
	"context"
	"testing"
)

// newServiceFixture wires a Service over the same manager/store a
// managerFixture uses, so a manual download can be started and watched
// through to completion with no network anywhere.
type serviceFixture struct {
	managerFixture

	svc *Service
}

func newServiceFixture(t *testing.T) serviceFixture {
	t.Helper()

	mf := newManagerFixture(t)
	svc := NewService(slogDiscard(), mf.manager, mf.store, NewMemSecretStore())

	return serviceFixture{managerFixture: mf, svc: svc}
}

// A manual download for something anchored by MBID must leave a
// durable Request behind, whether or not the download itself succeeds
// — that is the whole point of ensureRequest: a manual attempt that
// finds nothing right now is not just lost, the reconciler picks it up
// later on its normal schedule.
func TestStartDownloadCreatesRequestForAnchoredDownload(t *testing.T) {
	t.Parallel()

	f := newServiceFixture(t)
	ctx := context.Background()

	provider := fakeWithAlbum(1, "source", ".flac")
	f.manager.installProvider(Config{ID: 1, Priority: 50}, provider)

	dl := fourTrackDownload()

	if _, err := f.svc.StartDownload(SearchRequest{
		LibraryID:        1,
		ReleaseGroupMBID: "rg-1",
		Artist:           dl.Artist,
		Album:            dl.Album,
		Expected:         dl.Expected,
	}); err != nil {
		t.Fatalf("StartDownload: %v", err)
	}

	req, found, err := f.store.FindRequest(ctx, "rg-1", 1)
	if err != nil {
		t.Fatalf("FindRequest: %v", err)
	}

	if !found {
		t.Fatal("manual anchored download did not create a durable request")
	}

	if req.Entity != EntityReleaseGroup {
		t.Errorf("entity = %q, want release-group", req.Entity)
	}

	if req.State != RequestStateWanted {
		t.Errorf("state = %q, want wanted", req.State)
	}
}

// A free-text download (no MBID) has nothing stable to attach a
// request to, and must not create one.
func TestStartDownloadFreeTextCreatesNoRequest(t *testing.T) {
	t.Parallel()

	f := newServiceFixture(t)
	ctx := context.Background()

	provider := fakeWithAlbum(1, "source", ".flac")
	f.manager.installProvider(Config{ID: 1, Priority: 50}, provider)

	if _, err := f.svc.StartDownload(SearchRequest{
		LibraryID: 1,
		Query:     "some free text search",
	}); err != nil {
		t.Fatalf("StartDownload: %v", err)
	}

	all, err := f.store.ListRequests(ctx)
	if err != nil {
		t.Fatalf("ListRequests: %v", err)
	}

	if len(all) != 0 {
		t.Errorf("free-text download created %d requests, want 0", len(all))
	}
}

// A manual download must not un-pause a request the user deliberately
// paused: it runs its one interactive attempt regardless, but the
// request's own state is left alone.
func TestStartDownloadDoesNotUnpauseExistingRequest(t *testing.T) {
	t.Parallel()

	f := newServiceFixture(t)
	ctx := context.Background()

	id, err := f.store.AddRequest(ctx, Request{
		MBID:      "rg-1",
		Entity:    EntityReleaseGroup,
		LibraryID: 1,
		Artist:    "Radiohead",
		Title:     "OK Computer",
	})
	if err != nil {
		t.Fatalf("AddRequest: %v", err)
	}

	if err := f.store.SetRequestState(
		ctx, id, RequestStatePaused, "",
	); err != nil {
		t.Fatalf("SetRequestState: %v", err)
	}

	provider := fakeWithAlbum(1, "source", ".flac")
	f.manager.installProvider(Config{ID: 1, Priority: 50}, provider)

	dl := fourTrackDownload()

	if _, err := f.svc.StartDownload(SearchRequest{
		LibraryID:        1,
		ReleaseGroupMBID: "rg-1",
		Artist:           dl.Artist,
		Album:            dl.Album,
		Expected:         dl.Expected,
	}); err != nil {
		t.Fatalf("StartDownload: %v", err)
	}

	req, err := f.store.GetRequest(ctx, id)
	if err != nil {
		t.Fatalf("GetRequest: %v", err)
	}

	if req.State != RequestStatePaused {
		t.Errorf(
			"a manual download un-paused the request: state = %q, want paused",
			req.State,
		)
	}
}

// A manual download that clearly wins auto-pick still satisfies the
// durable request it was attached to when it completes — the same
// SatisfyRequest call the reconciler relies on.
func TestManualDownloadSatisfiesRequestOnSuccess(t *testing.T) {
	t.Parallel()

	f := newServiceFixture(t)
	ctx := context.Background()

	provider := fakeWithAlbum(1, "source", ".flac")
	f.manager.installProvider(Config{ID: 1, Priority: 50}, provider)

	dl := fourTrackDownload()

	result, err := f.svc.StartDownload(SearchRequest{
		LibraryID:        1,
		ReleaseGroupMBID: "rg-1",
		Artist:           dl.Artist,
		Album:            dl.Album,
		Expected:         dl.Expected,
	})
	if err != nil {
		t.Fatalf("StartDownload: %v", err)
	}

	if !result.AutoPicked {
		t.Fatal("expected a clear single-provider winner to auto-pick")
	}

	waitForDownloadState(t, f.store, result.DownloadID, StateComplete)

	waitFor(t, func() bool {
		req, found, err := f.store.FindRequest(ctx, "rg-1", 1)

		return err == nil && found && req.State == RequestStateSatisfied
	}, "request was never satisfied after its manual download completed")
}
