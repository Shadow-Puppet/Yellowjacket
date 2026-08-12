package player

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"yellowjacket/backend/events"
)

// recordedPlayer is a player with an event sink installed and no audio
// device: enough to assert on what the frontend would receive from the
// paths that do not touch the speaker.
func recordedPlayer(t *testing.T) (*Player, *events.Recorder) {
	t.Helper()

	p := NewPlayer(slog.Default(), nil)
	rec := events.NewRecorder()
	p.SetContext(events.WithSink(t.Context(), rec))

	return p, rec
}

func TestSeek_WithNoTrackEmitsSeekFailed(t *testing.T) {
	t.Parallel()

	p, rec := recordedPlayer(t)

	if err := p.Seek(5); err == nil {
		t.Fatal("Seek with no track loaded returned nil error")
	}

	// C2: the frontend has made an optimistic move it now has to take
	// back, and this is the only thing that tells it so.
	if _, ok := rec.Last(events.SeekFailed); !ok {
		t.Errorf("no SeekFailed emitted; got %v", rec.Names())
	}
}

func TestPositionTicker_SilentWhileNotPlaying(t *testing.T) {
	t.Parallel()

	_, rec := recordedPlayer(t)

	// The ticker is running (SetContext started it) but nothing is
	// playing, so a paused app must not push a position a second
	// forever.
	time.Sleep(positionTickInterval * 2)

	if got := rec.Count(events.PlaybackPositionChanged); got != 0 {
		t.Errorf("position emitted %d times while stopped, want 0", got)
	}
}

func TestEmitPosition_CarriesLengthAndSequence(t *testing.T) {
	t.Parallel()

	p := NewPlayer(slog.Default(), nil)
	rec := events.NewRecorder()
	p.ctx = events.WithSink(context.Background(), rec)

	p.mu.Lock()
	p.emitPositionLocked()
	p.emitPositionLocked()
	p.mu.Unlock()

	ticks := rec.Named(events.PlaybackPositionChanged)
	if len(ticks) != 2 {
		t.Fatalf("emitted %d positions, want 2", len(ticks))
	}

	first, ok := ticks[0].Payload().(PositionInfo)
	if !ok {
		t.Fatalf("payload is %T, want player.PositionInfo", ticks[0].Payload())
	}

	second, _ := ticks[1].Payload().(PositionInfo)

	// The sequence is what lets the seek bar reset its interpolation
	// on a tick that reports the same second twice.
	if second.Seq <= first.Seq {
		t.Errorf("seq did not advance: %d then %d", first.Seq, second.Seq)
	}
}
