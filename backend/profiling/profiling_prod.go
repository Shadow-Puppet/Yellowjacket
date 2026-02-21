//go:build !dev

package profiling

import "log/slog"

// Start is a no-op in production builds. The pprof and runtime/trace
// imports are excluded entirely, adding zero overhead to the binary.
func Start(_ *slog.Logger) func() {
	return func() {}
}
