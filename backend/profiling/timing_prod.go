//go:build !dev

package profiling

import "log/slog"

func noop() {}

// TimeOp is a no-op in production builds. The compiler will inline
// and eliminate this entirely.
func TimeOp(_ *slog.Logger, _ string) func() {
	return noop
}
