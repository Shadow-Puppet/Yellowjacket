package database

import (
	"testing"
)

// TestOneRowPerTrackForAMultiArtistCredit pins what is left of the
// multi-artist problem, which is now much smaller than it was.
//
// It used to be possible for one file to produce several rows: an
// artist credit was a row in its own table linking *many* artists, so
// any query that joined artist_credit_artist to read the artist MBID
// returned the same track once per credited artist.  The playlist, the
// queue, the library list and the phantom resolver all did, and all
// showed collaborations twice.  Nine queries carried a
// first-credited-artist subquery to work around it.
//
// The join is gone: a file carries its credit as text and points at one
// primary artist, so the fan-out has nothing to fan out from.  What is
// still worth pinning is that the credit text survives intact - a
// collaboration must still *read* as one - and that the file resolves
// to exactly one row wherever it is asked for.
func TestOneRowPerTrackForAMultiArtistCredit(t *testing.T) {
	t.Parallel()

	db := NewTestDB(t)

	id := InsertTestTrack(t, db, TestTrack{
		FilePath:   "/lib/collab.mp3",
		Title:      "Collab Song",
		Artist:     "A feat. B",
		ArtistMBID: "mbid-a",
		Album:      "An Album",
		LengthMs:   200000,
	})

	t.Run("one row in the view", func(t *testing.T) {
		var n int
		if err := db.QueryRowWriter(
			`SELECT COUNT(*) FROM track_metadata WHERE id = ?`, id,
		).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}

		if n != 1 {
			t.Errorf("track_metadata rows = %d, want 1", n)
		}
	})

	t.Run("the credit is preserved and the artist resolved", func(t *testing.T) {
		rows, err := db.Queries.GetTracks(db.Ctx, 0)
		if err != nil {
			t.Fatalf("get tracks: %v", err)
		}

		if len(rows) != 1 {
			t.Fatalf("tracks = %d, want 1", len(rows))
		}

		if rows[0].ArtistName != "A feat. B" {
			t.Errorf("artist credit = %q, want %q", rows[0].ArtistName, "A feat. B")
		}

		if rows[0].ArtistMbid != "mbid-a" {
			t.Errorf("artist mbid = %q, want %q", rows[0].ArtistMbid, "mbid-a")
		}
	})

	t.Run("one row per album track", func(t *testing.T) {
		var albumID int64
		if err := db.QueryRowWriter(
			`SELECT album_id FROM audio_files WHERE id = ?`, id,
		).Scan(&albumID); err != nil {
			t.Fatalf("album id: %v", err)
		}

		rows, err := db.Queries.GetTracks(db.Ctx, 0)
		if err != nil {
			t.Fatalf("album tracks: %v", err)
		}

		if len(rows) != 1 {
			t.Errorf("album tracks = %d, want 1", len(rows))
		}
	})
}
