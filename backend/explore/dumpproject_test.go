//go:build indexbuild

package explore

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"yellowjacket/backend/database"
)

// serveRangeBlob serves payload with Range support, counting the bytes
// actually delivered so a test can assert how much crossed the wire.
func serveRangeBlob(t *testing.T, payload []byte) (*httptest.Server, *atomic.Int64) {
	t.Helper()

	var served atomic.Int64

	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			rec := &countingResponseWriter{ResponseWriter: w, n: &served}
			http.ServeContent(rec, r, "dump.tar", time.Unix(0, 0), bytes.NewReader(payload))
		},
	))

	t.Cleanup(srv.Close)

	return srv, &served
}

type countingResponseWriter struct {
	http.ResponseWriter

	n *atomic.Int64
}

func (w *countingResponseWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.n.Add(int64(n))

	return n, err
}

// bigSparkTar builds a tar whose single parquet member has enough rows
// that the unprojected columns dominate its size.
func bigSparkTar(t *testing.T) []byte {
	t.Helper()

	rows := make([]sparkFixtureRow, 0, 20_000)

	for i := range 20_000 {
		rows = append(rows, sparkFixtureRow{
			ListenedAt: int64(i),
			UserID:     int64(i % 977),
			// A high-cardinality column that is not projected: it is what
			// projection must avoid downloading.
			ArtistName:    strings.Repeat("padding-", 12) + string(rune('a'+i%26)) + itoa(i),
			RecordingMBID: recA,
			ReleaseMBID:   relA,
			ArtistMBIDs:   []string{artA},
		})
	}

	name := "listenbrainz-spark-dump-1-20260101-000003-full/1.parquet"

	return makeTar(t, map[string][]byte{name: makeParquet(t, rows)}, []string{name})
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}

	var b []byte

	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}

	return string(b)
}

func TestWalkTarMembersFindsMembersWithoutReadingThem(t *testing.T) {
	t.Parallel()

	payload := fixtureSparkTar(t)
	srv, served := serveRangeBlob(t, payload)

	f := &rangeFetcher{ctx: t.Context(), client: srv.Client(), url: srv.URL}
	out := make(chan tarMember, 8)

	if err := walkTarMembers(t.Context(), f, 0, int64(len(payload)), out); err != nil {
		t.Fatalf("walk: %v", err)
	}

	var members []tarMember
	for m := range out {
		members = append(members, m)
	}

	if len(members) != 2 {
		t.Fatalf("got %d members, want 2", len(members))
	}

	for i, m := range members {
		if !strings.HasSuffix(m.name, ".parquet") {
			t.Errorf("member %d: name %q", i, m.name)
		}

		if m.size <= 0 {
			t.Errorf("member %d: size %d", i, m.size)
		}

		// The member's declared bytes must match what the tar really holds.
		want := payload[m.dataOffset : m.dataOffset+m.size]
		if !bytes.HasPrefix(want, []byte("PAR1")) {
			t.Errorf("member %d: data offset %d does not start a parquet file", i, m.dataOffset)
		}
	}

	// The walk must read headers only — not the multi-KB member bodies.
	if n := served.Load(); n > int64(4*tarHeaderSize) {
		t.Errorf("walk downloaded %d bytes, want only tar headers", n)
	}
}

