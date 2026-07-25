package explore

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"yellowjacket/backend/jobs"
)

// errIndexStageFailed wraps the per-stage error text reported by the
// dump importer so the job carries a typed failure.
var errIndexStageFailed = errors.New("index build stage failed")

// indexJobID is the stable registry ID for the search index build.
// There is only ever one, so the ID is a constant.
const indexJobID = "index:build"

// indexPausedKey marks a build the user paused.  It lives in
// explore_index_meta alongside the import's other checkpoints so a
// paused build stays paused across a restart instead of resuming on
// the next launch.
const indexPausedKey = "index_build_paused"

// SetJobRegistry wires the background job registry so index builds
// report progress, stage state, logs, and pause/cancel controls.
func (si *SearchIndex) SetJobRegistry(reg *jobs.Registry) {
	si.mu.Lock()
	si.jobs = reg
	si.mu.Unlock()
}

// jobRegistry returns the registry, or nil when none is wired.
func (si *SearchIndex) jobRegistry() *jobs.Registry {
	si.mu.RLock()
	defer si.mu.RUnlock()

	return si.jobs
}

// logIndexJob appends a line to the index build's job log, if a build
// job is currently registered.
func (si *SearchIndex) logIndexJob(level jobs.Level, message string) {
	reg := si.jobRegistry()
	if reg == nil {
		return
	}

	if h := reg.Get(indexJobID); h != nil {
		h.Logf(level, message)
	}
}

// indexJobSpec builds the registry spec for the index build.
//
// The build is genuinely pausable rather than merely cancellable: the
// dump importer checkpoints its listen-count offset to counts.bin and
// its stage to state.json, so stopping and restarting picks up where it
// left off instead of re-downloading multiple gigabytes.
func (si *SearchIndex) indexJobSpec(state jobs.State) jobs.Spec {
	return jobs.Spec{
		ID:       indexJobID,
		Kind:     jobs.KindIndexBuild,
		Title:    "Building search index",
		Subtitle: "MusicBrainz catalog + ListenBrainz popularity",
		State:    state,
		Caps: jobs.Caps{
			Pausable:    true,
			Cancellable: true,
		},
		Durable: true,
		Controls: jobs.Controls{
			Pause:  si.PauseBuild,
			Resume: si.ResumeBuild,
			Cancel: si.CancelBuild,
		},
	}
}

// syncIndexJob mirrors an IndexStatus snapshot into the job registry.
// It is driven from emitStatus, which every status mutation already
// funnels through, so there is no path that updates one view and not
// the other.
func (si *SearchIndex) syncIndexJob(status IndexStatus) {
	reg := si.jobRegistry()
	if reg == nil {
		return
	}

	si.mu.RLock()
	paused := si.buildPaused
	si.mu.RUnlock()

	h := reg.Get(indexJobID)

	// A build with no stages is the early-return path in runDumpBuild
	// (the catalog import is already done).  Nothing to show.
	if h == nil {
		if !status.Building || len(status.Tiers) == 0 {
			return
		}

		h = reg.Start(si.indexJobSpec(jobs.StateRunning))
		h.Logf(jobs.LevelInfo, "Index build started")
	}

	// A finished job is immutable.  Without this guard the 3-second
	// status ticker would keep touching it forever, re-emitting
	// JobsChanged long after the build ended.
	if h.State().IsTerminal() {
		return
	}

	si.applyStagesToJob(h, status)

	if status.Building {
		// Don't stomp a pause or cancel that is still settling; those
		// transitions are confirmed by their own control paths.
		if h.State() == jobs.StateQueued {
			h.SetState(jobs.StateRunning)
		}

		return
	}

	si.finishIndexJob(h, paused)
}

// applyStagesToJob maps index tiers onto job stages and derives the
// headline progress bar from whichever tier is currently running.
func (si *SearchIndex) applyStagesToJob(h *jobs.Handle, status IndexStatus) {
	stages := make([]jobs.Stage, 0, len(status.Tiers))

	var (
		phase            string
		current, total   int64
		foundRunningTier bool
	)

	for _, t := range status.Tiers {
		stages = append(stages, jobs.Stage{
			Name:    t.Name,
			State:   t.State,
			Current: int64(t.Completed),
			Total:   int64(t.Total),
			Error:   t.Error,
		})

		if t.State == "running" && !foundRunningTier {
			foundRunningTier = true
			phase = t.Name
			current = int64(t.Completed)
			total = int64(t.Total)
		}
	}

	h.SetStages(stages)

	if foundRunningTier {
		h.SetPhase(phase)
		h.SetProgress(current, total)
	}

	h.SetStats([]jobs.Stat{
		{Label: "Artists", Value: strconv.Itoa(status.Artists)},
		{Label: "Release groups", Value: strconv.Itoa(status.ReleaseGroups)},
		{Label: "Recordings", Value: strconv.Itoa(status.Recordings)},
		{Label: "Total rows", Value: strconv.Itoa(status.TotalRows)},
	})
}

