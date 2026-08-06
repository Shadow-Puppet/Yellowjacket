package download

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Prowlarr is the one provider that cannot finish a job by itself: it
// searches dozens of indexers and hands back magnet links and NZB URLs,
// which some other client has to actually fetch.  That is the whole
// reason the role interfaces are separate — Prowlarr implements
// Searcher and nothing else, and the pipeline pairs its results with
// whichever enabled transport handles the protocol.
//
// Its search results also carry no file list.  A torrent is one opaque
// blob until it is fetched, so match scoring here has only the release
// title to work with, and candidates are marked accordingly rather than
// pretending to know what is inside.

// Prowlarr provider errors.
var (
	// ErrProwlarrUnreachable means the instance did not answer.
	ErrProwlarrUnreachable = errors.New("prowlarr is unreachable")

	// ErrProwlarrAuth means the API key was rejected.
	ErrProwlarrAuth = errors.New("prowlarr rejected the API key")

	// ErrProwlarrNoIndexers means nothing is configured to search.
	ErrProwlarrNoIndexers = errors.New("prowlarr has no enabled indexers")
)

// prowlarrHTTPTimeout bounds one API call.  Indexer fan-out is slow, so
// this is longer than the other adapters'.
const prowlarrHTTPTimeout = 45 * time.Second

// prowlarrMusicCategory is Newznab's music category.  Searching without
// it returns every match across film and software too.
const prowlarrMusicCategory = "3000"

// prowlarrMaxResults caps how many results are turned into candidates.
const prowlarrMaxResults = 40

func init() {
	Register(
		Descriptor{
			Kind: KindProwlarr,
			Name: "Prowlarr",
			Summary: "Search many torrent and usenet indexers at once. " +
				"Needs a download client (qBittorrent or SABnzbd) to fetch results.",
			RequiresExternal: "Prowlarr",
			Caps: Caps{
				CanSearch: true,
			},
			Fields: []Field{
				{
					Key:         "url",
					Label:       "Prowlarr URL",
					Placeholder: "http://localhost:9696",
					Required:    true,
					Default:     "http://localhost:9696",
				},
				{
					Key:      "apiKey",
					Label:    "API key",
					Secret:   true,
					Required: true,
					Help:     "Prowlarr → Settings → General → API Key.",
				},
				{
					Key:   "indexerIds",
					Label: "Indexer IDs",
					Help: "Comma-separated numeric IDs to restrict the search to. " +
						"Leave blank to search all enabled indexers.",
				},
				{
					Key:   "minSeeders",
					Label: "Minimum seeders",
					Help: "Torrent results below this are hidden. " +
						"Defaults to 1.",
					Default: "1",
				},
			},
		},
		newProwlarr,
	)
}

// prowlarr is the Prowlarr search provider.
type prowlarr struct {
	info   ProviderInfo
	logger *slog.Logger
	client *apiClient

	indexerIDs []string
	minSeeders int
}

// newProwlarr builds the provider from config.
func newProwlarr(
	cfg Config,
	secrets SecretLookup,
	logger *slog.Logger,
) (Provider, error) {
	base := strings.TrimRight(cfg.Setting("url", ""), "/")
	if base == "" {
		return nil, fmt.Errorf("%w: Prowlarr URL is required", ErrNotConfigured)
	}

	apiKey := ""

	if secrets != nil {
		key, err := secrets("apiKey")
		if err != nil {
			return nil, fmt.Errorf("%w: no API key stored", ErrNotConfigured)
		}

		apiKey = key
	}

	minSeeders, err := strconv.Atoi(cfg.Setting("minSeeders", "1"))
	if err != nil {
		minSeeders = 1
	}

	var indexers []string

	for _, id := range strings.Split(cfg.Setting("indexerIds", ""), ",") {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			indexers = append(indexers, trimmed)
		}
	}

	return &prowlarr{
		info: ProviderInfo{
			ID:       cfg.ID,
			Kind:     KindProwlarr,
			Name:     cfg.Name,
			Enabled:  cfg.Enabled,
			Priority: cfg.Priority,
			Caps:     Caps{CanSearch: true},
		},
		logger: logger.With("provider", "prowlarr"),
		client: newAPIClient(
			base, "X-Api-Key", apiKey, prowlarrHTTPTimeout,
			ErrProwlarrUnreachable, ErrProwlarrAuth,
		),
		indexerIDs: indexers,
		minSeeders: minSeeders,
	}, nil
}

