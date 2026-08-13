package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	goruntime "runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"yellowjacket/backend/autotag"
	"yellowjacket/backend/database"
	"yellowjacket/backend/database/sql/sqlcgen"
	"yellowjacket/backend/events"
	"yellowjacket/backend/jobs"
	"yellowjacket/backend/metadata"
	"yellowjacket/backend/system"
)

// scanBatchSize controls how many files are committed in a single
// database transaction during a scan.  Larger batches amortize
// SQLite's fsync cost but increase the blast radius of a failed commit.
const scanBatchSize = 300

// entityCache holds recently resolved database entities so that
// repeated upserts for the same artist/album/cover art within a scan
// can be served from memory instead of hitting the database.
// It is only accessed from the single DB-writer goroutine and
// therefore needs no synchronisation.
type entityCache struct {
	artistCredits map[string]sqlcgen.ArtistCredit
	artists       map[string]sqlcgen.Artist
	releaseGroups map[string]sqlcgen.ReleaseGroup
	coverArt      map[string]sqlcgen.CoverArt
	genres        map[string]sqlcgen.Genre
	// linkedCredits tracks artist-credit-artist links already created
	// so we skip the duplicate INSERT.  Key is "artistID:creditID".
	linkedCredits map[string]struct{}
}

func newEntityCache() *entityCache {
	return &entityCache{
		artistCredits: make(map[string]sqlcgen.ArtistCredit),
		artists:       make(map[string]sqlcgen.Artist),
		releaseGroups: make(map[string]sqlcgen.ReleaseGroup),
		coverArt:      make(map[string]sqlcgen.CoverArt),
		genres:        make(map[string]sqlcgen.Genre),
		linkedCredits: make(map[string]struct{}),
	}
}

// RescanHooks holds optional callbacks that run before and after
// the library-clear-and-scan phase of a full rescan.  The app
// layer sets these to coordinate cross-cutting concerns (e.g.
// clearing the queue, restoring playlists) without the library
// needing to know about those packages.
type RescanHooks struct {
	// PreClear runs before library data is wiped
	// (e.g. clear queue and stop playback).
	PreClear func()
	// PostScan runs after the scan completes
	// (e.g. restore playlists from M3U8 files).
	PostScan func()
}

// ScanHooks contains callbacks invoked after a library scan
// completes.  The app layer wires these so the library package
// does not depend on the playlist package directly.
type ScanHooks struct {
	// RepopulatePlaylists re-imports tracks for playlists that
	// lost their playlist_tracks rows (e.g., from a pre-fix
	// FullRescan).  Runs before ResolvePhantoms.
	RepopulatePlaylists func()
	// ResolvePhantoms re-links phantom playlist tracks whose
	// files now exist in the library after scanning.
	ResolvePhantoms func()
	// OnAllScansComplete runs after ALL queued scans finish
	// (queue drained).
	OnAllScansComplete func()
}

// Library manages scanning and querying the music collection.
type Library struct {
	// mu protects ctx, conf, and rescanHooks from concurrent
	// access during initialization.
	mu          sync.Mutex
	ctx         context.Context
	logger      *slog.Logger
	conf        *Config
	db          *database.DB
	rescanHooks RescanHooks

	// Scan control fields — protected by mu.
	scanActive  bool
	scanCancel  context.CancelFunc
	scanPaused  bool
	scanPauseCh chan struct{}

	// jobs is the background job registry.  Nil in tests that do not
	// exercise progress reporting; every use site must nil-check.
	jobs *jobs.Registry

	// Scan queue fields — protected by mu.
	scanQueue              []scanQueueEntry
	currentScanLibraryID   int64
	currentScanLibraryName string

	// removalHooks holds callbacks for cross-cutting concerns during
	// library removal (e.g. stopping playback, compacting queue).
	removalHooks RemovalHooks

	// scanHooks holds callbacks for post-scan processing
	// (e.g. resolving phantom playlist tracks).
	scanHooks ScanHooks

	// pipelineMu provides mutual exclusion between the scan
	// pipeline and the tag write pipeline.  Acquired at the
	// start of each pipeline, released at the end.
	pipelineMu sync.Mutex
}

// SetRescanHooks provides optional hooks for cross-cutting
// orchestration during FullRescan.
func (l *Library) SetRescanHooks(h RescanHooks) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.rescanHooks = h
}

// SetScanHooks provides optional hooks for cross-cutting
// orchestration after each library scan.
func (l *Library) SetScanHooks(h ScanHooks) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.scanHooks = h
}

// NewLibrary creates a new library with the given configuration.
// A nil config is permitted; scan paths come from the database
// rather than from the config's DirectoryPath.
func NewLibrary(
	ctx context.Context,
	logger *slog.Logger,
	conf *Config,
	db *database.DB,
) (*Library, error) {
	if conf == nil {
		conf = &Config{}
	}

	if err := conf.Validate(); err != nil {
		return nil, fmt.Errorf("invalid library config %#v: %w", conf, err)
	}

	library := &Library{
		ctx:    ctx,
		logger: logger,
		conf:   conf,
		db:     db,
	}

	return library, nil
}

// AcquirePipelineLock acquires the pipeline mutex for a tag write
// operation.  The caller must call ReleasePipelineLock when done.
// If a scan is currently in progress, AcquirePipelineLock blocks
// until it completes (and vice versa).
func (l *Library) AcquirePipelineLock() { l.pipelineMu.Lock() }

// ReleasePipelineLock releases the pipeline mutex after a tag write.
func (l *Library) ReleasePipelineLock() { l.pipelineMu.Unlock() }

// SetContext sets the Wails runtime context and registers event handlers.
func (l *Library) SetContext(ctx context.Context) {
	l.mu.Lock()
	l.ctx = ctx
	l.mu.Unlock()

	l.registerEventHandlers()
}

// emit publishes a Wails event under the library lock, which the
// background scan workers need because they outlive the context that
// started them.  events.Emit tolerates a context with no Wails runtime;
// see its doc comment.
func (l *Library) emit(event string, data ...any) {
	l.mu.Lock()
	ctx := l.ctx
	l.mu.Unlock()

	events.Emit(ctx, event, data...)
}

// registerEventHandlers sets up Wails runtime event listeners.
// The legacy LibraryConfigChanged handler was removed — in the
// multi-library model, libraries are managed through the CRUD
// API (Phase 12) and scanned via ScanLibrary/ScanAllLibraries.
func (l *Library) registerEventHandlers() {
	if l.ctx == nil {
		l.logger.Error(
			"Context is nil, cannot register event handlers",
		)
	}
}

