package database_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// writeVerb matches the first SQL keyword of a statement that mutates.
// Anchored to the start of the trimmed line, because a subquery or a
// column named "update" is not a write.
var writeVerb = regexp.MustCompile(
	`^\s*` + "`" + `?\s*(?i:INSERT|UPDATE|DELETE|REPLACE|CREATE|DROP|ALTER)\s`,
)

// queryCall matches a call to one of the read-pool helpers.  These
// route to DB.reader(), which in a real app is a second sql.DB opened
// query-only over the same file.
var queryCall = regexp.MustCompile(
	`\.Query(?:Context|ContextWith|Row)\s*\(`,
)

// TestNoWritesOnTheReadPool fails if a mutating statement is issued
// through one of the query-only read helpers.
//
// This is worth a test of its own because the failure mode is invisible
// to every other tier.  `CreateSmartPlaylist` ran an
// `INSERT ... RETURNING` through `QueryContext` — a write wearing a
// query's shape — and failed at runtime with "attempt to write a
// readonly database (8)", i.e. no smart playlist could be created at
// all.  Nothing caught it: `NewTestDB` shares one in-memory connection
// and sets `readDB` to nil, so `reader()` returns the *writer* there
// and every unit test of that path passed against a handle the app does
// not have.
//
// A text walk rather than a lint rule, for the same reason as
// TestNoDirectRuntimeEmits: golangci-lint runs once per build
// configuration and would not see a call in a tagged file.
func TestNoWritesOnTheReadPool(t *testing.T) {
	root := filepath.Join("..", "..")

	skipDirs := map[string]bool{
		".git":         true,
		"node_modules": true,
		"frontend":     true,
		"build":        true,
		".dev":         true,
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

		if filepath.Ext(path) != ".go" ||
			strings.HasSuffix(path, "_test.go") {
			return nil
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}

		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}

		lines := strings.Split(string(src), "\n")

		for i, line := range lines {
			if !queryCall.MatchString(line) ||
				strings.Contains(line, "QueryRowWriter") {
				continue
			}

			// The statement is usually on the following line, in a raw
			// string literal.  Look a little way ahead rather than only
			// at the call itself.
			for j := i; j < min(i+3, len(lines)); j++ {
				if writeVerb.MatchString(lines[j]) {
					t.Errorf(
						"%s:%d issues a write through a read-pool helper; "+
							"use ExecContext or QueryRowWriter\n\t%s",
						rel, j+1, strings.TrimSpace(lines[j]),
					)

					break
				}
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
}
