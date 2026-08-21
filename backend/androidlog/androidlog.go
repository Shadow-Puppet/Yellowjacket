// Package androidlog routes slog to logcat.
//
// **An Android app's fd 1 and 2 go to /dev/null**, so every line this
// app writes with slog is discarded on that platform -- including the
// one naming the error it is about to os.Exit on. #52 is what that
// cost: a process that vanished with no tombstone, no AndroidRuntime
// stack and nothing in `logcat -b crash`, at Priority/Critical for
// months, whose entire diagnosis was one slog.Error main.go was
// already writing.
//
// The platform's own sink is __android_log_write, which is a handful
// of cgo -- and cgo compiled by nothing `make lint` or `make test`
// runs, since the only toolchain that builds the android tag is a
// cross-compiler and the only thing that runs it is a phone. So the
// split here is the one backend/mediacontrols/androidpayload.go makes,
// pushed as far as it will go: **everything except the write itself is
// in this file, untagged**. The priority mapping, the formatting, the
// chunking and the handler's own attr and group bookkeeping are
// ordinary Go that `go test` exercises on any platform; android.go is
// fifteen lines that hand a string to liblog.
package androidlog

import (
	"bytes"
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync"
)

// Tag is what logcat labels these lines with.
//
// It is a constant of ours rather than the application id, because the
// debug build carries `applicationIdSuffix ".dev"` so that it can be
// installed beside the release app -- so a tag derived from the package
// name is a *different* tag on the one build that can be inspected, and
// the filter that is supposed to show these lines would hide them on
// exactly the build used to look for them.
const Tag = "yellowjacket"

// Android's priorities, from android/log.h. These are the values
// __android_log_write takes; android.go asserts at compile time that
// they still match the header, so a renumbered platform is a build
// failure here rather than a warning silently logged as an error.
const (
	PrioVerbose = 2
	PrioDebug   = 3
	PrioInfo    = 4
	PrioWarn    = 5
	PrioError   = 6
	PrioFatal   = 7
)

// maxPayload is how much of one line liblog will carry.
//
// The kernel logger's entry is 4068 bytes for the tag, the message and
// their two NULs together, and what does not fit is **dropped without
// comment** -- so a long line would be truncated in the middle of the
// thing worth reading. 3500 leaves room for the tag and for the "(N/M)"
// a continuation carries.
const maxPayload = 3500

// WriteFunc is the platform sink: one already-formatted line, at one
// priority, under one tag.
//
// It is a parameter rather than a package-level function so that the
// handler can be driven by a test on a machine with no liblog at all.
type WriteFunc func(prio int, tag, msg string)

// Priority maps a slog level onto an Android one.
//
// slog's levels are open -- a caller may define its own at any int --
// so this is a banding rather than a lookup: anything below Info is
// debug, anything at or above Error is error. A custom level between
// two of the standard ones lands in the band beneath it, which is what
// slog's own level naming does.
func Priority(level slog.Level) int {
	switch {
	case level < slog.LevelDebug:
		return PrioVerbose
	case level < slog.LevelInfo:
		return PrioDebug
	case level < slog.LevelWarn:
		return PrioInfo
	case level < slog.LevelError:
		return PrioWarn
	default:
		return PrioError
	}
}

// Handler formats records with slog's own TextHandler and hands each
// line to a WriteFunc.
//
// It delegates the formatting rather than doing it, because WithAttrs
// and WithGroup are the half of slog.Handler that is easy to get subtly
// wrong -- and a logger whose groups are wrong is a logger nobody reads.
// What it does own is what logcat needs and TextHandler does not know
// about: the priority, and the fact that a line has a maximum length.
type Handler struct {
	write WriteFunc

	// mu guards buf, which the delegate writes into. slog.Handler is
	// documented as safe for concurrent use.
	mu       *sync.Mutex
	buf      *bytes.Buffer
	delegate slog.Handler
}

