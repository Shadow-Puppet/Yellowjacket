package explore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"
)

const (
	listenBrainzBaseURL = "https://api.listenbrainz.org"
	lbUserAgent         = "YellowJacket/dev"
)

// ErrListenBrainzHTTP is returned when the ListenBrainz API
// responds with a non-2xx status code.
var ErrListenBrainzHTTP = errors.New("listenbrainz HTTP error")

// ListenBrainzClient is a thin HTTP client for the ListenBrainz
// popularity and labs APIs.  All requests are rate-limited via the
// shared RateLimiter and cached via the shared Cache.
type ListenBrainzClient struct {
	http    *http.Client
	limiter *RateLimiter
	cache   *Cache
	logger  *slog.Logger
}

// NewListenBrainzClient creates a ListenBrainz API client.
func NewListenBrainzClient(
	limiter *RateLimiter,
	cache *Cache,
	logger *slog.Logger,
) *ListenBrainzClient {
	return &ListenBrainzClient{
		http:    &http.Client{Timeout: 30 * time.Second},
		limiter: limiter,
		cache:   cache,
		logger:  logger,
	}
}

// TopRecordingsForArtist returns the most-listened recordings for
// the artist identified by artistMBID.
func (c *ListenBrainzClient) TopRecordingsForArtist(
	ctx context.Context, artistMBID string,
) ([]LBTopRecording, error) {
	url := fmt.Sprintf(
		"%s/1/popularity/top-recordings-for-artist/%s",
		listenBrainzBaseURL,
		artistMBID,
	)
	cacheKey := "lb:top-recordings:" + artistMBID

	if data, ok := c.cache.Get(cacheKey); ok {
		var out []LBTopRecording
		if err := json.Unmarshal(data, &out); err == nil {
			return out, nil
		}
	}

	body, err := c.doGet(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("listenbrainz top recordings: %w", err)
	}

	// The API returns snake_case JSON — unmarshal into wire type,
	// then convert to the camelCase Wails type.
	var wire []lbTopRecordingWire
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("listenbrainz top recordings unmarshal: %w", err)
	}

	const maxTopRecordings = 10

	limit := len(wire)
	if limit > maxTopRecordings {
		limit = maxTopRecordings
	}

	out := make([]LBTopRecording, limit)
	for i := range limit {
		out[i] = wire[i].toPublic()
	}

	c.cacheJSON(cacheKey, out, cacheTTLSearch, artistMBID, "artist")

	return out, nil
}

// SimilarArtists returns artists similar to the one identified by
// artistMBID, using the ListenBrainz labs API.  Returns nil, nil
// if the endpoint is unavailable (labs API may be unstable).
func (c *ListenBrainzClient) SimilarArtists(
	ctx context.Context, artistMBID string,
) ([]LBSimilarArtist, error) {
	url := fmt.Sprintf(
		"%s/similar-artists/json?artist_mbids=%s&algorithm=%s",
		labsBaseURL,
		artistMBID,
		labsSimilarAlgorithm,
	)
	cacheKey := "lb:similar-artists:" + artistMBID

	if data, ok := c.cache.Get(cacheKey); ok {
		var out []LBSimilarArtist
		if err := json.Unmarshal(data, &out); err == nil {
			return out, nil
		}
	}

	body, err := c.doGet(ctx, url)
	if err != nil {
		// Labs API may be unstable — log and return empty.
		c.logger.Warn("listenbrainz similar artists unavailable",
			"artistMBID", artistMBID,
			"err", err,
		)

		return nil, nil //nolint:nilnil // graceful degradation for unstable endpoint
	}

	// Labs API returns snake_case — unmarshal into wire type,
	// then convert to camelCase Wails type.
	var wire []lbSimilarArtistWire
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("listenbrainz similar artists unmarshal: %w", err)
	}

	out := make([]LBSimilarArtist, len(wire))
	for i, w := range wire {
		out[i] = LBSimilarArtist{
			ArtistMBID: w.ArtistMBID,
			Name:       w.Name,
			Score:      float64(w.Score),
		}
	}

	c.cacheJSON(cacheKey, out, cacheTTLEntity, artistMBID, "artist")

	return out, nil
}

// ---------------------------------------------------------------------------
// Bulk popularity lookups (POST endpoints)
// ---------------------------------------------------------------------------

// lbPopularityResult is the response shape for all three bulk
// popularity endpoints.  The JSON field names are snake_case from
// the ListenBrainz API.
type lbPopularityResult struct {
	MBID             string `json:"artist_mbid"`
	RecordingMBID    string `json:"recording_mbid"`
	ReleaseGroupMBID string `json:"release_group_mbid"`
	TotalListenCount *int   `json:"total_listen_count"`
	TotalUserCount   *int   `json:"total_user_count"`
}

// ArtistPopularity fetches total listen counts for a batch of
// artist MBIDs.  Returns a map[mbid]→listenCount.  Artists with
// null counts (unknown to LB) are omitted from the map.
func (c *ListenBrainzClient) ArtistPopularity(
	ctx context.Context, mbids []string,
) (map[string]int, error) {
	if len(mbids) == 0 {
		return nil, nil //nolint:nilnil
	}

	url := listenBrainzBaseURL + "/1/popularity/artist"
	cacheKey := "lb:pop:artist:" + hashMBIDs(mbids)

	if data, ok := c.cache.Get(cacheKey); ok {
		var out map[string]int
		if err := json.Unmarshal(data, &out); err == nil {
			return out, nil
		}
	}

	body, err := c.doPost(ctx, url, map[string][]string{
		"artist_mbids": mbids,
	})
	if err != nil {
		return nil, fmt.Errorf("artist popularity: %w", err)
	}

	return c.parsePopularity(cacheKey, body, func(r lbPopularityResult) string {
		return r.MBID
	})
}

