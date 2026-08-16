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

	"github.com/wailsapp/wails/v3/pkg/application"
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

// entityCache holds recently resolved database rows so repeated
// upserts for the same artist/album/cover art within a scan can be
// served from memory instead of hitting the database.
// It is only accessed from the single DB-writer goroutine and
// therefore needs no synchronisation.
type entityCache struct {
	artists  map[string]sqlcgen.Artist
	albums   map[string]sqlcgen.Album
	coverArt map[string]sqlcgen.CoverArt
	genres   map[string]sqlcgen.Genre
}

func newEntityCache() *entityCache {
	return &entityCache{
		artists:  make(map[string]sqlcgen.Artist),
		albums:   make(map[string]sqlcgen.Album),
		coverArt: make(map[string]sqlcgen.CoverArt),
		genres:   make(map[string]sqlcgen.Genre),
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
//
//wails:ignore // internal wiring, not part of the app's IPC surface.
func (l *Library) SetRescanHooks(h RescanHooks) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.rescanHooks = h
}

// SetScanHooks provides optional hooks for cross-cutting
// orchestration after each library scan.
//
//wails:ignore // internal wiring, not part of the app's IPC surface.
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
//
//wails:ignore // internal wiring, not part of the app's IPC surface.
func (l *Library) AcquirePipelineLock() { l.pipelineMu.Lock() }

// ReleasePipelineLock releases the pipeline mutex after a tag write.
//
//wails:ignore // internal wiring, not part of the app's IPC surface.
func (l *Library) ReleasePipelineLock() { l.pipelineMu.Unlock() }

// ServiceStartup is v3's service lifecycle hook: it runs once the
// runtime exists, and ctx is cancelled when the app shuts down.  It
// replaces v2's SetContext, which had to be called by hand from
// OnStartup and was exported, so it was also bound to the frontend.
func (l *Library) ServiceStartup(
	ctx context.Context,
	_ application.ServiceOptions,
) error {
	l.mu.Lock()
	l.ctx = ctx
	l.mu.Unlock()

	l.registerEventHandlers()

	return nil
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

	// Paths the user has removed from the library.  Loaded once per
	// scan: the walk consults it per file, and the counts above and
	// below must agree with it or the progress bar and the soft scan
	// both describe a library that is not the one being built.
	excluded := l.excludedPathSet(libraryID)

	totalFiles := countAudioFiles(basePath, excluded)

	l.logger.Debug(
		"pre-walk file count complete",
		"total", totalFiles,
	)

	// --- Phase 1: load existing files from DB (per-library) ---
	loadStart := time.Now()

	existingFiles, err := l.db.Queries.GetAudioFilesInLibrary(
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

				// The user removed this path from the library.  Leaving
				// it out of existingPaths' LoadAndDelete as well is
				// deliberate: if a row somehow exists for an excluded
				// path, orphan cleanup below deletes it, which is the
				// state the user asked for.
				if isExcluded(excluded, absoluteFilePath) {
					l.logger.Debug(
						"skipping excluded path",
						"path", absoluteFilePath,
					)

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

					// A file with no title has never had its tags read
					// (the row exists, the metadata pass did not run),
					// which is the same "needs metadata" signal the
					// recording_id == 0 test used to be.
					if audioFile.Title == "" || contentChanged {
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
		l.pruneEmptyEntities()
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

// pruneEmptyEntities removes albums and artists left with nothing
// pointing at them.
//
// This used to be four sweeps in dependency order - recordings and
// their two link tables, then release groups with no recordings, then
// artist credits, then artists - because deleting an audio_files row
// cascaded to none of them.  Two of those tables are gone and the third
// (file_genres) cascades, so what is left is the two tables that
// genuinely outlive a file: an album whose last track was removed, and
// an artist whose last album was.
//
// Best effort: logs and continues rather than failing the scan.
func (l *Library) pruneEmptyEntities() {
	tx, err := l.db.BeginTx()
	if err != nil {
		l.logger.Warn("could not begin entity cleanup transaction", "err", err)

		return
	}

	defer func() { _ = tx.Rollback() }() // no-op after commit

	txq := l.db.Queries.WithTx(tx)

	albumIDs, err := txq.GetEmptyAlbumIDs(l.ctx)
	if err != nil {
		l.logger.Warn("could not find empty albums", "err", err)

		return
	}

	for _, id := range albumIDs {
		if err := txq.DeleteAlbum(l.ctx, id); err != nil {
			l.logger.Warn("could not delete empty album", "id", id, "err", err)
		}
	}

	// Artists after albums: an artist is unreferenced only once the
	// albums pointing at it are gone.
	artistIDs, err := txq.GetUnreferencedArtistIDs(l.ctx)
	if err != nil {
		l.logger.Warn("could not find unreferenced artists", "err", err)

		return
	}

	for _, id := range artistIDs {
		if err := txq.DeleteArtist(l.ctx, id); err != nil {
			l.logger.Warn("could not delete unreferenced artist", "id", id, "err", err)
		}
	}

	genreIDs, err := txq.GetUnusedGenreIDs(l.ctx)
	if err != nil {
		l.logger.Warn("could not find unused genres", "err", err)

		return
	}

	for _, id := range genreIDs {
		if err := txq.DeleteGenre(l.ctx, id); err != nil {
			l.logger.Warn("could not delete unused genre", "id", id, "err", err)
		}
	}

	if err := tx.Commit(); err != nil {
		l.logger.Warn("could not commit entity cleanup", "err", err)

		return
	}

	if len(albumIDs) > 0 || len(artistIDs) > 0 || len(genreIDs) > 0 {
		l.logger.Info("pruned empty library entities",
			"albums", len(albumIDs),
			"artists", len(artistIDs),
			"genres", len(genreIDs),
		)
	}
}

// countAudioFiles performs a fast walk of the library directory,
// counting only files with supported audio extensions.  No per-file
// I/O is performed — this reads only directory entries.
func countAudioFiles(basePath string, excluded map[string]struct{}) int64 {
	var count int64

	_ = fs.WalkDir(
		os.DirFS(basePath), ".",
		func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}

			ext := filepath.Ext(d.Name())
			if _, ok := metadata.GetSupportedFileType(ext); !ok {
				return nil
			}

			if isExcluded(excluded, filepath.Join(basePath, path)) {
				return nil
			}

			count++

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
//
// Both walks take the library's excluded paths and skip them, because
// both answer "how many files would a scan import", not "how many
// files are there".
func surveyAudioFiles(
	basePath string,
	excluded map[string]struct{},
) (count, maxModTime int64) {
	_ = fs.WalkDir(
		os.DirFS(basePath), ".",
		func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}

			ext := filepath.Ext(d.Name())
			if _, ok := metadata.GetSupportedFileType(ext); !ok {
				return nil
			}

			// An excluded path is not a file this scan would import,
			// so it must not be counted: the soft scan compares this
			// count against the database's, and a permanent
			// disagreement queues a full scan on every launch.
			if isExcluded(excluded, filepath.Join(basePath, path)) {
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

	// Resolve the rows this file shares with others: its artist and
	// its album.  Everything else about it is a column on the file.
	entities := l.resolveTagEntities(q, cache, metrics, result, thumbChan)

	props := result.audioProps
	if props == nil {
		props = &metadata.AudioProperties{}
	}

	tags := result.tags
	if tags == nil {
		tags = &metadata.TrackMetadata{}
	}

	groupKey := autotag.GroupKey(
		result.libraryID,
		result.absolutePath,
		tags.DiscNumber,
	)

	tagStatus := "untagged"
	if tags.RecordingMBID != "" {
		tagStatus = "user_confirmed"
	}

	artistCredit := tags.Artist
	if artistCredit == "" {
		artistCredit = "Unknown Artist"
	}

	title := l.getRecordingName(tags, result.absolutePath)

	af, err := q.CreateAudioFile(
		l.ctx, sqlcgen.CreateAudioFileParams{
			FilePath:  result.absolutePath,
			LibraryID: result.libraryID,
			FileTypeID: int64(
				slices.Index(
					metadata.SupportedFileExtensions,
					result.fileType,
				),
			),
			LengthMilliseconds: result.lengthMillis,
			SampleRate:         int64(props.SampleRate),
			BitDepth:           int64(props.BitDepth),
			Channels:           int64(props.Channels),
			Bitrate:            int64(props.Bitrate),
			FileSize:           props.FileSize,
			Title:              title,
			ArtistCredit:       artistCredit,
			ArtistID:           entities.artistID,
			AlbumID:            entities.albumID,
			TrackNumber:        toNullInt64(tags.TrackNumber),
			DiscNumber:         toNullInt64(tags.DiscNumber),
			TotalTracks:        toNullInt64(tags.TotalTracks),
			Year:               toNullInt64(tags.Year),
			Composer:           tags.Composer,
			Comment:            tags.Comment,
			RecordingMbid:      toNullString(tags.RecordingMBID),
			Basename:           filepath.Base(result.absolutePath),
			GroupKey:           groupKey,
			ModifiedAt:         result.modTime,
			TagStatus:          tagStatus,
		})
	if err != nil {
		return fmt.Errorf(
			"could not save audio file to db: %w", err,
		)
	}

	l.linkFileGenres(q, cache, tags.Genre, af.ID)

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
	// SAFETY: FTS5 virtual table, see search.go:InsertSearchIndex. All values parameterized.
	if _, err := tx.ExecContext(
		l.ctx,
		`INSERT INTO search_index(rowid, file_path, title, artist, album)
		 VALUES (?, ?, ?, ?, ?)`,
		af.ID, result.absolutePath, title, artistCredit, tags.Album,
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

	tags := result.tags
	if tags == nil {
		tags = &metadata.TrackMetadata{}
	}

	// Resolve the file's artist and album from the tags as they are
	// now.  This used to create a *new* recording row and repoint the
	// file at it, abandoning the old one - which is where 812 orphaned
	// rows and every phantom "you own this" in a real library came
	// from.  The file's tags are its own columns, so a retag is an
	// UPDATE and there is nothing left behind to strand.
	entities := l.resolveTagEntities(q, cache, metrics, result, thumbChan)

	props := result.audioProps
	if props == nil {
		props = &metadata.AudioProperties{}
	}

	artistCredit := tags.Artist
	if artistCredit == "" {
		artistCredit = "Unknown Artist"
	}

	if err := q.UpdateAudioFileTags(
		l.ctx, sqlcgen.UpdateAudioFileTagsParams{
			Title:              l.getRecordingName(tags, result.absolutePath),
			ArtistCredit:       artistCredit,
			ArtistID:           entities.artistID,
			AlbumID:            entities.albumID,
			TrackNumber:        toNullInt64(tags.TrackNumber),
			DiscNumber:         toNullInt64(tags.DiscNumber),
			TotalTracks:        toNullInt64(tags.TotalTracks),
			Year:               toNullInt64(tags.Year),
			Composer:           tags.Composer,
			Comment:            tags.Comment,
			RecordingMbid:      toNullString(tags.RecordingMBID),
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
			"could not update audio file tags: %w", err,
		)
	}

	// Genres are relinked wholesale: the tag is the whole truth about
	// which genres a file carries, so a genre dropped from the tag has
	// to be dropped from the link table too.
	if err := q.DeleteFileGenres(l.ctx, result.existingFileID); err != nil {
		l.logger.Warn("could not clear file genres",
			"path", result.absolutePath, "err", err)
	}

	l.linkFileGenres(q, cache, tags.Genre, result.existingFileID)

	// Re-index in FTS5 search_index.
	// With contentless_delete=1 (migration 8), DeleteSearchIndex
	// now works for individual row removal.  For scan updates we
	// still do delete + reinsert; Phase 16 will use the same
	// pattern for inline tag edits.
	// A file another tagger stamped with MBIDs since import is only
	// discovered here — the insert path is what sets tag_status, so
	// without this the file stays 'untagged' forever and its folder
	// keeps asking to be tagged.
	if tags.RecordingMBID != "" {
		if err := q.PromoteAudioFileTagStatusIfUntagged(
			l.ctx, result.existingFileID,
		); err != nil {
			l.logger.Warn(
				"could not promote tag status after metadata update",
				"path", result.absolutePath,
				"err", err,
			)
		}
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

// trackEntities are the shared rows a file's tags resolve to: the
// artist and album it belongs to, and the cover art of that album.
//
// This replaced processMetadata, which created a `recordings` row per
// file plus an artist_credit, an artist_credit_artist link and a
// release_group_recordings link, then wrote MBIDs onto three of them
// with raw SQL.  A file's tags are columns on the file now, so the only
// rows that still have to be *shared* are the two that genuinely are:
// the album several files belong to, and the artist several albums do.
type trackEntities struct {
	artistID sql.NullInt64
	albumID  sql.NullInt64
}

// resolveTagEntities upserts the artist and album a file's tags name,
// and returns their ids for the file row.
func (l *Library) resolveTagEntities(
	q *sqlcgen.Queries,
	cache *entityCache,
	metrics *ScanMetrics,
	result importResult,
	thumbChan chan<- thumbnailWork,
) trackEntities {
	tags := result.tags
	if tags == nil {
		tags = &metadata.TrackMetadata{}
	}

	coverArtID := l.processCoverArt(q, cache, metrics, tags, thumbChan)

	// The track artist.  primaryArtist collapses a featured-artist
	// credit to the artist the MBIDs actually identify, so "A feat. B"
	// does not fork into its own artist row sharing A's MBID.
	primaryName, primaryMBID := primaryArtist(tags)
	artist := l.cachedUpsertArtist(q, cache, primaryName, primaryMBID)

	entities := trackEntities{}
	if artist.ID > 0 {
		entities.artistID = sql.NullInt64{Int64: artist.ID, Valid: true}
	}

	if tags.Album == "" {
		return entities
	}

	// The album artist, which is the track artist unless the tags say
	// otherwise.
	albumCredit := tags.AlbumArtist
	albumArtistID := entities.artistID

	if albumCredit == "" || albumCredit == tags.Artist {
		albumCredit = tags.Artist
	} else {
		albumArtist := l.cachedUpsertArtist(q, cache, albumCredit, tags.AlbumArtistMBID)
		if albumArtist.ID > 0 {
			albumArtistID = sql.NullInt64{Int64: albumArtist.ID, Valid: true}
		}
	}

	album := l.cachedUpsertAlbum(q, cache, albumParams{
		name:       tags.Album,
		credit:     albumCredit,
		artistID:   albumArtistID,
		year:       toNullInt64(tags.Year),
		coverArtID: coverArtID,
	})
	if album.ID == 0 {
		return entities
	}

	entities.albumID = sql.NullInt64{Int64: album.ID, Valid: true}
	l.stampAlbumMBID(q, cache, album, tags)

	return entities
}

// albumParams is what an album upsert needs from a file's tags.
type albumParams struct {
	name       string
	credit     string
	artistID   sql.NullInt64
	year       sql.NullInt64
	coverArtID sql.NullInt64
}

// cachedUpsertArtist returns the artist row for a name, upserting it
// once per scan.  The MBID is written on the way in rather than by a
// separate UPDATE afterwards.
func (l *Library) cachedUpsertArtist(
	q *sqlcgen.Queries,
	cache *entityCache,
	name, mbid string,
) sqlcgen.Artist {
	if name == "" {
		name = "Unknown Artist"
	}

	if cached, ok := cache.artists[name]; ok {
		if mbid != "" && !cached.Mbid.Valid {
			if err := q.SetArtistMBID(l.ctx, sqlcgen.SetArtistMBIDParams{
				Mbid: sql.NullString{String: mbid, Valid: true},
				ID:   cached.ID,
			}); err == nil {
				cached.Mbid = sql.NullString{String: mbid, Valid: true}
				cache.artists[name] = cached
			}
		}

		return cached
	}

	artist, err := q.UpsertArtist(l.ctx, sqlcgen.UpsertArtistParams{
		Name: name,
		Mbid: toNullString(mbid),
	})
	if err != nil {
		l.logger.Warn("could not upsert artist", "artist", name, "err", err)

		return sqlcgen.Artist{}
	}

	cache.artists[name] = artist

	return artist
}

// cachedUpsertAlbum returns the album row for (name, credit), upserting
// it once per scan and filling in cover art the first time a file
// carries some.
func (l *Library) cachedUpsertAlbum(
	q *sqlcgen.Queries,
	cache *entityCache,
	p albumParams,
) sqlcgen.Album {
	// The key is the album's identity - name and credit - so two
	// albums of the same name by different artists do not collide.
	cacheKey := p.name + "\x00" + p.credit

	if cached, ok := cache.albums[cacheKey]; ok {
		if p.coverArtID.Valid && !cached.CoverArtID.Valid {
			if err := q.SetAlbumCoverArt(l.ctx, sqlcgen.SetAlbumCoverArtParams{
				CoverArtID: p.coverArtID,
				ID:         cached.ID,
			}); err != nil {
				l.logger.Warn("could not update album cover art", "err", err)
			} else {
				cached.CoverArtID = p.coverArtID
				cache.albums[cacheKey] = cached
			}
		}

		return cached
	}

	album, err := q.UpsertAlbum(l.ctx, sqlcgen.UpsertAlbumParams{
		Name:         p.name,
		ArtistCredit: p.credit,
		ArtistID:     p.artistID,
		Year:         p.year,
		CoverArtID:   p.coverArtID,
	})
	if err != nil {
		l.logger.Warn("could not upsert album", "album", p.name, "err", err)

		return sqlcgen.Album{}
	}

	cache.albums[cacheKey] = album

	return album
}

// stampAlbumMBID writes the album's MusicBrainz identity from the tags.
//
// Many taggers write MUSICBRAINZ_ALBUMID (a specific release) but not
// MUSICBRAINZ_RELEASEGROUPID (the abstract release group everything
// else is keyed by) - without the second branch, a genuinely MBID-
// tagged album shows as "library only" forever.  A scan cannot afford a
// live MusicBrainz call to resolve release -> release group, so the
// release MBID is stashed for BackfillReleaseGroupMBIDs to resolve in
// the background.
func (l *Library) stampAlbumMBID(
	q *sqlcgen.Queries,
	cache *entityCache,
	album sqlcgen.Album,
	tags *metadata.TrackMetadata,
) {
	if album.Mbid.Valid && album.Mbid.String != "" {
		return
	}

	switch {
	case tags.ReleaseGroupMBID != "":
		if err := q.SetAlbumMBID(l.ctx, sqlcgen.SetAlbumMBIDParams{
			Mbid: sql.NullString{String: tags.ReleaseGroupMBID, Valid: true},
			ID:   album.ID,
		}); err != nil {
			l.logger.Warn("could not set album mbid", "err", err)

			return
		}

		album.Mbid = sql.NullString{String: tags.ReleaseGroupMBID, Valid: true}
		cache.albums[album.Name+"\x00"+album.ArtistCredit] = album
	case tags.ReleaseMBID != "" && !album.PendingReleaseMbid.Valid:
		if err := q.SetAlbumPendingReleaseMBID(
			l.ctx, sqlcgen.SetAlbumPendingReleaseMBIDParams{
				PendingReleaseMbid: sql.NullString{String: tags.ReleaseMBID, Valid: true},
				ID:                 album.ID,
			},
		); err != nil {
			l.logger.Warn("could not set pending release mbid", "err", err)
		}
	}
}

// linkFileGenres parses the raw genre string and links the file to each
// genre it names.
func (l *Library) linkFileGenres(
	q *sqlcgen.Queries,
	cache *entityCache,
	rawGenre string,
	audioFileID int64,
) {
	for _, name := range metadata.ParseGenres(rawGenre) {
		genre, err := l.cachedUpsertGenre(q, cache, name)
		if err != nil {
			l.logger.Warn("could not upsert genre", "genre", name, "err", err)

			continue
		}

		if err := q.LinkFileGenre(l.ctx, sqlcgen.LinkFileGenreParams{
			AudioFileID: audioFileID,
			GenreID:     genre.ID,
		}); err != nil {
			l.logger.Warn("could not link file to genre",
				"genre", name, "audioFileID", audioFileID, "err", err)
		}
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
