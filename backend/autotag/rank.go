package autotag

import (
	"sort"
	"strconv"
	"strings"
)

// Release-level scoring weights.  Aggregate track score is the
// dominant signal — the release-level signals are tie-breakers
// when the track alignment is roughly comparable.  Weights sum to
// 1.0 so a perfect candidate scores 1.0 before the evidence scale.
const (
	weightTrackAggregate  = 0.55
	weightArtist          = 0.12 // album-artist vs candidate artist-credit
	weightAlbumTitle      = 0.10 // folder album name vs candidate release title
	weightTrackCountMatch = 0.13
	weightReleaseMeta     = 0.10 // official + country + RG type, averaged

	// Country preference: a very mild nudge toward releases from
	// the user's locale.  Will become a config option in 012.
	preferredCountry = "US"

	// Evidence scaling: a folder with very few tracks offers little
	// corroborating signal, so even a perfect title+length match on
	// a single track is inherently less trustworthy than the same
	// match across a full album.  The final release score is scaled
	// by evidenceFactor(localTrackCount): folders at or above
	// evidenceFullTracks are unscaled; smaller folders are pulled
	// toward evidenceFloor.  This is the "harsher on singletons"
	// lever — a single can never present as a near-certain match on
	// its own, which is also why 012 keeps singletons out of
	// auto-accept entirely.
	evidenceFloor      = 0.85
	evidenceFullTracks = 3
)

// vaNames are artist strings that signal "various artists" — used
// both to detect VA-likely folders and to recognize VA candidate
// credits.  Mirrors beets' VA_ARTISTS.
var vaNames = map[string]bool{
	"various artists": true,
	"various":         true,
	"va":              true,
	"v a":             true, // "V.A." after Normalize
	"unknown":         true,
}

// isVAName reports whether an artist string reads as "various
// artists".  Empty strings are NOT VA — they're unknown, which the
// artist term already treats as neutral.
func isVAName(s string) bool {
	return vaNames[Normalize(s)]
}

// vaLikely reports whether a group is probably a various-artists
// compilation: the album-artist tag says so outright, or the
// per-track artists have no consensus (≥2 distinct values, or none
// at all).  Mirrors beets' va_likely heuristic.
func vaLikely(g Group) bool {
	if isVAName(g.AlbumArtist) {
		return true
	}

	if g.AlbumArtist != "" {
		return false
	}

	distinct := make(map[string]bool, 2) //nolint:mnd

	for _, t := range g.Tracks {
		if t.Artist == "" {
			continue
		}

		distinct[Normalize(t.Artist)] = true
	}

	return len(distinct) != 1
}

