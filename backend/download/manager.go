package download

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"yellowjacket/backend/jobs"
)

// Manager owns the download pipeline: it builds providers from stored
// config, fans a request out across them, ranks what comes back, drives
// the chosen candidate through grab → verify → tag → import, and
// reports the whole thing as one job.
//
// One job per request, not per file.  The user asked for an album; the
// fact that it arrives as twelve transfers is an implementation detail
// they should not have to read a job list to understand.

// Timeouts and limits.
const (
	// searchTimeout bounds one provider's search.  The fan-out takes
	// whatever returned in time rather than blocking on the slowest —
	// a wedged Prowlarr indexer must not stall a Soulseek result that
	// arrived in 200ms.
	searchTimeout = 25 * time.Second

	// grabTimeout bounds one transfer.  Soulseek queues are measured in
	// hours when a peer is busy, so this is generous by design.
	grabTimeout = 6 * time.Hour

	// pollInterval is how often delegating managers are asked for
	// status.
	pollInterval = 15 * time.Second

	// delegateTimeout bounds how long we wait for a delegate to finish
	// before giving up and telling the user to check that system.
	delegateTimeout = 12 * time.Hour

	// defaultConcurrency bounds simultaneous grabs across all
	// providers.  Soulseek peers queue or ban on parallel requests, so
	// the default is deliberately low.
	defaultConcurrency = 2
)

// concurrencyKey is the per-provider setting that overrides its kind's
// default transfer limit.
const concurrencyKey = "maxConcurrent"

// kindConcurrency is the default number of simultaneous transfers each
// provider kind will tolerate.
//
// A single global cap is the wrong shape here: usenet and torrent
// clients are built to run many transfers at once and are throttled by
// bandwidth, while Soulseek transfers come from one person's home
// upload slot.  Hitting the same peer with parallel requests gets you
// queued behind everyone else at best and banned at worst, so slskd is
// capped at one — the polite number, and the one that actually
// completes fastest, because a Soulseek peer serves one file at a time
// regardless of how many you ask for.
var kindConcurrency = map[Kind]int{
	KindSlskd:       1,
	KindYtDlp:       2,
	KindQBittorrent: 4,
	KindSABnzbd:     4,
	KindProwlarr:    4,
	KindLidarr:      4,
	KindFake:        4,
}

// concurrencyFor returns a provider's transfer limit: its configured
// override, else its kind's default, else the global default.
func concurrencyFor(cfg Config) int {
	if raw, ok := cfg.Settings[concurrencyKey]; ok && raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}

	if n, ok := kindConcurrency[cfg.Kind]; ok {
		return n
	}

	return defaultConcurrency
}

// Manager errors.
var (
	// ErrNoProviders means nothing is configured and enabled.
	ErrNoProviders = errors.New("no download providers are enabled")

	// ErrNoCandidates means every provider searched and found nothing.
	ErrNoCandidates = errors.New("no candidates found")

	// ErrCandidateGone means the chosen candidate is no longer in the
	// request's result set — usually a stale UI.
	ErrCandidateGone = errors.New("candidate is no longer available")

	// ErrDelegateFailed means an external manager reported that it
	// could not fulfil the request.
	ErrDelegateFailed = errors.New("delegate reported failure")
)

// Manager coordinates the download subsystem.
type Manager struct {
	logger   *slog.Logger
	store    *Store
	secrets  SecretStore
	staging  *Staging
	importer *Importer
	library  LibraryPort

	// jobsReg is optional; without it downloads still work but do not
	// appear in the background jobs panel.
	jobsReg *jobs.Registry

	// opts describes the library layout imports follow.
	optsMu sync.RWMutex
	opts   ImportOptions

	// providers caches built provider instances by config ID.  Rebuilt
	// whenever config changes, so a settings edit takes effect without
	// a restart.
	provMu    sync.RWMutex
	providers map[int64]Provider
	configs   map[int64]Config

	// results holds the ranked candidates of live requests, so the
	// picker can be reopened without re-searching.
	resMu   sync.RWMutex
	results map[string][]Candidate

	// active tracks cancel functions for in-flight requests.
	actMu  sync.Mutex
	active map[string]context.CancelFunc

	// sem bounds concurrent grabs across every provider.
	sem chan struct{}

	// provSem bounds concurrent grabs per transporting provider,
	// rebuilt on Reload alongside the providers themselves.  A grab
	// takes its provider's slot before the global one, so a queue on a
	// busy Soulseek daemon cannot sit on a global slot that a usenet
	// transfer could have used.
	semMu   sync.Mutex
	provSem map[int64]chan struct{}

	// delegatePoll is how often delegating managers are asked for
	// status.  A field rather than the constant so tests can drive the
	// full delegate flow without sleeping through it.
	delegatePoll time.Duration
}