// scanInternal performs the full scan pipeline for a single library.
// It is called from the scan queue coordinator (startScan) or the
// legacy Scan() wrapper. The caller is responsible for goroutine
// management; this method blocks until the scan completes.
func (l *Library) scanInternal(
	libraryID int64,
	libraryName string,
	libraryPath string,
) *ScanMetrics {
	// Acquire pipeline lock for scan/write mutual exclusion.
	l.pipelineMu.Lock()
	defer l.pipelineMu.Unlock()

	metrics := newScanMetrics()
	metrics.LibraryID = libraryID
	metrics.LibraryName = libraryName
	scanStart := time.Now()

	// Register the background job before any work starts so the UI
	// indicator appears immediately, even during the pre-walk count.
	jobHandle := l.startScanJob(scanQueueEntry{
		libraryID:   libraryID,
		libraryName: libraryName,
		libraryPath: libraryPath,
	})

	// Stream non-fatal issues into the job log as they happen rather
	// than dumping them all at completion — the point of the log pane
	// is to answer "what is it doing right now".
	metrics.onWarning = func(w ScanWarning) {
		if jobHandle == nil {
			return
		}

		jobHandle.LogDetail(jobs.LevelWarn, w.Phase+": "+w.Err, w.FilePath)
	}

	scanCtx, scanCancel := context.WithCancel(l.ctx)
	defer scanCancel()

	l.mu.Lock()
	l.scanCancel = scanCancel
	l.scanActive = true
	l.scanPaused = false
	l.scanPauseCh = nil
	l.mu.Unlock()

	defer func() {
		l.mu.Lock()
		l.scanCancel = nil
		// If still paused, unpause so no dangling channel.
		if l.scanPaused {
			l.scanPaused = false
			if l.scanPauseCh != nil {
				close(l.scanPauseCh)
			}
		}

		l.scanPauseCh = nil
		l.mu.Unlock()
	}()

	workerCount := resolveScanWorkerCount(
		ScanConcurrencyAuto,
		libraryPath,
	)

	l.logger.Info(
		"beginning library scan",
		"libraryID", libraryID,
		"libraryName", libraryName,
		"libraryPath", libraryPath,
		"workers", workerCount,
	)

	// Helper to build a ScanProgress with library identification.
	queuedCount := func() int {
		l.mu.Lock()
		defer l.mu.Unlock()

		return len(l.scanQueue)
	}

	mkProgress := func(
		phase string,
		total, processed, a, s, u int64,
	) ScanProgress {
		return ScanProgress{
			Phase:       phase,
			Total:       total,
			Processed:   processed,
			Added:       a,
			Skipped:     s,
			Updated:     u,
			LibraryID:   libraryID,
			LibraryName: libraryName,
			QueuedCount: queuedCount(),
		}
	}

	// emitProgress publishes one progress update to both consumers: the
	// legacy LibraryScanProgress event and the shared job registry.
	// Routing everything through here keeps the two from drifting.
	emitProgress := func(p ScanProgress) {
		l.emit(events.LibraryScanProgress, p)
		reportScanProgress(jobHandle, p)
	}

	l.emit(events.LibraryScanStarted, map[string]any{
		"libraryId":   libraryID,
		"libraryName": libraryName,
	})

	basePath := libraryPath

	// --- Pre-walk: count audio files for progress reporting ---
	emitProgress(mkProgress("counting", 0, 0, 0, 0, 0))

	totalFiles := countAudioFiles(basePath)

	l.logger.Debug(
		"pre-walk file count complete",
		"total", totalFiles,
	)

	// --- Phase 1: load existing files from DB (per-library) ---
	loadStart := time.Now()

	existingFiles, err := l.db.Queries.GetAudioFilesByLibrary(
		l.ctx, libraryID,
	)
	if err != nil {
		l.logger.Error(
			"could not load existing audio files",
			"libraryID", libraryID,
			"err", err,
		)

		return metrics
	}

	existingPaths := &sync.Map{}
	for _, f := range existingFiles {
		existingPaths.Store(f.FilePath, f)
	}

	metrics.LoadExisting = time.Since(loadStart)

	l.logger.Debug(
		"loaded existing files from database",
		"count", len(existingFiles),
		"libraryID", libraryID,
		"libraryPath", libraryPath,
	)

	workChan := make(chan scanWork, 100)
	resultChan := make(chan importResult, 100)
	dirDoneChan := make(chan dirClosed, 100)

	var added, skipped, updated atomic.Int64

	var scanErr error

	var errMu sync.Mutex

	// statBackfill collects staleness baselines for skipped files whose
	// rows predate migration 47.  Appended to only by the walk goroutine
	// and read after workChan closes, which orders the writes before the
	// flush.
	var statBackfill []sqlcgen.UpdateAudioFileStatParams

	// --- Phase 2: directory walk ---
	walkStart := time.Now()

	go func() {
		defer func() {
			metrics.WalkDuration = time.Since(walkStart)

			close(workChan)
		}()

		// stack tracks the directories the walk currently has open, so
		// that once one is fully enumerated (see isWithinDir) its total
		// scanWork count can be reported to the DB writer as a single
		// dirClosed event — see the dirClosed doc comment for why.
		var stack []*openDir

		closeDirsNotContaining := func(path string) {
			for len(stack) > 0 && !isWithinDir(path, stack[len(stack)-1].relPath) {
				top := stack[len(stack)-1]
				stack = stack[:len(stack)-1]

				select {
				case dirDoneChan <- dirClosed{dir: top.absDir, expected: top.expected}:
				case <-scanCtx.Done():
				}
			}
		}

		walkErr := fs.WalkDir(
			os.DirFS(basePath),
			".",
			func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					l.logger.Error(
						"problem walking directory",
						"path", path, "err", err,
					)

					return nil // continue walking
				}

				closeDirsNotContaining(path)

				if d.IsDir() {
					stack = append(stack, &openDir{
						relPath: path,
						absDir:  filepath.Join(basePath, path),
					})

					return nil
				}

				absoluteFilePath := filepath.Join(
					basePath, path,
				)
				fileExt := filepath.Ext(d.Name())

				fileType, isSupportedAudioFile := metadata.GetSupportedFileType(fileExt)
				if !isSupportedAudioFile {
					return nil
				}

				// Stat the entry for the staleness comparison below.
				// This happens before the file is read, so a file
				// modified mid-scan records the pre-read mtime and is
				// picked up again next scan — the safe direction.
				var (
					diskModTime int64
					diskSize    int64
				)

				if info, infoErr := d.Info(); infoErr == nil {
					diskModTime = info.ModTime().Unix()
					diskSize = info.Size()
				} else {
					l.logger.Debug(
						"could not stat file, treating as unchanged",
						"path", absoluteFilePath, "err", infoErr,
					)
				}

				// Check if file already exists in database.
				if existing, exists := existingPaths.LoadAndDelete(absoluteFilePath); exists {
					audioFile := existing.(sqlcgen.AudioFile)

					contentChanged := fileContentChanged(
						audioFile, diskModTime, diskSize,
					)

					if audioFile.RecordingID == 0 || contentChanged {
						l.logger.Debug(
							"file needs metadata update",
							"path", absoluteFilePath,
							"contentChanged", contentChanged,
						)

						select {
						case workChan <- scanWork{
							absolutePath:   absoluteFilePath,
							fileType:       fileType,
							existingFileID: audioFile.ID,
							needsUpdate:    true,
							existingLength: audioFile.LengthMilliseconds,
							contentChanged: contentChanged,
							modTime:        diskModTime,
						}:
							if len(stack) > 0 {
								stack[len(stack)-1].expected++
							}
						case <-scanCtx.Done():
							return scanCtx.Err()
						}

						return nil
					}

					l.logger.Debug(
						"file already in library with metadata, skipping",
						"path",
						absoluteFilePath,
					)
					skipped.Add(1)

					// Record the baseline for a row that lacks one so the
					// next scan can detect edits.  Collected here and
					// flushed in one transaction after the walk rather
					// than issuing an UPDATE per file.
					if audioFile.ModifiedAt == 0 && diskModTime != 0 {
						statBackfill = append(
							statBackfill,
							sqlcgen.UpdateAudioFileStatParams{
								ModifiedAt: diskModTime,
								FileSize:   diskSize,
								ID:         audioFile.ID,
							},
						)
					}

					return nil
				}

				l.logger.Debug(
					"queueing file for import",
					"path", absoluteFilePath,
				)

				select {
				case workChan <- scanWork{
					absolutePath: absoluteFilePath,
					fileType:     fileType,
					modTime:      diskModTime,
				}:
					if len(stack) > 0 {
						stack[len(stack)-1].expected++
					}
				case <-scanCtx.Done():
					return scanCtx.Err()
				}

				return nil
			},
		)
		if walkErr != nil {
			metrics.addWarning(
				"", "walk",
				fmt.Errorf(
					"problem walking library directory: %w",
					walkErr,
				),
			)
		}

		// Close whatever's left on the stack, root included — the walk
		// ended (normally or via cancellation) without another path
		// ever coming along to trigger closeDirsNotContaining for
		// these.  isWithinDir treats "." (root) as containing every
		// path, so closeDirsNotContaining itself can never pop it;
		// unwind directly instead.
		for len(stack) > 0 {
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]

			select {
			case dirDoneChan <- dirClosed{dir: top.absDir, expected: top.expected}:
			case <-scanCtx.Done():
			}
		}

		close(dirDoneChan)
	}()

	// --- Thumbnail worker pool (async, decoupled from DB writer) ---
	thumbChan := make(chan thumbnailWork, 100)

	var thumbWg sync.WaitGroup

	for range workerCount {
		thumbWg.Add(1)

		go func() {
			defer thumbWg.Done()

			for work := range thumbChan {
				if err := l.generateSizedVariantsWithMetrics(
					work.imgData,
					work.dir,
					work.hashStr,
					work.metrics,
				); err != nil {
					l.logger.Warn(
						"could not generate thumbnails",
						"hash", work.hashStr,
						"err", err,
					)

					metrics.addWarning(
						"", "variant", err,
					)
				}
			}
		}()
	}

	// --- Progress ticker ---
	// Periodically emits scan progress to the frontend.  Stopped
	// when the main scan phases (walk + extraction + DB writes)
	// are complete, before orphan cleanup begins.
	stopProgress := make(chan struct{})

	go func() {
		ticker := time.NewTicker(progressInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				a := added.Load()
				s := skipped.Load()
				u := updated.Load()

				emitProgress(mkProgress(
					"scanning", totalFiles,
					a+s+u, a, s, u,
				))
			case <-stopProgress:
				return
			}
		}
	}()

	// --- Phase 4: DB writer goroutine ---
	var dbWg sync.WaitGroup

	dbWg.Add(1)

	go func() {
		defer dbWg.Done()

		cache := newEntityCache()

		var (
			batch      []importResult
			dbStarted  bool
			dbStartVal time.Time
		)

		flushBatch := func() {
			if len(batch) == 0 {
				return
			}

			batchStart := time.Now()

			if batchErr := l.commitBatch(
				batch, cache, metrics,
				&added, &updated, &skipped,
				thumbChan,
			); batchErr != nil {
				errMu.Lock()
				scanErr = errors.Join(scanErr, batchErr)
				errMu.Unlock()
			}

			metrics.BatchCommits += time.Since(batchStart)
			batch = batch[:0]
		}

		// pending buffers extracted results by directory (keyed the
		// same way GroupKey's caller derives it, filepath.Dir on the
		// absolute path) until that directory's dirClosed event says
		// no more are coming — see the dirClosed doc comment. Only
		// then can ResolveDirectoryDiscNumbers see the whole
		// directory's disc tags at once instead of each file
		// guessing from its own tag alone.
		pending := make(map[string][]importResult)
		expected := make(map[string]int)
		dirClosedSeen := make(map[string]bool)

		resolveAndBatch := func(dir string) {
			results := pending[dir]
			delete(pending, dir)
			delete(expected, dir)
			delete(dirClosedSeen, dir)

			if len(results) == 0 {
				return
			}

			discs := make([]int, len(results))
			for i, r := range results {
				if r.tags != nil {
					discs[i] = r.tags.DiscNumber
				}
			}

			resolved := autotag.ResolveDirectoryDiscNumbers(discs)

			for i := range results {
				if results[i].tags != nil {
					results[i].tags.DiscNumber = resolved[i]
				}

				// Thread library ID into each result for saveAudioFile.
				results[i].libraryID = libraryID
				batch = append(batch, results[i])
			}

			if len(batch) >= scanBatchSize {
				flushBatch()
			}
		}

		rc, dc := resultChan, dirDoneChan

		for rc != nil || dc != nil {
			select {
			case result, ok := <-rc:
				if !ok {
					rc = nil

					continue
				}

				if !dbStarted {
					dbStartVal = time.Now()
					dbStarted = true
				}

				dir := filepath.Dir(result.absolutePath)
				pending[dir] = append(pending[dir], result)

				if dirClosedSeen[dir] && len(pending[dir]) >= expected[dir] {
					resolveAndBatch(dir)
				}
			case d, ok := <-dc:
				if !ok {
					dc = nil

					continue
				}

				expected[d.dir] = d.expected
				dirClosedSeen[d.dir] = true

				if len(pending[d.dir]) >= d.expected {
					resolveAndBatch(d.dir)
				}
			}
		}

		// Anything still buffered here belongs to a directory whose
		// expected count was never reached — an extraction failure
		// (see Phase 3: a failed file is warned-and-dropped, never
		// reaching resultChan) or a dirClosed event lost to
		// cancellation. Flush it anyway so no extracted file is
		// silently dropped; it just resolves from whatever subset of
		// the directory's disc tags actually arrived.
		for dir := range pending {
			resolveAndBatch(dir)
		}

		flushBatch()

		if dbStarted {
			metrics.DBWritesWallClock = time.Since(
				dbStartVal,
			)
		}
	}()

	// --- Phase 3: worker pool ---
	extractStart := time.Now()

	g := new(errgroup.Group)
	g.SetLimit(workerCount)

	for work := range workChan {
		g.Go(func() error {
			if err := l.waitIfPaused(scanCtx); err != nil {
				return err
			}

			result, err := l.extractAudioMetadata(
				work, metrics,
			)
			if err != nil {
				l.logger.Warn(
					"failed to extract metadata",
					"path", work.absolutePath,
					"err", err,
				)

				metrics.addWarning(
					work.absolutePath,
					"extraction", err,
				)

				return nil
			}

			select {
			case resultChan <- result:
			case <-scanCtx.Done():
				return scanCtx.Err()
			}

			return nil
		})
	}

	_ = g.Wait()

	metrics.ExtractionWallClock = time.Since(extractStart)

	close(resultChan)
	dbWg.Wait()

	// Stop the progress ticker — main scan phases are done.
	close(stopProgress)

	// Emit a final "scanning" progress so the bar reaches 100%.
	a := added.Load()
	s := skipped.Load()
	u := updated.Load()

	emitProgress(mkProgress("scanning", totalFiles, a+s+u, a, s, u))

	// Close thumbnail channel and wait for all thumbnail workers
	// to finish.  The DB writer has stopped sending work at this
	// point so it is safe to close.
	thumbStart := time.Now()

	emitProgress(mkProgress("thumbnails", totalFiles, a+s+u, a, s, u))

	close(thumbChan)
	thumbWg.Wait()

	metrics.ThumbnailWallClock = time.Since(thumbStart)

	// Establish staleness baselines for unchanged files that lacked one.
	// Safe to run even on a cancelled scan: every entry was individually
	// confirmed against the file on disk during the walk.
	l.flushStatBackfill(statBackfill)

	// Skip orphan cleanup if the scan was cancelled — existingPaths
	// still contains unvisited files that would be incorrectly deleted.
	cancelled := scanCtx.Err() != nil

	var removed atomic.Int64

	if cancelled {
		metrics.Cancelled = true

		l.logger.Info("scan cancelled, skipping orphan cleanup")
	} else {
		// --- Phase 5: orphan cleanup ---
		emitProgress(mkProgress("orphans", totalFiles, a+s+u, a, s, u))

		orphanStart := time.Now()

		existingPaths.Range(func(key, value any) bool {
			path := key.(string)
			audioFile := value.(sqlcgen.AudioFile)

			l.logger.Debug(
				"removing orphaned database entry",
				"path", path, "id", audioFile.ID,
			)

			if err := l.db.Queries.DeleteAudioFile(
				l.ctx, audioFile.ID,
			); err != nil {
				l.logger.Warn(
					"failed to delete orphaned audio file",
					"path", path,
					"id", audioFile.ID,
					"err", err,
				)

				metrics.addWarning(path, "orphan", err)

				return true
			}

			// Keep the file's tagging group in sync: drop the group's
			// track count and clear it out once empty, mirroring the
			// bookkeeping maybeRebindTaggingGroup does for a group_key
			// change. Without this, a folder whose files are removed
			// and replaced leaves a stale tagging_items row behind —
			// its track_count still counts the deleted files, and it
			// never clears from the autotag queue.
			if audioFile.GroupKey != "" {
				if err := l.db.Queries.DecrementTaggingItemTrackCount(
					l.ctx, audioFile.GroupKey,
				); err != nil {
					l.logger.Warn(
						"failed to decrement tagging group for orphan",
						"path", path,
						"group_key", audioFile.GroupKey,
						"err", err,
					)

					metrics.addWarning(path, "orphan", err)
				} else if err := l.db.Queries.DeleteTaggingItemIfEmpty(
					l.ctx, audioFile.GroupKey,
				); err != nil {
					l.logger.Warn(
						"failed to clean up emptied tagging group for orphan",
						"path", path,
						"group_key", audioFile.GroupKey,
						"err", err,
					)

					metrics.addWarning(path, "orphan", err)
				}
			}

			// Remove from FTS5 search index.
			if err := l.db.DeleteSearchIndex(
				audioFile.ID,
			); err != nil {
				l.logger.Warn(
					"failed to delete FTS entry for orphan",
					"id", audioFile.ID,
					"err", err,
				)

				metrics.addWarning(path, "orphan", err)
			}

			removed.Add(1)

			return true
		})

		metrics.OrphanCleanup = time.Since(orphanStart)

		// --- Phase 5b: orphaned metadata cleanup ---
		// Deleting an audio_files row above doesn't cascade to the
		// recording/release_group/artist_credit/artist rows it was the
		// last owner of — clean those up too, so a swapped-out artist
		// doesn't leave stale rows behind for the Explore index to
		// keep pointing at.
		l.pruneOrphanedMetadata()
	}

	// --- Phase 6: repopulate + resolve phantom playlist tracks ---
	// Repopulate first: re-imports tracks for playlists that lost
	// their rows (from a pre-fix FullRescan that deleted them).
	if !cancelled && l.scanHooks.RepopulatePlaylists != nil {
		l.scanHooks.RepopulatePlaylists()
	}
	// Then resolve: re-links phantom tracks to audio_files.
	if !cancelled && l.scanHooks.ResolvePhantoms != nil {
		l.scanHooks.ResolvePhantoms()
	}

	// --- Phase 7: post-scan variant generation ---
	if !cancelled {
		variantStart := time.Now()

		if err := l.generateMissingSizedVariants(); err != nil {
			l.logger.Warn(
				"could not generate missing sized variants",
				"err", err,
			)

			metrics.addWarning("", "variant", err)
		}

		metrics.PostScanVariants = time.Since(variantStart)
	}

	// --- Finalize ---
	metrics.Added = added.Load()
	metrics.Updated = updated.Load()
	metrics.Skipped = skipped.Load()
	metrics.Removed = removed.Load()
	metrics.Total = time.Since(scanStart)

	if scanErr != nil {
		l.logger.Warn(
			"scan completed with errors",
			"err", scanErr,
		)
	}

	l.logger.Info(
		"library scan complete",
		"libraryID", libraryID,
		"libraryName", libraryName,
		"added", metrics.Added,
		"updated", metrics.Updated,
		"removed", metrics.Removed,
		"skipped", metrics.Skipped,
		"warnings", len(metrics.Warnings),
		"cancelled", cancelled,
		"total", metrics.Total,
	)

	finishScanJob(jobHandle, metrics, cancelled)

	if cancelled {
		l.emit(events.LibraryScanCancelled, metrics)
	} else {
		l.emit(events.LibraryScanComplete, metrics)
	}

	return metrics
}

