//go:build android

// The write itself, and nothing else. Everything decidable off a phone
// is in androidlog.go; see the package comment for why.

package androidlog

/*
#cgo LDFLAGS: -llog
#include <stdlib.h>
#include <android/log.h>
*/
import "C"

import (
	"log/slog"
	"unsafe"
)

// The priorities in androidlog.go are android/log.h's own values, and
// these are what says so. A constant expression that would be negative
// does not compile as a uint, so a renumbered header fails the build
// here rather than logging everything at the wrong severity -- which is
// the failure that would otherwise be invisible, since logcat would
// happily print whatever number it was handed.
const (
	_ = uint(C.ANDROID_LOG_VERBOSE - PrioVerbose)
	_ = uint(PrioVerbose - C.ANDROID_LOG_VERBOSE)
	_ = uint(C.ANDROID_LOG_DEBUG - PrioDebug)
	_ = uint(PrioDebug - C.ANDROID_LOG_DEBUG)
	_ = uint(C.ANDROID_LOG_INFO - PrioInfo)
	_ = uint(PrioInfo - C.ANDROID_LOG_INFO)
	_ = uint(C.ANDROID_LOG_WARN - PrioWarn)
	_ = uint(PrioWarn - C.ANDROID_LOG_WARN)
	_ = uint(C.ANDROID_LOG_ERROR - PrioError)
	_ = uint(PrioError - C.ANDROID_LOG_ERROR)
	_ = uint(C.ANDROID_LOG_FATAL - PrioFatal)
	_ = uint(PrioFatal - C.ANDROID_LOG_FATAL)
)

// New returns the handler main() installs on Android.
func New(opts *slog.HandlerOptions) slog.Handler {
	return NewHandler(opts, write)
}

// write hands one line to liblog.
func write(prio int, tag, msg string) {
	cTag := C.CString(tag)
	defer C.free(unsafe.Pointer(cTag))

	cMsg := C.CString(msg)
	defer C.free(unsafe.Pointer(cMsg))

	C.__android_log_write(C.int(prio), cTag, cMsg)
}
