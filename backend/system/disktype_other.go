//go:build !linux

package system

// IsRotationalDisk reports whether the block device backing the
// given path is a rotational (spinning) disk.  On non-Linux
// platforms this always returns false (assumes SSD).
func IsRotationalDisk(_ string) bool {
	return false
}
