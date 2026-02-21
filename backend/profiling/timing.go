//go:build dev

package profiling

import (
	"log/slog"
	"time"
)

// TimeOp starts a timer and returns a function that, when called, logs the
// elapsed duration. Intended for use with defer:
//
//	defer profiling.TimeOp(logger, "database.Init")()
//
// The extra () is required — defer evaluates the outer call immediately
// (capturing the start time) and defers the returned closure.
func TimeOp(logger *slog.Logger, operation string) func() {
	start := time.Now()

	logger.Debug("operation started", "op", operation)

	return func() {
		logger.Info(
			"operation completed",
			"op", operation,
			"duration", time.Since(start),
		)
	}
}
