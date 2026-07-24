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
	queries       []string
	searchByStep  map[int][]MBReleaseGroupHit
	browseByMBID  map[string][]MBRelease
	browseCalls   map[string]int
	lookupRels    map[string]MBRelease
	lookupRGs     map[string]MBReleaseGroupHit
	searchRecs    []MBRecordingHit
	recRelsByMBID map[string][]MBReleaseRef
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
	if f.browseCalls == nil {
		f.browseCalls = make(map[string]int)
	}

	f.browseCalls[mbid]++

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

func (f *fakeMBClient) SearchRecordings(
	_ context.Context, query string, _ int,
) ([]MBRecordingHit, int, error) {
	f.queries = append(f.queries, query)

	return f.searchRecs, len(f.searchRecs), nil
}

func (f *fakeMBClient) LookupRecordingReleases(
	_ context.Context, mbid string,
) ([]MBReleaseRef, error) {
	return f.recRelsByMBID[mbid], nil
}

func TestBuildMBQueryCascade_StepsOrder(t *testing.T) {
	t.Parallel()

	// Normalize() is a no-op for these inputs — ASCII, no
	// qualifier suffix, no punctuation — so cascade builds directly.
	steps := buildMBQueryCascade("abbey road", "the beatles", 17, false)

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
	steps := buildMBQueryCascade("abbey road", "", 0, false)
	for _, step := range steps {
		if strings.Contains(strings.ToLower(step.query), "remastered") {
			t.Errorf("step %q should not contain qualifier: %q", step.label, step.query)
		}
	}
}

func TestBuildMBQueryCascade_VariousArtists(t *testing.T) {
	t.Parallel()

	// VA-likely groups must filter on the Various Artists arid, not
	// on whatever plurality artist the compilation's tracks have.
	steps := buildMBQueryCascade("now that's music", "artist one", 12, true)

	if !strings.Contains(steps[0].query, "arid:"+mbidVariousArtists) {
		t.Errorf("VA step 1 should carry the VA arid, got %q", steps[0].query)
	}

	if strings.Contains(steps[0].query, "artist:") {
		t.Errorf("VA step 1 must not carry an artist: clause, got %q", steps[0].query)
	}
}

// abbeyRoadGroup is a group whose single track matches the rg1
// fixture release well enough to clear cascadeSufficient.
func abbeyRoadGroup() Group {
	return Group{
		AlbumName:   "Abbey Road",
		AlbumArtist: "The Beatles",
		Tracks: []LocalTrack{{
			Title: "Come Together", TrackNumber: 1, LengthMillis: 259000,
		}},
	}
}

