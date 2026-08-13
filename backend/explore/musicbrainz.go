package explore

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode"

	"go.uploadedlobster.com/mbtypes"
	"go.uploadedlobster.com/musicbrainzws2"
)

const (
	// cacheTTLSearch is the TTL for search results (results may shift).
	cacheTTLSearch = 24 * time.Hour
	// cacheTTLEntity is the TTL for lookup/browse results (entity data
	// changes rarely).
	cacheTTLEntity = 7 * 24 * time.Hour
	// cacheTTLReleases is the TTL for a release group's releases +
	// tracklists.  This data is effectively immutable once published, so
	// it's cached far longer than other entities: it's the local store
	// that keeps an album page's cold fetch a once-per-quarter event
	// rather than a weekly one.
	cacheTTLReleases = 90 * 24 * time.Hour
)

// MusicBrainzClient wraps the musicbrainzws2 library with a local
// response cache.  Every API call checks the cache first and stores
// successful responses for future hits.
//
// A proactive rate limiter gates all outgoing requests at 1 req/sec
// to avoid triggering MusicBrainz 429 responses.  The underlying
// musicbrainzws2.Client still retries on 429 as a safety net, but
// the limiter should prevent most rate-limit hits.
type MusicBrainzClient struct {
	mb      *musicbrainzws2.Client
	cache   *Cache
	limiter *RateLimiter
	logger  *slog.Logger
}

// NewMusicBrainzClient creates a MusicBrainz API client that caches
// responses in the given Cache.  The provided rate limiter is shared
// with all other MB consumers (e.g. artist image resolution) to
// prevent concurrent bursts from triggering 429s.
func NewMusicBrainzClient(
	cache *Cache,
	limiter *RateLimiter,
	logger *slog.Logger,
) *MusicBrainzClient {
	mb := musicbrainzws2.NewClient(musicbrainzws2.AppInfo{
		Name:    "YellowJacket",
		Version: "dev",
		URL:     "https://github.com/yellowjacket",
	})

	return &MusicBrainzClient{
		mb:      mb,
		cache:   cache,
		limiter: limiter,
		logger:  logger,
	}
}

// Close releases resources held by the underlying HTTP client.
func (c *MusicBrainzClient) Close() error {
	return c.mb.Close()
}

// ---------------------------------------------------------------------------
// Search
// ---------------------------------------------------------------------------

// SearchArtists queries MusicBrainz for artists matching the given
// query string.  Returns results, the total match count from MB,
// and any error.  Results are cached for 1 day.
func (c *MusicBrainzClient) SearchArtists(
	ctx context.Context, query string, limit int,
) ([]MBArtist, int, error) {
	cacheKey := fmt.Sprintf("mb:search:artist:%s:%d", query, limit)

	if data, ok := c.cache.Get(cacheKey); ok {
		var cached mbSearchCache[MBArtist]
		if err := json.Unmarshal(data, &cached); err == nil {
			return cached.Results, cached.TotalCount, nil
		}
	}

	if err := c.limiter.Wait(ctx); err != nil {
		return nil, 0, err
	}

	c.logger.Info("musicbrainz search artists",
		"query", query,
		"limit", limit,
	)

	result, err := c.mb.SearchArtists(ctx,
		musicbrainzws2.SearchFilter{Query: query},
		musicbrainzws2.Paginator{Limit: clampLimit(limit)},
	)
	if err != nil {
		return nil, 0, err
	}

	out := convertArtists(result.Artists)

	c.cacheJSON(cacheKey, mbSearchCache[MBArtist]{
		Results: out, TotalCount: result.Count,
	}, cacheTTLSearch, "", "")

	return out, result.Count, nil
}

