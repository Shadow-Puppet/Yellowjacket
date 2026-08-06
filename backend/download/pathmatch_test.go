package download

import "testing"

func TestParsePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		path      string
		wantDisc  int
		wantTrack int
		wantTitle string
	}{
		{
			name:      "soulseek windows path with disc and track",
			path:      `@@abc\Music\Pink Floyd - The Wall (1979) [FLAC]\1-05 Another Brick In The Wall.flac`,
			wantDisc:  1,
			wantTrack: 5,
			wantTitle: "Another Brick In The Wall",
		},
		{
			name:      "dash separated track number",
			path:      "Radiohead - OK Computer/03 - Subterranean Homesick Alien.mp3",
			wantTrack: 3,
			wantTitle: "Subterranean Homesick Alien",
		},
		{
			name:      "dotted track number",
			path:      "Album/7. Karma Police.flac",
			wantTrack: 7,
			wantTitle: "Karma Police",
		},
		{
			name:      "bracketed track number",
			path:      "Album/[02] Paranoid Android.mp3",
			wantTrack: 2,
			wantTitle: "Paranoid Android",
		},
		{
			name:      "bare number and space",
			path:      "Album/11 Lucky.ogg",
			wantTrack: 11,
			wantTitle: "Lucky",
		},
		{
			name:      "artist credit inside filename is dropped",
			path:      "VA - Comp/04 - Aphex Twin - Xtal.flac",
			wantTrack: 4,
			wantTitle: "Xtal",
		},
		{
			name:      "no track number",
			path:      "Album/Introduction.flac",
			wantTrack: 0,
			wantTitle: "Introduction",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ParsePath(tt.path)

			if got.Disc != tt.wantDisc {
				t.Errorf("Disc = %d, want %d", got.Disc, tt.wantDisc)
			}

			if got.Track != tt.wantTrack {
				t.Errorf("Track = %d, want %d", got.Track, tt.wantTrack)
			}

			if got.Title != tt.wantTitle {
				t.Errorf("Title = %q, want %q", got.Title, tt.wantTitle)
			}
		})
	}
}

func TestCleanAlbumName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{"Pink Floyd - The Wall (1979) [FLAC]", "The Wall"},
		{"Radiohead - OK Computer [V0]", "OK Computer"},
		{"In Rainbows", "In Rainbows"},
		{"Artist - Album [320kbps]", "Album"},
		{"Kid A (2000)", "Kid A"},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()

			if got := cleanAlbumName(tt.in); got != tt.want {
				t.Errorf("cleanAlbumName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestBitrateForPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want int
	}{
		{"Album [320]/01 Track.mp3", 320},
		{"Album [V0]/01 Track.mp3", 245},
		{"Album (V2)/01 Track.mp3", 190},
		{"Album 192kbps/01 Track.mp3", 192},
		{"Album/01 Track.flac", 0},
		{"Album [9999]/01 Track.mp3", 0}, // out of plausible range
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()

			if got := BitrateForPath(tt.in); got != tt.want {
				t.Errorf("BitrateForPath(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestFormatForPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path      string
		want      Format
		wantAudio bool
	}{
		{"a/b.flac", FormatFLAC, true},
		{"a/b.MP3", FormatMP3, true},
		{`a\b.ogg`, FormatOGG, true},
		{"a/cover.jpg", FormatUnknown, false},
		{"a/rip.log", FormatUnknown, false},
		{"a/playlist.m3u", FormatUnknown, false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()

			got, isAudio := FormatForPath(tt.path)

			if isAudio != tt.wantAudio {
				t.Errorf("isAudio = %v, want %v", isAudio, tt.wantAudio)
			}

			if isAudio && got != tt.want {
				t.Errorf("format = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMatchFilesByTrackNumber(t *testing.T) {
	t.Parallel()

	expected := []ExpectedTrack{
		{Position: 1, Title: "Airbag"},
		{Position: 2, Title: "Paranoid Android"},
		{Position: 3, Title: "Subterranean Homesick Alien"},
	}

	files := []CandidateFile{
		{Path: "OK Computer/01 - Airbag.flac", IsAudio: true},
		{Path: "OK Computer/02 - Paranoid Android.flac", IsAudio: true},
		{Path: "OK Computer/03 - Subterranean Homesick Alien.flac", IsAudio: true},
	}

	matched, sim := matchFiles(files, expected)

	for i, m := range matched {
		if m.MatchedTo != i+1 {
			t.Errorf("file %d matched to %d, want %d", i, m.MatchedTo, i+1)
		}
	}

	if sim < 0.99 {
		t.Errorf("similarity = %f, want ~1.0", sim)
	}
}

// Track numbers that lie are the common Soulseek failure: a folder
// numbered 1..N whose contents are a different album entirely.  Title
// matching has to be what catches it.
func TestMatchFilesFallsBackToTitles(t *testing.T) {
	t.Parallel()

	expected := []ExpectedTrack{
		{Position: 1, Title: "Airbag"},
		{Position: 2, Title: "Paranoid Android"},
	}

	files := []CandidateFile{
		{Path: "Album/Paranoid Android.flac", IsAudio: true},
		{Path: "Album/Airbag.flac", IsAudio: true},
	}

	matched, sim := matchFiles(files, expected)

	if matched[0].MatchedTo != 2 {
		t.Errorf("first file matched to %d, want 2", matched[0].MatchedTo)
	}

	if matched[1].MatchedTo != 1 {
		t.Errorf("second file matched to %d, want 1", matched[1].MatchedTo)
	}

	if sim < 0.9 {
		t.Errorf("similarity = %f, want high", sim)
	}
}

func TestMatchFilesUnrelatedScoresLow(t *testing.T) {
	t.Parallel()

	expected := []ExpectedTrack{
		{Position: 1, Title: "Airbag"},
		{Position: 2, Title: "Paranoid Android"},
	}

	files := []CandidateFile{
		{Path: "Other/Enter Sandman.flac", IsAudio: true},
		{Path: "Other/Master Of Puppets.flac", IsAudio: true},
	}

	_, sim := matchFiles(files, expected)

	if sim > 0.5 {
		t.Errorf("similarity = %f, want low for unrelated tracks", sim)
	}
}
