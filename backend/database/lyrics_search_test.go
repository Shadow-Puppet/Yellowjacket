package database

import (
	"testing"
)

// seedLyricsTrack inserts the minimal FK chain (artist_credit →
// recording → audio_file → release_group link) for one track with the
// given lyrics, so lyric-search tests have realistic joins.
func seedLyricsTrack(
	t *testing.T,
	db *DB,
	id int64,
	title, artist, album, lyrics string,
	lenMs int64,
) {
	t.Helper()

	if _, err := db.ExecContext(
		"INSERT OR IGNORE INTO artist_credit (id, text) VALUES (?, ?)", id, artist,
	); err != nil {
		t.Fatalf("insert artist_credit: %v", err)
	}

	if _, err := db.ExecContext(
		"INSERT OR IGNORE INTO release_groups (id, name) VALUES (?, ?)", id, album,
	); err != nil {
		t.Fatalf("insert release_group: %v", err)
	}

	if _, err := db.ExecContext(
		"INSERT INTO recordings (id, name, artist_credit_id, lyrics) VALUES (?, ?, ?, ?)",
		id, title, id, nullableLyrics(lyrics),
	); err != nil {
		t.Fatalf("insert recording: %v", err)
	}

	if _, err := db.ExecContext(
		"INSERT INTO audio_files (id, file_path, length_milliseconds, file_type_id, recording_id) "+
			"VALUES (?, ?, ?, ?, ?)",
		id, "/music/track"+itoa(id)+".mp3", lenMs, 0, id,
	); err != nil {
		t.Fatalf("insert audio_file: %v", err)
	}

	if _, err := db.ExecContext(
		"INSERT INTO release_group_recordings (release_group_id, recording_id) VALUES (?, ?)",
		id, id,
	); err != nil {
		t.Fatalf("insert release_group_recordings: %v", err)
	}
}

func nullableLyrics(l string) any {
	if l == "" {
		return nil
	}

	return l
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}

	var b []byte

	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}

	return string(b)
}

func TestSearchLyrics(t *testing.T) {
	t.Parallel()

	db := NewTestDB(t)

	seedLyricsTrack(
		t,
		db,
		1,
		"The Sound of Silence",
		"Simon & Garfunkel",
		"Sounds of Silence",
		"Hello darkness my old friend\nI've come to talk with you again",
		180000,
	)
	seedLyricsTrack(t, db, 2, "Bohemian Rhapsody", "Queen", "A Night at the Opera",
		"Is this the real life? Is this just fantasy?", 354000)
	seedLyricsTrack(t, db, 3, "Instrumental Track", "Some Artist", "Some Album",
		"", 200000) // no lyrics — must never appear in results

	if err := db.RebuildLyricsIndex(); err != nil {
		t.Fatalf("RebuildLyricsIndex: %v", err)
	}

	t.Run("phrase match returns the right track with metadata", func(t *testing.T) {
		t.Parallel()

		hits, err := db.SearchLyrics("hello darkness my old friend", 10)
		if err != nil {
			t.Fatalf("SearchLyrics: %v", err)
		}

		if len(hits) != 1 {
			t.Fatalf("expected 1 hit, got %d: %+v", len(hits), hits)
		}

		h := hits[0]
		if h.RecordingID != 1 {
			t.Errorf("RecordingID = %d, want 1", h.RecordingID)
		}

		if h.Title != "The Sound of Silence" {
			t.Errorf("Title = %q, want The Sound of Silence", h.Title)
		}

		if h.Artist != "Simon & Garfunkel" {
			t.Errorf("Artist = %q, want Simon & Garfunkel", h.Artist)
		}

		if h.Album != "Sounds of Silence" {
			t.Errorf("Album = %q, want Sounds of Silence", h.Album)
		}

		if h.FilePath == "" {
			t.Error("FilePath is empty; expected a playable path")
		}
	})

	t.Run("adjacency: scrambled words do not match as a phrase", func(t *testing.T) {
		t.Parallel()

		hits, err := db.SearchLyrics("friend old darkness", 10)
		if err != nil {
			t.Fatalf("SearchLyrics: %v", err)
		}

		if len(hits) != 0 {
			t.Errorf("expected 0 phrase hits for scrambled words, got %d", len(hits))
		}
	})

	t.Run("empty query returns nil", func(t *testing.T) {
		t.Parallel()

		hits, err := db.SearchLyrics("   ", 10)
		if err != nil {
			t.Fatalf("SearchLyrics: %v", err)
		}

		if hits != nil {
			t.Errorf("expected nil for empty query, got %+v", hits)
		}
	})

	t.Run("no match returns no hits", func(t *testing.T) {
		t.Parallel()

		hits, err := db.SearchLyrics("this phrase appears in no song", 10)
		if err != nil {
			t.Fatalf("SearchLyrics: %v", err)
		}

		if len(hits) != 0 {
			t.Errorf("expected 0 hits, got %d", len(hits))
		}
	})
}

