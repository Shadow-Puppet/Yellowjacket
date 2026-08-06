//go:build indexbuild

package explore

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Parallel dump streaming.  MetaBrainz shapes throughput per
// connection, so a single sequential stream of the listens dump tops
// out near 5 MB/s no matter how fast the link is — the 205GB stage-1
// download alone runs for eleven hours that way.  Several concurrent
// Range requests lift that to tens of MB/s.
//
// parallelReader hides the concurrency behind the same sequential
// io.Reader the tar decoders already consume: lanes fetch fixed-size
// chunks ahead of the caller, and chunks are handed over strictly in
// order.  Pos() therefore keeps meaning "absolute offset of the next
// byte to be delivered", which is what the stage-1 checkpoint records.
//
// For the listens dump this is now the fallback: dumpproject.go fetches
// only the columns stage 1 reads, which is well under half the bytes.
// This path still serves the canonical dump (a zstd stream that cannot
// be range-projected) and any origin that refuses Range requests.
//
// A caution on the rates quoted below: measured again on 2026-07-28,
// data.metabrainz.org served ~0.65 MB/s aggregate to one client and got
// *slower* past eight connections — the shaping is per-IP, not per
// connection, so raising dumpLanes buys throttling rather than speed.

const (
	// dumpLanes is the number of concurrent Range requests.  One lane
	// gets ~5 MB/s because MetaBrainz shapes per connection; four reach
	// tens of MB/s.  Deliberately conservative: these are a nonprofit's
	// servers, they answer sustained heavy use with 503s, and pushing
	// past this trades politeness for throughput that backoff eats
	// anyway.
	dumpLanes = 4

	// dumpChunkSize is how much a lane fetches per request.  Each
	// request pays a ramp-up cost, so small chunks squander the gain —
	// 32MB chunks measured roughly 40% slower than 128MB ones.
	dumpChunkSize = 128 << 20

	// dumpWindowChunks bounds the chunks in flight or buffered awaiting
	// in-order delivery.  Kept just above the lane count so lanes never
	// idle waiting for the consumer; costs dumpWindowChunks *
	// dumpChunkSize of buffer.
	dumpWindowChunks = dumpLanes + 2

	// dumpProbeTimeout bounds the HEAD request that sizes a resource
	// before a parallel stream starts.
	dumpProbeTimeout = 30 * time.Second

	// dumpMaxIdleConns keeps a pooled connection per lane so chunk
	// requests reuse TCP+TLS instead of reconnecting each time.
	dumpMaxIdleConns = dumpLanes * 2

	// dumpRetryAfterCap bounds how long a server-supplied Retry-After is
	// honoured, so a bad header can't park a lane indefinitely.
	dumpRetryAfterCap = 60 * time.Second
)

// dumpStream is the streaming surface the dump importers consume,
// implemented by both parallelReader and resumableReader.
type dumpStream interface {
	io.ReadCloser

	// Pos is the absolute byte offset of the next byte to be delivered.
	// This is what an interrupted import checkpoints and resumes from.
	Pos() int64

	// Fetched is the absolute byte offset the downloader has reached.
	// With prefetching lanes this runs ahead of Pos, and it — not Pos —
	// is what progress and stall reporting should watch: a reader
	// buffering a 128MB chunk is downloading, not stalled.
	Fetched() int64

	// Total is the total resource size, or -1 while unknown.
	Total() int64
}

// newDumpHTTPClient builds the client used for dump discovery and
// streaming.  HTTP/2 is disabled deliberately: it multiplexes every
// lane onto a single TCP connection, which collapses parallel Range
// requests back to one shaped stream (measured at 1-3 MB/s).
func newDumpHTTPClient() *http.Client {
	transport := &http.Transport{
		// Explicitly HTTP/1.1: ALPN would otherwise negotiate h2 and
		// silently undo the parallelism below.
		TLSClientConfig: &tls.Config{
			NextProtos: []string{"http/1.1"},
			MinVersion: tls.VersionTLS12,
		},
		ForceAttemptHTTP2:   false,
		TLSNextProto:        map[string]func(string, *tls.Conn) http.RoundTripper{},
		MaxIdleConns:        dumpMaxIdleConns,
		MaxIdleConnsPerHost: dumpMaxIdleConns,
		IdleConnTimeout:     90 * time.Second,
	}

	// No client-level timeout: dump streams run for hours.  Per-request
	// deadlines come from the caller's context, and chunk fetches retry
	// on their own.
	return &http.Client{Transport: transport}
}

