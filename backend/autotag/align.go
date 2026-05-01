package autotag

// alignTitleFloor is the minimum title similarity required to
// pair a local file with a candidate track at all.  Below this,
// even the best available pair is treated as no pair: the local
// stays "unmatched" and the candidate slot stays "missing".  Set
// well below titleReject so a local file with the wrong track
// number but mostly-correct title still pairs (and surfaces the
// number mismatch as a diff), while a totally unrelated file that
// happens to share a track number stays in its own group.
const alignTitleFloor = 0.30

// AlignTracks pairs local tracks with candidate tracks using a
// greedy best-score algorithm: repeatedly pick the (local, cand)
// pair with the highest trackDistance that hasn't already been
// claimed.  Not optimal (Hungarian would be), but good enough for
// the small cardinalities we see (album tracks, ~10-50) and much
// simpler.
//
// Pairs whose title similarity is below alignTitleFloor are
// rejected: the local stays "unmatched" and the candidate slot
// surfaces as "missing".  This lets a wrong-track-number-but-
// matching-title file pair correctly while keeping a totally
// unrelated file from being force-paired into a slot.
//
// Returns one TrackAlignment per local track (status = matched,
// mismatched, or unmatched) plus additional missing alignments
// for candidate tracks with no local file.  The caller sums Score
// fields to get a release-level score.
func AlignTracks(locals []LocalTrack, cands []CandidateTrack) []TrackAlignment {
	type pair struct {
		li    int
		ci    int
		score float64
		title float64
	}

	// Score every (local, cand) combination.
	pairs := make([]pair, 0, len(locals)*len(cands))

	for li, local := range locals {
		for ci, cand := range cands {
			pairs = append(pairs, pair{
				li:    li,
				ci:    ci,
				score: trackDistance(local, cand),
				title: titleSimilarity(local.Title, cand.Title),
			})
		}
	}

	// Sort descending by score — pick best pairs first.  Insertion
	// sort keeps the dependency surface zero; N^2 here is fine for
	// album-sized inputs.
	for i := 1; i < len(pairs); i++ {
		for j := i; j > 0 && pairs[j].score > pairs[j-1].score; j-- {
			pairs[j], pairs[j-1] = pairs[j-1], pairs[j]
		}
	}

	localUsed := make([]bool, len(locals))
	candUsed := make([]bool, len(cands))
	alignments := make([]TrackAlignment, len(locals))

	localMatched := 0

	for _, p := range pairs {
		if localMatched == len(locals) {
			break
		}

		if localUsed[p.li] || candUsed[p.ci] {
			continue
		}

		// Below the floor: this is the best pair available for both
		// of these slots, but the title similarity is so low that
		// pairing them would just be noise.  Leave both unclaimed —
		// they'll fall through to the unmatched/missing fixups below.
		if p.title < alignTitleFloor {
			continue
		}

		localUsed[p.li] = true
		candUsed[p.ci] = true
		localMatched++

		l := locals[p.li]
		c := cands[p.ci]

		status := AlignmentMatched
		if p.title < titleReject {
			status = AlignmentMismatched
		}

		delta := l.LengthMillis - c.LengthMillis
		if delta < 0 {
			delta = -delta
		}

		alignments[p.li] = TrackAlignment{
			LocalIndex:          p.li,
			LocalTitle:          l.Title,
			LocalLengthMillis:   l.LengthMillis,
			CandidatePosition:   c.Position,
			CandidateDiscNumber: c.DiscNumber,
			CandidateTitle:      c.Title,
			CandidateMBID:       c.MBID,
			CandidateLength:     c.LengthMillis,
			TitleScore:          p.title,
			LengthDeltaMs:       delta,
			TrackNumberOK:       l.TrackNumber > 0 && l.TrackNumber == c.Position,
			Status:              status,
		}
	}

	// Local tracks left unclaimed → folder has them, candidate
	// doesn't (or pairing was rejected by the floor).
	for li, used := range localUsed {
		if used {
			continue
		}

		l := locals[li]
		alignments[li] = TrackAlignment{
			LocalIndex:        li,
			LocalTitle:        l.Title,
			LocalLengthMillis: l.LengthMillis,
			Status:            AlignmentUnmatched,
		}
	}

	// Candidate tracks left unclaimed → candidate has them, folder
	// doesn't.
	for ci, used := range candUsed {
		if used {
			continue
		}

		c := cands[ci]
		alignments = append(alignments, TrackAlignment{
			LocalIndex:          -1,
			CandidatePosition:   c.Position,
			CandidateDiscNumber: c.DiscNumber,
			CandidateTitle:      c.Title,
			CandidateMBID:       c.MBID,
			CandidateLength:     c.LengthMillis,
			Status:              AlignmentMissing,
		})
	}

	return alignments
}
