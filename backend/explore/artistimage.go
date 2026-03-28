package explore

import (
	"context"
	"crypto/md5" //nolint:gosec // MD5 used for Wikimedia URL hashing, not security
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"yellowjacket/backend/database"
	"yellowjacket/backend/system"
)

// ErrArtistImage is returned when an artist image HTTP fetch fails.
var ErrArtistImage = errors.New("artist image fetch failed")

const (
	wikimediaThumbBase  = "https://upload.wikimedia.org/wikipedia/commons/thumb"
	wikidataAPIBase     = "https://www.wikidata.org/w/api.php"
	artistImageSize     = 250
	artistImageTimeout  = 10 * time.Second
	artistImageCacheTTL = 30 * 24 * time.Hour
	artistImageDir      = "artist-image-cache"
	artistImageMaxBytes = 2 * 1024 * 1024 // 2 MB max per image
)

// ArtistImageProvider resolves artist MBIDs to images.  It checks
// three sources in order:
//  1. Local disk cache (instant, from previous fetch)
//  2. MB url-rels → Wikimedia Commons thumb URL → fetch + cache
//  3. Wikidata P18 → Wikimedia Commons thumb URL → fetch + cache
//
// Returns base64 data URLs for display in <img src="data:...">.
type ArtistImageProvider struct {
	db        *database.DB
	cache     *Cache
	mbLimiter *RateLimiter
	client    *http.Client
	logger    *slog.Logger
	imageDir  string
	mu        sync.Mutex // serializes disk writes
}

// NewArtistImageProvider creates a provider that resolves and caches
// artist images.
func NewArtistImageProvider(
	db *database.DB,
	cache *Cache,
	mbLimiter *RateLimiter,
	logger *slog.Logger,
) *ArtistImageProvider {
	dir := ""

	dataDir, err := system.GetUserDataDirPath()
	if err == nil {
		dir = filepath.Join(dataDir, artistImageDir)
		_ = os.MkdirAll(dir, 0o755)
	}

	return &ArtistImageProvider{
		db:        db,
		cache:     cache,
		mbLimiter: mbLimiter,
		client:    &http.Client{Timeout: artistImageTimeout},
		logger:    logger,
		imageDir:  dir,
	}
}

// GetArtistImage returns a base64 data URL for the artist's photo.
// Checks disk cache first, then resolves via MB/Wikidata and fetches
// the image from Wikimedia Commons.  Returns "" if no image.
func (p *ArtistImageProvider) GetArtistImage(artistMBID string) string {
	if artistMBID == "" || p.imageDir == "" {
		return ""
	}

	// Source 1: disk cache (instant).
	if dataURL := p.readDiskCache(artistMBID); dataURL != "" {
		return dataURL
	}

	// Check if we already know there's no image (cached miss marker).
	if p.isDiskCacheMiss(artistMBID) {
		return ""
	}

	// Source 2+3: resolve URL then fetch image.
	imageURL := p.resolveURL(artistMBID)
	if imageURL == "" {
		p.writeDiskCache(artistMBID, nil) // miss marker

		return ""
	}

	// Fetch the actual image bytes.
	data, err := p.fetchImageBytes(imageURL)
	if err != nil || len(data) == 0 {
		p.writeDiskCache(artistMBID, nil)

		return ""
	}

	p.writeDiskCache(artistMBID, data)

	return toDataURL(data, artistMBID)
}

// resolveURL finds the Wikimedia Commons thumbnail URL for an
// artist via MB url-rels and Wikidata.  The URL itself (not image
// bytes) is cached in explore_cache for 30 days.
func (p *ArtistImageProvider) resolveURL(artistMBID string) string {
	cacheKey := "artist-image-url:" + artistMBID

	if data, ok := p.cache.Get(cacheKey); ok {
		return string(data)
	}

	rels := p.fetchMBRels(artistMBID)
	if rels == nil {
		p.cache.Set(cacheKey, []byte(""), artistImageCacheTTL, artistMBID, "artist")

		return ""
	}

	imageURL := p.fromDirectImageRel(rels)

	if imageURL == "" {
		imageURL = p.fromWikidataRel(rels)
	}

	p.cache.Set(cacheKey, []byte(imageURL), artistImageCacheTTL, artistMBID, "artist")

	return imageURL
}

// ---------------------------------------------------------------------------
// MB url-rels
// ---------------------------------------------------------------------------

type mbRelation struct {
	Type string `json:"type"`
	URL  struct {
		Resource string `json:"resource"`
	} `json:"url"`
}

func (p *ArtistImageProvider) fetchMBRels(artistMBID string) []mbRelation {
	cacheKey := "mb:artist-rels:" + artistMBID

	if data, ok := p.cache.Get(cacheKey); ok {
		var envelope struct {
			Relations []mbRelation `json:"relations"`
		}

		if err := json.Unmarshal(data, &envelope); err == nil {
			return envelope.Relations
		}
	}

	url := fmt.Sprintf(
		"https://musicbrainz.org/ws/2/artist/%s?fmt=json&inc=url-rels+aliases",
		artistMBID,
	)

	if err := p.mbLimiter.Wait(context.Background()); err != nil {
		return nil
	}

	body, err := p.fetchURL(url)
	if err != nil {
		p.logger.Debug("artist image: MB rels fetch failed",
			"mbid", artistMBID,
			"error", err,
		)

		return nil
	}

	p.cache.Set(cacheKey, body, artistImageCacheTTL, artistMBID, "artist")

	var envelope struct {
		Relations []mbRelation `json:"relations"`
	}

	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil
	}

	return envelope.Relations
}

