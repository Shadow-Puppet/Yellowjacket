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

	ranked := autotag.RankCandidates(
		autotag.Group{Tracks: local},
		[]autotag.Candidate{longer, matching},
	)
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

	ranked := autotag.RankCandidates(
		autotag.Group{Tracks: local},
		[]autotag.Candidate{promo, official},
	)
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

	ranked := autotag.RankCandidates(autotag.Group{Tracks: local}, []autotag.Candidate{cand})
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

	ranked := autotag.RankCandidates(autotag.Group{Tracks: local}, []autotag.Candidate{cand})
	if ranked[0].Score < 0.75 { //nolint:mnd
		t.Errorf("VA-compilation exact track match = %.2f, want >= 0.75", ranked[0].Score)
	}
}

func TestRankCandidates_PenalizesWrongArtistSingleton(t *testing.T) {
	t.Parallel()

	// The reported false positive: a single with a generic title
	// matched a same-titled song by a DIFFERENT artist with a very
	// different length, at high confidence.  Artist was only a search
	// filter, never a scoring signal, so nothing pulled the wrong
	// candidate down.  Now the artist term + singleton evidence
	// scaling must keep it well below a confident match.
	local := []autotag.LocalTrack{
		{Title: "Intro", Artist: "Real Artist", TrackNumber: 1, LengthMillis: 90000},
	}

	// Same title, wrong artist, and a wildly different length — an MB
	// search hit that happens to share a common title.
	wrongArtist := autotag.Candidate{
		ReleaseMBID:  "wrong",
		Title:        "Intro",
		ArtistCredit: "Some Other Band",
		Status:       "Official",
		Source:       autotag.SourceMusicBrainz,
		Tracks: []autotag.CandidateTrack{
			{Position: 1, Title: "Intro", LengthMillis: 240000},
		},
	}

	// The correct release: same title, right artist, right length.
	rightArtist := autotag.Candidate{
		ReleaseMBID:  "right",
		Title:        "Intro",
		ArtistCredit: "Real Artist",
		Status:       "Official",
		Source:       autotag.SourceMusicBrainz,
		Tracks: []autotag.CandidateTrack{
			{Position: 1, Title: "Intro", LengthMillis: 90000},
		},
	}

	ranked := autotag.RankCandidates(
		autotag.Group{Tracks: local},
		[]autotag.Candidate{wrongArtist, rightArtist},
	)

	if ranked[0].ReleaseMBID != "right" {
		t.Fatalf(
			"top = %q (%.2f vs %.2f), want 'right'",
			ranked[0].ReleaseMBID, ranked[0].Score, ranked[1].Score,
		)
	}

	// The wrong-artist candidate must not read as a confident match.
	var wrongScore float64

	for _, c := range ranked {
		if c.ReleaseMBID == "wrong" {
			wrongScore = c.Score
		}
	}

	if wrongScore >= 0.75 { //nolint:mnd
		t.Errorf("wrong-artist singleton scored %.2f, want < 0.75", wrongScore)
	}
}

