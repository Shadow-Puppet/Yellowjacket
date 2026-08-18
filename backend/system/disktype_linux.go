//go:build linux

package system

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

var errNoBlockDevice = errors.New(
	"no matching block device found",
)

// DiskProfile is what the scanner needs to know about the device a
// library sits on.  Both fields are about the same question — how many
// reads should be in flight at once — and they answer different halves
// of it, so they travel together rather than as two probes.
type DiskProfile struct {
	// Device is the whole-disk kernel name ("sdb"), or "" when the
	// path could not be resolved to one.
	Device string

	// Rotational is /sys/block/<dev>/queue/rotational: true for a
	// spinning disk, where a seek costs milliseconds.
	Rotational bool

	// QueueDepth is /sys/block/<dev>/device/queue_depth — how many
	// commands the drive will accept and reorder at once.  This is
	// NCQ: a SATA disk with it enabled reports 31 or 32, and one
	// without reports 1.  Zero means the file was not there to read,
	// which is the case for anything that is not a SCSI/SATA device
	// (NVMe, MMC, device-mapper, loop, a VM's virtio disk).
	//
	// It is the difference between concurrency helping and hurting.
	// With queueing, several outstanding reads let the drive service
	// them in the order its head passes over them, which is most of
	// why a parallel scan is faster at all.  Without it, every extra
	// worker is one more seek competing for one head, and the scan
	// gets slower the harder it is pushed.
	QueueDepth int
}

// Queues reports whether the drive can reorder outstanding commands.
//
// An unknown depth (0) counts as queueing: everything that does not
// publish this file is a device where concurrency is fine — NVMe has
// its own queues, virtio and device-mapper are not the physical layer
// at all.  The only case worth being careful about is the one that
// says so explicitly.
func (p DiskProfile) Queues() bool {
	return p.QueueDepth != 1
}

// IsRotationalDisk reports whether the block device backing the
// given path is a rotational (spinning) disk.  Returns false on any
// error (assumes SSD).
func IsRotationalDisk(path string) bool {
	return ProfileForPath(path).Rotational
}

// ProfileForPath describes the device backing a filesystem path.  A
// path that cannot be resolved yields the zero profile, which reads as
// "not rotational, queueing" — the permissive answer, since assuming a
// spinning disk on an SSD would halve a scan for nothing.
func ProfileForPath(path string) DiskProfile {
	dev, err := diskForPath(path)
	if err != nil {
		return DiskProfile{}
	}

	return DiskProfile{
		Device:     dev,
		Rotational: sysfsInt(dev, "queue", "rotational") == 1,
		QueueDepth: sysfsInt(dev, "device", "queue_depth"),
	}
}

// sysfsInt reads one small integer out of /sys/block/<dev>/<parts...>,
// returning 0 when it is absent or unparseable.  Every attribute here
// is optional: sysfs layout varies by driver, and a missing file is
// "this device does not say", never an error worth propagating.
func sysfsInt(dev string, parts ...string) int {
	p := filepath.Join(
		append([]string{"/sys/block", dev}, parts...)...,
	)

	data, err := os.ReadFile(p) //nolint:gosec // sysfs, name from the kernel
	if err != nil {
		return 0
	}

	n, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}

	return n
}

// diskForPath resolves a filesystem path to the *whole disk* backing
// it — "sdb" for a file on "sdb3".
//
// It goes through /sys/dev/block/<major>:<minor>, which the kernel
// maintains as a symlink to the device's own sysfs directory, and then
// walks up to the parent when that directory turns out to be a
// partition.  The previous implementation scanned /sys/block comparing
// dev numbers and, failing an exact match, took the first entry whose
// *major* agreed — and every SATA disk shares major 8.  So a library on
// /dev/sdb3 resolved to whatever /sys/block listed first, which is
// alphabetical, which is sda.  On the machine this was found on that
// meant a 6 TB spinning disk was read as the SSD next to it and scanned
// with one worker per core.  Matching on major alone cannot be right
// whenever a machine has two disks, which is the case this exists for.
func diskForPath(path string) (string, error) {
	var st syscall.Stat_t
	if err := syscall.Stat(path, &st); err != nil {
		return "", fmt.Errorf(
			"could not stat path: %w", err,
		)
	}

	// Linux packs dev_t as 12 bits of major and 20 of minor, split
	// across the word.  Masking the low byte of each — which is what
	// this used to do — is right only for the first 256 of either.
	major := unixMajor(uint64(st.Dev))
	minor := unixMinor(uint64(st.Dev))

	link := filepath.Join(
		"/sys/dev/block",
		strconv.FormatUint(major, 10)+":"+
			strconv.FormatUint(minor, 10),
	)

	target, err := filepath.EvalSymlinks(link)
	if err != nil {
		return "", fmt.Errorf(
			"%w: %s (%w)", errNoBlockDevice, link, err,
		)
	}

	// A partition's directory sits inside its disk's, and only the
	// disk carries `queue`. Climb at most one level: sysfs nests a
	// partition exactly one deep under its disk.
	name := filepath.Base(target)

	if _, err := os.Stat(filepath.Join(target, "queue")); err != nil {
		name = filepath.Base(filepath.Dir(target))
	}

	if name == "" || name == "." || name == string(filepath.Separator) {
		return "", fmt.Errorf(
			"%w for %d:%d", errNoBlockDevice, major, minor,
		)
	}

	return name, nil
}

// unixMajor and unixMinor decode a Linux dev_t.  Spelled out rather
// than taken from golang.org/x/sys/unix so this file stays readable
// beside the encoding it is undoing.
func unixMajor(dev uint64) uint64 {
	return (dev>>8)&0xfff | (dev >> 32 & ^uint64(0xfff))
}

func unixMinor(dev uint64) uint64 {
	return dev&0xff | (dev >> 12 & ^uint64(0xff))
}
