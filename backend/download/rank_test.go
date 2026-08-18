package download

import (
	"strings"
	"testing"
)

// trackMillis is five minutes; okComputer's four of them make a
// twenty-minute release, which is what turns a candidate's byte count
// into a bitrate the assertions below can name.
const trackMillis = 5 * 60 * 1000

// okComputerRuntime is that release's runtime, for the helpers that
// need it directly.
const okComputerRuntime = 4 * trackMillis

// kbpsCandidate builds an annotated candidate whose audio adds up to
// the given average bitrate over okComputer's runtime.
func kbpsCandidate(id, ext string, kbps int) Candidate {
	// bits = kbps × 1000 × (runtimeMillis / 1000), so the thousands
	// cancel and the byte count is kbps × runtimeMillis / 8.
	const bitsPerByte = 8

	total := int64(kbps) * okComputerRuntime / bitsPerByte

	c := candidateFor(id, allTitles(), ext, total/int64(len(allTitles())))
	c.Files = AnnotateFiles(c.Files)
	c.TotalSize = total

	return c
}

// okComputer is the reference request used across ranking tests.
func okComputer() Download {
	return Download{
		ReleaseMBID: "mbid-ok-computer",
		Artist:      "Radiohead",
		Album:       "OK Computer",
		// Four five-minute tracks: twenty minutes, so a candidate's
		// bitrate is a number these tests can state exactly. Without
		// lengths there is no runtime and the bitrate window has
		// nothing to divide by.
		Expected: []ExpectedTrack{
			{Position: 1, Title: "Airbag", LengthMillis: trackMillis},
			{Position: 2, Title: "Paranoid Android", LengthMillis: trackMillis},
			{Position: 3, Title: "Subterranean Homesick Alien", LengthMillis: trackMillis},
			{Position: 4, Title: "Exit Music (For a Film)", LengthMillis: trackMillis},
		},
	}
}

// candidateFor builds a candidate whose files follow "NN - Title.ext".
func candidateFor(id string, titles []string, ext string, size int64) Candidate {
	files := make([]CandidateFile, 0, len(titles))

	for i, tt := range titles {
		files = append(files, CandidateFile{
			Path: "Radiohead - OK Computer/" +
				trackToken(i+1) + " - " + tt + ext,
			Size: size,
		})
	}

	return Candidate{
		ID:       id,
		Protocol: ProtocolDirect,
		Title:    "Radiohead - OK Computer",
		Artist:   "Radiohead",
		Files:    files,
		Health:   0.5,
	}
}

func allTitles() []string {
	return []string{
		"Airbag",
		"Paranoid Android",
		"Subterranean Homesick Alien",
		"Exit Music (For a Film)",
	}
}

// The headline behaviour: a well-matched FLAC beats a well-matched
// 128kbps MP3, but a mismatched FLAC loses to both.
func TestRankPrefersQualityAtEqualMatch(t *testing.T) {
	t.Parallel()

	dl := okComputer()

	flac := candidateFor("flac", allTitles(), ".flac", 30_000_000)
	mp3 := candidateFor("mp3", allTitles(), ".mp3", 3_000_000)

	for i := range mp3.Files {
		mp3.Files[i].Bitrate = 128
	}

	ranked := Rank(dl, []Candidate{mp3, flac}, nil, AutoDownloadPrefs{})

	if ranked[0].ID != "flac" {
		t.Fatalf("winner = %s, want flac", ranked[0].ID)
	}

	if ranked[0].Match.Overall < 0.9 {
		t.Errorf("flac match = %f, want high", ranked[0].Match.Overall)
	}

	if ranked[0].Quality.Overall <= ranked[1].Quality.Overall {
		t.Errorf(
			"flac quality %f should exceed mp3 %f",
			ranked[0].Quality.Overall, ranked[1].Quality.Overall,
		)
	}
}

