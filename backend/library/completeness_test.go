package library

import (
	"testing"
)

// track is one row of release_group_recordings as the scan would write
// it: a position on a disc, and whatever total the file's tag declared
// (0 meaning the tag did not say).
type track struct {
	recordingID int
	disc        int
	number      int
	total       int
}

// stageAlbum writes an album's tracks straight into
// release_group_recordings.  The completeness query reads only that
// table, so this exercises the arithmetic without standing up a scan.
func stageAlbum(t *testing.T, lib *Library, albumID int, tracks []track) {
	t.Helper()

	// The foreign keys are enforced, so the album and its recordings
	// have to exist before they can be linked.
	if _, err := lib.db.ExecContext(
		`INSERT INTO artist_credit (id, text) VALUES (1, 'Test Artist')`,
	); err != nil {
		t.Fatalf("staging artist credit: %v", err)
	}

	if _, err := lib.db.ExecContext(
		`INSERT INTO release_groups (id, name, album_artist_credit_id)
		 VALUES (?, ?, 1)`,
		albumID, "Test Album",
	); err != nil {
		t.Fatalf("staging album: %v", err)
	}

	for _, tr := range tracks {
		if _, err := lib.db.ExecContext(
			`INSERT INTO recordings (id, name, artist_credit_id) VALUES (?, ?, 1)`,
			tr.recordingID, "Test Track",
		); err != nil {
			t.Fatalf("staging recording %d: %v", tr.recordingID, err)
		}
	}

	for _, tr := range tracks {
		var total any
		if tr.total > 0 {
			total = tr.total
		}

		var number any
		if tr.number > 0 {
			number = tr.number
		}

		_, err := lib.db.ExecContext(
			`INSERT INTO release_group_recordings
			   (release_group_id, recording_id, track_number, disc_number, total_tracks)
			 VALUES (?, ?, ?, ?, ?)`,
			albumID, tr.recordingID, number, tr.disc, total,
		)
		if err != nil {
			t.Fatalf("staging track %d: %v", tr.recordingID, err)
		}
	}
}

// disc builds a run of tracks on one disc, each declaring the same
// total — which is what a correctly tagged rip looks like.
func disc(discNum, firstRecordingID, held, declared int) []track {
	out := make([]track, 0, held)

	for i := range held {
		out = append(out, track{
			recordingID: firstRecordingID + i,
			disc:        discNum,
			number:      i + 1,
			total:       declared,
		})
	}

	return out
}

func TestGetAlbumCompleteness(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		tracks       []track
		wantOwned    int
		wantExpected int
		wantKnown    bool
		wantComplete bool
	}{
		{
			name:         "every track present",
			tracks:       disc(1, 100, 12, 12),
			wantOwned:    12,
			wantExpected: 12,
			wantKnown:    true,
			wantComplete: true,
		},
		{
			name:         "three tracks short",
			tracks:       disc(1, 200, 9, 12),
			wantOwned:    9,
			wantExpected: 12,
			wantKnown:    true,
			wantComplete: false,
		},
		{
			// A bonus track puts the folder over its declared total.
			// That is a complete album, not a broken one — which is
			// why Complete is >= and not ==.
			name:         "bonus track over the declared total",
			tracks:       disc(1, 300, 13, 12),
			wantOwned:    13,
			wantExpected: 12,
			wantKnown:    true,
			wantComplete: true,
		},
		{
			// The whole reason Known exists: an untagged rip declares
			// no total, and a ring drawn from that would mark most of
			// an untagged library incomplete on no evidence.
			name: "no totals declared at all",
			tracks: []track{
				{recordingID: 400, disc: 1, number: 1},
				{recordingID: 401, disc: 1, number: 2},
			},
			wantOwned:    2,
			wantExpected: 0,
			wantKnown:    false,
			wantComplete: false,
		},
		{
			// Totals are per disc, so the expectation is a sum and not
			// a single number — the bug this shape exists to catch is
			// reading one disc's "10" as the whole album's.
			name:         "two discs, one short",
			tracks:       append(disc(1, 500, 10, 10), disc(2, 600, 2, 5)...),
			wantOwned:    12,
			wantExpected: 15,
			wantKnown:    true,
			wantComplete: false,
		},
		{
			name:         "two discs, both complete",
			tracks:       append(disc(1, 700, 10, 10), disc(2, 800, 5, 5)...),
			wantOwned:    15,
			wantExpected: 15,
			wantKnown:    true,
			wantComplete: true,
		},
		{
			// One disc ripped by a tagger that wrote totals, one by a
			// tagger that did not. The album's total is unknowable —
			// the disc that did declare cannot stand in for the one
			// that did not.
			name: "one disc untotalled",
			tracks: append(
				disc(1, 900, 10, 10),
				track{recordingID: 950, disc: 2, number: 1},
			),
			wantOwned:    11,
			wantExpected: 10,
			wantKnown:    false,
			wantComplete: false,
		},
		{
			// This app detects duplicates, so it must not be fooled by
			// them: two files of track 3 are one track held, and
			// counting both would report a short album as complete.
			name: "a duplicated track counts once",
			tracks: append(
				disc(1, 1000, 5, 6),
				track{recordingID: 1099, disc: 1, number: 3, total: 6},
			),
			wantOwned:    5,
			wantExpected: 6,
			wantKnown:    true,
			wantComplete: false,
		},
		{
			// Untotalled *and* unnumbered: the fallback keys off the
			// recording id, so these must not collapse into one.
			name: "unnumbered tracks stay distinct",
			tracks: []track{
				{recordingID: 1100, disc: 1},
				{recordingID: 1101, disc: 1},
				{recordingID: 1102, disc: 1},
			},
			wantOwned:    3,
			wantExpected: 0,
			wantKnown:    false,
			wantComplete: false,
		},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			lib, _ := setupTestLibrary(t)
			albumID := i + 1

			stageAlbum(t, lib, albumID, tc.tracks)

			got, err := lib.GetAlbumCompleteness(int64(albumID))
			if err != nil {
				t.Fatalf("GetAlbumCompleteness: %v", err)
			}

			if got.Owned != tc.wantOwned {
				t.Errorf("owned = %d, want %d", got.Owned, tc.wantOwned)
			}

			if got.Expected != tc.wantExpected {
				t.Errorf("expected = %d, want %d", got.Expected, tc.wantExpected)
			}

			if got.Known != tc.wantKnown {
				t.Errorf("known = %v, want %v", got.Known, tc.wantKnown)
			}

			if got.Complete != tc.wantComplete {
				t.Errorf("complete = %v, want %v", got.Complete, tc.wantComplete)
			}
		})
	}
}

// An album with no rows at all must not read as "complete" by virtue of
// holding everything it knows about, which is nothing.
func TestGetAlbumCompleteness_EmptyAlbum(t *testing.T) {
	t.Parallel()

	lib, _ := setupTestLibrary(t)

	got, err := lib.GetAlbumCompleteness(999)
	if err != nil {
		t.Fatalf("GetAlbumCompleteness: %v", err)
	}

	if got.Known || got.Complete || got.Owned != 0 {
		t.Errorf("empty album reported %+v, want zero and unknown", got)
	}
}
