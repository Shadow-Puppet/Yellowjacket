package models

// Album represents a music album with its tracks and metadata.
type Album struct {
	Name                 string
	Tracks               []Track
	MusicBrainzReleaseID string
	CoverArt             Art
}

// Track represents a single music track.
type Track struct {
	Name                   string
	MusicBrainzRecordingID string
}

// Artist represents a music artist.
type Artist struct {
	Name                string
	MusicBrainzArtistID string
}
