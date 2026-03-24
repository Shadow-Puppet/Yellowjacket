package explore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
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
		"%s/1/explore/similar-artists/%s",
		listenBrainzBaseURL,
		artistMBID,
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

	var out []LBSimilarArtist
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("listenbrainz similar artists unmarshal: %w", err)
	}

	c.cacheJSON(cacheKey, out, cacheTTLEntity, artistMBID, "artist")

	return out, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// doGet performs a rate-limited GET request and returns the response
// body.  Non-2xx status codes are returned as errors.
func (c *ListenBrainzClient) doGet(
	ctx context.Context, url string,
) ([]byte, error) {
	c.logger.Debug("listenbrainz rate limiter wait", "url", url)

	if err := c.limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limiter: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", lbUserAgent)

	c.logger.Info("listenbrainz request",
		"method", http.MethodGet,
		"url", url,
	)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}

	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	c.logger.Info("listenbrainz response",
		"url", url,
		"status", resp.StatusCode,
	)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf(
			"%w: %d %s", ErrListenBrainzHTTP, resp.StatusCode, truncateBody(body),
		)
	}

	return body, nil
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
