package events

import (
	"context"
	"errors"
	"log/slog"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ErrNoRuntime is returned by Deliver when the context carries neither
// a test Sink nor a live Wails runtime, so the event went nowhere.
var ErrNoRuntime = errors.New(
	"no Wails runtime or event sink in context",
)

// Sink receives events in place of the Wails runtime.
//
// Installing one with WithSink is what makes a service that emits
// events testable in-process: see Deliver for why the real runtime
// cannot be used there.
type Sink interface {
	Emit(name string, data ...any)
}

// sinkKey is the private context key an installed Sink is stored under.
type sinkKey struct{}

// WithSink returns a context whose events are recorded by sink rather
// than pushed to the frontend.
//
// The sink travels in the context rather than in a package-level
// variable so that parallel tests cannot observe each other's events
// and so that production emits pay no synchronisation cost.
func WithSink(ctx context.Context, sink Sink) context.Context {
	return context.WithValue(ctx, sinkKey{}, sink)
}

// sinkFrom returns the Sink installed in ctx, or nil.
func sinkFrom(ctx context.Context) Sink {
	sink, _ := ctx.Value(sinkKey{}).(Sink)

	return sink
}

// Emit publishes a Wails event, tolerating any context.
//
// This is the only supported way to emit an event: nothing outside this
// package may call runtime.EventsEmit, which TestNoDirectEventsEmit
// enforces.
//
// runtime.EventsEmit calls log.Fatalf when the context is nil or lacks
// the runtime's "events" value, terminating the process rather than
// returning an error.  Background workers that outlive a context, and
// any test that constructs a service directly, both hit that path — so
// an event with nowhere to go is dropped and logged here instead.
func Emit(ctx context.Context, name string, data ...any) {
	if err := Deliver(ctx, name, data...); err != nil {
		slog.Default().Debug(
			"dropping event, no Wails runtime in context",
			"event", name,
		)
	}
}

// Deliver is Emit for the one caller that must know whether delivery
// happened: the dev control surface (backend/testctl), whose whole
// purpose is to impersonate a backend emit and which would otherwise
// report success for an event that went nowhere.
//
// Ordinary emitters want Emit.
func Deliver(ctx context.Context, name string, data ...any) error {
	if ctx == nil {
		return ErrNoRuntime
	}

	if sink := sinkFrom(ctx); sink != nil {
		sink.Emit(name, data...)

		return nil
	}

	if ctx.Value("events") == nil {
		return ErrNoRuntime
	}

	runtime.EventsEmit(ctx, name, data...)

	return nil
}
