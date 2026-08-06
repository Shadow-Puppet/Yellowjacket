//go:build indexbuild

package explore

import (
	"bytes"
	"context"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// serveBlob returns a Range-capable server for a fixed payload, plus a
// counter of the GET requests it served.
func serveBlob(t *testing.T, payload []byte) (*httptest.Server, *atomic.Int64) {
	t.Helper()

	var gets atomic.Int64

	modTime := time.Now()

	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				gets.Add(1)
			}

			http.ServeContent(w, r, "blob.bin", modTime, bytes.NewReader(payload))
		},
	))
	t.Cleanup(srv.Close)

	return srv, &gets
}

func randomPayload(n int) []byte {
	buf := make([]byte, n)

	rng := rand.New(rand.NewSource(1)) //nolint:gosec // deterministic fixture
	_, _ = rng.Read(buf)

	return buf
}

// The whole point of the reader is that concurrency stays invisible:
// bytes must come out in the same order a single stream would produce.
func TestParallelReaderDeliversBytesInOrder(t *testing.T) {
	payload := randomPayload(200_000)
	srv, gets := serveBlob(t, payload)

	p := newParallelReaderWith(
		context.Background(), srv.Client(), srv.URL, 0, 4, 8_192, 6,
	)
	if p == nil {
		t.Fatal("newParallelReaderWith returned nil, want a parallel reader")
	}

	defer func() { _ = p.Close() }()

	got, err := io.ReadAll(p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if !bytes.Equal(got, payload) {
		t.Fatalf("payload mismatch: got %d bytes, want %d", len(got), len(payload))
	}

	// Confirm it really did fan out rather than quietly falling back.
	if n := gets.Load(); n < 2 {
		t.Errorf("served %d GETs, want one per chunk", n)
	}
}

// Pos is what the stage-1 checkpoint records, so it must track bytes
// handed to the caller — not bytes fetched by the lanes running ahead.
func TestParallelReaderPosTracksDeliveredBytes(t *testing.T) {
	payload := randomPayload(100_000)
	srv, _ := serveBlob(t, payload)

	p := newParallelReaderWith(
		context.Background(), srv.Client(), srv.URL, 0, 4, 4_096, 8,
	)
	if p == nil {
		t.Fatal("newParallelReaderWith returned nil")
	}

	defer func() { _ = p.Close() }()

	if got := p.Total(); got != int64(len(payload)) {
		t.Errorf("Total() = %d, want %d", got, len(payload))
	}

	buf := make([]byte, 1_000)

	read, err := io.ReadFull(p, buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if got := p.Pos(); got != int64(read) {
		t.Errorf("Pos() = %d after reading %d bytes, want %d", got, read, read)
	}

	// Let the lanes race ahead, then confirm Pos still reflects delivery.
	time.Sleep(50 * time.Millisecond)

	if got := p.Pos(); got != int64(read) {
		t.Errorf("Pos() = %d after lanes prefetched, want %d", got, read)
	}
}

// Progress reporting watches Fetched rather than Pos, because a reader
// buffering a chunk ahead of the caller is downloading, not stalled.
// Fetched must therefore outrun Pos while lanes prefetch.
func TestParallelReaderFetchedOutrunsPos(t *testing.T) {
	payload := randomPayload(200_000)
	srv, _ := serveBlob(t, payload)

	p := newParallelReaderWith(
		context.Background(), srv.Client(), srv.URL, 0, 4, 8_192, 8,
	)
	if p == nil {
		t.Fatal("newParallelReaderWith returned nil")
	}

	defer func() { _ = p.Close() }()

	// Read a single byte, then let the lanes fill the window.
	buf := make([]byte, 1)
	if _, err := io.ReadFull(p, buf); err != nil {
		t.Fatalf("read: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && p.Fetched() <= p.Pos() {
		time.Sleep(10 * time.Millisecond)
	}

	if p.Fetched() <= p.Pos() {
		t.Errorf("Fetched() = %d, Pos() = %d; want Fetched ahead while prefetching",
			p.Fetched(), p.Pos())
	}
}

// Resuming an interrupted import constructs a reader at the
// checkpointed offset; it must yield exactly the remaining tail.
func TestParallelReaderResumesFromOffset(t *testing.T) {
	payload := randomPayload(120_000)
	srv, _ := serveBlob(t, payload)

	const offset = 37_000

	p := newParallelReaderWith(
		context.Background(), srv.Client(), srv.URL, offset, 3, 8_192, 5,
	)
	if p == nil {
		t.Fatal("newParallelReaderWith returned nil")
	}

	defer func() { _ = p.Close() }()

	if got := p.Pos(); got != offset {
		t.Errorf("Pos() = %d before reading, want %d", got, offset)
	}

	got, err := io.ReadAll(p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if !bytes.Equal(got, payload[offset:]) {
		t.Fatalf("resumed payload mismatch: got %d bytes, want %d",
			len(got), len(payload)-offset)
	}
}

// A lane that hits a transient failure must retry its range rather than
// tear down the whole multi-hour stream.
func TestParallelReaderRetriesFailedChunk(t *testing.T) {
	payload := randomPayload(60_000)

	var attempts atomic.Int64

	modTime := time.Now()

	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			// Fail the third GET once; every other request succeeds.
			if r.Method == http.MethodGet && attempts.Add(1) == 3 {
				hj, ok := w.(http.Hijacker)
				if ok {
					conn, _, err := hj.Hijack()
					if err == nil {
						_ = conn.Close()

						return
					}
				}
			}

			http.ServeContent(w, r, "blob.bin", modTime, bytes.NewReader(payload))
		},
	))
	t.Cleanup(srv.Close)

	p := newParallelReaderWith(
		context.Background(), srv.Client(), srv.URL, 0, 2, 8_192, 4,
	)
	if p == nil {
		t.Fatal("newParallelReaderWith returned nil")
	}

	defer func() { _ = p.Close() }()

	got, err := io.ReadAll(p)
	if err != nil {
		t.Fatalf("read after transient failure: %v", err)
	}

	if !bytes.Equal(got, payload) {
		t.Fatalf("payload mismatch after retry: got %d bytes, want %d",
			len(got), len(payload))
	}
}

// A loaded dump server answers with 503 rather than queueing.  That is
// "come back shortly", not a failure, so the lane must retry and the
// stream must still complete — this is what stalled a real import.
func TestParallelReaderRecoversFrom503(t *testing.T) {
	payload := randomPayload(60_000)

	var gets atomic.Int64

	modTime := time.Now()

	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			// Refuse the first two range GETs the way a busy
			// MetaBrainz mirror does.
			if r.Method == http.MethodGet && gets.Add(1) <= 2 {
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(http.StatusServiceUnavailable)

				return
			}

			http.ServeContent(w, r, "blob.bin", modTime, bytes.NewReader(payload))
		},
	))
	t.Cleanup(srv.Close)

	p := newParallelReaderWith(
		context.Background(), srv.Client(), srv.URL, 0, 2, 8_192, 4,
	)
	if p == nil {
		t.Fatal("newParallelReaderWith returned nil")
	}

	defer func() { _ = p.Close() }()

	got, err := io.ReadAll(p)
	if err != nil {
		t.Fatalf("read after 503s: %v", err)
	}

	if !bytes.Equal(got, payload) {
		t.Fatalf("payload mismatch after 503s: got %d bytes, want %d",
			len(got), len(payload))
	}
}