// RecordingPopularity fetches total listen counts for a batch of
// recording MBIDs.  Returns a map[mbid]→listenCount.
func (c *ListenBrainzClient) RecordingPopularity(
	ctx context.Context, mbids []string,
) (map[string]int, error) {
	if len(mbids) == 0 {
		return nil, nil //nolint:nilnil
	}

	url := listenBrainzBaseURL + "/1/popularity/recording"
	cacheKey := "lb:pop:recording:" + hashMBIDs(mbids)

	if data, ok := c.cache.Get(cacheKey); ok {
		var out map[string]int
		if err := json.Unmarshal(data, &out); err == nil {
			return out, nil
		}
	}

	body, err := c.doPost(ctx, url, map[string][]string{
		"recording_mbids": mbids,
	})
	if err != nil {
		return nil, fmt.Errorf("recording popularity: %w", err)
	}

	return c.parsePopularity(cacheKey, body, func(r lbPopularityResult) string {
		return r.RecordingMBID
	})
}

// ReleaseGroupPopularity fetches total listen counts for a batch of
// release group MBIDs.  Returns a map[mbid]→listenCount.
func (c *ListenBrainzClient) ReleaseGroupPopularity(
	ctx context.Context, mbids []string,
) (map[string]int, error) {
	if len(mbids) == 0 {
		return nil, nil //nolint:nilnil
	}

	url := listenBrainzBaseURL + "/1/popularity/release-group"
	cacheKey := "lb:pop:release-group:" + hashMBIDs(mbids)

	if data, ok := c.cache.Get(cacheKey); ok {
		var out map[string]int
		if err := json.Unmarshal(data, &out); err == nil {
			return out, nil
		}
	}

	body, err := c.doPost(ctx, url, map[string][]string{
		"release_group_mbids": mbids,
	})
	if err != nil {
		return nil, fmt.Errorf("release group popularity: %w", err)
	}

	return c.parsePopularity(cacheKey, body, func(r lbPopularityResult) string {
		return r.ReleaseGroupMBID
	})
}

// parsePopularity unmarshals a bulk popularity response, extracts
// the MBID→listenCount mapping, caches it, and returns it.
func (c *ListenBrainzClient) parsePopularity(
	cacheKey string,
	body []byte,
	extractMBID func(lbPopularityResult) string,
) (map[string]int, error) {
	var raw []lbPopularityResult
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("popularity unmarshal: %w", err)
	}

	out := make(map[string]int, len(raw))

	for _, r := range raw {
		mbid := extractMBID(r)
		if mbid != "" && r.TotalListenCount != nil {
			out[mbid] = *r.TotalListenCount
		}
	}

	c.cacheJSON(cacheKey, out, cacheTTLSearch, "", "")

	return out, nil
}

// hashMBIDs produces a short deterministic key from a slice of
// MBIDs by sorting and hashing.  Used for cache keys.
func hashMBIDs(mbids []string) string {
	sorted := make([]string, len(mbids))
	copy(sorted, mbids)
	slices.Sort(sorted)

	h := sha256.Sum256([]byte(strings.Join(sorted, "|")))

	return hex.EncodeToString(h[:8])
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// doGet performs a rate-limited GET request and returns the response
// body.  Non-2xx status codes are returned as errors.
func (c *ListenBrainzClient) doGet(
	ctx context.Context, url string,
) ([]byte, error) {
	return c.doRequest(ctx, http.MethodGet, url, nil)
}

// doPost performs a rate-limited POST request with a JSON body and
// returns the response body.
func (c *ListenBrainzClient) doPost(
	ctx context.Context, url string, body any,
) ([]byte, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal POST body: %w", err)
	}

	return c.doRequest(ctx, http.MethodPost, url, payload)
}

// doRequest is the shared HTTP helper for GET and POST.
func (c *ListenBrainzClient) doRequest(
	ctx context.Context, method string, url string, body []byte,
) ([]byte, error) {
	c.logger.Debug("listenbrainz rate limiter wait", "url", url)

	if err := c.limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limiter: %w", err)
	}

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", lbUserAgent)

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	c.logger.Info("listenbrainz request",
		"method", method,
		"url", url,
	)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}

	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	c.logger.Info("listenbrainz response",
		"url", url,
		"status", resp.StatusCode,
	)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf(
			"%w: %d %s", ErrListenBrainzHTTP, resp.StatusCode, truncateBody(respBody),
		)
	}

	return respBody, nil
}

// cacheJSON marshals v to JSON and stores it in the cache.
func (c *ListenBrainzClient) cacheJSON(
	key string,
	v any,
	ttl time.Duration,
	mbid string,
	entityType string,
) {
	data, err := json.Marshal(v)
	if err != nil {
		c.logger.Warn("listenbrainz cache marshal error",
			"key", key,
			"err", err,
		)

		return
	}

	c.cache.Set(key, data, ttl, mbid, entityType)
}

// truncateBody returns the first 200 bytes of an error response
// for diagnostic logging.
func truncateBody(body []byte) string {
	const maxLen = 200

	if len(body) <= maxLen {
		return string(body)
	}

	return string(body[:maxLen]) + "…"
}
