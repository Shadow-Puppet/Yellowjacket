package explore

import (
	"context"
	"encoding/json"
	"log/slog"
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
func NewMusicBrainzClient(cache *Cache, limiter *RateLimiter, logger *slog.Logger) *MusicBrainzClient {
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
// query string.  Results are cached for 1 day.
func (c *MusicBrainzClient) SearchArtists(
	ctx context.Context, query string, limit int,
) ([]MBArtist, error) {
	cacheKey := "mb:search:artist:" + query

	if data, ok := c.cache.Get(cacheKey); ok {
		var out []MBArtist
		if err := json.Unmarshal(data, &out); err == nil {
			return out, nil
		}
	}

	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
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
		return nil, err
	}

	out := convertArtists(result.Artists)

	c.cacheJSON(cacheKey, out, cacheTTLSearch, "", "")

	return out, nil
}

// SearchReleaseGroups queries MusicBrainz for release groups
// matching the given query string.
func (c *MusicBrainzClient) SearchReleaseGroups(
	ctx context.Context, query string, limit int,
) ([]MBReleaseGroup, error) {
	cacheKey := "mb:search:release-group:" + query

	if data, ok := c.cache.Get(cacheKey); ok {
		var out []MBReleaseGroup
		if err := json.Unmarshal(data, &out); err == nil {
			return out, nil
		}
	}

	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
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
		return nil, err
	}

	out := convertReleaseGroups(result.ReleaseGroups)

	c.cacheJSON(cacheKey, out, cacheTTLSearch, "", "")

	return out, nil
}

// SearchRecordings queries MusicBrainz for recordings matching the
// given query string.
func (c *MusicBrainzClient) SearchRecordings(
	ctx context.Context, query string, limit int,
) ([]MBRecording, error) {
	cacheKey := "mb:search:recording:" + query

	if data, ok := c.cache.Get(cacheKey); ok {
		var out []MBRecording
		if err := json.Unmarshal(data, &out); err == nil {
			return out, nil
		}
	}

	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
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
		return nil, err
	}

	out := convertRecordings(result.Recordings)

	c.cacheJSON(cacheKey, out, cacheTTLSearch, "", "")

	return out, nil
}

// ---------------------------------------------------------------------------
// Lookup
// ---------------------------------------------------------------------------

// LookupArtist fetches a single artist by MBID.  Cached for 7 days.
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
		musicbrainzws2.IncludesFilter{},
	)
	if err != nil {
		return nil, err
	}

	out := convertArtist(a)

	c.cacheJSON(cacheKey, out, cacheTTLEntity, mbid, "artist")

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

// BrowseReleases fetches the releases for a given release group
// MBID, including media/track information.  Cached for 7 days.
func (c *MusicBrainzClient) BrowseReleases(
	ctx context.Context, releaseGroupMBID string,
) ([]MBRelease, error) {
	cacheKey := "mb:browse:releases:" + releaseGroupMBID

	if data, ok := c.cache.Get(cacheKey); ok {
		var out []MBRelease
		if err := json.Unmarshal(data, &out); err == nil {
			return out, nil
		}
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

	c.cacheJSON(cacheKey, out, cacheTTLEntity, releaseGroupMBID, "release-group")

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
		MBID:    string(r.ID),
		Title:   r.Title,
		Date:    r.Date.String(),
		Country: string(r.CountryCode),
		Status:  r.Status,
	}

	for _, m := range r.Media {
		for _, t := range m.Tracks {
			rel.Tracks = append(rel.Tracks, MBTrack{
				Position:   t.Position,
				DiscNumber: m.Position,
				Title:      t.Title,
				Length:     int(t.Length.Milliseconds()),
				MBID:       string(t.ID),
			})
		}
	}

	return rel
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
