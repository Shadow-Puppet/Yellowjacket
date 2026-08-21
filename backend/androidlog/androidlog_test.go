package androidlog_test

import (
	"log/slog"
	"strings"
	"sync"
	"testing"

	"yellowjacket/backend/androidlog"
)

// entry is one call to the sink.
type entry struct {
	prio int
	tag  string
	msg  string
}

// recorder is the platform write, on a machine with no platform.
type recorder struct {
	mu      sync.Mutex
	entries []entry
}

func (r *recorder) write(prio int, tag, msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.entries = append(r.entries, entry{prio: prio, tag: tag, msg: msg})
}

func (r *recorder) only(t *testing.T) entry {
	t.Helper()

	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.entries) != 1 {
		t.Fatalf("want exactly one entry, got %d: %v", len(r.entries), r.entries)
	}

	return r.entries[0]
}

func newLogger(r *recorder, level slog.Level) *slog.Logger {
	return slog.New(androidlog.NewHandler(
		&slog.HandlerOptions{Level: level},
		r.write,
	))
}

// TestPriorityMapsEveryLevel pins the level banding.
//
// This is the one thing in #160 that a wrong answer hides rather than
// breaks: logcat prints whatever priority it is handed, so an Error
// filed as Info is a line that is present, correct and invisible to
// every filter anyone would use to look for it.
func TestPriorityMapsEveryLevel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		level slog.Level
		want  int
	}{
		{"below debug is verbose", slog.LevelDebug - 1, androidlog.PrioVerbose},
		{"debug", slog.LevelDebug, androidlog.PrioDebug},
		{"info", slog.LevelInfo, androidlog.PrioInfo},
		{"warn", slog.LevelWarn, androidlog.PrioWarn},
		{"error", slog.LevelError, androidlog.PrioError},

		// slog's levels are open, so a caller may sit between two of
		// the named ones.  Each lands in the band beneath it, which is
		// what slog's own level naming does ("INFO+2").
		{"between info and warn", slog.LevelInfo + 2, androidlog.PrioInfo},
		{"between warn and error", slog.LevelWarn + 1, androidlog.PrioWarn},
		{"above error", slog.LevelError + 4, androidlog.PrioError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := androidlog.Priority(tt.level); got != tt.want {
				t.Errorf("Priority(%v) = %d, want %d", tt.level, got, tt.want)
			}
		})
	}
}

// TestPrioritiesAreTheHeadersValues pins the constants themselves.
//
// android.go asserts these against android/log.h at compile time, but
// only a cross-compiler ever builds that file.  This is the assertion
// that runs in CI, and the numbers are written out longhand on purpose
// -- comparing a constant to itself would pass on any renumbering.
func TestPrioritiesAreTheHeadersValues(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		got  int
		want int
	}{
		{"verbose", androidlog.PrioVerbose, 2},
		{"debug", androidlog.PrioDebug, 3},
		{"info", androidlog.PrioInfo, 4},
		{"warn", androidlog.PrioWarn, 5},
		{"error", androidlog.PrioError, 6},
		{"fatal", androidlog.PrioFatal, 7},
	} {
		if tt.got != tt.want {
			t.Errorf("%s priority = %d, want %d", tt.name, tt.got, tt.want)
		}
	}
}

// TestRecordReachesTheSink is the whole point of the package: a line
// written with slog arrives, under the app's tag, at the right
// priority.
func TestRecordReachesTheSink(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	newLogger(rec, slog.LevelInfo).Error("application error", "err", "boom")

	got := rec.only(t)

	if got.prio != androidlog.PrioError {
		t.Errorf("priority = %d, want %d", got.prio, androidlog.PrioError)
	}

	if got.tag != androidlog.Tag {
		t.Errorf("tag = %q, want %q", got.tag, androidlog.Tag)
	}

	if !strings.Contains(got.msg, "application error") {
		t.Errorf("message %q does not carry the message", got.msg)
	}

	if !strings.Contains(got.msg, `err=boom`) {
		t.Errorf("message %q does not carry the attribute", got.msg)
	}
}

// TestTheTagIsNotTheApplicationID guards the trap the tag exists to
// avoid.
//
// The debug build carries `applicationIdSuffix ".dev"`, so it is
// installed as app.yellowjacket.dev -- and it is the *only* build whose
// WebView can be inspected, so it is the build anyone debugging this
// app is running.  A tag derived from the application id therefore
// differs between the build being looked at and the build the filter
// was written for, which is the failure this whole issue is about
// wearing a different hat.
func TestTheTagIsNotTheApplicationID(t *testing.T) {
	t.Parallel()

	if strings.Contains(androidlog.Tag, ".") {
		t.Errorf(
			"tag %q looks like an application id; it must be stable "+
				"across the debug suffix",
			androidlog.Tag,
		)
	}

	// Logcat's tag field is 23 bytes.  A longer one is truncated, and a
	// truncated tag matches no filter.
	if len(androidlog.Tag) > 23 {
		t.Errorf("tag %q is %d bytes, over logcat's 23", androidlog.Tag, len(androidlog.Tag))
	}
}