// NewManager builds a download manager.  Providers are not constructed
// until Reload is called, so a manager can be created before the Wails
// runtime exists.
func NewManager(
	logger *slog.Logger,
	store *Store,
	secrets SecretStore,
	staging *Staging,
	importer *Importer,
	library LibraryPort,
) *Manager {
	return &Manager{
		logger:       logger,
		store:        store,
		secrets:      secrets,
		staging:      staging,
		importer:     importer,
		library:      library,
		providers:    map[int64]Provider{},
		configs:      map[int64]Config{},
		results:      map[string][]Candidate{},
		active:       map[string]context.CancelFunc{},
		sem:          make(chan struct{}, defaultConcurrency),
		provSem:      map[int64]chan struct{}{},
		delegatePoll: pollInterval,
	}
}

// SetJobRegistry wires the background jobs panel.
func (m *Manager) SetJobRegistry(reg *jobs.Registry) {
	m.jobsReg = reg
}

// SetImportOptions configures the library layout imports follow.
func (m *Manager) SetImportOptions(opts ImportOptions) {
	m.optsMu.Lock()
	defer m.optsMu.Unlock()

	m.opts = opts
}

// importOptions returns the current layout options.
func (m *Manager) importOptions() ImportOptions {
	m.optsMu.RLock()
	defer m.optsMu.RUnlock()

	return m.opts
}

// Reload rebuilds every provider from stored config.  Called at startup
// and after any provider settings change.
//
// A provider that fails to build is logged and skipped rather than
// failing the reload: one misconfigured client must not disable the
// others.
func (m *Manager) Reload(ctx context.Context) error {
	configs, err := m.store.ListProviders(ctx)
	if err != nil {
		return err
	}

	built := make(map[int64]Provider, len(configs))
	kept := make(map[int64]Config, len(configs))

	for _, cfg := range configs {
		kept[cfg.ID] = cfg

		if !cfg.Enabled {
			continue
		}

		p, err := New(cfg, lookupFor(m.secrets, cfg.ID), m.logger)
		if err != nil {
			m.logger.Warn(
				"could not build download provider",
				"provider", cfg.Name,
				"kind", cfg.Kind,
				"error", err,
			)

			continue
		}

		built[cfg.ID] = p
	}

	m.provMu.Lock()
	old := m.providers
	m.providers = built
	m.configs = kept
	m.provMu.Unlock()

	m.syncSemaphores(kept)

	for id, p := range old {
		if _, reused := built[id]; reused {
			continue
		}

		if err := p.Close(); err != nil {
			m.logger.Debug(
				"error closing replaced provider", "id", id, "error", err,
			)
		}
	}

	return nil
}