// finishIndexJob resolves a build that is no longer running into the
// right terminal (or paused) state.  A stopped build that never wrote
// the done marker is reported as cancelled rather than complete —
// claiming success for a half-finished import would be a lie.
func (si *SearchIndex) finishIndexJob(h *jobs.Handle, paused bool) {
	if h.State().IsTerminal() || h.State() == jobs.StatePaused {
		return
	}

	// A stage that errored means the build failed; reporting that as
	// "stopped" would hide a real failure behind a neutral word.
	if !paused {
		for _, stage := range h.Snapshot().Stages {
			if stage.State == "error" {
				h.Fail(fmt.Errorf("%w: %s: %s",
					errIndexStageFailed, stage.Name, stage.Error))

				return
			}
		}
	}

	if paused {
		h.SetPhase("Paused")
		h.SetState(jobs.StatePaused)
		h.Logf(jobs.LevelInfo,
			"Build paused — progress is checkpointed and will resume "+
				"from here")

		return
	}

	if si.hasMeta(dumpImportDoneKey) {
		h.Logf(jobs.LevelInfo, "Index build complete")
		h.Complete()

		return
	}

	h.Logf(jobs.LevelInfo, "Build stopped before finishing")
	h.Cancelled()
}

// PauseBuild stops the in-flight index build and remembers that the
// user asked for it, so it is not restarted automatically — including
// on the next launch.  Blocks until the build goroutine exits; the job
// registry invokes controls on their own goroutine.
func (si *SearchIndex) PauseBuild() {
	si.mu.Lock()
	si.buildPaused = true
	si.mu.Unlock()

	si.setMeta(indexPausedKey, "1")
	si.StopBuild()
	si.emitStatus()
}

// ResumeBuild clears the pause and restarts the build, which picks up
// from the importer's last checkpoint.
func (si *SearchIndex) ResumeBuild() {
	si.mu.Lock()
	si.buildPaused = false
	ctx := si.runtimeCtx
	si.mu.Unlock()

	si.deleteMeta(indexPausedKey)

	if reg := si.jobRegistry(); reg != nil {
		if h := reg.Get(indexJobID); h != nil {
			h.SetState(jobs.StateRunning)
			h.Logf(jobs.LevelInfo, "Resuming from last checkpoint")
		}
	}

	if ctx != nil {
		si.StartBuild(ctx)
	}
}

// CancelBuild stops the build without marking it paused.  The importer's
// on-disk checkpoints are left in place, so starting a new build later
// still resumes rather than re-downloading — cancel here means "stop
// working now", not "throw away the progress".
func (si *SearchIndex) CancelBuild() {
	si.mu.Lock()
	si.buildPaused = false
	si.mu.Unlock()

	si.deleteMeta(indexPausedKey)
	si.StopBuild()
	si.emitStatus()
}

// ImportComplete reports whether the dump import wrote its done marker,
// meaning every stage finished.  A resumable import that was interrupted
// leaves this false even though the index may already be queryable.
func (si *SearchIndex) ImportComplete() bool {
	return si.hasMeta(dumpImportDoneKey)
}

// BaselineSeries returns the incremental listens series the index's
// popularity numbers are currently caught up to, or 0 when no baseline
// import has completed.  A change in this value between two runs is the
// signal that a refresh actually folded in new data.
func (si *SearchIndex) BaselineSeries() int {
	series, _ := si.metaInt(listensAppliedSeriesKey)

	return series
}

// LastImported returns when the dump import last completed, or the zero
// time if it never has.  Drives the rebuild cadence.
func (si *SearchIndex) LastImported() time.Time {
	rows, err := si.db.QueryContext(
		"SELECT value FROM explore_index_meta WHERE key = ?", dumpImportDoneKey,
	)
	if err != nil {
		return time.Time{}
	}

	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return time.Time{}
	}

	var raw string
	if err := rows.Scan(&raw); err != nil {
		return time.Time{}
	}

	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}

	return parsed
}

// PrepareRebuild clears the completion marker so the next StartBuild
// re-imports from the newest published dump instead of short-circuiting.
//
// The importer deletes its staging directory on completion, so there is
// no stale checkpoint to clear as well — a rebuild rediscovers the
// current dump and starts from offset zero.  Existing rows are left in
// place: assembly upserts by MBID, so the index stays queryable
// throughout rather than going empty for the length of a re-import.
func (si *SearchIndex) PrepareRebuild() {
	si.deleteMeta(dumpImportDoneKey)
	si.logger.Info("search index: cleared completion marker for rebuild")
}

// RefreshNow folds any newly published incremental listens dumps into
// the index's popularity numbers, synchronously.  Pass 0 to bypass the
// cadence gate.
func (si *SearchIndex) RefreshNow(ctx context.Context, minInterval time.Duration) {
	si.RefreshListenCounts(ctx, minInterval)
}

// buildPausedByUser reports whether a build was paused and not resumed,
// including by a previous session.
func (si *SearchIndex) buildPausedByUser() bool {
	si.mu.RLock()
	paused := si.buildPaused
	si.mu.RUnlock()

	if paused {
		return true
	}

	return si.hasMeta(indexPausedKey)
}

// AdoptPausedBuild re-registers a build that was paused when the app
// last shut down, so it shows up in the jobs panel with a resume button
// instead of silently not running.  Called during startup.
func (si *SearchIndex) AdoptPausedBuild() {
	reg := si.jobRegistry()
	if reg == nil {
		return
	}

	// A completed import cannot be meaningfully paused; clear a stale
	// marker rather than showing a job that would never do anything.
	if si.hasMeta(dumpImportDoneKey) {
		si.deleteMeta(indexPausedKey)
		reg.Remove(indexJobID)

		return
	}

	if !si.hasMeta(indexPausedKey) {
		return
	}

	si.mu.Lock()
	si.buildPaused = true
	si.mu.Unlock()

	h := reg.Start(si.indexJobSpec(jobs.StatePaused))
	h.SetPhase("Paused")
	h.Logf(jobs.LevelInfo,
		"Paused in a previous session — resume to continue from the "+
			"last checkpoint")

	si.logger.Info("search index: restored paused build")
}