// TestTimeAndLevelAreDropped checks the formatting decision.
//
// logcat stamps every entry with a timestamp and a priority letter, so
// carrying slog's own is the same information twice on a 424px screen.
func TestTimeAndLevelAreDropped(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	newLogger(rec, slog.LevelInfo).Warn("scan finished", "files", 1577)

	got := rec.only(t).msg

	if strings.Contains(got, "time=") {
		t.Errorf("message %q still carries a timestamp", got)
	}

	if strings.Contains(got, "level=") {
		t.Errorf("message %q still carries a level", got)
	}

	if !strings.Contains(got, "files=1577") {
		t.Errorf("message %q lost its attributes with them", got)
	}
}

// TestACallersOwnLevelAttrSurvives is a regression, and it was found on
// the phone rather than here.
//
// Dropping slog's built-in time and level by key alone also drops a
// caller's attribute of the same name, because ReplaceAttr sees an
// empty group path for both. The probe that verified this package on
// the device wrote slog.Info("...", "level", "info") and logcat showed
// the message with no attributes at all.
func TestACallersOwnLevelAttrSurvives(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	newLogger(rec, slog.LevelInfo).Info("probe", "level", "info", "time", "soon")

	got := rec.only(t).msg

	for _, want := range []string{"level=info", "time=soon"} {
		if !strings.Contains(got, want) {
			t.Errorf("message %q lost the caller's %q", got, want)
		}
	}

	// And slog's own are still gone: the built-in level renders as a
	// bare word like INFO, never as the caller's value.
	if strings.Contains(got, "level=INFO") {
		t.Errorf("message %q carries slog's own level", got)
	}
}

// TestLevelIsHonoured checks that Enabled reaches the delegate.
func TestLevelIsHonoured(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	log := newLogger(rec, slog.LevelWarn)

	log.Info("not this one")
	log.Warn("this one")

	if got := rec.only(t).msg; !strings.Contains(got, "this one") {
		t.Errorf("wrong record survived: %q", got)
	}
}

// TestGroupsAndAttrsSurvive covers the half of slog.Handler this
// delegates rather than implements -- the reason it delegates at all.
func TestGroupsAndAttrsSurvive(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	log := newLogger(rec, slog.LevelInfo).
		With("component", "player").
		WithGroup("track")

	log.Info("loaded", "path", "/sdcard/Music/a.flac")

	got := rec.only(t).msg

	for _, want := range []string{
		"component=player",
		"track.path=/sdcard/Music/a.flac",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("message %q is missing %q", got, want)
		}
	}
}

// TestDerivedHandlersDoNotInterleave is why derive shares the buffer's
// mutex rather than taking a new one.
//
// Two loggers derived from one write into the same buffer, so a second
// mutex would guard nothing and a concurrent pair would splice each
// other's bytes into a single line -- which reads as corrupted logs
// under load and as nothing at all in a test that logs once.
func TestDerivedHandlersDoNotInterleave(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	base := newLogger(rec, slog.LevelInfo)

	var wg sync.WaitGroup

	for i := range 8 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			log := base.With("worker", i).WithGroup("g")
			for range 50 {
				log.Info("tick", "n", i)
			}
		}()
	}

	wg.Wait()

	rec.mu.Lock()
	defer rec.mu.Unlock()

	if len(rec.entries) != 8*50 {
		t.Fatalf("got %d entries, want %d", len(rec.entries), 8*50)
	}

	for _, e := range rec.entries {
		if strings.Count(e.msg, "msg=tick") != 1 {
			t.Fatalf("interleaved line: %q", e.msg)
		}
	}
}

// TestChunkLeavesShortLinesAlone is the common case: no numbering
// appears on a record that was never going to be truncated.
func TestChunkLeavesShortLinesAlone(t *testing.T) {
	t.Parallel()

	got := androidlog.Chunk("msg=short")

	if len(got) != 1 || got[0] != "msg=short" {
		t.Errorf("Chunk(short) = %q, want the input unchanged", got)
	}
}

// TestChunkSplitsWhatWouldBeTruncated covers the case liblog drops
// silently.
func TestChunkSplitsWhatWouldBeTruncated(t *testing.T) {
	t.Parallel()

	const n = 9000

	long := strings.Repeat("x", n)
	parts := androidlog.Chunk(long)

	if len(parts) < 2 {
		t.Fatalf("a %d-byte line was not split", n)
	}

	var payload strings.Builder

	for i, p := range parts {
		if len(p) > 4000 {
			t.Errorf("part %d is %d bytes, over liblog's entry", i, len(p))
		}

		_, rest, found := strings.Cut(p, ") ")
		if !found {
			t.Fatalf("part %d carries no (n/m) marker: %q", i, p)
		}

		payload.WriteString(rest)
	}

	if payload.String() != long {
		t.Errorf("the parts do not reassemble into the input")
	}
}
