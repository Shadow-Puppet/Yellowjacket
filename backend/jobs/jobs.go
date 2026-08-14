// Package jobs provides a central registry for long-running background
// work — library scans, search index builds, and anything else that runs
// while the user is doing something else.  Producers report progress
// through a Handle; the registry coalesces those updates into a single
// JobsChanged event so the frontend can render one indicator, one job
// list, and one log viewer regardless of which subsystem is working.
package jobs

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"yellowjacket/backend/events"
)

// Kind identifies the subsystem that owns a job.  The frontend uses it
// to pick an icon and to route "view details" to the right panel.
type Kind string

// Job kinds.
const (
	KindLibraryScan  Kind = "library-scan"
	KindIndexBuild   Kind = "index-build"
	KindDownload     Kind = "download"
	KindAutotagApply Kind = "autotag-apply"

	// KindCatalogEnrich is background catalog work for content the user
	// already owns — the discography backfills.  It is distinct from
	// KindIndexBuild because the two differ in what cancelling costs:
	// an index build discards hours of downloading and the frontend
	// confirms before stopping one, where a backfill is resumable per
	// artist and stopping it is free.
	KindCatalogEnrich Kind = "catalog-enrich"
)

// State is the lifecycle position of a job.
type State string

// Job states.  Queued, Running, Paused and Pausing are live; Complete,
// Cancelled and Error are terminal.
const (
	StateQueued     State = "queued"
	StateRunning    State = "running"
	StatePausing    State = "pausing"
	StatePaused     State = "paused"
	StateCancelling State = "cancelling"
	StateComplete   State = "complete"
	StateCancelled  State = "cancelled"
	StateError      State = "error"
)

// IsTerminal reports whether the state means the job will not progress
// further without being started again from scratch.
func (s State) IsTerminal() bool {
	return s == StateComplete || s == StateCancelled || s == StateError
}

// Level is the severity of a job log entry.
type Level string

