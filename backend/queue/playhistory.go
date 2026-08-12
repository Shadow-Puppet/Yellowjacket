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

	// Update the denormalized columns on audio_files, and read back what
	// they became in the same statement: the event below has to carry the
	// new values, and a follow-up SELECT could race another play.
	//
	// RETURNING requires the writer connection — the read pool is a
	// separate sql.DB, and this is a write.
	var (
		playCount int64
		filePath  string
	)

	err = q.db.QueryRowWriter(
		`UPDATE audio_files
		 SET play_count = play_count + 1,
		     last_played = ?
		 WHERE id = ?
		 RETURNING play_count, file_path`,
		now, audioFileID,
	).Scan(&playCount, &filePath)
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
		"playCount", playCount,
	)

	// Deliberately NOT TrackMetadataChanged.  That event means "the tags
	// on disk were rewritten", which can change an album, an artist or a
	// genre, so the frontend answers it by discarding its whole library
	// cache and refetching — measured at ~37 MB across the IPC and ~0.8 s
	// of blocked main thread, once per finished song, plus clearing
	// whatever the user had selected in the track list (perf.C1/C2).
	//
	// A play count is one integer on one row, and this payload carries
	// enough for the frontend to patch it in place.
	events.Emit(q.ctx, events.TrackPlayCountChanged, map[string]any{
		"audioFileId": audioFileID,
		"filePath":    filePath,
		"playCount":   playCount,
		"lastPlayed":  now,
	})
}
