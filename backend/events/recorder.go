package events

import (
	"sync"
	"time"
)

// Event is one recorded emission.
type Event struct {
	Name string
	Data []any
}

// Payload returns the single data argument almost every event carries,
// or nil for the handful emitted with none.
func (e Event) Payload() any {
	if len(e.Data) == 0 {
		return nil
	}

	return e.Data[0]
}

// Recorder is a Sink that buffers events for later assertion.
//
// It is safe for concurrent use: several services emit from background
// goroutines, and Wait exists so a test can block on one of those
// rather than sleep.
type Recorder struct {
	mu     sync.Mutex
	events []Event

	// notify is closed and replaced on every emit, so waiters wake
	// without the Recorder having to track them individually.
	notify chan struct{}
}

// NewRecorder returns an empty Recorder.
func NewRecorder() *Recorder {
	return &Recorder{notify: make(chan struct{})}
}

// Emit implements Sink.
func (r *Recorder) Emit(name string, data ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.events = append(r.events, Event{Name: name, Data: data})

	close(r.notify)
	r.notify = make(chan struct{})
}

// Events returns every event recorded so far, in order.
func (r *Recorder) Events() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]Event(nil), r.events...)
}

// Named returns every recorded event with the given name, in order.
func (r *Recorder) Named(name string) []Event {
	r.mu.Lock()
	defer r.mu.Unlock()

	var out []Event

	for _, ev := range r.events {
		if ev.Name == name {
			out = append(out, ev)
		}
	}

	return out
}

// Names returns the name of every recorded event, in order.
//
// Assertions read better against this than against Events when what
// matters is which events fired and in what order.
func (r *Recorder) Names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]string, 0, len(r.events))
	for _, ev := range r.events {
		out = append(out, ev.Name)
	}

	return out
}

// Count returns how many times the named event was recorded.
func (r *Recorder) Count(name string) int {
	return len(r.Named(name))
}

// Last returns the most recent event with the given name.
func (r *Recorder) Last(name string) (Event, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i := len(r.events) - 1; i >= 0; i-- {
		if r.events[i].Name == name {
			return r.events[i], true
		}
	}

	return Event{}, false
}

// Reset discards everything recorded so far.
func (r *Recorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.events = nil
}

// Wait blocks until an event with the given name is recorded, and
// returns it.  It returns false if timeout elapses first.
//
// Events already recorded count, so a test cannot lose a race by
// calling Wait after the emit it is waiting for.
func (r *Recorder) Wait(name string, timeout time.Duration) (Event, bool) {
	deadline := time.After(timeout)
	from := 0

	for {
		r.mu.Lock()

		for i := from; i < len(r.events); i++ {
			if r.events[i].Name == name {
				ev := r.events[i]
				r.mu.Unlock()

				return ev, true
			}
		}

		from = len(r.events)
		notify := r.notify

		r.mu.Unlock()

		select {
		case <-notify:
		case <-deadline:
			return Event{}, false
		}
	}
}