// SearchReleaseGroups queries MusicBrainz for release groups
// matching the given query string.
func (c *MusicBrainzClient) SearchReleaseGroups(
	ctx context.Context, query string, limit int,
) ([]MBReleaseGroup, int, error) {
	cacheKey := fmt.Sprintf("mb:search:release-group:%s:%d", query, limit)

	if data, ok := c.cache.Get(cacheKey); ok {
		var cached mbSearchCache[MBReleaseGroup]
		if err := json.Unmarshal(data, &cached); err == nil {
			return cached.Results, cached.TotalCount, nil
		}
	}

	if err := c.limiter.Wait(ctx); err != nil {
		return nil, 0, err
	}

	c.logger.Info("musicbrainz search release groups",
		"query", query,
		"limit", limit,
	)

	result, err := c.mb.SearchReleaseGroups(ctx,
		musicbrainzws2.SearchFilter{Query: query},
		musicbrainzws2.Paginator{Limit: clampLimit(limit)},
	)
	if err != nil {
		return nil, 0, err
	}

	out := convertReleaseGroups(result.ReleaseGroups)

	c.cacheJSON(cacheKey, mbSearchCache[MBReleaseGroup]{
		Results: out, TotalCount: result.Count,
	}, cacheTTLSearch, "", "")

	return out, result.Count, nil
}

// SearchRecordings queries MusicBrainz for recordings matching the
// given query string.
func (c *MusicBrainzClient) SearchRecordings(
	ctx context.Context, query string, limit int,
) ([]MBRecording, int, error) {
	cacheKey := fmt.Sprintf("mb:search:recording:%s:%d", query, limit)

	if data, ok := c.cache.Get(cacheKey); ok {
		var cached mbSearchCache[MBRecording]
		if err := json.Unmarshal(data, &cached); err == nil {
			return cached.Results, cached.TotalCount, nil
		}
	}

	if err := c.limiter.Wait(ctx); err != nil {
		return nil, 0, err
	}

	c.logger.Info("musicbrainz search recordings",
		"query", query,
		"limit", limit,
	)

	result, err := c.mb.SearchRecordings(ctx,
		musicbrainzws2.SearchFilter{Query: query},
		musicbrainzws2.Paginator{Limit: clampLimit(limit)},
	)
	if err != nil {
		return nil, 0, err
	}

	out := convertRecordings(result.Recordings)

	c.cacheJSON(cacheKey, mbSearchCache[MBRecording]{
		Results: out, TotalCount: result.Count,
	}, cacheTTLSearch, "", "")

	return out, result.Count, nil
}

// mbSearchCache wraps search results with the total count for caching.
type mbSearchCache[T any] struct {
	Results    []T `json:"results"`
	TotalCount int `json:"totalCount"`
}

// ---------------------------------------------------------------------------
// Lookup
// ---------------------------------------------------------------------------

// LookupArtist fetches a single artist by MBID.  Cached for 7 days.
// Uses inc=release-groups to pre-populate the browse cache so the
// subsequent BrowseReleaseGroups call is a free cache hit.
func (c *MusicBrainzClient) LookupArtist(
	ctx context.Context, mbid string,
) (*MBArtist, error) {
	cacheKey := "mb:lookup:artist:" + mbid

	if data, ok := c.cache.Get(cacheKey); ok {
		var out MBArtist
		if err := json.Unmarshal(data, &out); err == nil {
			return &out, nil
		}
	}

	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	c.logger.Info("musicbrainz lookup artist", "mbid", mbid)

	a, err := c.mb.LookupArtist(ctx,
		mbtypes.MBID(mbid),
		musicbrainzws2.IncludesFilter{Includes: []string{"release-groups"}},
	)
	if err != nil {
		return nil, err
	}

	out := convertArtist(a)

	c.cacheJSON(cacheKey, out, cacheTTLEntity, mbid, "artist")

	// Pre-populate the browse cache with the included release groups
	// so BrowseReleaseGroups returns instantly from cache.
	// The inc= response is limited to 25 items; only cache if we
	// likely got the full discography (< 25 means no truncation).
	if len(a.ReleaseGroups) > 0 && len(a.ReleaseGroups) < 25 {
		browseKey := "mb:browse:release-groups:" + mbid
		rgs := convertReleaseGroups(a.ReleaseGroups)
		c.cacheJSON(browseKey, rgs, cacheTTLEntity, mbid, "artist")
	}

	return &out, nil
}

// LookupReleaseGroup fetches a single release group by MBID.
func (c *MusicBrainzClient) LookupReleaseGroup(
	ctx context.Context, mbid string,
) (*MBReleaseGroup, error) {
	cacheKey := "mb:lookup:release-group:" + mbid

	if data, ok := c.cache.Get(cacheKey); ok {
		var out MBReleaseGroup
		if err := json.Unmarshal(data, &out); err == nil {
			return &out, nil
		}
	}

	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	c.logger.Info("musicbrainz lookup release group", "mbid", mbid)

	rg, err := c.mb.LookupReleaseGroup(ctx,
		mbtypes.MBID(mbid),
		musicbrainzws2.IncludesFilter{Includes: []string{"artist-credits"}},
	)
	if err != nil {
		return nil, err
	}

	out := convertReleaseGroup(rg)

	c.cacheJSON(cacheKey, out, cacheTTLEntity, mbid, "release-group")

	return &out, nil
}

