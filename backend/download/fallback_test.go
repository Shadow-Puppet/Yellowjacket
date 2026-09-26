package download

import (
	"context"
	"errors"
	"os"
	"testing"
)

// A transfer that fails on one copy of an album is not a failed
// download while another acceptable copy exists.  On Soulseek the usual
// failure is one peer being offline, with several others offering the
// same folder.

var errPeerOffline = errors.New("peer went offline")

func TestManagerFallsBackToTheNextCandidate(t *testing.T) {
	t.Parallel()

	f := newManagerFixture(t)

	// The failing source ranks first on priority, so the fallback is
	// what reaches the one that works.
	bad := fakeWithAlbum(1, "offline-peer", ".flac")
	bad.GrabErr = errPeerOffline
	good := fakeWithAlbum(2, "online-peer", ".flac")

	f.manager.installProvider(Config{ID: 1, Priority: 90}, bad)
	f.manager.installProvider(Config{ID: 2, Priority: 10}, good)

	dl := fourTrackDownload()

	if _, err := f.manager.Start(context.Background(), dl); err != nil {
		t.Fatalf("Start: %v", err)
	}

	waitForDownloadState(t, f.store, dl.ID, StateComplete)

	if bad.GrabCalls != 1 || good.GrabCalls != 1 {
		t.Errorf(
			"grabs: failing=%d working=%d, want 1 and 1",
			bad.GrabCalls, good.GrabCalls,
		)
	}

	// The abandoned attempt's staging goes with it; only a request that
	// fails outright keeps its staging for inspection.
	waitFor(t, func() bool {
		entries, err := os.ReadDir(f.staging.Root())

		return err == nil && len(entries) == 0
	}, "the failed attempt's staging was never released")
}

// Falling back must not lower the bar.  A second choice outside the
// user's guardrails is not a choice auto-pick may make, first or second.
func TestManagerFallbackRespectsTheGuardrails(t *testing.T) {
	t.Parallel()

	f := newManagerFixture(t)
	f.manager.SetPreferences(AutoDownloadPrefs{MaxSizeMB: 50})

	bad := fakeWithAlbum(1, "offline-peer", ".flac")
	bad.GrabErr = errPeerOffline
	bad.Candidates[0].TotalSize = 40 << 20

	huge := fakeWithAlbum(2, "oversized", ".flac")
	huge.Candidates[0].TotalSize = 900 << 20

	f.manager.installProvider(Config{ID: 1, Priority: 90}, bad)
	f.manager.installProvider(Config{ID: 2, Priority: 10}, huge)

	dl := fourTrackDownload()

	if _, err := f.manager.Start(context.Background(), dl); err != nil {
		t.Fatalf("Start: %v", err)
	}

	waitForDownloadState(t, f.store, dl.ID, StateFailed)

	if huge.GrabCalls != 0 {
		t.Errorf("fell back to a candidate over the size ceiling")
	}
}

// A copy the user picked by hand is the copy they asked for.  Quietly
// substituting another is a decision they did not make.
func TestManagerPickDoesNotFallBack(t *testing.T) {
	t.Parallel()

	f := newManagerFixture(t)

	bad := fakeWithAlbum(1, "offline-peer", ".flac")
	bad.GrabErr = errPeerOffline
	good := fakeWithAlbum(2, "online-peer", ".flac")

	f.manager.installProvider(Config{ID: 1, Priority: 90}, bad)
	f.manager.installProvider(Config{ID: 2, Priority: 10}, good)

	// A ceiling below both copies parks the result set for the user.
	f.manager.SetPreferences(AutoDownloadPrefs{MaxSizeMB: 1})

	bad.Candidates[0].TotalSize = 30 << 20
	good.Candidates[0].TotalSize = 30 << 20

	dl := fourTrackDownload()

	if _, err := f.manager.Start(context.Background(), dl); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := f.manager.Pick(
		context.Background(), dl.ID, "offline-peer-cand",
	); err != nil {
		t.Fatalf("Pick: %v", err)
	}

	waitForDownloadState(t, f.store, dl.ID, StateFailed)

	if good.GrabCalls != 0 {
		t.Errorf("a hand-picked grab fell back to another candidate")
	}
}

