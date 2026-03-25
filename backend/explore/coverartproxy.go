package explore

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"yellowjacket/backend/system"
)

// ErrCoverArt is returned when the Cover Art Archive responds
// with a non-200 status code.
var ErrCoverArt = errors.New("cover art fetch failed")

const (
	// thumbnailDir is the subdirectory under the user data dir
	// where cached cover art thumbnails are stored.
	thumbnailDir = "cover-art-cache"

	// thumbnailTimeout is the HTTP timeout for fetching a thumbnail.
	thumbnailTimeout = 10 * time.Second

	// thumbnailMaxSize is the maximum image size to cache (2 MB).
	thumbnailMaxSize = 2 * 1024 * 1024
)

// CoverArtProxy fetches and caches cover art thumbnails locally.
// Wails-bound methods return base64-encoded image data for display
// in <img src="data:..."> tags, eliminating browser HTTP requests
// to the slow Cover Art Archive.
type CoverArtProxy struct {
	cacheDir string
	client   *http.Client
	limiter  *RateLimiter
	mu       sync.Mutex // serializes disk writes
}

// NewCoverArtProxy creates a proxy that caches thumbnails under
// the user data directory.
func NewCoverArtProxy(limiter *RateLimiter) *CoverArtProxy {
	dir := ""

	dataDir, err := system.GetUserDataDirPath()
	if err == nil {
		dir = filepath.Join(dataDir, thumbnailDir)
		_ = os.MkdirAll(dir, 0o755)
	}

	return &CoverArtProxy{
		cacheDir: dir,
		client:   &http.Client{Timeout: thumbnailTimeout},
		limiter:  limiter,
	}
}

// GetThumbnail returns a base64-encoded JPEG data URL for the given
// release group MBID.  Returns from local cache if available,
// otherwise fetches from the Cover Art Archive.  Returns "" on
// failure (no cover art, network error, etc.).
func (p *CoverArtProxy) GetThumbnail(releaseGroupMBID string) string {
	if p.cacheDir == "" || releaseGroupMBID == "" {
		return ""
	}

	// Check disk cache.
	cached := p.readCache(releaseGroupMBID)
	if cached != "" {
		return cached
	}

	// Fetch from CAA.
	url := CoverArtGroupURL(releaseGroupMBID)
	data, err := p.fetch(url)

	if err != nil || len(data) == 0 {
		// Cache the miss as an empty file so we don't retry.
		p.writeCache(releaseGroupMBID, nil)

		return ""
	}

	p.writeCache(releaseGroupMBID, data)

	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(data)
}

func (p *CoverArtProxy) fetch(url string) ([]byte, error) {
	// Rate-limit CAA requests.
	ctx := context.Background()
	if err := p.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", lbUserAgent)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %d", ErrCoverArt, resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, thumbnailMaxSize))
	if err != nil {
		return nil, err
	}

	return data, nil
}

func (p *CoverArtProxy) cachePath(mbid string) string {
	return filepath.Join(p.cacheDir, mbid+".jpg")
}

func (p *CoverArtProxy) readCache(mbid string) string {
	path := p.cachePath(mbid)

	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	// Empty file = cached miss.
	if len(data) == 0 {
		return ""
	}

	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(data)
}

func (p *CoverArtProxy) writeCache(mbid string, data []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()

	path := p.cachePath(mbid)

	if data == nil {
		data = []byte{} // empty file = miss marker
	}

	_ = os.WriteFile(path, data, 0o644)
}
