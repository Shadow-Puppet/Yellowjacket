package explore

import (
	"context"
	"crypto/md5" //nolint:gosec // MD5 for Wikimedia URL hashing
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png" // register PNG decoder
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/image/draw"

	"yellowjacket/backend/database"
	"yellowjacket/backend/system"
)

// ErrArtistImage is returned when an artist image HTTP fetch fails.
var ErrArtistImage = errors.New("artist image fetch failed")

const (
	wikimediaThumbBase      = "https://upload.wikimedia.org/wikipedia/commons/thumb"
	wikidataAPIBase         = "https://www.wikidata.org/w/api.php"
	wikipediaAPIBase        = "https://en.wikipedia.org/w/api.php"
	fanartTVAPIBase         = "https://webservice.fanart.tv/v3/music"
	audioDBAPIBase          = "https://www.theaudiodb.com/api/v1/json/2"
	artistImageTimeout      = 10 * time.Second
	artistImageCacheTTL     = 365 * 24 * time.Hour // positive results: ~permanent
	artistImageMissCacheTTL = 30 * 24 * time.Hour  // negative results: retry monthly
	artistImageBaseDir      = "artist-images"
	artistImageMaxBytes     = 2 * 1024 * 1024
	artistImageMaxSize      = 500 // max dimension for stored full-res images
	maxImagesPerArtist      = 10
)

// fanartTVProjectKey is the project API key for fanart.tv.
// Set via -ldflags at build time, or FANART_TV_API_KEY env var.
// Users can provide their own personal key via FANART_TV_PERSONAL_KEY.
// Per fanart.tv terms: images are CC-BY-SA, attribution required.
//
//nolint:gochecknoglobals
var fanartTVProjectKey = ""

// artistImageTier defines a thumbnail size variant.
type artistImageTier struct {
	Suffix  string
	MaxSize int
	Quality int
}

var artistImageTiers = []artistImageTier{
	{Suffix: "_sm", MaxSize: 100, Quality: 75},
	{Suffix: "_md", MaxSize: 200, Quality: 80},
	{Suffix: "_lg", MaxSize: 400, Quality: 85},
}

// ArtistImageProvider resolves, fetches, and caches artist images
// from multiple sources.  Stores up to 10 images per artist with
// sm/md/lg thumbnails for the primary image.
type ArtistImageProvider struct {
	db           *database.DB
	cache        *Cache
	mbLimiter    *RateLimiter
	client       *http.Client
	logger       *slog.Logger
	baseDir      string
	fanartAPIKey string // resolved project key + optional personal key
}

