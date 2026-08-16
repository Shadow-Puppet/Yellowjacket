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

// dbSyncParams holds the context needed by syncDatabase to update the
// database after a successful file tag write.
type dbSyncParams struct {
	audioFileID int64
	filePath    string
	changes     TagChanges
	oldFile     sqlcgen.AudioFile
}

// syncDatabase runs the database updates for a tag write inside a
// single transaction: the file's own tag columns, its album and artist
// links, its genres, its cover art, and the FTS row.  If anything fails
// the whole transaction rolls back.
//
// This used to be four times longer, and three of the four parts were
// bookkeeping for tables that no longer exist: relinking a recording to
// a new artist_credit, unlinking and relinking release_group_recordings,
// and then three orphan sweeps to delete whichever of those rows the
// relink had stranded.  Tags live on the file now, so changing a tag is
// an UPDATE, and nothing can be stranded by one.
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
	old := params.oldFile

	// ------------------------------------------------------------------
	// 1. Resolve the artist, if it changed.
	// ------------------------------------------------------------------
	artistCredit := old.ArtistCredit
	artistID := old.ArtistID

	if v, ok := params.changes[FieldArtist].(string); ok {
		artistCredit = v

		artist, artErr := txq.UpsertArtist(ctx, sqlcgen.UpsertArtistParams{Name: v})
		if artErr != nil {
			return fmt.Errorf("upsert artist: %w", artErr)
		}

		artistID = sql.NullInt64{Int64: artist.ID, Valid: true}
	}

	// ------------------------------------------------------------------
	// 2. Resolve the album, if it changed.
	// ------------------------------------------------------------------
	albumID := old.AlbumID

	if newAlbumName, ok := params.changes[FieldAlbum].(string); ok {
		albumCredit := artistCredit
		albumArtistID := artistID

		if aav, aaOK := params.changes[FieldAlbumArtist].(string); aaOK && aav != "" {
			albumCredit = aav

			aaArtist, aaErr := txq.UpsertArtist(ctx, sqlcgen.UpsertArtistParams{Name: aav})
			if aaErr != nil {
				return fmt.Errorf("upsert album artist: %w", aaErr)
			}

			albumArtistID = sql.NullInt64{Int64: aaArtist.ID, Valid: true}
		}

		year := old.Year
		if yv, yOK := asInt(params.changes[FieldYear]); yOK {
			year = toNullInt64(yv)
		}

		album, albErr := txq.UpsertAlbum(ctx, sqlcgen.UpsertAlbumParams{
			Name:         newAlbumName,
			ArtistCredit: albumCredit,
			ArtistID:     albumArtistID,
			Year:         year,
		})
		if albErr != nil {
			return fmt.Errorf("upsert album: %w", albErr)
		}

		albumID = sql.NullInt64{Int64: album.ID, Valid: true}
	}

	// ------------------------------------------------------------------
	// 3. Genres, if they changed.
	// ------------------------------------------------------------------
	if newGenre, ok := params.changes[FieldGenre].(string); ok {
		if delErr := txq.DeleteFileGenres(ctx, params.audioFileID); delErr != nil {
			return fmt.Errorf("delete file genres: %w", delErr)
		}

		for _, gName := range metadata.ParseGenres(newGenre) {
			g, gErr := txq.UpsertGenre(ctx, gName)
			if gErr != nil {
				return fmt.Errorf("upsert genre %q: %w", gName, gErr)
			}

			if linkErr := txq.LinkFileGenre(ctx, sqlcgen.LinkFileGenreParams{
				AudioFileID: params.audioFileID,
				GenreID:     g.ID,
			}); linkErr != nil {
				return fmt.Errorf("link file genre: %w", linkErr)
			}
		}
	}

	// ------------------------------------------------------------------
	// 4. Cover art, if it changed.  It belongs to the album, so a file
	//    with no album has nowhere to put it.
	// ------------------------------------------------------------------
	if _, hasCoverArt := params.changes[FieldCoverArt]; hasCoverArt && albumID.Valid {
		coverArtID := sql.NullInt64{}

		if data, isBytes := asBytes(params.changes[FieldCoverArt]); isBytes && len(data) > 0 {
			newID, caErr := saveCoverArtAndSync(ctx, logger, txq, data)
			if caErr != nil {
				logger.Warn("cover art sync failed", "err", caErr)
			} else {
				coverArtID = sql.NullInt64{Int64: newID, Valid: true}
			}
		}

		if upErr := txq.SetAlbumCoverArt(ctx, sqlcgen.SetAlbumCoverArtParams{
			CoverArtID: coverArtID,
			ID:         albumID.Int64,
		}); upErr != nil {
			logger.Warn("update album cover art failed", "err", upErr, "albumID", albumID.Int64)
		}
	}

	// ------------------------------------------------------------------
	// 5. The file's own tag columns.
	// ------------------------------------------------------------------
	title := old.Title
	if v, ok := params.changes[FieldTitle].(string); ok {
		title = v
	}

	year := old.Year
	if v, ok := asInt(params.changes[FieldYear]); ok {
		year = toNullInt64(v)
	}

	trackNum := old.TrackNumber
	if v, ok := asInt(params.changes[FieldTrackNumber]); ok {
		trackNum = toNullInt64(v)
	}

	discNum := old.DiscNumber
	if v, ok := asInt(params.changes[FieldDiscNumber]); ok {
		discNum = toNullInt64(v)
	}

	composer := old.Composer
	if v, ok := params.changes[FieldComposer].(string); ok {
		composer = v
	}

	// Writing tags rewrites the file, changing its mtime and possibly
	// its size.  Recording the new values keeps the scan from mistaking
	// YellowJacket's own edit for an external one and re-importing.
	modifiedAt, fileSize := old.ModifiedAt, old.FileSize

	if info, statErr := os.Stat(params.filePath); statErr != nil {
		logger.Warn("could not stat file after tag write",
			"path", params.filePath, "err", statErr)
	} else {
		modifiedAt, fileSize = info.ModTime().Unix(), info.Size()
	}

	if updErr := txq.UpdateAudioFileTags(ctx, sqlcgen.UpdateAudioFileTagsParams{
		Title:              title,
		ArtistCredit:       artistCredit,
		ArtistID:           artistID,
		AlbumID:            albumID,
		TrackNumber:        trackNum,
		DiscNumber:         discNum,
		TotalTracks:        old.TotalTracks,
		Year:               year,
		Composer:           composer,
		Comment:            old.Comment,
		RecordingMbid:      old.RecordingMbid,
		SampleRate:         old.SampleRate,
		BitDepth:           old.BitDepth,
		Channels:           old.Channels,
		Bitrate:            old.Bitrate,
		FileSize:           fileSize,
		LengthMilliseconds: old.LengthMilliseconds,
		ModifiedAt:         modifiedAt,
		ID:                 params.audioFileID,
	}); updErr != nil {
		return fmt.Errorf("update audio file tags: %w", updErr)
	}

	// ------------------------------------------------------------------
	// 6. The FTS row.
	// ------------------------------------------------------------------
	album := ""

	if albumID.Valid {
		if row, albErr := txq.GetAlbum(ctx, albumID.Int64); albErr == nil {
			album = row.Name
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
		params.audioFileID, params.filePath, title, artistCredit, album,
	); ftsInsErr != nil {
		logger.Warn("FTS5 insert failed", "err", ftsInsErr,
			"audioFileID", params.audioFileID)
	}

	// Albums and artists left empty by this write are swept by the
	// library's own cleanup, which is one query each now.
	return tx.Commit()
}

// toNullInt64 converts an int to sql.NullInt64, treating 0 as null.
func toNullInt64(v int) sql.NullInt64 {
	if v == 0 {
		return sql.NullInt64{}
	}

	return sql.NullInt64{Int64: int64(v), Valid: true}
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

	// Content-hash filename (same scheme as library/coverart.go): the
	// tiers are the only thing stored, and the largest is the cover's
	// canonical path.  The full-resolution bytes stay in the file the
	// user just wrote them to.
	hash := sha256.Sum256(data)
	hashStr := hex.EncodeToString(hash[:8])
	mime := detectMIME(data)

	filePath := filepath.Join(
		coverDir, coverart.SizedFilename(hashStr, largestTierSuffix),
	)

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

// largestTierSuffix is the tier stored as a cover's canonical path.
const largestTierSuffix = "_lg"

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