func TestRankCandidates_EvidenceScalingIsSourceAware(t *testing.T) {
	t.Parallel()

	// A perfect single-track match: identical title, artist, length.
	// From MusicBrainz it should be evidence-scaled (thin corroboration
	// on one track); from a local library it should NOT be — a local
	// candidate is the same release-group already tagged with MBIDs
	// elsewhere, so its confidence is not heuristic.
	local := []autotag.LocalTrack{
		{Title: "Solo", Artist: "Someone", TrackNumber: 1, LengthMillis: 200000},
	}

	tracks := []autotag.CandidateTrack{
		{Position: 1, Title: "Solo", LengthMillis: 200000},
	}

	mbCand := autotag.Candidate{
		ReleaseMBID: "mb", ArtistCredit: "Someone", Status: "Official",
		Source: autotag.SourceMusicBrainz, Tracks: tracks,
	}
	localCand := autotag.Candidate{
		ReleaseMBID: "local", ArtistCredit: "Someone", Status: "Official",
		Source: autotag.SourceLocal, Tracks: tracks,
	}

	scoredMB := autotag.RankCandidates(autotag.Group{Tracks: local}, []autotag.Candidate{mbCand})[0]
	scoredLocal := autotag.RankCandidates(autotag.Group{Tracks: local}, []autotag.Candidate{localCand})[0]

	if scoredMB.Breakdown.Evidence >= 1.0 {
		t.Errorf("MB singleton evidence = %.3f, want < 1.0", scoredMB.Breakdown.Evidence)
	}

	if scoredLocal.Breakdown.Evidence < 1.0 {
		t.Errorf(
			"local singleton evidence = %.3f, want 1.0 (not scaled)",
			scoredLocal.Breakdown.Evidence,
		)
	}

	if scoredLocal.Score <= scoredMB.Score {
		t.Errorf(
			"local perfect single (%.3f) should outscore the evidence-scaled MB single (%.3f)",
			scoredLocal.Score, scoredMB.Score,
		)
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

	ranked := autotag.RankCandidates(
		autotag.Group{Tracks: local},
		[]autotag.Candidate{queen, eagles},
	)
	if ranked[0].ReleaseMBID != "eagles-gh" {
		t.Errorf(
			"top = %q (%.2f vs %.2f), want 'eagles-gh'",
			ranked[0].ReleaseMBID, ranked[0].Score, ranked[1].Score,
		)
	}
}

func TestRankCandidates_AlbumTitleSeparatesCompilation(t *testing.T) {
	t.Parallel()

	// Identical tracklists: the studio album and a greatest-hits comp
	// that contains the same recordings.  Track alignment can't
	// separate them — the folder's album name must.
	local := []autotag.LocalTrack{
		{Title: "Song A", Artist: "The Band", TrackNumber: 1, LengthMillis: 200000},
		{Title: "Song B", Artist: "The Band", TrackNumber: 2, LengthMillis: 210000},
		{Title: "Song C", Artist: "The Band", TrackNumber: 3, LengthMillis: 195000},
	}

	tracks := []autotag.CandidateTrack{
		{Position: 1, Title: "Song A", LengthMillis: 200000},
		{Position: 2, Title: "Song B", LengthMillis: 210000},
		{Position: 3, Title: "Song C", LengthMillis: 195000},
	}

	album := autotag.Candidate{
		ReleaseMBID: "studio", Title: "The Studio Album",
		ArtistCredit: "The Band", Status: "Official", Tracks: tracks,
	}
	comp := autotag.Candidate{
		ReleaseMBID: "comp", Title: "Greatest Hits",
		ArtistCredit: "The Band", Status: "Official", Tracks: tracks,
	}

	g := autotag.Group{
		AlbumName: "The Studio Album", AlbumArtist: "The Band", Tracks: local,
	}

	ranked := autotag.RankCandidates(g, []autotag.Candidate{comp, album})
	if ranked[0].ReleaseMBID != "studio" {
		t.Errorf(
			"top = %q (%.3f vs %.3f), want 'studio' (album-title term)",
			ranked[0].ReleaseMBID, ranked[0].Score, ranked[1].Score,
		)
	}
}

func TestRankCandidates_ReleaseGroupTypeBreaksTies(t *testing.T) {
	t.Parallel()

	// Same tracks, same title — one RG is an Album, the other a
	// Compilation.  Type preference should break the tie toward the
	// studio album (Picard weights release type for the same reason).
	local := []autotag.LocalTrack{
		{Title: "Song A", TrackNumber: 1, LengthMillis: 200000},
		{Title: "Song B", TrackNumber: 2, LengthMillis: 210000},
		{Title: "Song C", TrackNumber: 3, LengthMillis: 195000},
	}

	tracks := []autotag.CandidateTrack{
		{Position: 1, Title: "Song A", LengthMillis: 200000},
		{Position: 2, Title: "Song B", LengthMillis: 210000},
		{Position: 3, Title: "Song C", LengthMillis: 195000},
	}

	album := autotag.Candidate{
		ReleaseMBID: "album", Title: "X", Status: "Official",
		PrimaryType: "Album", Tracks: tracks,
	}
	comp := autotag.Candidate{
		ReleaseMBID: "comp", Title: "X", Status: "Official",
		PrimaryType: "Compilation", Tracks: tracks,
	}

	ranked := autotag.RankCandidates(
		autotag.Group{Tracks: local}, []autotag.Candidate{comp, album},
	)
	if ranked[0].ReleaseMBID != "album" {
		t.Errorf(
			"top = %q (%.3f vs %.3f), want 'album' (RG type preference)",
			ranked[0].ReleaseMBID, ranked[0].Score, ranked[1].Score,
		)
	}
}

func TestRankCandidates_SingleDiscGroupAgainstMultiDiscRelease(t *testing.T) {
	t.Parallel()

	// The group key is per-disc, so "disc 1 of 2" folders score
	// against multi-disc releases.  Alignment and track count must
	// compare against disc 1's tracks only — not get punished for
	// "missing" all of disc 2.
	local := []autotag.LocalTrack{
		{Title: "D1T1", DiscNumber: 1, TrackNumber: 1, LengthMillis: 200000},
		{Title: "D1T2", DiscNumber: 1, TrackNumber: 2, LengthMillis: 210000},
		{Title: "D1T3", DiscNumber: 1, TrackNumber: 3, LengthMillis: 195000},
	}

	cand := autotag.Candidate{
		ReleaseMBID: "2disc",
		Status:      "Official",
		Tracks: []autotag.CandidateTrack{
			{Position: 1, DiscNumber: 1, Title: "D1T1", LengthMillis: 200000},
			{Position: 2, DiscNumber: 1, Title: "D1T2", LengthMillis: 210000},
			{Position: 3, DiscNumber: 1, Title: "D1T3", LengthMillis: 195000},
			{Position: 1, DiscNumber: 2, Title: "D2T1", LengthMillis: 220000},
			{Position: 2, DiscNumber: 2, Title: "D2T2", LengthMillis: 230000},
			{Position: 3, DiscNumber: 2, Title: "D2T3", LengthMillis: 240000},
		},
	}

	ranked := autotag.RankCandidates(
		autotag.Group{Tracks: local}, []autotag.Candidate{cand},
	)

	top := ranked[0]
	if top.Score < 0.85 { //nolint:mnd
		t.Errorf("disc-1 folder vs 2-disc release = %.3f, want >= 0.85", top.Score)
	}

	if top.Breakdown.TrackCountFit != 1.0 {
		t.Errorf(
			"track count fit = %.2f, want 1.0 (counted against disc 1 only)",
			top.Breakdown.TrackCountFit,
		)
	}

	// No "missing" rows for disc 2 — the folder is complete for its
	// disc.
	for _, a := range top.Alignments {
		if a.Status == autotag.AlignmentMissing {
			t.Errorf("unexpected missing alignment for %q", a.CandidateTitle)
		}
	}
}

func TestRankCandidates_RecordingMBIDLocksAlignment(t *testing.T) {
	t.Parallel()

	// The local title is garbled, but its recording MBID matches a
	// candidate track — identity beats similarity: the pair must
	// align, count as matched, and not drag the title average down.
	local := []autotag.LocalTrack{
		{Title: "trck 01", RecordingMBID: "rec-a", TrackNumber: 1, LengthMillis: 200000},
		{Title: "Song B", TrackNumber: 2, LengthMillis: 210000},
		{Title: "Song C", TrackNumber: 3, LengthMillis: 195000},
	}

	cand := autotag.Candidate{
		ReleaseMBID: "rel",
		Status:      "Official",
		Tracks: []autotag.CandidateTrack{
			{Position: 1, Title: "Song A", LengthMillis: 200000, MBID: "rec-a"},
			{Position: 2, Title: "Song B", LengthMillis: 210000},
			{Position: 3, Title: "Song C", LengthMillis: 195000},
		},
	}

	ranked := autotag.RankCandidates(
		autotag.Group{Tracks: local}, []autotag.Candidate{cand},
	)

	top := ranked[0]

	var locked *autotag.TrackAlignment

	for i := range top.Alignments {
		if top.Alignments[i].LocalIndex == 0 {
			locked = &top.Alignments[i]
		}
	}

	if locked == nil || locked.Status != autotag.AlignmentMatched || !locked.IDMatch {
		t.Fatalf("garbled-title track should be ID-locked matched, got %+v", locked)
	}

	if top.Breakdown.TitleAvg < 0.99 {
		t.Errorf(
			"title avg = %.3f, want ~1.0 (ID-locked pair counts as perfect title)",
			top.Breakdown.TitleAvg,
		)
	}
}