// Sweep cleans staging directories left by a previous run.  Called at
// startup after the store is available.
func (m *Manager) Sweep(ctx context.Context) {
	live, err := m.store.ListLiveItems(ctx)
	if err != nil {
		m.logger.Warn("could not list live download items", "error", err)

		return
	}

	// Anything the database still thinks is live cannot be resumed: the
	// transports do not survive a restart.  Mark them failed so the UI
	// does not show a phantom transfer, then let staging be swept.
	liveIDs := make(map[string]bool, len(live))

	for _, item := range live {
		liveIDs[item.ID] = true

		if err := m.store.SetItemState(
			ctx, item.ID, StateFailed, "interrupted by restart",
		); err != nil {
			m.logger.Warn(
				"could not fail interrupted download item",
				"item", item.ID, "error", err,
			)
		}

		if err := m.store.SetRequestState(
			ctx, item.RequestID, StateFailed, "interrupted by restart",
		); err != nil {
			m.logger.Warn(
				"could not fail interrupted download request",
				"request", item.RequestID, "error", err,
			)
		}
	}

	if _, err := m.staging.Sweep(); err != nil {
		m.logger.Warn("could not sweep staging directory", "error", err)
	}

	if _, err := m.staging.SweepOrphans(map[string]bool{}); err != nil {
		m.logger.Warn("could not sweep orphaned staging dirs", "error", err)
	}
}

// enabledProviders returns a snapshot of built providers with their
// configs.
func (m *Manager) enabledProviders() map[int64]Provider {
	m.provMu.RLock()
	defer m.provMu.RUnlock()

	out := make(map[int64]Provider, len(m.providers))
	for id, p := range m.providers {
		out[id] = p
	}

	return out
}

// SetMaxConcurrent sets the global transfer limit.  Called once at
// startup from the user's config; a change takes effect for transfers
// that start afterwards, since a transfer already running holds a slot
// in the semaphore it acquired.
func (m *Manager) SetMaxConcurrent(n int) {
	if n <= 0 {
		n = defaultConcurrency
	}

	m.semMu.Lock()
	defer m.semMu.Unlock()

	m.sem = make(chan struct{}, n)
}

// globalSem returns the current global semaphore.  Callers must hold on
// to what they get: releasing into a semaphore that was replaced in the
// meantime would return a slot to the wrong pool.
func (m *Manager) globalSem() chan struct{} {
	m.semMu.Lock()
	defer m.semMu.Unlock()

	return m.sem
}

// semaphoreFor returns a provider's own transfer semaphore, creating it
// on first use from that provider's configured or default limit.
func (m *Manager) semaphoreFor(id int64) chan struct{} {
	m.provMu.RLock()
	cfg, known := m.configs[id]
	m.provMu.RUnlock()

	m.semMu.Lock()
	defer m.semMu.Unlock()

	if sem, ok := m.provSem[id]; ok {
		return sem
	}

	limit := defaultConcurrency
	if known {
		limit = concurrencyFor(cfg)
	}

	sem := make(chan struct{}, limit)
	m.provSem[id] = sem

	return sem
}

// syncSemaphores drops semaphores for providers that no longer exist
// and for providers whose limit changed.  Transfers already holding a
// slot keep their own reference to the old channel, so replacing the
// map entry cannot strand them; it only means the new limit applies
// from the next transfer on.
func (m *Manager) syncSemaphores(configs map[int64]Config) {
	m.semMu.Lock()
	defer m.semMu.Unlock()

	for id, sem := range m.provSem {
		cfg, ok := configs[id]
		if !ok {
			delete(m.provSem, id)

			continue
		}

		if cap(sem) != concurrencyFor(cfg) {
			delete(m.provSem, id)
		}
	}
}

// listers returns every enabled provider that keeps a persistent wanted
// list of its own, keyed by provider ID.
func (m *Manager) listers() map[int64]Lister {
	m.provMu.RLock()
	defer m.provMu.RUnlock()

	out := map[int64]Lister{}

	for id, p := range m.providers {
		if l, ok := asLister(p); ok {
			out[id] = l
		}
	}

	return out
}

// priorityFor returns a provider's configured priority.
func (m *Manager) priorityFor(id int64) int {
	m.provMu.RLock()
	defer m.provMu.RUnlock()

	if cfg, ok := m.configs[id]; ok {
		return cfg.Priority
	}

	return 50
}

