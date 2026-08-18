//go:build !linux

package system

// DiskProfile is what the scanner needs to know about the device a
// library sits on.  See the Linux implementation for what each field
// means; off Linux nothing fills them, because neither macOS nor
// Windows publishes an equivalent of sysfs's `rotational` and
// `queue_depth` without going through platform APIs this package
// deliberately does not link.
type DiskProfile struct {
	Device     string
	Rotational bool
	QueueDepth int
}

// Queues reports whether the drive can reorder outstanding commands.
// Always true here: an unknown depth is the permissive answer, and
// assuming otherwise would halve every scan on every Mac.
func (p DiskProfile) Queues() bool {
	return p.QueueDepth != 1
}

// IsRotationalDisk reports whether the block device backing the
// given path is a rotational (spinning) disk.  On non-Linux
// platforms this always returns false (assumes SSD).
func IsRotationalDisk(_ string) bool {
	return false
}

// ProfileForPath describes the device backing a filesystem path.  Off
// Linux that is the zero profile, which reads as "an SSD that queues".
func ProfileForPath(_ string) DiskProfile {
	return DiskProfile{}
}