// progressInterval controls how often scan progress events are
// emitted to the frontend.
const progressInterval = 300 * time.Millisecond

// fileContentChanged reports whether a file on disk differs from what
// was imported, by comparing mtime and size against the recorded
// baseline.  This is what catches another application retagging a file
// in place — without it the scan skips every path already in the
// database and the edit stays invisible until a full rescan.
//
// Two cases are deliberately treated as unchanged:
//
//   - A recorded mtime of 0 means no baseline exists (the row predates
//     migration 47).  There is nothing to compare against, so reporting
//     a change would re-import the entire library on first upgrade.
//   - A disk mtime of 0 means the stat failed.  Skipping is preferable
//     to re-importing a file on no evidence.
//
// A writer that preserves mtime and lands on an identical file size
// defeats this check.  That needs content hashing to catch, which costs
// a full read of every file — deliberately out of scope.
func fileContentChanged(
	audioFile sqlcgen.AudioFile,
	diskModTime, diskSize int64,
) bool {
	if audioFile.ModifiedAt == 0 || diskModTime == 0 {
		return false
	}

	return diskModTime != audioFile.ModifiedAt ||
		diskSize != audioFile.FileSize
}

// flushStatBackfill writes mtime/size baselines for files the scan
// skipped but that had no baseline recorded.  Failures are logged and
// not fatal — a missing baseline only means the file is re-checked on
// the next scan.
func (l *Library) flushStatBackfill(
	entries []sqlcgen.UpdateAudioFileStatParams,
) {
	if len(entries) == 0 {
		return
	}

	tx, err := l.db.BeginTx()
	if err != nil {
		l.logger.Warn(
			"could not begin stat backfill transaction",
			"count", len(entries), "err", err,
		)

		return
	}

	defer func() { _ = tx.Rollback() }() // no-op after commit

	txq := l.db.Queries.WithTx(tx)

	for _, e := range entries {
		if updErr := txq.UpdateAudioFileStat(l.ctx, e); updErr != nil {
			l.logger.Warn(
				"could not backfill file stat",
				"audioFileID", e.ID, "err", updErr,
			)
		}
	}

	if err := tx.Commit(); err != nil {
		l.logger.Warn(
			"could not commit stat backfill",
			"count", len(entries), "err", err,
		)

		return
	}

	l.logger.Info(
		"recorded staleness baselines for existing files",
		"count", len(entries),
	)
}

