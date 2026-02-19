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

// IsRotationalDisk reports whether the block device backing the
// given path is a rotational (spinning) disk.  Detection uses the
// Linux sysfs interface at /sys/block/<dev>/queue/rotational.
// Returns false on any error (assumes SSD).
func IsRotationalDisk(path string) bool {
	dev, err := deviceForPath(path)
	if err != nil {
		return false
	}

	rotational, err := os.ReadFile(
		filepath.Join(
			"/sys/block", dev, "queue", "rotational",
		),
	)
	if err != nil {
		return false
	}

	return strings.TrimSpace(string(rotational)) == "1"
}

// deviceForPath resolves a filesystem path to its underlying block
// device name (e.g. "sda") by matching the device major:minor
// from stat(2) against /sys/block/ entries.
func deviceForPath(path string) (string, error) {
	var st syscall.Stat_t
	if err := syscall.Stat(path, &st); err != nil {
		return "", fmt.Errorf(
			"could not stat path: %w", err,
		)
	}

	// Extract major and minor device numbers.
	major := (st.Dev >> 8) & 0xff
	minor := st.Dev & 0xff

	// Scan /sys/block/ for a matching device.
	entries, err := os.ReadDir("/sys/block")
	if err != nil {
		return "", fmt.Errorf(
			"could not read /sys/block: %w", err,
		)
	}

	majorStr := strconv.FormatUint(major, 10)
	devStr := majorStr + ":" +
		strconv.FormatUint(minor, 10)

	for _, entry := range entries {
		devFile := filepath.Join(
			"/sys/block", entry.Name(), "dev",
		)

		data, err := os.ReadFile(devFile)
		if err != nil {
			continue
		}

		content := strings.TrimSpace(string(data))

		if content == devStr {
			return entry.Name(), nil
		}

		// The filesystem might be on a partition (e.g. sda1)
		// whose parent block device is sda.  Check if the
		// major number matches.
		parts := strings.SplitN(content, ":", 2)
		if len(parts) == 2 && parts[0] == majorStr {
			return entry.Name(), nil
		}
	}

	return "", fmt.Errorf(
		"%w for %s", errNoBlockDevice, devStr,
	)
}