func TestRankMatchDominatesQuality(t *testing.T) {
	t.Parallel()

	dl := okComputer()

	// Right album, poor bitrate.
	right := candidateFor("right", allTitles(), ".mp3", 2_000_000)
	for i := range right.Files {
		right.Files[i].Bitrate = 128
	}

	// Wrong album, pristine FLAC.
	wrong := candidateFor("wrong", []string{
		"Enter Sandman", "Sad But True", "Holier Than Thou", "The Unforgiven",
	}, ".flac", 30_000_000)
	wrong.Title = "Metallica - Metallica"
	wrong.Artist = "Metallica"

	for i := range wrong.Files {
		wrong.Files[i].Path = "Metallica - Metallica/" +
			trackToken(i+1) + " - x.flac"
	}

	ranked := Rank(dl, []Candidate{wrong, right}, nil, AutoDownloadPrefs{})

	if ranked[0].ID != "right" {
		t.Fatalf(
			"winner = %s (score %f vs %f), want the correctly matched album",
			ranked[0].ID, ranked[0].Score, ranked[1].Score,
		)
	}
}

func TestIncompleteCandidateScoresLower(t *testing.T) {
	t.Parallel()

	dl := okComputer()

	full := candidateFor("full", allTitles(), ".flac", 30_000_000)
	partial := candidateFor("partial", allTitles()[:2], ".flac", 30_000_000)

	ranked := Rank(dl, []Candidate{partial, full}, nil, AutoDownloadPrefs{})

	if ranked[0].ID != "full" {
		t.Fatalf("winner = %s, want full", ranked[0].ID)
	}

	if ranked[1].Match.Completeness >= ranked[0].Match.Completeness {
		t.Errorf(
			"partial completeness %f should be below full %f",
			ranked[1].Match.Completeness, ranked[0].Match.Completeness,
		)
	}
}

func TestMixedFormatIsPenalized(t *testing.T) {
	t.Parallel()

	dl := okComputer()

	clean := candidateFor("clean", allTitles(), ".flac", 30_000_000)

	mixed := candidateFor("mixed", allTitles(), ".flac", 30_000_000)
	mixed.Files[2].Path = "Radiohead - OK Computer/03 - x.mp3"
	mixed.Files[2].Format = FormatUnknown

	ranked := Rank(dl, []Candidate{mixed, clean}, nil, AutoDownloadPrefs{})

	var mixedScore QualityScore

	for _, c := range ranked {
		if c.ID == "mixed" {
			mixedScore = c.Quality
		}
	}

	if !mixedScore.Mixed {
		t.Error("mixed-format candidate not flagged")
	}

	if ranked[0].ID != "clean" {
		t.Errorf("winner = %s, want clean", ranked[0].ID)
	}
}

// Without an MBID there is no tracklist to be right about, so the match
// score must not look confident regardless of how good the strings are.
func TestUnanchoredMatchIsCapped(t *testing.T) {
	t.Parallel()

	dl := Download{Artist: "Radiohead", Album: "OK Computer"}
	c := candidateFor("c", allTitles(), ".flac", 30_000_000)

	scored := Score(dl, c, 50, AutoDownloadPrefs{})

	if scored.Match.Anchored {
		t.Error("free-text request reported as anchored")
	}

	if scored.Match.Overall > unanchoredCap {
		t.Errorf(
			"unanchored match = %f, want <= %f",
			scored.Match.Overall, unanchoredCap,
		)
	}
}

