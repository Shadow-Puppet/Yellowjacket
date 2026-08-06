package tagwriter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png" // Register PNG decoder for cover art thumbnails.
	"log/slog"
	"os"
	"path/filepath"

	"golang.org/x/image/draw"

	"yellowjacket/backend/coverart"
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
		if yv, yOK := asInt(params.changes[FieldYear]); yOK {
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
		if tn, tnOK := asInt(params.changes[FieldTrackNumber]); tnOK {
			trackNum = toNullInt64(tn)
		}

		discNum := params.oldRecording.DiscNumber
		if dn, dnOK := asInt(params.changes[FieldDiscNumber]); dnOK {
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
	// 4. Handle cover art change — save image to covers cache,
	//    upsert cover_art row, and update release_groups.cover_art_id.
	// ------------------------------------------------------------------
	if _, hasCoverArt := params.changes[FieldCoverArt]; hasCoverArt {
		coverArtData, isBytes := asBytes(params.changes[FieldCoverArt])

		if isBytes && len(coverArtData) > 0 {
			// Save to covers dir and upsert DB row.
			newCoverArtID, caErr := saveCoverArtAndSync(
				ctx, logger, txq, coverArtData,
			)
			if caErr != nil {
				logger.Warn("cover art sync failed", "err", caErr)
			} else {
				// Update all release groups linked to this recording.
				for _, rgLink := range params.oldRGLinks {
					if upErr := txq.UpdateReleaseGroupCoverArt(ctx,
						sqlcgen.UpdateReleaseGroupCoverArtParams{
							CoverArtID: sql.NullInt64{Int64: newCoverArtID, Valid: true},
							ID:         rgLink.ReleaseGroupID,
						},
					); upErr != nil {
						logger.Warn("update rg cover art failed",
							"err", upErr,
							"releaseGroupID", rgLink.ReleaseGroupID)
					}
				}
			}
		} else {
			// Clear: set cover_art_id to NULL on all linked release groups.
			for _, rgLink := range params.oldRGLinks {
				if upErr := txq.UpdateReleaseGroupCoverArt(ctx,
					sqlcgen.UpdateReleaseGroupCoverArtParams{
						CoverArtID: sql.NullInt64{},
						ID:         rgLink.ReleaseGroupID,
					},
				); upErr != nil {
					logger.Warn("clear rg cover art failed",
						"err", upErr,
						"releaseGroupID", rgLink.ReleaseGroupID)
				}
			}
		}
	}

	// ------------------------------------------------------------------
	// 5. Update recording with all changed fields.
	// ------------------------------------------------------------------
	rec := params.oldRecording

	newName := rec.Name
	if v, ok := params.changes[FieldTitle].(string); ok {
		newName = v
	}

	newYear := rec.Year
	if v, ok := asInt(params.changes[FieldYear]); ok {
		newYear = toNullInt64(v)
	}

	newTrackNum := rec.TrackNumber
	if v, ok := asInt(params.changes[FieldTrackNumber]); ok {
		newTrackNum = toNullInt64(v)
	}

	newDiscNum := rec.DiscNumber
	if v, ok := asInt(params.changes[FieldDiscNumber]); ok {
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
	// 7d. Re-baseline the staleness fields.  Writing tags rewrites the
	//     file, changing its mtime and possibly its size.  Recording the
	//     new values here keeps the scan from mistaking YellowJacket's
	//     own edit for an external one and re-importing the track.
	// ------------------------------------------------------------------
	if info, statErr := os.Stat(params.filePath); statErr != nil {
		logger.Warn("could not stat file after tag write",
			"path", params.filePath, "err", statErr)
	} else if updErr := txq.UpdateAudioFileStat(ctx,
		sqlcgen.UpdateAudioFileStatParams{
			ModifiedAt: info.ModTime().Unix(),
			FileSize:   info.Size(),
			ID:         params.audioFileID,
		},
	); updErr != nil {
		logger.Warn("could not update file stat after tag write",
			"path", params.filePath, "err", updErr)
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

// saveCoverArtAndSync saves cover art bytes to the covers cache
// directory (with content-hash deduplication), generates sized
// thumbnails, upserts a cover_art DB row, and returns the row ID.
func saveCoverArtAndSync(
	ctx context.Context,
	logger *slog.Logger,
	txq *sqlcgen.Queries,
	data []byte,
) (int64, error) {
	coverDir, err := coverart.CoversDir()
	if err != nil {
		return 0, fmt.Errorf("resolve covers dir: %w", err)
	}

	if err := os.MkdirAll(coverDir, 0o755); err != nil {
		return 0, fmt.Errorf("create covers dir: %w", err)
	}

	// Content-hash filename (same scheme as library/coverart.go).
	hash := sha256.Sum256(data)
	hashStr := hex.EncodeToString(hash[:8])
	mime := detectMIME(data)

	ext := "jpg"
	if mime == "image/png" {
		ext = "png"
	}

	filename := fmt.Sprintf("%s.%s", hashStr, ext)
	filePath := filepath.Join(coverDir, filename)

	// Write original if not already present.
	if _, statErr := os.Stat(filePath); statErr != nil {
		if writeErr := os.WriteFile(filePath, data, 0o644); writeErr != nil {
			return 0, fmt.Errorf("write cover art: %w", writeErr)
		}
	}

	// Generate sized variants (thumbnails).
	generateSizedVariants(logger, data, coverDir, hashStr)

	// Upsert cover_art DB row.
	ca, err := txq.UpsertCoverArt(ctx, sqlcgen.UpsertCoverArtParams{
		IsEmbedded: true,
		FilePath:   filePath,
		MimeType:   mime,
	})
	if err != nil {
		return 0, fmt.Errorf("upsert cover art: %w", err)
	}

	return ca.ID, nil
}

// thumbnailTier defines a single size tier for generated thumbnails.
type thumbnailTier struct {
	suffix  string
	maxSize int
	quality int
}

// thumbnailTiers matches the tiers in library/coverart.go.
var thumbnailTiers = []thumbnailTier{
	{suffix: "_sm", maxSize: 100, quality: 75},
	{suffix: "_md", maxSize: 200, quality: 80},
	{suffix: "_lg", maxSize: 400, quality: 85},
}

// generateSizedVariants creates all thumbnail tiers for the given image.
func generateSizedVariants(logger *slog.Logger, imgData []byte, dir, hashStr string) {
	src, _, err := image.Decode(bytes.NewReader(imgData))
	if err != nil {
		logger.Warn("could not decode image for thumbnails", "err", err)

		return
	}

	bounds := src.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()

	for _, tier := range thumbnailTiers {
		tierPath := filepath.Join(dir, fmt.Sprintf("%s%s.jpg", hashStr, tier.suffix))

		// Skip if already exists.
		if _, statErr := os.Stat(tierPath); statErr == nil {
			continue
		}

		w, h := fitDimensions(srcW, srcH, tier.maxSize)

		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)

		var buf bytes.Buffer
		if encErr := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: tier.quality}); encErr != nil {
			logger.Warn("could not encode thumbnail", "tier", tier.suffix, "err", encErr)

			continue
		}

		if writeErr := os.WriteFile(tierPath, buf.Bytes(), 0o644); writeErr != nil {
			logger.Warn("could not write thumbnail", "tier", tier.suffix, "err", writeErr)
		}
	}
}

// fitDimensions calculates output dimensions that fit within maxSize
// while preserving aspect ratio.
func fitDimensions(srcW, srcH, maxSize int) (int, int) {
	if srcW <= maxSize && srcH <= maxSize {
		return srcW, srcH
	}

	w, h := maxSize, maxSize
	if srcW > srcH {
		h = srcH * maxSize / srcW
	} else {
		w = srcW * maxSize / srcH
	}

	return w, h
}
