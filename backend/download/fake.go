package download

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// The fake provider exists so the pipeline can be tested end to end
// without a network, a daemon, or a binary on PATH.  It is registered
// like any real adapter and excluded from Descriptors(), so it can
// never be offered in the UI.

// FakeProvider is an in-memory Provider used by tests.  It fills all
// three roles; which ones are active is controlled by its Caps.
type FakeProvider struct {
	info ProviderInfo

	mu sync.Mutex

	// Candidates is what Search returns.
	Candidates []Candidate

	// SearchErr, GrabErr and CheckErr are returned when set.
	SearchErr error
	GrabErr   error
	CheckErr  error

	// Written maps a relative file name to the bytes Grab creates in
	// the staging directory, simulating a completed transfer.
	Written map[string][]byte

	// DelegateStatuses is returned by Poll in order, the last repeating.
	DelegateStatuses []DelegateStatus

	// GrabGate, when set, blocks Grab until it is closed or the
	// context ends.  It lets a test hold transfers open long enough to
	// observe how many run at once.
	GrabGate chan struct{}

	// concurrent tracks how many Grabs are in flight, and maxParallel
	// the high-water mark, which is what a concurrency cap is asserted
	// against.
	concurrent  int
	maxParallel int

	// Calls records what happened, for assertions.
	SearchCalls   int
	GrabCalls     int
	DelegateCalls int
	pollIndex     int
}

// NewFakeProvider returns a fake with the given capabilities.
func NewFakeProvider(id int64, name string, caps Caps) *FakeProvider {
	return &FakeProvider{
		info: ProviderInfo{
			ID:       id,
			Kind:     KindFake,
			Name:     name,
			Enabled:  true,
			Priority: 50,
			Caps:     caps,
		},
		Written: map[string][]byte{},
	}
}

// Info returns the fake's identity.
func (f *FakeProvider) Info() ProviderInfo {
	return f.info
}

// SetPriority adjusts the fake's priority.
func (f *FakeProvider) SetPriority(p int) {
	f.info.Priority = p
}

// Check reports configured health.
func (f *FakeProvider) Check(_ context.Context) error {
	return f.CheckErr
}

// Close is a no-op.
func (f *FakeProvider) Close() error {
	return nil
}

// Search returns the configured candidates.
func (f *FakeProvider) Search(
	ctx context.Context,
	_ Request,
) ([]Candidate, error) {
	f.mu.Lock()
	f.SearchCalls++
	f.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return nil, err //nolint:wrapcheck // context error, already meaningful
	}

	if f.SearchErr != nil {
		return nil, f.SearchErr
	}

	out := make([]Candidate, len(f.Candidates))
	copy(out, f.Candidates)

	for i := range out {
		out[i].ProviderID = f.info.ID
	}

	return out, nil
}

// Grab writes the configured files into dst.
func (f *FakeProvider) Grab(
	ctx context.Context,
	_ Candidate,
	dst string,
	onProgress ProgressFunc,
) (Result, error) {
	f.mu.Lock()
	f.GrabCalls++
	f.concurrent++

	if f.concurrent > f.maxParallel {
		f.maxParallel = f.concurrent
	}

	gate := f.GrabGate
	f.mu.Unlock()

	defer func() {
		f.mu.Lock()
		f.concurrent--
		f.mu.Unlock()
	}()

	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return Result{}, ctx.Err() //nolint:wrapcheck // context error
		}
	}

	if err := ctx.Err(); err != nil {
		return Result{}, err //nolint:wrapcheck // context error
	}

	if f.GrabErr != nil {
		return Result{}, f.GrabErr
	}

	files := make([]string, 0, len(f.Written))

	var total int64

	for name, data := range f.Written {
		path := filepath.Join(dst, name)

		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return Result{}, err //nolint:wrapcheck // test helper
		}

		if err := os.WriteFile(path, data, 0o640); err != nil {
			return Result{}, err //nolint:wrapcheck // test helper
		}

		files = append(files, path)
		total += int64(len(data))

		if onProgress != nil {
			onProgress(Progress{Current: total, Total: total})
		}
	}

	return Result{Dir: dst, Files: files, BytesTransferred: total}, nil
}

// GrabCallCount reports how many transfers have been started, safe to
// read while transfers are in flight.
func (f *FakeProvider) GrabCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.GrabCalls
}

// MaxParallelGrabs reports the most simultaneous transfers this
// provider ever saw, which is what a per-provider cap is asserted
// against.
func (f *FakeProvider) MaxParallelGrabs() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.maxParallel
}

// errNoDelegateStatus is returned when a fake delegator runs out of
// scripted statuses.
var errNoDelegateStatus = errors.New("fake: no delegate status configured")

// Delegate records the call and returns a fixed external ID.
func (f *FakeProvider) Delegate(
	_ context.Context,
	_ Request,
) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.DelegateCalls++

	return "fake-external-1", nil
}

// Poll returns the next scripted status, repeating the last.
func (f *FakeProvider) Poll(
	_ context.Context,
	_ string,
) (DelegateStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.DelegateStatuses) == 0 {
		return DelegateStatus{}, errNoDelegateStatus
	}

	i := f.pollIndex
	if i >= len(f.DelegateStatuses) {
		i = len(f.DelegateStatuses) - 1
	} else {
		f.pollIndex++
	}

	return f.DelegateStatuses[i], nil
}

// Withdraw is a no-op.
func (f *FakeProvider) Withdraw(_ context.Context, _ string) error {
	return nil
}

// fakeRegistry lets tests install providers directly, bypassing the
// database and the constructor registry.
func (m *Manager) installProvider(cfg Config, p Provider) {
	m.provMu.Lock()
	defer m.provMu.Unlock()

	m.providers[cfg.ID] = p
	m.configs[cfg.ID] = cfg
}

func init() {
	Register(
		Descriptor{
			Kind:    KindFake,
			Name:    "Fake (testing)",
			Summary: "In-memory provider used by the test suite.",
			Caps: Caps{
				CanSearch:    true,
				CanTransport: true,
			},
		},
		func(cfg Config, _ SecretLookup, _ *slog.Logger) (Provider, error) {
			return NewFakeProvider(cfg.ID, cfg.Name, Caps{
				CanSearch:    true,
				CanTransport: true,
			}), nil
		},
	)
}

// slogDiscard returns a logger that writes nowhere, for tests.
func slogDiscard() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError + 1,
	}))
}
