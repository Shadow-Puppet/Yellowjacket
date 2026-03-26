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

	// artistImageCacheTTL is how long resolved image URLs are cached.
	artistImageCacheTTL = 30 * 24 * time.Hour
)

// ArtistImageProvider resolves artist MBIDs to image URLs.  It
// fetches the artist's MB url-rels (once, cached 30 days), extracts
// image sources from them, and returns a Wikimedia Commons thumbnail
// URL.  Designed to be extended with additional sources (fanart.tv,
// etc.) by adding to the resolve chain.
type ArtistImageProvider struct {
	cache  *Cache
	client *http.Client
	logger *slog.Logger
}

// NewArtistImageProvider creates a provider that resolves artist
// images via MusicBrainz relationships and Wikidata.
func NewArtistImageProvider(
	cache *Cache,
	logger *slog.Logger,
) *ArtistImageProvider {
	return &ArtistImageProvider{
		cache:  cache,
		client: &http.Client{Timeout: artistImageTimeout},
		logger: logger,
	}
}

// GetArtistImageURL returns a Wikimedia Commons thumbnail URL for
// the given artist MBID, or "" if no image is available.  Results
// are cached for 30 days.
func (p *ArtistImageProvider) GetArtistImageURL(artistMBID string) string {
	if artistMBID == "" {
		return ""
	}

	// Check resolved URL cache first.
	cacheKey := "artist-image:" + artistMBID

	if data, ok := p.cache.Get(cacheKey); ok {
		return string(data)
	}

	// Fetch MB url-rels (cached separately, shared with other uses).
	rels := p.fetchMBRels(artistMBID)
	if rels == nil {
		p.cache.Set(cacheKey, []byte(""), artistImageCacheTTL, artistMBID, "artist")

		return ""
	}

	// Try each source in priority order.
	imageURL := p.fromDirectImageRel(rels)

	if imageURL == "" {
		imageURL = p.fromWikidataRel(rels)
	}

	// Cache the result (even "" = no image).
	p.cache.Set(cacheKey, []byte(imageURL), artistImageCacheTTL, artistMBID, "artist")

	if imageURL != "" {
		p.logger.Debug("artist image resolved",
			"mbid", artistMBID,
			"url", imageURL,
		)
	}

	return imageURL
}

// ---------------------------------------------------------------------------
// MB url-rels fetching (shared by all sources)
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
		"https://musicbrainz.org/ws/2/artist/%s?fmt=json&inc=url-rels",
		artistMBID,
	)

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
// Source 1: direct image relation (Commons wiki page link)
// ---------------------------------------------------------------------------

func (p *ArtistImageProvider) fromDirectImageRel(rels []mbRelation) string {
	for _, rel := range rels {
		if rel.Type != "image" {
			continue
		}

		resource := rel.URL.Resource

		// "https://commons.wikimedia.org/wiki/File:Name.jpg"
		if idx := strings.LastIndex(resource, "File:"); idx >= 0 {
			filename := resource[idx+5:]

			return wikimediaThumbURL(filename)
		}
	}

	return ""
}

// ---------------------------------------------------------------------------
// Source 2: Wikidata P18 property
// ---------------------------------------------------------------------------

func (p *ArtistImageProvider) fromWikidataRel(rels []mbRelation) string {
	// Find the wikidata Q-ID.
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

	// Check cache for this Wikidata entity.
	cacheKey := "wikidata-p18:" + qid

	if data, ok := p.cache.Get(cacheKey); ok {
		return string(data)
	}

	// Fetch P18 from Wikidata.
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
// Helpers
// ---------------------------------------------------------------------------

// wikimediaThumbURL constructs a Wikimedia Commons thumbnail URL
// from a filename using the MD5 directory bucketing scheme.
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
