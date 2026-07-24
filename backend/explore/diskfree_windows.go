//go:build windows

package explore

import "golang.org/x/sys/windows"

// diskFreeBytes returns the free bytes available to the current user
// on the volume containing path.  ok is false when unknown.
func diskFreeBytes(path string) (free uint64, ok bool) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, false
	}

	var freeToCaller, total, totalFree uint64

	if err := windows.GetDiskFreeSpaceEx(p, &freeToCaller, &total, &totalFree); err != nil {
		return 0, false
	}

	return freeToCaller, true
}
