//go:build linux

package library

import (
	"os"

	"golang.org/x/sys/unix"
)

// hintReadahead asks the kernel to start fetching the head of a file
// that is about to be read.
//
// `POSIX_FADV_WILLNEED` returns immediately and queues the read, which
// is the whole point: on a spinning disk the first access to a file
// costs a seek of several milliseconds, and that latency can only be
// hidden by having the next seek already in flight while the current
// file is being parsed.  A drive with command queueing can then service
// the queued reads in head order rather than in the order they were
// asked for.
//
// Errors are dropped on purpose.  This is a hint: a file that has since
// been deleted, a filesystem that does not implement fadvise, or a
// permission the walk saw and this open does not, all mean "no
// prefetch", never "fail the scan".  The read that follows is what
// reports a genuine problem.
func hintReadahead(path string, bytes int64) {
	f, err := os.Open(path)
	if err != nil {
		return
	}

	defer func() { _ = f.Close() }()

	_ = unix.Fadvise(
		int(f.Fd()), 0, bytes, unix.FADV_WILLNEED,
	)
}
