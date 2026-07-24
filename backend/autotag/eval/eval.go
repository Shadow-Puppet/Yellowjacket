// Package eval is the autotag candidate-scoring evaluation harness.
// It turns "this match feels wrong" into a number that goes up or
// down, so a scoring change can be validated against a frozen set of
// labelled cases instead of tuned by anecdote.
//
// A case describes a local album-group (the files on disk) plus a set
// of candidate releases, and pins expectations: which candidate must
// rank first, and per-candidate score floors/ceilings.  The harness
// is decoupled from the scorer: a caller adapts whatever ranking
// function it wants to measure to the Ranker interface (the autotag
// package wires autotag.RankCandidates to it in eval_harness_test.go).
//
// The point of the ceiling assertions is negative testing: a known
// wrong candidate (same title, different artist) must stay BELOW a
// confidence bar, which is exactly the false-positive class the
// artist term + evidence scaling exist to suppress.
package eval

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

// ErrNoCases is returned when a fixture file contains zero cases.
var ErrNoCases = errors.New("eval: fixture set is empty")

// LocalTrackFixture is one on-disk track, in the shape a case author
// hand-writes.  Millisecond durations match the scorer's units.
type LocalTrackFixture struct {
	Title    string `json:"title"`
	Artist   string `json:"artist,omitempty"`
	Track    int    `json:"track,omitempty"`
	Disc     int    `json:"disc,omitempty"`
	LengthMs int64  `json:"lengthMs,omitempty"`
}

// CandidateTrackFixture is one track inside a candidate release.
type CandidateTrackFixture struct {
	Pos      int    `json:"pos"`
	Disc     int    `json:"disc,omitempty"`
	Title    string `json:"title"`
	LengthMs int64  `json:"lengthMs,omitempty"`
}

// CandidateFixture is one candidate release the scorer must rank.
// Source is "local" or "musicbrainz" (default) — it drives
// evidence scaling, so it matters for singleton cases.
type CandidateFixture struct {
	MBID         string                  `json:"mbid"`
	Title        string                  `json:"title,omitempty"`
	ArtistCredit string                  `json:"artistCredit,omitempty"`
	Status       string                  `json:"status,omitempty"`
	Country      string                  `json:"country,omitempty"`
	PrimaryType  string                  `json:"primaryType,omitempty"`
	Source       string                  `json:"source,omitempty"`
	Tracks       []CandidateTrackFixture `json:"tracks"`
}

// Case is a single labelled scoring scenario.  Every real-world
// mismatch worth guarding against belongs here so it can never
// silently regress.
//
//   - AlbumName / AlbumArtist mirror the tagging item's fields —
//     leave them empty to keep the album-title / artist terms
//     neutral for the case.
//   - ExpectTop, when set, is the MBID that must rank first.
//   - MaxScore pins per-MBID ceilings (candidate must score <= value).
//   - MinScore pins per-MBID floors  (candidate must score >= value).
type Case struct {
	Note        string              `json:"note,omitempty"`
	AlbumName   string              `json:"albumName,omitempty"`
	AlbumArtist string              `json:"albumArtist,omitempty"`
	Local       []LocalTrackFixture `json:"local"`
	Candidates  []CandidateFixture  `json:"candidates"`
	ExpectTop   string              `json:"expectTop,omitempty"`
	MaxScore    map[string]float64  `json:"maxScore,omitempty"`
	MinScore    map[string]float64  `json:"minScore,omitempty"`
}

// LoadCases reads a JSON fixture file from disk.
func LoadCases(path string) ([]Case, error) {
	f, err := os.Open(path) //nolint:gosec // path is a test fixture, not user input
	if err != nil {
		return nil, fmt.Errorf("eval: open cases: %w", err)
	}

	defer func() { _ = f.Close() }()

	return ParseCases(f)
}

// ParseCases decodes a JSON case set from a reader.
func ParseCases(r io.Reader) ([]Case, error) {
	var cases []Case

	if err := json.NewDecoder(r).Decode(&cases); err != nil {
		return nil, fmt.Errorf("eval: decode cases: %w", err)
	}

	if len(cases) == 0 {
		return nil, ErrNoCases
	}

	return cases, nil
}