// ---------------------------------------------------------------------------
// Browse
// ---------------------------------------------------------------------------

// BrowseReleaseGroups fetches the release groups for a given artist
// MBID.  Cached for 7 days.
func (c *MusicBrainzClient) BrowseReleaseGroups(
	ctx context.Context, artistMBID string,
) ([]MBReleaseGroup, error) {
	cacheKey := "mb:browse:release-groups:" + artistMBID

	if data, ok := c.cache.Get(cacheKey); ok {
		var out []MBReleaseGroup
		if err := json.Unmarshal(data, &out); err == nil {
			return out, nil
		}
	}

	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	c.logger.Info("musicbrainz browse release groups",
		"artistMBID", artistMBID,
	)

	result, err := c.mb.BrowseReleaseGroups(ctx,
		musicbrainzws2.ReleaseGroupFilter{
			ArtistMBID: mbtypes.MBID(artistMBID),
		},
		musicbrainzws2.Paginator{Limit: musicbrainzws2.MaxLimit},
	)
	if err != nil {
		return nil, err
	}

	out := convertReleaseGroups(result.ReleaseGroups)

	c.cacheJSON(cacheKey, out, cacheTTLEntity, artistMBID, "artist")

	return out, nil
}

// LookupRelease fetches a single release by MBID (with media +
// recordings).  Used by the autotag paste-URL escape hatch.
// Cached for 7 days.
func (c *MusicBrainzClient) LookupRelease(
	ctx context.Context, mbid string,
) (*MBRelease, error) {
	cacheKey := "mb:lookup:release:" + mbid

	if data, ok := c.cache.Get(cacheKey); ok {
		var out MBRelease
		if err := json.Unmarshal(data, &out); err == nil {
			return &out, nil
		}
	}

	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	c.logger.Info("musicbrainz lookup release", "mbid", mbid)

	r, err := c.mb.LookupRelease(
		ctx,
		mbtypes.MBID(mbid),
		musicbrainzws2.IncludesFilter{
			Includes: []string{"recordings", "media", "artist-credits", "release-groups"},
		},
	)
	if err != nil {
		return nil, err
	}

	out := convertRelease(r)

	c.cacheJSON(cacheKey, out, cacheTTLEntity, mbid, "release")

	return &out, nil
}