// ---------------------------------------------------------------------------
// Source 1: direct image relation
// ---------------------------------------------------------------------------

func (p *ArtistImageProvider) fromDirectImageRel(rels []mbRelation) string {
	for _, rel := range rels {
		if rel.Type != "image" {
			continue
		}

		resource := rel.URL.Resource

		if idx := strings.LastIndex(resource, "File:"); idx >= 0 {
			filename := resource[idx+5:]

			return wikimediaThumbURL(filename)
		}
	}

	return ""
}

// ---------------------------------------------------------------------------
// Source 2: Wikidata P18
// ---------------------------------------------------------------------------

func (p *ArtistImageProvider) fromWikidataRel(rels []mbRelation) string {
	qid := ""

	for _, rel := range rels {
		if rel.Type == "wikidata" {
			parts := strings.Split(rel.URL.Resource, "/")
			qid = parts[len(parts)-1]

			break
		}
	}

	if qid == "" {
		return ""
	}

	cacheKey := "wikidata-p18:" + qid

	if data, ok := p.cache.Get(cacheKey); ok {
		return string(data)
	}

	url := fmt.Sprintf(
		"%s?action=wbgetclaims&entity=%s&property=P18&format=json",
		wikidataAPIBase, qid,
	)

	body, err := p.fetchURL(url)
	if err != nil {
		return ""
	}

	var wd struct {
		Claims struct {
			P18 []struct {
				Mainsnak struct {
					Datavalue struct {
						Value string `json:"value"`
					} `json:"datavalue"`
				} `json:"mainsnak"`
			} `json:"P18"`
		} `json:"claims"`
	}

	thumbURL := ""

	if err := json.Unmarshal(body, &wd); err == nil && len(wd.Claims.P18) > 0 {
		filename := strings.ReplaceAll(wd.Claims.P18[0].Mainsnak.Datavalue.Value, " ", "_")
		thumbURL = wikimediaThumbURL(filename)
	}

	p.cache.Set(cacheKey, []byte(thumbURL), artistImageCacheTTL, "", "")

	return thumbURL
}

// ---------------------------------------------------------------------------
// Disk cache
// ---------------------------------------------------------------------------

func (p *ArtistImageProvider) diskCachePath(mbid string) string {
	return filepath.Join(p.imageDir, mbid+".jpg")
}

func (p *ArtistImageProvider) readDiskCache(mbid string) string {
	data, err := os.ReadFile(p.diskCachePath(mbid))
	if err != nil {
		return ""
	}

	if len(data) == 0 {
		return "" // miss marker
	}

	return toDataURL(data, mbid)
}

func (p *ArtistImageProvider) isDiskCacheMiss(mbid string) bool {
	info, err := os.Stat(p.diskCachePath(mbid))

	return err == nil && info.Size() == 0
}

func (p *ArtistImageProvider) writeDiskCache(mbid string, data []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if data == nil {
		data = []byte{} // miss marker
	}

	_ = os.WriteFile(p.diskCachePath(mbid), data, 0o644)
}

// ---------------------------------------------------------------------------
// Image fetching
// ---------------------------------------------------------------------------

func (p *ArtistImageProvider) fetchImageBytes(imageURL string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), artistImageTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
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
		return nil, fmt.Errorf("%w: HTTP %d", ErrArtistImage, resp.StatusCode)
	}

	return io.ReadAll(io.LimitReader(resp.Body, artistImageMaxBytes))
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func wikimediaThumbURL(filename string) string {
	if filename == "" {
		return ""
	}

	filename = strings.ReplaceAll(filename, " ", "_")

	hash := fmt.Sprintf("%x", md5.Sum([]byte(filename))) //nolint:gosec
	h1 := string(hash[0])
	h2 := hash[:2]

	return fmt.Sprintf("%s/%s/%s/%s/%dpx-%s",
		wikimediaThumbBase, h1, h2, filename, artistImageSize, filename,
	)
}

// GetAliases returns the artist's aliases as a space-separated
// string, extracted from the cached MB rels response.  Returns ""
// if no aliases are cached.
func (p *ArtistImageProvider) GetAliases(artistMBID string) string {
	cacheKey := "mb:artist-rels:" + artistMBID

	data, ok := p.cache.Get(cacheKey)
	if !ok {
		return ""
	}

	var envelope struct {
		Aliases []struct {
			Name string `json:"name"`
		} `json:"aliases"`
	}

	if err := json.Unmarshal(data, &envelope); err != nil || len(envelope.Aliases) == 0 {
		return ""
	}

	names := make([]string, 0, len(envelope.Aliases))

	for _, a := range envelope.Aliases {
		if a.Name != "" {
			names = append(names, a.Name)
		}
	}

	return strings.Join(names, " ")
}

func (p *ArtistImageProvider) fetchURL(url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), artistImageTimeout)
	defer cancel()

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
		return nil, fmt.Errorf("%w: HTTP %d", ErrArtistImage, resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

func toDataURL(data []byte, _ string) string {
	mime := "image/jpeg"
	if len(data) > 1 && data[0] == 0x89 && data[1] == 0x50 {
		mime = "image/png"
	}

	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}
