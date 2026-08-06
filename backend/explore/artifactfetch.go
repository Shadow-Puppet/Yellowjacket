package explore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"

	"yellowjacket/backend/jobs"
	"yellowjacket/backend/system"
)

// Download of the prebuilt core index artifact.
//
// The artifact is published by .gitea/workflows/index-artifact.yml to a
// Gitea generic package registry under a fixed "latest" version, so the
// client needs no package-listing API (which requires a token) — only an
// anonymous GET of a predictable URL.
//
// Everything here degrades to "no artifact" rather than failing: an
// offline install, a 404 before the first artifact is published, or a
// corrupt download all leave the caller free to fall back to the normal
// build path.  A missing artifact must never be the reason Explore is
// broken.

const (
	// defaultCoreArtifactBaseURL is where the CI-built artifact lives.
	// Overridable via YJ_CORE_INDEX_URL for testing and for anyone
	// self-hosting their own index.
	defaultCoreArtifactBaseURL = "https://git.ljones.me/api/packages/yonlu/" +
		"generic/yellowjacket-core-index/latest/"

	// coreArtifactFile is the compressed artifact's filename, and
	// coreArtifactChecksumFile its detached sha256.
	coreArtifactFile     = "core-index.db.zst"
	coreArtifactChecksum = "core-index.db.zst.sha256"

	// artifactURLEnv overrides the base URL.
	artifactURLEnv = "YJ_CORE_INDEX_URL"

	// artifactDiscoverTimeout bounds the checksum fetch, which doubles as
	// the availability probe.  Short: a first run should not sit for
	// minutes deciding whether an artifact exists.
	artifactDiscoverTimeout = 30 * time.Second

	// artifactMinFreeBytes is the free disk needed to fetch and unpack.
	// The compressed artifact plus its expansion plus merge headroom —
	// two orders of magnitude below the full dump import's 6GB floor,
	// which is much of the point.
	artifactMinFreeBytes = 1 << 30

	// artifactMaxRetries bounds resume attempts for the body download.
	artifactMaxRetries = 5
)

// ErrArtifactUnavailable means no artifact could be fetched.  It is an
// expected outcome (offline, not yet published), not a failure.
var ErrArtifactUnavailable = errors.New("core index artifact unavailable")

// coreArtifactBaseURL resolves the artifact location, honouring the
// environment override.
func coreArtifactBaseURL() string {
	if v := strings.TrimSpace(os.Getenv(artifactURLEnv)); v != "" {
		if !strings.HasSuffix(v, "/") {
			return v + "/"
		}

		return v
	}

	return defaultCoreArtifactBaseURL
}

// artifactFetcher downloads and unpacks the core index artifact.
type artifactFetcher struct {
	si         *SearchIndex
	client     *http.Client
	baseURL    string
	stagingDir string
}

func newArtifactFetcher(si *SearchIndex) (*artifactFetcher, error) {
	dataDir, err := system.GetUserDataDirPath()
	if err != nil {
		return nil, fmt.Errorf("core artifact: data dir: %w", err)
	}

	stagingDir := filepath.Join(dataDir, "explore-staging")
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return nil, fmt.Errorf("core artifact: staging dir: %w", err)
	}

	return &artifactFetcher{
		si: si,
		// No client-level timeout: a 70MB body over a slow link can take
		// a while.  Stalls are handled by resume rather than by killing
		// the whole download.
		client:     &http.Client{},
		baseURL:    coreArtifactBaseURL(),
		stagingDir: stagingDir,
	}, nil
}

// compressedPath and unpackedPath are the two staging files.  Both live
// beside the dump importer's own staging data and are removed on success.
func (f *artifactFetcher) compressedPath() string {
	return filepath.Join(f.stagingDir, coreArtifactFile)
}

func (f *artifactFetcher) unpackedPath() string {
	return filepath.Join(f.stagingDir, "core-index.db")
}

// fetch downloads, verifies and decompresses the artifact, returning the
// path to the ready-to-merge database.
func (f *artifactFetcher) fetch(ctx context.Context) (string, error) {
	if err := checkFreeDisk(f.stagingDir, artifactMinFreeBytes); err != nil {
		return "", err
	}

	want, err := f.fetchChecksum(ctx)
	if err != nil {
		return "", err
	}

	if err := f.download(ctx); err != nil {
		return "", err
	}

	got, err := fileSHA256(f.compressedPath())
	if err != nil {
		return "", err
	}

	if got != want {
		// A partial file that resumed against a newer published artifact
		// would fail here forever; discard it so the next attempt starts
		// clean rather than re-resuming into the same mismatch.
		_ = os.Remove(f.compressedPath())

		return "", fmt.Errorf("%w: checksum mismatch (got %s, want %s)",
			ErrArtifactUnusable, got, want)
	}

	if err := f.decompress(ctx); err != nil {
		return "", err
	}

	// The compressed copy is dead weight once unpacked.
	_ = os.Remove(f.compressedPath())

	return f.unpackedPath(), nil
}

// fetchChecksum retrieves the expected sha256.  This doubles as the
// availability probe: it is a few bytes, so a missing or unreachable
// artifact is discovered without starting a large download.
func (f *artifactFetcher) fetchChecksum(ctx context.Context) (string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, artifactDiscoverTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(
		probeCtx, http.MethodGet, f.baseURL+coreArtifactChecksum, nil)
	if err != nil {
		return "", fmt.Errorf("%w: checksum request: %w", ErrArtifactUnavailable, err)
	}

	req.Header.Set("User-Agent", lbUserAgent)

	resp, err := f.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrArtifactUnavailable, err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: HTTP %d fetching checksum",
			ErrArtifactUnavailable, resp.StatusCode)
	}

	// The file is `sha256sum` output: "<hex>  <filename>".
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", fmt.Errorf("%w: read checksum: %w", ErrArtifactUnavailable, err)
	}

	sum := strings.TrimSpace(string(body))
	if i := strings.IndexAny(sum, " \t"); i > 0 {
		sum = sum[:i]
	}

	if len(sum) != sha256.Size*2 {
		return "", fmt.Errorf("%w: malformed checksum %q", ErrArtifactUnusable, sum)
	}

	return strings.ToLower(sum), nil
}

