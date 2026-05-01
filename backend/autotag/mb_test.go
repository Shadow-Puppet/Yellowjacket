package autotag

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

var errFakeNotFound = errors.New("fake: not found")

// fakeMBClient is a minimal stub that records the queries the
// resolver sends and returns canned results per query/MBID.  The
// cascade tests orchestrate `searchByStep` so step N returns hits
// only when the resolver has already tried steps < N.
type fakeMBClient struct {
	queries      []string
	searchByStep map[int][]MBReleaseGroupHit
	browseByMBID map[string][]MBRelease
	lookupRels   map[string]MBRelease
	lookupRGs    map[string]MBReleaseGroupHit
}

func (f *fakeMBClient) SearchReleaseGroups(
	_ context.Context, query string, _ int,
) ([]MBReleaseGroupHit, int, error) {
	f.queries = append(f.queries, query)

	step := len(f.queries) - 1
	hits := f.searchByStep[step]

	return hits, len(hits), nil
}

func (f *fakeMBClient) BrowseReleases(
	_ context.Context, mbid string,
) ([]MBRelease, error) {
	return f.browseByMBID[mbid], nil
}

func (f *fakeMBClient) LookupRelease(
	_ context.Context, mbid string,
) (MBRelease, error) {
	rel, ok := f.lookupRels[mbid]
	if !ok {
		return MBRelease{}, errFakeNotFound
	}

	return rel, nil
}

func (f *fakeMBClient) LookupReleaseGroup(
	_ context.Context, mbid string,
) (MBReleaseGroupHit, error) {
	rg, ok := f.lookupRGs[mbid]
	if !ok {
		return MBReleaseGroupHit{}, errFakeNotFound
	}

	return rg, nil
}

func (f *fakeMBClient) LookupArtist(_ context.Context, _ string) (string, error) {
	return "", nil
}

func TestBuildMBQueryCascade_StepsOrder(t *testing.T) {
	t.Parallel()

	// Normalize() is a no-op for these inputs — ASCII, no
	// qualifier suffix, no punctuation — so cascade builds directly.
	steps := buildMBQueryCascade("abbey road", "the beatles", 17, "")

	if len(steps) < 3 {
		t.Fatalf("expected ≥3 cascade steps, got %d", len(steps))
	}

	if !strings.Contains(steps[0].query, "tracks:17") {
		t.Errorf("step 1 should include tracks:17, got %q", steps[0].query)
	}

	if strings.Contains(steps[1].query, "tracks:") {
		t.Errorf("step 2 should drop tracks:, got %q", steps[1].query)
	}

	if strings.Contains(steps[2].query, "artist:") {
		t.Errorf("step 3 should drop artist:, got %q", steps[2].query)
	}
}

func TestBuildMBQueryCascade_NormalizesInputs(t *testing.T) {
	t.Parallel()

	// Qualifier suffix "(Remastered 2009)" must be stripped by
	// the *caller*; verify the query emitter doesn't reintroduce it.
	steps := buildMBQueryCascade("abbey road", "", 0, "")
	for _, step := range steps {
		if strings.Contains(strings.ToLower(step.query), "remastered") {
			t.Errorf("step %q should not contain qualifier: %q", step.label, step.query)
		}
	}
}

func TestMBResolver_CascadeStopsOnFirstHit(t *testing.T) {
	t.Parallel()

	fake := &fakeMBClient{
		searchByStep: map[int][]MBReleaseGroupHit{
			// step 0 (strict) returns nothing; step 1 (no-track-count) hits.
			1: {{MBID: "rg1", Title: "Abbey Road"}},
		},
		browseByMBID: map[string][]MBRelease{
			"rg1": {{MBID: "rel1", Title: "Abbey Road", Tracks: []CandidateTrack{
				{Position: 1, Title: "Come Together"},
			}}},
		},
	}

	r := NewMBResolver(fake, slog.New(slog.DiscardHandler))

	cands, err := r.ResolveMB(
		context.Background(), "Abbey Road", "The Beatles", 17, "",
	)
	if err != nil {
		t.Fatalf("ResolveMB: %v", err)
	}

	if len(cands) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(cands))
	}

	if len(fake.queries) != 2 { //nolint:mnd
		t.Errorf("expected 2 search queries (strict + no-track-count), got %d", len(fake.queries))
	}

	if cands[0].Provenance != "no-track-count" {
		t.Errorf("provenance = %q, want 'no-track-count'", cands[0].Provenance)
	}
}

func TestMBResolver_AbortsOnEmptyAlbumName(t *testing.T) {
	t.Parallel()

	fake := &fakeMBClient{}
	r := NewMBResolver(fake, slog.New(slog.DiscardHandler))

	cands, err := r.ResolveMB(context.Background(), "", "", 0, "")
	if err != nil {
		t.Fatalf("ResolveMB: %v", err)
	}

	if cands != nil {
		t.Errorf("cands should be nil for empty album, got %d", len(cands))
	}

	if len(fake.queries) != 0 {
		t.Errorf("empty album should not trigger search, got %d queries", len(fake.queries))
	}
}

func TestMBResolver_ResolveOneReleaseMBID(t *testing.T) {
	t.Parallel()

	fake := &fakeMBClient{
		lookupRels: map[string]MBRelease{
			"rel-mbid": {
				MBID: "rel-mbid", Title: "Abbey Road",
				ArtistCredit: "The Beatles",
				Tracks:       []CandidateTrack{{Position: 1, Title: "Come Together"}},
			},
		},
	}

	r := NewMBResolver(fake, slog.New(slog.DiscardHandler))

	cand, err := r.ResolveOneReleaseMBID(context.Background(), "rel-mbid")
	if err != nil {
		t.Fatalf("ResolveOneReleaseMBID: %v", err)
	}

	if cand.Title != "Abbey Road" {
		t.Errorf("title = %q, want Abbey Road", cand.Title)
	}

	if cand.Provenance != "paste" {
		t.Errorf("provenance = %q, want 'paste'", cand.Provenance)
	}

	if len(cand.Tracks) != 1 {
		t.Errorf("expected 1 track, got %d", len(cand.Tracks))
	}
}