func TestMBResolver_CascadeStopsWhenSufficient(t *testing.T) {
	t.Parallel()

	fake := &fakeMBClient{
		searchByStep: map[int][]MBReleaseGroupHit{
			// step 0 (strict) returns nothing; step 1 (no-track-count)
			// hits with a release that scores well against the group.
			1: {{MBID: "rg1", Title: "Abbey Road"}},
		},
		browseByMBID: map[string][]MBRelease{
			"rg1": {{
				MBID: "rel1", Title: "Abbey Road", Status: "Official",
				Tracks: []CandidateTrack{
					{Position: 1, Title: "Come Together", LengthMillis: 259000},
				},
			}},
		},
	}

	r := NewMBResolver(fake, slog.New(slog.DiscardHandler))

	cands, err := r.ResolveMB(context.Background(), abbeyRoadGroup())
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

func TestMBResolver_CascadeContinuesPastMediocreHits(t *testing.T) {
	t.Parallel()

	// Step 0 returns a same-title release whose track list doesn't
	// match the folder at all — plausible enough to browse, but it
	// must NOT stop the cascade ("first non-empty step wins" was the
	// old, wrong behavior).  Step 1 surfaces the real album; both
	// candidates come back merged.
	fake := &fakeMBClient{
		searchByStep: map[int][]MBReleaseGroupHit{
			0: {{MBID: "rg-decoy", Title: "Abbey Road"}},
			1: {{MBID: "rg-real", Title: "Abbey Road"}},
		},
		browseByMBID: map[string][]MBRelease{
			"rg-decoy": {{
				MBID: "rel-decoy", Title: "Abbey Road",
				Tracks: []CandidateTrack{
					{Position: 1, Title: "Something Else Entirely", LengthMillis: 111000},
					{Position: 2, Title: "Not It Either", LengthMillis: 122000},
				},
			}},
			"rg-real": {{
				MBID: "rel-real", Title: "Abbey Road", Status: "Official",
				Tracks: []CandidateTrack{
					{Position: 1, Title: "Come Together", LengthMillis: 259000},
				},
			}},
		},
	}

	r := NewMBResolver(fake, slog.New(slog.DiscardHandler))

	cands, err := r.ResolveMB(context.Background(), abbeyRoadGroup())
	if err != nil {
		t.Fatalf("ResolveMB: %v", err)
	}

	if len(cands) != 2 { //nolint:mnd
		t.Fatalf("expected merged candidates from both steps, got %d", len(cands))
	}

	if len(fake.queries) < 2 { //nolint:mnd
		t.Errorf(
			"cascade should have continued past the decoy step, got %d queries",
			len(fake.queries),
		)
	}
}

func TestMBResolver_CascadeBrowsesEachReleaseGroupOnce(t *testing.T) {
	t.Parallel()

	// The same release group surfacing at multiple cascade steps must
	// only be browsed (and returned) once.
	fake := &fakeMBClient{
		searchByStep: map[int][]MBReleaseGroupHit{
			0: {{MBID: "rg-dup", Title: "Abbey Road"}},
			1: {{MBID: "rg-dup", Title: "Abbey Road"}},
			2: {{MBID: "rg-dup", Title: "Abbey Road"}},
			3: {{MBID: "rg-dup", Title: "Abbey Road"}},
		},
		browseByMBID: map[string][]MBRelease{
			// Poor track match so the cascade keeps going.
			"rg-dup": {{
				MBID: "rel-dup", Title: "Abbey Road",
				Tracks: []CandidateTrack{
					{Position: 1, Title: "Unrelated", LengthMillis: 100000},
				},
			}},
		},
	}

	r := NewMBResolver(fake, slog.New(slog.DiscardHandler))

	cands, err := r.ResolveMB(context.Background(), abbeyRoadGroup())
	if err != nil {
		t.Fatalf("ResolveMB: %v", err)
	}

	if len(cands) != 1 {
		t.Fatalf("expected 1 deduplicated candidate, got %d", len(cands))
	}

	if fake.browseCalls["rg-dup"] != 1 {
		t.Errorf("browse calls for rg-dup = %d, want 1", fake.browseCalls["rg-dup"])
	}
}

func TestMBResolver_SkipsImplausibleHits(t *testing.T) {
	t.Parallel()

	// A hit resembling neither the album name nor the artist must
	// not cost a browse round-trip.
	fake := &fakeMBClient{
		searchByStep: map[int][]MBReleaseGroupHit{
			0: {{MBID: "rg-junk", Title: "Polka Party Hits", ArtistCredit: "Zzyzx Ensemble"}},
		},
	}

	r := NewMBResolver(fake, slog.New(slog.DiscardHandler))

	if _, err := r.ResolveMB(context.Background(), abbeyRoadGroup()); err != nil {
		t.Fatalf("ResolveMB: %v", err)
	}

	if fake.browseCalls["rg-junk"] != 0 {
		t.Errorf("junk hit was browsed %d times, want 0", fake.browseCalls["rg-junk"])
	}
}

func TestMBResolver_AbortsOnEmptyAlbumName(t *testing.T) {
	t.Parallel()

	fake := &fakeMBClient{}
	r := NewMBResolver(fake, slog.New(slog.DiscardHandler))

	cands, err := r.ResolveMB(context.Background(), Group{})
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

func TestResolveByRecordingMBIDs_VotesAcrossRecordings(t *testing.T) {
	t.Parallel()

	// rec-1 and rec-2 both appear on rel-shared; rec-1 also appears
	// on rel-solo.  The shared release gets 2 votes and wins.
	fake := &fakeMBClient{
		recRelsByMBID: map[string][]MBReleaseRef{
			"rec-1": {
				{MBID: "rel-shared", Status: "Official", Date: "1969"},
				{MBID: "rel-solo", Status: "Official", Date: "1968"},
			},
			"rec-2": {
				{MBID: "rel-shared", Status: "Official", Date: "1969"},
			},
		},
		lookupRels: map[string]MBRelease{
			"rel-shared": {
				MBID: "rel-shared", Title: "Abbey Road",
				Tracks: []CandidateTrack{
					{Position: 1, Title: "Come Together", MBID: "rec-1"},
					{Position: 2, Title: "Something", MBID: "rec-2"},
				},
			},
		},
	}

	r := NewMBResolver(fake, slog.New(slog.DiscardHandler))

	cands, err := r.ResolveByRecordingMBIDs(
		context.Background(), []string{"rec-1", "rec-2"},
	)
	if err != nil {
		t.Fatalf("ResolveByRecordingMBIDs: %v", err)
	}

	if len(cands) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(cands))
	}

	if cands[0].ReleaseMBID != "rel-shared" {
		t.Errorf("release = %q, want 'rel-shared' (2 votes beats 1)", cands[0].ReleaseMBID)
	}

	if cands[0].Provenance != "id" {
		t.Errorf("provenance = %q, want 'id'", cands[0].Provenance)
	}
}