// NewArtistImageProvider creates a multi-source artist image provider.
func NewArtistImageProvider(
	db *database.DB,
	cache *Cache,
	mbLimiter *RateLimiter,
	logger *slog.Logger,
) *ArtistImageProvider {
	dir := ""

	dataDir, err := system.GetUserDataDirPath()
	if err == nil {
		dir = filepath.Join(dataDir, artistImageBaseDir)
		_ = os.MkdirAll(dir, 0o755)
	}

	// Resolve fanart.tv API key: env var > build-time ldflags.
	fanartKey := os.Getenv("FANART_TV_API_KEY")
	if fanartKey == "" {
		fanartKey = fanartTVProjectKey
	}

	if fanartKey != "" {
		logger.Info("fanart.tv API key configured")
	}

	return &ArtistImageProvider{
		db:           db,
		cache:        cache,
		mbLimiter:    mbLimiter,
		client:       &http.Client{Timeout: artistImageTimeout},
		logger:       logger,
		baseDir:      dir,
		fanartAPIKey: fanartKey,
	}
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

// GetArtistImage returns the primary image as a base64 data URL.
// Resolves from all sources if not yet cached.
func (p *ArtistImageProvider) GetArtistImage(artistMBID string) string {
	if artistMBID == "" || p.baseDir == "" {
		return ""
	}

	// Check for existing primary image on disk.
	primaryPath := p.primaryPath(artistMBID)
	if data := readFileData(primaryPath); data != "" {
		return data
	}

	// Check if we already know there's no image.
	if p.isMiss(artistMBID) {
		return ""
	}

	// Resolve from all sources and select primary.
	p.resolveAllSources(artistMBID)

	// Try again after resolution.
	if data := readFileData(primaryPath); data != "" {
		return data
	}

	// Mark as miss.
	p.writeMiss(artistMBID)

	return ""
}

// GetCachedImage returns the primary image from disk cache only.
// No network fetches.
func (p *ArtistImageProvider) GetCachedImage(artistMBID string) string {
	if artistMBID == "" || p.baseDir == "" {
		return ""
	}

	return readFileData(p.primaryPath(artistMBID))
}

// GetImageURLs returns the asset-handler URLs for the primary image
// at all size tiers.  Returns empty strings if no image.
func (p *ArtistImageProvider) GetImageURLs(artistMBID string) (string, string, string, string) {
	if artistMBID == "" || p.baseDir == "" {
		return "", "", "", ""
	}

	dir := p.artistDir(artistMBID)
	prefix := "/artist-images/" + artistMBID[:2] + "/" + artistMBID + "/"

	if _, err := os.Stat(filepath.Join(dir, "primary.jpg")); err != nil {
		return "", "", "", ""
	}

	var small, medium, large string

	full := prefix + "primary.jpg"

	for _, tier := range artistImageTiers {
		path := filepath.Join(dir, "primary"+tier.Suffix+".jpg")
		if _, err := os.Stat(path); err == nil {
			url := prefix + "primary" + tier.Suffix + ".jpg"

			switch tier.Suffix {
			case "_sm":
				small = url
			case "_md":
				medium = url
			case "_lg":
				large = url
			}
		}
	}

	return small, medium, large, full
}

// GetAliases returns artist aliases from cached MB rels.
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

// ArtistDetails holds the structured metadata extracted from MB's
// artist lookup response.  Returned by GetArtistDetails.
type ArtistDetails struct {
	Type           string
	Country        string
	Disambiguation string
	SortName       string
	Aliases        string
}

// GetArtistDetails returns structured metadata for an artist from
// the cached MB artist-rels response (which we fetch anyway during
// image resolution).  Returns nil if not cached.
func (p *ArtistImageProvider) GetArtistDetails(artistMBID string) *ArtistDetails {
	cacheKey := "mb:artist-rels:" + artistMBID

	data, ok := p.cache.Get(cacheKey)
	if !ok {
		return nil
	}

	var envelope struct {
		Type           string `json:"type"`
		Country        string `json:"country"`
		Disambiguation string `json:"disambiguation"`
		SortName       string `json:"sort-name"`
		Aliases        []struct {
			Name string `json:"name"`
		} `json:"aliases"`
	}

	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil
	}

	names := make([]string, 0, len(envelope.Aliases))
	for _, a := range envelope.Aliases {
		if a.Name != "" {
			names = append(names, a.Name)
		}
	}

	return &ArtistDetails{
		Type:           envelope.Type,
		Country:        envelope.Country,
		Disambiguation: envelope.Disambiguation,
		SortName:       envelope.SortName,
		Aliases:        strings.Join(names, " "),
	}
}

// PreloadArtistRels writes a synthesized mb:artist-rels cache entry
// derived from LB batch metadata.  This lets fetchMBRels skip the
// per-artist MB network call — we already have type, country, name,
// and wikidata QID from LB.  Aliases and disambiguation are left
// empty (those only come from a real MB call).
//
// The envelope shape matches what fetchMBRels reads, so the cache
// hit is transparent to the image resolution pipeline.
func (p *ArtistImageProvider) PreloadArtistRels(mbid string, meta ArtistMetadata) {
	cacheKey := "mb:artist-rels:" + mbid

	// Don't overwrite a real MB response if we already have one.
	if data, ok := p.cache.Get(cacheKey); ok && len(data) > 0 {
		return
	}

	// Construct an envelope compatible with both fetchMBRels
	// (which reads `relations`) and GetArtistDetails (which reads
	// `type`, `country`, `disambiguation`, `sort-name`, `aliases`).
	envelope := struct {
		Type           string       `json:"type"`
		Country        string       `json:"country"`
		SortName       string       `json:"sort-name"`
		Disambiguation string       `json:"disambiguation"`
		Name           string       `json:"name"`
		Relations      []mbRelation `json:"relations"`
		Aliases        []struct {
			Name string `json:"name"`
		} `json:"aliases"`
	}{
		Type:    meta.Type,
		Country: meta.Country,
		Name:    meta.Name,
	}

	// Add a wikidata relation so getWikidataQID finds the QID.
	if meta.WikidataQID != "" {
		envelope.Relations = append(envelope.Relations, mbRelation{
			Type: "wikidata",
			URL: struct {
				Resource string `json:"resource"`
			}{
				Resource: "https://www.wikidata.org/wiki/" + meta.WikidataQID,
			},
		})
	}

	data, err := json.Marshal(envelope)
	if err != nil {
		return
	}

	p.cache.Set(cacheKey, data, artistImageCacheTTL, mbid, "artist")
}