// Search fans a request out across every enabled searching provider and
// returns ranked candidates.  Providers are searched concurrently with
// a per-provider timeout; a provider that errors or times out is logged
// and skipped, because partial results beat no results.
func (m *Manager) Search(
	ctx context.Context,
	req Request,
) ([]Candidate, error) {
	providers := m.enabledProviders()
	if len(providers) == 0 {
		return nil, ErrNoProviders
	}

	type found struct {
		candidates []Candidate
		err        error
		id         int64
	}

	results := make(chan found)
	searched := 0

	for id, p := range providers {
		s, ok := asSearcher(p)
		if !ok {
			continue
		}

		searched++

		go func(id int64, s Searcher) {
			sctx, cancel := context.WithTimeout(ctx, searchTimeout)
			defer cancel()

			c, err := s.Search(sctx, req)
			results <- found{candidates: c, err: err, id: id}
		}(id, s)
	}

	if searched == 0 {
		return nil, fmt.Errorf("%w: none can search", ErrNoProviders)
	}

	all := make([]Candidate, 0, searched*8)

	for range searched {
		r := <-results

		if r.err != nil {
			m.logger.Warn(
				"download provider search failed",
				"provider", r.id,
				"error", r.err,
			)

			continue
		}

		for i := range r.candidates {
			r.candidates[i].ProviderID = r.id
		}

		all = append(all, r.candidates...)
	}

	if len(all) == 0 {
		return nil, ErrNoCandidates
	}

	return Rank(req, all, m.priorityFor), nil
}

// Start creates a request, searches for it, and either grabs the clear
// winner automatically or parks the ranked list for the user to pick
// from.  It returns as soon as the search completes; the transfer runs
// in the background under a job.
func (m *Manager) Start(
	ctx context.Context,
	req Request,
) ([]Candidate, error) {
	if req.ID == "" {
		req.ID = newID()
	}

	if err := m.store.CreateRequest(ctx, req); err != nil {
		return nil, err
	}

	job := m.startJob(req)

	ranked, err := m.Search(ctx, req)
	if err != nil {
		m.failRequest(ctx, job, req.ID, err)

		return nil, err
	}

	m.resMu.Lock()
	m.results[req.ID] = ranked
	m.resMu.Unlock()

	if err := m.store.SetRequestState(
		ctx, req.ID, StateFound, "",
	); err != nil {
		m.logger.Warn("could not record found state", "error", err)
	}

	if job != nil {
		job.Logf(jobs.LevelInfo, fmt.Sprintf(
			"Found %d candidates across enabled providers", len(ranked),
		))
	}

	if AutoPickable(req, ranked) {
		if job != nil {
			job.Logf(jobs.LevelInfo, "Auto-selected best candidate")
		}

		go m.grab(context.WithoutCancel(ctx), req, ranked[0], job)

		return ranked, nil
	}

	if job != nil {
		job.SetPhase("Waiting for you to pick")
		job.SetState(jobs.StatePaused)
	}

	return ranked, nil
}

// Attempt searches on behalf of the wanted list and starts a download
// only if there is a clear winner.  It returns whether it started and,
// when it did not, a sentence the wanted list can show the user.
//
// Unlike Start it persists nothing when it does not act.  A want that
// is retried weekly for a year would otherwise leave fifty failed
// request rows behind it, all saying the same thing the want itself
// already says — and none of them anything the user can do something
// about.  Nobody is watching a reconcile pass, so the only two honest
// outcomes are "downloading it now" and "still looking".
func (m *Manager) Attempt(
	ctx context.Context,
	req Request,
) (bool, string, error) {
	if req.ID == "" {
		req.ID = newID()
	}

	ranked, err := m.Search(ctx, req)
	if err != nil {
		return false, "", err
	}

	if !AutoPickable(req, ranked) {
		best := ranked[0]

		return false, fmt.Sprintf(
			"best of %d found is not a confident enough match "+
				"(match %.0f%%, quality %.0f%%)",
			len(ranked),
			best.Match.Overall*100, //nolint:mnd // percent
			best.Quality.Overall*100,
		), nil
	}

	if err := m.store.CreateRequest(ctx, req); err != nil {
		return false, "", err
	}

	m.resMu.Lock()
	m.results[req.ID] = ranked
	m.resMu.Unlock()

	if err := m.store.SetRequestState(ctx, req.ID, StateFound, ""); err != nil {
		m.logger.Warn("could not record found state", "error", err)
	}

	job := m.startJob(req)

	if job != nil {
		job.Logf(jobs.LevelInfo, fmt.Sprintf(
			"Wanted list: auto-selected the best of %d candidates",
			len(ranked),
		))
	}

	go m.grab(context.WithoutCancel(ctx), req, ranked[0], job)

	return true, "", nil
}

