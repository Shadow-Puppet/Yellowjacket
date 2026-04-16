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
	"strings"
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
// It checks three sources in order:
//  1. Local library cover art (instant, matched by album+artist name)
//  2. Disk cache from a previous CAA fetch (instant)
//  3. Cover Art Archive network fetch (slow, cached to disk)
type CoverArtProxy struct {
	db       *database.DB
	cacheDir string
	client   *http.Client
	limiter  *RateLimiter

	mu       sync.Mutex // serializes disk writes
	libOnce  sync.Once
	libIndex map[string]string // "album\x00artist" → cover art file path
}

// NewCoverArtProxy creates a proxy that checks the local library
// first and caches CAA thumbnails under the user data directory.
func NewCoverArtProxy(db *database.DB, limiter *RateLimiter) *CoverArtProxy {
	dir := ""

	dataDir, err := system.GetUserDataDirPath()
	if err == nil {
		dir = filepath.Join(dataDir, thumbnailDir)
		_ = os.MkdirAll(dir, 0o755)
	}

	return &CoverArtProxy{
		db:       db,
		cacheDir: dir,
		client:   &http.Client{Timeout: thumbnailTimeout},
		limiter:  limiter,
	}
}

// GetThumbnail returns a base64-encoded JPEG data URL for the given
// release group.  Checks local library art first (by name match),
// GetThumbnail returns a base64 data URL for an album's cover art.
// Checks local library art first, then disk cache, then fetches from CAA.
// Returns "" on failure.
//
// The mbid argument MUST be a release group MBID.  Track-level cover
// art (where you only have a release MBID) should be resolved by
// looking up the parent release group via SearchIndex first.
func (p *CoverArtProxy) GetThumbnail(
	releaseGroupMBID, albumName, artistName string,
) string {
	// Source 1+2: local library art + disk cache (instant).
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

// GetThumbnailCached checks only local library art and disk cache.
// Returns "" if not cached — does NOT fetch from the network.
func (p *CoverArtProxy) GetThumbnailCached(
	releaseGroupMBID, albumName, artistName string,
) string {
	// Source 1: local library cover art (instant).
	if albumName != "" {
		if dataURL := p.libraryArt(albumName, artistName); dataURL != "" {
			return dataURL
		}
	}

	if p.cacheDir == "" || releaseGroupMBID == "" {
		return ""
	}

	// Source 2: disk cache from previous CAA fetch (instant).
	return p.readCache(releaseGroupMBID)
}

// GetTrackThumbnail returns cover art for a track.  Tries, in order:
//  1. Local library art by album/artist name.
//  2. Disk cache for the release group MBID (shared with discography).
//  3. Disk cache for the release MBID (per-track fallback).
//  4. CAA network fetch on the release group (populates RG cache).
//  5. CAA network fetch on the release (populates release cache).
//
// Either or both MBIDs may be empty — whichever is present is tried.
// Release group is preferred because it shares the cache with the
// discography and top-releases sections; release is the fallback for
// tracks whose caa_release_mbid doesn't resolve to a known RG in the
// index (e.g. the track is on a release not fetched for that artist).
func (p *CoverArtProxy) GetTrackThumbnail(
	releaseMBID, releaseGroupMBID, albumName, artistName string,
) string {
	// Source 1: local library art (instant).
	if albumName != "" {
		if dataURL := p.libraryArt(albumName, artistName); dataURL != "" {
			return dataURL
		}
	}

	if p.cacheDir == "" {
		return ""
	}

	// Source 2: disk cache for release group (shared with discography).
	if releaseGroupMBID != "" {
		if cached := p.readCache(releaseGroupMBID); cached != "" {
			return cached
		}
	}

	// Source 3: disk cache for release (per-track fallback).
	if releaseMBID != "" {
		if cached := p.readCache(releaseMBID); cached != "" {
			return cached
		}
	}

	// Source 4: CAA network fetch on release group.
	if releaseGroupMBID != "" {
		url := CoverArtGroupURL(releaseGroupMBID)
		data, cacheable, err := p.fetch(url)

		if err == nil && len(data) > 0 {
			p.writeCache(releaseGroupMBID, data)

			return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(data)
		}

		if cacheable {
			// Mark RG miss so we don't re-fetch it, but fall through
			// to the release-level fallback.
			p.writeCache(releaseGroupMBID, nil)
		}
	}

	// Source 5: CAA network fetch on release (fallback).
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

// GetTrackThumbnailCached returns a cached track thumbnail without
// hitting the network.  Tries library art, then RG cache, then
// release cache.  Returns "" if nothing is cached.
func (p *CoverArtProxy) GetTrackThumbnailCached(
	releaseMBID, releaseGroupMBID, albumName, artistName string,
) string {
	if albumName != "" {
		if dataURL := p.libraryArt(albumName, artistName); dataURL != "" {
			return dataURL
		}
	}

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
// Source 1: local library art
// ---------------------------------------------------------------------------

// libraryArt returns a base64 data URL for the album if it exists
// in the local music library.  Matched by lowercased album name +
// artist name.
func (p *CoverArtProxy) libraryArt(albumName, artistName string) string {
	p.libOnce.Do(p.buildLibraryIndex)

	key := libraryArtKey(albumName, artistName)

	path, ok := p.libIndex[key]
	if !ok || path == "" {
		return ""
	}

	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return ""
	}

	mime := "image/jpeg"
	if strings.HasSuffix(strings.ToLower(path), ".png") {
		mime = "image/png"
	}

	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func (p *CoverArtProxy) buildLibraryIndex() {
	p.libIndex = make(map[string]string)

	if p.db == nil {
		return
	}

	rows, err := p.db.QueryContext(`
		SELECT rg.name, a.name, ca.file_path
		FROM release_groups rg
		JOIN artist_credit ac ON ac.id = rg.album_artist_credit_id
		JOIN artist_credit_artist aca ON aca.credit_id = ac.id
		JOIN artists a ON a.id = aca.artist_id
		LEFT JOIN cover_art ca ON ca.id = rg.cover_art_id
		WHERE ca.file_path IS NOT NULL AND ca.file_path != ''
	`)
	if err != nil {
		return
	}

	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var album, artist, path string
		if err := rows.Scan(&album, &artist, &path); err == nil {
			key := libraryArtKey(album, artist)
			p.libIndex[key] = path
		}
	}
}

func libraryArtKey(album, artist string) string {
	return strings.ToLower(album) + "\x00" + strings.ToLower(artist)
}

// ---------------------------------------------------------------------------
// Source 2+3: CAA disk cache and network fetch
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
