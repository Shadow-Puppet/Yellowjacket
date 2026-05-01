package autotag_test

import (
	"testing"

	"yellowjacket/backend/autotag"
)

func TestRankCandidates_PrefersExactTrackCountMatch(t *testing.T) {
	t.Parallel()

	local := []autotag.LocalTrack{
		{Title: "A", TrackNumber: 1, LengthMillis: 200000},
		{Title: "B", TrackNumber: 2, LengthMillis: 200000},
		{Title: "C", TrackNumber: 3, LengthMillis: 200000},
	}

	matching := autotag.Candidate{
		ReleaseMBID: "exact",
		Title:       "Album",
		Tracks: []autotag.CandidateTrack{
			{Position: 1, Title: "A", LengthMillis: 200000},
			{Position: 2, Title: "B", LengthMillis: 200000},
			{Position: 3, Title: "C", LengthMillis: 200000},
		},
	}

	longer := autotag.Candidate{
		ReleaseMBID: "longer",
		Title:       "Album",
		Tracks: []autotag.CandidateTrack{
			{Position: 1, Title: "A", LengthMillis: 200000},
			{Position: 2, Title: "B", LengthMillis: 200000},
			{Position: 3, Title: "C", LengthMillis: 200000},
			{Position: 4, Title: "Bonus", LengthMillis: 200000},
			{Position: 5, Title: "Extra", LengthMillis: 200000},
		},
	}

	ranked := autotag.RankCandidates(local, []autotag.Candidate{longer, matching})
	if ranked[0].ReleaseMBID != "exact" {
		t.Errorf(
			"top = %q (score %.2f vs %.2f), want 'exact'",
			ranked[0].ReleaseMBID, ranked[0].Score, ranked[1].Score,
		)
	}
}

func TestRankCandidates_PrefersOfficial(t *testing.T) {
	t.Parallel()

	local := []autotag.LocalTrack{
		{Title: "A", TrackNumber: 1, LengthMillis: 200000},
	}

	tracks := []autotag.CandidateTrack{
		{Position: 1, Title: "A", LengthMillis: 200000},
	}

	official := autotag.Candidate{
		ReleaseMBID: "official",
		Status:      "Official",
		Tracks:      tracks,
	}

	promo := autotag.Candidate{
		ReleaseMBID: "promo",
		Status:      "Promotion",
		Tracks:      tracks,
	}

	ranked := autotag.RankCandidates(local, []autotag.Candidate{promo, official})
	if ranked[0].ReleaseMBID != "official" {
		t.Errorf("top = %q, want 'official'", ranked[0].ReleaseMBID)
	}
}

func TestRankCandidates_MultiDisc(t *testing.T) {
	t.Parallel()

	// Two-disc album: local has 4 tracks across 2 discs, candidate
	// matches them.  Should score near-perfectly.
	local := []autotag.LocalTrack{
		{Title: "D1T1", DiscNumber: 1, TrackNumber: 1, LengthMillis: 200000},
		{Title: "D1T2", DiscNumber: 1, TrackNumber: 2, LengthMillis: 200000},
		{Title: "D2T1", DiscNumber: 2, TrackNumber: 1, LengthMillis: 200000},
		{Title: "D2T2", DiscNumber: 2, TrackNumber: 2, LengthMillis: 200000},
	}

	cand := autotag.Candidate{
		ReleaseMBID: "multidisc",
		Status:      "Official",
		Tracks: []autotag.CandidateTrack{
			{Position: 1, DiscNumber: 1, Title: "D1T1", LengthMillis: 200000},
			{Position: 2, DiscNumber: 1, Title: "D1T2", LengthMillis: 200000},
			{Position: 1, DiscNumber: 2, Title: "D2T1", LengthMillis: 200000},
			{Position: 2, DiscNumber: 2, Title: "D2T2", LengthMillis: 200000},
		},
	}

	ranked := autotag.RankCandidates(local, []autotag.Candidate{cand})
	if ranked[0].Score < 0.75 { //nolint:mnd
		t.Errorf("multi-disc exact match = %.2f, want >= 0.75", ranked[0].Score)
	}
}

func TestRankCandidates_VariousArtists(t *testing.T) {
	t.Parallel()

	// Compilation: every track has a different artist in the tag.
	// Scoring should still produce a high score on title + length
	// even though artist credits differ.
	local := []autotag.LocalTrack{
		{Title: "Song 1", Artist: "Artist One", TrackNumber: 1, LengthMillis: 200000},
		{Title: "Song 2", Artist: "Artist Two", TrackNumber: 2, LengthMillis: 180000},
		{Title: "Song 3", Artist: "Artist Three", TrackNumber: 3, LengthMillis: 220000},
	}

	cand := autotag.Candidate{
		ReleaseMBID: "va-comp",
		Title:       "Various: Great Hits",
		Status:      "Official",
		Tracks: []autotag.CandidateTrack{
			{Position: 1, Title: "Song 1", LengthMillis: 200000},
			{Position: 2, Title: "Song 2", LengthMillis: 180000},
			{Position: 3, Title: "Song 3", LengthMillis: 220000},
		},
	}

	ranked := autotag.RankCandidates(local, []autotag.Candidate{cand})
	if ranked[0].Score < 0.75 { //nolint:mnd
		t.Errorf("VA-compilation exact track match = %.2f, want >= 0.75", ranked[0].Score)
	}
}

func TestRankCandidates_AmbiguousAlbumNames(t *testing.T) {
	t.Parallel()

	// Two candidates with the same album title ("Greatest Hits")
	// but one has matching tracks and one doesn't.  The track
	// alignment should pick the right one.
	local := []autotag.LocalTrack{
		{Title: "Hotel California", TrackNumber: 1, LengthMillis: 391000},
		{Title: "Take It Easy", TrackNumber: 2, LengthMillis: 213000},
	}

	eagles := autotag.Candidate{
		ReleaseMBID:  "eagles-gh",
		Title:        "Greatest Hits",
		ArtistCredit: "Eagles",
		Tracks: []autotag.CandidateTrack{
			{Position: 1, Title: "Hotel California", LengthMillis: 391000},
			{Position: 2, Title: "Take It Easy", LengthMillis: 213000},
		},
	}

	queen := autotag.Candidate{
		ReleaseMBID:  "queen-gh",
		Title:        "Greatest Hits",
		ArtistCredit: "Queen",
		Tracks: []autotag.CandidateTrack{
			{Position: 1, Title: "Bohemian Rhapsody", LengthMillis: 354000},
			{Position: 2, Title: "We Will Rock You", LengthMillis: 121000},
		},
	}

	ranked := autotag.RankCandidates(local, []autotag.Candidate{queen, eagles})
	if ranked[0].ReleaseMBID != "eagles-gh" {
		t.Errorf(
			"top = %q (%.2f vs %.2f), want 'eagles-gh'",
			ranked[0].ReleaseMBID, ranked[0].Score, ranked[1].Score,
		)
	}
}