// ---------------------------------------------------------------------------
// Source resolution
// ---------------------------------------------------------------------------

type mbRelation struct {
	Type string `json:"type"`
	URL  struct {
		Resource string `json:"resource"`
	} `json:"url"`
}

func (p *ArtistImageProvider) resolveAllSources(artistMBID string) {
	type imageSource struct {
		source string
		url    string
	}

	var urls []imageSource

	// Source 0 (highest priority): fanart.tv artist thumbnails.
	if p.fanartAPIKey != "" {
		fanartURLs := p.fetchFanartTV(artistMBID)

		for _, u := range fanartURLs {
			urls = append(urls, imageSource{source: "fanart", url: u})
		}
	}

	// Source 1: TheAudioDB artist thumb.
	if audioDBURLs := p.fetchAudioDB(artistMBID); len(audioDBURLs) > 0 {
		for _, u := range audioDBURLs {
			urls = append(urls, imageSource{source: "audiodb", url: u})
		}
	}

	rels := p.fetchMBRels(artistMBID)

	// Source 2: MB direct image relations (Wikimedia Commons).
	for _, rel := range rels {
		if rel.Type != "image" {
			continue
		}

		resource := rel.URL.Resource

		if idx := strings.LastIndex(resource, "File:"); idx >= 0 {
			filename := resource[idx+5:]
			thumbURL := wikimediaThumbURL(filename)

			if thumbURL != "" {
				urls = append(urls, imageSource{source: "wikimedia", url: thumbURL})
			}
		}
	}

	// Source 2: Wikidata P18.
	qid := p.getWikidataQID(rels)
	if qid != "" {
		if thumbURL := p.fetchWikidataP18(qid); thumbURL != "" {
			// Avoid duplicates with source 1.
			dup := false

			for _, u := range urls {
				if u.url == thumbURL {
					dup = true

					break
				}
			}

			if !dup {
				urls = append(urls, imageSource{"wikidata", thumbURL})
			}
		}

		// Source 3: Wikipedia lead image.
		if leadURL := p.fetchWikipediaLeadImage(qid); leadURL != "" {
			dup := false

			for _, u := range urls {
				if u.url == leadURL {
					dup = true

					break
				}
			}

			if !dup {
				urls = append(urls, imageSource{"wikipedia", leadURL})
			}
		}
	}

	if len(urls) == 0 {
		return
	}

	// Cap at maxImagesPerArtist.
	if len(urls) > maxImagesPerArtist {
		urls = urls[:maxImagesPerArtist]
	}

	// Fetch and store each image.
	dir := p.artistDir(artistMBID)
	_ = os.MkdirAll(dir, 0o755)

	for i, u := range urls {
		imgData, err := p.fetchImageBytes(u.url)
		if err != nil || len(imgData) == 0 {
			continue
		}

		filename := fmt.Sprintf("%s_%d.jpg", u.source, i)
		path := filepath.Join(dir, filename)
		_ = os.WriteFile(path, imgData, 0o644)

		// Store in DB.
		isPrimary := 0
		if i == 0 {
			isPrimary = 1
		}

		_, _ = p.db.ExecContext(`
			INSERT OR REPLACE INTO artist_images
				(artist_mbid, source, source_url, file_path, is_primary, sort_order, file_size)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, artistMBID, u.source, u.url, path, isPrimary, i, len(imgData))

		// Generate thumbnails for the primary image.
		if i == 0 {
			p.setPrimary(artistMBID, dir, imgData)
		}
	}
}

// setPrimary copies image data to primary.jpg and generates thumbnails.
func (p *ArtistImageProvider) setPrimary(artistMBID, dir string, imgData []byte) {
	primaryPath := filepath.Join(dir, "primary.jpg")
	_ = os.WriteFile(primaryPath, imgData, 0o644)

	// Decode and generate thumbnails.
	img, _, err := image.Decode(strings.NewReader(string(imgData)))
	if err != nil {
		// Try as bytes reader.
		reader := strings.NewReader(string(imgData))

		img, _, err = image.Decode(reader)
		if err != nil {
			p.logger.Debug("artist image: could not decode for thumbnails",
				"mbid", artistMBID, "error", err)

			return
		}
	}

	for _, tier := range artistImageTiers {
		thumbPath := filepath.Join(dir, "primary"+tier.Suffix+".jpg")
		p.generateThumbnail(img, thumbPath, tier.MaxSize, tier.Quality)
	}
}

func (p *ArtistImageProvider) generateThumbnail(
	src image.Image, path string, maxSize, quality int,
) {
	bounds := src.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()

	if w <= maxSize && h <= maxSize {
		// Image already small enough — just encode as JPEG.
		f, err := os.Create(path)
		if err != nil {
			return
		}

		defer func() { _ = f.Close() }()

		_ = jpeg.Encode(f, src, &jpeg.Options{Quality: quality})

		return
	}

	// Scale down maintaining aspect ratio.
	var newW, newH int
	if w > h {
		newW = maxSize
		newH = maxSize * h / w
	} else {
		newH = maxSize
		newW = maxSize * w / h
	}

	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	draw.BiLinear.Scale(dst, dst.Bounds(), src, bounds, draw.Over, nil)

	f, err := os.Create(path)
	if err != nil {
		return
	}

	defer func() { _ = f.Close() }()

	_ = jpeg.Encode(f, dst, &jpeg.Options{Quality: quality})
}

// ---------------------------------------------------------------------------
// MB rels + Wikidata + Wikipedia
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Source 0: fanart.tv
// ---------------------------------------------------------------------------

// fetchFanartTV returns artist thumbnail URLs from fanart.tv.
// Uses the project API key + optional user personal key.
// Returns up to 5 URLs (artistthumb images, sorted by likes).
func (p *ArtistImageProvider) fetchFanartTV(artistMBID string) []string {
	cacheKey := "fanart:" + artistMBID

	if data, ok := p.cache.Get(cacheKey); ok {
		var cached []string
		if err := json.Unmarshal(data, &cached); err == nil {
			return cached
		}
	}

	url := fmt.Sprintf("%s/%s?api_key=%s", fanartTVAPIBase, artistMBID, p.fanartAPIKey)

	// Add personal key if the user configured one.
	if personalKey := os.Getenv("FANART_TV_PERSONAL_KEY"); personalKey != "" {
		url += "&client_key=" + personalKey
	}

	body, err := p.fetchURL(url)
	if err != nil {
		// Cache empty result to avoid re-fetching.
		p.cache.Set(cacheKey, []byte("[]"), artistImageMissCacheTTL, artistMBID, "artist")

		return nil
	}

	var response struct {
		ArtistThumb []struct {
			URL   string `json:"url"`
			Likes string `json:"likes"`
		} `json:"artistthumb"`
	}

	if err := json.Unmarshal(body, &response); err != nil || len(response.ArtistThumb) == 0 {
		p.cache.Set(cacheKey, []byte("[]"), artistImageMissCacheTTL, artistMBID, "artist")

		return nil
	}

	// Take up to 5 thumbs (they're already sorted by likes on the API side).
	limit := 5
	if limit > len(response.ArtistThumb) {
		limit = len(response.ArtistThumb)
	}

	urls := make([]string, limit)
	for i := range limit {
		urls[i] = response.ArtistThumb[i].URL
	}

	// Cache the resolved URLs.
	data, _ := json.Marshal(urls)
	p.cache.Set(cacheKey, data, artistImageCacheTTL, artistMBID, "artist")

	return urls
}

// ---------------------------------------------------------------------------
// Source 1: TheAudioDB
// ---------------------------------------------------------------------------

// fetchAudioDB returns artist thumb URLs from TheAudioDB.
// Uses the free API key (2) for MBID-based lookups.
func (p *ArtistImageProvider) fetchAudioDB(artistMBID string) []string {
	cacheKey := "audiodb:" + artistMBID

	if data, ok := p.cache.Get(cacheKey); ok {
		var cached []string
		if err := json.Unmarshal(data, &cached); err == nil {
			return cached
		}
	}

	url := fmt.Sprintf("%s/artist-mb.php?i=%s", audioDBAPIBase, artistMBID)

	body, err := p.fetchURL(url)
	if err != nil {
		p.cache.Set(cacheKey, []byte("[]"), artistImageMissCacheTTL, artistMBID, "artist")

		return nil
	}

	var response struct {
		Artists []struct {
			Thumb   *string `json:"strArtistThumb"`
			Fanart  *string `json:"strArtistFanart"`
			Fanart2 *string `json:"strArtistFanart2"`
			Fanart3 *string `json:"strArtistFanart3"`
		} `json:"artists"`
	}

	if err := json.Unmarshal(body, &response); err != nil || len(response.Artists) == 0 {
		p.cache.Set(cacheKey, []byte("[]"), artistImageMissCacheTTL, artistMBID, "artist")

		return nil
	}

	artist := response.Artists[0]

	var urls []string

	// Thumb is the primary portrait photo; fanart images are wider/background shots.
	for _, u := range []*string{artist.Thumb, artist.Fanart, artist.Fanart2, artist.Fanart3} {
		if u != nil && *u != "" {
			urls = append(urls, *u)
		}
	}

	data, _ := json.Marshal(urls)
	p.cache.Set(cacheKey, data, artistImageCacheTTL, artistMBID, "artist")

	return urls
}

// ---------------------------------------------------------------------------
// Source 2-4: MB rels + Wikidata + Wikipedia
// ---------------------------------------------------------------------------

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
		// Cache the miss so we don't re-request on every build.
		p.cache.Set(cacheKey, []byte("{}"), artistImageMissCacheTTL, artistMBID, "artist")

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

func (p *ArtistImageProvider) getWikidataQID(rels []mbRelation) string {
	for _, rel := range rels {
		if rel.Type == "wikidata" {
			parts := strings.Split(rel.URL.Resource, "/")

			return parts[len(parts)-1]
		}
	}

	return ""
}

func (p *ArtistImageProvider) fetchWikidataP18(qid string) string {
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

func (p *ArtistImageProvider) fetchWikipediaLeadImage(qid string) string {
	cacheKey := "wikipedia-lead:" + qid

	if data, ok := p.cache.Get(cacheKey); ok {
		return string(data)
	}

	// Get the English Wikipedia article title from Wikidata sitelinks.
	titleURL := fmt.Sprintf(
		"%s?action=wbgetentities&ids=%s&props=sitelinks&sitefilter=enwiki&format=json",
		wikidataAPIBase, qid,
	)

	titleBody, err := p.fetchURL(titleURL)
	if err != nil {
		return ""
	}

	var sitelinks struct {
		Entities map[string]struct {
			Sitelinks map[string]struct {
				Title string `json:"title"`
			} `json:"sitelinks"`
		} `json:"entities"`
	}

	if err := json.Unmarshal(titleBody, &sitelinks); err != nil {
		return ""
	}

	entity, ok := sitelinks.Entities[qid]
	if !ok {
		return ""
	}

	enwiki, ok := entity.Sitelinks["enwiki"]
	if !ok || enwiki.Title == "" {
		p.cache.Set(cacheKey, []byte(""), artistImageMissCacheTTL, "", "")

		return ""
	}

	// Fetch the lead image from Wikipedia.
	imgURL := fmt.Sprintf(
		"%s?action=query&titles=%s&prop=pageimages&format=json&pithumbsize=%d",
		wikipediaAPIBase,
		strings.ReplaceAll(enwiki.Title, " ", "_"),
		artistImageMaxSize,
	)

	imgBody, err := p.fetchURL(imgURL)
	if err != nil {
		return ""
	}

	var wp struct {
		Query struct {
			Pages map[string]struct {
				Thumbnail struct {
					Source string `json:"source"`
				} `json:"thumbnail"`
			} `json:"pages"`
		} `json:"query"`
	}

	if err := json.Unmarshal(imgBody, &wp); err != nil {
		return ""
	}

	leadURL := ""

	for _, page := range wp.Query.Pages {
		if page.Thumbnail.Source != "" {
			leadURL = page.Thumbnail.Source

			break
		}
	}

	p.cache.Set(cacheKey, []byte(leadURL), artistImageCacheTTL, "", "")

	return leadURL
}

// ---------------------------------------------------------------------------
// Disk paths
// ---------------------------------------------------------------------------

func (p *ArtistImageProvider) artistDir(mbid string) string {
	if len(mbid) < 2 {
		return filepath.Join(p.baseDir, "xx", mbid)
	}

	return filepath.Join(p.baseDir, mbid[:2], mbid)
}

func (p *ArtistImageProvider) primaryPath(mbid string) string {
	return filepath.Join(p.artistDir(mbid), "primary.jpg")
}

func (p *ArtistImageProvider) isMiss(mbid string) bool {
	missPath := filepath.Join(p.artistDir(mbid), ".miss")
	_, err := os.Stat(missPath)

	return err == nil
}

func (p *ArtistImageProvider) writeMiss(mbid string) {
	dir := p.artistDir(mbid)
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, ".miss"), []byte{}, 0o644)
}

// ---------------------------------------------------------------------------
// HTTP helpers
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

// ---------------------------------------------------------------------------
// Shared helpers
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
		wikimediaThumbBase, h1, h2, filename, artistImageMaxSize, filename,
	)
}

func readFileData(path string) string {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return ""
	}

	mime := "image/jpeg"
	if len(data) > 1 && data[0] == 0x89 && data[1] == 0x50 {
		mime = "image/png"
	}

	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}
