package coverart

import (
	"fmt"
	"net/http"
	"path/filepath"
)

// Handler serves cover art images via HTTP.
type Handler struct {
	coversDir string
}

// NewHandler creates an HTTP handler that serves cover art from the
// user data directory.
func NewHandler() (*Handler, error) {
	dir, err := CoversDir()
	if err != nil {
		return nil, fmt.Errorf(
			"could not resolve covers directory: %w", err,
		)
	}

	return &Handler{coversDir: dir}, nil
}

// ServeHTTP handles requests for cover art images.
func (h *Handler) ServeHTTP(
	w http.ResponseWriter,
	r *http.Request,
) {
	// Extract filename from path like "/covers/abc123.jpg".
	filename := filepath.Base(r.URL.Path)

	// Prevent directory traversal.
	if filename == "." || filename == ".." {
		http.NotFound(w, r)

		return
	}

	// Filenames are content-hashed (SHA-256), so they are immutable.
	// Set aggressive cache headers to avoid redundant re-fetches.
	w.Header().Set(
		"Cache-Control",
		"public, max-age=31536000, immutable",
	)

	filePath := filepath.Join(h.coversDir, filename)
	http.ServeFile(w, r, filePath)
}
