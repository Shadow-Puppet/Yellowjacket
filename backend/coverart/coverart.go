// Package coverart provides utilities for cover art filenames and URL resolution.
package coverart

import (
	"fmt"
	"path/filepath"
	"strings"

	"yellowjacket/backend/system"
)

// PathPrefix is the URL path prefix for cover art served by the asset handler.
const PathPrefix = "/covers/"

// URLs holds the resolved URL paths for all cover art size variants.
type URLs struct {
	Original string
	Small    string
	Medium   string
	Large    string
}

// dirName is the subdirectory name under the user data directory
// where cover art files are stored.
const dirName = "covers"

// CoversDir returns the absolute path to the cover art cache directory.
func CoversDir() (string, error) {
	dataDir, err := system.GetUserDataDirPath()
	if err != nil {
		return "", fmt.Errorf(
			"could not get user data directory: %w", err,
		)
	}

	return filepath.Join(dataDir, dirName), nil
}

// SizedFilename derives a sized-variant filename from an original cover art
// filename and a size suffix.
// For example, SizedFilename("a1b2c3d4.jpg", "_sm") returns "a1b2c3d4_sm.jpg".
func SizedFilename(originalFilename, suffix string) string {
	ext := filepath.Ext(originalFilename)
	name := strings.TrimSuffix(originalFilename, ext)

	return name + suffix + ".jpg"
}

// ResolveURLs converts a cover art filesystem path into URL paths
// for the original and all size variants (small, medium, large).
func ResolveURLs(filesystemPath string) URLs {
	base := filepath.Base(filesystemPath)

	return URLs{
		Original: PathPrefix + base,
		Small:    PathPrefix + SizedFilename(base, "_sm"),
		Medium:   PathPrefix + SizedFilename(base, "_md"),
		Large:    PathPrefix + SizedFilename(base, "_lg"),
	}
}
