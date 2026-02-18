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

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sync/errgroup"

	"yellowjacket/backend/database"
	"yellowjacket/backend/database/sql/sqlcgen"
	"yellowjacket/backend/events"
	"yellowjacket/backend/metadata"
)

var errLibraryDirNotConfigured = errors.New("library directory not configured")

// Library manages scanning and querying the music collection.
type Library struct {
	ctx    context.Context
	logger *slog.Logger
	conf   *Config
	db     *database.DB
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
// Files that exist but have incomplete metadata (recording_id = 0) will be updated.
func (l *Library) Scan() error {
	l.logger.Info("beginning library scan", "workers", scanWorkerCount)

	if len(l.conf.DirectoryPath) == 0 {
		return errLibraryDirNotConfigured
	}

	// Load existing file paths from the database into a sync.Map for concurrent access.
	// The map tracks path → audioFile; entries are removed as files are "seen" during the walk.
	// Any entries remaining after the walk are orphans (files deleted from disk).
	existingFiles, err := l.db.Queries.GetAllAudioFiles(l.ctx)
	if err != nil {
		return fmt.Errorf("could not load existing audio files: %w", err)
	}

	existingPaths := &sync.Map{}
	for _, f := range existingFiles {
		existingPaths.Store(f.FilePath, f)
	}

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

	// Walker goroutine: traverse directory and send work items to workers
	go func() {
		defer close(workChan)

		walkErr := fs.WalkDir(
			os.DirFS(basePath),
			".",
			func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					l.logger.Error("problem walking directory", "path", path, "err", err)

					return nil // continue walking
				}

				if d.IsDir() {
					return nil
				}

				absoluteFilePath := filepath.Join(basePath, path)
				fileExt := filepath.Ext(d.Name())

				fileType, isSupportedAudioFile := metadata.GetSupportedFileType(fileExt)
				if !isSupportedAudioFile {
					return nil
				}

				// Check if file already exists in database
				if existing, exists := existingPaths.LoadAndDelete(absoluteFilePath); exists {
					audioFile := existing.(sqlcgen.AudioFile)

					// Check if this file needs metadata update (recording_id = 0)
					if audioFile.RecordingID == 0 {
						l.logger.Debug("file needs metadata update", "path", absoluteFilePath)

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

				l.logger.Debug("queueing file for import", "path", absoluteFilePath)

				// Send to workers for processing
				select {
				case workChan <- scanWork{absolutePath: absoluteFilePath, fileType: fileType}:
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
				fmt.Errorf("problem walking library directory: %w", walkErr),
			)
			errMu.Unlock()
		}
	}()

	// DB writer goroutine: serialize all database writes to avoid SQLite contention
	var dbWg sync.WaitGroup

	dbWg.Add(1)

	go func() {
		defer dbWg.Done()

		for result := range resultChan {
			var saveErr error

			if result.needsUpdate {
				saveErr = l.updateAudioFileMetadata(result)
				if saveErr == nil {
					updated.Add(1)
				}
			} else {
				saveErr = l.saveAudioFile(result)
				if saveErr == nil {
					added.Add(1)
				}
			}

			if saveErr != nil {
				l.logger.Warn(
					"failed to save audio file",
					"path",
					result.absolutePath,
					"err",
					saveErr,
				)

				errMu.Lock()
				scanErr = errors.Join(scanErr, saveErr)
				errMu.Unlock()
			}
		}
	}()

	// Worker pool: extract metadata concurrently, send results to DB writer
	g := new(errgroup.Group)
	g.SetLimit(scanWorkerCount)

	for work := range workChan {
		g.Go(func() error {
			result, err := l.extractAudioMetadata(work)
			if err != nil {
				l.logger.Warn("failed to extract metadata", "path", work.absolutePath, "err", err)

				errMu.Lock()
				scanErr = errors.Join(scanErr, err)
				errMu.Unlock()

				return nil // continue processing other files
			}

			// Send to DB writer
			select {
			case resultChan <- result:
			case <-l.ctx.Done():
				return l.ctx.Err()
			}

			return nil
		})
	}

	_ = g.Wait() // Wait for all metadata extraction to complete

	close(resultChan) // Signal DB writer to finish
	dbWg.Wait()       // Wait for all DB writes to complete

	// Orphan cleanup: any entries remaining in existingPaths are files deleted from disk
	var removed atomic.Int64

	existingPaths.Range(func(key, value any) bool {
		path := key.(string)
		audioFile := value.(sqlcgen.AudioFile)

		l.logger.Debug("removing orphaned database entry", "path", path, "id", audioFile.ID)

		if err := l.db.Queries.DeleteAudioFile(l.ctx, audioFile.ID); err != nil {
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

	// Generate sized variants for any cover art missing them,
	// and migrate legacy _thumb files.
	if err := l.generateMissingSizedVariants(); err != nil {
		l.logger.Warn(
			"could not generate missing sized variants",
			"err", err,
		)
	}

	l.logger.Info(
		"library scan complete",
		"added", added.Load(),
		"updated", updated.Load(),
		"removed", removed.Load(),
		"skipped", skipped.Load(),
		"library", l.conf.DirectoryPath,
	)

	runtime.EventsEmit(l.ctx, events.LibraryScanComplete)

	return scanErr
}

// scanWorkerCount controls the number of concurrent file processors.
// TODO: make configurable via Config.
var scanWorkerCount = goruntime.NumCPU()

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
func (l *Library) extractAudioMetadata(work scanWork) (importResult, error) {
	result := importResult{
		absolutePath:   work.absolutePath,
		fileType:       work.fileType,
		existingFileID: work.existingFileID,
		needsUpdate:    work.needsUpdate,
	}

	// Get duration (skip if updating and we already have it)
	if work.needsUpdate && work.existingLength > 0 {
		result.lengthMillis = work.existingLength
	} else {
		trackLengthMillis, err := metadata.GetTrackLengthMillis(work.absolutePath)
		if err != nil {
			return result, fmt.Errorf(
				"could not get track length for %s: %w",
				work.absolutePath,
				err,
			)
		}

		result.lengthMillis = trackLengthMillis
	}

	// Extract tags
	tags, err := metadata.ExtractTags(work.absolutePath)
	if err != nil {
		l.logger.Warn("could not extract tags", "path", work.absolutePath, "err", err)
		// Continue with empty tags - not a fatal error
		tags = &metadata.TrackMetadata{}
	}

	result.tags = tags

	return result, nil
}

// saveAudioFile writes audio file metadata to the database (new files).
func (l *Library) saveAudioFile(result importResult) error {
	l.logger.Debug(
		"saving audio file to db",
		"absolute-path", result.absolutePath,
		"track-length-millis", result.lengthMillis,
		"file-type", int64(slices.Index(metadata.SupportedFileExtensions, result.fileType)),
	)

	// Process metadata and create related records
	recordingID, err := l.processMetadata(result)
	if err != nil {
		return fmt.Errorf("could not process metadata: %w", err)
	}

	if _, err := l.db.Queries.CreateAudioFile(
		l.ctx, sqlcgen.CreateAudioFileParams{
			FilePath:           result.absolutePath,
			LengthMilliseconds: result.lengthMillis,
			FileTypeID: int64(
				slices.Index(metadata.SupportedFileExtensions, result.fileType),
			),
			RecordingID: recordingID,
		}); err != nil {
		return fmt.Errorf("could not save audio file to db: %w", err)
	}

	l.logger.Debug("added audio file to library", "path", result.absolutePath)

	return nil
}

// updateAudioFileMetadata updates an existing audio file with extracted metadata.
func (l *Library) updateAudioFileMetadata(result importResult) error {
	l.logger.Debug(
		"updating audio file metadata",
		"absolute-path", result.absolutePath,
		"file-id", result.existingFileID,
	)

	// Process metadata and create related records
	recordingID, err := l.processMetadata(result)
	if err != nil {
		return fmt.Errorf("could not process metadata: %w", err)
	}

	if err := l.db.Queries.UpdateAudioFileRecording(
		l.ctx, sqlcgen.UpdateAudioFileRecordingParams{
			RecordingID: recordingID,
			ID:          result.existingFileID,
		}); err != nil {
		return fmt.Errorf("could not update audio file recording: %w", err)
	}

	l.logger.Debug("updated audio file metadata", "path", result.absolutePath)

	return nil
}

// processMetadata creates all related database records for metadata and returns the recording ID.
func (l *Library) processMetadata(result importResult) (int64, error) {
	tags := result.tags
	if tags == nil {
		tags = &metadata.TrackMetadata{}
	}

	// 1. Handle cover art (if present)
	var coverArtID sql.NullInt64

	if tags.Picture != nil {
		coverPath, err := l.saveCoverArt(tags.Picture)
		if err != nil {
			l.logger.Warn("could not save cover art", "err", err)
		} else if coverPath != "" {
			// Use upsert to avoid duplicates
			ca, err := l.db.Queries.UpsertCoverArt(l.ctx, sqlcgen.UpsertCoverArtParams{
				IsEmbedded: true,
				FilePath:   coverPath,
				MimeType:   tags.Picture.MIMEType,
			})
			if err != nil {
				l.logger.Warn("could not create cover art record", "err", err)
			} else {
				coverArtID = sql.NullInt64{Int64: ca.ID, Valid: true}
			}
		}
	}

	// 2. Get or create artist credit for track artist
	artistName := tags.Artist
	if artistName == "" {
		artistName = "Unknown Artist"
	}

	artistCredit, err := l.db.Queries.UpsertArtistCredit(l.ctx, artistName)
	if err != nil {
		return 0, fmt.Errorf("could not upsert artist credit: %w", err)
	}

	// Also create the artist record and link (best effort)
	artist, err := l.db.Queries.UpsertArtist(l.ctx, artistName)
	if err != nil {
		l.logger.Warn("could not upsert artist", "err", err)
	} else {
		// Link artist to credit (ignore error if already linked)
		_, _ = l.db.Queries.CreateArtistCreditArtist(l.ctx, sqlcgen.CreateArtistCreditArtistParams{
			ArtistID: artist.ID,
			CreditID: artistCredit.ID,
		})
	}

	// 3. Get or create artist credit for album artist.
	// Always assign an album artist credit so the cover grid displays an
	// artist name.  When the AlbumArtist tag is absent or identical to the
	// track Artist, reuse the track artist credit instead of leaving it NULL.
	var albumArtistCreditID sql.NullInt64

	if tags.AlbumArtist != "" && tags.AlbumArtist != tags.Artist {
		albumArtistCredit, err := l.db.Queries.UpsertArtistCredit(
			l.ctx, tags.AlbumArtist,
		)
		if err != nil {
			l.logger.Warn("could not upsert album artist credit", "err", err)
		} else {
			albumArtistCreditID = sql.NullInt64{
				Int64: albumArtistCredit.ID, Valid: true,
			}

			// Also create the artist record and link.
			albumArtist, err := l.db.Queries.UpsertArtist(
				l.ctx, tags.AlbumArtist,
			)
			if err != nil {
				l.logger.Warn("could not upsert album artist", "err", err)
			} else {
				_, _ = l.db.Queries.CreateArtistCreditArtist(
					l.ctx,
					sqlcgen.CreateArtistCreditArtistParams{
						ArtistID: albumArtist.ID,
						CreditID: albumArtistCredit.ID,
					},
				)
			}
		}
	} else {
		// AlbumArtist is empty or matches the track artist — reuse the
		// track artist credit so the release group always has an artist.
		albumArtistCreditID = sql.NullInt64{
			Int64: artistCredit.ID, Valid: true,
		}
	}

	// 4. Get or create release group (album)
	var releaseGroupID sql.NullInt64

	if tags.Album != "" {
		rg, err := l.db.Queries.UpsertReleaseGroup(l.ctx, sqlcgen.UpsertReleaseGroupParams{
			Name:                tags.Album,
			AlbumArtistCreditID: albumArtistCreditID,
			Year:                toNullInt64(tags.Year),
		})
		if err != nil {
			l.logger.Warn("could not upsert release group", "err", err)
		} else {
			releaseGroupID = sql.NullInt64{Int64: rg.ID, Valid: true}

			// Update cover art if this album doesn't have one yet
			if coverArtID.Valid && !rg.CoverArtID.Valid {
				err := l.db.Queries.UpdateReleaseGroupCoverArt(
					l.ctx,
					sqlcgen.UpdateReleaseGroupCoverArtParams{
						CoverArtID: coverArtID,
						ID:         rg.ID,
					},
				)
				if err != nil {
					l.logger.Warn("could not update release group cover art", "err", err)
				}
			}
		}
	}

	// 5. Create recording
	recording, err := l.db.Queries.CreateRecordingFull(l.ctx, sqlcgen.CreateRecordingFullParams{
		Name:           l.getRecordingName(tags, result.absolutePath),
		ArtistCreditID: artistCredit.ID,
		TrackNumber:    toNullInt64(tags.TrackNumber),
		DiscNumber:     toNullInt64(tags.DiscNumber),
		Year:           toNullInt64(tags.Year),
		Genre:          toNullString(tags.Genre),
		Composer:       toNullString(tags.Composer),
		Lyrics:         toNullString(tags.Lyrics),
		Comment:        toNullString(tags.Comment),
	})
	if err != nil {
		return 0, fmt.Errorf("could not create recording: %w", err)
	}

	// 6. Link recording to release group
	if releaseGroupID.Valid {
		_, err = l.db.Queries.CreateReleaseGroupRecording(
			l.ctx,
			sqlcgen.CreateReleaseGroupRecordingParams{
				ReleaseGroupID: releaseGroupID.Int64,
				RecordingID:    recording.ID,
				TrackNumber:    toNullInt64(tags.TrackNumber),
				DiscNumber:     toNullInt64(tags.DiscNumber),
			},
		)
		if err != nil {
			l.logger.Warn("could not link recording to release group", "err", err)
		}
	}

	return recording.ID, nil
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
		if err := l.Scan(); err != nil {
			updateErr = errors.Join(
				updateErr,
				fmt.Errorf("problem scanning library on config update: %w", err),
			)
		}
	}

	return updateErr
}
