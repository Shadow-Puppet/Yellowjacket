package explore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"time"
)

// Streaming helpers for the MetaBrainz dump imports.  Dumps are never
// written to disk: the HTTP body is decoded (tar / zstd+tar) in flight.
// resumableReader reconnects with HTTP Range requests on transient
// failures, which also lets the listens import resume across app
// restarts from a checkpointed byte offset.

const (
	// maxStreamRetries is the number of consecutive failed reconnect
	// attempts before a stream read gives up.  The counter resets
	// whenever bytes are successfully delivered.
	maxStreamRetries = 8

	// streamRetryBaseDelay is the initial reconnect backoff; it
	// doubles per consecutive failure.
	streamRetryBaseDelay = 2 * time.Second

	// dumpDiscoveryTimeout bounds the small directory-listing
	// requests (not the multi-hour stream requests).
	dumpDiscoveryTimeout = 30 * time.Second
)

// ErrDumpDiscovery is returned when a dump directory listing does not
// contain the expected entries.
var ErrDumpDiscovery = errors.New("dump discovery failed")

// ErrDumpStream is returned when a dump stream fails permanently.
var ErrDumpStream = errors.New("dump stream failed")

var hrefRe = regexp.MustCompile(`href="([^"?/][^"?]*)"`)

// listHrefs fetches an Apache-style index page and returns the href
// values (directory entries end with a trailing slash).
func listHrefs(ctx context.Context, client *http.Client, url string) ([]string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, dumpDiscoveryTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("dump listing request: %w", err)
	}

	req.Header.Set("User-Agent", lbUserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("dump listing fetch: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"%w: listing %s returned HTTP %d", ErrDumpDiscovery, url, resp.StatusCode,
		)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("dump listing read: %w", err)
	}

	var hrefs []string

	for _, m := range hrefRe.FindAllStringSubmatch(string(body), -1) {
		hrefs = append(hrefs, m[1])
	}

	return hrefs, nil
}

// discoverDumpFile walks a dump base directory, finds subdirectories
// matching dirRe (newest first, lexicographically — MetaBrainz dump
// directory names embed sortable timestamps), and returns the full URL
// of the first file inside matching fileRe.  Directories that don't
// contain a matching file (e.g. partial uploads) are skipped.
func discoverDumpFile(
	ctx context.Context,
	client *http.Client,
	baseURL string,
	dirRe, fileRe *regexp.Regexp,
) (string, error) {
	hrefs, err := listHrefs(ctx, client, baseURL)
	if err != nil {
		return "", err
	}

	var dirs []string

	for _, h := range hrefs {
		trimmed := trimTrailingSlash(h)
		if dirRe.MatchString(trimmed) {
			dirs = append(dirs, trimmed)
		}
	}

	if len(dirs) == 0 {
		return "", fmt.Errorf("%w: no dump directories under %s", ErrDumpDiscovery, baseURL)
	}

	sort.Sort(sort.Reverse(sort.StringSlice(dirs)))

	for _, dir := range dirs {
		dirURL := baseURL + dir + "/"

		files, err := listHrefs(ctx, client, dirURL)
		if err != nil {
			continue
		}

		for _, f := range files {
			if fileRe.MatchString(f) {
				return dirURL + f, nil
			}
		}
	}

	return "", fmt.Errorf("%w: no matching dump file under %s", ErrDumpDiscovery, baseURL)
}

func trimTrailingSlash(s string) string {
	if len(s) > 0 && s[len(s)-1] == '/' {
		return s[:len(s)-1]
	}

	return s
}

// resumableReader is an io.Reader over an HTTP resource that survives
// connection failures by reconnecting with a Range request at the
// current offset.  Offset is the absolute position of the next byte to
// deliver, so callers can checkpoint it and construct a new
// resumableReader later to resume a partially-processed stream.
type resumableReader struct {
	ctx    context.Context
	client *http.Client
	url    string

	// Offset is the absolute byte position of the next read.
	Offset int64

	// Size is the total resource size, learned from the first
	// response.  -1 until known.
	Size int64

	body    io.ReadCloser
	retries int
}

