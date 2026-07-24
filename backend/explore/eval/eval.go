// Package eval is the search-ranking evaluation harness.  It turns
// "this query feels wrong" into a number that goes up or down, so a
// ranking change can be validated against a frozen set of labelled
// queries instead of tuned by anecdote.
//
// The harness is deliberately decoupled from the explore package: it
// knows nothing about MusicBrainz, ListenBrainz, or the search index.
// A caller adapts whatever ranking function it wants to measure to the
// Ranker interface, loads a fixture set, and runs Evaluate.  The
// explore package wires its real index Search to this in an
// integration test (see explore/eval_harness_test.go).
package eval

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// ErrNoFixtures is returned when a fixture file contains zero queries.
var ErrNoFixtures = errors.New("eval: fixture set is empty")

// Result is one ranked search hit, reduced to the only two fields the
// harness needs to decide whether it matches an expectation.
type Result struct {
	EntityType string `json:"entityType"`
	MBID       string `json:"mbid"`
}

// Ranker produces an ordered result list for a query.  Best result
// first.  Implemented by adapting a real search function.
type Ranker interface {
	Rank(query string, limit int) []Result
}

// RankerFunc adapts a plain function to the Ranker interface.
type RankerFunc func(query string, limit int) []Result

// Rank calls the underlying function.
func (f RankerFunc) Rank(query string, limit int) []Result {
	return f(query, limit)
}

// Expected is one acceptable result for a fixture query.  Grade is the
// graded-relevance weight used by nDCG (higher = more relevant); it
// defaults to 1 when omitted.  Type is optional — when set, a ranked
// result must match both MBID and entity type to count as a hit.
type Expected struct {
	Type  string `json:"type,omitempty"`
	MBID  string `json:"mbid"`
	Grade int    `json:"grade,omitempty"`
}

// Fixture is a single labelled query: the input plus the result(s) a
// user should get.  Every edge case ever hand-fixed in the ranker
// belongs here so it can never silently regress.
type Fixture struct {
	Query  string     `json:"query"`
	Note   string     `json:"note,omitempty"`
	Expect []Expected `json:"expect"`
}

// LoadFixtures reads a JSON fixture file from disk.
func LoadFixtures(path string) ([]Fixture, error) {
	f, err := os.Open(path) //nolint:gosec // path is a test fixture, not user input
	if err != nil {
		return nil, fmt.Errorf("eval: open fixtures: %w", err)
	}

	defer func() { _ = f.Close() }()

	return ParseFixtures(f)
}

// ParseFixtures decodes a JSON fixture set from a reader.
func ParseFixtures(r io.Reader) ([]Fixture, error) {
	var fixtures []Fixture

	if err := json.NewDecoder(r).Decode(&fixtures); err != nil {
		return nil, fmt.Errorf("eval: decode fixtures: %w", err)
	}

	if len(fixtures) == 0 {
		return nil, ErrNoFixtures
	}

	return fixtures, nil
}

// matches reports whether a ranked result satisfies an expectation.
// MBID match is required; entity type is checked only when the
// expectation pins one.
func (e Expected) matches(r Result) bool {
	if !strings.EqualFold(e.MBID, r.MBID) {
		return false
	}

	if e.Type != "" && !strings.EqualFold(e.Type, r.EntityType) {
		return false
	}

	return true
}

// grade returns the graded-relevance weight, defaulting to 1.
func (e Expected) grade() int {
	if e.Grade <= 0 {
		return 1
	}

	return e.Grade
}
