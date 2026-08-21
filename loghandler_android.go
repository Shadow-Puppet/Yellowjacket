//go:build android

package main

import (
	"log/slog"

	"yellowjacket/backend/androidlog"
)

// newLogHandler builds the handler slog writes through.
//
// On Android stdout is /dev/null, so devslog here writes the app's
// entire diagnosis into a hole -- see backend/androidlog. logcat is
// the platform's sink and this is what reaches it.
func newLogHandler(opts *slog.HandlerOptions) slog.Handler {
	return androidlog.New(opts)
}
