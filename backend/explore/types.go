package explore

// Wails-serializable wrapper types for MusicBrainz, ListenBrainz,
// and Cover Art Archive API responses.  These are the types that
// appear in the generated TypeScript bindings — all fields are
// exported with plain Go types (no mbtypes.MBID, no
// mbtypes.Duration) so the Wails type generator produces clean TS
// interfaces.

// MBSearchResult aggregates the three searchable entity types
// returned by the MusicBrainz search API.
type MBSearchResult struct {
	Artists       []MBArtist       `json:"artists,omitempty"`
	ReleaseGroups []MBReleaseGroup `json:"releaseGroups,omitempty"`
	Recordings    []MBRecording    `json:"recordings,omitempty"`
}

// MBArtist is a Wails-friendly projection of a MusicBrainz artist.
type MBArtist struct {
	MBID           string `json:"mbid"`
	Name           string `json:"name"`
	SortName       string `json:"sortName"`
	Type           string `json:"type"`
	Country        string `json:"country"`
	Disambiguation string `json:"disambiguation"`
	Score          int    `json:"score"`
}

// MBReleaseGroup is a Wails-friendly projection of a MusicBrainz
// release group.
type MBReleaseGroup struct {
	MBID             string   `json:"mbid"`
	Title            string   `json:"title"`
	PrimaryType      string   `json:"primaryType"`
	SecondaryTypes   []string `json:"secondaryTypes,omitempty"`
	FirstReleaseDate string   `json:"firstReleaseDate"`
	ArtistCredit     string   `json:"artistCredit"`
}

// MBRelease is a Wails-friendly projection of a MusicBrainz release.
type MBRelease struct {
	MBID    string    `json:"mbid"`
	Title   string    `json:"title"`
	Date    string    `json:"date"`
	Country string    `json:"country"`
	Status  string    `json:"status"`
	Tracks  []MBTrack `json:"tracks,omitempty"`
}

// MBRecording is a Wails-friendly projection of a MusicBrainz
// recording.
type MBRecording struct {
	MBID         string `json:"mbid"`
	Title        string `json:"title"`
	Length       int    `json:"length"`
	ArtistCredit string `json:"artistCredit"`
	Score        int    `json:"score"`
}

// MBTrack is a Wails-friendly projection of a MusicBrainz track.
type MBTrack struct {
	Position   int    `json:"position"`
	DiscNumber int    `json:"discNumber"`
	Title      string `json:"title"`
	Length     int    `json:"length"`
	MBID       string `json:"mbid"`
}

// LBTopRecording represents a popular recording from the
// ListenBrainz popularity API.
type LBTopRecording struct {
	RecordingMBID    string `json:"recordingMbid"`
	ArtistName       string `json:"artistName"`
	TrackName        string `json:"trackName"`
	TotalListenCount int    `json:"totalListenCount"`
}

// LBSimilarArtist represents a similar artist from the
// ListenBrainz labs API.
type LBSimilarArtist struct {
	ArtistMBID string  `json:"artistMbid"`
	Name       string  `json:"name"`
	Score      float64 `json:"score"`
}