func TestAutoPickableRequiresAnchorAndTracklist(t *testing.T) {
	t.Parallel()

	dl := okComputer()
	best := Score(dl, candidateFor("a", allTitles(), ".flac", 30_000_000), 50, AutoDownloadPrefs{})

	t.Run("clear winner is auto-pickable", func(t *testing.T) {
		t.Parallel()

		weak := Score(
			dl,
			candidateFor("b", allTitles()[:2], ".mp3", 1_000_000),
			50,
			AutoDownloadPrefs{},
		)

		if !AutoPickable(dl, []Candidate{best, weak}, AutoDownloadPrefs{}) {
			t.Errorf(
				"want auto-pickable: match %f quality %f lead %f",
				best.Match.Overall, best.Quality.Overall, best.Score-weak.Score,
			)
		}
	})

	// Two identical copies are a spare, not an ambiguity. This
	// asserted the opposite while auto-pick required daylight over the
	// runner-up — a rule that made abundance the thing that stopped a
	// request being satisfied, which is backwards.
	t.Run("two equally good candidates still are", func(t *testing.T) {
		t.Parallel()

		twin := best
		twin.ID = "twin"

		if !AutoPickable(dl, []Candidate{best, twin}, AutoDownloadPrefs{}) {
			t.Error("identical good candidates must auto-pick")
		}
	})

	t.Run("free text is never auto-pickable", func(t *testing.T) {
		t.Parallel()

		free := Download{Artist: "Radiohead", Album: "OK Computer"}

		if AutoPickable(free, []Candidate{best}, AutoDownloadPrefs{}) {
			t.Error("unanchored request must not auto-pick")
		}
	})

	t.Run("empty list is not", func(t *testing.T) {
		t.Parallel()

		if AutoPickable(dl, nil, AutoDownloadPrefs{}) {
			t.Error("empty candidate list must not auto-pick")
		}
	})
}

func TestCompleteness(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		got      int
		want     int
		minScore float64
		maxScore float64
	}{
		{"exact", 10, 10, 1.0, 1.0},
		{"half missing", 5, 10, 0.49, 0.51},
		{"one bonus track", 11, 10, 0.95, 1.0},
		{"double", 20, 10, 0.74, 0.76},
		{"nothing", 0, 10, 0, 0},
		{"no expectation", 5, 0, 0.5, 0.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := completeness(tt.got, tt.want)
			if got < tt.minScore || got > tt.maxScore {
				t.Errorf(
					"completeness(%d, %d) = %f, want in [%f, %f]",
					tt.got, tt.want, got, tt.minScore, tt.maxScore,
				)
			}
		})
	}
}

func TestProviderPriorityBreaksTies(t *testing.T) {
	t.Parallel()

	dl := okComputer()

	a := candidateFor("a", allTitles(), ".flac", 30_000_000)
	a.ProviderID = 1

	b := candidateFor("b", allTitles(), ".flac", 30_000_000)
	b.ProviderID = 2

	priority := func(id int64) int {
		if id == 2 {
			return 90
		}

		return 10
	}

	ranked := Rank(dl, []Candidate{a, b}, priority, AutoDownloadPrefs{})

	if ranked[0].ID != "b" {
		t.Errorf("winner = %s, want b (higher provider priority)", ranked[0].ID)
	}
}

func TestAutoDownloadPrefsEligible(t *testing.T) {
	t.Parallel()

	flacCandidate := kbpsCandidate("c", ".flac", 900)
	mp3Candidate := kbpsCandidate("c", ".mp3", 128)

	tests := []struct {
		name  string
		prefs AutoDownloadPrefs
		c     Candidate
		want  bool
	}{
		{"zero value is permissive", AutoDownloadPrefs{}, flacCandidate, true},
		{
			"within the bitrate window",
			AutoDownloadPrefs{MinKbps: 320, MaxKbps: 1200},
			flacCandidate, true,
		},
		{
			"below the minimum bitrate",
			AutoDownloadPrefs{MinKbps: 500},
			mp3Candidate, false,
		},
		{
			"above the maximum bitrate",
			AutoDownloadPrefs{MaxKbps: 500},
			flacCandidate, false,
		},
		{
			// The ceiling is bytes, not a rate, and it is the guard
			// that still works when the bitrate cannot be worked out.
			"above the hard size ceiling",
			AutoDownloadPrefs{MaxSizeMB: 50},
			flacCandidate, false,
		},
		{
			"allowed format passes",
			AutoDownloadPrefs{AllowedFormats: []Format{FormatFLAC}},
			flacCandidate, true,
		},
		{
			"disallowed format rejected",
			AutoDownloadPrefs{AllowedFormats: []Format{FormatFLAC}},
			mp3Candidate, false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.prefs.eligible(tt.c, okComputerRuntime)
			if got != tt.want {
				t.Errorf("eligible() = %v, want %v", got, tt.want)
			}
		})
	}
}