// Pick starts the transfer for a candidate the user chose.
func (m *Manager) Pick(
	ctx context.Context,
	requestID, candidateID string,
) error {
	req, err := m.store.GetRequest(ctx, requestID)
	if err != nil {
		return err
	}

	m.resMu.RLock()
	ranked := m.results[requestID]
	m.resMu.RUnlock()

	var chosen *Candidate

	for i := range ranked {
		if ranked[i].ID == candidateID {
			chosen = &ranked[i]

			break
		}
	}

	if chosen == nil {
		return fmt.Errorf("%w: %s", ErrCandidateGone, candidateID)
	}

	job := m.startJob(req)

	go m.grab(context.WithoutCancel(ctx), req, *chosen, job)

	return nil
}

// Cancel aborts a live request.
func (m *Manager) Cancel(ctx context.Context, requestID string) error {
	m.actMu.Lock()
	cancel, ok := m.active[requestID]
	m.actMu.Unlock()

	if ok {
		cancel()
	}

	if err := m.store.SetRequestState(
		ctx, requestID, StateCancelled, "",
	); err != nil {
		return err
	}

	return nil
}

// grab drives one candidate all the way to the library.  It runs on its
// own goroutine and owns the job from here on.
func (m *Manager) grab(
	ctx context.Context,
	req Request,
	c Candidate,
	job *jobs.Handle,
) {
	ctx, cancel := context.WithTimeout(ctx, grabTimeout)
	defer cancel()

	m.actMu.Lock()
	m.active[req.ID] = cancel
	m.actMu.Unlock()

	defer func() {
		m.actMu.Lock()
		delete(m.active, req.ID)
		m.actMu.Unlock()
	}()

	// Who will move the bytes is decided before any slot is taken, so
	// the transfer waits in its own provider's queue rather than in a
	// global one.  A delegate takes no slot at all: the transfer is
	// happening inside another system, which is doing its own limiting,
	// and blocking a local slot on it would be counting someone else's
	// work against our budget.
	plan, err := m.planTransfer(req, c)
	if err != nil {
		m.failRequest(ctx, job, req.ID, err)

		return
	}

	if !plan.delegated() {
		provSem := m.semaphoreFor(plan.transportID)

		select {
		case provSem <- struct{}{}:
			defer func() { <-provSem }()
		case <-ctx.Done():
			m.failRequest(ctx, job, req.ID, ctx.Err())

			return
		}

		globalSem := m.globalSem()

		select {
		case globalSem <- struct{}{}:
			defer func() { <-globalSem }()
		case <-ctx.Done():
			m.failRequest(ctx, job, req.ID, ctx.Err())

			return
		}
	}

	item := Item{
		ID:         newID(),
		RequestID:  req.ID,
		ProviderID: c.ProviderID,
		Candidate:  c,
		State:      StateQueued,
		BytesTotal: c.TotalSize,
	}

	dir, err := m.staging.Reserve(item.ID)
	if err != nil {
		m.failRequest(ctx, job, req.ID, err)

		return
	}

	item.StagingDir = dir

	if err := m.store.CreateItem(ctx, item); err != nil {
		m.failRequest(ctx, job, req.ID, err)

		return
	}

	result, err := m.transfer(ctx, req, item, plan, job)
	if err != nil {
		m.failItem(ctx, job, item, req.ID, err)

		return
	}

	m.setStates(ctx, req.ID, item.ID, StateImporting)

	if job != nil {
		job.SetPhase("Importing")
		job.SetStages(importStages(2))
	}

	var imported ImportResult

	if result.Delegated {
		// The external manager already placed and tagged these files in
		// its own library.  Moving them out from under a system that is
		// still managing them would be worse than useless, so the files
		// are recorded where they are and the library scan picks them
		// up in place.
		imported = ImportResult{Paths: result.Files}

		if job != nil {
			job.Logf(jobs.LevelInfo, fmt.Sprintf(
				"External manager imported %d files; recording them in place",
				len(result.Files),
			))
		}
	} else {
		opts := m.importOptions()
		opts.WriteTags = true

		imported, err = m.importer.Import(ctx, req, result, opts)
		if err != nil {
			m.failItem(ctx, job, item, req.ID, err)

			return
		}
	}

	if err := m.store.SetItemImported(
		ctx, item.ID, imported.Paths,
	); err != nil {
		m.logger.Warn("could not record imported paths", "error", err)
	}

	if err := m.store.SetRequestState(
		ctx, req.ID, StateComplete, "",
	); err != nil {
		m.logger.Warn("could not record complete state", "error", err)
	}

	// A request raised from the wanted list retires its want here
	// rather than waiting for the next reconcile pass to notice the
	// files, so the wanted list is right the moment the download
	// finishes.  The pass would reach the same conclusion by asking the
	// library; this is the same answer, sooner.
	if req.WantID != 0 {
		if err := m.store.SatisfyWant(ctx, req.WantID); err != nil {
			m.logger.Warn(
				"could not satisfy want", "want", req.WantID, "error", err,
			)
		}
	}

	// The ranked list only existed so the picker could be reopened
	// mid-flight.  Holding it after the download completes would leak a
	// few hundred candidates per request for the life of the process.
	m.resMu.Lock()
	delete(m.results, req.ID)
	m.resMu.Unlock()

	// Staging is only released on a fully successful import; a failure
	// leaves the files for retry or inspection.
	if err := m.staging.Release(item.StagingDir); err != nil {
		m.logger.Warn("could not release staging dir", "error", err)
	}

	if m.library != nil {
		if err := m.library.ScanLibrary(req.LibraryID); err != nil {
			m.logger.Warn(
				"could not trigger scan after import",
				"library", req.LibraryID,
				"error", err,
			)
		}
	}

	if job != nil {
		job.SetStats([]jobs.Stat{
			{Label: "Imported", Value: itoa(len(imported.Paths))},
			{Label: "Tagged", Value: itoa(imported.Tagged)},
		})
		job.Logf(jobs.LevelInfo, fmt.Sprintf(
			"Imported %d files into the library", len(imported.Paths),
		))
		job.Complete()
	}
}

