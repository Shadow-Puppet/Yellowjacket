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

	"yellowjacket/backend/database"
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
// It checks two sources in order:
//  1. Disk cache from a previous CAA fetch (instant)
//  2. Cover Art Archive network fetch (slow, cached to disk)
//
// The proxy used to also consult the local library's cover_art
// table by album/artist name, but that path conflated externally-
// fetched art with audio-file embedded ID3 art (the cover_art
// table writes both with is_embedded=true, so they're
// indistinguishable downstream).  For autotag review and explore
// browsing we want the canonical CAA cover, not whatever bytes
// happen to be tagged on a user's local file — so the library
// lookup was removed.  The user's own library views (cover grid,
// album page) still display embedded art via a separate code path
// that reads cover_art.file_path directly, which is fine because
// that's their library, not Explore's view of MB.
type CoverArtProxy struct {
	cacheDir string
	client   *http.Client
	limiter  *RateLimiter

	mu sync.Mutex // serializes disk writes
}

// NewCoverArtProxy creates a proxy that caches CAA thumbnails
// under the user data directory.  The db parameter is accepted
// for API stability but is no longer read; future cover-art
// logic that needs DB access can wire it back up.
func NewCoverArtProxy(_ *database.DB, limiter *RateLimiter) *CoverArtProxy {
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

// GetThumbnail returns a base64-encoded JPEG data URL for the
// given release group.  Checks the disk cache first; falls back
// to a CAA network fetch.  Returns "" on failure.  The albumName
// / artistName args are accepted for API stability and ignored
// (they used to drive a library-by-name lookup; see the proxy
// type comment for why that was removed).
//
// The mbid argument MUST be a release group MBID.  Track-level cover
// art (where you only have a release MBID) should be resolved by
// looking up the parent release group via SearchIndex first.
func (p *CoverArtProxy) GetThumbnail(
	releaseGroupMBID, albumName, artistName string,
) string {
	if cached := p.GetThumbnailCached(releaseGroupMBID, albumName, artistName); cached != "" {
		return cached
	}

	if p.cacheDir == "" || releaseGroupMBID == "" {
		return ""
	}

	// Source 3: fetch from Cover Art Archive (slow, cached to disk).
	url := CoverArtGroupURL(releaseGroupMBID)
	data, cacheable, err := p.fetch(url)

	if err != nil || len(data) == 0 {
		if cacheable {
			p.writeCache(releaseGroupMBID, nil)
		}

		return ""
	}

	p.writeCache(releaseGroupMBID, data)

	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(data)
}

// GetThumbnailCached returns the disk-cached cover art for the
// release group, or "" if it isn't on disk.  Does NOT fetch from
// the network.  albumName/artistName accepted for API stability
// and ignored — see the proxy type comment.
func (p *CoverArtProxy) GetThumbnailCached(
	releaseGroupMBID, _, _ string,
) string {
	if p.cacheDir == "" || releaseGroupMBID == "" {
		return ""
	}

	return p.readCache(releaseGroupMBID)
}

// GetTrackThumbnail returns cover art for a track.  Tries, in order:
//  1. Disk cache for the release group MBID (shared with discography).
//  2. Disk cache for the release MBID (per-track fallback).
//  3. CAA network fetch on the release group (populates RG cache).
//  4. CAA network fetch on the release (populates release cache).
//
// Either or both MBIDs may be empty — whichever is present is tried.
// Release group is preferred because it shares the cache with the
// discography and top-releases sections; release is the fallback for
// tracks whose caa_release_mbid doesn't resolve to a known RG in the
// index (e.g. the track is on a release not fetched for that artist).
//
// The albumName/artistName args are accepted for API stability and
// ignored — see the proxy type comment for why the library-by-name
// step was removed.
func (p *CoverArtProxy) GetTrackThumbnail(
	releaseMBID, releaseGroupMBID, _, _ string,
) string {
	return p.GetCandidateThumbnail(releaseMBID, releaseGroupMBID)
}

// GetCandidateThumbnail is the canonical CAA-only lookup: disk
// cache for the release group, disk cache for the release, then
// CAA network on each in turn.  Used everywhere the user is
// browsing or reviewing albums that aren't *their* library copy
// (autotag review, explore) — for those views we want the
// canonical CAA art, not whatever ID3 bytes happen to be tagged
// on a local file.  Returns "" when no art is available.
func (p *CoverArtProxy) GetCandidateThumbnail(
	releaseMBID, releaseGroupMBID string,
) string {
	if p.cacheDir == "" {
		return ""
	}

	// Disk cache for release group (shared with discography).
	if releaseGroupMBID != "" {
		if cached := p.readCache(releaseGroupMBID); cached != "" {
			return cached
		}
	}

	// Disk cache for release (per-track fallback).
	if releaseMBID != "" {
		if cached := p.readCache(releaseMBID); cached != "" {
			return cached
		}
	}

	// Network fetch on release group.
	if releaseGroupMBID != "" {
		url := CoverArtGroupURL(releaseGroupMBID)
		data, cacheable, err := p.fetch(url)

		if err == nil && len(data) > 0 {
			p.writeCache(releaseGroupMBID, data)

			return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(data)
		}

		if cacheable {
			p.writeCache(releaseGroupMBID, nil)
		}
	}

	// Network fetch on release (fallback).
	if releaseMBID != "" {
		url := CoverArtURL(releaseMBID)
		data, cacheable, err := p.fetch(url)

		if err != nil || len(data) == 0 {
			if cacheable {
				p.writeCache(releaseMBID, nil)
			}

			return ""
		}

		p.writeCache(releaseMBID, data)

		return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(data)
	}

	return ""
}

// GetTrackThumbnailCached returns the disk-cached track thumbnail
// (RG MBID first, then release MBID).  Returns "" when nothing is
// cached.  Does NOT fetch from the network.  albumName/artistName
// accepted for API stability and ignored — see the proxy type
// comment.
func (p *CoverArtProxy) GetTrackThumbnailCached(
	releaseMBID, releaseGroupMBID, _, _ string,
) string {
	if p.cacheDir == "" {
		return ""
	}

	if releaseGroupMBID != "" {
		if cached := p.readCache(releaseGroupMBID); cached != "" {
			return cached
		}
	}

	if releaseMBID != "" {
		if cached := p.readCache(releaseMBID); cached != "" {
			return cached
		}
	}

	return ""
}

// ---------------------------------------------------------------------------
// CAA disk cache and network fetch
// ---------------------------------------------------------------------------

func (p *CoverArtProxy) fetch(url string) ([]byte, bool, error) {
	ctx := context.Background()
	if err := p.limiter.Wait(ctx); err != nil {
		return nil, false, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, err
	}

	req.Header.Set("User-Agent", lbUserAgent)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, false, err
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, true, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("%w: %d", ErrCoverArt, resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, thumbnailMaxSize))
	if err != nil {
		return nil, false, err
	}

	return data, true, nil
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
		data = []byte{}
	}

	_ = os.WriteFile(path, data, 0o644)
}
