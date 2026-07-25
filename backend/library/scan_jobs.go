package library

import (
	"strconv"
	"time"

	"yellowjacket/backend/jobs"
)

// scanJobPrefix namespaces library scan jobs in the shared registry.
const scanJobPrefix = "scan:"

// scanPhaseLabels maps internal scan phase identifiers to the labels
// shown in the jobs UI.
var scanPhaseLabels = map[string]string{
	"counting":   "Counting files",
	"scanning":   "Reading metadata",
	"thumbnails": "Generating thumbnails",
	"orphans":    "Cleaning up removed files",
}

// SetJobRegistry wires the background job registry so scans report
// progress, logs, and pause/cancel controls to the frontend.
func (l *Library) SetJobRegistry(reg *jobs.Registry) {
	l.mu.Lock()
	l.jobs = reg
	l.mu.Unlock()
}

// jobRegistry returns the registry, or nil when none is wired.
func (l *Library) jobRegistry() *jobs.Registry {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.jobs
}

// scanJobID returns the stable registry ID for a library's scan job.
// It is stable across restarts so a durable pause can be matched back
// to the library it belongs to.
func scanJobID(libraryID int64) string {
	return scanJobPrefix + strconv.FormatInt(libraryID, 10)
}

// libraryIDFromJobID parses a library ID back out of a scan job ID.
func libraryIDFromJobID(id string) (int64, bool) {
	if len(id) <= len(scanJobPrefix) || id[:len(scanJobPrefix)] != scanJobPrefix {
		return 0, false
	}

	parsed, err := strconv.ParseInt(id[len(scanJobPrefix):], 10, 64)
	if err != nil {
		return 0, false
	}

	return parsed, true
}

// scanJobSpec builds the registry spec for a library scan.  Controls are
// bound to the library ID rather than "the current scan" so that a
// control arriving for a queued or stale job cannot disturb a different
// library's scan.
func (l *Library) scanJobSpec(
	entry scanQueueEntry,
	state jobs.State,
) jobs.Spec {
	return jobs.Spec{
		ID:       scanJobID(entry.libraryID),
		Kind:     jobs.KindLibraryScan,
		Title:    "Scanning " + entry.libraryName,
		Subtitle: entry.libraryPath,
		State:    state,
		Caps: jobs.Caps{
			Pausable:    true,
			Cancellable: true,
		},
		Durable: true,
		Controls: jobs.Controls{
			Pause:  func() { l.pauseScanForLibrary(entry.libraryID) },
			Resume: func() { l.resumeScanForLibrary(entry) },
			Cancel: func() { l.cancelScanForLibrary(entry.libraryID) },
		},
	}
}

// registerQueuedScanJob records a library that is waiting behind another
// scan, so the user can see the whole pipeline rather than just the head.
func (l *Library) registerQueuedScanJob(entry scanQueueEntry) {
	reg := l.jobRegistry()
	if reg == nil {
		return
	}

	h := reg.Start(l.scanJobSpec(entry, jobs.StateQueued))
	h.SetPhase("Waiting for other scans")
	h.Logf(jobs.LevelInfo, "Queued behind an in-progress scan")
}

// startScanJob registers (or re-registers, for a queued job now starting)
// the running job for a scan and returns its handle.
func (l *Library) startScanJob(entry scanQueueEntry) *jobs.Handle {
	reg := l.jobRegistry()
	if reg == nil {
		return nil
	}

	h := reg.Start(l.scanJobSpec(entry, jobs.StateRunning))
	h.SetProgress(0, 0)
	h.Logf(jobs.LevelInfo, "Scan started for "+entry.libraryPath)

	return h
}

// reportScanProgress mirrors a ScanProgress payload into the job
// registry.  Called from the same places that emit LibraryScanProgress
// so the two views never disagree.
func reportScanProgress(h *jobs.Handle, p ScanProgress) {
	if h == nil {
		return
	}

	label, ok := scanPhaseLabels[p.Phase]
	if !ok {
		label = p.Phase
	}

	h.SetPhase(label)

	// The counting pre-walk has no denominator to report against, so
	// leave the job indeterminate until the total is known.
	if p.Phase == "counting" {
		h.SetProgress(0, 0)
	} else {
		h.SetProgress(p.Processed, p.Total)
	}

	h.SetStats([]jobs.Stat{
		{Label: "Added", Value: strconv.FormatInt(p.Added, 10)},
		{Label: "Updated", Value: strconv.FormatInt(p.Updated, 10)},
		{Label: "Skipped", Value: strconv.FormatInt(p.Skipped, 10)},
	})
}

// finishScanJob applies the terminal state and summary for a completed,
// cancelled, or paused-then-abandoned scan.
func finishScanJob(h *jobs.Handle, m *ScanMetrics, cancelled bool) {
	if h == nil {
		return
	}

	h.SetStats([]jobs.Stat{
		{Label: "Added", Value: strconv.FormatInt(m.Added, 10)},
		{Label: "Updated", Value: strconv.FormatInt(m.Updated, 10)},
		{Label: "Skipped", Value: strconv.FormatInt(m.Skipped, 10)},
		{Label: "Removed", Value: strconv.FormatInt(m.Removed, 10)},
		{Label: "Duration", Value: m.Total.Round(time.Millisecond).String()},
		{Label: "Walk", Value: m.WalkDuration.Round(time.Millisecond).String()},
		{Label: "Metadata", Value: m.ExtractionWallClock.Round(time.Millisecond).String()},
		{Label: "DB writes", Value: m.DBWritesWallClock.Round(time.Millisecond).String()},
		{Label: "Thumbnails", Value: m.ThumbnailWallClock.Round(time.Millisecond).String()},
	})

	// The full timing breakdown goes into the log rather than the stats
	// grid: it is a profiling aid, wanted rarely and in full, and the
	// log pane already has a copy-to-clipboard button.
	h.LogDetail(jobs.LevelInfo, "Timing breakdown", m.timingBreakdown())

	if cancelled {
		h.Logf(jobs.LevelInfo, "Scan cancelled")
		h.Cancelled()

		return
	}

	h.Logf(jobs.LevelInfo,
		"Scan complete — "+
			strconv.FormatInt(m.Added, 10)+" added, "+
			strconv.FormatInt(m.Updated, 10)+" updated, "+
			strconv.FormatInt(m.Removed, 10)+" removed",
	)
	h.Complete()
}