// pruneOrphanedMetadata removes recording/release_group/artist_credit/
// artist rows left behind once the audio_files rows that justified them
// are gone — deleting an audio_files row doesn't cascade to any of
// these.  Runs in dependency order: recordings first (and their
// release_group_recordings/recording_genres rows), then release groups
// left with no recordings, then artist credits left with no
// recordings/release groups, then artists left with no credits.  Best
// effort — logs and continues on error rather than failing the scan.
func (l *Library) pruneOrphanedMetadata() {
	tx, err := l.db.BeginTx()
	if err != nil {
		l.logger.Warn("could not begin orphaned metadata cleanup transaction", "err", err)

		return
	}

	defer func() { _ = tx.Rollback() }() // no-op after commit

	txq := l.db.Queries.WithTx(tx)

	recordingIDs, err := txq.GetOrphanedRecordingIDs(l.ctx)
	if err != nil {
		l.logger.Warn("could not find orphaned recordings", "err", err)

		return
	}

	for _, id := range recordingIDs {
		if err := txq.DeleteReleaseGroupRecordingsByRecording(l.ctx, id); err != nil {
			l.logger.Warn(
				"could not delete release group links for orphaned recording",
				"id",
				id,
				"err",
				err,
			)
		}

		if err := txq.DeleteRecordingGenres(l.ctx, id); err != nil {
			l.logger.Warn("could not delete genres for orphaned recording", "id", id, "err", err)
		}

		if err := txq.DeleteRecording(l.ctx, id); err != nil {
			l.logger.Warn("could not delete orphaned recording", "id", id, "err", err)
		}
	}

	releaseGroupIDs, err := txq.GetOrphanedReleaseGroupIDs(l.ctx)
	if err != nil {
		l.logger.Warn("could not find orphaned release groups", "err", err)

		return
	}

	for _, id := range releaseGroupIDs {
		if err := txq.DeleteReleaseGroup(l.ctx, id); err != nil {
			l.logger.Warn("could not delete orphaned release group", "id", id, "err", err)
		}
	}

	artistCreditIDs, err := txq.GetOrphanedArtistCreditIDs(l.ctx)
	if err != nil {
		l.logger.Warn("could not find orphaned artist credits", "err", err)

		return
	}

	for _, id := range artistCreditIDs {
		if err := txq.DeleteArtistCreditArtistByCredit(l.ctx, id); err != nil {
			l.logger.Warn(
				"could not delete artist links for orphaned artist credit",
				"id",
				id,
				"err",
				err,
			)
		}

		if err := txq.DeleteArtistCredit(l.ctx, id); err != nil {
			l.logger.Warn("could not delete orphaned artist credit", "id", id, "err", err)
		}
	}

	artistIDs, err := txq.GetOrphanedArtistIDs(l.ctx)
	if err != nil {
		l.logger.Warn("could not find orphaned artists", "err", err)

		return
	}

	for _, id := range artistIDs {
		if err := txq.DeleteArtist(l.ctx, id); err != nil {
			l.logger.Warn("could not delete orphaned artist", "id", id, "err", err)
		}
	}

	if err := tx.Commit(); err != nil {
		l.logger.Warn("could not commit orphaned metadata cleanup", "err", err)

		return
	}

	if len(recordingIDs) > 0 || len(releaseGroupIDs) > 0 || len(artistCreditIDs) > 0 ||
		len(artistIDs) > 0 {
		l.logger.Info(
			"pruned orphaned library metadata",
			"recordings", len(recordingIDs),
			"releaseGroups", len(releaseGroupIDs),
			"artistCredits", len(artistCreditIDs),
			"artists", len(artistIDs),
		)
	}
}

