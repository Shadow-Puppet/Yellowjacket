package library

import (
	"fmt"
	"net/http"
	"path/filepath"

	"yellowjacket/backend/system"
)

// CoverArtHandler serves cover art images via HTTP.
type CoverArtHandler struct {
	coversDir string
}

// NewCoverArtHandler creates a handler that serves cover art from the user data directory.
func NewCoverArtHandler() (*CoverArtHandler, error) {
	dataDir, err := system.GetUserDataDirPath()
	if err != nil {
		return nil, fmt.Errorf("could not get user data directory: %w", err)
	}

	return &CoverArtHandler{
		coversDir: filepath.Join(dataDir, "covers"),
	}, nil
}

// ServeHTTP handles requests for cover art images.
func (h *CoverArtHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Extract filename from path like "/covers/abc123.jpg"
	filename := filepath.Base(r.URL.Path)

	// Prevent directory traversal
	if filename == "." || filename == ".." {
		http.NotFound(w, r)

		return
	}

	// Filenames are content-hashed (SHA-256), so they are immutable.
	// Set aggressive cache headers to avoid redundant re-fetches.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")

	filePath := filepath.Join(h.coversDir, filename)
	http.ServeFile(w, r, filePath)
}
