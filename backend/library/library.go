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

var errLibraryDirNotConfigured = errors.New("library directory not configured")

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
		linkedCredits: make(map[string]struct{}),
	}
}

// queueClearer is a narrow interface for clearing the playback queue.
type queueClearer interface {
	Clear()
}

// Library manages scanning and querying the music collection.
type Library struct {
	ctx    context.Context
	logger *slog.Logger
	conf   *Config
	db     *database.DB
	queue  queueClearer
}

// SetQueue provides the library with a reference to the queue so
// that destructive operations like FullRescan can clear the queue
// and stop playback before wiping data.
func (l *Library) SetQueue(q queueClearer) {
	l.queue = q
}

// NewLibrary creates a new library with the given configuration.
// A nil config is permitted; the library will be inert until a valid
// configuration is supplied via the LibraryConfigChanged event.
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
	l.ctx = ctx
	l.registerEventHandlers()
}

func (l *Library) registerEventHandlers() {
	if l.ctx == nil {
		l.logger.Error("Context is nil, cannot register event handlers")

		return
	}

	runtime.EventsOn(l.ctx, events.LibraryConfigChanged, func(data ...any) {
		l.logger.Info("Received LibraryConfigChanged event")

		if len(data) == 0 {
			l.logger.Error("LibraryConfigChanged event received with no data")

			return
		}

		configMap, ok := data[0].(map[string]any)
		if !ok {
			l.logger.Error("LibraryConfigChanged event data is not a map", "data", data[0])

			return
		}

		dir, ok := configMap["DirectoryPath"].(string)
		if !ok {
			l.logger.Error("DirectoryPath not found or not a string in config event")

			return
		}

		updatedConfig := Config{DirectoryPath: Directory(dir)}
		if err := l.handleConfigUpdate(updatedConfig); err != nil {
			l.logger.Error("Failed to handle config update", "err", err)
		}
	})
}

// Scan syncs the library by adding new files and removing deleted ones.
// Files that exist but have incomplete metadata (recording_id = 0)
// will be updated.  The returned ScanMetrics contains timing and
// count data for every phase of the scan.
func (l *Library) Scan() (*ScanMetrics, error) {
	metrics := newScanMetrics()
	scanStart := time.Now()

	if len(l.conf.DirectoryPath) == 0 {
		return metrics, errLibraryDirNotConfigured
	}

	workerCount := resolveScanWorkerCount(
		l.conf.ScanConcurrency,
		string(l.conf.DirectoryPath),
	)

	l.logger.Info(
		"beginning library scan",
		"workers", workerCount,
		"concurrencyMode", l.conf.ScanConcurrency,
	)

	runtime.EventsEmit(l.ctx, events.LibraryScanStarted)

	// --- Phase 1: load existing files from DB ---
	loadStart := time.Now()

	existingFiles, err := l.db.Queries.GetAllAudioFiles(l.ctx)
	if err != nil {
		return metrics, fmt.Errorf(
			"could not load existing audio files: %w", err,
		)
	}

	existingPaths := &sync.Map{}
	for _, f := range existingFiles {
		existingPaths.Store(f.FilePath, f)
	}

	metrics.LoadExisting = time.Since(loadStart)

	l.logger.Debug(
		"loaded existing files from database",
		"count", len(existingFiles),
		"library-directory", l.conf.DirectoryPath,
	)

	basePath := string(l.conf.DirectoryPath)
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
						case <-l.ctx.Done():
							return l.ctx.Err()
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
				case <-l.ctx.Done():
					return l.ctx.Err()
				}

				return nil
			},
		)

		if walkErr != nil {
			errMu.Lock()
			scanErr = errors.Join(
				scanErr,
				fmt.Errorf(
					"problem walking library directory: %w",
					walkErr,
				),
			)
			errMu.Unlock()
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
				}
			}
		}()
	}

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
			result, err := l.extractAudioMetadata(
				work, metrics,
			)
			if err != nil {
				l.logger.Warn(
					"failed to extract metadata",
					"path", work.absolutePath,
					"err", err,
				)

				errMu.Lock()
				scanErr = errors.Join(scanErr, err)
				errMu.Unlock()

				return nil
			}

			select {
			case resultChan <- result:
			case <-l.ctx.Done():
				return l.ctx.Err()
			}

			return nil
		})
	}

	_ = g.Wait()

	metrics.ExtractionWallClock = time.Since(extractStart)

	close(resultChan)
	dbWg.Wait()

	// Close thumbnail channel and wait for all thumbnail workers
	// to finish.  The DB writer has stopped sending work at this
	// point so it is safe to close.
	thumbStart := time.Now()

	close(thumbChan)
	thumbWg.Wait()

	metrics.ThumbnailWallClock = time.Since(thumbStart)

	// --- Phase 5: orphan cleanup ---
	orphanStart := time.Now()

	var removed atomic.Int64

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

			return true
		}

		removed.Add(1)

		return true
	})

	metrics.OrphanCleanup = time.Since(orphanStart)

	// --- Phase 6: post-scan variant generation ---
	variantStart := time.Now()

	if err := l.generateMissingSizedVariants(); err != nil {
		l.logger.Warn(
			"could not generate missing sized variants",
			"err", err,
		)
	}

	metrics.PostScanVariants = time.Since(variantStart)

	// --- Finalize ---
	metrics.Added = added.Load()
	metrics.Updated = updated.Load()
	metrics.Skipped = skipped.Load()
	metrics.Removed = removed.Load()
	metrics.Total = time.Since(scanStart)

	l.logger.Info(
		"library scan complete",
		"added", metrics.Added,
		"updated", metrics.Updated,
		"removed", metrics.Removed,
		"skipped", metrics.Skipped,
		"total", metrics.Total,
		"library", l.conf.DirectoryPath,
	)

	runtime.EventsEmit(
		l.ctx, events.LibraryScanComplete, metrics,
	)

	return metrics, scanErr
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
	existingFileID int64 // non-zero if this is an update
	needsUpdate    bool
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

	tags, lengthMillis, timing, err := metadata.ExtractAllMetadata(
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

	var batchErr error

	for i := range batch {
		result := &batch[i]

		var saveErr error

		if result.needsUpdate {
			saveErr = l.updateAudioFileMetadata(
				txq, cache, metrics, *result,
				thumbChan,
			)
			if saveErr == nil {
				updated.Add(1)
			}
		} else {
			saveErr = l.saveAudioFile(
				txq, cache, metrics, *result,
				thumbChan,
			)
			if saveErr == nil {
				added.Add(1)
			}
		}

		if saveErr != nil {
			l.logger.Warn(
				"failed to save audio file",
				"path", result.absolutePath,
				"err", saveErr,
			)

			batchErr = errors.Join(batchErr, saveErr)
		}
	}

	if commitErr := tx.Commit(); commitErr != nil {
		return fmt.Errorf(
			"could not commit batch of %d files: %w",
			len(batch), commitErr,
		)
	}

	return batchErr
}

