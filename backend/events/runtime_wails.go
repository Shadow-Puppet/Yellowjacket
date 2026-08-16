//go:build !indexbuild

package events

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

// emitRuntime pushes an event through the running Wails application.
//
// It is the app's half of a two-file split, and the split exists for a
// build constraint rather than a design one: v3's application package
// is cgo on Linux, so anything importing it needs GTK and WebKit
// headers to compile.  cmd/indexbuild and cmd/indexexport reach this
// package through backend/explore and are built in a plain golang
// container with CGO_ENABLED=0 (.gitea/workflows/index-artifact.yml),
// where that is not available and not wanted.  See
// runtime_indexbuild.go for the other half.
func emitRuntime(name string, data ...any) error {
	// application.Get() returns nil when no app is running — under
	// test, before Run, and after shutdown.  It does not terminate the
	// process, which is what the v2 probe of the private "events"
	// context key existed to avoid.
	app := application.Get()
	if app == nil {
		return ErrNoRuntime
	}

	app.Event.Emit(name, data...)

	return nil
}