func TestSetRecordingLyricsUpdatesIndex(t *testing.T) {
	t.Parallel()

	db := NewTestDB(t)

	// Track starts with no lyrics.
	seedLyricsTrack(t, db, 1, "Yesterday", "The Beatles", "Help!", "", 125000)

	if err := db.RebuildLyricsIndex(); err != nil {
		t.Fatalf("RebuildLyricsIndex: %v", err)
	}

	// Nothing indexed yet.
	if hits, _ := db.SearchLyrics("yesterday all my troubles", 10); len(hits) != 0 {
		t.Fatalf("expected 0 hits before backfill, got %d", len(hits))
	}

	// Backfill lyrics — should update both the column and the FTS index.
	const lyrics = "Yesterday all my troubles seemed so far away"
	if err := db.SetRecordingLyrics(1, lyrics); err != nil {
		t.Fatalf("SetRecordingLyrics: %v", err)
	}

	stored, err := db.GetRecordingLyrics(1)
	if err != nil {
		t.Fatalf("GetRecordingLyrics: %v", err)
	}

	if stored != lyrics {
		t.Errorf("stored lyrics = %q, want %q", stored, lyrics)
	}

	hits, err := db.SearchLyrics("all my troubles seemed so far away", 10)
	if err != nil {
		t.Fatalf("SearchLyrics: %v", err)
	}

	if len(hits) != 1 || hits[0].RecordingID != 1 {
		t.Fatalf("expected recording 1 after backfill, got %+v", hits)
	}
}

func TestRecordingsMissingLyrics(t *testing.T) {
	t.Parallel()

	db := NewTestDB(t)

	seedLyricsTrack(t, db, 1, "Has Lyrics", "Artist A", "Album A", "some words here", 100000)
	seedLyricsTrack(t, db, 2, "No Lyrics", "Artist B", "Album B", "", 200000)

	missing, err := db.RecordingsMissingLyrics(50)
	if err != nil {
		t.Fatalf("RecordingsMissingLyrics: %v", err)
	}

	if len(missing) != 1 {
		t.Fatalf("expected 1 candidate, got %d: %+v", len(missing), missing)
	}

	c := missing[0]
	if c.RecordingID != 2 || c.Title != "No Lyrics" || c.Artist != "Artist B" {
		t.Errorf("unexpected candidate: %+v", c)
	}

	if c.LengthMilliseconds != 200000 {
		t.Errorf("LengthMilliseconds = %d, want 200000", c.LengthMilliseconds)
	}

	// Single-recording lookup mirrors the batch fields.
	one, err := db.RecordingLyricLookup(2)
	if err != nil {
		t.Fatalf("RecordingLyricLookup: %v", err)
	}

	if one == nil || one.Artist != "Artist B" || one.Album != "Album B" {
		t.Errorf("unexpected lookup: %+v", one)
	}
}