// countAudioFiles performs a fast walk of the library directory,
// counting only files with supported audio extensions.  No per-file
// I/O is performed — this reads only directory entries.
func countAudioFiles(basePath string) int64 {
	var count int64

	_ = fs.WalkDir(
		os.DirFS(basePath), ".",
		func(_ string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}

			ext := filepath.Ext(d.Name())
			if _, ok := metadata.GetSupportedFileType(ext); ok {
				count++
			}

			return nil
		},
	)

	return count
}

// surveyAudioFiles walks the library directory and returns both the
// number of supported audio files and the newest mtime among them
// (Unix seconds).  The soft scan compares both against the database:
// the count catches added and removed files, the mtime catches files
// another application edited in place.
//
// Unlike countAudioFiles this stats every entry, so it is the more
// expensive of the two walks.  Only the startup soft scan uses it —
// the in-scan progress total does not need mtimes.
func surveyAudioFiles(basePath string) (count, maxModTime int64) {
	_ = fs.WalkDir(
		os.DirFS(basePath), ".",
		func(_ string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}

			ext := filepath.Ext(d.Name())
			if _, ok := metadata.GetSupportedFileType(ext); !ok {
				return nil
			}

			count++

			info, infoErr := d.Info()
			if infoErr != nil {
				return nil
			}

			if mt := info.ModTime().Unix(); mt > maxModTime {
				maxModTime = mt
			}

			return nil
		},
	)

	return count, maxModTime
}

// hddWorkerCount is the maximum number of concurrent extraction
// workers when the library resides on a spinning disk.
const hddWorkerCount = 2

// resolveScanWorkerCount returns the number of concurrent
// extraction workers based on the configured concurrency mode
// and the storage type of the library directory.
func resolveScanWorkerCount(
	mode ScanConcurrency,
	libraryPath string,
) int {
	switch mode {
	case ScanConcurrencySSD:
		return goruntime.NumCPU()
	case ScanConcurrencyHDD:
		return min(hddWorkerCount, goruntime.NumCPU())
	default: // auto
		if system.IsRotationalDisk(libraryPath) {
			return min(
				hddWorkerCount, goruntime.NumCPU(),
			)
		}

		return goruntime.NumCPU()
	}
}

// scanWork represents a file to be processed by a worker.
type scanWork struct {
	absolutePath   string
	fileType       metadata.AudioFileExtension
	existingFileID int64 // non-zero if this is an update
	needsUpdate    bool
	existingLength int64 // existing length if updating
	// contentChanged marks a file whose bytes differ from what was
	// imported (mtime/size mismatch), as opposed to one merely missing
	// its metadata link.  The audio itself may have changed, so cached
	// values like duration cannot be reused.
	contentChanged bool
	// modTime is the file's mtime (Unix seconds) observed during the
	// walk, stored as the new staleness baseline.
	modTime int64
}

// importResult holds metadata extracted by workers, ready for DB insertion.
type importResult struct {
	absolutePath   string
	fileType       metadata.AudioFileExtension
	lengthMillis   int64
	tags           *metadata.TrackMetadata
	audioProps     *metadata.AudioProperties
	existingFileID int64 // non-zero if this is an update
	needsUpdate    bool
	libraryID      int64 // library this file belongs to
	modTime        int64 // mtime baseline to persist (Unix seconds)
}

// dirClosed reports that the walk has fully enumerated a directory's
// audio files and will never enqueue another scanWork for it — expected
// is exactly how many scanWork items were sent for it. The DB writer
// uses this to know when it has every file it's going to get for that
// directory, so it can resolve disc-number consensus across the whole
// directory (autotag.ResolveDirectoryDiscNumbers) instead of each file
// guessing in isolation.
type dirClosed struct {
	dir      string
	expected int
}

// openDir is one frame of the walk goroutine's directory stack — see
// isWithinDir and its use in scanInternal's walk phase.  relPath is
// the fs.WalkDir-relative path (slash-separated, root as "."), used
// only to detect when the walk has moved on to something outside this
// directory; absDir is the OS-native absolute path, which is what
// dirClosed reports and what the DB writer's importResult.absolutePath
// values key against via filepath.Dir.
type openDir struct {
	relPath  string
	absDir   string
	expected int
}

// isWithinDir reports whether the fs.WalkDir-relative path is dir
// itself or something inside it. dir == "." (the library root) is
// always within, since fs.WalkDir's root path is "." and nothing on
// this stack can ever be outside the tree being walked.
func isWithinDir(path, dir string) bool {
	if dir == "." {
		return true
	}

	return path == dir || strings.HasPrefix(path, dir+"/")
}

// extractAudioMetadata reads and extracts metadata from an audio file.
// It opens the file once, extracting both tags and duration in a
// single pass, and records per-file timing in the shared metrics.
func (l *Library) extractAudioMetadata(
	work scanWork,
	metrics *ScanMetrics,
) (importResult, error) {
	result := importResult{
		absolutePath:   work.absolutePath,
		fileType:       work.fileType,
		existingFileID: work.existingFileID,
		needsUpdate:    work.needsUpdate,
		modTime:        work.modTime,
	}

	// Skip duration decode if we already have it from a previous import.
	// A file whose bytes changed is decoded again — a re-encode or a
	// replaced file can have a different duration than the one on record.
	skipDuration := work.needsUpdate &&
		work.existingLength > 0 &&
		!work.contentChanged

	tags, lengthMillis, audioProps, timing, err := metadata.ExtractAllMetadata(
		work.absolutePath, skipDuration,
	)

	if timing != nil {
		metrics.addExtraction(
			string(work.fileType),
			timing.TagExtraction,
			timing.DurationExtraction,
		)
	}

	if err != nil {
		return result, fmt.Errorf(
			"could not extract metadata for %s: %w",
			work.absolutePath,
			err,
		)
	}

	// A degraded tag read is reported but never fatal — the track is
	// imported either way, falling back to the filename if the tag
	// yielded nothing.
	if tags.TagReadWarning != nil {
		l.logger.Warn(
			"degraded tag read",
			"path", work.absolutePath,
			"err", tags.TagReadWarning,
		)

		metrics.addWarning(work.absolutePath, "tags", tags.TagReadWarning)
	}

	result.tags = tags
	result.audioProps = audioProps

	if skipDuration {
		result.lengthMillis = work.existingLength
	} else {
		result.lengthMillis = lengthMillis
	}

	return result, nil
}

// commitBatch wraps a slice of import results in a single database
// transaction, creating all related records and audio file entries.
// Individual file failures are logged and accumulated but do not
// abort the entire batch.  thumbChan dispatches thumbnail generation
// to the async worker pool.
func (l *Library) commitBatch(
	batch []importResult,
	cache *entityCache,
	metrics *ScanMetrics,
	added, updated, skipped *atomic.Int64,
	thumbChan chan<- thumbnailWork,
) error {
	tx, err := l.db.BeginTx()
	if err != nil {
		return fmt.Errorf("could not begin transaction: %w", err)
	}

	txq := l.db.Queries.WithTx(tx)

	for i := range batch {
		result := &batch[i]

		var saveErr error

		if result.needsUpdate {
			saveErr = l.updateAudioFileMetadata(
				txq, tx, cache, metrics, *result,
				thumbChan,
			)
			if saveErr == nil {
				updated.Add(1)
			}
		} else {
			saveErr = l.saveAudioFile(
				txq, tx, cache, metrics, *result,
				thumbChan,
			)
			if saveErr == nil {
				added.Add(1)
			}
		}

		if saveErr != nil {
			l.logger.Debug(
				"failed to save audio file",
				"path", result.absolutePath,
				"err", saveErr,
			)

			metrics.addWarning(
				result.absolutePath, "commit", saveErr,
			)

			// Count failed saves as skipped so the progress bar
			// advances (e.g. UNIQUE constraint from pre-existing tracks).
			skipped.Add(1)
		}
	}

	if commitErr := tx.Commit(); commitErr != nil {
		return fmt.Errorf(
			"could not commit batch of %d files: %w",
			len(batch), commitErr,
		)
	}

	return nil
}

