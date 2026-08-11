package queue

import (
	"time"

	"yellowjacket/backend/events"
)

// recordPlay inserts a play_history row and updates the denormalized
// play_count / last_played columns on audio_files. Called from
// OnPlaybackFinished for the track that just finished.
//
// SAFETY: Uses ExecContext with parameterized queries only.
// Must be called without q.mu held — it acquires the DB connection
// which is single-writer (MaxOpenConns 1).
func (q *Queue) recordPlay(audioFileID int64) {
	if audioFileID <= 0 {
		return
	}

	now := time.Now().UTC().Format(time.DateTime)

	// Insert play_history row.
	_, err := q.db.ExecContext(
		`INSERT INTO play_history (audio_file_id, played_at)
		 VALUES (?, ?)`,
		audioFileID, now,
	)
	if err != nil {
		q.logger.Error(
			"failed to insert play history",
			"audioFileId", audioFileID,
			"error", err,
		)

		return
	}

	// Update denormalized columns on audio_files.
	_, err = q.db.ExecContext(
		`UPDATE audio_files
		 SET play_count = play_count + 1,
		     last_played = ?
		 WHERE id = ?`,
		now, audioFileID,
	)
	if err != nil {
		q.logger.Error(
			"failed to update play count",
			"audioFileId", audioFileID,
			"error", err,
		)

		return
	}

	q.logger.Info(
		"Play recorded",
		"audioFileId", audioFileID,
	)

	// Notify frontend so the track list refreshes play count.
	events.Emit(q.ctx, events.TrackMetadataChanged)
}