// ScoreCandidate fills in c.Alignments, c.Score, c.Breakdown, and
// c.TrackCount for a single candidate against the given group.
// The returned Candidate is safe to copy — no shared state with
// the caller's slice.
func ScoreCandidate(g Group, c Candidate) Candidate {
	local := g.Tracks

	// When the group is one disc of a multi-disc candidate, align
	// and count against that disc only — a "disc 1 of 2" folder is
	// complete for its disc, not half an album.
	targets := alignmentTargets(local, c.Tracks)

	c.Alignments = AlignTracks(local, targets)

	var (
		titleSum  float64
		lengthSum float64
		counted   int
	)

	for _, a := range c.Alignments {
		if a.Status != AlignmentMatched && a.Status != AlignmentMismatched {
			continue
		}

		counted++

		// A recording-MBID lock is identity, not similarity: the
		// title may be garbled in the local tag, but the track IS
		// the candidate's track.  Count it as a perfect title so a
		// confirmed match isn't dragged down by its own typos (the
		// UI still shows the textual diff).
		if a.IDMatch {
			titleSum++
		} else {
			titleSum += a.TitleScore
		}

		lengthSum += a.LengthScore
	}

	titleAvg, lengthAvg := 0.0, 0.0
	if counted > 0 {
		titleAvg = titleSum / float64(counted)
		lengthAvg = lengthSum / float64(counted)
	}

	// Aggregate track score: weighted title + length (renormalized
	// so a perfect match scales to 1.0 regardless of the absolute
	// weights), scaled by how many of our local tracks actually
	// matched — extra or missing tracks punish proportionally.
	coverage := 0.0
	if len(local) > 0 {
		coverage = float64(counted) / float64(len(local))
	}

	const trackWeightSum = weightTitle + weightLength

	trackAgg := ((titleAvg*weightTitle + lengthAvg*weightLength) / trackWeightSum) * coverage

	trackCountScore := trackCountMatch(len(targets), len(local))

	// Artist fit: compare the folder's artist against the
	// candidate's release artist-credit.  This is a SOFT signal, not
	// a gate — user artist tags are often slightly wrong (misspelled,
	// "&" vs "and", missing "feat."), so an almost-right artist still
	// matches well while a completely different artist is penalised.
	// Critically, artist is otherwise only a *search* filter (see
	// buildMBQueryCascade), and that cascade drops the artist clause
	// on its looser steps — so without this term a same-title,
	// different-artist release scores as if the artist matched.
	artistFit := artistCreditFit(groupArtist(g), c.ArtistCredit)

	// Album-title fit: the same soft-signal contract for the release
	// title.  Without it, a compilation containing the same
	// recordings scores as if it WERE the album ("Greatest Hits" vs
	// the studio album with an identical tracklist).
	albumFit := albumTitleFit(g.AlbumName, c.Title)

	// Release-meta: official-status + country preference + release-
	// group type, averaged.  All mild tie-breakers.  (We used to mix
	// in a year bonus too, but that compared candidate years against
	// time.Now() — see git history.)
	const metaTerms = 3.0

	meta := (officialBonus(c.Status) + countryBonus(c.Country) + rgTypeBonus(c.PrimaryType)) /
		metaTerms

	// Evidence scaling applies only to MusicBrainz candidates.  A
	// local candidate is the *same* release-group already tagged with
	// MBIDs in another library — its confidence comes from that
	// confirmed tagging, not from thin per-track heuristics, so a
	// small-folder local match stays fully trusted (and keeps
	// clearing the localSufficient MB-skip short-circuit).  MB
	// matches, by contrast, are fuzzy search results where a
	// single-track folder genuinely offers little corroboration.
	evidence := 1.0
	if c.Source == SourceMusicBrainz {
		evidence = evidenceFactor(len(local))
	}

	c.Score = (trackAgg*weightTrackAggregate +
		artistFit*weightArtist +
		albumFit*weightAlbumTitle +
		trackCountScore*weightTrackCountMatch +
		meta*weightReleaseMeta) * evidence

	c.Breakdown = ScoreBreakdown{
		TitleAvg:      titleAvg,
		LengthAvg:     lengthAvg,
		ArtistFit:     artistFit,
		AlbumFit:      albumFit,
		TrackCountFit: trackCountScore,
		ReleaseMeta:   meta,
		Evidence:      evidence,
	}
	c.TrackCount = len(c.Tracks)

	return c
}

// alignmentTargets returns the candidate tracks the local group
// should be aligned against.  When every local track sits on the
// same disc D and the candidate spans multiple discs including D,
// only disc D's tracks are targets — the group key is per-disc, so
// a single-disc folder must not be penalised for "missing" the
// candidate's other discs.
func alignmentTargets(local []LocalTrack, cands []CandidateTrack) []CandidateTrack {
	disc := uniformDisc(local)
	if disc == 0 {
		return cands
	}

	var (
		onDisc     int
		multiDiscs bool
	)

	for _, c := range cands {
		if c.DiscNumber == disc {
			onDisc++
		} else if c.DiscNumber > 0 {
			multiDiscs = true
		}
	}

	if !multiDiscs || onDisc == 0 {
		return cands
	}

	out := make([]CandidateTrack, 0, onDisc)

	for _, c := range cands {
		if c.DiscNumber == disc {
			out = append(out, c)
		}
	}

	return out
}

// uniformDisc returns the disc number shared by every local track,
// or 0 when discs are mixed or unknown.
func uniformDisc(local []LocalTrack) int {
	disc := 0

	for _, t := range local {
		switch {
		case t.DiscNumber <= 0:
			return 0
		case disc == 0:
			disc = t.DiscNumber
		case t.DiscNumber != disc:
			return 0
		}
	}

	return disc
}

// groupArtist returns the artist string to compare candidates
// against: the tagging item's album-artist when it's a real name,
// otherwise the most common per-track artist.  Returns "" when
// nothing is known (neutral, no penalty).
func groupArtist(g Group) string {
	if g.AlbumArtist != "" && !isVAName(g.AlbumArtist) {
		return g.AlbumArtist
	}

	return dominantArtist(g.Tracks)
}

// dominantArtist returns the most common non-empty per-track artist
// in a local group.  Ties resolve to the first-seen value so the
// result is deterministic.  Returns "" when no track has an artist,
// which artistCreditFit treats as "unknown, no penalty".
func dominantArtist(local []LocalTrack) string {
	counts := make(map[string]int, len(local))

	var (
		best      string
		bestCount int
	)

	for _, t := range local {
		if t.Artist == "" {
			continue
		}

		counts[t.Artist]++
		if counts[t.Artist] > bestCount {
			best = t.Artist
			bestCount = counts[t.Artist]
		}
	}

	return best
}