// saveAudioFile writes audio file metadata to the database (new files).
func (l *Library) saveAudioFile(
	q *sqlcgen.Queries,
	tx *sql.Tx,
	cache *entityCache,
	metrics *ScanMetrics,
	result importResult,
	thumbChan chan<- thumbnailWork,
) error {
	l.logger.Debug(
		"saving audio file to db",
		"absolute-path", result.absolutePath,
		"track-length-millis", result.lengthMillis,
		"file-type", int64(
			slices.Index(
				metadata.SupportedFileExtensions,
				result.fileType,
			),
		),
	)

	// Process metadata and create related records.
	recordingID, err := l.processMetadata(
		q, tx, cache, metrics, result, thumbChan,
	)
	if err != nil {
		return fmt.Errorf("could not process metadata: %w", err)
	}

	props := result.audioProps
	if props == nil {
		props = &metadata.AudioProperties{}
	}

	tags := result.tags
	if tags == nil {
		tags = &metadata.TrackMetadata{}
	}

	basename := filepath.Base(result.absolutePath)

	groupKey := autotag.GroupKey(
		result.libraryID,
		result.absolutePath,
		tags.DiscNumber,
	)

	tagStatus := "untagged"
	if tags.RecordingMBID != "" {
		tagStatus = "user_confirmed"
	}

	af, err := q.CreateAudioFileWithGroupKey(
		l.ctx, sqlcgen.CreateAudioFileWithGroupKeyParams{
			FilePath:           result.absolutePath,
			LengthMilliseconds: result.lengthMillis,
			FileTypeID: int64(
				slices.Index(
					metadata.SupportedFileExtensions,
					result.fileType,
				),
			),
			RecordingID: recordingID,
			SampleRate:  int64(props.SampleRate),
			BitDepth:    int64(props.BitDepth),
			Channels:    int64(props.Channels),
			Bitrate:     int64(props.Bitrate),
			FileSize:    props.FileSize,
			Basename:    basename,
			LibraryID:   result.libraryID,
			GroupKey:    groupKey,
			TagStatus:   tagStatus,
			ModifiedAt:  result.modTime,
		})
	if err != nil {
		return fmt.Errorf(
			"could not save audio file to db: %w", err,
		)
	}

	if err := q.UpsertTaggingItemOnTrackAdd(
		l.ctx, sqlcgen.UpsertTaggingItemOnTrackAddParams{
			GroupKey:    groupKey,
			LibraryID:   result.libraryID,
			AlbumName:   tags.Album,
			AlbumArtist: tags.AlbumArtist,
			DiscNumber:  int64(tags.DiscNumber),
		},
	); err != nil {
		l.logger.Warn(
			"could not upsert tagging_items row",
			"path", result.absolutePath,
			"err", err,
		)

		metrics.addWarning(result.absolutePath, "commit", err)
	}

	// Index in FTS5 search_index.
	title := l.getRecordingName(tags, result.absolutePath)

	artistName := tags.Artist
	if artistName == "" {
		artistName = "Unknown Artist"
	}

	album := tags.Album

	// SAFETY: FTS5 virtual table, see search.go:InsertSearchIndex. All values parameterized.
	if _, err := tx.ExecContext(
		l.ctx,
		`INSERT INTO search_index(rowid, file_path, title, artist, album)
		 VALUES (?, ?, ?, ?, ?)`,
		af.ID, result.absolutePath, title, artistName, album,
	); err != nil {
		l.logger.Warn(
			"could not index audio file in FTS",
			"path", result.absolutePath,
			"err", err,
		)

		metrics.addWarning(result.absolutePath, "commit", err)
	}

	l.logger.Debug(
		"added audio file to library",
		"path", result.absolutePath,
	)

	return nil
}

// updateAudioFileMetadata updates an existing audio file with extracted metadata.
func (l *Library) updateAudioFileMetadata(
	q *sqlcgen.Queries,
	tx *sql.Tx,
	cache *entityCache,
	metrics *ScanMetrics,
	result importResult,
	thumbChan chan<- thumbnailWork,
) error {
	l.logger.Debug(
		"updating audio file metadata",
		"absolute-path", result.absolutePath,
		"file-id", result.existingFileID,
	)

	// Process metadata and create related records.
	recordingID, err := l.processMetadata(
		q, tx, cache, metrics, result, thumbChan,
	)
	if err != nil {
		return fmt.Errorf("could not process metadata: %w", err)
	}

	props := result.audioProps
	if props == nil {
		props = &metadata.AudioProperties{}
	}

	if err := q.UpdateAudioFileRecording(
		l.ctx, sqlcgen.UpdateAudioFileRecordingParams{
			RecordingID:        recordingID,
			SampleRate:         int64(props.SampleRate),
			BitDepth:           int64(props.BitDepth),
			Channels:           int64(props.Channels),
			Bitrate:            int64(props.Bitrate),
			FileSize:           props.FileSize,
			LengthMilliseconds: result.lengthMillis,
			ModifiedAt:         result.modTime,
			ID:                 result.existingFileID,
		}); err != nil {
		return fmt.Errorf(
			"could not update audio file recording: %w", err,
		)
	}

	// Re-index in FTS5 search_index.
	// With contentless_delete=1 (migration 8), DeleteSearchIndex
	// now works for individual row removal.  For scan updates we
	// still do delete + reinsert; Phase 16 will use the same
	// pattern for inline tag edits.
	tags := result.tags
	if tags == nil {
		tags = &metadata.TrackMetadata{}
	}

	if err := l.maybeRebindTaggingGroup(q, result, tags); err != nil {
		l.logger.Warn(
			"could not rebind tagging group after metadata update",
			"path", result.absolutePath,
			"err", err,
		)

		metrics.addWarning(result.absolutePath, "commit", err)
	}

	title := l.getRecordingName(tags, result.absolutePath)

	artistName := tags.Artist
	if artistName == "" {
		artistName = "Unknown Artist"
	}

	album := tags.Album

	// SAFETY: FTS5 virtual table, see search.go:InsertSearchIndex. All values parameterized.
	if _, err := tx.ExecContext(
		l.ctx,
		`INSERT INTO search_index(rowid, file_path, title, artist, album)
		 VALUES (?, ?, ?, ?, ?)`,
		result.existingFileID,
		result.absolutePath,
		title,
		artistName,
		album,
	); err != nil {
		l.logger.Warn(
			"could not index updated audio file in FTS",
			"path", result.absolutePath,
			"err", err,
		)

		metrics.addWarning(result.absolutePath, "commit", err)
	}

	l.logger.Debug(
		"updated audio file metadata",
		"path", result.absolutePath,
	)

	return nil
}