// Info returns the provider's identity.
func (p *prowlarr) Info() ProviderInfo {
	return p.info
}

// Close is a no-op.
func (p *prowlarr) Close() error {
	return nil
}

// Check verifies the instance answers and has something to search.
func (p *prowlarr) Check(ctx context.Context) error {
	var status struct {
		Version string `json:"version"`
	}

	if err := p.client.get(ctx, "/api/v1/system/status", &status); err != nil {
		return err
	}

	var indexers []struct {
		ID     int  `json:"id"`
		Enable bool `json:"enable"`
	}

	if err := p.client.get(ctx, "/api/v1/indexer", &indexers); err != nil {
		return err
	}

	for _, i := range indexers {
		if i.Enable {
			return nil
		}
	}

	return ErrProwlarrNoIndexers
}

// prowlarrResult is one indexer hit.
type prowlarrResult struct {
	GUID        string `json:"guid"`
	Title       string `json:"title"`
	Indexer     string `json:"indexer"`
	Size        int64  `json:"size"`
	Seeders     int    `json:"seeders"`
	Leechers    int    `json:"leechers"`
	Protocol    string `json:"protocol"` // "torrent" or "usenet"
	DownloadURL string `json:"downloadUrl"`
	MagnetURL   string `json:"magnetUrl"`
	InfoHash    string `json:"infoHash"`
}

// Search queries every configured indexer through Prowlarr.
func (p *prowlarr) Search(
	ctx context.Context,
	req Request,
) ([]Candidate, error) {
	query := url.Values{}
	query.Set("query", req.SearchText())
	query.Set("categories", prowlarrMusicCategory)
	query.Set("type", "search")

	for _, id := range p.indexerIDs {
		query.Add("indexerIds", id)
	}

	var results []prowlarrResult

	endpoint := "/api/v1/search?" + query.Encode()

	if err := p.client.get(ctx, endpoint, &results); err != nil {
		return nil, err
	}

	out := make([]Candidate, 0, len(results))

	for _, r := range results {
		if len(out) >= prowlarrMaxResults {
			break
		}

		protocol := protocolFor(r.Protocol)
		if protocol == ProtocolDirect {
			continue
		}

		// A torrent with no seeders will never finish.  Offering it
		// wastes the user's pick on something that cannot complete.
		if protocol == ProtocolTorrent && r.Seeders < p.minSeeders {
			continue
		}

		link := r.MagnetURL
		if link == "" {
			link = r.DownloadURL
		}

		if link == "" {
			continue
		}

		out = append(out, Candidate{
			ID:       "prowlarr:" + r.GUID,
			Kind:     KindProwlarr,
			Protocol: protocol,
			Title:    r.Title,
			Origin:   r.Indexer,
			// Indexer results are opaque before they are fetched: there
			// is no file list, so no per-file scoring is possible and
			// the ranker works from the release title alone.
			Files:     nil,
			TotalSize: r.Size,
			Health:    swarmHealth(protocol, r.Seeders),
			Payload: map[string]string{
				"link":     link,
				"indexer":  r.Indexer,
				"infoHash": r.InfoHash,
			},
		})
	}

	return out, nil
}

// protocolFor maps Prowlarr's protocol string onto ours.
func protocolFor(s string) Protocol {
	switch strings.ToLower(s) {
	case "torrent":
		return ProtocolTorrent
	case "usenet":
		return ProtocolUsenet
	default:
		return ProtocolDirect
	}
}

// swarmHealth scores availability, in 0..1.  Usenet has no swarm: a
// retained article either downloads at full speed or is gone, so it
// gets a flat, confident score.
func swarmHealth(protocol Protocol, seeders int) float64 {
	if protocol == ProtocolUsenet {
		return 0.85
	}

	// Seeder counts have sharply diminishing returns — the difference
	// between 1 and 10 is enormous, between 100 and 500 irrelevant.
	switch {
	case seeders <= 0:
		return 0.05
	case seeders == 1:
		return 0.3
	case seeders < 5:
		return 0.5
	case seeders < 20:
		return 0.75
	case seeders < 100:
		return 0.9
	default:
		return 1.0
	}
}