// artistCreditFit scores how well a folder's artist matches a
// candidate's release artist-credit, in [0, 1].  Returns 1.0 (no
// penalty) when either side is unknown or reads as "various
// artists": absence of artist data must not push a candidate down,
// and VA credits are placeholders, not disagreements.  Reuses the
// edit-distance similarity so near-right artists stay high.
func artistCreditFit(localArtist, candidateArtist string) float64 {
	if localArtist == "" || candidateArtist == "" {
		return 1.0
	}

	if isVAName(localArtist) || isVAName(candidateArtist) {
		return 1.0
	}

	return titleSimilarity(localArtist, candidateArtist)
}

// albumTitleFit scores how well the folder's album name matches the
// candidate's release title, in [0, 1].  Neutral (1.0) when either
// side is unknown — same soft-signal contract as artistCreditFit.
func albumTitleFit(albumName, candidateTitle string) float64 {
	if albumName == "" || candidateTitle == "" {
		return 1.0
	}

	return titleSimilarity(albumName, candidateTitle)
}

// evidenceFactor scales the release score down when a folder has too
// few tracks to corroborate the match.  Folders at or above
// evidenceFullTracks are unscaled (1.0); a single-track folder is
// pulled to evidenceFloor; two tracks land halfway.  See the
// evidence-scaling note on the weight constants.
func evidenceFactor(localTrackCount int) float64 {
	if localTrackCount >= evidenceFullTracks {
		return 1.0
	}

	if localTrackCount <= 1 {
		return evidenceFloor
	}

	span := float64(localTrackCount-1) / float64(evidenceFullTracks-1)

	return evidenceFloor + (1.0-evidenceFloor)*span
}

// trackCountMatch returns 1.0 when equal, 0.0 when off by >= 50%,
// linear between.
func trackCountMatch(a, b int) float64 {
	if a == 0 && b == 0 {
		return 1.0
	}

	if a == 0 || b == 0 {
		return 0.0
	}

	diff := a - b
	if diff < 0 {
		diff = -diff
	}

	larger := max(a, b)

	frac := float64(diff) / float64(larger)

	const halfwayPenalty = 0.5
	if frac >= halfwayPenalty {
		return 0.0
	}

	return 1.0 - frac/halfwayPenalty
}

// officialBonus returns 1.0 for Official releases, 0.5 for others
// (Promotion, Bootleg, ...), 0.5 when unknown.
func officialBonus(status string) float64 {
	const partial = 0.5

	if strings.EqualFold(status, "official") {
		return 1.0
	}

	return partial
}

// countryBonus gives a mild nudge toward releases from the
// preferred country.  Neutral (0.5) when country is absent.
func countryBonus(country string) float64 {
	const (
		neutral = 0.5
		hit     = 1.0
	)

	if country == "" {
		return neutral
	}

	if strings.EqualFold(country, preferredCountry) {
		return hit
	}

	return neutral
}

// rgTypeBonus nudges toward studio albums over compilations and
// live releases when the track evidence is otherwise comparable —
// Picard weights release type heavily for the same reason.  The
// nudge is mild: a genuine single folder still matches its Single
// release because track count and alignment dominate.  Unknown
// types (including all local candidates) sit near the top so the
// term only separates candidates we positively know differ.
func rgTypeBonus(primaryType string) float64 {
	switch strings.ToLower(primaryType) {
	case "album":
		return 1.0
	case "ep":
		return 0.9
	case "single":
		return 0.85
	case "":
		return 0.85
	case "soundtrack":
		return 0.7
	case "compilation", "live":
		return 0.6
	default:
		return 0.7
	}
}

// parseYear pulls the first 4-digit year out of date strings like
// "2009", "2009-05-18", "".
func parseYear(date string) int {
	if len(date) < 4 { //nolint:mnd
		return 0
	}

	y, err := strconv.Atoi(date[:4])
	if err != nil {
		return 0
	}

	return y
}

// RankCandidates scores each candidate against the group and
// returns a new slice sorted descending by score.  Input slice is
// not modified.
func RankCandidates(g Group, candidates []Candidate) []Candidate {
	scored := make([]Candidate, 0, len(candidates))
	for _, c := range candidates {
		scored = append(scored, ScoreCandidate(g, c))
	}

	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})

	return scored
}
