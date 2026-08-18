package autotagservice

import (
	"testing"

	"yellowjacket/backend/autotag"
	"yellowjacket/backend/tagwriter"
)

// twAdapter passes the diff map through unchanged, so autotag's field
// constants and tagwriter's are the same keys written down twice --
// deliberately, to keep autotag out of the write pipeline's import
// graph.  A key that drifts does not fail to compile and does not fail
// to write: the writer simply finds no entry under the name it looks
// for, and the field is silently dropped.  That is what this pins, and
// this package is the one place that imports both.
func TestAutotagAndTagwriterAgreeOnFieldNames(t *testing.T) {
	t.Parallel()

	pairs := map[string][2]string{
		"title":        {autotag.FieldTitle, tagwriter.FieldTitle},
		"artist":       {autotag.FieldArtist, tagwriter.FieldArtist},
		"album":        {autotag.FieldAlbum, tagwriter.FieldAlbum},
		"album artist": {autotag.FieldAlbumArtist, tagwriter.FieldAlbumArtist},
		"year":         {autotag.FieldYear, tagwriter.FieldYear},
		"track number": {autotag.FieldTrackNumber, tagwriter.FieldTrackNumber},
		"disc number":  {autotag.FieldDiscNumber, tagwriter.FieldDiscNumber},
		"total tracks": {autotag.FieldTotalTracks, tagwriter.FieldTotalTracks},
		"total discs":  {autotag.FieldTotalDiscs, tagwriter.FieldTotalDiscs},
		"cover art":    {autotag.FieldCoverArt, tagwriter.FieldCoverArt},
	}

	for name, pair := range pairs {
		if pair[0] != pair[1] {
			t.Errorf("%s: autotag says %q, tagwriter says %q", name, pair[0], pair[1])
		}
	}
}
