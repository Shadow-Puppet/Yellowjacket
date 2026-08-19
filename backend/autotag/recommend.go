package autotag

// Recommendation is a qualitative confidence tier for a group's
// ranked candidates — the piece a raw score can't express on its
// own.  Modeled on beets' Recommendation enum: the tier starts from
// the top candidate's absolute score and is then CAPPED by defects
// (ambiguity with a different release group, missing/unmatched
// tracks, thin evidence).  Auto-accept (plan 011) should require
// RecommendationStrong; the review UI can badge the rest.
type Recommendation string

// Recommendation tiers, weakest to strongest.
const (
	RecommendationNone   Recommendation = "none"
	RecommendationLow    Recommendation = "low"
	RecommendationMedium Recommendation = "medium"
	RecommendationStrong Recommendation = "strong"
)

// ConfidentTier is the tier at which this package considers a match
// good enough to act on without being asked to look.
//
// It exists as a name rather than as `== RecommendationStrong` at
// each call site because two features read it and they must not
// disagree about what "high confidence" means: the album page tells
// the user unprompted that the autotagger has a match (#28), and
// strict auto-accept will rewrite the files without asking (#90).
// A page that says "we are sure" about something the auto-accept
// pass would decline is the app contradicting itself.
//
// What the two do *not* share is everything else. Surfacing a match
// is a suggestion with a confirm dialog behind it; auto-accept is an
// irreversible on-disk rewrite, and #90 gates it on further
// conditions this tier cannot express — exact track count, every
// title matching, lengths within a couple of seconds, no cover
// replacement, no MBID conflict. So this is the floor both stand on,
// not the whole of either test.
const ConfidentTier = RecommendationStrong

// Confident reports whether a tier clears ConfidentTier.
//
// A comparison rather than an equality, so adding a tier above
// "strong" later does not silently stop qualifying.
func Confident(r Recommendation) bool {
	return recommendationRank(r) >= recommendationRank(ConfidentTier)
}

const (
	// Absolute score tiers.
	strongScoreThresh = 0.90
	mediumScoreThresh = 0.75

	// A runner-up from a DIFFERENT release group within this margin
	// of the top score makes the match ambiguous — two genuinely
	// different albums both fit, so a human should look.  Editions
	// of the same release group are expected to score nearly
	// identically and never count as ambiguity.
	ambiguityMargin = 0.05
)

// Recommend derives the confidence tier for a ranked candidate
// list.  candidates must already be sorted best-first (the shape
// RankCandidates returns).
func Recommend(g Group, candidates []Candidate) Recommendation {
	if len(candidates) == 0 {
		return RecommendationNone
	}

	top := candidates[0]

	var rec Recommendation

	switch {
	case top.Score >= strongScoreThresh:
		rec = RecommendationStrong
	case top.Score >= mediumScoreThresh:
		rec = RecommendationMedium
	default:
		return RecommendationLow
	}

	// Cap: a different release group scoring within the ambiguity
	// margin means the score alone can't pick between two albums.
	if rivalWithinMargin(top, candidates[1:]) {
		rec = minRecommendation(rec, RecommendationMedium)
	}

	// Cap: missing or unmatched tracks mean the alignment itself is
	// incomplete, however good the matched tracks look (beets caps
	// these penalties at "medium" the same way).  A synthetic
	// (tag-clustered) group is, by construction, a subset of a
	// bigger folder, so AlignmentMissing (the candidate has tracks
	// the group doesn't) is the expected shape rather than a defect
	// and doesn't cap the recommendation.  AlignmentUnmatched (the
	// group has a track the candidate doesn't) is still a real
	// discrepancy regardless of source.
	for _, a := range top.Alignments {
		if a.Status == AlignmentUnmatched ||
			(a.Status == AlignmentMissing && !g.Synthetic) {
			rec = minRecommendation(rec, RecommendationMedium)

			break
		}
	}

	// Cap: tiny folders can't corroborate a match strongly enough
	// to act on without review, whatever the arithmetic says.
	if len(g.Tracks) < evidenceFullTracks {
		rec = minRecommendation(rec, RecommendationMedium)
	}

	return rec
}

// rivalWithinMargin reports whether any candidate from a different
// release group scores within ambiguityMargin of the top candidate.
func rivalWithinMargin(top Candidate, rest []Candidate) bool {
	for _, c := range rest {
		if top.Score-c.Score > ambiguityMargin {
			// Sorted descending: everything further is farther away.
			return false
		}

		if !sameReleaseGroup(top, c) {
			return true
		}
	}

	return false
}

// sameReleaseGroup reports whether two candidates belong to the
// same release group — by MBID when both carry one, by normalized
// title + artist-credit otherwise (local candidates may lack RG
// MBIDs).
func sameReleaseGroup(a, b Candidate) bool {
	if a.ReleaseGroupMBID != "" && b.ReleaseGroupMBID != "" {
		return a.ReleaseGroupMBID == b.ReleaseGroupMBID
	}

	return Normalize(a.Title) == Normalize(b.Title) &&
		Normalize(a.ArtistCredit) == Normalize(b.ArtistCredit)
}

// recommendationRank orders tiers for min-comparison.
func recommendationRank(r Recommendation) int {
	switch r {
	case RecommendationNone:
		return 0
	case RecommendationLow:
		return 1
	case RecommendationMedium:
		return 2
	case RecommendationStrong:
		return 3
	default:
		return 0
	}
}

// minRecommendation returns the weaker of two tiers.
func minRecommendation(a, b Recommendation) Recommendation {
	if recommendationRank(a) <= recommendationRank(b) {
		return a
	}

	return b
}
