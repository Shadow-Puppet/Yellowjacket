package explore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // Register decoder.
	_ "image/png"  // Register decoder.
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"yellowjacket/backend/metadata"
)

// minCoverArtDimensionPx is the minimum size on the shortest side
// of a cover-art image the autotagger will embed.  Below this the
// image is considered too low-res for desktop/mobile display.
// REVIEW-05 in the 010 plan pins this; 012 may surface it as a
// config option.
const minCoverArtDimensionPx = 500

// caaFetchTimeout bounds the single CAA GET.  CAA redirects to
// archive.org, which can be slow but shouldn't stall apply for
// minutes.
const caaFetchTimeout = 30 * time.Second

// errCAANot2xx signals a CAA response that wasn't 200 or 404.
var errCAANot2xx = errors.New("autotag cover art: unexpected CAA status")

// AutotagCoverArt implements autotag.CoverArtEmbedder against the
// Cover Art Archive.  Rule: never replace existing art; only embed
// when the file has none AND CAA returns an image ≥500 px on the
// shortest side.
type AutotagCoverArt struct {
	limiter *RateLimiter
	logger  *slog.Logger
	httpCli *http.Client
}

// NewAutotagCoverArt wires up the embedder with the shared CAA
// limiter.  httpClient may be nil — a default 30 s client is used.
func NewAutotagCoverArt(limiter *RateLimiter, logger *slog.Logger) *AutotagCoverArt {
	return &AutotagCoverArt{
		limiter: limiter,
		logger:  logger,
		httpCli: &http.Client{Timeout: caaFetchTimeout},
	}
}

// FetchArt is the network half of autotag.CoverArtEmbedder.  Hits
// CAA for the release group, validates the result is at least
// 500 px on the shortest side, and returns the raw bytes ready to
// embed.  Returns (nil, nil) when CAA has nothing or the result is
// below the minimum size; (nil, err) when the network or decode
// failed.  Caller is expected to invoke this once per album and
// reuse the bytes across every track that lacks embedded art.
func (c *AutotagCoverArt) FetchArt(
	ctx context.Context, releaseGroupMBID string,
) ([]byte, error) {
	if releaseGroupMBID == "" {
		return nil, nil
	}

	if err := c.limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("wait for CAA limiter: %w", err)
	}

	url := CoverArtGroupURLSize(releaseGroupMBID, minCoverArtDimensionPx*2) //nolint:mnd

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build CAA request: %w", err)
	}

	resp, err := c.httpCli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("CAA GET: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %d for %s", errCAANot2xx, resp.StatusCode, url)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read CAA body: %w", err)
	}

	cfg, _, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("decode CAA image: %w", err)
	}

	shortest := cfg.Width
	if cfg.Height < shortest {
		shortest = cfg.Height
	}

	if shortest < minCoverArtDimensionPx {
		c.logger.Debug(
			"cover art: CAA result below 500px, skipping",
			"width", cfg.Width, "height", cfg.Height,
			"release_group_mbid", releaseGroupMBID,
		)

		return nil, nil
	}

	return body, nil
}

// HasEmbeddedArt reports whether the local audio file already
// carries embedded picture data.  The autotag pipeline calls this
// per track to decide whether to merge the album's CAA art into
// that track's changes — never replacing existing art is the
// invariant.  If metadata extraction fails the function returns
// false (safer default: fall through to "no art present, may
// embed" — the writer still won't overwrite anything because the
// tag-level diff is built from this signal).
func (c *AutotagCoverArt) HasEmbeddedArt(filePath string) bool {
	if _, err := os.Stat(filePath); err != nil {
		c.logger.Debug(
			"cover art: stat for embedded-art probe failed",
			"path", filePath, "err", err,
		)

		return false
	}

	tags, _, _, _, err := metadata.ExtractAllMetadata(filePath, true)
	if err != nil {
		c.logger.Debug(
			"cover art: extract for embedded-art probe failed",
			"path", filePath, "err", err,
		)

		return false
	}

	return tags != nil && tags.Picture != nil && len(tags.Picture.Data) > 0
}
