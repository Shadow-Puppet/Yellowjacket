package library

import (
	"fmt"
	"testing"

	"yellowjacket/backend/database"
)

// track is one file as the scan would write it: a position on a disc,
// and whatever total the file's tag declared (0 meaning the tag did not
// say).
type track struct {
	recordingID int
	disc        int
	number      int
	total       int
}

// stageAlbum writes an album's files straight in.  The completeness
// query reads only audio_files now - the totals used to live on a join
// table - so this exercises the arithmetic without standing up a scan.
func stageAlbum(t *testing.T, lib *Library, albumID int, tracks []track) {
	t.Helper()

	for _, tr := range tracks {
		database.InsertTestTrack(t, lib.db, database.TestTrack{
			FilePath:    fmt.Sprintf("/music/album%d/%d.mp3", albumID, tr.recordingID),
			Title:       "Test Track",
			Artist:      "Test Artist",
			Album:       fmt.Sprintf("Test Album %d", albumID),
			TrackNumber: int64(tr.number),
			DiscNumber:  int64(tr.disc),
			TotalTracks: int64(tr.total),
		})
	}
}

// albumIDFor reads back the id stageAlbum's files were filed under.
func albumIDFor(t *testing.T, lib *Library, albumID int) int64 {
	t.Helper()

	var id int64
	if err := lib.db.QueryRowWriter(
		"SELECT id FROM albums WHERE name = ?", fmt.Sprintf("Test Album %d", albumID),
	).Scan(&id); err != nil {
		t.Fatalf("read album id: %v", err)
	}

	return id
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

			got, err := lib.GetAlbumCompleteness(albumIDFor(t, lib, albumID))
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

// The batch and the single-album query are two spellings of one
// question, and the thing worth pinning is that they never disagree.
//
// They are genuinely different SQL — the single-album form is
// correlated subqueries over one album, the batch is two grouping
// levels over a slice — so the risk is not a typo but a drift in
// meaning: a disc's total counted once per file, a duplicate counted
// twice, a disc with no total silently covered by one that had one.
// Every shape the table above cares about is staged here at once,
// because a batch that is only ever asked about one album is not being
// asked the question that can go wrong.
func TestGetAlbumsCompletenessAgreesWithTheSingleAlbumQuery(t *testing.T) {
	t.Parallel()

	lib, _ := setupTestLibrary(t)

	shapes := map[int][]track{
		1: disc(1, 100, 12, 12),
		2: disc(1, 200, 9, 12),
		3: disc(1, 300, 13, 12),
		4: {{recordingID: 400, disc: 1, number: 1}},
		5: append(disc(1, 500, 10, 10), disc(2, 600, 2, 5)...),
		6: append(
			disc(1, 700, 10, 10),
			track{recordingID: 750, disc: 2, number: 1},
		),
		7: append(
			disc(1, 800, 5, 6),
			track{recordingID: 899, disc: 1, number: 3, total: 6},
		),
	}

	ids := make([]int64, 0, len(shapes))

	for albumID, tracks := range shapes {
		stageAlbum(t, lib, albumID, tracks)
		ids = append(ids, albumIDFor(t, lib, albumID))
	}

	batch, err := lib.GetAlbumsCompleteness(ids)
	if err != nil {
		t.Fatalf("GetAlbumsCompleteness: %v", err)
	}

	if len(batch) != len(ids) {
		t.Fatalf("batch answered for %d albums, want %d", len(batch), len(ids))
	}

	for _, id := range ids {
		one, err := lib.GetAlbumCompleteness(id)
		if err != nil {
			t.Fatalf("GetAlbumCompleteness(%d): %v", id, err)
		}

		if got := batch[id]; got != one {
			t.Errorf("album %d: batch says %+v, single says %+v", id, got, one)
		}
	}
}

// An album with no files is absent from the batch, not zeroed.
//
// "I have none of this" and "I have no idea" are the third state Known
// exists to keep apart, and a caller reading a missing key gets nothing
// rather than a confident zero it would have to know to distrust.
func TestGetAlbumsCompletenessOmitsAnAlbumWithNoFiles(t *testing.T) {
	t.Parallel()

	lib, _ := setupTestLibrary(t)

	stageAlbum(t, lib, 1, disc(1, 100, 3, 3))

	held := albumIDFor(t, lib, 1)

	got, err := lib.GetAlbumsCompleteness([]int64{held, 4242})
	if err != nil {
		t.Fatalf("GetAlbumsCompleteness: %v", err)
	}

	if _, ok := got[4242]; ok {
		t.Errorf("an album with no files answered %+v, want absent", got[4242])
	}

	if !got[held].Complete {
		t.Errorf("held album reported %+v, want complete", got[held])
	}
}

// A caller with nothing to ask about must not issue a query at all —
// sqlc's empty-slice branch rewrites the placeholder to NULL, which is
// a perfectly valid query returning nothing, so this is about the round
// trip rather than the answer.
func TestGetAlbumsCompletenessAsksNothingForAnEmptyList(t *testing.T) {
	t.Parallel()

	lib, _ := setupTestLibrary(t)

	for _, ids := range [][]int64{nil, {}, {0}, {-1, 0}} {
		got, err := lib.GetAlbumsCompleteness(ids)
		if err != nil {
			t.Fatalf("GetAlbumsCompleteness(%v): %v", ids, err)
		}

		if len(got) != 0 {
			t.Errorf("GetAlbumsCompleteness(%v) = %+v, want empty", ids, got)
		}
	}
}
