package download

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Soulseek politeness is per peer, not per daemon (#272).

func TestKeyedLockSerialisesOneKeyOnly(t *testing.T) {
	t.Parallel()

	var l keyedLock[string]

	ctx := context.Background()

	releaseA, err := l.acquire(ctx, "a")
	if err != nil {
		t.Fatalf("acquire a: %v", err)
	}

	// Another key is free while "a" is held.
	releaseB, err := l.acquire(ctx, "b")
	if err != nil {
		t.Fatalf("acquire b: %v", err)
	}

	releaseB()

	// The same key waits, and gives up with its context.
	short, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()

	if _, err := l.acquire(short, "a"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second acquire of a held key = %v, want the deadline", err)
	}

	releaseA()
	releaseA() // Idempotent: a second call must not free someone else's hold.

	if n := l.size(); n != 0 {
		t.Errorf("%d keys left behind, want none once nobody holds or waits", n)
	}
}

// grabEach runs one grab per candidate and returns a function that waits
// for all of them; grabAll's reasons for waiting apply.
func grabEach(t *testing.T, f managerFixture, cands []Candidate) func() {
	t.Helper()

	ctx := context.Background()

	var wg sync.WaitGroup

	for i, c := range cands {
		dl := fourTrackDownload()
		dl.ID = "dl-" + string(rune('a'+i))

		if err := f.store.CreateDownload(ctx, dl); err != nil {
			t.Fatalf("CreateDownload: %v", err)
		}

		wg.Add(1)

		go func() {
			defer wg.Done()

			f.manager.grab(ctx, dl, c, nil, false)
		}()
	}

	return func() {
		done := make(chan struct{})

		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("transfers did not finish")
		}
	}
}

func slskdCandidates(p *FakeProvider, peers ...string) []Candidate {
	out := make([]Candidate, 0, len(peers))

	for i, peer := range peers {
		c := p.Candidates[0]
		c.ID = c.ID + "-" + itoa(i)
		c.Kind = KindSlskd
		c.ProviderID = 1
		c.Origin = peer
		out = append(out, c)
	}

	return out
}

// Three albums from one user are asked for one at a time, even though
// the daemon would allow three transfers.
func TestOnePeerIsAskedForOneThingAtATime(t *testing.T) {
	t.Parallel()

	f := newManagerFixture(t)
	f.manager.SetMaxConcurrent(4)

	p := fakeWithAlbum(1, "slskd", ".flac")
	p.GrabGate = make(chan struct{})

	f.manager.installProvider(Config{ID: 1, Kind: KindSlskd, Priority: 50}, p)

	wait := grabEach(t, f, slskdCandidates(p, "alice", "alice", "alice"))

	waitFor(t, func() bool { return p.GrabCallCount() >= 1 }, "no grab started")
	time.Sleep(150 * time.Millisecond)

	if got := p.MaxParallelGrabs(); got != 1 {
		t.Errorf("%d simultaneous grabs from one peer, want 1", got)
	}

	close(p.GrabGate)

	waitFor(t, func() bool { return p.GrabCallCount() == 3 }, "queued grabs never ran")
	wait()

	if n := f.manager.peerLocks.size(); n != 0 {
		t.Errorf("%d peer locks left behind", n)
	}
}

// Different users run at once, up to the daemon's cap — the point of
// the change: one slow peer no longer holds up every other.
func TestDifferentPeersRunTogether(t *testing.T) {
	t.Parallel()

	f := newManagerFixture(t)
	f.manager.SetMaxConcurrent(8)

	p := fakeWithAlbum(1, "slskd", ".flac")
	p.GrabGate = make(chan struct{})

	f.manager.installProvider(Config{ID: 1, Kind: KindSlskd, Priority: 50}, p)

	wait := grabEach(t, f, slskdCandidates(p, "alice", "bob", "carol", "dave"))

	waitFor(
		t,
		func() bool { return p.MaxParallelGrabs() >= kindConcurrency[KindSlskd] },
		"different peers were serialised",
	)
	time.Sleep(100 * time.Millisecond)

	if got := p.MaxParallelGrabs(); got != kindConcurrency[KindSlskd] {
		t.Errorf("%d simultaneous grabs, want the daemon cap %d", got, kindConcurrency[KindSlskd])
	}

	close(p.GrabGate)
	wait()
}

func TestSlskdLocalFolders(t *testing.T) {
	t.Parallel()

	s := &slskd{downloadsPath: "/dl"}

	got := s.localFolders(Candidate{Files: []CandidateFile{
		{Path: `\m\The Wall\CD2\01 Hey You.flac`},
		{Path: `\m\The Wall\CD1\01 In The Flesh.flac`},
		{Path: `\m\The Wall\CD1\02 The Thin Ice.flac`},
	}})

	want := []string{filepath.Join("/dl", "CD1"), filepath.Join("/dl", "CD2")}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("localFolders = %q, want %q", got, want)
	}
}

// Two peers' "Greatest Hits" land in one slskd directory, so the second
// grab does not enqueue until the first has collected its files.
func TestSlskdSameFolderNameWaits(t *testing.T) {
	t.Parallel()

	stub := newSlskdStub(t)
	s, downloads := newStubSlskd(t, stub)

	c := Candidate{
		Payload: map[string]string{"username": "bob"},
		Files: []CandidateFile{
			{Path: `\music\Greatest Hits\01 Intro.flac`, Size: 1, IsAudio: true},
		},
	}

	release, err := lockSlskdFolders(
		context.Background(), []string{filepath.Join(downloads, "Greatest Hits")},
	)
	if err != nil {
		t.Fatalf("lock: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	if _, err := s.Grab(ctx, c, t.TempDir(), nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Grab = %v, want it to wait on the held folder", err)
	}

	release()

	stub.mu.Lock()
	posted := stub.posted
	stub.mu.Unlock()

	if posted {
		t.Error("enqueued transfers into a folder another grab held")
	}
}
