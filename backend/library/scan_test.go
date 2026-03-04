package library

import (
	"database/sql"
	"testing"

	"yellowjacket/backend/metadata"
)

// ---------------------------------------------------------------------------
// Pure helper tests — no database dependency
// ---------------------------------------------------------------------------

func TestGetRecordingName(t *testing.T) {
	t.Parallel()

	lib := &Library{} // getRecordingName uses only tags + filePath

	tests := []struct {
		name     string
		title    string
		filePath string
		want     string
	}{
		{
			name:     "title present",
			title:    "Bohemian Rhapsody",
			filePath: "/music/queen/bohemian.mp3",
			want:     "Bohemian Rhapsody",
		},
		{
			name:     "title empty falls back to filename sans extension",
			title:    "",
			filePath: "/music/song.mp3",
			want:     "song",
		},
		{
			name:     "title empty with complex filename",
			title:    "",
			filePath: "/music/Artist - Track.flac",
			want:     "Artist - Track",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tags := &metadata.TrackMetadata{Title: tt.title}
			got := lib.getRecordingName(tags, tt.filePath)

			if got != tt.want {
				t.Errorf("getRecordingName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestToNullInt64(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input int
		want  sql.NullInt64
	}{
		{
			name:  "zero is null",
			input: 0,
			want:  sql.NullInt64{},
		},
		{
			name:  "positive is valid",
			input: 5,
			want:  sql.NullInt64{Int64: 5, Valid: true},
		},
		{
			name:  "negative is valid",
			input: -1,
			want:  sql.NullInt64{Int64: -1, Valid: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := toNullInt64(tt.input)
			if got != tt.want {
				t.Errorf("toNullInt64(%d) = %+v, want %+v", tt.input, got, tt.want)
			}
		})
	}
}

func TestToNullString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  sql.NullString
	}{
		{
			name:  "empty is null",
			input: "",
			want:  sql.NullString{},
		},
		{
			name:  "non-empty is valid",
			input: "rock",
			want:  sql.NullString{String: "rock", Valid: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := toNullString(tt.input)
			if got != tt.want {
				t.Errorf("toNullString(%q) = %+v, want %+v", tt.input, got, tt.want)
			}
		})
	}
}

func TestSplitGenres(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "empty string returns nil",
			input: "",
			want:  nil,
		},
		{
			name:  "single genre",
			input: "Rock",
			want:  []string{"Rock"},
		},
		{
			name:  "multiple genres",
			input: "Rock||Jazz||Blues",
			want:  []string{"Rock", "Jazz", "Blues"},
		},
		{
			name:  "two genres",
			input: "Electronic||Ambient",
			want:  []string{"Electronic", "Ambient"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := splitGenres(tt.input)

			if tt.want == nil {
				if got != nil {
					t.Errorf("splitGenres(%q) = %v, want nil", tt.input, got)
				}

				return
			}

			if len(got) != len(tt.want) {
				t.Fatalf("splitGenres(%q) length = %d, want %d", tt.input, len(got), len(tt.want))
			}

			for i, v := range got {
				if v != tt.want[i] {
					t.Errorf("splitGenres(%q)[%d] = %q, want %q", tt.input, i, v, tt.want[i])
				}
			}
		})
	}
}

func TestMapTrackRow(t *testing.T) {
	t.Parallel()

	track := mapTrackRow(
		"/music/queen/bohemian.flac",         // filePath
		180000,                               // lengthMs
		"Bohemian Rhapsody",                  // title
		"Queen",                              // artistName
		sql.NullInt64{Int64: 1, Valid: true}, // trackNumber
		sql.NullInt64{Int64: 1, Valid: true}, // discNumber
		"A Night at the Opera",               // album
		"Rock||Progressive Rock",             // genre
		1975,                                 // year
		"Freddie Mercury",                    // composer
		".flac",                              // fileType
		44100,                                // sampleRate
		16,                                   // bitDepth
		2,                                    // channels
		1411,                                 // bitrate
		35000000,                             // fileSize
	)

	// Verify all 16 fields.
	if track.TrackName != "Bohemian Rhapsody" {
		t.Errorf("TrackName = %q, want %q", track.TrackName, "Bohemian Rhapsody")
	}

	if track.ArtistName != "Queen" {
		t.Errorf("ArtistName = %q, want %q", track.ArtistName, "Queen")
	}

	// TrackLength is string-formatted milliseconds.
	if track.TrackLength != "180000" {
		t.Errorf("TrackLength = %q, want %q", track.TrackLength, "180000")
	}

	if track.FilePath != "/music/queen/bohemian.flac" {
		t.Errorf("FilePath = %q, want %q", track.FilePath, "/music/queen/bohemian.flac")
	}

	if track.TrackNumber != 1 {
		t.Errorf("TrackNumber = %d, want %d", track.TrackNumber, 1)
	}

	if track.DiscNumber != 1 {
		t.Errorf("DiscNumber = %d, want %d", track.DiscNumber, 1)
	}

	if track.Album != "A Night at the Opera" {
		t.Errorf("Album = %q, want %q", track.Album, "A Night at the Opera")
	}

	wantGenres := []string{"Rock", "Progressive Rock"}
	if len(track.Genre) != len(wantGenres) {
		t.Fatalf("Genre length = %d, want %d", len(track.Genre), len(wantGenres))
	}

	for i, g := range track.Genre {
		if g != wantGenres[i] {
			t.Errorf("Genre[%d] = %q, want %q", i, g, wantGenres[i])
		}
	}

	if track.Year != 1975 {
		t.Errorf("Year = %d, want %d", track.Year, 1975)
	}

	if track.Composer != "Freddie Mercury" {
		t.Errorf("Composer = %q, want %q", track.Composer, "Freddie Mercury")
	}

	if track.FileType != ".flac" {
		t.Errorf("FileType = %q, want %q", track.FileType, ".flac")
	}

	if track.SampleRate != 44100 {
		t.Errorf("SampleRate = %d, want %d", track.SampleRate, 44100)
	}

	if track.BitDepth != 16 {
		t.Errorf("BitDepth = %d, want %d", track.BitDepth, 16)
	}

	if track.Channels != 2 {
		t.Errorf("Channels = %d, want %d", track.Channels, 2)
	}

	if track.Bitrate != 1411 {
		t.Errorf("Bitrate = %d, want %d", track.Bitrate, 1411)
	}

	if track.FileSize != 35000000 {
		t.Errorf("FileSize = %d, want %d", track.FileSize, 35000000)
	}

	// Verify NullInt64 with Valid=false yields 0.
	trackNull := mapTrackRow(
		"/music/unknown.mp3", 0, "Test", "Artist",
		sql.NullInt64{}, sql.NullInt64{}, // invalid (null)
		"", "", 0, "", "", 0, 0, 0, 0, 0,
	)

	if trackNull.TrackNumber != 0 {
		t.Errorf("null TrackNumber = %d, want 0", trackNull.TrackNumber)
	}

	if trackNull.DiscNumber != 0 {
		t.Errorf("null DiscNumber = %d, want 0", trackNull.DiscNumber)
	}
}