// Retry-After is honoured but clamped, so a hostile or buggy header
// can't park a download lane for hours.
func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		header string
		want   time.Duration
	}{
		{"", 0},
		{"5", 5 * time.Second},
		{"0", 0},
		{"-3", 0},
		{"not-a-number", 0},
		{"Wed, 21 Oct 2026 07:28:00 GMT", 0}, // HTTP-date form: ignored
		{"99999", dumpRetryAfterCap},
	}

	for _, tt := range tests {
		if got := parseRetryAfter(tt.header); got != tt.want {
			t.Errorf("parseRetryAfter(%q) = %v, want %v", tt.header, got, tt.want)
		}
	}
}

// Servers that won't serve ranges must fall back to the sequential
// reader instead of failing the import.
func TestParallelReaderDeclinesWithoutRangeSupport(t *testing.T) {
	payload := randomPayload(100_000)

	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			// No Accept-Ranges header: a plain, non-seekable response.
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			_, _ = w.Write(payload)
		},
	))
	t.Cleanup(srv.Close)

	if p := newParallelReaderWith(
		context.Background(), srv.Client(), srv.URL, 0, 4, 8_192, 6,
	); p != nil {
		_ = p.Close()

		t.Fatal("got a parallel reader for a server without range support, want nil")
	}
}

// Payloads too small to split aren't worth the fan-out.
func TestParallelReaderDeclinesTinyPayload(t *testing.T) {
	payload := randomPayload(1_000)
	srv, _ := serveBlob(t, payload)

	if p := newParallelReaderWith(
		context.Background(), srv.Client(), srv.URL, 0, 4, 8_192, 6,
	); p != nil {
		_ = p.Close()

		t.Fatal("got a parallel reader for a sub-chunk payload, want nil")
	}
}

// Cancelling the import must stop the lanes promptly rather than let
// them keep pulling gigabytes in the background.
func TestParallelReaderStopsOnCancel(t *testing.T) {
	payload := randomPayload(400_000)
	srv, _ := serveBlob(t, payload)

	ctx, cancel := context.WithCancel(context.Background())

	p := newParallelReaderWith(ctx, srv.Client(), srv.URL, 0, 4, 8_192, 6)
	if p == nil {
		t.Fatal("newParallelReaderWith returned nil")
	}

	buf := make([]byte, 100)
	if _, err := io.ReadFull(p, buf); err != nil {
		t.Fatalf("initial read: %v", err)
	}

	cancel()

	// Close waits for every lane, so returning at all proves they exited.
	done := make(chan struct{})

	go func() {
		_ = p.Close()

		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Close did not return after cancel; lanes are still running")
	}
}