// MBRecordingRelease is a slim reference to one release a recording
// appears on — enough for the autotagger to pick a representative
// release and then LookupRelease it in full.
type MBRecordingRelease struct {
	MBID   string `json:"mbid"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Date   string `json:"date"`
}

// LookupRecordingReleases fetches the releases a recording appears on
// (id / title / status / date only).  Used by the autotag recording-
// search path to resolve a picked recording to a concrete release.
// Cached for 7 days.
func (c *MusicBrainzClient) LookupRecordingReleases(
	ctx context.Context, recordingMBID string,
) ([]MBRecordingRelease, error) {
	cacheKey := "mb:lookup:recording-releases:" + recordingMBID

	if data, ok := c.cache.Get(cacheKey); ok {
		var out []MBRecordingRelease
		if err := json.Unmarshal(data, &out); err == nil {
			return out, nil
		}
	}

	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	c.logger.Info("musicbrainz lookup recording releases", "mbid", recordingMBID)

	rec, err := c.mb.LookupRecording(
		ctx,
		mbtypes.MBID(recordingMBID),
		musicbrainzws2.IncludesFilter{Includes: []string{"releases"}},
	)
	if err != nil {
		return nil, err
	}

	out := make([]MBRecordingRelease, 0, len(rec.Releases))
	for _, rel := range rec.Releases {
		out = append(out, MBRecordingRelease{
			MBID:   string(rel.ID),
			Title:  rel.Title,
			Status: rel.Status,
			Date:   rel.Date.String(),
		})
	}

	c.cacheJSON(cacheKey, out, cacheTTLEntity, recordingMBID, "recording")

	return out, nil
}

// BrowseReleases fetches the releases for a given release group
// MBID, including media/track information.  Cached for 7 days.
// browseReleasesCacheKey returns the response-cache key for a release
// group's releases.
func browseReleasesCacheKey(releaseGroupMBID string) string {
	return "mb:browse:releases:" + releaseGroupMBID
}

// BrowseReleasesCached returns a release group's releases from the local
// response cache only, never hitting the network.  The bool reports
// whether a fresh (unexpired) cache entry was found.  Used by the album
// page's local-first path so a cold fetch can be deferred to the
// background instead of blocking the request.
func (c *MusicBrainzClient) BrowseReleasesCached(
	releaseGroupMBID string,
) ([]MBRelease, bool) {
	data, ok := c.cache.Get(browseReleasesCacheKey(releaseGroupMBID))
	if !ok {
		return nil, false
	}

	var out []MBRelease
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, false
	}

	return out, true
}

// BrowseReleases fetches all releases (with recordings + media) for a
// release group, serving from the local response cache when warm and
// otherwise hitting MusicBrainz and caching the result.
func (c *MusicBrainzClient) BrowseReleases(
	ctx context.Context, releaseGroupMBID string,
) ([]MBRelease, error) {
	cacheKey := browseReleasesCacheKey(releaseGroupMBID)

	if out, ok := c.BrowseReleasesCached(releaseGroupMBID); ok {
		return out, nil
	}

	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	c.logger.Info("musicbrainz browse releases",
		"releaseGroupMBID", releaseGroupMBID,
	)

	result, err := c.mb.BrowseReleases(ctx,
		musicbrainzws2.ReleaseFilter{
			ReleaseGroupMBID: mbtypes.MBID(releaseGroupMBID),
			Includes:         []string{"recordings", "media"},
		},
		musicbrainzws2.Paginator{Limit: musicbrainzws2.MaxLimit},
	)
	if err != nil {
		return nil, err
	}

	out := convertReleases(result.Releases)

	c.cacheJSON(cacheKey, out, cacheTTLReleases, releaseGroupMBID, "release-group")

	return out, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// cacheJSON marshals v to JSON and stores it in the cache.
func (c *MusicBrainzClient) cacheJSON(
	key string,
	v any,
	ttl time.Duration,
	mbid string,
	entityType string,
) {
	data, err := json.Marshal(v)
	if err != nil {
		c.logger.Warn("musicbrainz cache marshal error",
			"key", key,
			"err", err,
		)

		return
	}

	c.cache.Set(key, data, ttl, mbid, entityType)
}

// clampLimit restricts the search limit to the MusicBrainz maximum.
func clampLimit(limit int) int {
	if limit <= 0 || limit > musicbrainzws2.MaxLimit {
		return musicbrainzws2.DefaultLimit
	}

	return limit
}

// ---------------------------------------------------------------------------
// Type converters (musicbrainzws2 → Wails wrapper types)
// ---------------------------------------------------------------------------

func convertArtist(a musicbrainzws2.Artist) MBArtist {
	out := MBArtist{
		MBID:           string(a.ID),
		Name:           a.Name,
		SortName:       a.SortName,
		Type:           a.Type,
		Country:        string(a.CountryCode),
		Disambiguation: a.Disambiguation,
		Score:          a.Score,
		OriginalScore:  a.Score,
	}

	// Extract the primary English alias when the canonical name
	// is non-Latin (CJK, Cyrillic, etc.).  This lets the frontend
	// show "Tatsuro Yamashita" alongside "山下達郎".
	if !isLatinScript(a.Name) {
		out.EnglishName = primaryEnglishAlias(a.Aliases)
	}

	return out
}

func convertArtists(artists []musicbrainzws2.Artist) []MBArtist {
	out := make([]MBArtist, len(artists))
	for i, a := range artists {
		out[i] = convertArtist(a)
	}

	return out
}

// primaryEnglishAlias returns the primary English alias name from
// a slice of aliases, or "" if none exists.
func primaryEnglishAlias(aliases []musicbrainzws2.Alias) string {
	// Prefer primary English alias.
	for _, a := range aliases {
		if a.Locale == "en" && a.IsPrimary {
			return a.Name
		}
	}

	// Fall back to any English alias.
	for _, a := range aliases {
		if a.Locale == "en" {
			return a.Name
		}
	}

	return ""
}

// isLatinScript returns true if the string consists primarily of
// Latin characters, digits, and common punctuation.  Returns false
// for CJK, Cyrillic, Arabic, etc.
func isLatinScript(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) && !unicode.In(r, unicode.Latin) {
			return false
		}
	}

	return true
}

func convertReleaseGroup(rg musicbrainzws2.ReleaseGroup) MBReleaseGroup {
	return MBReleaseGroup{
		MBID:             string(rg.ID),
		Title:            rg.Title,
		PrimaryType:      rg.PrimaryType,
		SecondaryTypes:   rg.SecondaryTypes,
		FirstReleaseDate: rg.FirstReleaseDate.String(),
		ArtistCredit:     rg.ArtistCredit.String(),
		Score:            rg.Score,
	}
}

func convertReleaseGroups(rgs []musicbrainzws2.ReleaseGroup) []MBReleaseGroup {
	out := make([]MBReleaseGroup, len(rgs))
	for i, rg := range rgs {
		out[i] = convertReleaseGroup(rg)
	}

	return out
}

func convertRelease(r musicbrainzws2.Release) MBRelease {
	rel := MBRelease{
		MBID:         string(r.ID),
		Title:        r.Title,
		Date:         r.Date.String(),
		Country:      string(r.CountryCode),
		Status:       r.Status,
		ArtistCredit: r.ArtistCredit.String(),
	}

	if r.ReleaseGroup != nil {
		rel.ReleaseGroupMBID = string(r.ReleaseGroup.ID)
	}

	for _, m := range r.Media {
		// Skip video media outright — DVD/Blu-ray bonus discs
		// inflate track counts and wreck track-count-based scoring
		// (beets ignores video/data tracks for the same reason).
		if isVideoFormat(m.Format) {
			continue
		}

		for _, t := range m.Tracks {
			// Same for individual video recordings on audio media.
			if t.Recording.IsVideo {
				continue
			}

			// Use the recording MBID, not the track MBID.  Tracks
			// and recordings have distinct MBIDs in MusicBrainz:
			// a track is the placement of a recording on a specific
			// medium/release, while a recording is the underlying
			// audio work.  Library-tagged audio files store the
			// recording MBID (MusicBrainz Track Id is a misnomer),
			// so that's what the local recordings.mbid column
			// contains — and that's what we need to match against
			// for the library-status indicator to be accurate.
			recordingMBID := string(t.Recording.ID)
			if recordingMBID == "" {
				// Fall back to the track MBID if the API response
				// didn't include the recording relation (older
				// browse endpoints).  Better than empty.
				recordingMBID = string(t.ID)
			}

			rel.Tracks = append(rel.Tracks, MBTrack{
				Position:   t.Position,
				DiscNumber: m.Position,
				Title:      t.Title,
				Length:     int(t.Length.Milliseconds()),
				MBID:       recordingMBID,
			})
		}
	}

	return rel
}

// isVideoFormat reports whether a medium's format string names a
// video carrier.  "DVD-Audio" stays audio; bare "DVD", "DVD-Video",
// "Blu-ray", "HD-DVD", "VHS", "VCD"/"SVCD" are video.
func isVideoFormat(format string) bool {
	f := strings.ToLower(format)

	if strings.Contains(f, "dvd-audio") {
		return false
	}

	for _, v := range []string{"dvd", "blu-ray", "bluray", "hd-dvd", "vhs", "vcd"} {
		if strings.Contains(f, v) {
			return true
		}
	}

	return false
}

func convertReleases(releases []musicbrainzws2.Release) []MBRelease {
	out := make([]MBRelease, len(releases))
	for i, r := range releases {
		out[i] = convertRelease(r)
	}

	return out
}

func convertRecording(r musicbrainzws2.Recording) MBRecording {
	return MBRecording{
		MBID:         string(r.ID),
		Title:        r.Title,
		Length:       int(r.Length.Milliseconds()),
		ArtistCredit: r.ArtistCredit.String(),
		Score:        r.Score,
	}
}

func convertRecordings(recordings []musicbrainzws2.Recording) []MBRecording {
	out := make([]MBRecording, len(recordings))
	for i, r := range recordings {
		out[i] = convertRecording(r)
	}

	return out
}
