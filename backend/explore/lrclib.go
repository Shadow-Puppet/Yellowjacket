package explore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	// lrclibBaseURL is the LRCLIB lyrics API.  LRCLIB is a free,
	// community-maintained lyrics database (plain + synced) with no
	// auth required.
	lrclibBaseURL = "https://lrclib.net"

	// lrclibUserAgent identifies the app per LRCLIB's guidelines.
	lrclibUserAgent = "YellowJacket (https://github.com/yellowjacket)"

	// lrclibRate is the requests-per-second budget for LRCLIB.  The
	// service has no hard published limit but asks clients to be
	// gentle; this keeps the library backfill polite.
	lrclibRate = 3
)

// ErrLyricsNotFound is returned when LRCLIB has no match for the
// requested track.
var ErrLyricsNotFound = errors.New("lyrics not found")

// Lyrics holds the plain and (optional) time-synced lyrics for a
// track, plus whether the track is marked instrumental.
type Lyrics struct {
	Plain        string `json:"plain"`
	Synced       string `json:"synced"`
	Instrumental bool   `json:"instrumental"`
}

// LRCLibClient is a thin, rate-limited, cached HTTP client for the
// LRCLIB lyrics API.
type LRCLibClient struct {
	http    *http.Client
	limiter *RateLimiter
	cache   *Cache
	logger  *slog.Logger
	baseURL string // overridable in tests
}

// NewLRCLibClient creates an LRCLIB client sharing the given cache.
func NewLRCLibClient(cache *Cache, logger *slog.Logger) *LRCLibClient {
	return &LRCLibClient{
		http:    &http.Client{Timeout: 20 * time.Second},
		limiter: NewRateLimiterN(lrclibRate),
		cache:   cache,
		logger:  logger,
		baseURL: lrclibBaseURL,
	}
}

// lrclibResponse is the wire shape of LRCLIB's /api/get response.
type lrclibResponse struct {
	ID           int64   `json:"id"`
	TrackName    string  `json:"trackName"`
	ArtistName   string  `json:"artistName"`
	AlbumName    string  `json:"albumName"`
	Duration     float64 `json:"duration"`
	Instrumental bool    `json:"instrumental"`
	PlainLyrics  string  `json:"plainLyrics"`
	SyncedLyrics string  `json:"syncedLyrics"`
}

// GetLyrics fetches lyrics for a track by artist, title, album, and
// duration (seconds; pass 0 if unknown).  LRCLIB matches on the
// metadata with a small duration tolerance.  Returns ErrLyricsNotFound
// when no match exists.  Successful and negative results are both
// cached so a repeated backfill doesn't re-hit the network.
func (c *LRCLibClient) GetLyrics(
	ctx context.Context, artist, title, album string, durationSec int,
) (*Lyrics, error) {
	if artist == "" || title == "" {
		return nil, ErrLyricsNotFound
	}

	q := url.Values{}
	q.Set("artist_name", artist)
	q.Set("track_name", title)

	if album != "" {
		q.Set("album_name", album)
	}

	if durationSec > 0 {
		q.Set("duration", strconv.Itoa(durationSec))
	}

	reqURL := c.baseURL + "/api/get?" + q.Encode()
	cacheKey := "lrclib:get:" + q.Encode()

	if data, ok := c.cache.Get(cacheKey); ok {
		return decodeCachedLyrics(data)
	}

	body, status, err := c.doGet(ctx, reqURL)
	if err != nil {
		return nil, fmt.Errorf("lrclib get: %w", err)
	}

	if status == http.StatusNotFound {
		// Cache the miss as a sentinel so we don't re-request it.
		c.cache.Set(cacheKey, []byte(lyricsMissSentinel), cacheTTLSearch, "", "lyrics")

		return nil, ErrLyricsNotFound
	}

	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("lrclib get: %w: status %d", ErrListenBrainzHTTP, status)
	}

	var wire lrclibResponse
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("lrclib get unmarshal: %w", err)
	}

	lyrics := &Lyrics{
		Plain:        wire.PlainLyrics,
		Synced:       wire.SyncedLyrics,
		Instrumental: wire.Instrumental,
	}

	// Persist the normalized result (not the raw wire body) so the
	// cached shape matches what callers expect.
	if encoded, err := json.Marshal(lyrics); err == nil {
		c.cache.Set(cacheKey, encoded, cacheTTLSearch, "", "lyrics")
	}

	return lyrics, nil
}

// lyricsMissSentinel marks a cached "no lyrics found" result.
const lyricsMissSentinel = "\x00miss"

// decodeCachedLyrics interprets a cached LRCLIB payload, mapping the
// miss sentinel back to ErrLyricsNotFound.
func decodeCachedLyrics(data []byte) (*Lyrics, error) {
	if string(data) == lyricsMissSentinel {
		return nil, ErrLyricsNotFound
	}

	var lyrics Lyrics
	if err := json.Unmarshal(data, &lyrics); err != nil {
		return nil, fmt.Errorf("lrclib cache decode: %w", err)
	}

	return &lyrics, nil
}

// doGet performs a rate-limited GET and returns the body and status.
// Unlike the ListenBrainz client, a 404 is a normal "no lyrics"
// outcome, so the status is returned rather than folded into an error.
func (c *LRCLibClient) doGet(ctx context.Context, reqURL string) ([]byte, int, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, 0, fmt.Errorf("rate limiter: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, 0, err
	}

	req.Header.Set("User-Agent", lrclibUserAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}

	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}

	return body, resp.StatusCode, nil
}
