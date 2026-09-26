package download

import (
	"context"
	"strings"
	"testing"
)

// What Soulseek is asked, how, and what is kept from the answer (#271).

func TestSlskdQueries(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		dl   Download
		want []string
	}{
		{
			name: "a plain request is searched once",
			dl:   Download{Artist: "Radiohead", Album: "OK Computer"},
			want: []string{"Radiohead OK Computer"},
		},
		{
			name: "an edition qualifier gets a second query without it",
			dl:   Download{Artist: "Radiohead", Album: "OK Computer (Collector's Edition)"},
			want: []string{
				"Radiohead OK Computer (Collector's Edition)",
				"Radiohead OK Computer",
			},
		},
		{
			name: "a trailing remaster note",
			dl:   Download{Artist: "Pink Floyd", Album: "Animals - 2018 Remaster"},
			want: []string{
				"Pink Floyd Animals - 2018 Remaster",
				"Pink Floyd Animals",
			},
		},
		{
			name: "a leading dash would be an exclusion",
			dl:   Download{Artist: "Mocky", Album: "-ism"},
			want: []string{"Mocky -ism", "Mocky ism"},
		},
		{
			name: "a compilation is not searched by its placeholder artist",
			dl:   Download{Artist: "Various Artists", Album: "Pulp Fiction"},
			want: []string{"Various Artists Pulp Fiction", "Pulp Fiction"},
		},
		{
			name: "what the user typed is searched as written",
			dl:   Download{Query: "ok computer (deluxe)", Album: "OK Computer (Deluxe)"},
			want: []string{"ok computer (deluxe)"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := slskdQueries(tc.dl)
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("slskdQueries = %q, want %q", got, tc.want)
			}
		})
	}
}

// Both queries run, the options are stated rather than left to the
// daemon's defaults, and a folder both queries found is one candidate.
func TestSlskdSearchRunsBothQueriesAndMerges(t *testing.T) {
	t.Parallel()

	stub := newSlskdStub(t)
	stub.responses = []slskdResponse{{
		Username: "peer",
		Files: []slskdFile{
			{Filename: `\m\Radiohead - OK Computer\01 Airbag.flac`, Size: 1, Length: 284},
			{Filename: `\m\Radiohead - OK Computer\02 Paranoid Android.flac`, Size: 1, Length: 383},
		},
	}}

	s, _ := newStubSlskd(t, stub)

	got, err := s.Search(context.Background(), Download{
		ReleaseMBID: "rel", Artist: "Radiohead", Album: "OK Computer (Deluxe Edition)",
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("got %d candidates, want the one folder once", len(got))
	}

	if got[0].Files[0].LengthMillis != 284_000 {
		t.Errorf("length = %d ms, want 284000 from slskd's seconds", got[0].Files[0].LengthMillis)
	}

	stub.mu.Lock()
	searches := append([]map[string]any(nil), stub.searches...)
	gets := append([]string(nil), stub.searchGets...)
	stub.mu.Unlock()

	if len(searches) != 2 {
		t.Fatalf("ran %d searches, want 2", len(searches))
	}

	for _, body := range searches {
		for _, key := range []string{
			"searchTimeout", "responseLimit", "fileLimit",
			"minimumResponseFileCount", "maximumPeerQueueLength",
		} {
			if _, ok := body[key]; !ok {
				t.Errorf("search %q does not state %s", body["searchText"], key)
			}
		}
	}

	// The responses are fetched once at the end, not with every poll.
	for _, uri := range gets {
		if strings.Contains(uri, "includeResponses") {
			t.Errorf("poll %s asked for every response", uri)
		}
	}
}

// A daemon without the responses endpoint still returns results.
func TestSlskdSearchFallsBackForAnOlderDaemon(t *testing.T) {
	t.Parallel()

	stub := newSlskdStub(t)
	stub.noResponsesEndpoint = true
	stub.responses = []slskdResponse{{
		Username: "peer",
		Files: []slskdFile{
			{Filename: `\m\Album\01 A.flac`, Size: 1},
			{Filename: `\m\Album\02 B.flac`, Size: 1},
		},
	}}

	s, _ := newStubSlskd(t, stub)

	got, err := s.Search(context.Background(), Download{Query: "album"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(got) != 1 {
		t.Errorf("got %d candidates, want 1 through the fallback", len(got))
	}
}

func TestDurationAgreement(t *testing.T) {
	t.Parallel()

	cases := []struct {
		got, want int64
		score     float64
	}{
		{300_000, 300_000, 1},
		{301_500, 300_000, 1}, // a second of silence
		{300_000, 316_500, 0.5},
		{300_000, 345_000, 0}, // a different edit
	}

	for _, tc := range cases {
		if got := durationAgreement(tc.got, tc.want); got < tc.score-0.01 || got > tc.score+0.01 {
			t.Errorf("durationAgreement(%d, %d) = %f, want %f", tc.got, tc.want, got, tc.score)
		}
	}
}

// Two folders with the same track names are told apart by their
// lengths: one is the album, the other a live record of the same songs.
func TestDurationsSeparateTheRightRecording(t *testing.T) {
	t.Parallel()

	dl := okComputer()

	timed := func(id string, lengths ...int64) Candidate {
		c := candidateFor(id, allTitles(), ".flac", 30_000_000)
		for i := range c.Files {
			c.Files[i].LengthMillis = lengths[i]
		}

		return c
	}

	studio := timed("studio", trackMillis, trackMillis+1_000, trackMillis, trackMillis-500)
	live := timed(
		"live",
		trackMillis+60_000,
		trackMillis+75_000,
		trackMillis+50_000,
		trackMillis+90_000,
	)

	ranked := Rank(dl, []Candidate{live, studio}, nil, AutoDownloadPrefs{})

	if ranked[0].ID != "studio" {
		t.Fatalf("winner = %s, want the recording whose lengths match", ranked[0].ID)
	}

	if !ranked[0].Match.DurationKnown || ranked[0].Match.DurationFit < 0.99 {
		t.Errorf(
			"studio duration fit = %f known=%v",
			ranked[0].Match.DurationFit,
			ranked[0].Match.DurationKnown,
		)
	}

	if ranked[1].Match.DurationFit != 0 {
		t.Errorf("live duration fit = %f, want 0", ranked[1].Match.DurationFit)
	}
}

// Without lengths the score is exactly what it was before durations
// were read, so a provider that reports none is not penalised.
func TestUnknownDurationsLeaveTheScoreAlone(t *testing.T) {
	t.Parallel()

	dl := okComputer()
	c := Score(dl, candidateFor("c", allTitles(), ".flac", 30_000_000), 50, AutoDownloadPrefs{})

	if c.Match.DurationKnown {
		t.Fatal("no file states a length, yet durations are known")
	}

	want := weightTitleFit*c.Match.TitleFit +
		weightCompleteness*c.Match.Completeness +
		weightAlbumFit*c.Match.AlbumFit +
		weightArtistFit*c.Match.ArtistFit

	if c.Match.Overall != want {
		t.Errorf("match = %f, want the untimed formula's %f", c.Match.Overall, want)
	}
}
