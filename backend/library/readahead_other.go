//go:build !linux

package library

// hintReadahead is a no-op off Linux.
//
// macOS has `F_RDADVISE` and Windows has `FILE_FLAG_SEQUENTIAL_SCAN`,
// and neither is wired up here for the reason the scan concurrency
// heuristic is not either: this package cannot tell a spinning disk
// from an SSD on those platforms (see system.ProfileForPath), so it
// would be prefetching without knowing whether prefetching is what the
// device wants.
func hintReadahead(_ string, _ int64) {}
