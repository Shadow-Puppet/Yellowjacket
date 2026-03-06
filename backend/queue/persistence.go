package queue

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"yellowjacket/backend/database/sql/sqlcgen"
	"yellowjacket/backend/profiling"
)

// persistAddTrack inserts a single track at the end of the queue.
// No position shifting is needed because this is always an append.
// The caller must hold q.mu.
func (q *Queue) persistAddTrack(track Track) {
	_, err := q.db.Queries.InsertQueueTrack(q.db.Ctx, sqlcgen.InsertQueueTrackParams{
		AudioFileID: track.AudioFileID,
		Position:    track.Position,
	})
	if err != nil {
		q.logger.Error("Failed to persist added track", "err", err)
	}
}

// persistAddTracks inserts multiple tracks at the end of the queue
// atomically in a transaction. No position shifting is needed because
// these are always appends.
// The caller must hold q.mu.
func (q *Queue) persistAddTracks(tracks []Track) {
	if len(tracks) == 0 {
		return
	}

	tx, err := q.db.BeginTx()
	if err != nil {
		q.logger.Error("Failed to begin transaction", "err", err)

		return
	}

	committed := false

	defer func() {
		if !committed {
			if rbErr := tx.Rollback(); rbErr != nil {
				q.logger.Error(
					"Failed to rollback transaction",
					"err", rbErr,
				)
			}
		}
	}()

	txQueries := q.db.Queries.WithTx(tx)

	for _, track := range tracks {
		_, insertErr := txQueries.InsertQueueTrack(q.db.Ctx, sqlcgen.InsertQueueTrackParams{
			AudioFileID: track.AudioFileID,
			Position:    track.Position,
		})
		if insertErr != nil {
			q.logger.Error("Failed to insert track", "err", insertErr)

			return
		}
	}

	if commitErr := tx.Commit(); commitErr != nil {
		q.logger.Error("Failed to commit transaction", "err", commitErr)

		return
	}

	committed = true
}

// persistInsertTracks inserts multiple tracks at a given position,
// shifting existing tracks to make room. Uses a transaction for atomicity.
// The caller must hold q.mu.
func (q *Queue) persistInsertTracks(tracks []Track, insertPos int) {
	if len(tracks) == 0 {
		return
	}

	tx, err := q.db.BeginTx()
	if err != nil {
		q.logger.Error("Failed to begin transaction", "err", err)

		return
	}

	committed := false

	defer func() {
		if !committed {
			if rbErr := tx.Rollback(); rbErr != nil {
				q.logger.Error(
					"Failed to rollback transaction",
					"err", rbErr,
				)
			}
		}
	}()

	// SAFETY: Multi-row position shift by variable N unsupported by sqlc
	// (ShiftQueuePositionsUp only shifts by 1). Bind variables match args;
	// no string interpolation.
	_, err = tx.ExecContext(
		q.db.Ctx,
		"UPDATE queue_tracks SET position = position + ? WHERE position >= ?",
		len(tracks), insertPos,
	)
	if err != nil {
		q.logger.Error("Failed to shift positions up", "err", err)

		return
	}

	txQueries := q.db.Queries.WithTx(tx)

	for i, track := range tracks {
		_, insertErr := txQueries.InsertQueueTrack(q.db.Ctx, sqlcgen.InsertQueueTrackParams{
			AudioFileID: track.AudioFileID,
			Position:    int64(insertPos + i),
		})
		if insertErr != nil {
			q.logger.Error("Failed to insert track", "err", insertErr)

			return
		}
	}

	if commitErr := tx.Commit(); commitErr != nil {
		q.logger.Error("Failed to commit transaction", "err", commitErr)

		return
	}

	committed = true
}