// A release nobody knows the length of cannot be judged on bitrate, and
// the window must not become a silent embargo because MusicBrainz is
// missing a track length.  The size ceiling still applies — that is why
// it is a separate field.
func TestBitrateWindowPassesAnUnknownRuntime(t *testing.T) {
	t.Parallel()

	c := kbpsCandidate("c", ".mp3", 128)
	prefs := AutoDownloadPrefs{MinKbps: 900}

	if !prefs.eligible(c, 0) {
		t.Error("an unknown runtime must pass the bitrate window")
	}

	if prefs.eligible(c, okComputerRuntime) {
		t.Error("a known runtime must still be judged")
	}

	ceiling := AutoDownloadPrefs{MaxSizeMB: 1}
	if ceiling.eligible(c, 0) {
		t.Error("the size ceiling must apply even with no runtime")
	}
}

// Artwork is not part of the bitrate.  A folder carrying 30 MB of
// scans would otherwise read as a better rip than the same music
// without them, which is backwards.
func TestBitrateIgnoresNonAudioFiles(t *testing.T) {
	t.Parallel()

	c := kbpsCandidate("c", ".mp3", 320)
	bare := candidateKbps(c, okComputerRuntime)

	c.Files = append(c.Files, CandidateFile{
		Path: "Radiohead - OK Computer/cover.jpg",
		Size: 30 << 20,
	})
	c.Files = AnnotateFiles(c.Files)

	if got := candidateKbps(c, okComputerRuntime); got != bare {
		t.Errorf("bitrate with artwork = %f, want %f", got, bare)
	}
}

// Where no runtime is known, a stated per-file bitrate is better than
// no answer at all.
func TestBitrateFallsBackToTheStatedRate(t *testing.T) {
	t.Parallel()

	c := candidateFor("c", allTitles(), ".mp3", 3_000_000)
	for i := range c.Files {
		c.Files[i].Bitrate = 192
	}

	c.Files = AnnotateFiles(c.Files)

	if got := candidateKbps(c, 0); got != 192 {
		t.Errorf("stated bitrate = %f, want 192", got)
	}
}

func TestAutoDownloadPrefsFilter(t *testing.T) {
	t.Parallel()

	lossy := kbpsCandidate("lossy", ".mp3", 128)
	lossless := kbpsCandidate("lossless", ".flac", 900)

	prefs := AutoDownloadPrefs{MinKbps: 500}

	filtered := prefs.filter(
		[]Candidate{lossy, lossless}, okComputerRuntime,
	)

	if len(filtered) != 1 || filtered[0].ID != "lossless" {
		t.Errorf("filter() = %v, want only the in-window candidate", filtered)
	}
}