// pauseScanForLibrary pauses the running scan, but only when the library
// asking is the one currently being scanned.
func (l *Library) pauseScanForLibrary(libraryID int64) {
	l.mu.Lock()
	current := l.currentScanLibraryID
	l.mu.Unlock()

	if current != libraryID {
		return
	}

	l.PauseScan()
}

// cancelScanForLibrary cancels a scan for one library.  A queued library
// is dropped from the queue; the running one is cancelled outright.
func (l *Library) cancelScanForLibrary(libraryID int64) {
	l.mu.Lock()

	current := l.currentScanLibraryID

	if current != libraryID {
		// Not running — drop it from the queue if it is waiting there.
		kept := l.scanQueue[:0]

		for _, entry := range l.scanQueue {
			if entry.libraryID != libraryID {
				kept = append(kept, entry)
			}
		}

		l.scanQueue = kept
		l.mu.Unlock()

		if reg := l.jobRegistry(); reg != nil {
			if h := reg.Get(scanJobID(libraryID)); h != nil {
				h.Cancelled()
			}
		}

		return
	}

	cancel := l.scanCancel
	paused := l.scanPaused
	pauseCh := l.scanPauseCh

	// Unblock paused workers so they observe the cancelled context
	// instead of sitting on the pause channel forever.
	if paused {
		l.scanPaused = false

		if pauseCh != nil {
			close(pauseCh)
		}

		l.scanPauseCh = nil
	}

	l.mu.Unlock()

	if cancel != nil {
		cancel()

		return
	}

	// No live scan goroutine — this is a scan that was paused before a
	// restart and never resumed.  Retire the adopted job directly.
	if reg := l.jobRegistry(); reg != nil {
		if h := reg.Get(scanJobID(libraryID)); h != nil {
			h.Cancelled()
		}
	}
}

// resumeScanForLibrary continues a paused scan.  When the scan goroutine
// is still alive it is simply unblocked.  When the pause outlived the
// process, a fresh scan is queued instead: scans are incremental, so
// already-imported files are skipped on the second pass and the effect
// is a resume rather than a restart.
func (l *Library) resumeScanForLibrary(entry scanQueueEntry) {
	l.mu.Lock()
	live := l.scanActive && l.currentScanLibraryID == entry.libraryID
	l.mu.Unlock()

	if live {
		l.ResumeScan()

		return
	}

	if reg := l.jobRegistry(); reg != nil {
		if h := reg.Get(scanJobID(entry.libraryID)); h != nil {
			h.Logf(jobs.LevelInfo,
				"Resuming from a previous session — already-imported "+
					"files are skipped")
		}
	}

	// Clear the durable pause record before restarting, otherwise
	// RestorePausedScans would adopt it again on the next launch.
	if reg := l.jobRegistry(); reg != nil {
		reg.Remove(scanJobID(entry.libraryID))
	}

	if err := l.ScanLibrary(entry.libraryID); err != nil {
		l.logger.Warn("could not resume scan",
			"libraryID", entry.libraryID, "err", err)
	}
}

// RestorePausedScans adopts scans that were paused when the app last
// shut down back into the job registry, still paused.  Call during
// startup before SoftScanAllLibraries so the soft scan does not restart
// a library the user deliberately paused.
func (l *Library) RestorePausedScans() {
	reg := l.jobRegistry()
	if reg == nil {
		return
	}

	for _, p := range reg.PausedEntries(jobs.KindLibraryScan) {
		libraryID, ok := libraryIDFromJobID(p.ID)
		if !ok {
			reg.Remove(p.ID)

			continue
		}

		lib, err := l.db.Queries.GetLibrary(l.ctx, libraryID)
		if err != nil {
			// The library was removed while the scan was paused.
			l.logger.Info("dropping paused scan for missing library",
				"libraryID", libraryID)
			reg.Remove(p.ID)

			continue
		}

		entry := scanQueueEntry{
			libraryID:   lib.ID,
			libraryName: lib.Name,
			libraryPath: lib.Path,
		}

		h := reg.Start(l.scanJobSpec(entry, jobs.StatePaused))
		h.SetPhase("Paused")
		h.Logf(jobs.LevelInfo, "Paused in a previous session — resume to continue")

		l.logger.Info("restored paused library scan",
			"libraryID", lib.ID, "libraryName", lib.Name)
	}
}

// isScanPausedPersistently reports whether a library's scan was left
// paused by a previous session.
func (l *Library) isScanPausedPersistently(libraryID int64) bool {
	reg := l.jobRegistry()
	if reg == nil {
		return false
	}

	return reg.IsPersistentlyPaused(scanJobID(libraryID))
}
