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
	TopResults    []TopResult      `json:"topResults,omitempty"`
}

// TopResult represents a single top-result card shown above the
// categorized search lists.  Computed by intent scoring after all
// reranking is complete.
type TopResult struct {
	EntityType   string  `json:"entityType"` // "artist", "release_group", "recording"
	MBID         string  `json:"mbid"`
	Name         string  `json:"name"`
	ArtistCredit string  `json:"artistCredit,omitempty"` // for tracks/albums
	ArtistMBID   string  `json:"artistMbid,omitempty"`   // for linking the artist subtitle
	IntentScore  float64 `json:"intentScore"`
	// Artist-specific
	ArtistType string `json:"artistType,omitempty"` // "Group", "Person"
	Country    string `json:"country,omitempty"`
	// Album-specific
	PrimaryType string `json:"primaryType,omitempty"`
	Year        string `json:"year,omitempty"`
	// Track-specific.  ReleaseGroupMBID is resolved (from CAAReleaseMBID)
	// so a track click can open its album page with the track highlighted,
	// matching how tracks behave everywhere else.  ReleaseName is the album
	// title used for the album page header.
	Length           int    `json:"length,omitempty"`
	CAAReleaseMBID   string `json:"caaReleaseMbid,omitempty"`
	ReleaseGroupMBID string `json:"releaseGroupMbid,omitempty"`
	ReleaseName      string `json:"releaseName,omitempty"`
	// Library status — populated from index cross-reference columns.
	InLibrary bool `json:"inLibrary"`
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
	OriginalScore  int    `json:"-"`          // MB search relevance, preserved across reranking
	HasPopularity  bool   `json:"-"`          // true if LB/index had listen data for this artist
	Popularity     int    `json:"popularity"` // raw LB listen count (0 if unknown)
	ListenerCount  int    `json:"listenerCount"`
	InLibrary      bool   `json:"inLibrary"`         // true if the user owns music by this artist
	LocalID        int64  `json:"localId,omitempty"` // local artist row ID for navigation
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
	ArtistMBID       string   `json:"artistMbid,omitempty"` // for linking the artist to its detail page
	Score            int      `json:"-"`                    // MB search relevance, used for reranking
	Popularity       int      `json:"popularity"`           // raw LB listen count (0 if unknown)
	ListenerCount    int      `json:"listenerCount"`
	InLibrary        bool     `json:"inLibrary"`         // true if the user owns this album
	LocalID          int64    `json:"localId,omitempty"` // local release_group row ID
}

// MBRelease is a Wails-friendly projection of a MusicBrainz release.
type MBRelease struct {
	MBID         string    `json:"mbid"`
	Title        string    `json:"title"`
	Date         string    `json:"date"`
	Country      string    `json:"country"`
	Status       string    `json:"status"`
	ArtistCredit string    `json:"artistCredit,omitempty"`
	Tracks       []MBTrack `json:"tracks,omitempty"`
	// ReleaseGroupMBID is the parent release group's MBID. Empty unless
	// the lookup requested the "release-groups" include (LookupRelease
	// does); used to resolve a release-level MBID (what many taggers
	// write) back to the release-group MBID everything else on the
	// album page is keyed by.
	ReleaseGroupMBID string `json:"releaseGroupMbid,omitempty"`
}

// MBRecording is a Wails-friendly projection of a MusicBrainz
// recording.
type MBRecording struct {
	MBID           string `json:"mbid"`
	Title          string `json:"title"`
	Length         int    `json:"length"`
	ArtistCredit   string `json:"artistCredit"`
	ArtistMBID     string `json:"artistMbid,omitempty"` // for linking the artist to its detail page
	Score          int    `json:"score"`
	Popularity     int    `json:"popularity"` // raw LB listen count (0 if unknown)
	ListenerCount  int    `json:"listenerCount"`
	CAAReleaseMBID string `json:"caaReleaseMbid,omitempty"` // parent release, for album navigation
	// ReleaseGroupMBID is resolved from CAAReleaseMBID so a track can
	// link to its album page with the track highlighted, matching how
	// tracks behave everywhere else.
	ReleaseGroupMBID string `json:"releaseGroupMbid,omitempty"`
	ReleaseName      string `json:"releaseName,omitempty"` // album title
	InLibrary        bool   `json:"inLibrary"`             // true if the user owns this recording
	LocalID          int64  `json:"localId,omitempty"`     // local recording row ID
}

// MBTrack is a Wails-friendly projection of a MusicBrainz track.
type MBTrack struct {
	Position   int    `json:"position"`
	DiscNumber int    `json:"discNumber"`
	Title      string `json:"title"`
	Length     int    `json:"length"`
	MBID       string `json:"mbid"`
	InLibrary  bool   `json:"inLibrary"`
	LocalID    int64  `json:"localId,omitempty"`
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
	CAAReleaseMBID   string `json:"caaReleaseMbid"`
	// ReleaseGroupMBID is resolved from CAAReleaseMBID so a top-track
	// row can link to its album page with the track highlighted.
	ReleaseGroupMBID string `json:"releaseGroupMbid,omitempty"`
	ReleaseName      string `json:"releaseName"`
	Length           int    `json:"length"` // milliseconds (from LB API)
	InLibrary        bool   `json:"inLibrary"`
	LocalID          int64  `json:"localId,omitempty"`
}

// lbTopRecordingWire matches the ListenBrainz API's snake_case
// JSON response for the popularity/top-recordings-for-artist
// endpoint.
type lbTopRecordingWire struct {
	RecordingMBID    string `json:"recording_mbid"`
	ArtistName       string `json:"artist_name"`
	RecordingName    string `json:"recording_name"`
	TotalListenCount int    `json:"total_listen_count"`
	CAAReleaseMBID   string `json:"caa_release_mbid"`
	ReleaseName      string `json:"release_name"`
	Length           int    `json:"length"` // milliseconds
}

func (w lbTopRecordingWire) toPublic() LBTopRecording {
	return LBTopRecording{
		RecordingMBID:    w.RecordingMBID,
		ArtistName:       w.ArtistName,
		TrackName:        w.RecordingName,
		TotalListenCount: w.TotalListenCount,
		CAAReleaseMBID:   w.CAAReleaseMBID,
		ReleaseName:      w.ReleaseName,
		Length:           w.Length,
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
	Date             string `json:"date"`
	TotalListenCount int    `json:"totalListenCount"`
	CAAReleaseMBID   string `json:"caaReleaseMbid"`
	InLibrary        bool   `json:"inLibrary"`
	LocalID          int64  `json:"localId,omitempty"`
}

// lbTopReleaseGroupWire matches the ListenBrainz API's snake_case
// JSON response for the popularity/top-release-groups-for-artist
// endpoint.
type lbTopReleaseGroupWire struct {
	ReleaseGroupMBID string `json:"release_group_mbid"`
	TotalListenCount int    `json:"total_listen_count"`
	ReleaseGroup     struct {
		Name           string `json:"name"`
		Type           string `json:"type"`
		Date           string `json:"date"`
		CAAReleaseMBID string `json:"caa_release_mbid"`
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
		Date:             w.ReleaseGroup.Date,
		TotalListenCount: w.TotalListenCount,
		CAAReleaseMBID:   w.ReleaseGroup.CAAReleaseMBID,
	}
}
