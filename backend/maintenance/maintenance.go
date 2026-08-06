// Package maintenance runs the janitorial work that keeps persisted
// data from accumulating without bound.
//
// It exists because cleanup used to have no owner.  Functions that
// deleted expired rows were written and then never called; files written
// by one package had no counterpart anywhere that removed them.  A
// registry makes the set of janitorial jobs a single visible list, so a
// new cache that forgets to register is obvious in review rather than
// discovered years later as unbounded growth.
//
// Policies come from the classification in backend/datamap:
//
//   - Derived data is swept against a live set computed from the data it
//     was derived from.  Anything not in the live set is garbage.
//   - Cache data is evicted by age, because it has no owner to be
//     compared against and is merely expensive — not impossible — to
//     re-fetch.
//
// Sweeps are idempotent and safe to interrupt: each deletes only what it
// has positively identified as unreferenced, so a partial run simply
// leaves work for the next one.
package maintenance

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Result reports what a single job reclaimed.
type Result struct {
	// RowsDeleted counts database rows removed.
	RowsDeleted int64
	// FilesDeleted counts files removed from disk.
	FilesDeleted int64
	// BytesFreed is the size of those files.
	BytesFreed int64
}

// empty reports whether the job found nothing to do, so quiet runs can
// be logged at a lower level.
func (r Result) empty() bool {
	return r.RowsDeleted == 0 && r.FilesDeleted == 0
}

// Job is one unit of janitorial work.
type Job struct {
	// Name identifies the job in logs and in the run record.
	Name string
	// MinInterval is the minimum time between runs.  A job is skipped if
	// it ran more recently than this, so hooking the runner to a
	// frequently-firing trigger stays cheap.
	MinInterval time.Duration
	// Run performs the work.  It must be idempotent and must respect
	// context cancellation.
	Run func(ctx context.Context) (Result, error)
}

// Runner holds the registered jobs and enforces their intervals.
type Runner struct {
	mu      sync.Mutex
	jobs    []Job
	lastRun map[string]time.Time
	logger  *slog.Logger
}

// NewRunner returns an empty runner.
func NewRunner(logger *slog.Logger) *Runner {
	return &Runner{
		lastRun: make(map[string]time.Time),
		logger:  logger,
	}
}

// Register adds a job.  Registering a name twice replaces the earlier
// job, so wiring code can be re-run without accumulating duplicates.
func (r *Runner) Register(job Job) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i, existing := range r.jobs {
		if existing.Name == job.Name {
			r.jobs[i] = job

			return
		}
	}

	r.jobs = append(r.jobs, job)
}

// JobNames returns the registered job names, for tests and diagnostics.
func (r *Runner) JobNames() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	names := make([]string, 0, len(r.jobs))
	for _, j := range r.jobs {
		names = append(names, j.Name)
	}

	return names
}

// due reports whether a job's interval has elapsed.
func (r *Runner) due(job Job, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	last, ran := r.lastRun[job.Name]
	if !ran {
		return true
	}

	return now.Sub(last) >= job.MinInterval
}

func (r *Runner) markRun(name string, at time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.lastRun[name] = at
}

// snapshot copies the job list so a run does not hold the lock while
// executing jobs.
func (r *Runner) snapshot() []Job {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]Job, len(r.jobs))
	copy(out, r.jobs)

	return out
}

// RunDue runs every job whose interval has elapsed.  A job that fails is
// logged and does not prevent the others from running; janitorial work
// is best-effort by nature and the next run will retry.
func (r *Runner) RunDue(ctx context.Context) Result {
	var total Result

	for _, job := range r.snapshot() {
		if ctx.Err() != nil {
			r.logger.Info("maintenance cancelled", "after", job.Name)

			break
		}

		now := time.Now()
		if !r.due(job, now) {
			continue
		}

		start := time.Now()

		result, err := job.Run(ctx)

		r.markRun(job.Name, now)

		if err != nil {
			r.logger.Warn("maintenance job failed",
				"job", job.Name, "err", err,
				"duration", time.Since(start),
			)

			continue
		}

		total.RowsDeleted += result.RowsDeleted
		total.FilesDeleted += result.FilesDeleted
		total.BytesFreed += result.BytesFreed

		if result.empty() {
			r.logger.Debug("maintenance job found nothing",
				"job", job.Name, "duration", time.Since(start),
			)

			continue
		}

		r.logger.Info("maintenance job reclaimed",
			"job", job.Name,
			"rows", result.RowsDeleted,
			"files", result.FilesDeleted,
			"bytes", result.BytesFreed,
			"duration", time.Since(start),
		)
	}

	return total
}

// Start runs the due jobs immediately and then on every tick until the
// context is cancelled.  It returns straight away; the loop runs in its
// own goroutine.
func (r *Runner) Start(ctx context.Context, tick time.Duration) {
	go func() {
		r.RunDue(ctx)

		ticker := time.NewTicker(tick)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.RunDue(ctx)
			}
		}
	}()
}