// processMetadata creates all related database records for metadata
// and returns the recording ID.  It uses the provided queries object
// (which may be transaction-scoped) and the entity cache to avoid
// redundant upserts for repeated artist/album/cover-art values.
// When thumbChan is non-nil, thumbnail generation is dispatched
// asynchronously.
func (l *Library) processMetadata(
	q *sqlcgen.Queries,
	tx *sql.Tx,
	cache *entityCache,
	metrics *ScanMetrics,
	result importResult,
	thumbChan chan<- thumbnailWork,
) (int64, error) {
	tags := result.tags
	if tags == nil {
		tags = &metadata.TrackMetadata{}
	}

	// 1. Handle cover art (if present).
	coverArtID := l.processCoverArt(
		q, cache, metrics, tags, thumbChan,
	)

	// 2. Get or create the artist credit for the track.  The credit
	// text is the full tagged string (e.g. "Lana Del Rey ft. Sean
	// Lennon") and is kept only for display; the artist *entity* it
	// links to is the primary artist, resolved cleanly by primaryArtist
	// so featured-artist credits don't fork into their own bogus artist
	// rows (all sharing the primary's single MBID).
	creditText := tags.Artist
	if creditText == "" {
		creditText = "Unknown Artist"
	}

	artistCredit, err := l.cachedUpsertArtistCredit(
		q, cache, creditText,
	)
	if err != nil {
		return 0, fmt.Errorf(
			"could not upsert artist credit: %w", err,
		)
	}

	primaryName, primaryMBID := primaryArtist(tags)

	l.cachedLinkArtist(q, cache, metrics, primaryName, artistCredit.ID)

	// 3. Get or create artist credit for album artist.
	albumArtistCreditID := l.resolveAlbumArtistCredit(
		q, cache, metrics, tags, artistCredit.ID,
	)

	// 4. Get or create release group (album).
	releaseGroupID := l.resolveReleaseGroup(
		q, cache, tags, albumArtistCreditID, coverArtID,
	)

	// 5. Create recording.
	recording, err := q.CreateRecordingFull(
		l.ctx, sqlcgen.CreateRecordingFullParams{
			Name: l.getRecordingName(
				tags, result.absolutePath,
			),
			ArtistCreditID: artistCredit.ID,
			TrackNumber:    toNullInt64(tags.TrackNumber),
			DiscNumber:     toNullInt64(tags.DiscNumber),
			Year:           toNullInt64(tags.Year),
			Genre:          toNullString(tags.Genre),
			Composer:       toNullString(tags.Composer),
			Lyrics:         toNullString(tags.Lyrics),
			Comment:        toNullString(tags.Comment),
		},
	)
	if err != nil {
		return 0, fmt.Errorf(
			"could not create recording: %w", err,
		)
	}

	// 6. Link recording to genres.
	l.linkRecordingGenres(q, cache, tags.Genre, recording.ID)

	// 7. Link recording to release group.
	if releaseGroupID.Valid {
		_, err = q.CreateReleaseGroupRecording(
			l.ctx,
			sqlcgen.CreateReleaseGroupRecordingParams{
				ReleaseGroupID: releaseGroupID.Int64,
				RecordingID:    recording.ID,
				TrackNumber:    toNullInt64(tags.TrackNumber),
				DiscNumber:     toNullInt64(tags.DiscNumber),
				TotalTracks:    toNullInt64(tags.TotalTracks),
			},
		)
		if err != nil {
			l.logger.Warn(
				"could not link recording to release group",
				"err", err,
			)
		}
	}

	// 7. Update MusicBrainz IDs (if present in tags).
	if releaseGroupID.Valid {
		l.updateMBIDs(tx, cache, tags, primaryName, primaryMBID, releaseGroupID.Int64, recording.ID)
	} else {
		l.updateMBIDs(tx, cache, tags, primaryName, primaryMBID, 0, recording.ID)
	}

	return recording.ID, nil
}

// updateMBIDs writes MusicBrainz IDs from audio file tags to the
// corresponding database entities.  Uses raw SQL since the sqlc
// queries predate the mbid columns.  Skips silently if tags have
// no MBIDs.
func (l *Library) updateMBIDs(
	tx *sql.Tx,
	cache *entityCache,
	tags *metadata.TrackMetadata,
	artistName string,
	artistMBID string,
	releaseGroupID int64,
	recordingID int64,
) {
	// Artist MBID (the primary artist's, resolved by primaryArtist).
	if artistMBID != "" {
		if artist, ok := cache.artists[artistName]; ok {
			_, _ = tx.ExecContext(l.ctx,
				"UPDATE artists SET mbid = ? WHERE id = ? AND (mbid IS NULL OR mbid = '')",
				artistMBID, artist.ID,
			)
		}
	}

	// Release group MBID.
	if tags.ReleaseGroupMBID != "" && releaseGroupID > 0 {
		_, _ = tx.ExecContext(l.ctx,
			"UPDATE release_groups SET mbid = ? WHERE id = ? AND (mbid IS NULL OR mbid = '')",
			tags.ReleaseGroupMBID, releaseGroupID,
		)
	} else if tags.ReleaseMBID != "" && releaseGroupID > 0 {
		// Many taggers write MUSICBRAINZ_ALBUMID (a specific release)
		// but not MUSICBRAINZ_RELEASEGROUPID (the abstract release
		// group everything else on this page is keyed by) — without
		// this, a genuinely MBID-tagged album shows as "library only"
		// forever. A scan can't afford a live MusicBrainz call to
		// resolve release->release-group here, so the release MBID is
		// stashed for `explore.Service.BackfillReleaseGroupMBIDs` to
		// resolve in the background, the same way discography
		// enrichment is deferred out of the scan path.
		_, _ = tx.ExecContext(l.ctx,
			"UPDATE release_groups SET pending_release_mbid = ? "+
				"WHERE id = ? AND (mbid IS NULL OR mbid = '') "+
				"AND (pending_release_mbid IS NULL OR pending_release_mbid = '')",
			tags.ReleaseMBID, releaseGroupID,
		)
	}

	// Recording MBID.
	if tags.RecordingMBID != "" && recordingID > 0 {
		_, _ = tx.ExecContext(l.ctx,
			"UPDATE recordings SET mbid = ? WHERE id = ? AND (mbid IS NULL OR mbid = '')",
			tags.RecordingMBID, recordingID,
		)
	}
}

// maybeRebindTaggingGroup recomputes the group key from the freshly
// extracted metadata and, if it differs from the row's current
// group_key, migrates the track: decrement the old group's count
// (dropping it if emptied), upsert the new group, and write the new
// key onto the audio_files row.  A no-op when the key is unchanged.
func (l *Library) maybeRebindTaggingGroup(
	q *sqlcgen.Queries,
	result importResult,
	tags *metadata.TrackMetadata,
) error {
	newKey := autotag.GroupKey(
		result.libraryID,
		result.absolutePath,
		tags.DiscNumber,
	)

	oldKey, err := q.GetAudioFileGroupKey(l.ctx, result.existingFileID)
	if err != nil {
		return fmt.Errorf("read existing group_key: %w", err)
	}

	if oldKey == newKey {
		return nil
	}

	if oldKey != "" {
		if err := q.DecrementTaggingItemTrackCount(l.ctx, oldKey); err != nil {
			return fmt.Errorf("decrement old group: %w", err)
		}

		if err := q.DeleteTaggingItemIfEmpty(l.ctx, oldKey); err != nil {
			return fmt.Errorf("cleanup old group: %w", err)
		}
	}

	if err := q.UpsertTaggingItemOnTrackAdd(
		l.ctx, sqlcgen.UpsertTaggingItemOnTrackAddParams{
			GroupKey:    newKey,
			LibraryID:   result.libraryID,
			AlbumName:   tags.Album,
			AlbumArtist: tags.AlbumArtist,
			DiscNumber:  int64(tags.DiscNumber),
		},
	); err != nil {
		return fmt.Errorf("upsert new group: %w", err)
	}

	if err := q.SetAudioFileGroupKey(
		l.ctx, sqlcgen.SetAudioFileGroupKeyParams{
			GroupKey: newKey,
			ID:       result.existingFileID,
		},
	); err != nil {
		return fmt.Errorf("write new group_key: %w", err)
	}

	return nil
}

// processCoverArt saves cover art to disk and upserts the DB record,
// using the cache to skip work for previously seen images.  When
// thumbChan is non-nil, thumbnail generation is dispatched to the
// async worker pool.
func (l *Library) processCoverArt(
	q *sqlcgen.Queries,
	cache *entityCache,
	metrics *ScanMetrics,
	tags *metadata.TrackMetadata,
	thumbChan chan<- thumbnailWork,
) sql.NullInt64 {
	if tags.Picture == nil {
		return sql.NullInt64{}
	}

	coverPath, err := l.saveCoverArt(
		tags.Picture, metrics, thumbChan,
	)
	if err != nil {
		l.logger.Warn("could not save cover art", "err", err)

		return sql.NullInt64{}
	}

	if coverPath == "" {
		return sql.NullInt64{}
	}

	// Check cache first.
	if cached, ok := cache.coverArt[coverPath]; ok {
		return sql.NullInt64{Int64: cached.ID, Valid: true}
	}

	ca, err := q.UpsertCoverArt(l.ctx, sqlcgen.UpsertCoverArtParams{
		IsEmbedded: true,
		FilePath:   coverPath,
		MimeType:   tags.Picture.MIMEType,
	})
	if err != nil {
		l.logger.Warn(
			"could not create cover art record", "err", err,
		)

		return sql.NullInt64{}
	}

	cache.coverArt[coverPath] = ca

	return sql.NullInt64{Int64: ca.ID, Valid: true}
}