// transfer moves the bytes, dispatching on whether the candidate's
// provider fetches its own results, needs a separate transport, or
// delegates the whole thing.
func (m *Manager) transfer(
	ctx context.Context,
	req Request,
	item Item,
	plan transferPlan,
	job *jobs.Handle,
) (Result, error) {
	if plan.delegated() {
		return m.delegate(ctx, req, item, plan.delegate, job)
	}

	m.setStates(ctx, req.ID, item.ID, StateGrabbing)

	if job != nil {
		job.SetPhase("Downloading")
		job.SetProgress(0, item.Candidate.TotalSize)
	}

	onProgress := m.progressReporter(ctx, item.ID, job)

	result, err := plan.transport.Grab(
		ctx, item.Candidate, item.StagingDir, onProgress,
	)
	if err != nil {
		return Result{}, fmt.Errorf("grab failed: %w", err)
	}

	m.setStates(ctx, req.ID, item.ID, StateVerifying)

	if job != nil {
		job.SetPhase("Verifying")
	}

	return result, nil
}

// transportFor picks the transport that will fetch a candidate: the
// finding provider itself when it can, otherwise the highest-priority
// enabled provider that handles the candidate's protocol.
func (m *Manager) transportFor(
	providers map[int64]Provider,
	sourceID int64,
	source Provider,
	c Candidate,
) (Transporter, int64, error) {
	if c.Protocol == ProtocolDirect {
		t, ok := asTransporter(source)
		if !ok {
			return nil, 0, fmt.Errorf(
				"%w: %s cannot fetch its own results",
				ErrUnsupported, source.Info().Kind,
			)
		}

		return t, sourceID, nil
	}

	var (
		best     Transporter
		bestID   int64
		bestPrio = -1
	)

	for id, p := range providers {
		t, ok := asTransporter(p)
		if !ok || !p.Info().Caps.Handles(c.Protocol) {
			continue
		}

		if prio := m.priorityFor(id); prio > bestPrio {
			best, bestID, bestPrio = t, id, prio
		}
	}

	if best == nil {
		return nil, 0, fmt.Errorf("%w: %s", ErrNoTransport, c.Protocol)
	}

	return best, bestID, nil
}

