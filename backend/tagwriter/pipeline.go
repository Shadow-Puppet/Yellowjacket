package tagwriter

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"yellowjacket/backend/database"
	"yellowjacket/backend/events"
)

// errNoChanges is returned when WriteTrackTags is called with an
// empty TagChanges map.
var errNoChanges = errors.New("tagwriter: no changes provided")

// PlayerStopper checks whether a file is currently playing and
// stops playback if needed.  Defined as an interface to break the
// import cycle between tagwriter and player.
type PlayerStopper interface {
	// CurrentFilePath returns the file path of the currently-
	// loaded track, or empty string if nothing is loaded.
	CurrentFilePath() string
	// StopAndRelease stops playback and releases the file handle.
	StopAndRelease()
}

// PipelineLocker abstracts the library's pipeline mutex for
// scan/write mutual exclusion.
type PipelineLocker interface {
	AcquirePipelineLock()
	ReleasePipelineLock()
}

// TagWriter orchestrates the complete tag writing pipeline:
// file write → DB sync → event emission.
type TagWriter struct {
	logger  *slog.Logger
	db      *database.DB
	ctx     context.Context // Wails context for event emission
	player  PlayerStopper
	library PipelineLocker
}

// NewTagWriter creates a TagWriter with the given dependencies.
// Call SetContext after the Wails runtime is available.
func NewTagWriter(
	logger *slog.Logger,
	db *database.DB,
	player PlayerStopper,
	library PipelineLocker,
) *TagWriter {
	return &TagWriter{
		logger:  logger.WithGroup("tagwriter"),
		db:      db,
		player:  player,
		library: library,
	}
}

// SetContext stores the Wails runtime context for event emission.
// Called during the two-phase init pattern in OnStartup.
func (tw *TagWriter) SetContext(ctx context.Context) {
	tw.ctx = ctx
}

// WriteTrackTags is the single entry point for writing metadata to
// a track's audio file and synchronising all changes to the
// database.  It accepts a track ID (audio_file.id) and a diff map
// of changed fields.
//
// The pipeline:
//  1. Look up track from DB.
//  2. Detect audio format.
//  3. Acquire pipeline lock (mutual exclusion with scan).
//  4. Stop player if this file is currently playing.
//  5. Write tags to file (format-specific writer).
//  6. Sync database (entity relink, FTS5, orphan cleanup).
//  7. Emit TrackMetadataChanged event.
func (tw *TagWriter) WriteTrackTags(trackID int64, changes TagChanges) error {
	start := time.Now()
	ctx := context.Background()

	if len(changes) == 0 {
		return errNoChanges
	}

	// 1. Look up track.
	audioFile, err := tw.db.Queries.GetAudioFile(ctx, trackID)
	if err != nil {
		return fmt.Errorf("get audio file %d: %w", trackID, err)
	}

	recording, err := tw.db.Queries.GetRecording(ctx, audioFile.RecordingID)
	if err != nil {
		return fmt.Errorf("get recording %d: %w", audioFile.RecordingID, err)
	}

	rgLinks, err := tw.db.Queries.GetRecordingReleaseGroups(ctx, recording.ID)
	if err != nil {
		return fmt.Errorf("get recording rg links: %w", err)
	}

	// 2. Detect format.
	format, err := DetectFormat(audioFile.FilePath)
	if err != nil {
		return fmt.Errorf("detect format: %w", err)
	}

	// 3. Acquire pipeline lock.
	tw.library.AcquirePipelineLock()
	defer tw.library.ReleasePipelineLock()

	// 4. Player safety check.
	if tw.player != nil && tw.player.CurrentFilePath() == audioFile.FilePath {
		tw.logger.Info("stopping playback for tag write",
			"path", audioFile.FilePath)
		tw.player.StopAndRelease()
	}

	// 5. Write file tags.
	switch format {
	case FormatMP3:
		err = writeMp3Tags(tw.logger, audioFile.FilePath, changes)
	case FormatFLAC:
		err = writeFlacTags(tw.logger, audioFile.FilePath, changes)
	default:
		err = fmt.Errorf("%w: %s", errUnsupportedFormat, format)
	}

	if err != nil {
		return fmt.Errorf("write file tags: %w", err)
	}

	// 6. Sync database.
	if syncErr := syncDatabase(ctx, tw.logger, tw.db, dbSyncParams{
		audioFileID:  audioFile.ID,
		recordingID:  recording.ID,
		filePath:     audioFile.FilePath,
		changes:      changes,
		oldRecording: recording,
		oldRGLinks:   rgLinks,
	}); syncErr != nil {
		tw.logger.Error("db sync failed after successful file write",
			"err", syncErr,
			"trackID", trackID,
			"path", audioFile.FilePath,
		)

		return fmt.Errorf("sync database: %w", syncErr)
	}

	// 7. Emit event.
	if tw.ctx != nil {
		wailsruntime.EventsEmit(tw.ctx, events.TrackMetadataChanged,
			map[string]any{
				"trackId":  trackID,
				"filePath": audioFile.FilePath,
			},
		)
	}

	tw.logger.Info("tag write complete",
		"trackID", trackID,
		"path", audioFile.FilePath,
		"duration", time.Since(start),
		"changedFields", len(changes),
	)

	return nil
}
