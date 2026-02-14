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

	"golang.org/x/image/draw"

	"yellowjacket/backend/metadata"
	"yellowjacket/backend/system"
)

const (
	// thumbnailMaxSize is the maximum width/height for generated thumbnails.
	thumbnailMaxSize = 256
	// thumbnailQuality is the JPEG encoding quality for thumbnails.
	thumbnailQuality = 80
	// thumbnailSuffix is appended to the content hash for thumbnail filenames.
	thumbnailSuffix = "_thumb"
)

// saveCoverArt saves embedded cover art to the cache directory.
// Returns the file path where the art was saved, or empty string if no picture data.
func (l *Library) saveCoverArt(pic *metadata.PictureData) (string, error) {
	if pic == nil || len(pic.Data) == 0 {
		return "", nil
	}

	// Get the data directory for storing cover art
	dataDir, err := system.GetUserDataDirPath()
	if err != nil {
		return "", fmt.Errorf("could not get user data directory: %w", err)
	}

	coverDir := filepath.Join(dataDir, "covers")

	// Ensure directory exists
	if err := os.MkdirAll(coverDir, 0o755); err != nil {
		return "", fmt.Errorf("could not create covers directory: %w", err)
	}

	// Generate filename from content hash (deduplication)
	hash := sha256.Sum256(pic.Data)
	hashStr := hex.EncodeToString(hash[:8]) // First 8 bytes = 16 hex chars

	ext := pic.Ext
	if ext == "" {
		// Determine extension from MIME type
		ext = extensionFromMIME(pic.MIMEType)
	}

	filename := fmt.Sprintf("%s.%s", hashStr, ext)
	filePath := filepath.Join(coverDir, filename)

	// Skip if already exists (same content hash).
	// Missing thumbnails are handled by generateMissingThumbnails() at the end of a scan.
	if _, err := os.Stat(filePath); err == nil {
		l.logger.Debug("cover art already exists", "path", filePath)

		return filePath, nil
	}

	// Write file
	if err := os.WriteFile(filePath, pic.Data, 0o644); err != nil {
		return "", fmt.Errorf("could not write cover art: %w", err)
	}

	l.logger.Debug("saved cover art", "path", filePath, "size", len(pic.Data))

	// Generate thumbnail alongside the original
	if err := l.generateThumbnail(pic.Data, coverDir, hashStr); err != nil {
		l.logger.Warn("could not generate thumbnail", "path", filePath, "err", err)
	}

	return filePath, nil
}

// generateThumbnail creates a downscaled JPEG thumbnail from cover art image data.
// The thumbnail is saved as {hashStr}_thumb.jpg in the given directory.
func (l *Library) generateThumbnail(imgData []byte, dir, hashStr string) error {
	thumbFilename := fmt.Sprintf("%s%s.jpg", hashStr, thumbnailSuffix)
	thumbPath := filepath.Join(dir, thumbFilename)

	// Decode the source image
	src, _, err := image.Decode(bytes.NewReader(imgData))
	if err != nil {
		return fmt.Errorf("could not decode image for thumbnail: %w", err)
	}

	// Calculate thumbnail dimensions preserving aspect ratio
	bounds := src.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()

	// Skip if image is already smaller than the thumbnail size
	if srcW <= thumbnailMaxSize && srcH <= thumbnailMaxSize {
		// Still save a JPEG copy for consistent serving
		return l.encodeAndSaveThumbnail(src, thumbPath, srcW, srcH)
	}

	// Scale down preserving aspect ratio
	thumbW, thumbH := thumbnailMaxSize, thumbnailMaxSize
	if srcW > srcH {
		thumbH = srcH * thumbnailMaxSize / srcW
	} else {
		thumbW = srcW * thumbnailMaxSize / srcH
	}

	return l.encodeAndSaveThumbnail(src, thumbPath, thumbW, thumbH)
}

// encodeAndSaveThumbnail scales the source image to the given dimensions and saves as JPEG.
func (l *Library) encodeAndSaveThumbnail(src image.Image, path string, w, h int) error {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)

	var buf bytes.Buffer

	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: thumbnailQuality}); err != nil {
		return fmt.Errorf("could not encode thumbnail: %w", err)
	}

	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("could not write thumbnail: %w", err)
	}

	l.logger.Debug(
		"saved thumbnail",
		"path",
		path,
		"size",
		buf.Len(),
		"dimensions",
		fmt.Sprintf("%dx%d", w, h),
	)

	return nil
}

// generateMissingThumbnails scans the covers directory and generates thumbnails
// for any original cover art files that do not yet have a corresponding _thumb.jpg.
func (l *Library) generateMissingThumbnails() error {
	dataDir, err := system.GetUserDataDirPath()
	if err != nil {
		return fmt.Errorf("could not get user data directory: %w", err)
	}

	coverDir := filepath.Join(dataDir, "covers")

	entries, err := os.ReadDir(coverDir)
	if err != nil {
		return fmt.Errorf("could not read covers directory: %w", err)
	}

	// Build a set of existing filenames for quick lookup.
	existing := make(map[string]struct{}, len(entries))

	for _, entry := range entries {
		if !entry.IsDir() {
			existing[entry.Name()] = struct{}{}
		}
	}

	var generated, skipped int

	for _, entry := range entries {
		name := entry.Name()

		// Skip directories and thumbnails themselves.
		if entry.IsDir() || strings.Contains(name, thumbnailSuffix) {
			continue
		}

		thumbName := ThumbnailFilename(name)
		if _, exists := existing[thumbName]; exists {
			skipped++

			continue
		}

		// Extract hash from filename (everything before the first dot).
		hashStr := strings.SplitN(name, ".", 2)[0]

		imgData, err := os.ReadFile(filepath.Join(coverDir, name))
		if err != nil {
			l.logger.Warn("could not read cover art for thumbnail generation", "file", name, "err", err)

			continue
		}

		if err := l.generateThumbnail(imgData, coverDir, hashStr); err != nil {
			l.logger.Warn("could not generate thumbnail", "file", name, "err", err)

			continue
		}

		generated++
	}

	l.logger.Info("thumbnail generation complete", "generated", generated, "skipped", skipped)

	return nil
}

// ThumbnailFilename derives the thumbnail filename from an original cover art filename.
// For example, "a1b2c3d4.jpg" becomes "a1b2c3d4_thumb.jpg".
func ThumbnailFilename(originalFilename string) string {
	ext := filepath.Ext(originalFilename)
	name := strings.TrimSuffix(originalFilename, ext)

	return name + thumbnailSuffix + ".jpg"
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
		return "jpg" // Default to jpg
	}
}
