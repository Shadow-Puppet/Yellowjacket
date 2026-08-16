package events_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// allowedEmitters are the only files permitted to call the Wails
// runtime's event emitter directly.  There is one call and it lives in
// runtime_wails.go rather than emit.go because naming the application
// package is what forces cgo, and cmd/indexbuild builds this package
// without it.
var allowedEmitters = map[string]bool{
	filepath.Join("backend", "events", "runtime_wails.go"): true,
}

// TestNoDirectRuntimeEmits fails if anything outside backend/events
// calls the Wails runtime's event emitter directly.
//
// The original reason was survival: v2's runtime.EventsEmit called
// log.Fatalf on a context that did not carry the runtime, so a direct
// call from a background worker could take the process down.  v3's
// emit takes no context and cannot do that, and the rule is kept for
// the weaker but still real reason — one emit path is what lets
// emitStatus drop an unchanged payload for every caller at once.
//
// This is a text walk rather than a golangci-lint rule because
// golangci-lint runs once per build configuration, so a call in an
// indexbuild- or dev-tagged file is only seen by the pass that compiles
// it.  Walking the tree sees all three, plus anything tagged out
// entirely.
func TestNoDirectRuntimeEmits(t *testing.T) {
	// A selector, not a bare name: built at runtime so this file does
	// not match itself, and qualified so it catches the call however
	// the application value is named (app.Event.Emit, a.Event.Emit,
	// application.Get().Event.Emit) without matching an identifier
	// that merely ends in the same letters.
	needle := ".Event" + ".Emit("

	root := filepath.Join("..", "..")

	skipDirs := map[string]bool{
		".git":         true,
		"node_modules": true,
		"frontend":     true,
		"build":        true,
	}

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}

			return nil
		}

		if filepath.Ext(path) != ".go" {
			return nil
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}

		if allowedEmitters[rel] {
			return nil
		}

		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}

		for i, line := range strings.Split(string(src), "\n") {
			if strings.Contains(line, needle) {
				t.Errorf(
					"%s:%d calls the Wails emitter directly; use events.Emit\n\t%s",
					rel, i+1, strings.TrimSpace(line),
				)
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
}