// download fetches the artifact body, resuming a partial file with a
// Range request rather than restarting it.
func (f *artifactFetcher) download(ctx context.Context) error {
	var lastErr error

	for attempt := range artifactMaxRetries {
		if err := ctx.Err(); err != nil {
			return err
		}

		if attempt > 0 {
			delay := min(streamRetryBaseDelay<<(attempt-1), streamRetryMaxDelay)

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		err := f.downloadOnce(ctx)
		if err == nil {
			return nil
		}

		lastErr = err

		f.si.logger.Warn("core artifact: download attempt failed",
			"attempt", attempt+1, "error", err,
		)
	}

	return fmt.Errorf("%w: after %d attempts: %w",
		ErrArtifactUnavailable, artifactMaxRetries, lastErr)
}

func (f *artifactFetcher) downloadOnce(ctx context.Context) error {
	// A file left by a previous attempt is resumed from its length.
	var have int64

	if fi, err := os.Stat(f.compressedPath()); err == nil {
		have = fi.Size()
	}

	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet, f.baseURL+coreArtifactFile, nil)
	if err != nil {
		return fmt.Errorf("artifact request: %w", err)
	}

	req.Header.Set("User-Agent", lbUserAgent)

	if have > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(have, 10)+"-")
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return fmt.Errorf("artifact fetch: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusPartialContent:
		// Resuming: append.
	case http.StatusOK:
		// The server ignored the Range header (or there was nothing to
		// resume), so this is a whole-file body and the partial must go.
		have = 0
	default:
		return fmt.Errorf("%w: HTTP %d fetching artifact",
			ErrArtifactUnavailable, resp.StatusCode)
	}

	flags := os.O_CREATE | os.O_WRONLY
	if have > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}

	file, err := os.OpenFile(f.compressedPath(), flags, 0o644)
	if err != nil {
		return fmt.Errorf("open artifact file: %w", err)
	}

	defer func() { _ = file.Close() }()

	total := have + resp.ContentLength

	if _, err := io.Copy(file, f.progressReader(resp.Body, have, total)); err != nil {
		return fmt.Errorf("artifact download: %w", err)
	}

	return file.Close()
}

// progressReader wraps the body so download progress reaches the jobs
// panel, since this is the one visible wait on a fresh install.
func (f *artifactFetcher) progressReader(r io.Reader, done, total int64) io.Reader {
	return &artifactProgress{
		inner: r,
		done:  done,
		total: total,
		si:    f.si,
		last:  time.Now(),
	}
}

type artifactProgress struct {
	inner       io.Reader
	done, total int64
	si          *SearchIndex
	last        time.Time
}

func (p *artifactProgress) Read(b []byte) (int, error) {
	n, err := p.inner.Read(b)
	p.done += int64(n)

	if time.Since(p.last) >= time.Second {
		p.last = time.Now()

		detail := formatGB(p.done)
		if p.total > 0 {
			detail = fmt.Sprintf("%.0f%% of %s",
				100*float64(p.done)/float64(p.total), formatGB(p.total))
		}

		// Reported in KiB so a multi-hundred-MB artifact cannot overflow
		// the int progress fields on a 32-bit build.
		p.si.setTierDetail(
			artifactStageNames[artifactStageDownload], "running",
			int(p.done>>10), int(p.total>>10), detail,
		)
	}

	if err != nil && !errors.Is(err, io.EOF) {
		return n, fmt.Errorf("artifact body read: %w", err)
	}

	return n, err //nolint:wrapcheck // io.EOF must reach the caller unwrapped.
}

// decompress expands the zstd artifact into the staging directory.
func (f *artifactFetcher) decompress(ctx context.Context) error {
	src, err := os.Open(f.compressedPath())
	if err != nil {
		return fmt.Errorf("%w: open compressed artifact: %w", ErrArtifactUnusable, err)
	}

	defer func() { _ = src.Close() }()

	zr, err := zstd.NewReader(src)
	if err != nil {
		return fmt.Errorf("%w: zstd reader: %w", ErrArtifactUnusable, err)
	}

	defer zr.Close()

	dst, err := os.Create(f.unpackedPath())
	if err != nil {
		return fmt.Errorf("%w: create artifact db: %w", ErrArtifactUnusable, err)
	}

	defer func() { _ = dst.Close() }()

	f.si.logIndexJob(jobs.LevelInfo, "Unpacking prebuilt catalog")

	if _, err := io.Copy(dst, zr.IOReadCloser()); err != nil {
		// A half-written database would be rejected by inspectArtifact,
		// but leaving it around means the next run re-reads the same
		// wreckage before rejecting it.
		_ = os.Remove(f.unpackedPath())

		return fmt.Errorf("%w: decompress: %w", ErrArtifactUnusable, err)
	}

	if err := ctx.Err(); err != nil {
		_ = os.Remove(f.unpackedPath())

		return err
	}

	return dst.Close()
}

// fileSHA256 hashes a file.  The whole file is hashed after the download
// completes rather than incrementally, because a resumed download never
// sees the bytes it skipped.
func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("%w: open for checksum: %w", ErrArtifactUnusable, err)
	}

	defer func() { _ = file.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", fmt.Errorf("%w: checksum read: %w", ErrArtifactUnusable, err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
