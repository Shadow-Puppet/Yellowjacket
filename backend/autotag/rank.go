package autotag

import (
	"sort"
	"strconv"
	"strings"
)

// Release-level scoring weights.  Aggregate track score is the
// dominant signal — the release-level signals are tie-breakers
// when the track alignment is roughly comparable.
const (
	weightTrackAggregate  = 0.70
	weightTrackCountMatch = 0.15
	weightReleaseMeta     = 0.15 // Official + country averaged

	// Country preference: a very mild nudge toward releases from
	// the user's locale.  Will become a config option in 012.
	preferredCountry = "US"
)

// ScoreCandidate fills in c.Alignments, c.Score, c.Breakdown, and
// c.TrackCount for a single candidate against the given local
// tracks.  The returned Candidate is safe to copy — no shared
// state with the caller's slice.
func ScoreCandidate(local []LocalTrack, c Candidate, localTrackCount int) Candidate {
	c.Alignments = AlignTracks(local, c.Tracks)

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
		titleSum += a.TitleScore

		l := local[a.LocalIndex]
		lengthSum += lengthScore(l.LengthMillis, a.CandidateLength)
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

	trackCountScore := trackCountMatch(len(c.Tracks), localTrackCount)
	// Release-meta is just official-status + country preference,
	// averaged.  We used to mix in a year bonus too, but that
	// compared candidate years against time.Now() — penalising
	// every album that wasn't from this year, regardless of how
	// well it matched the local files.  See git history.
	const metaTerms = 2.0

	meta := (officialBonus(c.Status) + countryBonus(c.Country)) / metaTerms

	c.Score = trackAgg*weightTrackAggregate +
		trackCountScore*weightTrackCountMatch +
		meta*weightReleaseMeta

	c.Breakdown = ScoreBreakdown{
		TitleAvg:      titleAvg,
		LengthAvg:     lengthAvg,
		TrackCountFit: trackCountScore,
		ReleaseMeta:   meta,
	}
	c.TrackCount = len(c.Tracks)

	return c
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

	larger := a
	if b > larger {
		larger = b
	}

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

	switch strings.ToLower(status) {
	case "official":
		return 1.0
	case "":
		return partial
	default:
		return partial
	}
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

// RankCandidates scores each candidate against the local tracks
// and returns a new slice sorted descending by score.  Input slice
// is not modified.
func RankCandidates(local []LocalTrack, candidates []Candidate) []Candidate {
	scored := make([]Candidate, 0, len(candidates))
	for _, c := range candidates {
		scored = append(scored, ScoreCandidate(local, c, len(local)))
	}

	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})

	return scored
}