// Log levels.
const (
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// maxLogEntries bounds the per-job log ring buffer.  Scans can emit a
// warning per unreadable file, so the buffer is a tail, not an archive.
const maxLogEntries = 500

// emitInterval is how often a dirty registry is flushed to the frontend.
// Progress tickers run at 300ms, so this keeps re-render cost bounded
// without making the UI feel laggy.
const emitInterval = 250 * time.Millisecond

// finishedRetention is how long terminal jobs stay in the registry so
// the user can read their logs after the fact.
const finishedRetention = 30 * time.Minute

// maxFinished caps how many terminal jobs are retained regardless of age.
const maxFinished = 25

// Caps describes which controls a job supports.  The frontend renders
// buttons from these rather than switching on Kind, so a job that gains
// pause support later needs no frontend change.
type Caps struct {
	Pausable    bool `json:"pausable"`
	Cancellable bool `json:"cancellable"`
}

// Stage is one named sub-step of a multi-stage job, such as an index
// build tier.  Jobs with a single linear phase leave Stages empty.
type Stage struct {
	Name    string `json:"name"`
	State   string `json:"state"` // pending, running, complete, error, skipped
	Current int64  `json:"current"`
	Total   int64  `json:"total"`
	Error   string `json:"error,omitempty"`
}

// Stat is a display-only key/value pair shown in the job detail panel
// (e.g. "Added" / "1,204").
type Stat struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// LogEntry is one line of a job's output log.
type LogEntry struct {
	Time    int64  `json:"time"` // unix milliseconds
	Level   Level  `json:"level"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

// Job is the frontend-facing snapshot of a single background job.
type Job struct {
	ID       string `json:"id"`
	Kind     Kind   `json:"kind"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle,omitempty"`

	State State  `json:"state"`
	Phase string `json:"phase,omitempty"`

	// Current/Total drive the progress bar.  Total == 0 means the job
	// is indeterminate and the frontend should render a spinner.
	Current int64 `json:"current"`
	Total   int64 `json:"total"`

	Caps   Caps    `json:"caps"`
	Stages []Stage `json:"stages"`
	Stats  []Stat  `json:"stats"`
	Error  string  `json:"error,omitempty"`

	StartedAt int64 `json:"startedAt"` // unix milliseconds
	UpdatedAt int64 `json:"updatedAt"`
	EndedAt   int64 `json:"endedAt,omitempty"`

	LogCount   int `json:"logCount"`
	WarnCount  int `json:"warnCount"`
	ErrorCount int `json:"errorCount"`
}

// Controls holds the callbacks the registry invokes when the user asks
// for a job to be paused, resumed, or cancelled.  All three are optional;
// a nil callback means the corresponding capability is unavailable.
//
// Callbacks are invoked on a dedicated goroutine, so implementations may
// block (StopBuild waits for its build goroutine to exit, for instance)
// without stalling the Wails call dispatcher.
type Controls struct {
	Pause  func()
	Resume func()
	Cancel func()
}

// Spec describes a job at registration time.
type Spec struct {
	ID       string
	Kind     Kind
	Title    string
	Subtitle string
	Total    int64
	State    State
	Caps     Caps
	Controls Controls

	// Durable marks a job whose paused state should survive an app
	// restart.  On the next launch the owning subsystem adopts it back
	// into the registry as paused instead of silently resuming.
	Durable bool
}

// Handle is a producer's write side of a registered job.  Every mutator
// marks the registry dirty; the emitter coalesces those into one event.
type Handle struct {
	reg *Registry
	id  string

	mu       sync.Mutex
	job      Job
	controls Controls
	durable  bool
	log      []LogEntry
}

// Registry owns every known job and pushes coalesced snapshots to the
// frontend.  It is safe for concurrent use.
type Registry struct {
	logger *slog.Logger
	store  *Store

	mu    sync.RWMutex
	ctx   context.Context
	jobs  map[string]*Handle
	order []string

	dirty atomic.Bool
}

// NewRegistry creates a registry.  Pass a nil store to disable
// pause-across-restart persistence (tests do this).
func NewRegistry(logger *slog.Logger, store *Store) *Registry {
	return &Registry{
		logger: logger,
		store:  store,
		jobs:   make(map[string]*Handle),
	}
}

// SetContext injects the Wails runtime context and starts the coalescing
// emitter.  Until it is called, updates are recorded but not pushed.
func (r *Registry) SetContext(ctx context.Context) {
	r.mu.Lock()
	r.ctx = ctx
	r.mu.Unlock()

	go r.emitLoop(ctx)
}

// emitLoop flushes the registry to the frontend whenever it is dirty.
func (r *Registry) emitLoop(ctx context.Context) {
	ticker := time.NewTicker(emitInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if r.dirty.Swap(false) {
				r.emit()
			}
		}
	}
}

// emit pushes a full snapshot to the frontend.  A full snapshot (rather
// than a delta) means a component that mounts mid-scan is correct from
// the first event it receives.
func (r *Registry) emit() {
	r.mu.RLock()
	ctx := r.ctx
	r.mu.RUnlock()

	if ctx == nil {
		return
	}

	events.Emit(ctx, events.JobsChanged, r.Snapshot())
}

// touch marks the registry dirty so the next emitter tick publishes it.
func (r *Registry) touch() {
	r.dirty.Store(true)
}

// flush publishes immediately.  Used for state transitions, where a
// quarter-second of lag would make a button press feel unresponsive.
func (r *Registry) flush() {
	r.dirty.Store(false)
	r.emit()
}

