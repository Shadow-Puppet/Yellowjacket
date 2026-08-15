package backend

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// startupService is the cross-service wiring, wearing a service's
// clothes so the runtime starts it like everything else.
//
// The wiring belongs to no single service — it is the hooks, adapters
// and callbacks that make one package drive another — so v2 put it in
// OnStartup and the first v3 port hung it off
// events.Common.ApplicationStarted, which fires after every service's
// own ServiceStartup and is therefore the right *moment*.
//
// It is the wrong *mechanism*, because server mode emits no
// application events at all: v3's setupCommonEvents is an explicit
// no-op under `-tags server` ("server mode has no platform-specific
// events to map"). So the desktop build wired itself and the headless
// build did not, which showed up as "No player set, cannot load track"
// — the queue had no TrackLoader, so a track played from the UI
// changed the queue and then silently did nothing.
//
// Registering last is what preserves the ordering the wiring depends
// on: services start in registration order, on the main goroutine,
// before the platform run loop (application.Run's startup closure), so
// every service this touches has taken its context by the time this
// runs. A service is also the honest description of what this is —
// something with a lifecycle the app owns — and it costs no bindings,
// since ServiceStartup and ServiceShutdown are excluded from them.
type startupService struct {
	app *YellowJacketApp
}

// ServiceStartup runs the app-level wiring.
//
// OnDomReady no longer means the DOM is ready — nothing in v3 offers
// that — and it does not need to: what it does is start the soft
// rescan and report a startup failure, neither of which wants a
// frontend. The frontend drives its own state synchronisation by
// calling EmitCurrentState once its stores are listening, which is
// what makes the rename harmless.
func (s *startupService) ServiceStartup(
	ctx context.Context,
	_ application.ServiceOptions,
) error {
	s.app.OnStartup(ctx)
	s.app.OnDomReady(ctx)

	return nil
}