// probeDumpSize returns the resource size when the server advertises
// one and supports Range requests, else (0, false).
func probeDumpSize(ctx context.Context, client *http.Client, url string) (int64, bool) {
	reqCtx, cancel := context.WithTimeout(ctx, dumpProbeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodHead, url, nil)
	if err != nil {
		return 0, false
	}

	req.Header.Set("User-Agent", lbUserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return 0, false
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK || resp.ContentLength <= 0 {
		return 0, false
	}

	if resp.Header.Get("Accept-Ranges") != "bytes" {
		return 0, false
	}

	return resp.ContentLength, true
}

// chunkResult is one fetched chunk awaiting in-order delivery.  bufp is
// the pooled backing array, returned to the pool once drained.
type chunkResult struct {
	bufp *[]byte
	n    int
	err  error
}

// parallelReader streams [base, size) of an HTTP resource through
// several concurrent Range requests, delivering bytes in order.
type parallelReader struct {
	ctx    context.Context
	cancel context.CancelFunc
	client *http.Client
	url    string
	logger *slog.Logger

	base        int64
	size        int64
	totalChunks int64

	// lanes, chunkSize and window are fields rather than constants so
	// tests can drive the reader with small chunks.
	lanes     int
	chunkSize int64
	window    int64

	mu      sync.Mutex
	cond    *sync.Cond
	ready   map[int64]*chunkResult
	nextDL  int64 // next chunk index to hand to a lane
	nextOut int64 // next chunk index to deliver
	closed  bool

	delivered atomic.Int64

	// fetched counts bytes pulled down by the lanes, including chunks
	// still buffered ahead of the caller.
	fetched atomic.Int64

	// cur is the undelivered tail of the chunk being drained.
	cur     []byte
	curBufp *[]byte

	pool sync.Pool
	wg   sync.WaitGroup
}

// newParallelReader starts the lanes and returns a reader positioned at
// offset.  Returns nil when the resource can't be range-streamed, so
// callers fall back to a sequential reader.
func newParallelReader(
	ctx context.Context, client *http.Client, logger *slog.Logger, url string, offset int64,
) *parallelReader {
	return newParallelReaderLogged(
		ctx, client, logger, url, offset, dumpLanes, dumpChunkSize, dumpWindowChunks,
	)
}

// newParallelReaderWith is newParallelReader with the lane geometry
// spelled out, so tests can exercise ordering and resume with chunks
// small enough to be practical.
func newParallelReaderWith(
	ctx context.Context, client *http.Client, url string, offset int64,
	lanes int, chunkSize, window int64,
) *parallelReader {
	return newParallelReaderLogged(
		ctx, client, slog.New(slog.DiscardHandler), url, offset, lanes, chunkSize, window,
	)
}

// newParallelReaderLogged is newParallelReaderWith with a logger, so a
// throttled or retrying stream is diagnosable from the app log.
func newParallelReaderLogged(
	ctx context.Context, client *http.Client, logger *slog.Logger,
	url string, offset int64, lanes int, chunkSize, window int64,
) *parallelReader {
	size, ok := probeDumpSize(ctx, client, url)
	if !ok || offset >= size {
		return nil
	}

	// Below a couple of chunks there's nothing to parallelise.
	if size-offset < 2*chunkSize {
		return nil
	}

	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	streamCtx, cancel := context.WithCancel(ctx)

	p := &parallelReader{
		ctx:       streamCtx,
		cancel:    cancel,
		client:    client,
		logger:    logger,
		url:       url,
		base:      offset,
		size:      size,
		lanes:     lanes,
		chunkSize: chunkSize,
		window:    window,
		ready:     make(map[int64]*chunkResult, window),
		pool: sync.Pool{New: func() any {
			b := make([]byte, chunkSize)

			return &b
		}},
	}

	remaining := size - offset
	p.totalChunks = (remaining + chunkSize - 1) / chunkSize
	p.cond = sync.NewCond(&p.mu)

	for range lanes {
		p.wg.Add(1)

		go p.lane()
	}

	// A cancelled context must wake anyone blocked on the condition.
	go func() {
		<-streamCtx.Done()

		p.mu.Lock()
		p.cond.Broadcast()
		p.mu.Unlock()
	}()

	return p
}

// Pos returns the absolute offset of the next byte to be delivered.
func (p *parallelReader) Pos() int64 {
	return p.base + p.delivered.Load()
}

// Fetched returns the absolute offset the lanes have downloaded to.
func (p *parallelReader) Fetched() int64 {
	return p.base + p.fetched.Load()
}

// Total returns the total resource size.
func (p *parallelReader) Total() int64 {
	return p.size
}

// lane fetches chunks until the window is exhausted or the stream ends.
func (p *parallelReader) lane() {
	defer p.wg.Done()

	for {
		p.mu.Lock()

		for {
			if p.closed || p.ctx.Err() != nil || p.nextDL >= p.totalChunks {
				p.mu.Unlock()

				return
			}

			// Stay inside the delivery window so buffered chunks can't
			// outrun the consumer.
			if p.nextDL < p.nextOut+p.window {
				break
			}

			p.cond.Wait()
		}

		idx := p.nextDL
		p.nextDL++

		p.mu.Unlock()

		bufp, n, err := p.fetchChunk(idx)
		if err == nil {
			p.fetched.Add(int64(n))
		}

		p.mu.Lock()
		p.ready[idx] = &chunkResult{bufp: bufp, n: n, err: err}
		p.cond.Broadcast()
		p.mu.Unlock()
	}
}

// fetchChunk retrieves one chunk, retrying transient failures.  The
// range is fully specified, so a retry simply re-requests it.
func (p *parallelReader) fetchChunk(idx int64) (*[]byte, int, error) {
	lo := p.base + idx*p.chunkSize

	hi := lo + p.chunkSize - 1
	if hi >= p.size {
		hi = p.size - 1
	}

	want := int(hi - lo + 1)

	bufp, _ := p.pool.Get().(*[]byte)

	var (
		lastErr error
		wait    time.Duration
	)

	for attempt := 0; attempt <= maxStreamRetries; attempt++ {
		if attempt > 0 {
			delay := min(streamRetryBaseDelay<<(attempt-1), streamRetryMaxDelay)
			if wait > 0 {
				delay = wait
			}

			select {
			case <-p.ctx.Done():
				p.pool.Put(bufp)

				return nil, 0, p.ctx.Err()
			case <-time.After(delay):
			}
		}

		n, retryAfter, err := p.fetchOnce(lo, hi, (*bufp)[:want])
		if err == nil && n == want {
			return bufp, n, nil
		}

		if err == nil {
			err = io.ErrUnexpectedEOF
		}

		lastErr = err
		wait = retryAfter

		p.logger.Warn("dump import: chunk fetch failed, retrying",
			"chunk", idx,
			"attempt", attempt+1,
			"retryAfter", retryAfter,
			"error", err,
		)

		if p.ctx.Err() != nil {
			p.pool.Put(bufp)

			return nil, 0, p.ctx.Err()
		}
	}

	p.pool.Put(bufp)

	return nil, 0, fmt.Errorf("%w: %s chunk %d after %d retries: %w",
		ErrDumpStream, p.url, idx, maxStreamRetries, lastErr)
}

// fetchOnce performs a single Range request into buf.  The second
// return value is the server's requested Retry-After delay, if any.
func (p *parallelReader) fetchOnce(lo, hi int64, buf []byte) (int, time.Duration, error) {
	req, err := http.NewRequestWithContext(p.ctx, http.MethodGet, p.url, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("dump chunk request: %w", err)
	}

	req.Header.Set("User-Agent", lbUserAgent)
	req.Header.Set("Range", "bytes="+strconv.FormatInt(lo, 10)+"-"+strconv.FormatInt(hi, 10))

	resp, err := p.client.Do(req)
	if err != nil {
		return 0, 0, fmt.Errorf("dump chunk fetch: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusPartialContent {
		// A loaded dump server answers with 503 (or 429) rather than
		// queueing; that is a "come back shortly", not a failure.
		return 0, parseRetryAfter(resp.Header.Get("Retry-After")),
			fmt.Errorf("%w: HTTP %d from %s", ErrDumpStream, resp.StatusCode, p.url)
	}

	n, err := io.ReadFull(resp.Body, buf)
	if err != nil {
		return n, 0, fmt.Errorf("dump chunk read: %w", err)
	}

	return n, 0, nil
}

// parseRetryAfter reads a Retry-After header given in seconds, clamped
// to dumpRetryAfterCap.  Returns 0 when absent or unparseable.
func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}

	secs, err := strconv.Atoi(v)
	if err != nil || secs <= 0 {
		return 0
	}

	return min(time.Duration(secs)*time.Second, dumpRetryAfterCap)
}

