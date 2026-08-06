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
	"strings"
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

// legacyThumbSuffix is the old single-thumbnail suffix used before the
// multi-tier system.  Kept for migration purposes only.
const legacyThumbSuffix = "_thumb"

// CoverArtFileSet returns every file on disk belonging to one cover art
// entry: the original plus each generated size variant, plus the legacy
// _thumb file for databases that predate the multi-tier thumbnails.
//
// Only the original is recorded in cover_art.file_path — the variants
// are derived filenames — so any code deleting cover art has to expand
// the set or the thumbnails are orphaned.
func CoverArtFileSet(originalPath string) []string {
	dir := filepath.Dir(originalPath)
	base := filepath.Base(originalPath)

	paths := make([]string, 0, len(thumbnailTiers)+2) //nolint:mnd

	paths = append(paths, originalPath)

	for _, tier := range thumbnailTiers {
		paths = append(paths, filepath.Join(
			dir, coverart.SizedFilename(base, tier.Suffix),
		))
	}

	return append(paths, filepath.Join(
		dir, coverart.SizedFilename(base, legacyThumbSuffix),
	))
}

// isSizedVariant reports whether a filename contains any known size suffix
// (current tiers or legacy).
func isSizedVariant(name string) bool {
	if strings.Contains(name, legacyThumbSuffix) {
		return true
	}

	for _, tier := range thumbnailTiers {
		if strings.Contains(name, tier.Suffix) {
			return true
		}
	}

	return false
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

	// Generate filename from content hash (deduplication).
	hash := sha256.Sum256(pic.Data)
	hashStr := hex.EncodeToString(hash[:8]) // First 8 bytes = 16 hex chars.

	ext := pic.Ext
	if ext == "" {
		ext = extensionFromMIME(pic.MIMEType)
	}

	filename := fmt.Sprintf("%s.%s", hashStr, ext)
	filePath := filepath.Join(coverDir, filename)

	// Skip if already exists (same content hash).
	// Missing sized variants are handled by
	// generateMissingSizedVariants() at the end of a scan.
	if _, err := os.Stat(filePath); err == nil {
		l.logger.Debug(
			"cover art already exists", "path", filePath,
		)

		return filePath, nil
	}

	// Write file.
	if err := os.WriteFile(
		filePath, pic.Data, 0o644,
	); err != nil {
		return "", fmt.Errorf(
			"could not write cover art: %w", err,
		)
	}

	metrics.addCoverArtSave(time.Since(saveStart))

	l.logger.Debug(
		"saved cover art",
		"path", filePath, "size", len(pic.Data),
	)

	// Dispatch thumbnail generation to the async worker pool
	// if available, otherwise generate inline.
	if thumbChan != nil {
		thumbChan <- thumbnailWork{
			imgData: pic.Data,
			dir:     coverDir,
			hashStr: hashStr,
			metrics: metrics,
		}
	} else {
		if err := l.generateSizedVariantsWithMetrics(
			pic.Data, coverDir, hashStr, metrics,
		); err != nil {
			l.logger.Warn(
				"could not generate sized variants",
				"path", filePath, "err", err,
			)
		}
	}

	return filePath, nil
}

