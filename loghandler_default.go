//go:build !android

package main

import (
	"log/slog"
	"os"

	"github.com/golang-cz/devslog"
)

// newLogHandler builds the handler slog writes through.
//
// Off Android that is devslog to stdout, as it has always been. The
// selection is a build tag rather than a runtime check so that a
// desktop binary links no cgo for a platform it will never run on --
// backend/androidlog's write is -llog, which does not exist here.
func newLogHandler(opts *slog.HandlerOptions) slog.Handler {
	return devslog.NewHandler(os.Stdout, &devslog.Options{HandlerOptions: opts})
}
