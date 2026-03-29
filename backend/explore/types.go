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
	EnglishName    string `json:"englishName,omitempty"`
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
//
// JSON tags use camelCase for Wails→frontend serialization.
// The API response uses snake_case, so we unmarshal into
// lbTopRecordingWire first, then convert.
type LBTopRecording struct {
	RecordingMBID    string `json:"recordingMbid"`
	ArtistName       string `json:"artistName"`
	TrackName        string `json:"trackName"`
	TotalListenCount int    `json:"totalListenCount"`
}

// lbTopRecordingWire matches the ListenBrainz API's snake_case
// JSON response for the popularity/top-recordings-for-artist
// endpoint.
type lbTopRecordingWire struct {
	RecordingMBID    string `json:"recording_mbid"`
	ArtistName       string `json:"artist_name"`
	RecordingName    string `json:"recording_name"`
	TotalListenCount int    `json:"total_listen_count"`
}

func (w lbTopRecordingWire) toPublic() LBTopRecording {
	return LBTopRecording{
		RecordingMBID:    w.RecordingMBID,
		ArtistName:       w.ArtistName,
		TrackName:        w.RecordingName,
		TotalListenCount: w.TotalListenCount,
	}
}

// LBSimilarArtist represents a similar artist from the
// ListenBrainz labs API.
type LBSimilarArtist struct {
	ArtistMBID string  `json:"artistMbid"`
	Name       string  `json:"name"`
	Score      float64 `json:"score"`
}

// LBTopReleaseGroup represents a popular release group from the
// ListenBrainz popularity API.
type LBTopReleaseGroup struct {
	ReleaseGroupMBID string `json:"releaseGroupMbid"`
	Title            string `json:"title"`
	ArtistName       string `json:"artistName"`
	Type             string `json:"type"`
	TotalListenCount int    `json:"totalListenCount"`
}

// lbTopReleaseGroupWire matches the ListenBrainz API's snake_case
// JSON response for the popularity/top-release-groups-for-artist
// endpoint.
type lbTopReleaseGroupWire struct {
	ReleaseGroupMBID string `json:"release_group_mbid"`
	TotalListenCount int    `json:"total_listen_count"`
	ReleaseGroup     struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"release_group"`
	Artist struct {
		Artists []struct {
			Name string `json:"name"`
		} `json:"artists"`
	} `json:"artist"`
}

func (w lbTopReleaseGroupWire) toPublic() LBTopReleaseGroup {
	artistName := ""
	if len(w.Artist.Artists) > 0 {
		artistName = w.Artist.Artists[0].Name
	}

	return LBTopReleaseGroup{
		ReleaseGroupMBID: w.ReleaseGroupMBID,
		Title:            w.ReleaseGroup.Name,
		ArtistName:       artistName,
		Type:             w.ReleaseGroup.Type,
		TotalListenCount: w.TotalListenCount,
	}
}
