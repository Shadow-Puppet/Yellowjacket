//go:build !indexbuild

package explore

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// ServiceStartup is v3's service lifecycle hook: it runs once the
// runtime exists, and ctx is cancelled when the app shuts down.  It
// replaces v2's SetContext, which had to be called by hand from
// OnStartup and was exported, so it was also bound to the frontend.
//
// It is the one thing in this package that names the Wails application,
// and it is behind a build tag for the reason
// backend/events/runtime_wails.go states: cmd/indexbuild imports this
// package and is built without cgo, GTK or WebKit.  Nothing under the
// indexbuild tag runs a Wails app, so the hook — and the context it
// installs — is simply absent there.
func (e *Service) ServiceStartup(
	ctx context.Context,
	_ application.ServiceOptions,
) error {
	e.ctx = ctx
	e.index.SetContext(ctx)

	return nil
}