// Fallback is for surviving an offline peer or two, not for walking a
// forty-peer list for six hours.
func TestManagerFallbackIsBounded(t *testing.T) {
	t.Parallel()

	f := newManagerFixture(t)

	var providers []*FakeProvider

	for i := int64(1); i <= maxGrabAttempts+2; i++ {
		p := fakeWithAlbum(i, "peer-"+itoa(int(i)), ".flac")
		p.GrabErr = errPeerOffline

		f.manager.installProvider(Config{ID: i, Priority: 50}, p)
		providers = append(providers, p)
	}

	dl := fourTrackDownload()

	if _, err := f.manager.Start(context.Background(), dl); err != nil {
		t.Fatalf("Start: %v", err)
	}

	waitForDownloadState(t, f.store, dl.ID, StateFailed)

	grabs := 0
	for _, p := range providers {
		grabs += p.GrabCalls
	}

	if grabs != maxGrabAttempts {
		t.Errorf("grabs = %d, want %d", grabs, maxGrabAttempts)
	}
}

// The veto judges the best candidate *inside* the guardrails, so the
// grab has to take that one — not the overall best, which may be the
// very copy the user said not to take unattended.
func TestManagerAutoPickTakesTheBestEligibleCandidate(t *testing.T) {
	t.Parallel()

	f := newManagerFixture(t)
	f.manager.SetPreferences(AutoDownloadPrefs{MaxSizeMB: 50})

	huge := fakeWithAlbum(1, "oversized", ".flac")
	huge.Candidates[0].TotalSize = 900 << 20

	fits := fakeWithAlbum(2, "fits", ".flac")
	fits.Candidates[0].TotalSize = 40 << 20

	f.manager.installProvider(Config{ID: 1, Priority: 90}, huge)
	f.manager.installProvider(Config{ID: 2, Priority: 10}, fits)

	dl := fourTrackDownload()

	ranked, err := f.manager.Start(context.Background(), dl)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if ranked[0].ID != "oversized-cand" {
		t.Fatalf("fixture: best overall is %s, want the oversized copy", ranked[0].ID)
	}

	waitForDownloadState(t, f.store, dl.ID, StateComplete)

	if huge.GrabCalls != 0 || fits.GrabCalls != 1 {
		t.Errorf(
			"grabs: oversized=%d fits=%d, want 0 and 1",
			huge.GrabCalls, fits.GrabCalls,
		)
	}
}

// On Soulseek a failure is the peer's, so every folder that peer offered
// goes with it.  Elsewhere a failure is the release's, and one indexer's
// other releases are still worth trying.
func TestRuledOutBy(t *testing.T) {
	t.Parallel()

	failed := []Candidate{
		{ID: "slskd:alice:Album", Kind: KindSlskd, ProviderID: 1, Origin: "alice"},
		{ID: "tracker-1", Kind: KindProwlarr, ProviderID: 2, Origin: "indexer"},
	}

	cases := []struct {
		name string
		c    Candidate
		want bool
	}{
		{
			name: "the same candidate",
			c:    Candidate{ID: "tracker-1", Kind: KindProwlarr, ProviderID: 2, Origin: "indexer"},
			want: true,
		},
		{
			name: "another folder from a failed peer",
			c: Candidate{
				ID:         "slskd:alice:Album (2)",
				Kind:       KindSlskd,
				ProviderID: 1,
				Origin:     "alice",
			},
			want: true,
		},
		{
			name: "another peer",
			c:    Candidate{ID: "slskd:bob:Album", Kind: KindSlskd, ProviderID: 1, Origin: "bob"},
			want: false,
		},
		{
			name: "another release from the same indexer",
			c:    Candidate{ID: "tracker-2", Kind: KindProwlarr, ProviderID: 2, Origin: "indexer"},
			want: false,
		},
		{
			name: "a peer of the same name on a different daemon",
			c: Candidate{
				ID:         "slskd:alice:Album",
				Kind:       KindSlskd,
				ProviderID: 3,
				Origin:     "alice",
			},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := ruledOutBy(tc.c, failed); got != tc.want {
				t.Errorf("ruledOutBy = %v, want %v", got, tc.want)
			}
		})
	}
}
