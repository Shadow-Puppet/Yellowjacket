// Package assets handles serving frontend static files.
package assets

import (
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// distRoot is where the frontend build lands inside the embedded FS.
// v2 knew this prefix itself; v3 takes an fs.FS rooted at the assets,
// so the sub-FS is taken here.
const distRoot = "frontend/dist"

// Handler serves frontend assets with custom route support.
type Handler struct {
	Options            application.AssetOptions
	logger             *slog.Logger
	frontendDistAssets embed.FS
	serveMux           *http.ServeMux
	wailsAssetHandler  http.Handler
}

// NewAssetHandler creates a new asset handler.
func NewAssetHandler(logger *slog.Logger, frontendDistAssets embed.FS) (*Handler, error) {
	handler := &Handler{
		logger:             logger,
		frontendDistAssets: frontendDistAssets,
		serveMux:           http.NewServeMux(),
	}

	dist, err := fs.Sub(frontendDistAssets, distRoot)
	if err != nil {
		return nil, fmt.Errorf(
			"could not open %s in the embedded assets: %w", distRoot, err,
		)
	}

	handler.Options = application.AssetOptions{
		Handler:    application.AssetFileServerFS(dist),
		Middleware: handler.Middleware,
	}

	return handler, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// if we dont have a custom handler defined, then use the wails asset handler
	if _, pattern := h.serveMux.Handler(r); len(pattern) == 0 {
		h.logger.Debug(
			"custom handler for request not found, using wails asset handler",
			"path",
			r.URL.Path,
		)
		h.wailsAssetHandler.ServeHTTP(w, r)

		return
	}

	h.logger.Debug(
		"using custom handler for request",
		"path",
		r.URL.Path,
	)
	h.serveMux.ServeHTTP(w, r)
}

// Middleware captures the Wails asset handler for fallback routing.
func (h *Handler) Middleware(next http.Handler) http.Handler {
	h.wailsAssetHandler = next

	return h
}

// RegisterHandler adds a custom handler for a URL pattern.
func (h *Handler) RegisterHandler(pattern string, handler http.Handler) {
	h.logger.Debug("registering asset handler", "pattern", pattern)
	h.serveMux.Handle(pattern, handler)
}
