package explore

import "testing"

// TestComputeIntentPrior_ArtistNameNotDrownedByOwnCatalog reproduces a bug
// where searching an artist's exact name (e.g. "blank banshee") failed to
// surface the artist in "top results", even though it appeared correctly
// in the dedicated Artists section. The cause: every album/track by that
// artist in the candidate pool also has ArtistCredit == query, and each
// one multiplied the album/recording category weight, drowning out the
// single true artist candidate which only boosted its own category once.
func TestComputeIntentPrior_ArtistNameNotDrownedByOwnCatalog(t *testing.T) {
	svc := &Service{}
	q := "blank banshee"

	result := &MBSearchResult{
		Artists: []MBArtist{
			{MBID: "artist-1", Name: "Blank Banshee", ListenerCount: 50000},
		},
		ReleaseGroups: []MBReleaseGroup{
			{
				MBID:          "rg-1",
				Title:         "Blank Banshee 0",
				ArtistCredit:  "Blank Banshee",
				ListenerCount: 20000,
			},
			{
				MBID:          "rg-2",
				Title:         "Blank Banshee 1",
				ArtistCredit:  "Blank Banshee",
				ListenerCount: 18000,
			},
			{
				MBID:          "rg-3",
				Title:         "Blank Banshee 1.5",
				ArtistCredit:  "Blank Banshee",
				ListenerCount: 15000,
			},
		},
		Recordings: []MBRecording{
			{
				MBID:          "rec-1",
				Title:         "Teen Pregnancy",
				ArtistCredit:  "Blank Banshee",
				ListenerCount: 12000,
			},
			{MBID: "rec-2", Title: "Chase", ArtistCredit: "Blank Banshee", ListenerCount: 11000},
			{MBID: "rec-3", Title: "Ghost", ArtistCredit: "Blank Banshee", ListenerCount: 9000},
		},
	}

	exactCandidates := []topCandidate{
		{category: "artist", topResult: TopResult{MBID: "artist-1", Name: "Blank Banshee"}},
		{
			category: "release_group",
			topResult: TopResult{
				MBID:         "rg-1",
				Name:         "Blank Banshee 0",
				ArtistCredit: "Blank Banshee",
			},
		},
		{
			category: "release_group",
			topResult: TopResult{
				MBID:         "rg-2",
				Name:         "Blank Banshee 1",
				ArtistCredit: "Blank Banshee",
			},
		},
		{
			category: "release_group",
			topResult: TopResult{
				MBID:         "rg-3",
				Name:         "Blank Banshee 1.5",
				ArtistCredit: "Blank Banshee",
			},
		},
		{
			category: "recording",
			topResult: TopResult{
				MBID:         "rec-1",
				Name:         "Teen Pregnancy",
				ArtistCredit: "Blank Banshee",
			},
		},
		{
			category:  "recording",
			topResult: TopResult{MBID: "rec-2", Name: "Chase", ArtistCredit: "Blank Banshee"},
		},
		{
			category:  "recording",
			topResult: TopResult{MBID: "rec-3", Name: "Ghost", ArtistCredit: "Blank Banshee"},
		},
	}

	prior := svc.computeIntentPrior(q, result, nil, exactCandidates)

	if prior.artist <= prior.album {
		t.Errorf(
			"expected artist prior (%v) > album prior (%v) for an exact artist-name search",
			prior.artist,
			prior.album,
		)
	}

	if prior.artist <= prior.recording {
		t.Errorf(
			"expected artist prior (%v) > recording prior (%v) for an exact artist-name search",
			prior.artist,
			prior.recording,
		)
	}
}
