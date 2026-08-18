//go:build linux && !android

package mediacontrols

import "testing"

// The one key that must be present even when it is empty.
//
// Everything else in the map may be omitted, because a client reading
// it renders a track with no title as a track with no title. Art is
// different: KDE's applet treats an *absent* mpris:artUrl as no news
// about the art and keeps drawing the last one it saw, so a track with
// no cover wore the previous album's sleeve — which reads as the wrong
// track playing rather than as missing artwork.
func TestMetadataMapAlwaysCarriesArtURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		meta Metadata
		want string
	}{
		{
			name: "no art at all",
			meta: Metadata{Title: "Blue in Green"},
			want: "",
		},
		{
			name: "art on disk",
			meta: Metadata{
				Title:       "Blue in Green",
				ArtFilePath: "/covers/kind-of-blue_lg.jpg",
			},
			want: "file:///covers/kind-of-blue_lg.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := metadataMap(tt.meta, 1)

			got, ok := m["mpris:artUrl"]
			if !ok {
				t.Fatal("mpris:artUrl is absent; it must always be sent")
			}

			if got != tt.want {
				t.Errorf("mpris:artUrl = %v, want %q", got, tt.want)
			}
		})
	}
}

// The trackid has to change between tracks or a client is entitled to
// treat the metadata as describing the same track it already has.
func TestMetadataMapTrackIDVaries(t *testing.T) {
	t.Parallel()

	first := metadataMap(Metadata{Title: "A"}, 1)["mpris:trackid"]
	second := metadataMap(Metadata{Title: "B"}, 2)["mpris:trackid"]

	if first == second {
		t.Errorf("trackid did not change: %v", first)
	}
}

// The optional keys stay optional — this is what makes artUrl's
// always-present treatment a deliberate exception rather than drift.
func TestMetadataMapOmitsEmptyOptionalFields(t *testing.T) {
	t.Parallel()

	m := metadataMap(Metadata{}, 1)

	for _, key := range []string{
		"xesam:title",
		"xesam:artist",
		"xesam:album",
		"mpris:length",
	} {
		if _, ok := m[key]; ok {
			t.Errorf("%s is present for an empty Metadata", key)
		}
	}
}
