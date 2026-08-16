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

// URLs holds the resolved URL paths for a cover's size variants.
//
// Original is the largest variant kept, which is the Large one: the
// full-resolution image is no longer stored.  It was 1,134 MB of a
// 1.4 GB covers directory on a real 2,057-album library against 110 MB
// for all three rendered tiers, and nothing rendered it - the grid caps
// at 350 px and the largest tier is 400.  The field keeps its name
// because it is what a caller means by "the cover", and the bytes it
// came from are still in the audio file if a bigger one is ever wanted.
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

// Suffixes are the size variants a cover is stored as, largest last.
var Suffixes = []string{"_sm", "_md", "_lg"}

// SizedFilename derives a sized-variant filename from a cover art
// filename and a size suffix.  The input may itself be a variant, so
// its suffix is stripped first: SizedFilename("a1b2_lg.jpg", "_sm")
// and SizedFilename("a1b2.jpg", "_sm") both return "a1b2_sm.jpg".
func SizedFilename(filename, suffix string) string {
	return BaseName(filename) + suffix + ".jpg"
}

// BaseName strips the extension and any size suffix from a cover art
// filename, leaving the content hash that identifies the cover.
func BaseName(filename string) string {
	name := strings.TrimSuffix(filename, filepath.Ext(filename))

	for _, suffix := range Suffixes {
		if strings.HasSuffix(name, suffix) {
			return strings.TrimSuffix(name, suffix)
		}
	}

	return name
}

// ResolveURLs converts a cover art filesystem path into URL paths
// for the original and all size variants (small, medium, large).
func ResolveURLs(filesystemPath string) URLs {
	base := filepath.Base(filesystemPath)
	large := PathPrefix + SizedFilename(base, "_lg")

	return URLs{
		Original: large,
		Small:    PathPrefix + SizedFilename(base, "_sm"),
		Medium:   PathPrefix + SizedFilename(base, "_md"),
		Large:    large,
	}
}
