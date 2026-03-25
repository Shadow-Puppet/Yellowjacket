package explore

import (
	"context"
	"crypto/md5" //nolint:gosec // MD5 used for Wikimedia URL hashing, not security
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// ErrArtistImage is returned when an artist image HTTP fetch fails.
var ErrArtistImage = errors.New("artist image fetch failed")

const (
	// wikimediaThumbBase is the base URL for Wikimedia Commons
	// thumbnail generation.
	wikimediaThumbBase = "https://upload.wikimedia.org/wikipedia/commons/thumb"

	// wikidataAPIBase is the base URL for the Wikidata API.
	wikidataAPIBase = "https://www.wikidata.org/w/api.php"

	// artistImageSize is the default thumbnail width in pixels.
	artistImageSize = 250

	// artistImageTimeout is the HTTP timeout for image URL lookups.
	artistImageTimeout = 10 * time.Second
)

// ArtistImageProvider resolves artist MBIDs to image URLs.  It
// checks multiple sources in priority order and caches results in
// the explore_cache.  Designed to be extended with additional
// sources (fanart.tv, etc.) by adding to the providers slice.
type ArtistImageProvider struct {
	mb     *MusicBrainzClient
	cache  *Cache
	client *http.Client
	logger *slog.Logger
}

// NewArtistImageProvider creates a provider that resolves artist
// images via MusicBrainz relationships and Wikidata.
func NewArtistImageProvider(
	mb *MusicBrainzClient,
	cache *Cache,
	logger *slog.Logger,
) *ArtistImageProvider {
	return &ArtistImageProvider{
		mb:     mb,
		cache:  cache,
		client: &http.Client{Timeout: artistImageTimeout},
		logger: logger,
	}
}

// GetArtistImageURL returns a Wikimedia Commons thumbnail URL for
// the given artist MBID, or "" if no image is available.  Results
// are cached in explore_cache with a 30-day TTL.
func (p *ArtistImageProvider) GetArtistImageURL(artistMBID string) string {
	if artistMBID == "" {
		return ""
	}

	cacheKey := "artist-image:" + artistMBID

	// Check cache.
	if data, ok := p.cache.Get(cacheKey); ok {
		return string(data)
	}

	// Resolve image URL.
	imageURL := p.resolve(artistMBID)

	// Cache the result (even empty string = no image found).
	cacheTTL := 30 * 24 * time.Hour
	p.cache.Set(cacheKey, []byte(imageURL), cacheTTL, artistMBID, "artist")

	return imageURL
}

// resolve tries each source in order and returns the first image
// URL found.
func (p *ArtistImageProvider) resolve(artistMBID string) string {
	// Source 1: MB direct image relation (Commons wiki page link).
	if url := p.fromMBImageRelation(artistMBID); url != "" {
		return url
	}

	// Source 2: MB wikidata relation → Wikidata P18 → Commons thumb.
	if url := p.fromWikidata(artistMBID); url != "" {
		return url
	}

	// No image found from any source.
	return ""
}

// ---------------------------------------------------------------------------
// Source 1: MB direct image relation
// ---------------------------------------------------------------------------

// fromMBImageRelation checks the artist's MB url-rels for a direct
// "image" type pointing to Wikimedia Commons.
func (p *ArtistImageProvider) fromMBImageRelation(artistMBID string) string {
	ctx := context.Background()

	artist, err := p.mb.LookupArtist(ctx, artistMBID)
	if err != nil || artist == nil {
		return ""
	}

	// The LookupArtist doesn't include rels in our current wrapper.
	// We need the raw MB data with url-rels. Check if there's a
	// cached response that includes relations.
	cacheKey := "mb:artist-rels:" + artistMBID

	if data, ok := p.cache.Get(cacheKey); ok {
		return p.parseImageFromRels(data)
	}

	// Fetch with url-rels included.
	url := fmt.Sprintf(
		"https://musicbrainz.org/ws/2/artist/%s?fmt=json&inc=url-rels",
		artistMBID,
	)

	body, err := p.fetchURL(ctx, url)
	if err != nil {
		return ""
	}

	// Cache the response.
	cacheTTL := 30 * 24 * time.Hour
	p.cache.Set(cacheKey, body, cacheTTL, artistMBID, "artist")

	return p.parseImageFromRels(body)
}

func (p *ArtistImageProvider) parseImageFromRels(data []byte) string {
	var mb struct {
		Relations []struct {
			Type string `json:"type"`
			URL  struct {
				Resource string `json:"resource"`
			} `json:"url"`
		} `json:"relations"`
	}

	if err := json.Unmarshal(data, &mb); err != nil {
		return ""
	}

	for _, rel := range mb.Relations {
		if rel.Type != "image" {
			continue
		}

		resource := rel.URL.Resource

		// Direct Commons file link: "https://commons.wikimedia.org/wiki/File:Name.jpg"
		if strings.Contains(resource, "commons.wikimedia.org/wiki/File:") {
			filename := resource[strings.LastIndex(resource, "File:")+5:]

			return wikimediaThumbURL(filename)
		}
	}

	return ""
}

// ---------------------------------------------------------------------------
// Source 2: Wikidata P18
// ---------------------------------------------------------------------------

// fromWikidata looks up the artist's Wikidata Q-ID from MB rels,
// then fetches the P18 (image) property from Wikidata.
func (p *ArtistImageProvider) fromWikidata(artistMBID string) string {
	// Get the wikidata Q-ID from cached MB rels.
	qid := p.getWikidataQID(artistMBID)
	if qid == "" {
		return ""
	}

	// Check cache for Wikidata image.
	cacheKey := "wikidata-image:" + qid

	if data, ok := p.cache.Get(cacheKey); ok {
		return string(data)
	}

	// Fetch P18 from Wikidata API.
	ctx := context.Background()
	url := fmt.Sprintf(
		"%s?action=wbgetclaims&entity=%s&property=P18&format=json",
		wikidataAPIBase, qid,
	)

	body, err := p.fetchURL(ctx, url)
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

	if err := json.Unmarshal(body, &wd); err != nil || len(wd.Claims.P18) == 0 {
		// Cache empty result.
		cacheTTL := 30 * 24 * time.Hour
		p.cache.Set(cacheKey, []byte(""), cacheTTL, "", "")

		return ""
	}

	filename := strings.ReplaceAll(wd.Claims.P18[0].Mainsnak.Datavalue.Value, " ", "_")
	thumbURL := wikimediaThumbURL(filename)

	cacheTTL := 30 * 24 * time.Hour
	p.cache.Set(cacheKey, []byte(thumbURL), cacheTTL, "", "")

	return thumbURL
}

func (p *ArtistImageProvider) getWikidataQID(artistMBID string) string {
	cacheKey := "mb:artist-rels:" + artistMBID

	data, ok := p.cache.Get(cacheKey)
	if !ok {
		// Need to fetch rels — fromMBImageRelation should have
		// populated this, but if not, fetch now.
		ctx := context.Background()
		url := fmt.Sprintf(
			"https://musicbrainz.org/ws/2/artist/%s?fmt=json&inc=url-rels",
			artistMBID,
		)

		var err error

		data, err = p.fetchURL(ctx, url)
		if err != nil {
			return ""
		}

		cacheTTL := 30 * 24 * time.Hour
		p.cache.Set(cacheKey, data, cacheTTL, artistMBID, "artist")
	}

	var mb struct {
		Relations []struct {
			Type string `json:"type"`
			URL  struct {
				Resource string `json:"resource"`
			} `json:"url"`
		} `json:"relations"`
	}

	if err := json.Unmarshal(data, &mb); err != nil {
		return ""
	}

	for _, rel := range mb.Relations {
		if rel.Type == "wikidata" {
			parts := strings.Split(rel.URL.Resource, "/")

			return parts[len(parts)-1]
		}
	}

	return ""
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// wikimediaThumbURL constructs a Wikimedia Commons thumbnail URL
// from a filename.  The URL scheme uses MD5 hashing of the filename
// for directory bucketing.
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

func (p *ArtistImageProvider) fetchURL(ctx context.Context, url string) ([]byte, error) {
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
