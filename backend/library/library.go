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

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sync/errgroup"

	"yellowjacket/backend/database"
	"yellowjacket/backend/database/sql/sqlcgen"
	"yellowjacket/backend/events"
	"yellowjacket/backend/metadata"
	"yellowjacket/backend/system"
)

// scanBatchSize controls how many files are committed in a single
// database transaction during a scan.  Larger batches amortize
// SQLite's fsync cost but increase the blast radius of a failed commit.
const scanBatchSize = 50

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

	// Scan queue fields — protected by mu.
	scanQueue              []scanQueueEntry
	currentScanLibraryID   int64
	currentScanLibraryName string

	// removalHooks holds callbacks for cross-cutting concerns during
	// library removal (e.g. stopping playback, compacting queue).
	removalHooks RemovalHooks
}

// SetRescanHooks provides optional hooks for cross-cutting
// orchestration during FullRescan.
func (l *Library) SetRescanHooks(h RescanHooks) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.rescanHooks = h
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

// SetContext sets the Wails runtime context and registers event handlers.
func (l *Library) SetContext(ctx context.Context) {
	l.mu.Lock()
	l.ctx = ctx
	l.mu.Unlock()

	l.registerEventHandlers()
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
	metrics := newScanMetrics()
	metrics.LibraryID = libraryID
	metrics.LibraryName = libraryName
	scanStart := time.Now()

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

	runtime.EventsEmit(l.ctx, events.LibraryScanStarted, map[string]any{
		"libraryId":   libraryID,
		"libraryName": libraryName,
	})

	basePath := libraryPath

	// --- Pre-walk: count audio files for progress reporting ---
	runtime.EventsEmit(l.ctx, events.LibraryScanProgress,
		mkProgress("counting", 0, 0, 0, 0, 0),
	)

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

	var added, skipped, updated atomic.Int64

	var scanErr error

	var errMu sync.Mutex

	// --- Phase 2: directory walk ---
	walkStart := time.Now()

	go func() {
		defer func() {
			metrics.WalkDuration = time.Since(walkStart)

			close(workChan)
		}()

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

				if d.IsDir() {
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

				// Check if file already exists in database.
				if existing, exists := existingPaths.LoadAndDelete(absoluteFilePath); exists {
					audioFile := existing.(sqlcgen.AudioFile)

					if audioFile.RecordingID == 0 {
						l.logger.Debug(
							"file needs metadata update",
							"path", absoluteFilePath,
						)

						select {
						case workChan <- scanWork{
							absolutePath:   absoluteFilePath,
							fileType:       fileType,
							existingFileID: audioFile.ID,
							needsUpdate:    true,
							existingLength: audioFile.LengthMilliseconds,
						}:
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
				}:
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

				runtime.EventsEmit(
					l.ctx,
					events.LibraryScanProgress,
					mkProgress(
						"scanning", totalFiles,
						a+s+u, a, s, u,
					),
				)
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
				&added, &updated,
				thumbChan,
			); batchErr != nil {
				errMu.Lock()
				scanErr = errors.Join(scanErr, batchErr)
				errMu.Unlock()
			}

			metrics.BatchCommits += time.Since(batchStart)
			batch = batch[:0]
		}

		for result := range resultChan {
			// Thread library ID into each result for saveAudioFile.
			result.libraryID = libraryID

			if !dbStarted {
				dbStartVal = time.Now()
				dbStarted = true
			}

			batch = append(batch, result)
			if len(batch) >= scanBatchSize {
				flushBatch()
			}
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

	runtime.EventsEmit(l.ctx, events.LibraryScanProgress,
		mkProgress("scanning", totalFiles, a+s+u, a, s, u),
	)

	// Close thumbnail channel and wait for all thumbnail workers
	// to finish.  The DB writer has stopped sending work at this
	// point so it is safe to close.
	thumbStart := time.Now()

	runtime.EventsEmit(l.ctx, events.LibraryScanProgress,
		mkProgress("thumbnails", totalFiles, a+s+u, a, s, u),
	)

	close(thumbChan)
	thumbWg.Wait()

	metrics.ThumbnailWallClock = time.Since(thumbStart)

	// Skip orphan cleanup if the scan was cancelled — existingPaths
	// still contains unvisited files that would be incorrectly deleted.
	cancelled := scanCtx.Err() != nil

	var removed atomic.Int64

	if cancelled {
		metrics.Cancelled = true

		l.logger.Info("scan cancelled, skipping orphan cleanup")
	} else {
		// --- Phase 5: orphan cleanup ---
		runtime.EventsEmit(l.ctx, events.LibraryScanProgress,
			mkProgress("orphans", totalFiles, a+s+u, a, s, u),
		)

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
	}

	// --- Phase 6: post-scan variant generation ---
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

	if cancelled {
		runtime.EventsEmit(
			l.ctx, events.LibraryScanCancelled, metrics,
		)
	} else {
		runtime.EventsEmit(
			l.ctx, events.LibraryScanComplete, metrics,
		)
	}

	return metrics
}

// progressInterval controls how often scan progress events are
// emitted to the frontend.
const progressInterval = 300 * time.Millisecond

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
	}

	// Skip duration decode if we already have it from a previous import.
	skipDuration := work.needsUpdate && work.existingLength > 0

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
	added, updated *atomic.Int64,
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
		q, cache, metrics, result, thumbChan,
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

	af, err := q.CreateAudioFile(
		l.ctx, sqlcgen.CreateAudioFileParams{
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
		})
	if err != nil {
		return fmt.Errorf(
			"could not save audio file to db: %w", err,
		)
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
		q, cache, metrics, result, thumbChan,
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
			RecordingID: recordingID,
			SampleRate:  int64(props.SampleRate),
			BitDepth:    int64(props.BitDepth),
			Channels:    int64(props.Channels),
			Bitrate:     int64(props.Bitrate),
			FileSize:    props.FileSize,
			ID:          result.existingFileID,
		}); err != nil {
		return fmt.Errorf(
			"could not update audio file recording: %w", err,
		)
	}

	// Re-index in FTS5 search_index.
	// Contentless FTS5 (content='') does not support DELETE, so we
	// cannot remove the old entry.  Inserting a new row with the
	// same rowid is accepted by FTS5 — the old entry becomes stale
	// but harmless (search JOINs against track_metadata filter it).
	// The index is fully rebuilt during FullRescan.
	tags := result.tags
	if tags == nil {
		tags = &metadata.TrackMetadata{}
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

	// 2. Get or create artist credit for track artist.
	artistName := tags.Artist
	if artistName == "" {
		artistName = "Unknown Artist"
	}

	artistCredit, err := l.cachedUpsertArtistCredit(
		q, cache, artistName,
	)
	if err != nil {
		return 0, fmt.Errorf(
			"could not upsert artist credit: %w", err,
		)
	}

	l.cachedLinkArtist(q, cache, metrics, artistName, artistCredit.ID)

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
			},
		)
		if err != nil {
			l.logger.Warn(
				"could not link recording to release group",
				"err", err,
			)
		}
	}

	return recording.ID, nil
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
