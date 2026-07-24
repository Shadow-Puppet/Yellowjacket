package library

import (
	"testing"

	"yellowjacket/backend/metadata"
)

func TestStripFeaturing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		credit string
		want   string
	}{
		{"plain", "Lana Del Rey", "Lana Del Rey"},
		{"ft dot", "Lana Del Rey ft. Sean Lennon", "Lana Del Rey"},
		{"feat dot", "2Pac feat. Nate Dogg", "2Pac"},
		{"featuring", "Beyoncé featuring The Weeknd", "Beyoncé"},
		{"ft no dot", "Drake ft Travis Scott", "Drake"},
		{"case insensitive", "Kanye West FEAT. PARTYNEXTDOOR", "Kanye West"},
		{"ampersand kept", "Simon & Garfunkel", "Simon & Garfunkel"},
		{"comma kept", "Tyler, the Creator", "Tyler, the Creator"},
		{"first marker wins", "A feat. B ft. C", "A"},
		{"trims", "  Daft Punk feat. Panda Bear  ", "Daft Punk"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := stripFeaturing(tt.credit); got != tt.want {
				t.Errorf("stripFeaturing(%q) = %q, want %q", tt.credit, got, tt.want)
			}
		})
	}
}

func TestPrimaryArtist(t *testing.T) {
	t.Parallel()

	const lana = "b7539c32-53e7-4908-bda3-81449c367da6"

	tests := []struct {
		name     string
		tags     metadata.TrackMetadata
		wantName string
		wantMBID string
	}{
		{
			name: "collab on own album uses clean album artist",
			tags: metadata.TrackMetadata{
				Artist:          "Lana Del Rey ft. Sean Lennon",
				AlbumArtist:     "Lana Del Rey",
				ArtistMBID:      lana,
				AlbumArtistMBID: lana,
			},
			wantName: "Lana Del Rey",
			wantMBID: lana,
		},
		{
			name: "solo track",
			tags: metadata.TrackMetadata{
				Artist:          "Lana Del Rey",
				AlbumArtist:     "Lana Del Rey",
				ArtistMBID:      lana,
				AlbumArtistMBID: lana,
			},
			wantName: "Lana Del Rey",
			wantMBID: lana,
		},
		{
			name: "compilation: album artist differs, strip featuring, keep track mbid",
			tags: metadata.TrackMetadata{
				Artist:          "Some Artist feat. Guest",
				AlbumArtist:     "Various Artists",
				ArtistMBID:      "aaaa",
				AlbumArtistMBID: "va-mbid",
			},
			wantName: "Some Artist",
			wantMBID: "aaaa",
		},
		{
			name: "no album artist, strip featuring",
			tags: metadata.TrackMetadata{
				Artist:     "Some Artist feat. Guest",
				ArtistMBID: "aaaa",
			},
			wantName: "Some Artist",
			wantMBID: "aaaa",
		},
		{
			name: "no track mbid falls back to album mbid",
			tags: metadata.TrackMetadata{
				Artist:          "Solo",
				AlbumArtist:     "Solo",
				AlbumArtistMBID: "album-mbid",
			},
			wantName: "Solo",
			wantMBID: "album-mbid",
		},
		{
			name:     "empty artist",
			tags:     metadata.TrackMetadata{},
			wantName: "Unknown Artist",
			wantMBID: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotName, gotMBID := primaryArtist(&tt.tags)
			if gotName != tt.wantName {
				t.Errorf("primaryArtist name = %q, want %q", gotName, tt.wantName)
			}

			if gotMBID != tt.wantMBID {
				t.Errorf("primaryArtist mbid = %q, want %q", gotMBID, tt.wantMBID)
			}
		})
	}
}