func TestAutoDownloadPrefsBitrateFit(t *testing.T) {
	t.Parallel()

	const neutral = 0.5

	tests := []struct {
		name  string
		prefs AutoDownloadPrefs
		c     Candidate
		want  float64
	}{
		{
			"no preference is neutral",
			AutoDownloadPrefs{},
			kbpsCandidate("c", ".flac", 900), neutral,
		},
		{
			"exact match scores 1",
			AutoDownloadPrefs{PreferredKbps: 320},
			kbpsCandidate("c", ".mp3", 320), 1.0,
		},
		{
			// The floor is neutral, not zero: this term carries 0.40
			// of the quality score once a preference is set, and a
			// span to zero would let "I like 320" quietly disqualify
			// every FLAC from auto-pick.
			"double the preferred rate falls to the neutral floor",
			AutoDownloadPrefs{PreferredKbps: 320},
			kbpsCandidate("c", ".flac", 640), neutral,
		},
		{
			"half the preferred rate falls to the neutral floor",
			AutoDownloadPrefs{PreferredKbps: 320},
			kbpsCandidate("c", ".mp3", 160), neutral,
		},
		{
			"an unknowable rate is neutral",
			AutoDownloadPrefs{PreferredKbps: 320},
			kbpsCandidate("c", ".mp3", 320), neutral,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// The last case deliberately withholds the runtime.
			runtime := int64(okComputerRuntime)
			if tt.name == "an unknowable rate is neutral" {
				runtime = 0
			}

			if got := tt.prefs.bitrateFit(tt.c, runtime); got != tt.want {
				t.Errorf("bitrateFit() = %f, want %f", got, tt.want)
			}
		})
	}
}

// An otherwise-perfect candidate must not auto-pick when it falls
// outside the configured guardrails: they apply before the match and
// quality checks, not as one more input averaged into them.
func TestAutoPickableRejectsCandidateOutsideTheGuardrails(t *testing.T) {
	t.Parallel()

	dl := okComputer()
	best := Score(dl, kbpsCandidate("a", ".flac", 900), 50, AutoDownloadPrefs{})

	if !AutoPickable(dl, []Candidate{best}, AutoDownloadPrefs{}) {
		t.Fatal("expected this candidate to be auto-pickable with no guardrails")
	}

	if AutoPickable(dl, []Candidate{best}, AutoDownloadPrefs{MaxKbps: 320}) {
		t.Error("candidate above the bitrate window must not auto-pick")
	}

	if AutoPickable(dl, []Candidate{best}, AutoDownloadPrefs{MaxSizeMB: 1}) {
		t.Error("candidate above the size ceiling must not auto-pick")
	}
}

// The refusal has to name the gate that refused.
//
// Before AutoPickVeto, every one of these came back as the same
// sentence built from `ranked[0]` — the best candidate before the size
// and format guardrails — so a request refused because the user's size
// window excluded every copy reported a match and a quality that both
// cleared their thresholds.  A refusal quoting numbers that pass is
// what made the matcher look broken from outside.
func TestAutoPickVetoNamesTheGate(t *testing.T) {
	t.Parallel()

	dl := okComputer()
	best := Score(
		dl,
		candidateFor("a", allTitles(), ".flac", 30_000_000),
		50,
		AutoDownloadPrefs{},
	)

	// candidateFor sizes the files and leaves TotalSize at 0, which is
	// what the guardrails read.
	sized := func(c Candidate, total int64) Candidate {
		c.TotalSize = total

		return c
	}

	tests := []struct {
		name    string
		dl      Download
		ranked  []Candidate
		prefs   AutoDownloadPrefs
		wantSub string
	}{
		{
			name:    "nothing found",
			dl:      dl,
			ranked:  nil,
			wantSub: "nothing found",
		},
		{
			name:    "free text",
			dl:      Download{Artist: "Radiohead", Album: "OK Computer"},
			ranked:  []Candidate{best},
			wantSub: "free text",
		},
		{
			name: "no tracklist behind the anchor",
			dl: Download{
				ReleaseMBID: "mbid-ok-computer",
				Artist:      "Radiohead",
				Album:       "OK Computer",
			},
			ranked:  []Candidate{best},
			wantSub: "no tracklist",
		},
		{
			// The candidate is 120 MB and the window tops out at 1 MB:
			// the old message reported its match and quality instead.
			name:    "outside the size window",
			dl:      dl,
			ranked:  []Candidate{sized(best, 120<<20)},
			prefs:   AutoDownloadPrefs{MaxSizeMB: 1},
			wantSub: "bitrate, size or format limits",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := AutoPickVeto(tt.dl, tt.ranked, tt.prefs)
			if !strings.Contains(got, tt.wantSub) {
				t.Errorf("veto = %q, want it to mention %q", got, tt.wantSub)
			}
		})
	}
}

