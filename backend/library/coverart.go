package library

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"yellowjacket/backend/metadata"
	"yellowjacket/backend/system"
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

	// Skip if already exists (same content hash)
	if _, err := os.Stat(filePath); err == nil {
		l.logger.Debug("cover art already exists", "path", filePath)

		return filePath, nil
	}

	// Write file
	if err := os.WriteFile(filePath, pic.Data, 0o644); err != nil {
		return "", fmt.Errorf("could not write cover art: %w", err)
	}

	l.logger.Debug("saved cover art", "path", filePath, "size", len(pic.Data))

	return filePath, nil
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