func (p *parallelReader) Read(b []byte) (int, error) {
	for len(p.cur) == 0 {
		if err := p.ctx.Err(); err != nil {
			return 0, err
		}

		res, err := p.nextChunk()
		if err != nil {
			return 0, err
		}

		p.curBufp = res.bufp
		p.cur = (*res.bufp)[:res.n]
	}

	n := copy(b, p.cur)
	p.cur = p.cur[n:]
	p.delivered.Add(int64(n))

	if len(p.cur) == 0 && p.curBufp != nil {
		p.pool.Put(p.curBufp)
		p.curBufp = nil
	}

	return n, nil
}

// nextChunk blocks until the next in-order chunk is available.
func (p *parallelReader) nextChunk() (*chunkResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for {
		if p.closed {
			return nil, io.ErrClosedPipe
		}

		if err := p.ctx.Err(); err != nil {
			return nil, err
		}

		if p.nextOut >= p.totalChunks {
			return nil, io.EOF
		}

		res, ok := p.ready[p.nextOut]
		if ok {
			delete(p.ready, p.nextOut)

			p.nextOut++

			// A freed window slot may unblock a waiting lane.
			p.cond.Broadcast()

			if res.err != nil {
				return nil, res.err
			}

			return res, nil
		}

		p.cond.Wait()
	}
}

// Close stops the lanes and releases buffered chunks.
func (p *parallelReader) Close() error {
	p.mu.Lock()

	if p.closed {
		p.mu.Unlock()

		return nil
	}

	p.closed = true
	p.cond.Broadcast()
	p.mu.Unlock()

	p.cancel()
	p.wg.Wait()

	p.mu.Lock()
	clear(p.ready)
	p.mu.Unlock()

	return nil
}

// openDumpStream returns the best available stream for a dump URL,
// preferring parallel Range lanes and falling back to a single
// resumable connection when the server won't serve ranges.
func (imp *dumpImporter) openDumpStream(
	ctx context.Context, url string, offset int64,
) dumpStream {
	if p := newParallelReader(ctx, imp.httpClient, imp.logger, url, offset); p != nil {
		imp.logger.Info("dump import: streaming in parallel",
			"lanes", dumpLanes,
			"chunkMB", dumpChunkSize>>20,
			"url", url,
		)

		return p
	}

	imp.logger.Info("dump import: parallel streaming unavailable, using single stream",
		"url", url,
	)

	return newResumableReader(ctx, imp.httpClient, url, offset)
}