// persistRemoveTrack deletes a single track at the given position and
// shifts subsequent positions down to close the gap.
// The caller must hold q.mu.
func (q *Queue) persistRemoveTrack(position int) {
	tx, err := q.db.BeginTx()
	if err != nil {
		q.logger.Error("Failed to begin transaction", "err", err)

		return
	}

	committed := false

	defer func() {
		if !committed {
			if rbErr := tx.Rollback(); rbErr != nil {
				q.logger.Error(
					"Failed to rollback transaction",
					"err", rbErr,
				)
			}
		}
	}()

	txQueries := q.db.Queries.WithTx(tx)

	if removeErr := txQueries.RemoveQueueTrackByPosition(
		q.db.Ctx, int64(position),
	); removeErr != nil {
		q.logger.Error("Failed to remove track by position", "err", removeErr)

		return
	}

	if shiftErr := txQueries.ShiftQueuePositionsDown(
		q.db.Ctx, int64(position),
	); shiftErr != nil {
		q.logger.Error("Failed to shift positions down", "err", shiftErr)

		return
	}

	if commitErr := tx.Commit(); commitErr != nil {
		q.logger.Error("Failed to commit transaction", "err", commitErr)

		return
	}

	committed = true
}

// lookupTrackMetaBatch fetches audio file IDs and metadata for a batch of
// file paths using a single query per chunk (instead of 2 queries per track).
// Returns a map keyed by file path. This is safe to call without holding q.mu.
func (q *Queue) lookupTrackMetaBatch(
	filePaths []string,
) map[string]trackMeta {
	result := make(map[string]trackMeta, len(filePaths))

	// Deduplicate paths to avoid redundant work.
	unique := make([]string, 0, len(filePaths))
	seen := make(map[string]bool, len(filePaths))

	for _, fp := range filePaths {
		if !seen[fp] {
			seen[fp] = true

			unique = append(unique, fp)
		}
	}

	// Process in chunks to stay under the SQLite bind variable limit.
	for i := 0; i < len(unique); i += maxSQLiteVars {
		end := i + maxSQLiteVars
		if end > len(unique) {
			end = len(unique)
		}

		chunk := unique[i:end]
		q.lookupChunk(chunk, result)
	}

	return result
}

// lookupChunk executes a single batch query for a chunk of file paths
// using the sqlc-generated LookupTrackMetaByPaths query against the
// track_metadata VIEW.
func (q *Queue) lookupChunk(
	paths []string,
	result map[string]trackMeta,
) {
	if len(paths) == 0 {
		return
	}

	rows, err := q.db.Queries.LookupTrackMetaByPaths(q.db.Ctx, paths)
	if err != nil {
		q.logger.Error("Batch metadata lookup failed", "err", err)

		return
	}

	for _, row := range rows {
		result[row.FilePath] = trackMeta{
			AudioFileID: row.ID,
			FilePath:    row.FilePath,
			Title:       row.Title,
			Artist:      row.ArtistName,
		}
	}
}

// persistTracks writes the current queue tracks to the database atomically
// using a transaction with batched multi-row inserts.
func (q *Queue) persistTracks() {
	tx, err := q.db.BeginTx()
	if err != nil {
		q.logger.Error("Failed to begin transaction", "err", err)

		return
	}

	committed := false

	defer func() {
		if !committed {
			if rbErr := tx.Rollback(); rbErr != nil {
				q.logger.Error(
					"Failed to rollback transaction",
					"err", rbErr,
				)
			}
		}
	}()

	// Clear existing tracks.
	txQueries := q.db.Queries.WithTx(tx)

	if clearErr := txQueries.ClearQueueTracks(q.db.Ctx); clearErr != nil {
		q.logger.Error("Failed to clear queue tracks", "err", clearErr)

		return
	}

	// Batch insert tracks. Each row needs 2 bind vars (audio_file_id, position).
	const varsPerRow = 2

	batchSize := maxSQLiteVars / varsPerRow

	for i := 0; i < len(q.tracks); i += batchSize {
		end := i + batchSize
		if end > len(q.tracks) {
			end = len(q.tracks)
		}

		batch := q.tracks[i:end]

		if insertErr := q.insertTrackBatch(tx, batch); insertErr != nil {
			q.logger.Error(
				"Failed to batch insert queue tracks",
				"err", insertErr,
			)

			return
		}
	}

	if commitErr := tx.Commit(); commitErr != nil {
		q.logger.Error("Failed to commit transaction", "err", commitErr)

		return
	}

	committed = true
}