// Start registers a job and returns its handle.  Re-registering an
// existing ID reuses the handle and its log, which is what happens when
// a queued scan is popped off the queue and actually begins.
func (r *Registry) Start(spec Spec) *Handle {
	now := nowMillis()

	state := spec.State
	if state == "" {
		state = StateRunning
	}

	r.mu.Lock()

	h, existing := r.jobs[spec.ID]
	if !existing {
		h = &Handle{reg: r, id: spec.ID}
		r.jobs[spec.ID] = h
		r.order = append(r.order, spec.ID)
	}

	r.mu.Unlock()

	h.mu.Lock()

	if !existing {
		h.job = Job{
			ID:        spec.ID,
			StartedAt: now,
			Stages:    []Stage{},
			Stats:     []Stat{},
		}
	}

	h.job.Kind = spec.Kind
	h.job.Title = spec.Title
	h.job.Subtitle = spec.Subtitle
	h.job.State = state
	h.job.Caps = spec.Caps
	h.job.Total = spec.Total
	h.job.UpdatedAt = now
	h.job.EndedAt = 0
	h.job.Error = ""
	h.controls = spec.Controls
	h.durable = spec.Durable
	h.mu.Unlock()

	r.persistPause(spec.ID, state)
	r.pruneFinished()
	r.flush()

	return h
}

// Get returns the handle for an ID, or nil when unknown.
func (r *Registry) Get(id string) *Handle {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.jobs[id]
}

// Snapshot returns every known job, oldest registration first.
func (r *Registry) Snapshot() []Job {
	r.mu.RLock()

	out := make([]Job, 0, len(r.order))
	handles := make([]*Handle, 0, len(r.order))

	for _, id := range r.order {
		if h, ok := r.jobs[id]; ok {
			handles = append(handles, h)
		}
	}

	r.mu.RUnlock()

	for _, h := range handles {
		out = append(out, h.Snapshot())
	}

	return out
}

