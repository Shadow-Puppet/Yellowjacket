package autotag_test

import (
	"testing"

	"yellowjacket/backend/autotag"
)

func TestAlignTracks_ExactMatch(t *testing.T) {
	t.Parallel()

	local := []autotag.LocalTrack{
		{Title: "Come Together", TrackNumber: 1, LengthMillis: 259000},
		{Title: "Something", TrackNumber: 2, LengthMillis: 183000},
	}

	cand := []autotag.CandidateTrack{
		{Position: 1, Title: "Come Together", LengthMillis: 259000},
		{Position: 2, Title: "Something", LengthMillis: 183000},
	}

	al := autotag.AlignTracks(local, cand)
	if len(al) != 2 { //nolint:mnd
		t.Fatalf("alignments = %d, want 2", len(al))
	}

	for i, a := range al {
		if a.Status != autotag.AlignmentMatched {
			t.Errorf("alignment %d: status = %q, want matched", i, a.Status)
		}

		if a.LocalIndex != i {
			t.Errorf("alignment %d: LocalIndex = %d", i, a.LocalIndex)
		}
	}
}

// Folder has 2 tracks, candidate has 3 — the third candidate
// track surfaces as "missing" (candidate has it, folder doesn't).
func TestAlignTracks_CandidateMissingTrack(t *testing.T) {
	t.Parallel()

	local := []autotag.LocalTrack{
		{Title: "Come Together", TrackNumber: 1, LengthMillis: 259000},
		{Title: "Something", TrackNumber: 2, LengthMillis: 183000},
	}

	cand := []autotag.CandidateTrack{
		{Position: 1, Title: "Come Together", LengthMillis: 259000},
		{Position: 2, Title: "Something", LengthMillis: 183000},
		{Position: 3, Title: "Here Comes the Sun", LengthMillis: 185000},
	}

	al := autotag.AlignTracks(local, cand)
	if len(al) != 3 { //nolint:mnd
		t.Fatalf("alignments = %d, want 3 (2 matched + 1 missing)", len(al))
	}

	var missing, matched int

	for _, a := range al {
		switch a.Status {
		case autotag.AlignmentMatched:
			matched++
		case autotag.AlignmentMissing:
			missing++

			if a.CandidateTitle != "Here Comes the Sun" {
				t.Errorf("missing alignment title = %q", a.CandidateTitle)
			}
		case autotag.AlignmentUnmatched, autotag.AlignmentMismatched:
			t.Errorf("unexpected status %q", a.Status)
		}
	}

	if matched != 2 || missing != 1 { //nolint:mnd
		t.Errorf("matched=%d missing=%d, want 2/1", matched, missing)
	}
}

// One local track has a low-but-non-zero similarity to the only
// candidate slot (above the floor, below titleReject) — pairs up
// as "mismatched" so the UI can flag the diff.
func TestAlignTracks_Mismatched(t *testing.T) {
	t.Parallel()

	// "yellow" vs "yellow submarine" normalises to a similarity of
	// ~0.375 — above alignTitleFloor (0.30), below titleReject
	// (0.60).
	local := []autotag.LocalTrack{
		{Title: "Yellow", TrackNumber: 1, LengthMillis: 100000},
	}

	cand := []autotag.CandidateTrack{
		{Position: 1, Title: "Yellow Submarine", LengthMillis: 100000},
	}

	al := autotag.AlignTracks(local, cand)
	if len(al) != 1 {
		t.Fatalf("alignments = %d, want 1", len(al))
	}

	if al[0].Status != autotag.AlignmentMismatched {
		t.Errorf("status = %q, want mismatched", al[0].Status)
	}
}

// A folder track that is genuinely unrelated to anything in the
// candidate list should surface as "unmatched" rather than getting
// force-paired into a slot, even when only one candidate slot is
// available.  The candidate slot becomes "missing" in turn.
func TestAlignTracks_RandomLocalTrack(t *testing.T) {
	t.Parallel()

	// Title similarity here is well below alignTitleFloor (0.30) —
	// no shared word stems, no shared length, no shared structure.
	local := []autotag.LocalTrack{
		{Title: "Random Garbage Title", TrackNumber: 1, LengthMillis: 100000},
	}

	cand := []autotag.CandidateTrack{
		{Position: 1, Title: "Specific Album Track", LengthMillis: 259000},
	}

	al := autotag.AlignTracks(local, cand)
	if len(al) != 2 { //nolint:mnd
		t.Fatalf("alignments = %d, want 2 (1 unmatched + 1 missing)", len(al))
	}

	var unmatched, missing int

	for _, a := range al {
		switch a.Status {
		case autotag.AlignmentUnmatched:
			unmatched++
		case autotag.AlignmentMissing:
			missing++
		default:
			t.Errorf("unexpected status %q", a.Status)
		}
	}

	if unmatched != 1 || missing != 1 {
		t.Errorf("unmatched=%d missing=%d, want 1/1", unmatched, missing)
	}
}

// A folder track with the wrong track number but a matching title
// + length should still pair up — the wrong number becomes a diff
// the UI surfaces, not a reason to refuse pairing.
func TestAlignTracks_WrongTrackNumberStillPairs(t *testing.T) {
	t.Parallel()

	local := []autotag.LocalTrack{
		{Title: "Come Together", TrackNumber: 7, LengthMillis: 259000},
	}

	cand := []autotag.CandidateTrack{
		{Position: 1, Title: "Come Together", LengthMillis: 259000},
	}

	al := autotag.AlignTracks(local, cand)
	if len(al) != 1 {
		t.Fatalf("alignments = %d, want 1", len(al))
	}

	if al[0].Status != autotag.AlignmentMatched {
		t.Errorf("status = %q, want matched (track-number diff is not a reject)", al[0].Status)
	}

	if al[0].TrackNumberOK {
		t.Errorf("TrackNumberOK = true, want false (local#=7 vs candidate#=1)")
	}

	if al[0].CandidatePosition != 1 {
		t.Errorf("CandidatePosition = %d, want 1", al[0].CandidatePosition)
	}
}