// saveAudioFile writes audio file metadata to the database (new files).
func (l *Library) saveAudioFile(
	q *sqlcgen.Queries,
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

	if _, err := q.CreateAudioFile(
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
		}); err != nil {
		return fmt.Errorf(
			"could not save audio file to db: %w", err,
		)
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

	if err := q.UpdateAudioFileRecording(
		l.ctx, sqlcgen.UpdateAudioFileRecordingParams{
			RecordingID: recordingID,
			ID:          result.existingFileID,
		}); err != nil {
		return fmt.Errorf(
			"could not update audio file recording: %w", err,
		)
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

	l.cachedLinkArtist(q, cache, artistName, artistCredit.ID)

	// 3. Get or create artist credit for album artist.
	albumArtistCreditID := l.resolveAlbumArtistCredit(
		q, cache, tags, artistCredit.ID,
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

	// 6. Link recording to release group.
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
func (l *Library) cachedLinkArtist(
	q *sqlcgen.Queries,
	cache *entityCache,
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

	_, _ = q.CreateArtistCreditArtist(
		l.ctx,
		sqlcgen.CreateArtistCreditArtistParams{
			ArtistID: artist.ID,
			CreditID: creditID,
		},
	)

	cache.linkedCredits[linkKey] = struct{}{}
}

// resolveAlbumArtistCredit returns the album artist credit ID.
// When the AlbumArtist tag is absent or matches the track artist,
// the track artist credit is reused.
func (l *Library) resolveAlbumArtistCredit(
	q *sqlcgen.Queries,
	cache *entityCache,
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
		q, cache, tags.AlbumArtist, albumArtistCredit.ID,
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

	// Check cache first.
	if cached, ok := cache.releaseGroups[tags.Album]; ok {
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
				cache.releaseGroups[tags.Album] = cached
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

	cache.releaseGroups[tags.Album] = rg

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

func (l *Library) handleConfigUpdate(updatedConfigValues Config) error {
	l.logger.Info("handling config update", "updated", updatedConfigValues)

	var updateErr error

	if l.conf.DirectoryPath != updatedConfigValues.DirectoryPath {
		l.logger.Info("new library, scanning")

		l.conf.DirectoryPath = updatedConfigValues.DirectoryPath

		if _, err := l.Scan(); err != nil {
			updateErr = errors.Join(
				updateErr,
				fmt.Errorf(
					"problem scanning library on config update: %w",
					err,
				),
			)
		}
	}

	return updateErr
}