func TestResolveByRecordingMBIDs_NoResults(t *testing.T) {
	t.Parallel()

	fake := &fakeMBClient{recRelsByMBID: map[string][]MBReleaseRef{}}
	r := NewMBResolver(fake, slog.New(slog.DiscardHandler))

	cands, err := r.ResolveByRecordingMBIDs(context.Background(), []string{"rec-x"})
	if err != nil {
		t.Fatalf("ResolveByRecordingMBIDs: %v", err)
	}

	if cands != nil {
		t.Errorf("expected nil candidates for unknown recordings, got %d", len(cands))
	}
}

func TestPickRepresentativeRelease(t *testing.T) {
	t.Parallel()

	refs := []MBReleaseRef{
		{MBID: "promo-1970", Status: "Promotion", Date: "1970"},
		{MBID: "official-1975", Status: "Official", Date: "1975"},
		{MBID: "official-1969", Status: "Official", Date: "1969"},
		{MBID: "undated-official", Status: "Official", Date: ""},
	}

	// Official beats Promotion; among Official the earliest date wins.
	best := pickRepresentativeRelease(refs)
	if best.MBID != "official-1969" {
		t.Errorf("best = %q, want 'official-1969'", best.MBID)
	}

	if got := pickRepresentativeRelease(nil); got.MBID != "" {
		t.Errorf("empty input = %+v, want zero", got)
	}
}

func TestResolveOneRecordingMBID_ResolvesToRepresentativeRelease(t *testing.T) {
	t.Parallel()

	fake := &fakeMBClient{
		recRelsByMBID: map[string][]MBReleaseRef{
			"rec-1": {
				{MBID: "rel-reissue", Status: "Official", Date: "2011"},
				{MBID: "rel-original", Status: "Official", Date: "1979"},
			},
		},
		lookupRels: map[string]MBRelease{
			"rel-original": {
				MBID: "rel-original", Title: "The Wall",
				Tracks: []CandidateTrack{{Position: 1, Title: "Hey You"}},
			},
		},
	}

	r := NewMBResolver(fake, slog.New(slog.DiscardHandler))

	cand, err := r.ResolveOneRecordingMBID(context.Background(), "rec-1")
	if err != nil {
		t.Fatalf("ResolveOneRecordingMBID: %v", err)
	}

	// Earliest official release chosen and resolved in full.
	if cand.ReleaseMBID != "rel-original" {
		t.Errorf("release = %q, want 'rel-original'", cand.ReleaseMBID)
	}

	if cand.Provenance != "search-recording" {
		t.Errorf("provenance = %q, want 'search-recording'", cand.Provenance)
	}
}

func TestResolveOneRecordingMBID_NoReleases(t *testing.T) {
	t.Parallel()

	fake := &fakeMBClient{recRelsByMBID: map[string][]MBReleaseRef{}}
	r := NewMBResolver(fake, slog.New(slog.DiscardHandler))

	if _, err := r.ResolveOneRecordingMBID(context.Background(), "rec-x"); err == nil {
		t.Fatal("expected error for recording with no releases")
	}
}