// A clear winner has no veto at all — the sentence is empty, which is
// what AutoPickable reads.
func TestAutoPickVetoIsEmptyForAClearWinner(t *testing.T) {
	t.Parallel()

	dl := okComputer()
	best := Score(
		dl,
		candidateFor("a", allTitles(), ".flac", 30_000_000),
		50,
		AutoDownloadPrefs{},
	)
	weak := Score(
		dl,
		candidateFor("b", allTitles()[:2], ".mp3", 1_000_000),
		50,
		AutoDownloadPrefs{},
	)

	if got := AutoPickVeto(dl, []Candidate{best, weak}, AutoDownloadPrefs{}); got != "" {
		t.Errorf("veto = %q, want none", got)
	}
}

// With several candidates that all clear the bar, the preferred
// bitrate decides which one is taken.
//
// This is what replaced the daylight requirement.  Auto-pick no longer
// refuses when the field is close; it takes the copy nearest the shape
// the user asked for, which is the question they actually answered in
// Settings.
func TestPreferredBitrateBreaksTheTie(t *testing.T) {
	t.Parallel()

	dl := okComputer()
	prefs := AutoDownloadPrefs{PreferredKbps: 320}

	// Same album, same completeness, same health, same provider — the
	// only difference between them is the rate.
	lossless := kbpsCandidate("lossless", ".flac", 900)
	perfect := kbpsCandidate("perfect", ".mp3", 320)

	ranked := Rank(
		dl, []Candidate{lossless, perfect}, nil, prefs,
	)

	if ranked[0].ID != "perfect" {
		t.Errorf(
			"winner = %q (fit %f) over %q (fit %f), want the 320 kbps copy",
			ranked[0].ID, ranked[0].Quality.BitrateFit,
			ranked[1].ID, ranked[1].Quality.BitrateFit,
		)
	}

	if AutoPickVeto(dl, ranked, prefs) != "" {
		t.Error("a close field must still auto-pick")
	}
}

// With no preference set, nothing changes: BitrateFit is the same
// neutral value for every candidate and the older tie-breaks decide.
func TestNoPreferredBitrateLeavesRankingAlone(t *testing.T) {
	t.Parallel()

	dl := okComputer()

	lossless := kbpsCandidate("lossless", ".flac", 900)
	lossy := kbpsCandidate("lossy", ".mp3", 320)

	ranked := Rank(
		dl, []Candidate{lossy, lossless}, nil, AutoDownloadPrefs{},
	)

	if ranked[0].ID != "lossless" {
		t.Errorf(
			"winner = %q, want the lossless copy on format alone",
			ranked[0].ID,
		)
	}
}

// A preferred bitrate promotes the copy that matches it and must never
// disqualify the ones that do not.  It carries 0.40 of the quality
// score, so a fit spanning down to zero would put a perfectly good FLAC
// under minQuality and out of auto-pick — turning a preference into a
// prohibition without saying so.  MinKbps and MaxKbps are how a user
// says that on purpose.
func TestAPreferredBitrateNeverDisqualifies(t *testing.T) {
	t.Parallel()

	dl := okComputer()
	far := AutoDownloadPrefs{PreferredKbps: 128}

	lossless := Score(dl, kbpsCandidate("flac", ".flac", 900), 50, far)

	if lossless.Quality.Overall < minQuality {
		t.Errorf(
			"quality = %f under a far-off preference, want >= %f",
			lossless.Quality.Overall, minQuality,
		)
	}

	if veto := AutoPickVeto(dl, []Candidate{lossless}, far); veto != "" {
		t.Errorf("a far-off preference vetoed the candidate: %s", veto)
	}
}
