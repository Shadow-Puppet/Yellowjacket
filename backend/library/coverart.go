package library

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png" // Register PNG decoder.
	"os"
	"path/filepath"
	"time"

	"golang.org/x/image/draw"

	"yellowjacket/backend/coverart"
	"yellowjacket/backend/metadata"
)

// thumbnailTier defines a single size tier for generated cover art thumbnails.
type thumbnailTier struct {
	// Suffix appended to the content hash (e.g. "_sm", "_md", "_lg").
	Suffix string
	// MaxSize is the maximum width or height in pixels.
	MaxSize int
	// Quality is the JPEG encoding quality (1-100).
	Quality int
}

// thumbnailWork is a unit of work for the async thumbnail worker pool.
type thumbnailWork struct {
	imgData []byte
	dir     string
	hashStr string
	metrics *ScanMetrics
}

// thumbnailTiers lists all generated size variants, ordered smallest to largest.
var thumbnailTiers = []thumbnailTier{
	{Suffix: "_sm", MaxSize: 100, Quality: 75},
	{Suffix: "_md", MaxSize: 200, Quality: 80},
	{Suffix: "_lg", MaxSize: 400, Quality: 85},
}

// largestTier is the tier stored as the cover's canonical file.
func largestTier() thumbnailTier {
	return thumbnailTiers[len(thumbnailTiers)-1]
}

// CoverArtFileSet returns every file on disk belonging to one cover art
// entry: each generated size variant.
//
// Only one of them is recorded in cover_art.file_path — the others are
// derived filenames — so any code deleting cover art has to expand the
// set or the rest are orphaned.
func CoverArtFileSet(coverPath string) []string {
	dir := filepath.Dir(coverPath)
	base := filepath.Base(coverPath)

	paths := make([]string, 0, len(thumbnailTiers))

	for _, tier := range thumbnailTiers {
		paths = append(paths, filepath.Join(
			dir, coverart.SizedFilename(base, tier.Suffix),
		))
	}

	return paths
}

// saveCoverArt saves embedded cover art to the cache directory.
// Returns the file path where the art was saved, or empty string
// if no picture data.  Timing is recorded in the provided metrics.
// When thumbChan is non-nil, thumbnail generation is dispatched
// asynchronously to a worker pool instead of running inline.
func (l *Library) saveCoverArt(
	pic *metadata.PictureData,
	metrics *ScanMetrics,
	thumbChan chan<- thumbnailWork,
) (string, error) {
	if pic == nil || len(pic.Data) == 0 {
		return "", nil
	}

	saveStart := time.Now()

	// Get the covers directory for storing cover art.
	coverDir, err := coverart.CoversDir()
	if err != nil {
		return "", fmt.Errorf(
			"could not resolve covers directory: %w", err,
		)
	}

	// Ensure directory exists.
	if err := os.MkdirAll(coverDir, 0o755); err != nil {
		return "", fmt.Errorf(
			"could not create covers directory: %w", err,
		)
	}

	// The content hash identifies the cover and dedupes it; the largest
	// tier is what gets stored under that name.
	//
	// The full-resolution image used to be written here too, and it was
	// 1,134 MB of a 1.4 GB covers directory on a real library - against
	// 110 MB for all three tiers together - with nothing rendering it.
	// The bytes are still in the audio file if a bigger one is ever
	// needed, which is where these came from.
	hash := sha256.Sum256(pic.Data)
	hashStr := hex.EncodeToString(hash[:8]) // First 8 bytes = 16 hex chars.

	filePath := filepath.Join(
		coverDir, coverart.SizedFilename(hashStr, largestTier().Suffix),
	)

	// Skip if this cover has already been stored (same content hash).
	if _, err := os.Stat(filePath); err == nil {
		l.logger.Debug("cover art already stored", "path", filePath)

		return filePath, nil
	}

	// Dispatch thumbnail generation to the async worker pool if
	// available, otherwise generate inline.
	if thumbChan != nil {
		thumbChan <- thumbnailWork{
			imgData: pic.Data,
			dir:     coverDir,
			hashStr: hashStr,
			metrics: metrics,
		}
	} else if err := l.generateSizedVariantsWithMetrics(
		pic.Data, coverDir, hashStr, metrics,
	); err != nil {
		l.logger.Warn("could not generate sized variants",
			"path", filePath, "err", err)
	}

	metrics.addCoverArtSave(time.Since(saveStart))

	return filePath, nil
}

// generateSizedVariantsWithMetrics is like generateSizedVariants
// but records per-tier timing in the provided metrics.
func (l *Library) generateSizedVariantsWithMetrics(
	imgData []byte,
	dir, hashStr string,
	metrics *ScanMetrics,
) error {
	src, _, err := image.Decode(bytes.NewReader(imgData))
	if err != nil {
		return fmt.Errorf(
			"could not decode image for thumbnails: %w", err,
		)
	}

	bounds := src.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()

	for _, tier := range thumbnailTiers {
		tierStart := time.Now()

		tierPath := filepath.Join(
			dir,
			fmt.Sprintf("%s%s.jpg", hashStr, tier.Suffix),
		)

		w, h := fitDimensions(srcW, srcH, tier.MaxSize)

		if err := encodeAndSaveImage(
			src, tierPath, w, h, tier.Quality,
		); err != nil {
			l.logger.Warn(
				"could not generate sized variant",
				"tier", tier.Suffix,
				"path", tierPath,
				"err", err,
			)

			continue
		}

		metrics.addThumbnailTier(
			tier.Suffix, time.Since(tierStart),
		)

		l.logger.Debug(
			"saved sized variant",
			"tier", tier.Suffix,
			"path", tierPath,
			"dimensions", fmt.Sprintf("%dx%d", w, h),
		)
	}

	return nil
}

// fitDimensions calculates the output dimensions that fit within maxSize
// while preserving the aspect ratio.  If the source is already smaller
// than maxSize, the original dimensions are returned unchanged.
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

// encodeAndSaveImage scales the source image to the given dimensions
// and saves it as a JPEG with the specified quality.
func encodeAndSaveImage(
	src image.Image,
	path string,
	w, h, quality int,
) error {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.ApproxBiLinear.Scale(
		dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil,
	)

	var buf bytes.Buffer

	if err := jpeg.Encode(
		&buf, dst, &jpeg.Options{Quality: quality},
	); err != nil {
		return fmt.Errorf("could not encode image: %w", err)
	}

	if err := os.WriteFile(
		path, buf.Bytes(), 0o644,
	); err != nil {
		return fmt.Errorf("could not write image: %w", err)
	}

	return nil
}