func newResumableReader(
	ctx context.Context, client *http.Client, url string, offset int64,
) *resumableReader {
	return &resumableReader{
		ctx:    ctx,
		client: client,
		url:    url,
		Offset: offset,
		Size:   -1,
	}
}

func (r *resumableReader) Read(p []byte) (int, error) {
	for {
		if err := r.ctx.Err(); err != nil {
			return 0, err
		}

		if r.body == nil {
			if err := r.connect(); err != nil {
				return 0, err
			}
		}

		n, err := r.body.Read(p)
		r.Offset += int64(n)

		if n > 0 {
			r.retries = 0
		}

		switch {
		case err == nil:
			return n, nil
		case errors.Is(err, io.EOF):
			// A server that closes early looks like EOF; only
			// trust it when we've seen the advertised size.
			if r.Size >= 0 && r.Offset < r.Size {
				r.closeBody()

				if retryErr := r.backoff(err); retryErr != nil {
					return n, retryErr
				}

				if n > 0 {
					return n, nil
				}

				continue
			}

			return n, io.EOF
		default:
			r.closeBody()

			if retryErr := r.backoff(err); retryErr != nil {
				return n, retryErr
			}

			if n > 0 {
				return n, nil
			}
		}
	}
}

// backoff sleeps with exponential backoff, or returns a terminal error
// once the retry budget is exhausted.
func (r *resumableReader) backoff(cause error) error {
	r.retries++
	if r.retries > maxStreamRetries {
		return fmt.Errorf(
			"%w: %s after %d retries: %w", ErrDumpStream, r.url, maxStreamRetries, cause,
		)
	}

	delay := streamRetryBaseDelay << (r.retries - 1)

	select {
	case <-r.ctx.Done():
		return r.ctx.Err()
	case <-time.After(delay):
		return nil
	}
}

func (r *resumableReader) connect() error {
	req, err := http.NewRequestWithContext(r.ctx, http.MethodGet, r.url, nil)
	if err != nil {
		return fmt.Errorf("dump stream request: %w", err)
	}

	req.Header.Set("User-Agent", lbUserAgent)

	if r.Offset > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(r.Offset, 10)+"-")
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return r.backoff(err)
	}

	switch resp.StatusCode {
	case http.StatusPartialContent:
		if r.Size < 0 {
			r.Size = parseContentRangeTotal(resp.Header.Get("Content-Range"))
		}

		r.body = resp.Body

		return nil
	case http.StatusOK:
		if r.Size < 0 && resp.ContentLength > 0 {
			r.Size = resp.ContentLength
		}

		// Server ignored the Range header: discard the prefix so
		// the caller still reads from the requested offset.
		if r.Offset > 0 {
			if _, err := io.CopyN(io.Discard, resp.Body, r.Offset); err != nil {
				_ = resp.Body.Close()

				return r.backoff(err)
			}
		}

		r.body = resp.Body

		return nil
	default:
		_ = resp.Body.Close()

		return r.backoff(fmt.Errorf("%w: HTTP %d from %s", ErrDumpStream, resp.StatusCode, r.url))
	}
}

func (r *resumableReader) closeBody() {
	if r.body != nil {
		_ = r.body.Close()
		r.body = nil
	}
}

// Close releases the underlying HTTP body, if any.
func (r *resumableReader) Close() error {
	r.closeBody()

	return nil
}

// parseContentRangeTotal extracts the total size from a Content-Range
// header ("bytes 100-199/12345").  Returns -1 if unavailable.
func parseContentRangeTotal(v string) int64 {
	for i := len(v) - 1; i >= 0; i-- {
		if v[i] == '/' {
			total, err := strconv.ParseInt(v[i+1:], 10, 64)
			if err != nil {
				return -1
			}

			return total
		}
	}

	return -1
}
