//go:build unix

package explore

import "golang.org/x/sys/unix"

// diskFreeBytes returns the free bytes available to the current user
// on the filesystem containing path.  ok is false when unknown.
func diskFreeBytes(path string) (free uint64, ok bool) {
	var st unix.Statfs_t

	if err := unix.Statfs(path, &st); err != nil {
		return 0, false
	}

	// Bavail/Bsize integer types vary by platform.
	//nolint:unconvert,gosec
	return st.Bavail * uint64(st.Bsize), true
}