// transferPlan is who will move a candidate's bytes, resolved before
// any concurrency slot is taken so a transfer queues against the
// provider that will actually do the work.
type transferPlan struct {
	// delegate is set when an external manager owns the whole transfer.
	delegate Delegator

	// transport and transportID are set otherwise.
	transport   Transporter
	transportID int64
}

// delegated reports whether this plan hands the work to another system.
func (p transferPlan) delegated() bool { return p.delegate != nil }

// planTransfer decides how a candidate will be fetched.
func (m *Manager) planTransfer(_ Request, c Candidate) (transferPlan, error) {
	providers := m.enabledProviders()

	source, ok := providers[c.ProviderID]
	if !ok {
		return transferPlan{}, fmt.Errorf(
			"%w: provider %d", ErrNotConfigured, c.ProviderID,
		)
	}

	if d, ok := asDelegator(source); ok {
		return transferPlan{delegate: d}, nil
	}

	transport, id, err := m.transportFor(providers, c.ProviderID, source, c)
	if err != nil {
		return transferPlan{}, err
	}

	return transferPlan{transport: transport, transportID: id}, nil
}

// delegate hands the request to an external manager and polls until it
// reports terminal state.
func (m *Manager) delegate(
	ctx context.Context,
	req Request,
	item Item,
	d Delegator,
	job *jobs.Handle,
) (Result, error) {
	externalID, err := d.Delegate(ctx, req)
	if err != nil {
		return Result{}, fmt.Errorf("delegate request: %w", err)
	}

	if err := m.store.SetItemExternalID(ctx, item.ID, externalID); err != nil {
		m.logger.Warn("could not record external id", "error", err)
	}

	m.setStates(ctx, req.ID, item.ID, StateGrabbing)

	if job != nil {
		job.SetPhase("Waiting on external manager")
		job.Logf(jobs.LevelInfo, "Handed request to "+string(item.Candidate.Kind))
	}

	ctx, cancel := context.WithTimeout(ctx, delegateTimeout)
	defer cancel()

	ticker := time.NewTicker(m.delegatePoll)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			_ = d.Withdraw(context.WithoutCancel(ctx), externalID)

			return Result{}, fmt.Errorf("delegate timed out: %w", ctx.Err())
		case <-ticker.C:
		}

		status, err := d.Poll(ctx, externalID)
		if err != nil {
			m.logger.Warn("delegate poll failed", "error", err)

			continue
		}

		if job != nil && status.Progress >= 0 {
			job.SetProgress(int64(status.Progress*100), 100)
		}

		switch status.State {
		case StateComplete:
			// The manager placed the files itself, so there is nothing
			// in staging and nothing for us to move.  Report its paths
			// so the item records what landed where.
			return Result{
				Files:     status.ImportedPaths,
				Delegated: true,
			}, nil
		case StateFailed, StateCancelled:
			return Result{}, fmt.Errorf(
				"%w: %s: %s", ErrDelegateFailed, status.State, status.Message,
			)
		case StateSearching, StateFound, StateQueued, StateGrabbing,
			StateVerifying, StateTagging, StateImporting:
			// Still working.
		}
	}
}