// insertTrackBatch inserts a batch of tracks in a single multi-row INSERT.
func (q *Queue) insertTrackBatch(tx *sql.Tx, batch []Track) error {
	if len(batch) == 0 {
		return nil
	}

	valuePlaceholders := make([]string, len(batch))
	args := make([]any, 0, len(batch)*2)

	for i, track := range batch {
		valuePlaceholders[i] = "(?, ?)"

		args = append(args, track.AudioFileID, track.Position)
	}

	// SAFETY: Multi-row INSERT with variable row count unsupported by sqlc. Placeholder count matches args length; no string interpolation.
	query := "INSERT INTO queue_tracks (audio_file_id, position) VALUES " +
		strings.Join(valuePlaceholders, ",")

	_, err := tx.ExecContext(q.db.Ctx, query, args...)
	if err != nil {
		return fmt.Errorf("batch insert failed: %w", err)
	}

	return nil
}

// persistState writes the queue metadata to the database.
func (q *Queue) persistState() {
	var shuffleOrderJSON sql.NullString

	if len(q.shuffleOrder) > 0 {
		data, err := json.Marshal(q.shuffleOrder)
		if err != nil {
			q.logger.Error(
				"Failed to marshal shuffle order",
				"err", err,
			)
		} else {
			shuffleOrderJSON = sql.NullString{
				String: string(data),
				Valid:  true,
			}
		}
	}

	sourcePlaylistID := sql.NullInt64{}
	if q.sourcePlaylistID > 0 {
		sourcePlaylistID = sql.NullInt64{
			Int64: q.sourcePlaylistID,
			Valid: true,
		}
	}

	err := q.db.Queries.UpdateQueueState(
		q.db.Ctx,
		sqlcgen.UpdateQueueStateParams{
			SourcePlaylistID: sourcePlaylistID,
			CurrentPosition:  int64(q.currentIndex),
			ShuffleMode:      q.shuffleMode,
			RepeatMode:       string(q.repeatMode),
			ShuffleOrder:     shuffleOrderJSON,
		},
	)
	if err != nil {
		q.logger.Error("Failed to persist queue state", "err", err)
	}
}

// SaveState persists the queue state to the database.
func (q *Queue) SaveState() {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.persistTracks()
	q.persistState()
	q.logger.Info("Queue state saved",
		"trackCount", len(q.tracks),
		"currentIndex", q.currentIndex,
		"shuffleMode", q.shuffleMode,
		"repeatMode", q.repeatMode,
	)
}

// RestoreState loads the queue state from the database.
func (q *Queue) RestoreState() {
	defer profiling.TimeOp(q.logger, "queue.RestoreState")()

	q.mu.Lock()
	defer q.mu.Unlock()

	// Restore queue metadata.
	state, err := q.db.Queries.GetQueueState(q.db.Ctx)
	if err != nil {
		q.logger.Error("Failed to load queue state", "err", err)

		return
	}

	q.currentIndex = int(state.CurrentPosition)
	q.shuffleMode = state.ShuffleMode
	q.repeatMode = RepeatMode(state.RepeatMode)

	if state.SourcePlaylistID.Valid {
		q.sourcePlaylistID = state.SourcePlaylistID.Int64
	}

	// Restore shuffle order.
	if state.ShuffleOrder.Valid && state.ShuffleOrder.String != "" {
		var order []int

		if err := json.Unmarshal(
			[]byte(state.ShuffleOrder.String), &order,
		); err != nil {
			q.logger.Warn("Failed to parse shuffle order", "err", err)
		} else {
			q.shuffleOrder = order
		}
	}

	// Restore queue tracks.
	rows, err := q.db.Queries.GetQueueTracks(q.db.Ctx)
	if err != nil {
		q.logger.Error("Failed to load queue tracks", "err", err)

		return
	}

	q.tracks = make([]Track, 0, len(rows))

	for _, row := range rows {
		q.tracks = append(q.tracks, Track{
			ID:          row.ID,
			AudioFileID: row.AudioFileID,
			FilePath:    row.FilePath,
			Position:    row.Position,
			Title:       row.Title,
			Artist:      row.Artist,
		})
	}

	// Clamp current index. A value of -1 is valid and means "no current
	// track" (e.g. the queue was exhausted before shutdown). Only clamp
	// when the index exceeds the restored track count.
	if q.currentIndex >= len(q.tracks) && len(q.tracks) > 0 {
		q.currentIndex = len(q.tracks) - 1
	}

	q.logger.Info("Queue state restored",
		"trackCount", len(q.tracks),
		"currentIndex", q.currentIndex,
		"shuffleMode", q.shuffleMode,
		"repeatMode", q.repeatMode,
	)
}
