package download

import (
	"context"
	"sync"
	"testing"
	"time"
)

// grabAll starts n transfers in the background and returns a function
// that waits for every one of them to *finish*.
//
// Waiting is not optional, and it is not about the assertions.  A
// `grab` outlives the provider's `Grab`: it imports the staged files,
// releases the reservation and writes the download's final state.  A
// test that returns while those are running races `t.TempDir()`'s
// cleanup, which deletes the staging directory underneath them and
// fails with `TempDir RemoveAll cleanup: directory not empty` — from
// the *test framework*, after the test has passed, naming no line of
// code.  It failed roughly one run in three and then, on a slower CI
// container, every run.
func grabAll(
	t *testing.T,
	f managerFixture,
	prov *FakeProvider,
	n int,
) func() {
	t.Helper()

	ctx := context.Background()

	var wg sync.WaitGroup

	for i := range n {
		dl := fourTrackDownload()
		dl.ID = "dl-" + string(rune('a'+i))

		if err := f.store.CreateDownload(ctx, dl); err != nil {
			t.Fatalf("CreateDownload: %v", err)
		}

		candidate := prov.Candidates[0]
		candidate.ProviderID = 1

		wg.Add(1)

		go func() {
			defer wg.Done()

			f.manager.grab(ctx, dl, candidate, nil)
		}()
	}

	return func() {
		done := make(chan struct{})

		go func() {
			wg.Wait()
			close(done)
		}()

		// A bare wg.Wait() on a transfer that never returns hangs the
		// package until the test binary's timeout, with no clue which
		// test is stuck.
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("transfers did not finish")
		}
	}
}

func TestConcurrencyForPrefersOverrideThenKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  Config
		want int
	}{
		{
			name: "slskd defaults to one",
			cfg:  Config{Kind: KindSlskd},
			want: 1,
		},
		{
			name: "usenet defaults higher",
			cfg:  Config{Kind: KindSABnzbd},
			want: 4,
		},
		{
			name: "explicit override wins",
			cfg: Config{
				Kind:     KindSlskd,
				Settings: map[string]string{concurrencyKey: "3"},
			},
			want: 3,
		},
		{
			name: "nonsense override falls back",
			cfg: Config{
				Kind:     KindSlskd,
				Settings: map[string]string{concurrencyKey: "not a number"},
			},
			want: 1,
		},
		{
			name: "zero override falls back",
			cfg: Config{
				Kind:     KindSlskd,
				Settings: map[string]string{concurrencyKey: "0"},
			},
			want: 1,
		},
		{
			name: "unknown kind falls back to the global default",
			cfg:  Config{Kind: Kind("something-new")},
			want: defaultConcurrency,
		},
	}

	for _, tt := range tests {
		if got := concurrencyFor(tt.cfg); got != tt.want {
			t.Errorf("%s: got %d, want %d", tt.name, got, tt.want)
		}
	}
}

// The reason the per-provider cap exists: a Soulseek daemon capped at
// one transfer must serialize, even when the global cap would allow
// more and the user has queued several albums at once.
func TestPerProviderCapSerializesTransfers(t *testing.T) {
	t.Parallel()

	f := newManagerFixture(t)
	f.manager.SetMaxConcurrent(4)

	slow := fakeWithAlbum(1, "slskd-like", ".flac")
	slow.GrabGate = make(chan struct{})

	f.manager.installProvider(Config{
		ID:       1,
		Kind:     KindSlskd,
		Priority: 50,
	}, slow)

	// Three requests against the same one-at-a-time provider.
	wait := grabAll(t, f, slow, 3)

	// Give all three a chance to reach the transport, then check how
	// many actually got through the gate.
	waitFor(t, func() bool { return slow.GrabCallCount() >= 1 }, "no grab started")
	time.Sleep(150 * time.Millisecond)

	if got := slow.MaxParallelGrabs(); got != 1 {
		t.Errorf("%d simultaneous transfers, want 1", got)
	}

	close(slow.GrabGate)

	waitFor(
		t,
		func() bool { return slow.GrabCallCount() == 3 },
		"not every queued transfer ran once the first finished",
	)

	if got := slow.MaxParallelGrabs(); got != 1 {
		t.Errorf("%d simultaneous transfers overall, want 1", got)
	}

	wait()
}

// A provider that tolerates parallelism is not held to Soulseek's
// limit, and the global cap is what bounds it.
func TestPerProviderCapAllowsParallelWhereSafe(t *testing.T) {
	t.Parallel()

	f := newManagerFixture(t)
	f.manager.SetMaxConcurrent(4)

	fast := fakeWithAlbum(1, "usenet-like", ".flac")
	fast.GrabGate = make(chan struct{})

	f.manager.installProvider(Config{
		ID:       1,
		Kind:     KindSABnzbd,
		Priority: 50,
	}, fast)

	wait := grabAll(t, f, fast, 3)

	waitFor(
		t,
		func() bool { return fast.MaxParallelGrabs() >= 3 },
		"transfers were serialized against a provider that allows parallelism",
	)

	close(fast.GrabGate)
	wait()
}

// Reload must not strand a running transfer's slot when a provider's
// limit changes underneath it.
func TestSyncSemaphoresReplacesChangedLimits(t *testing.T) {
	t.Parallel()

	f := newManagerFixture(t)

	f.manager.installProvider(Config{ID: 1, Kind: KindSlskd}, nil)

	first := f.manager.semaphoreFor(1)
	if cap(first) != 1 {
		t.Fatalf("slskd semaphore cap = %d, want 1", cap(first))
	}

	// Same limit: the semaphore is kept, so in-flight accounting is not
	// reset by an unrelated settings save.
	f.manager.syncSemaphores(map[int64]Config{1: {ID: 1, Kind: KindSlskd}})

	if again := f.manager.semaphoreFor(1); again != first {
		t.Error("semaphore was replaced despite an unchanged limit")
	}

	// Changed limit: a new semaphore, with the new capacity.
	f.manager.syncSemaphores(map[int64]Config{1: {
		ID:       1,
		Kind:     KindSlskd,
		Settings: map[string]string{concurrencyKey: "5"},
	}})

	f.manager.provMu.Lock()
	f.manager.configs[1] = Config{
		ID:       1,
		Kind:     KindSlskd,
		Settings: map[string]string{concurrencyKey: "5"},
	}
	f.manager.provMu.Unlock()

	if changed := f.manager.semaphoreFor(1); cap(changed) != 5 {
		t.Errorf("semaphore cap = %d after raising the limit, want 5", cap(changed))
	}

	// A provider that is gone leaves no semaphore behind.
	f.manager.syncSemaphores(map[int64]Config{})

	f.manager.semMu.Lock()
	_, still := f.manager.provSem[1]
	f.manager.semMu.Unlock()

	if still {
		t.Error("semaphore survived the provider being removed")
	}
}
