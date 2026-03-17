package tagwriter

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"yellowjacket/backend/database"
	"yellowjacket/backend/database/sql/sqlcgen"
	"yellowjacket/backend/metadata"
)

// dbSyncParams holds the context needed by syncDatabase to update
// all database entities after a successful file tag write.
type dbSyncParams struct {
	audioFileID  int64
	recordingID  int64
	filePath     string
	changes      TagChanges
	oldRecording sqlcgen.Recording
	oldRGLinks   []sqlcgen.ReleaseGroupRecording
}

// syncDatabase runs all database updates for a tag write inside a
// single transaction: entity upsert-and-relink, FTS5 update, and
// orphan cleanup.  If anything fails the entire transaction is
// rolled back, leaving the database at its previous state.
func syncDatabase(
	ctx context.Context,
	logger *slog.Logger,
	db *database.DB,
	params dbSyncParams,
) error {
	tx, err := db.BeginTx()
	if err != nil {
		return fmt.Errorf("begin sync tx: %w", err)
	}

	defer func() { _ = tx.Rollback() }() // no-op after commit

	txq := db.Queries.WithTx(tx)

	// ------------------------------------------------------------------
	// Track old entity IDs for orphan cleanup after relinking.
	// ------------------------------------------------------------------
	oldArtistCreditID := params.oldRecording.ArtistCreditID

	oldRGIDs := make([]int64, 0, len(params.oldRGLinks))
	for _, link := range params.oldRGLinks {
		oldRGIDs = append(oldRGIDs, link.ReleaseGroupID)
	}

	// newArtistCreditID starts as old; overwritten if artist changes.
	newArtistCreditID := oldArtistCreditID

	// ------------------------------------------------------------------
	// 1. Handle artist change.
	// ------------------------------------------------------------------
	if v, ok := params.changes[FieldArtist].(string); ok {
		newAC, acErr := txq.UpsertArtistCredit(ctx, v)
		if acErr != nil {
			return fmt.Errorf("upsert artist credit: %w", acErr)
		}

		newArtistCreditID = newAC.ID

		newArtist, artErr := txq.UpsertArtist(ctx, v)
		if artErr != nil {
			return fmt.Errorf("upsert artist: %w", artErr)
		}

		// Link artist → credit (INSERT OR IGNORE handles dupes).
		if _, linkErr := txq.CreateArtistCreditArtist(ctx,
			sqlcgen.CreateArtistCreditArtistParams{
				ArtistID: newArtist.ID,
				CreditID: newAC.ID,
			},
		); linkErr != nil && !database.IsUniqueViolation(linkErr) {
			return fmt.Errorf("link artist to credit: %w", linkErr)
		}
	}

	// ------------------------------------------------------------------
	// 2. Handle album change.
	// ------------------------------------------------------------------
	if newAlbumName, ok := params.changes[FieldAlbum].(string); ok {
		// Determine album-artist credit ID.
		albumArtistCreditID := sql.NullInt64{
			Int64: newArtistCreditID, Valid: true,
		}

		if aav, aaOK := params.changes[FieldAlbumArtist].(string); aaOK && aav != "" {
			aaCredit, aaErr := txq.UpsertArtistCredit(ctx, aav)
			if aaErr != nil {
				return fmt.Errorf("upsert album artist credit: %w", aaErr)
			}

			albumArtistCreditID = sql.NullInt64{
				Int64: aaCredit.ID, Valid: true,
			}

			aaArtist, aaArtErr := txq.UpsertArtist(ctx, aav)
			if aaArtErr != nil {
				return fmt.Errorf("upsert album artist: %w", aaArtErr)
			}

			if _, aaLinkErr := txq.CreateArtistCreditArtist(ctx,
				sqlcgen.CreateArtistCreditArtistParams{
					ArtistID: aaArtist.ID,
					CreditID: aaCredit.ID,
				},
			); aaLinkErr != nil && !database.IsUniqueViolation(aaLinkErr) {
				return fmt.Errorf("link album artist to credit: %w", aaLinkErr)
			}
		}

		// Determine year value.
		yearVal := params.oldRecording.Year
		if yv, yOK := params.changes[FieldYear].(int); yOK {
			yearVal = toNullInt64(yv)
		}

		// Upsert new release group.
		newRG, rgErr := txq.UpsertReleaseGroup(ctx,
			sqlcgen.UpsertReleaseGroupParams{
				Name:                newAlbumName,
				AlbumArtistCreditID: albumArtistCreditID,
				Year:                yearVal,
			},
		)
		if rgErr != nil {
			return fmt.Errorf("upsert release group: %w", rgErr)
		}

		// Unlink old release_group_recordings.
		for _, oldLink := range params.oldRGLinks {
			if unlinkErr := txq.DeleteReleaseGroupRecordingByFK(ctx,
				sqlcgen.DeleteReleaseGroupRecordingByFKParams{
					ReleaseGroupID: oldLink.ReleaseGroupID,
					RecordingID:    params.recordingID,
				},
			); unlinkErr != nil {
				return fmt.Errorf("unlink old rg recording: %w", unlinkErr)
			}
		}

		// Determine track/disc numbers.
		trackNum := params.oldRecording.TrackNumber
		if tn, tnOK := params.changes[FieldTrackNumber].(int); tnOK {
			trackNum = toNullInt64(tn)
		}

		discNum := params.oldRecording.DiscNumber
		if dn, dnOK := params.changes[FieldDiscNumber].(int); dnOK {
			discNum = toNullInt64(dn)
		}

		// Create new link.
		if _, linkErr := txq.CreateReleaseGroupRecording(ctx,
			sqlcgen.CreateReleaseGroupRecordingParams{
				ReleaseGroupID: newRG.ID,
				RecordingID:    params.recordingID,
				TrackNumber:    trackNum,
				DiscNumber:     discNum,
			},
		); linkErr != nil {
			return fmt.Errorf("create rg recording link: %w", linkErr)
		}
	}

	// ------------------------------------------------------------------
	// 3. Handle genre change.
	// ------------------------------------------------------------------
	if newGenre, ok := params.changes[FieldGenre].(string); ok {
		// Delete all existing recording_genres for this recording.
		if delErr := txq.DeleteRecordingGenres(ctx, params.recordingID); delErr != nil {
			return fmt.Errorf("delete recording genres: %w", delErr)
		}

		// Parse and link new genres.
		genres := metadata.ParseGenres(newGenre)
		for _, gName := range genres {
			g, gErr := txq.UpsertGenre(ctx, gName)
			if gErr != nil {
				return fmt.Errorf("upsert genre %q: %w", gName, gErr)
			}

			if rgErr := txq.CreateRecordingGenre(ctx,
				sqlcgen.CreateRecordingGenreParams{
					RecordingID: params.recordingID,
					GenreID:     g.ID,
				},
			); rgErr != nil {
				return fmt.Errorf("create recording genre: %w", rgErr)
			}
		}
	}

	// ------------------------------------------------------------------
	// 4. Handle cover art change (skipped in this plan — cover art
	//    save + thumbnail logic will be added when the UI sends
	//    cover art data, but the DB plumbing is ready).
	//    For now, cover art changes are a no-op in the DB sync.
	//    The file-level embed/clear is handled by the format writers.
	// ------------------------------------------------------------------

	// ------------------------------------------------------------------
	// 5. Update recording with all changed fields.
	// ------------------------------------------------------------------
	rec := params.oldRecording

	newName := rec.Name
	if v, ok := params.changes[FieldTitle].(string); ok {
		newName = v
	}

	newYear := rec.Year
	if v, ok := params.changes[FieldYear].(int); ok {
		newYear = toNullInt64(v)
	}

	newTrackNum := rec.TrackNumber
	if v, ok := params.changes[FieldTrackNumber].(int); ok {
		newTrackNum = toNullInt64(v)
	}

	newDiscNum := rec.DiscNumber
	if v, ok := params.changes[FieldDiscNumber].(int); ok {
		newDiscNum = toNullInt64(v)
	}

	newGenreStr := rec.Genre
	if v, ok := params.changes[FieldGenre].(string); ok {
		newGenreStr = toNullString(v)
	}

	newComposer := rec.Composer
	if v, ok := params.changes[FieldComposer].(string); ok {
		newComposer = toNullString(v)
	}

	if updErr := txq.UpdateRecordingFull(ctx, sqlcgen.UpdateRecordingFullParams{
		Name:           newName,
		ArtistCreditID: newArtistCreditID,
		TrackNumber:    newTrackNum,
		DiscNumber:     newDiscNum,
		Year:           newYear,
		Genre:          newGenreStr,
		Composer:       newComposer,
		Lyrics:         rec.Lyrics,
		Comment:        rec.Comment,
		ID:             params.recordingID,
	}); updErr != nil {
		return fmt.Errorf("update recording: %w", updErr)
	}

	// ------------------------------------------------------------------
	// 6. Update FTS5 search index (within the transaction).
	// ------------------------------------------------------------------
	newTitle := newName

	newArtist := params.changes[FieldArtist]
	artistStr := ""

	if newArtist != nil {
		artistStr, _ = newArtist.(string)
	}

	if artistStr == "" {
		// Look up current artist credit text from the old recording
		// if the artist hasn't changed.
		ac, acErr := txq.GetArtistCredit(ctx, newArtistCreditID)
		if acErr == nil {
			artistStr = ac.Text
		}
	}

	newAlbum := ""
	if v, ok := params.changes[FieldAlbum].(string); ok {
		newAlbum = v
	} else if len(params.oldRGLinks) > 0 {
		// Look up current album from release groups if unchanged.
		rg, rgErr := txq.GetReleaseGroup(ctx, params.oldRGLinks[0].ReleaseGroupID)
		if rgErr == nil {
			newAlbum = rg.Name
		}
	}

	// SAFETY: FTS5 DELETE on contentless_delete=1 table. Parameterized rowid.
	if _, ftsDelErr := tx.ExecContext(ctx,
		"DELETE FROM search_index WHERE rowid = ?",
		params.audioFileID,
	); ftsDelErr != nil {
		logger.Warn("FTS5 delete failed", "err", ftsDelErr,
			"audioFileID", params.audioFileID)
	}

	// SAFETY: FTS5 INSERT into contentless table. All values parameterized.
	if _, ftsInsErr := tx.ExecContext(ctx,
		`INSERT INTO search_index(rowid, file_path, title, artist, album)
		 VALUES (?, ?, ?, ?, ?)`,
		params.audioFileID, params.filePath, newTitle, artistStr, newAlbum,
	); ftsInsErr != nil {
		logger.Warn("FTS5 insert failed", "err", ftsInsErr,
			"audioFileID", params.audioFileID)
	}

	// ------------------------------------------------------------------
	// 7. Orphan cleanup (within same transaction).
	// ------------------------------------------------------------------

	// 7a. Artist credit orphan cleanup.
	if newArtistCreditID != oldArtistCreditID {
		refCount, refErr := txq.CountArtistCreditReferences(ctx, oldArtistCreditID)
		if refErr != nil {
			logger.Warn("count artist credit refs failed", "err", refErr)
		} else if refCount == 0 {
			// Delete artist_credit_artist entries for the orphaned credit,
			// then the credit itself.
			// SAFETY: Hand-crafted DELETE for orphan artist_credit_artist rows.
			// Parameterized credit_id. No sqlc query exists for this specific
			// delete-by-credit pattern.
			if _, acaErr := tx.ExecContext(ctx,
				"DELETE FROM artist_credit_artist WHERE credit_id = ?",
				oldArtistCreditID,
			); acaErr != nil {
				logger.Warn("delete orphan aca failed", "err", acaErr)
			}

			if delErr := txq.DeleteArtistCredit(ctx, oldArtistCreditID); delErr != nil {
				logger.Warn("delete orphan artist credit failed", "err", delErr)
			}
		}
	}

	// 7b. Release group orphan cleanup.
	if _, albumChanged := params.changes[FieldAlbum]; albumChanged {
		for _, oldRGID := range oldRGIDs {
			rgCount, rgErr := txq.CountReleaseGroupRecordings(ctx, oldRGID)
			if rgErr != nil {
				logger.Warn("count rg recordings failed", "err", rgErr,
					"releaseGroupID", oldRGID)

				continue
			}

			if rgCount == 0 {
				if delErr := txq.DeleteReleaseGroup(ctx, oldRGID); delErr != nil {
					logger.Warn("delete orphan release group failed",
						"err", delErr, "releaseGroupID", oldRGID)
				}
			}
		}
	}

	// 7c. Genre orphan cleanup — delete genres with no remaining
	//     recording_genres references.  This is safe because genres
	//     are only referenced via recording_genres.
	if _, genreChanged := params.changes[FieldGenre]; genreChanged {
		// SAFETY: Hand-crafted DELETE for orphan genres. No user input.
		// Matches the global orphan pattern from library/crud.go.
		if _, gErr := tx.ExecContext(ctx,
			`DELETE FROM genres WHERE id NOT IN
			 (SELECT DISTINCT genre_id FROM recording_genres)`,
		); gErr != nil {
			logger.Warn("genre orphan cleanup failed", "err", gErr)
		}
	}

	// ------------------------------------------------------------------
	// 8. Commit.
	// ------------------------------------------------------------------
	return tx.Commit()
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