// cachedUpsertArtistCredit returns the artist credit for the given
// name, using the cache when possible.
func (l *Library) cachedUpsertArtistCredit(
	q *sqlcgen.Queries,
	cache *entityCache,
	name string,
) (sqlcgen.ArtistCredit, error) {
	if cached, ok := cache.artistCredits[name]; ok {
		return cached, nil
	}

	ac, err := q.UpsertArtistCredit(l.ctx, name)
	if err != nil {
		return sqlcgen.ArtistCredit{}, err
	}

	cache.artistCredits[name] = ac

	return ac, nil
}

// cachedLinkArtist upserts the artist record and creates the
// artist-credit-artist link, skipping work already done.
// UNIQUE constraint violations are silently ignored (link already
// exists in the database).  Other errors are recorded as scan warnings.
func (l *Library) cachedLinkArtist(
	q *sqlcgen.Queries,
	cache *entityCache,
	metrics *ScanMetrics,
	name string,
	creditID int64,
) {
	artist, ok := cache.artists[name]
	if !ok {
		var err error

		artist, err = q.UpsertArtist(l.ctx, name)
		if err != nil {
			l.logger.Warn(
				"could not upsert artist", "err", err,
			)

			return
		}

		cache.artists[name] = artist
	}

	linkKey := fmt.Sprintf("%d:%d", artist.ID, creditID)
	if _, done := cache.linkedCredits[linkKey]; done {
		return
	}

	_, err := q.CreateArtistCreditArtist(
		l.ctx,
		sqlcgen.CreateArtistCreditArtistParams{
			ArtistID: artist.ID,
			CreditID: creditID,
		},
	)
	if err != nil {
		if !database.IsUniqueViolation(err) {
			l.logger.Warn(
				"could not link artist to credit",
				"artist", name,
				"creditID", creditID,
				"err", err,
			)

			metrics.addWarning(
				name, "commit",
				fmt.Errorf(
					"artist-credit link failed for %q: %w",
					name, err,
				),
			)
		}

		// UNIQUE violation: link already exists in DB, not an error.
		return
	}

	cache.linkedCredits[linkKey] = struct{}{}
}

// cachedUpsertGenre returns the genre for the given name, using
// the cache when possible.
func (l *Library) cachedUpsertGenre(
	q *sqlcgen.Queries,
	cache *entityCache,
	name string,
) (sqlcgen.Genre, error) {
	if cached, ok := cache.genres[name]; ok {
		return cached, nil
	}

	genre, err := q.UpsertGenre(l.ctx, name)
	if err != nil {
		return sqlcgen.Genre{}, err
	}

	cache.genres[name] = genre

	return genre, nil
}

// linkRecordingGenres parses the raw genre string, upserts each
// individual genre, and creates the recording-genre associations.
func (l *Library) linkRecordingGenres(
	q *sqlcgen.Queries,
	cache *entityCache,
	rawGenre string,
	recordingID int64,
) {
	genres := metadata.ParseGenres(rawGenre)

	for _, name := range genres {
		genre, err := l.cachedUpsertGenre(q, cache, name)
		if err != nil {
			l.logger.Warn(
				"could not upsert genre",
				"genre", name,
				"err", err,
			)

			continue
		}

		err = q.CreateRecordingGenre(
			l.ctx,
			sqlcgen.CreateRecordingGenreParams{
				RecordingID: recordingID,
				GenreID:     genre.ID,
			},
		)
		if err != nil {
			l.logger.Warn(
				"could not link recording to genre",
				"genre", name,
				"recordingID", recordingID,
				"err", err,
			)
		}
	}
}

// resolveAlbumArtistCredit returns the album artist credit ID.
// When the AlbumArtist tag is absent or matches the track artist,
// the track artist credit is reused.
func (l *Library) resolveAlbumArtistCredit(
	q *sqlcgen.Queries,
	cache *entityCache,
	metrics *ScanMetrics,
	tags *metadata.TrackMetadata,
	trackArtistCreditID int64,
) sql.NullInt64 {
	if tags.AlbumArtist == "" || tags.AlbumArtist == tags.Artist {
		return sql.NullInt64{
			Int64: trackArtistCreditID, Valid: true,
		}
	}

	albumArtistCredit, err := l.cachedUpsertArtistCredit(
		q, cache, tags.AlbumArtist,
	)
	if err != nil {
		l.logger.Warn(
			"could not upsert album artist credit", "err", err,
		)

		return sql.NullInt64{}
	}

	l.cachedLinkArtist(
		q, cache, metrics, tags.AlbumArtist, albumArtistCredit.ID,
	)

	return sql.NullInt64{
		Int64: albumArtistCredit.ID, Valid: true,
	}
}

// resolveReleaseGroup returns the release group ID for the album,
// using the cache when possible.
func (l *Library) resolveReleaseGroup(
	q *sqlcgen.Queries,
	cache *entityCache,
	tags *metadata.TrackMetadata,
	albumArtistCreditID sql.NullInt64,
	coverArtID sql.NullInt64,
) sql.NullInt64 {
	if tags.Album == "" {
		return sql.NullInt64{}
	}

	// Build composite cache key: "albumName\x00artistCreditID"
	// (or "albumName\x00-1" if no artist).  This prevents albums
	// with the same name by different artists from colliding.
	artistID := int64(-1)
	if albumArtistCreditID.Valid {
		artistID = albumArtistCreditID.Int64
	}

	cacheKey := fmt.Sprintf("%s\x00%d", tags.Album, artistID)

	// Check cache first.
	if cached, ok := cache.releaseGroups[cacheKey]; ok {
		// If the cached release group lacks cover art and we now
		// have it, update it.
		if coverArtID.Valid && !cached.CoverArtID.Valid {
			err := q.UpdateReleaseGroupCoverArt(
				l.ctx,
				sqlcgen.UpdateReleaseGroupCoverArtParams{
					CoverArtID: coverArtID,
					ID:         cached.ID,
				},
			)
			if err != nil {
				l.logger.Warn(
					"could not update release group cover art",
					"err", err,
				)
			} else {
				cached.CoverArtID = coverArtID
				cache.releaseGroups[cacheKey] = cached
			}
		}

		return sql.NullInt64{Int64: cached.ID, Valid: true}
	}

	rg, err := q.UpsertReleaseGroup(
		l.ctx, sqlcgen.UpsertReleaseGroupParams{
			Name:                tags.Album,
			AlbumArtistCreditID: albumArtistCreditID,
			Year:                toNullInt64(tags.Year),
		},
	)
	if err != nil {
		l.logger.Warn(
			"could not upsert release group", "err", err,
		)

		return sql.NullInt64{}
	}

	// Update cover art if this album doesn't have one yet.
	if coverArtID.Valid && !rg.CoverArtID.Valid {
		err := q.UpdateReleaseGroupCoverArt(
			l.ctx,
			sqlcgen.UpdateReleaseGroupCoverArtParams{
				CoverArtID: coverArtID,
				ID:         rg.ID,
			},
		)
		if err != nil {
			l.logger.Warn(
				"could not update release group cover art",
				"err", err,
			)
		} else {
			rg.CoverArtID = coverArtID
		}
	}

	cache.releaseGroups[cacheKey] = rg

	return sql.NullInt64{Int64: rg.ID, Valid: true}
}

// getRecordingName returns the track title, or falls back to the filename.
func (l *Library) getRecordingName(tags *metadata.TrackMetadata, filePath string) string {
	if tags.Title != "" {
		return tags.Title
	}
	// Fallback to filename without extension
	base := filepath.Base(filePath)

	return strings.TrimSuffix(base, filepath.Ext(base))
}

// toNullInt64 converts an int to sql.NullInt64, treating 0 as null.
func toNullInt64(v int) sql.NullInt64 {
	if v == 0 {
		return sql.NullInt64{}
	}

	return sql.NullInt64{Int64: int64(v), Valid: true}
}

// toNullString converts a string to sql.NullString, treating empty as null.
func toNullString(v string) sql.NullString {
	if v == "" {
		return sql.NullString{}
	}

	return sql.NullString{String: v, Valid: true}
}
