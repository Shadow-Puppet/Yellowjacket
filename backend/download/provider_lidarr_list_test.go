package download

import (
	"context"
	"testing"
)

// An artist subscription becomes a monitored Lidarr artist, with the
// scope translated into Lidarr's own monitor option — so the policy
// keeps applying to albums released after the push, which is the whole
// reason to mirror a subscription rather than a list of albums.
func TestLidarrPushArtistWantMapsScope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		scope       WantScope
		wantMonitor string
	}{
		{name: "future", scope: ScopeFuture, wantMonitor: "future"},
		{name: "all", scope: ScopeAll, wantMonitor: "missing"},
	}

	for _, tt := range tests {
		stub := newLidarrStub(t)
		l := newStubLidarr(t, stub)

		id, err := l.PushWant(context.Background(), Want{
			MBID:   "artist-mbid",
			Entity: EntityArtist,
			Artist: "Radiohead",
			Scope:  tt.scope,
		})
		if err != nil {
			t.Fatalf("%s: PushWant: %v", tt.name, err)
		}

		if id != "42" {
			t.Errorf("%s: external id = %q, want 42", tt.name, id)
		}

		stub.mu.Lock()
		added := append([]map[string]any(nil), stub.addedArtists...)
		stub.mu.Unlock()

		if len(added) != 1 {
			t.Fatalf("%s: added %d artists, want 1", tt.name, len(added))
		}

		opts, ok := added[0]["addOptions"].(map[string]any)
		if !ok {
			t.Fatalf("%s: no addOptions in the artist body", tt.name)
		}

		if opts["monitor"] != tt.wantMonitor {
			t.Errorf(
				"%s: monitor = %v, want %q",
				tt.name, opts["monitor"], tt.wantMonitor,
			)
		}

		// Adding an artist must never kick off a discography-wide
		// search on a system the user shares with their own queue.
		if opts["searchForMissingAlbums"] != false {
			t.Errorf("%s: push triggered a search", tt.name)
		}
	}
}

// Pushing a want Lidarr already has returns the existing ID instead of
// adding a second copy — the reconciler pushes on every pass, so this
// is load-bearing rather than tidy.
func TestLidarrPushArtistWantIsIdempotent(t *testing.T) {
	t.Parallel()

	stub := newLidarrStub(t)
	stub.artists = []map[string]any{{
		"id":              7,
		"artistName":      "Radiohead",
		"foreignArtistId": "artist-mbid",
		"monitored":       true,
	}}

	l := newStubLidarr(t, stub)

	id, err := l.PushWant(context.Background(), Want{
		MBID:   "artist-mbid",
		Entity: EntityArtist,
		Artist: "Radiohead",
	})
	if err != nil {
		t.Fatalf("PushWant: %v", err)
	}

	if id != "7" {
		t.Errorf("external id = %q, want the existing artist's 7", id)
	}

	stub.mu.Lock()
	added := len(stub.addedArtists)
	stub.mu.Unlock()

	if added != 0 {
		t.Errorf("added %d artists, want 0 — it already existed", added)
	}
}

// Lidarr cannot express "I want one track", and monitoring the whole
// album to get it would download far more than was asked for.
func TestLidarrPushRecordingWantIsSkipped(t *testing.T) {
	t.Parallel()

	stub := newLidarrStub(t)
	l := newStubLidarr(t, stub)

	id, err := l.PushWant(context.Background(), Want{
		MBID:   "recording-mbid",
		Entity: EntityRecording,
		Title:  "Paranoid Android",
	})
	if err != nil {
		t.Fatalf("PushWant: %v", err)
	}

	if id != "" {
		t.Errorf("external id = %q, want empty (not pushed)", id)
	}

	stub.mu.Lock()
	added := len(stub.addedArtists)
	monitors := len(stub.monitorCalls)
	stub.mu.Unlock()

	if added != 0 || monitors != 0 {
		t.Errorf(
			"track want touched Lidarr: %d artists, %d monitors",
			added, monitors,
		)
	}
}

// Importing adopts monitored artists conservatively: a subscription
// pulled in from elsewhere must not queue a back catalogue.
func TestLidarrListWantsImportsMonitoredArtistsOnly(t *testing.T) {
	t.Parallel()

	stub := newLidarrStub(t)
	stub.artists = []map[string]any{
		{
			"id":              1,
			"artistName":      "Radiohead",
			"foreignArtistId": "artist-1",
			"monitored":       true,
		},
		{
			"id":              2,
			"artistName":      "Unmonitored Band",
			"foreignArtistId": "artist-2",
			"monitored":       false,
		},
		{
			"id":              3,
			"artistName":      "No MBID",
			"foreignArtistId": "",
			"monitored":       true,
		},
	}

	l := newStubLidarr(t, stub)

	wants, err := l.ListWants(context.Background())
	if err != nil {
		t.Fatalf("ListWants: %v", err)
	}

	if len(wants) != 1 {
		t.Fatalf("imported %d wants, want 1", len(wants))
	}

	w := wants[0]

	if w.MBID != "artist-1" {
		t.Errorf("mbid = %q, want artist-1", w.MBID)
	}

	if w.Entity != EntityArtist {
		t.Errorf("entity = %q, want artist", w.Entity)
	}

	if w.Scope != ScopeFuture {
		t.Errorf("scope = %q, want the conservative future", w.Scope)
	}
}

// Removing a want must not tear down a Lidarr setup that may predate
// this app: it unmonitors, it does not delete.
func TestLidarrRemoveWantUnmonitorsOnly(t *testing.T) {
	t.Parallel()

	stub := newLidarrStub(t)
	l := newStubLidarr(t, stub)

	if err := l.RemoveWant(context.Background(), "55"); err != nil {
		t.Fatalf("RemoveWant: %v", err)
	}

	stub.mu.Lock()
	monitors := append([]map[string]any(nil), stub.monitorCalls...)
	stub.mu.Unlock()

	if len(monitors) != 1 {
		t.Fatalf("got %d monitor calls, want 1", len(monitors))
	}

	if monitors[0]["monitored"] != false {
		t.Errorf("monitored = %v, want false", monitors[0]["monitored"])
	}
}

// The Lister role has to be declared, not just implemented, or the
// reconciler never finds it.
func TestLidarrDeclaresListerCapability(t *testing.T) {
	t.Parallel()

	stub := newLidarrStub(t)
	l := newStubLidarr(t, stub)

	if _, ok := asLister(l); !ok {
		t.Error("lidarr does not present as a Lister")
	}

	desc, ok := DescriptorFor(KindLidarr)
	if !ok {
		t.Fatal("no descriptor registered for lidarr")
	}

	if !desc.Caps.CanList {
		t.Error("lidarr's descriptor does not declare CanList")
	}
}