// progressReporter returns a throttled ProgressFunc that updates both
// the job and the stored item.  Transports call this per chunk, so it
// must be cheap: the job registry already coalesces, but a database
// write per chunk would not survive contact with a fast transfer.
func (m *Manager) progressReporter(
	ctx context.Context,
	itemID string,
	job *jobs.Handle,
) ProgressFunc {
	const dbInterval = 3 * time.Second

	var (
		mu       sync.Mutex
		lastSave time.Time
	)

	return func(p Progress) {
		if job != nil {
			job.SetProgress(p.Current, p.Total)

			if p.Phase != "" {
				job.SetPhase(p.Phase)
			}
		}

		mu.Lock()

		if time.Since(lastSave) < dbInterval {
			mu.Unlock()

			return
		}

		lastSave = time.Now()
		mu.Unlock()

		if err := m.store.SetItemProgress(
			ctx, itemID, p.Current, p.Total,
		); err != nil {
			m.logger.Debug("could not save item progress", "error", err)
		}
	}
}

// Candidates returns the ranked candidates for a live request.
func (m *Manager) Candidates(requestID string) []Candidate {
	m.resMu.RLock()
	defer m.resMu.RUnlock()

	out := make([]Candidate, len(m.results[requestID]))
	copy(out, m.results[requestID])

	return out
}

// setStates advances a request and its item together.
func (m *Manager) setStates(
	ctx context.Context,
	requestID, itemID string,
	state State,
) {
	if err := m.store.SetRequestState(ctx, requestID, state, ""); err != nil {
		m.logger.Warn("could not set request state", "error", err)
	}

	if err := m.store.SetItemState(ctx, itemID, state, ""); err != nil {
		m.logger.Warn("could not set item state", "error", err)
	}
}

// failRequest records a request-level failure.
func (m *Manager) failRequest(
	ctx context.Context,
	job *jobs.Handle,
	requestID string,
	err error,
) {
	m.logger.Warn("download request failed", "request", requestID, "error", err)

	if serr := m.store.SetRequestState(
		ctx, requestID, StateFailed, err.Error(),
	); serr != nil {
		m.logger.Warn("could not record failure", "error", serr)
	}

	if job != nil {
		job.Fail(err)
	}
}

// failItem records an item-level failure and fails its request.
func (m *Manager) failItem(
	ctx context.Context,
	job *jobs.Handle,
	item Item,
	requestID string,
	err error,
) {
	if serr := m.store.SetItemState(
		ctx, item.ID, StateFailed, err.Error(),
	); serr != nil {
		m.logger.Warn("could not record item failure", "error", serr)
	}

	m.failRequest(ctx, job, requestID, err)
}

// startJob registers the request in the background jobs panel.
func (m *Manager) startJob(req Request) *jobs.Handle {
	if m.jobsReg == nil {
		return nil
	}

	title := req.Album
	if title == "" {
		title = req.SearchText()
	}

	return m.jobsReg.Start(jobs.Spec{
		ID:       "download-" + req.ID,
		Kind:     jobs.KindDownload,
		Title:    "Downloading " + title,
		Subtitle: req.Artist,
		State:    jobs.StateRunning,
		Caps:     jobs.Caps{Cancellable: true},
		Controls: jobs.Controls{
			Cancel: func() {
				if err := m.Cancel(context.Background(), req.ID); err != nil {
					m.logger.Warn("cancel failed", "error", err)
				}
			},
		},
	})
}

// importStages renders the pipeline tail as job stages.
func importStages(done int) []jobs.Stage {
	names := []string{"Search", "Download", "Import"}
	out := make([]jobs.Stage, 0, len(names))

	for i, n := range names {
		state := "pending"

		switch {
		case i < done:
			state = "complete"
		case i == done:
			state = "running"
		}

		out = append(out, jobs.Stage{Name: n, State: state})
	}

	return out
}

// newID returns a random identifier for a request or item.
func newID() string {
	var b [12]byte

	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failing means the system is in a state where a
		// timestamp fallback is the least of anyone's problems, but a
		// collision here would silently merge two downloads.
		return "dl-" + time.Now().Format("20060102150405.000000000")
	}

	return hex.EncodeToString(b[:])
}

// itoa formats an int for job stats.
func itoa(n int) string {
	return strconv.Itoa(n)
}
