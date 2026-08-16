package events

import (
	"context"
	"errors"
	"log/slog"
)

// ErrNoRuntime is returned by Deliver when there is neither a test Sink
// in the context nor a running application, so the event went nowhere.
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
// package may call app.Event.Emit, which TestNoDirectRuntimeEmits
// enforces.  That rule outlived its original reason — v2's
// runtime.EventsEmit called log.Fatalf on a context that did not carry
// the runtime, taking the process down from any background worker —
// and is kept because one emit path is what keeps emitStatus-style
// deduplication honest.
//
// The context is no longer a delivery mechanism: v3's emit takes none.
// It stays because WithSink travels in it, which is what makes a
// service that emits events testable in-process.
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
	// A nil context cannot carry a sink, but it is no longer a reason
	// not to deliver: v3 emits through the application, not the context.
	if ctx != nil {
		if sink := sinkFrom(ctx); sink != nil {
			sink.Emit(name, data...)

			return nil
		}
	}

	// emitRuntime is the only place the Wails application is touched,
	// and it is behind a build tag: see runtime_wails.go.
	return emitRuntime(name, data...)
}