// generateSizedVariants creates all thumbnail tiers for the given image data.
// Each tier is saved as {hashStr}{suffix}.jpg in the given directory.
func (l *Library) generateSizedVariants(
	imgData []byte,
	dir, hashStr string,
) error {
	src, _, err := image.Decode(bytes.NewReader(imgData))
	if err != nil {
		return fmt.Errorf(
			"could not decode image for thumbnails: %w", err,
		)
	}

	l.generateTiersFromImage(src, dir, hashStr)

	return nil
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

// generateTiersFromImage creates all thumbnail tiers from an
// already-decoded image.
func (l *Library) generateTiersFromImage(
	src image.Image,
	dir, hashStr string,
) {
	bounds := src.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()

	for _, tier := range thumbnailTiers {
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

		l.logger.Debug(
			"saved sized variant",
			"tier", tier.Suffix,
			"path", tierPath,
			"dimensions", fmt.Sprintf("%dx%d", w, h),
		)
	}
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

// generateMissingSizedVariants scans the covers directory, migrates legacy
// _thumb files to _md, and generates any missing sized variants for each
// original cover art file.
func (l *Library) generateMissingSizedVariants() error {
	coverDir, err := coverart.CoversDir()
	if err != nil {
		return fmt.Errorf(
			"could not resolve covers directory: %w", err,
		)
	}

	entries, err := os.ReadDir(coverDir)
	if err != nil {
		return fmt.Errorf(
			"could not read covers directory: %w", err,
		)
	}

	// Build a set of existing filenames for quick lookup.
	existing := make(map[string]struct{}, len(entries))

	for _, entry := range entries {
		if !entry.IsDir() {
			existing[entry.Name()] = struct{}{}
		}
	}

	// First pass: migrate legacy _thumb files to _md.
	migrated := l.migrateLegacyThumbs(
		coverDir, existing,
	)

	// Second pass: generate missing sized variants.
	var generated, skipped int

	for _, entry := range entries {
		name := entry.Name()

		// Skip directories and any sized variants.
		if entry.IsDir() || isSizedVariant(name) {
			continue
		}

		hashStr := strings.SplitN(name, ".", 2)[0]

		// Check which tiers are missing.
		allPresent := true

		for _, tier := range thumbnailTiers {
			tierName := fmt.Sprintf(
				"%s%s.jpg", hashStr, tier.Suffix,
			)
			if _, exists := existing[tierName]; !exists {
				allPresent = false

				break
			}
		}

		if allPresent {
			skipped++

			continue
		}

		// Read the original and generate missing tiers.
		imgData, err := os.ReadFile(
			filepath.Join(coverDir, name),
		)
		if err != nil {
			l.logger.Warn(
				"could not read cover art for variant generation",
				"file", name, "err", err,
			)

			continue
		}

		if err := l.generateSizedVariants(
			imgData, coverDir, hashStr,
		); err != nil {
			l.logger.Warn(
				"could not generate sized variants",
				"file", name, "err", err,
			)

			continue
		}

		generated++
	}

	l.logger.Info(
		"sized variant generation complete",
		"generated", generated,
		"skipped", skipped,
		"migrated", migrated,
	)

	return nil
}

// migrateLegacyThumbs renames _thumb.jpg files to _md.jpg.
// Returns the number of files migrated.
func (l *Library) migrateLegacyThumbs(
	coverDir string,
	existing map[string]struct{},
) int {
	var migrated int

	for name := range existing {
		if !strings.Contains(name, legacyThumbSuffix) {
			continue
		}

		// Derive the _md name from the legacy name.
		mdName := strings.Replace(
			name, legacyThumbSuffix, "_md", 1,
		)

		oldPath := filepath.Join(coverDir, name)
		newPath := filepath.Join(coverDir, mdName)

		// Only rename if _md doesn't already exist.
		if _, exists := existing[mdName]; exists {
			// Both exist; remove the legacy file.
			if err := os.Remove(oldPath); err != nil {
				l.logger.Warn(
					"could not remove legacy thumbnail",
					"file", name, "err", err,
				)
			}

			continue
		}

		if err := os.Rename(oldPath, newPath); err != nil {
			l.logger.Warn(
				"could not migrate legacy thumbnail",
				"from", name, "to", mdName, "err", err,
			)

			continue
		}

		// Update the existing set so subsequent lookups
		// see the new name.
		delete(existing, name)
		existing[mdName] = struct{}{}

		migrated++

		l.logger.Debug(
			"migrated legacy thumbnail",
			"from", name, "to", mdName,
		)
	}

	return migrated
}

// extensionFromMIME returns a file extension for common image MIME types.
func extensionFromMIME(mimeType string) string {
	switch mimeType {
	case "image/jpeg":
		return "jpg"
	case "image/png":
		return "png"
	case "image/gif":
		return "gif"
	case "image/webp":
		return "webp"
	case "image/bmp":
		return "bmp"
	default:
		return "jpg" // Default to jpg.
	}
}
