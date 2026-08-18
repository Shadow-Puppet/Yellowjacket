// Package tagtotals derives the totals a tag's "5/12" form declares.
//
// It exists because the two writers that know a release's full
// tracklist -- the autotag apply pass and the download importer --
// must not import each other or the tag writer, and because getting
// the denominator wrong is invisible: a total that is too large marks
// a complete album incomplete forever, and nothing fails.
package tagtotals

// Position is one track's place in a release.  A zero Disc means the
// release did not say, which is disc 1.
type Position struct {
	Disc  int
	Track int
}

// For returns the totals to write on a file sitting on disc `disc`:
// how many tracks that disc has, and how many discs the release has.
//
// The track total is **per disc** and not the release's track count,
// because that is what the tag form means and what
// GetAlbumCompleteness sums -- summing a release total once per disc
// would multiply a two-disc album's expectation by two.
//
// Tracks are counted by distinct position rather than by row: a
// tracklist that lists a position twice is a defect in the source, and
// counting it twice would put an album permanently out of reach of its
// own total.
func For(all []Position, disc int) (tracks, discs int) {
	disc = normaliseDisc(disc)

	seenTracks := make(map[int]struct{}, len(all))
	seenDiscs := make(map[int]struct{}, 1)

	for _, p := range all {
		d := normaliseDisc(p.Disc)
		seenDiscs[d] = struct{}{}

		if d != disc || p.Track <= 0 {
			continue
		}

		seenTracks[p.Track] = struct{}{}
	}

	return len(seenTracks), len(seenDiscs)
}

// normaliseDisc treats an undeclared disc as disc 1.
func normaliseDisc(d int) int {
	if d <= 0 {
		return 1
	}

	return d
}
