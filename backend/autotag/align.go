package autotag

import "slices"

// alignTitleFloor is the minimum title similarity required to
// pair a local file with a candidate track at all.  Below this,
// even the best available pair is treated as no pair: the local
// stays "unmatched" and the candidate slot stays "missing".  Set
// well below titleReject so a local file with the wrong track
// number but mostly-correct title still pairs (and surfaces the
// number mismatch as a diff), while a totally unrelated file that
// happens to share a track number stays in its own group.
const alignTitleFloor = 0.30

// alignPair carries the per-combination scores AlignTracks computes
// once per (local, candidate) pair.
type alignPair struct {
	li     int
	ci     int
	score  float64
	title  float64
	length float64
}

// AlignTracks pairs local tracks with candidate tracks in two
// passes:
//
//  1. Recording-MBID locks: a local track whose RecordingMBID equals
//     a candidate track's MBID is the same recording by definition —
//     it pairs unconditionally, regardless of how the titles compare.
//  2. Greedy best-score: repeatedly pick the remaining (local, cand)
//     pair with the highest track score.  Not optimal (Hungarian
//     would be), but good enough for the small cardinalities we see
//     (album tracks, ~10-50) and much simpler.
//
// Greedy pairs whose title similarity is below alignTitleFloor are
// rejected: the local stays "unmatched" and the candidate slot
// surfaces as "missing".  This lets a wrong-track-number-but-
// matching-title file pair correctly while keeping a totally
// unrelated file from being force-paired into a slot.
//
// Returns one TrackAlignment per local track (status = matched,
// mismatched, or unmatched) plus additional missing alignments
// for candidate tracks with no local file.
func AlignTracks(locals []LocalTrack, cands []CandidateTrack) []TrackAlignment {
	localUsed := make([]bool, len(locals))
	candUsed := make([]bool, len(cands))
	alignments := make([]TrackAlignment, len(locals))
	localMatched := 0

	// Pass 1: recording-MBID locks.
	for li, local := range locals {
		if local.RecordingMBID == "" {
			continue
		}

		for ci, cand := range cands {
			if candUsed[ci] || cand.MBID == "" || cand.MBID != local.RecordingMBID {
				continue
			}

			localUsed[li] = true
			candUsed[ci] = true
			localMatched++
			alignments[li] = mkAlignment(li, local, cand, alignPair{
				title:  titleSimilarity(local.Title, cand.Title),
				length: lengthScore(local.LengthMillis, cand.LengthMillis),
			}, true)

			break
		}
	}

	// Pass 2: greedy best-score over the remaining combinations.
	pairs := make([]alignPair, 0, len(locals)*len(cands))

	for li, local := range locals {
		if localUsed[li] {
			continue
		}

		for ci, cand := range cands {
			if candUsed[ci] {
				continue
			}

			title := titleSimilarity(local.Title, cand.Title)
			length := lengthScore(local.LengthMillis, cand.LengthMillis)
			pairs = append(pairs, alignPair{
				li:     li,
				ci:     ci,
				score:  combineTrackScore(title, length, trackNumberOK(local, cand)),
				title:  title,
				length: length,
			})
		}
	}

	// Sort descending by score — pick best pairs first.
	slices.SortStableFunc(pairs, func(a, b alignPair) int {
		switch {
		case a.score > b.score:
			return -1
		case a.score < b.score:
			return 1
		default:
			return 0
		}
	})

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
		alignments[p.li] = mkAlignment(p.li, locals[p.li], cands[p.ci], p, false)
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

// mkAlignment builds the matched/mismatched alignment for one
// claimed (local, candidate) pair.  idMatch pairs are always
// "matched" — same recording MBID means same recording, however
// the titles are spelled.
func mkAlignment(
	li int, l LocalTrack, c CandidateTrack, p alignPair, idMatch bool,
) TrackAlignment {
	status := AlignmentMatched
	if !idMatch && p.title < titleReject {
		status = AlignmentMismatched
	}

	delta := l.LengthMillis - c.LengthMillis
	if delta < 0 {
		delta = -delta
	}

	return TrackAlignment{
		LocalIndex:          li,
		LocalTitle:          l.Title,
		LocalLengthMillis:   l.LengthMillis,
		CandidatePosition:   c.Position,
		CandidateDiscNumber: c.DiscNumber,
		CandidateTitle:      c.Title,
		CandidateMBID:       c.MBID,
		CandidateLength:     c.LengthMillis,
		TitleScore:          p.title,
		LengthScore:         p.length,
		LengthDeltaMs:       delta,
		TrackNumberOK:       trackNumberOK(l, c),
		IDMatch:             idMatch,
		Status:              status,
	}
}