func TestFetchProjectedMemberMatchesFullParse(t *testing.T) {
	t.Parallel()

	payload := bigSparkTar(t)
	srv, served := serveRangeBlob(t, payload)

	f := &rangeFetcher{ctx: t.Context(), client: srv.Client(), url: srv.URL}
	out := make(chan tarMember, 4)

	if err := walkTarMembers(t.Context(), f, 0, int64(len(payload)), out); err != nil {
		t.Fatalf("walk: %v", err)
	}

	var member tarMember

	for m := range out {
		if strings.HasSuffix(m.name, ".parquet") {
			member = m
		}
	}

	if member.size == 0 {
		t.Fatal("no parquet member found")
	}

	// Ground truth: parse the member as the sequential path would.
	full := payload[member.dataOffset : member.dataOffset+member.size]

	wantDeltas, err := parseListenParquet(full)
	if err != nil {
		t.Fatalf("full parse: %v", err)
	}

	served.Store(0)

	buf := make([]byte, member.size)

	fetched, err := fetchProjectedMember(t.Context(), f, member, buf)
	if err != nil {
		t.Fatalf("projected fetch: %v", err)
	}

	gotDeltas, err := parseListenParquet(buf)
	if err != nil {
		t.Fatalf("projected parse: %v", err)
	}

	if len(gotDeltas) != len(wantDeltas) {
		t.Fatalf("projected parse produced %d entities, want %d", len(gotDeltas), len(wantDeltas))
	}

	for k, want := range wantDeltas {
		if got := gotDeltas[k]; got != want {
			t.Errorf("entity %x: count %d, want %d", k, got, want)
		}
	}

	// The point of the exercise: materially fewer bytes than the member.
	if fetched >= member.size {
		t.Errorf("projected fetch pulled %d bytes of a %d byte member", fetched, member.size)
	}

	t.Logf("projected fetch: %d of %d bytes (%.1f%%)",
		fetched, member.size, 100*float64(fetched)/float64(member.size))
}

func TestProjectedMemberRangesSkipsUnwantedColumns(t *testing.T) {
	t.Parallel()

	payload := bigSparkTar(t)
	srv, _ := serveRangeBlob(t, payload)

	f := &rangeFetcher{ctx: t.Context(), client: srv.Client(), url: srv.URL}
	out := make(chan tarMember, 4)

	if err := walkTarMembers(t.Context(), f, 0, int64(len(payload)), out); err != nil {
		t.Fatalf("walk: %v", err)
	}

	var member tarMember

	for m := range out {
		if strings.HasSuffix(m.name, ".parquet") {
			member = m
		}
	}

	buf := make([]byte, member.size)
	footerLen := min(f.probeBytes(), member.size)
	tailStart := member.dataOffset + member.size - footerLen
	copy(buf[member.size-footerLen:], payload[tailStart:member.dataOffset+member.size])

	meta, err := readFooterMetadata(buf, member.size, footerLen)
	if err != nil {
		t.Fatalf("footer: %v", err)
	}

	ranges := projectedMemberRanges(meta, member.size, footerLen)
	if len(ranges) == 0 {
		t.Fatal("no ranges computed")
	}

	var total int64

	for _, r := range ranges {
		if r.lo < 0 || r.hi > member.size || r.hi <= r.lo {
			t.Fatalf("range %v out of bounds for member size %d", r, member.size)
		}

		total += r.len()
	}

	if total >= member.size {
		t.Errorf("projected ranges cover %d of %d bytes", total, member.size)
	}
}