// Logs returns the retained log tail for a job, oldest entry first.
func (r *Registry) Logs(id string) []LogEntry {
	h := r.Get(id)
	if h == nil {
		return []LogEntry{}
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	out := make([]LogEntry, len(h.log))
	copy(out, h.log)

	return out
}

// HasActive reports whether any job is in a non-terminal state.
func (r *Registry) HasActive() bool {
	for _, j := range r.Snapshot() {
		if !j.State.IsTerminal() {
			return true
		}
	}

	return false
}

// Pause asks the owning subsystem to pause a job.  The job moves to
// "pausing" immediately for UI feedback; the producer confirms the
// transition to "paused" when it actually stops.
func (r *Registry) Pause(id string) {
	h := r.Get(id)
	if h == nil {
		return
	}

	h.mu.Lock()
	pause := h.controls.Pause
	pausable := h.job.Caps.Pausable
	live := !h.job.State.IsTerminal()
	h.mu.Unlock()

	if pause == nil || !pausable || !live {
		return
	}

	h.SetState(StatePausing)
	h.Logf(LevelInfo, "Pause requested")

	go pause()
}

// Resume asks the owning subsystem to continue a paused job.
func (r *Registry) Resume(id string) {
	h := r.Get(id)
	if h == nil {
		return
	}

	h.mu.Lock()
	resume := h.controls.Resume
	paused := h.job.State == StatePaused || h.job.State == StatePausing
	h.mu.Unlock()

	if resume == nil || !paused {
		return
	}

	h.Logf(LevelInfo, "Resume requested")

	go resume()
}

// Cancel asks the owning subsystem to abandon a job.
func (r *Registry) Cancel(id string) {
	h := r.Get(id)
	if h == nil {
		return
	}

	h.mu.Lock()
	cancel := h.controls.Cancel
	cancellable := h.job.Caps.Cancellable
	live := !h.job.State.IsTerminal()
	h.mu.Unlock()

	if cancel == nil || !cancellable || !live {
		return
	}

	h.SetState(StateCancelling)
	h.Logf(LevelInfo, "Cancel requested")

	go cancel()
}

// Remove drops a job from the registry entirely, discarding its log.
func (r *Registry) Remove(id string) {
	r.mu.Lock()

	delete(r.jobs, id)

	for i, existing := range r.order {
		if existing == id {
			r.order = append(r.order[:i], r.order[i+1:]...)

			break
		}
	}

	r.mu.Unlock()

	if r.store != nil {
		r.store.ClearPaused(id)
	}

	r.flush()
}

// ClearFinished drops every terminal job.  Bound to the "clear" action
// in the jobs panel.
func (r *Registry) ClearFinished() {
	r.mu.Lock()

	kept := r.order[:0]

	for _, id := range r.order {
		h, ok := r.jobs[id]
		if !ok {
			continue
		}

		h.mu.Lock()
		terminal := h.job.State.IsTerminal()
		h.mu.Unlock()

		if terminal {
			delete(r.jobs, id)

			continue
		}

		kept = append(kept, id)
	}

	r.order = kept
	r.mu.Unlock()

	r.flush()
}

// pruneFinished evicts terminal jobs that are older than the retention
// window, and trims the oldest when too many have accumulated.
func (r *Registry) pruneFinished() {
	cutoff := nowMillis() - finishedRetention.Milliseconds()

	r.mu.Lock()
	defer r.mu.Unlock()

	var finished []string

	for _, id := range r.order {
		h, ok := r.jobs[id]
		if !ok {
			continue
		}

		h.mu.Lock()
		terminal := h.job.State.IsTerminal()
		ended := h.job.EndedAt
		h.mu.Unlock()

		if terminal && ended > 0 && ended < cutoff {
			delete(r.jobs, id)

			continue
		}

		if terminal {
			finished = append(finished, id)
		}
	}

	// Trim the oldest terminal jobs beyond the cap.
	if excess := len(finished) - maxFinished; excess > 0 {
		for _, id := range finished[:excess] {
			delete(r.jobs, id)
		}
	}

	kept := r.order[:0]

	for _, id := range r.order {
		if _, ok := r.jobs[id]; ok {
			kept = append(kept, id)
		}
	}

	r.order = kept
}

// persistPause writes or clears the durable pause record for a job so a
// paused job comes back paused after a restart instead of silently
// resuming (or silently never running again).
func (r *Registry) persistPause(id string, state State) {
	if r.store == nil {
		return
	}

	h := r.Get(id)
	if h == nil {
		return
	}

	h.mu.Lock()
	durable := h.durable
	job := h.job
	h.mu.Unlock()

	if !durable {
		return
	}

	if state != StatePaused {
		r.store.ClearPaused(id)

		return
	}

	r.store.SetPaused(Persisted{
		ID:       job.ID,
		Kind:     job.Kind,
		Title:    job.Title,
		Subtitle: job.Subtitle,
	})
}

// PausedEntries returns the jobs of the given kind that were paused when
// the app last shut down.  Subsystems call this during startup and adopt
// each entry back into the registry with its controls attached.
func (r *Registry) PausedEntries(kind Kind) []Persisted {
	if r.store == nil {
		return nil
	}

	return r.store.PausedEntries(kind)
}

// IsPersistentlyPaused reports whether the given job ID was paused when
// the app last shut down.  Subsystems check this before auto-starting
// work at launch, so a paused job stays paused.
func (r *Registry) IsPersistentlyPaused(id string) bool {
	if r.store == nil {
		return false
	}

	return r.store.IsPaused(id)
}

// ---------------------------------------------------------------------------
// Handle
// ---------------------------------------------------------------------------

// Snapshot returns a copy of the job's current state.
func (h *Handle) Snapshot() Job {
	h.mu.Lock()
	defer h.mu.Unlock()

	job := h.job

	job.Stages = make([]Stage, len(h.job.Stages))
	copy(job.Stages, h.job.Stages)

	job.Stats = make([]Stat, len(h.job.Stats))
	copy(job.Stats, h.job.Stats)

	return job
}

// State returns the job's current state.
func (h *Handle) State() State {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.job.State
}

// SetState moves the job to a new state.  Terminal states stamp EndedAt.
// Transitions flush immediately so controls feel responsive.
func (h *Handle) SetState(state State) {
	h.mu.Lock()

	if h.job.State == state {
		h.mu.Unlock()

		return
	}

	h.job.State = state
	h.job.UpdatedAt = nowMillis()

	if state.IsTerminal() {
		h.job.EndedAt = h.job.UpdatedAt
	} else {
		h.job.EndedAt = 0
	}

	h.mu.Unlock()

	h.reg.persistPause(h.id, state)
	h.reg.flush()
}

// SetPhase records the human-readable phase label ("Scanning files").
func (h *Handle) SetPhase(phase string) {
	h.mu.Lock()

	changed := h.job.Phase != phase
	h.job.Phase = phase
	h.job.UpdatedAt = nowMillis()
	h.mu.Unlock()

	if changed {
		h.reg.flush()

		return
	}

	h.reg.touch()
}

// SetProgress updates the progress numerator and denominator.  Pass a
// total of zero to render the job as indeterminate.
func (h *Handle) SetProgress(current, total int64) {
	h.mu.Lock()
	h.job.Current = current
	h.job.Total = total
	h.job.UpdatedAt = nowMillis()
	h.mu.Unlock()

	h.reg.touch()
}

// SetSubtitle updates the secondary line shown under the job title.
func (h *Handle) SetSubtitle(subtitle string) {
	h.mu.Lock()
	h.job.Subtitle = subtitle
	h.job.UpdatedAt = nowMillis()
	h.mu.Unlock()

	h.reg.touch()
}

// SetStats replaces the job's display statistics.
func (h *Handle) SetStats(stats []Stat) {
	h.mu.Lock()
	h.job.Stats = stats
	h.job.UpdatedAt = nowMillis()
	h.mu.Unlock()

	h.reg.touch()
}

// SetStages replaces the job's stage list.  Used by multi-tier jobs such
// as the index build.
func (h *Handle) SetStages(stages []Stage) {
	h.mu.Lock()
	h.job.Stages = stages
	h.job.UpdatedAt = nowMillis()
	h.mu.Unlock()

	h.reg.touch()
}

// SetCaps updates which controls the job currently supports.
func (h *Handle) SetCaps(caps Caps) {
	h.mu.Lock()
	h.job.Caps = caps
	h.mu.Unlock()

	h.reg.touch()
}

// Logf appends a line to the job's log ring buffer.
func (h *Handle) Logf(level Level, message string) {
	h.logEntry(level, message, "")
}

// LogDetail appends a log line carrying a secondary detail string, such
// as the file path a warning refers to.
func (h *Handle) LogDetail(level Level, message, detail string) {
	h.logEntry(level, message, detail)
}

func (h *Handle) logEntry(level Level, message, detail string) {
	h.mu.Lock()

	if len(h.log) >= maxLogEntries {
		// Drop the oldest entry.  Log volume is low enough (phase
		// transitions and per-file warnings) that the copy is cheaper
		// than maintaining an explicit ring index.
		h.log = append(h.log[:0], h.log[1:]...)
	}

	h.log = append(h.log, LogEntry{
		Time:    nowMillis(),
		Level:   level,
		Message: message,
		Detail:  detail,
	})

	h.job.LogCount++

	switch level {
	case LevelWarn:
		h.job.WarnCount++
	case LevelError:
		h.job.ErrorCount++
	case LevelInfo:
	}

	h.mu.Unlock()

	h.reg.touch()
}

// Complete marks the job finished successfully.
func (h *Handle) Complete() {
	h.SetPhase("")
	h.SetState(StateComplete)
}

// Cancelled marks the job as abandoned at the user's request.
func (h *Handle) Cancelled() {
	h.SetPhase("")
	h.SetState(StateCancelled)
}

// Fail marks the job as errored and records the message.
func (h *Handle) Fail(err error) {
	h.mu.Lock()
	h.job.Error = err.Error()
	h.mu.Unlock()

	h.Logf(LevelError, err.Error())
	h.SetState(StateError)
}

func nowMillis() int64 {
	return time.Now().UnixMilli()
}