// NewHandler builds a handler over an arbitrary sink.
//
// The time and the level are dropped from the formatted line: logcat
// stamps every entry with both, and repeating them costs a quarter of
// the width of a phone-sized terminal to say the same thing twice.
func NewHandler(opts *slog.HandlerOptions, write WriteFunc) *Handler {
	buf := &bytes.Buffer{}

	inner := &slog.HandlerOptions{}
	if opts != nil {
		*inner = *opts
	}

	user := inner.ReplaceAttr
	inner.ReplaceAttr = func(groups []string, a slog.Attr) slog.Attr {
		if len(groups) == 0 && isBuiltin(a) {
			return slog.Attr{}
		}

		if user != nil {
			return user(groups, a)
		}

		return a
	}

	return &Handler{
		write:    write,
		mu:       &sync.Mutex{},
		buf:      buf,
		delegate: slog.NewTextHandler(buf, inner),
	}
}

// isBuiltin reports whether an attr is slog's own time or level,
// rather than a caller's attribute that happens to share the name.
//
// ReplaceAttr cannot tell those apart by key. It is called with an
// empty group path for the built-ins *and* for every top-level
// attribute, so a key comparison alone silently eats a caller's own
// "level" or "time" -- which is not hypothetical: the probe that
// verified this package on the device logged one, and the attribute
// vanished. The kinds are what separate them, because slog builds the
// built-ins as slog.Any(LevelKey, r.Level) and slog.Time(TimeKey, ...)
// and an attribute value of type slog.Level is not something a caller
// passes by accident.
func isBuiltin(a slog.Attr) bool {
	switch a.Key {
	case slog.TimeKey:
		return a.Value.Kind() == slog.KindTime
	case slog.LevelKey:
		_, ok := a.Value.Any().(slog.Level)

		return ok
	default:
		return false
	}
}

// Enabled reports whether the level is worth formatting.
func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.delegate.Enabled(ctx, level)
}

// Handle formats one record and writes it out, in as many entries as
// its length demands.
func (h *Handler) Handle(ctx context.Context, rec slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.buf.Reset()

	if err := h.delegate.Handle(ctx, rec); err != nil {
		return err
	}

	prio := Priority(rec.Level)
	for _, line := range Chunk(strings.TrimRight(h.buf.String(), "\n")) {
		h.write(prio, Tag, line)
	}

	return nil
}

// WithAttrs returns a handler carrying the given attributes.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h.derive(h.delegate.WithAttrs(attrs))
}

// WithGroup returns a handler that qualifies subsequent attributes.
func (h *Handler) WithGroup(name string) slog.Handler {
	return h.derive(h.delegate.WithGroup(name))
}

// derive shares the buffer and its mutex with the parent.
//
// They must be shared rather than copied: the delegate returned by
// WithAttrs writes into the *same* buffer this one does, so a second
// mutex would guard nothing and two loggers derived from one would
// interleave their bytes into a single line.
func (h *Handler) derive(delegate slog.Handler) *Handler {
	return &Handler{
		write:    h.write,
		mu:       h.mu,
		buf:      h.buf,
		delegate: delegate,
	}
}

// Chunk splits a formatted record into entries liblog will carry
// whole.
//
// A record short enough to fit is returned as it is, which is nearly
// every record; the numbering only appears where something was going
// to be silently truncated anyway. It splits on bytes rather than runes
// because the limit is a byte count -- a multi-byte rune straddling the
// boundary is a mojibake character in a log line, against a lost one.
func Chunk(msg string) []string {
	if len(msg) <= maxPayload {
		return []string{msg}
	}

	var parts []string

	for rest := msg; rest != ""; {
		n := min(maxPayload, len(rest))
		parts = append(parts, rest[:n])
		rest = rest[n:]
	}

	numbered := make([]string, 0, len(parts))
	for i, p := range parts {
		numbered = append(
			numbered,
			"("+strconv.Itoa(i+1)+"/"+strconv.Itoa(len(parts))+") "+p,
		)
	}

	return numbered
}
