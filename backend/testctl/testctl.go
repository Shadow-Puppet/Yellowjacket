// Package testctl mounts a dev-only HTTP control surface at /__test/
// on the app's own asset server.
//
// It exists for the residue of what an end-to-end harness genuinely
// cannot reach from the browser.  Everything the frontend can do is
// already reachable through the generated bindings on `window.go` —
// clicking, reading the DOM, calling a service — so this deliberately
// does *not* re-expose any of that.  What is left is server-side state:
// snapshotting and restoring the SQLite database mid-run, forcing a
// backend event so a push-driven view can be rendered without staging
// hours of real work, and reading a single authoritative "is the
// backend actually ready" answer.
//
// It is gated twice.  The implementation lives behind the `dev` build
// tag (the non-dev twin is an empty function, so nothing links into a
// release binary), and even in a dev build it refuses to register
// unless YJ_TESTCTL=1 — otherwise every `make dev` session a human runs
// would carry an arbitrary-SQL endpoint on a listening port.
package testctl

import (
	"context"
	"log/slog"
	"net/http"

	"yellowjacket/backend/database"
)

// EnvEnable must be set to "1" for the surface to register, even in a
// dev build.  scripts/dev-headless.sh sets it; `make dev` does not.
const EnvEnable = "YJ_TESTCTL"

// Prefix is the single mount point.  One pattern, one ServeMux entry.
const Prefix = "/__test/"

// Registrar is the slice of *assets.Handler this package needs, taken as
// an interface so testctl does not import the asset server (which would
// make the non-dev build's import graph differ from the dev one).
type Registrar interface {
	RegisterHandler(pattern string, handler http.Handler)
}

// Deps is everything the control surface is allowed to touch.  It is
// deliberately small: a database handle and a way to reach the Wails
// runtime context for event emission.
type Deps struct {
	Logger *slog.Logger
	DB     *database.DB
	// Context returns the live Wails application context.  It is a
	// function rather than a value because the context only exists
	// after OnStartup, which is later than registration.
	Context func() context.Context
}
