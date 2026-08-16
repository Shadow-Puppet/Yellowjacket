//go:build indexbuild

package main_test

import (
	"os/exec"
	"strings"
	"testing"
)

// wailsApp is the package that costs cgo: on Linux it is GTK and
// WebKit bindings, which the index build container does not have.
const wailsApp = "github.com/wailsapp/wails/v3/pkg/application"

// TestIndexToolsDoNotImportWails fails if cmd/indexbuild or
// cmd/indexexport reach the Wails application package.
//
// .gitea/workflows/index-artifact.yml builds both in a plain golang
// image with CGO_ENABLED=0, on the stated grounds that neither imports
// the app.  That stopped being true when the v3 migration put
// application.Get() in backend/events and a ServiceStartup hook in
// backend/explore, and the workflow only found out at the point where
// it is most expensive to find out — the job that owns the 205 GB
// checkpoint.  Both are behind the indexbuild tag now
// (backend/events/runtime_wails.go), and this is what keeps them there.
//
// It shells out to `go list` because the constraint is about the
// *linked* dependency graph under a particular tag, which no import of
// this package can observe.
func TestIndexToolsDoNotImportWails(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go toolchain on PATH")
	}

	for _, pkg := range []string{
		"yellowjacket/cmd/indexbuild",
		"yellowjacket/cmd/indexexport",
	} {
		t.Run(pkg, func(t *testing.T) {
			t.Parallel()

			out, err := exec.Command(
				"go", "list", "-deps", "-tags", "indexbuild", pkg,
			).Output()
			if err != nil {
				t.Fatalf("go list %s: %v", pkg, err)
			}

			for _, dep := range strings.Split(string(out), "\n") {
				if strings.TrimSpace(dep) == wailsApp {
					t.Errorf(
						"%s imports %s, so it needs cgo and a GTK/WebKit "+
							"toolchain; put the dependency behind the "+
							"indexbuild tag",
						pkg, wailsApp,
					)
				}
			}
		})
	}
}