func TestCoalesceRangesMergesNeighbours(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   []byteRange
		want []byteRange
	}{
		{
			name: "adjacent merge",
			in:   []byteRange{{0, 100}, {100, 200}},
			want: []byteRange{{0, 200}},
		},
		{
			name: "small gap merges",
			in:   []byteRange{{0, 100}, {100 + rangeGapCoalesce - 1, 500}},
			want: []byteRange{{0, 500}},
		},
		{
			name: "large gap stays split",
			in:   []byteRange{{0, 100}, {100 + rangeGapCoalesce + 1, 500}},
			want: []byteRange{{0, 100}, {100 + rangeGapCoalesce + 1, 500}},
		},
		{
			name: "unsorted input",
			in:   []byteRange{{10 * rangeGapCoalesce, 10*rangeGapCoalesce + 100}, {0, 100}},
			want: []byteRange{{0, 100}, {10 * rangeGapCoalesce, 10*rangeGapCoalesce + 100}},
		},
		{
			name: "overlap",
			in:   []byteRange{{0, 300}, {100, 200}},
			want: []byteRange{{0, 300}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := coalesceRanges(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}

			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("got %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestFetchProjectedMemberRejectsWrongBuffer(t *testing.T) {
	t.Parallel()

	f := &rangeFetcher{
		ctx:    context.Background(),
		client: http.DefaultClient,
		url:    "http://example.invalid",
	}

	if _, err := fetchProjectedMember(
		t.Context(), f, tarMember{size: 100}, make([]byte, 50),
	); err == nil {
		t.Fatal("expected an error for a mis-sized buffer")
	}
}

// serveDumpNoRanges serves the spark dump without advertising Range
// support, which is what forces the streamed fallback.
func serveDumpNoRanges(t *testing.T, sparkTar []byte) *httptest.Server {
	t.Helper()

	const (
		listensDir = "listenbrainz-dump-1-20260101-000003-full"
		sparkFile  = "listenbrainz-spark-dump-1-20260101-000003-full.tar"
	)

	mux := http.NewServeMux()

	mux.HandleFunc("/listens/", func(w http.ResponseWriter, r *http.Request) {
		switch strings.TrimPrefix(r.URL.Path, "/listens/") {
		case "":
			_, _ = fmt.Fprintf(w, `<a href="%s/">%s/</a>`, listensDir, listensDir)
		case listensDir + "/":
			_, _ = fmt.Fprintf(w, `<a href="%s">%s</a>`, sparkFile, sparkFile)
		case listensDir + "/" + sparkFile:
			// No Accept-Ranges, and Range headers are ignored.
			w.Header().Set("Content-Length", strconv.Itoa(len(sparkTar)))
			_, _ = w.Write(sparkTar)
		default:
			http.NotFound(w, r)
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv
}

// The projected and streamed paths must agree exactly: projection is an
// optimisation, not a different answer.
func TestProjectedAndStreamedCountsAgree(t *testing.T) {
	t.Parallel()

	sparkTar := bigSparkTar(t)

	counts := func(srv *httptest.Server) *countsState {
		t.Helper()

		db := database.NewTestDB(t)
		si := NewSearchIndex(db, nil, nil, testLogger())
		imp := testImporter(t, si, srv)

		url := srv.URL + "/listens/listenbrainz-dump-1-20260101-000003-full/" +
			"listenbrainz-spark-dump-1-20260101-000003-full.tar"

		st := &countsState{SparkURL: url}
		if err := imp.aggregateListenCounts(t.Context(), st); err != nil {
			t.Fatalf("aggregate: %v", err)
		}

		if !st.Done {
			t.Fatal("aggregation did not complete")
		}

		return st
	}

	ranged := serveDumps(t, sparkTar, nil)
	plain := serveDumpNoRanges(t, sparkTar)

	projected := counts(ranged)
	streamed := counts(plain)

	if len(projected.counts) == 0 {
		t.Fatal("projected run produced no counts")
	}

	if len(projected.counts) != len(streamed.counts) {
		t.Fatalf("projected %d entities, streamed %d",
			len(projected.counts), len(streamed.counts))
	}

	for k, want := range streamed.counts {
		if got := projected.counts[k]; got != want {
			t.Errorf("entity %x: projected %d, streamed %d", k, got, want)
		}
	}

	if projected.Offset != streamed.Offset {
		t.Errorf("checkpoint offset: projected %d, streamed %d",
			projected.Offset, streamed.Offset)
	}

	if projected.MemberIdx != streamed.MemberIdx {
		t.Errorf("member index: projected %d, streamed %d",
			projected.MemberIdx, streamed.MemberIdx)
	}
}

func TestDeclaredFooterLenReadsTrailer(t *testing.T) {
	t.Parallel()

	payload := bigSparkTar(t)
	srv, _ := serveRangeBlob(t, payload)

	f := &rangeFetcher{ctx: t.Context(), client: srv.Client(), url: srv.URL}
	out := make(chan tarMember, 4)

	if err := walkTarMembers(t.Context(), f, 0, int64(len(payload)), out); err != nil {
		t.Fatalf("walk: %v", err)
	}

	var m tarMember

	for got := range out {
		if strings.HasSuffix(got.name, ".parquet") {
			m = got
		}
	}

	member := payload[m.dataOffset : m.dataOffset+m.size]

	got := declaredFooterLen(member, m.size)
	if got <= 8 || got > m.size {
		t.Fatalf("declared footer length %d for a %d byte member", got, m.size)
	}

	// A tail of exactly that length must be enough to parse the footer.
	buf := make([]byte, m.size)
	copy(buf[m.size-got:], member[m.size-got:])

	if _, err := readFooterMetadata(buf, m.size, got); err != nil {
		t.Fatalf("footer parse with declared length: %v", err)
	}
}

// A footer bigger than the initial probe must trigger a second, larger
// tail fetch rather than failing.
func TestFetchProjectedMemberRefetchesOversizedFooter(t *testing.T) {
	t.Parallel()

	payload := bigSparkTar(t)
	srv, _ := serveRangeBlob(t, payload)

	f := &rangeFetcher{ctx: t.Context(), client: srv.Client(), url: srv.URL}
	out := make(chan tarMember, 4)

	if err := walkTarMembers(t.Context(), f, 0, int64(len(payload)), out); err != nil {
		t.Fatalf("walk: %v", err)
	}

	var m tarMember

	for got := range out {
		if strings.HasSuffix(got.name, ".parquet") {
			m = got
		}
	}

	member := payload[m.dataOffset : m.dataOffset+m.size]

	want, err := parseListenParquet(member)
	if err != nil {
		t.Fatalf("full parse: %v", err)
	}

	// Force the probe to land short of the real footer.
	footer := declaredFooterLen(member, m.size)

	short := &rangeFetcher{
		ctx:         t.Context(),
		client:      srv.Client(),
		url:         srv.URL,
		footerProbe: footer / 2,
	}

	buf := make([]byte, m.size)

	if _, err := fetchProjectedMember(t.Context(), short, m, buf); err != nil {
		t.Fatalf("projected fetch with short probe: %v", err)
	}

	got, err := parseListenParquet(buf)
	if err != nil {
		t.Fatalf("projected parse: %v", err)
	}

	if len(got) != len(want) {
		t.Fatalf("got %d entities, want %d", len(got), len(want))
	}
}

// makePaxTar writes members preceded by PAX extension headers, which is
// how the real ListenBrainz dump is written.  A lone header block from
// such an archive cannot be decoded with archive/tar, so this is the
// layout the walker must handle directly.
func makePaxTar(t *testing.T, members map[string][]byte, order []string) []byte {
	t.Helper()

	var buf bytes.Buffer

	tw := tar.NewWriter(&buf)

	for _, name := range order {
		data := members[name]
		hdr := &tar.Header{
			Name:     name,
			Mode:     0o644,
			Size:     int64(len(data)),
			Typeflag: tar.TypeReg,
			// Sub-second precision cannot be expressed in ustar, so the
			// writer emits a PAX extension header ahead of the member.
			ModTime: time.Unix(1700000000, 123456789),
			Format:  tar.FormatPAX,
		}

		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("tar header: %v", err)
		}

		if _, err := tw.Write(data); err != nil {
			t.Fatalf("tar write: %v", err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}

	return buf.Bytes()
}

func TestWalkTarMembersHandlesPaxHeaders(t *testing.T) {
	t.Parallel()

	rows := listensOf(4, recA, relA, []string{artA})
	name := "listenbrainz-spark-dump-1-20260101-000003-full/1.parquet"
	member := makeParquet(t, rows)

	payload := makePaxTar(t, map[string][]byte{name: member}, []string{name})

	// Guard the premise: the fixture really does contain a PAX header.
	if !bytes.Contains(payload[:4096], []byte("PaxHeader")) {
		t.Fatal("fixture has no PAX extension header")
	}

	srv, _ := serveRangeBlob(t, payload)

	f := &rangeFetcher{ctx: t.Context(), client: srv.Client(), url: srv.URL}
	out := make(chan tarMember, 16)

	if err := walkTarMembers(t.Context(), f, 0, int64(len(payload)), out); err != nil {
		t.Fatalf("walk: %v", err)
	}

	var found tarMember

	for m := range out {
		if isProjectableMember(m) {
			found = m
		}
	}

	if found.size != int64(len(member)) {
		t.Fatalf("member size %d, want %d", found.size, len(member))
	}

	if got := payload[found.dataOffset : found.dataOffset+4]; string(got) != "PAR1" {
		t.Fatalf("data offset %d does not start a parquet file (%q)", found.dataOffset, got)
	}
}

// End to end through the aggregator, against a PAX archive.
func TestAggregateProjectedWithPaxHeaders(t *testing.T) {
	t.Parallel()

	prefix := "listenbrainz-spark-dump-1-20260101-000003-full/"
	m1 := prefix + "1.parquet"
	m2 := prefix + "2.parquet"

	payload := makePaxTar(t,
		map[string][]byte{
			m1: makeParquet(t, listensOf(12, recA, relA, []string{artA})),
			m2: makeParquet(t, listensOf(11, recB, relA, []string{artA})),
		},
		[]string{m1, m2},
	)

	srv := serveDumps(t, payload, nil)

	db := database.NewTestDB(t)
	si := NewSearchIndex(db, nil, nil, testLogger())
	imp := testImporter(t, si, srv)

	url := srv.URL + "/listens/listenbrainz-dump-1-20260101-000003-full/" +
		"listenbrainz-spark-dump-1-20260101-000003-full.tar"

	st := &countsState{SparkURL: url}
	if err := imp.aggregateListenCounts(t.Context(), st); err != nil {
		t.Fatalf("aggregate: %v", err)
	}

	if !st.Done {
		t.Fatal("aggregation did not complete")
	}

	assert := func(kind byte, mbid string, want uint32) {
		t.Helper()

		key, ok := makeMBIDKey(kind, mbid)
		if !ok {
			t.Fatalf("bad fixture mbid %s", mbid)
		}

		if got := st.counts[key]; got != want {
			t.Errorf("count(kind=%d, %s) = %d, want %d", kind, mbid, got, want)
		}
	}

	assert(countKindRecording, recA, 12)
	assert(countKindRecording, recB, 11)
	assert(countKindRelease, relA, 23)
	assert(countKindArtist, artA, 23)
}

func TestParseTarHeaderRejectsCorruptBlock(t *testing.T) {
	t.Parallel()

	payload := fixtureSparkTar(t)

	valid := make([]byte, tarHeaderSize)
	copy(valid, payload[:tarHeaderSize])

	if _, ok, err := parseTarHeader(valid, 0); err != nil || !ok {
		t.Fatalf("valid header rejected: ok=%v err=%v", ok, err)
	}

	// A desynced walk lands mid-member; the checksum must catch it.
	corrupt := make([]byte, tarHeaderSize)
	copy(corrupt, valid)
	corrupt[10] ^= 0xFF

	if _, _, err := parseTarHeader(corrupt, 0); err == nil {
		t.Fatal("corrupt header accepted")
	}
}

// A member smaller than the footer probe arrives in the probe request;
// it must not then be fetched a second time.
func TestFetchProjectedMemberSkipsRefetchForTinyMembers(t *testing.T) {
	t.Parallel()

	rows := listensOf(2, recA, relA, []string{artA})
	name := "listenbrainz-spark-dump-1-20260101-000003-full/1.parquet"
	member := makeParquet(t, rows)

	if int64(len(member)) >= defaultFooterProbe {
		t.Skipf("fixture member is %d bytes, not smaller than the probe", len(member))
	}

	payload := makeTar(t, map[string][]byte{name: member}, []string{name})
	srv, served := serveRangeBlob(t, payload)

	f := &rangeFetcher{ctx: t.Context(), client: srv.Client(), url: srv.URL}
	out := make(chan tarMember, 8)

	if err := walkTarMembers(t.Context(), f, 0, int64(len(payload)), out); err != nil {
		t.Fatalf("walk: %v", err)
	}

	var m tarMember

	for got := range out {
		if isProjectableMember(got) {
			m = got
		}
	}

	served.Store(0)

	buf := make([]byte, m.size)

	fetched, err := fetchProjectedMember(t.Context(), f, m, buf)
	if err != nil {
		t.Fatalf("projected fetch: %v", err)
	}

	if fetched != m.size {
		t.Errorf("fetched %d bytes for a %d byte member", fetched, m.size)
	}

	if n := served.Load(); n > m.size {
		t.Errorf("server delivered %d bytes for a %d byte member", n, m.size)
	}

	if _, err := parseListenParquet(buf); err != nil {
		t.Fatalf("parse: %v", err)
	}
}
