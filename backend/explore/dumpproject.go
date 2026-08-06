//go:build indexbuild

package explore

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/format"
)

// Column-projected fetching of the spark listens dump.
//
// Streaming the tar end to end pulls all 205GB even though the counts
// aggregator reads three columns.  Parquet is columnar and the dump
// server serves Range requests, so the unread columns never have to
// cross the wire: for each member we fetch the footer, look up the byte
// ranges of the wanted column chunks, and download only those.
//
// Measured against listenbrainz-spark-dump-2593 (2026-07-28), the three
// projected columns are 43.4% of the row-group bytes:
//
//	recording_msid                    26.9%   (not read)
//	recording_mbid                    24.1%   ← wanted
//	recording_name                    15.2%   (not read)
//	release_mbid                      13.5%   ← wanted
//	release_name                       6.0%   (not read)
//	artist_credit_mbids.list.element   5.8%   ← wanted
//	artist_name / artist_credit_id / user_id / created / listened_at
//
// The decoder is untouched: the fetched ranges are placed at their real
// offsets in a member-sized buffer, so parseListenParquet still reads
// an ordinary parquet file.  It never touches the unfilled bytes,
// because it only projects those three columns.

const (
	// projectedColumnPaths are the parquet leaf columns sparkListenRow
	// projects.  Kept in sync with that struct by
	// TestProjectedColumnsMatchSchema.
	projectedRecordingMBID = "recording_mbid"
	projectedReleaseMBID   = "release_mbid"
	projectedArtistMBIDs   = "artist_credit_mbids.list.element"

	// rangeGapCoalesce merges two wanted byte ranges separated by less
	// than this much unwanted data.  Below a round-trip's worth of
	// bytes, downloading the gap is cheaper than a second request.
	rangeGapCoalesce = 2 << 20

	// projectFetchLanes is how many Range requests one member's column
	// fetch issues concurrently.  Total in-flight requests are this
	// times projectMembersInFlight, kept at the dumpLanes budget the
	// dump server tolerates.
	projectFetchLanes = 2

	// tarHeaderSize is the size of a tar header block.
	tarHeaderSize = 512

	// walkAheadMembers bounds how far the tar header walk runs ahead of
	// the fetchers.  The walk is one small request per member and is
	// latency-bound, so it needs a long leash to stay off the critical
	// path.
	walkAheadMembers = 64
)

// projectedColumns is the set of leaf column paths to download.
var projectedColumns = []string{
	projectedRecordingMBID,
	projectedReleaseMBID,
	projectedArtistMBIDs,
}

// tarMember is one regular file inside the dump tar.
type tarMember struct {
	// headerOffset is the absolute offset of the member's tar header.
	// This is what the stage-1 checkpoint records: resuming means
	// restarting the walk from here.
	headerOffset int64

	// dataOffset is where the member's contents begin, and size how
	// many bytes they occupy.
	dataOffset int64
	size       int64

	name string

	// typeflag is the tar entry type.  Selecting members by name alone
	// is not enough: an extension header can carry the name of the
	// member it describes, and reading one as data yields PAX records
	// where parquet is expected.
	typeflag byte
}

// nextHeaderOffset is the absolute offset of the following tar header.
func (m tarMember) nextHeaderOffset() int64 {
	return m.dataOffset + m.size + tarPadding(m.size)
}

// byteRange is a half-open [lo, hi) span of a resource.
type byteRange struct {
	lo, hi int64
}

func (r byteRange) len() int64 { return r.hi - r.lo }

// ---------------------------------------------------------------------------
// Range fetching
// ---------------------------------------------------------------------------

// rangeFetcher performs retrying HTTP Range reads against one URL.
type rangeFetcher struct {
	ctx    context.Context
	client *http.Client
	url    string

	// footerProbe is how much of a member's tail to fetch when reading
	// its parquet footer; zero means defaultFooterProbe.  The real
	// dump's footers measure well under that, and an undersized guess
	// costs one extra request rather than failing.
	footerProbe int64
}

// defaultFooterProbe is the tail size used when a fetcher does not
// override it.
const defaultFooterProbe = 64 << 10

func (f *rangeFetcher) probeBytes() int64 {
	if f.footerProbe > 0 {
		return f.footerProbe
	}

	return defaultFooterProbe
}

// fetch reads [lo, hi) into buf (which must be exactly that long),
// retrying transient failures and honouring Retry-After.
func (f *rangeFetcher) fetch(r byteRange, buf []byte) error {
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
			case <-f.ctx.Done():
				return f.ctx.Err()
			case <-time.After(delay):
			}
		}

		retryAfter, err := f.fetchOnce(r, buf)
		if err == nil {
			return nil
		}

		lastErr = err
		wait = retryAfter

		if f.ctx.Err() != nil {
			return f.ctx.Err()
		}
	}

	return fmt.Errorf("%w: %s bytes %d-%d after %d retries: %w",
		ErrDumpStream, f.url, r.lo, r.hi-1, maxStreamRetries, lastErr)
}

func (f *rangeFetcher) fetchOnce(r byteRange, buf []byte) (time.Duration, error) {
	req, err := http.NewRequestWithContext(f.ctx, http.MethodGet, f.url, nil)
	if err != nil {
		return 0, fmt.Errorf("dump range request: %w", err)
	}

	req.Header.Set("User-Agent", lbUserAgent)
	req.Header.Set("Range", "bytes="+strconv.FormatInt(r.lo, 10)+
		"-"+strconv.FormatInt(r.hi-1, 10))

	resp, err := f.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("dump range fetch: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusPartialContent {
		// A loaded dump server answers 503/429 rather than queueing.
		return parseRetryAfter(resp.Header.Get("Retry-After")),
			fmt.Errorf("%w: HTTP %d from %s", ErrDumpStream, resp.StatusCode, f.url)
	}

	if _, err := io.ReadFull(resp.Body, buf); err != nil {
		return 0, fmt.Errorf("dump range read: %w", err)
	}

	return 0, nil
}

// ---------------------------------------------------------------------------
// Tar member walking
// ---------------------------------------------------------------------------

// walkTarMembers reads tar headers by Range request, emitting every
// regular member from startOffset onward.  Only the 512-byte headers are
// downloaded; member contents are skipped by arithmetic rather than by
// pulling them over the wire, which is the whole point.
//
// PAX extension headers (which the dump uses for long names) are
// themselves regular members and are walked like any other; their
// contents are not needed because the aggregator selects members by
// suffix and the extended name only ever restates the short one.
func walkTarMembers(
	ctx context.Context, f *rangeFetcher, startOffset, total int64, out chan<- tarMember,
) error {
	defer close(out)

	hdr := make([]byte, tarHeaderSize)
	offset := startOffset

	for offset+tarHeaderSize <= total {
		if err := ctx.Err(); err != nil {
			return err
		}

		if err := f.fetch(byteRange{offset, offset + tarHeaderSize}, hdr); err != nil {
			return err
		}

		m, ok, err := parseTarHeader(hdr, offset)
		if err != nil {
			return err
		}

		if !ok {
			// Two zero blocks mark end of archive; one is enough to stop.
			return nil
		}

		// A GNU long-name entry would leave the following member's name
		// truncated in its ustar field, and this walk never reads member
		// bodies to recover it.  The dump uses PAX, so this is a format
		// change rather than something to paper over.
		if m.typeflag == tar.TypeGNULongName || m.typeflag == tar.TypeGNULongLink {
			return fmt.Errorf(
				"%w: GNU long-name entry at %d is not supported", ErrDumpFormat, offset,
			)
		}

		select {
		case out <- m:
		case <-ctx.Done():
			return ctx.Err()
		}

		offset = m.nextHeaderOffset()
	}

	return nil
}

// parseTarHeader decodes one 512-byte header block.  Returns ok=false
// at the end-of-archive marker.
func parseTarHeader(hdr []byte, offset int64) (tarMember, bool, error) {
	if isZeroBlock(hdr) {
		return tarMember{}, false, nil
	}

	// The fields are decoded by hand rather than with archive/tar.  A
	// tar.Reader given a lone header block works for plain ustar entries
	// but fails on the PAX extension headers the real dump writes before
	// every member: Next() reads a PAX record's body to merge its
	// attributes, and here that body is not in the block.  Those headers
	// only restate the name the ustar fields already carry, so decoding
	// the fixed fields is both sufficient and immune to that.
	if err := verifyTarChecksum(hdr); err != nil {
		return tarMember{}, false, fmt.Errorf(
			"%w: tar header at %d: %w", ErrDumpFormat, offset, err,
		)
	}

	size, err := parseTarSize(hdr[124:136])
	if err != nil {
		return tarMember{}, false, fmt.Errorf(
			"%w: tar header at %d: %w", ErrDumpFormat, offset, err,
		)
	}

	if size < 0 || offset+tarHeaderSize+size < 0 {
		return tarMember{}, false, fmt.Errorf(
			"%w: tar header at %d declares size %d", ErrDumpFormat, offset, size,
		)
	}

	return tarMember{
		headerOffset: offset,
		dataOffset:   offset + tarHeaderSize,
		size:         size,
		name:         tarName(hdr),
		typeflag:     hdr[156],
	}, true, nil
}

// tarName joins the ustar prefix and name fields.  Long names split
// across the two are rejoined; names carried only in a PAX record are
// not needed, because member selection is by suffix and the dump's
// ustar name field always holds the full path.
func tarName(hdr []byte) string {
	name := trimTarField(hdr[0:100])

	if string(hdr[257:262]) != "ustar" {
		return name
	}

	if prefix := trimTarField(hdr[345:500]); prefix != "" {
		return prefix + "/" + name
	}

	return name
}

func trimTarField(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}

	return string(b)
}

// parseTarSize decodes a tar size field, which is octal ASCII in the
// common case and big-endian base-256 (high bit set) for sizes that do
// not fit — GNU's encoding for files above 8GB.
func parseTarSize(field []byte) (int64, error) {
	if len(field) > 0 && field[0]&0x80 != 0 {
		var n int64

		// The high bit is a flag, not part of the magnitude.
		for i, c := range field {
			if i == 0 {
				c &= 0x7F
			}

			n = n<<8 | int64(c)
		}

		return n, nil
	}

	trimmed := strings.Trim(string(field), " \x00")
	if trimmed == "" {
		return 0, nil
	}

	n, err := strconv.ParseInt(trimmed, 8, 64)
	if err != nil {
		return 0, fmt.Errorf("bad size field %q: %w", field, err)
	}

	return n, nil
}

// verifyTarChecksum checks the header's own checksum.  The walk seeks to
// computed offsets rather than reading forward, so this is what catches
// a desync before it is mistaken for a member.
func verifyTarChecksum(hdr []byte) error {
	stored, err := parseTarSize(hdr[148:156])
	if err != nil {
		return fmt.Errorf("bad checksum field: %w", err)
	}

	var signed, unsigned int64

	for i, c := range hdr {
		// The checksum field itself is treated as spaces.
		if i >= 148 && i < 156 {
			c = ' '
		}

		unsigned += int64(c)
		signed += int64(int8(c))
	}

	if stored != unsigned && stored != signed {
		return fmt.Errorf(
			"%w: checksum %d does not match %d", ErrDumpFormat, stored, unsigned,
		)
	}

	return nil
}

func isZeroBlock(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}

	return true
}

// ---------------------------------------------------------------------------
// Column projection
// ---------------------------------------------------------------------------

// projectedMemberRanges returns the byte ranges of a member that must be
// downloaded to decode projectedColumns: the parquet header magic, the
// wanted column chunks, and the footer.  Offsets are member-relative.
func projectedMemberRanges(meta *format.FileMetaData, size int64, footerLen int64) []byteRange {
	ranges := []byteRange{
		// Leading "PAR1" magic — parquet readers verify it.
		{0, 4},
	}

	for i := range meta.RowGroups {
		for j := range meta.RowGroups[i].Columns {
			col := &meta.RowGroups[i].Columns[j]

			path := strings.Join(col.MetaData.PathInSchema, ".")
			if !slices.Contains(projectedColumns, path) {
				continue
			}

			lo := col.MetaData.DataPageOffset
			if col.MetaData.DictionaryPageOffset != 0 {
				lo = col.MetaData.DictionaryPageOffset
			}

			hi := lo + col.MetaData.TotalCompressedSize
			if lo < 0 || hi > size || hi <= lo {
				continue
			}

			ranges = append(ranges, byteRange{lo, hi})
		}
	}

	// The footer was already fetched to get here, but including it keeps
	// the buffer self-describing for the decoder.
	ranges = append(ranges, byteRange{size - footerLen, size})

	return coalesceRanges(ranges)
}

// coalesceRanges sorts and merges overlapping or near-adjacent ranges so
// each becomes one HTTP request.
func coalesceRanges(ranges []byteRange) []byteRange {
	if len(ranges) == 0 {
		return nil
	}

	slices.SortFunc(ranges, func(a, b byteRange) int {
		return int(a.lo - b.lo)
	})

	merged := ranges[:1]

	for _, r := range ranges[1:] {
		last := &merged[len(merged)-1]
		if r.lo <= last.hi+rangeGapCoalesce {
			last.hi = max(last.hi, r.hi)

			continue
		}

		merged = append(merged, r)
	}

	return merged
}

// ---------------------------------------------------------------------------
// Member fetching
// ---------------------------------------------------------------------------

// fetchProjectedMember downloads only the projected columns of one
// parquet member and returns a member-sized buffer with those bytes at
// their real offsets.  The returned count is how many bytes actually
// crossed the wire.
func fetchProjectedMember(
	ctx context.Context, f *rangeFetcher, m tarMember, buf []byte,
) (int64, error) {
	if int64(len(buf)) != m.size {
		return 0, fmt.Errorf("%w: buffer %d for member of %d bytes",
			ErrDumpFormat, len(buf), m.size)
	}

	// Unfilled gaps must not carry data from a previous member: a stale
	// page header there could be read as valid parquet.
	clear(buf)

	footerLen := min(f.probeBytes(), m.size)

	fetchTail := func(n int64) error {
		return f.fetch(
			byteRange{m.dataOffset + m.size - n, m.dataOffset + m.size},
			buf[m.size-n:],
		)
	}

	if err := fetchTail(footerLen); err != nil {
		return 0, err
	}

	fetched := footerLen

	// A member smaller than the probe arrived whole; there is nothing
	// left to project out of it.
	if footerLen == m.size {
		return fetched, nil
	}

	// A footer larger than the probe leaves the metadata truncated; the
	// trailing length field says how much is really needed.
	if need := declaredFooterLen(buf, m.size); need > footerLen {
		footerLen = min(need, m.size)

		if err := fetchTail(footerLen); err != nil {
			return 0, err
		}

		fetched += footerLen
	}

	meta, err := readFooterMetadata(buf, m.size, footerLen)
	if err != nil {
		return 0, err
	}

	ranges := projectedMemberRanges(meta, m.size, footerLen)

	got, err := fetchRangesConcurrent(ctx, f, m.dataOffset, ranges, buf)
	if err != nil {
		return 0, err
	}

	return fetched + got, nil
}

// declaredFooterLen reads the footer length a parquet file advertises in
// its last eight bytes, plus those eight bytes.  Returns 0 when the tail
// in buf is too short to hold the field.
func declaredFooterLen(buf []byte, size int64) int64 {
	const trailer = 8

	if size < trailer {
		return 0
	}

	return int64(binary.LittleEndian.Uint32(buf[size-trailer:size-4])) + trailer
}

// readFooterMetadata parses the parquet footer sitting in the tail of
// buf.  parquet.OpenFile is given a reader over the tail alone, offset
// so that footer-relative seeks land correctly.
func readFooterMetadata(buf []byte, size, footerLen int64) (*format.FileMetaData, error) {
	// A parquet file ends with the 4-byte footer length followed by
	// "PAR1"; the metadata precedes it.
	if footerLen < 8 {
		return nil, fmt.Errorf("%w: member too small for a parquet footer", ErrDumpFormat)
	}

	// Only the tail of buf holds real bytes at this point, which is all
	// footer parsing needs.  SkipMagicBytes suppresses the leading-magic
	// check: those four bytes are fetched with the column ranges and are
	// verified by the decoder when the assembled buffer is parsed.
	file, err := parquet.OpenFile(bytes.NewReader(buf), size,
		parquet.SkipMagicBytes(true),
		parquet.SkipPageIndex(true),
		parquet.SkipBloomFilters(true),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: parquet footer: %w", ErrDumpFormat, err)
	}

	return file.Metadata(), nil
}

// fetchRangesConcurrent downloads ranges (member-relative) into buf,
// using a few lanes so a member's chunks aren't fetched one round trip
// at a time.
func fetchRangesConcurrent(
	ctx context.Context, f *rangeFetcher, base int64, ranges []byteRange, buf []byte,
) (int64, error) {
	type job struct{ r byteRange }

	jobs := make(chan job)
	errs := make(chan error, projectFetchLanes)

	var fetched atomic.Int64

	fetchCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	laneFetcher := &rangeFetcher{ctx: fetchCtx, client: f.client, url: f.url}

	for range projectFetchLanes {
		go func() {
			var firstErr error

			for j := range jobs {
				if firstErr != nil {
					continue
				}

				dst := buf[j.r.lo:j.r.hi]
				abs := byteRange{base + j.r.lo, base + j.r.hi}

				if err := laneFetcher.fetch(abs, dst); err != nil {
					firstErr = err

					continue
				}

				fetched.Add(j.r.len())
			}

			errs <- firstErr
		}()
	}

	for _, r := range ranges {
		select {
		case jobs <- job{r}:
		case <-fetchCtx.Done():
			close(jobs)

			for range projectFetchLanes {
				<-errs
			}

			return 0, fetchCtx.Err()
		}
	}

	close(jobs)

	var firstErr error

	for range projectFetchLanes {
		if err := <-errs; err != nil && firstErr == nil {
			firstErr = err
		}
	}

	if firstErr != nil {
		return 0, firstErr
	}

	return fetched.Load(), nil
}

// projectionSupported reports whether the dump server will serve the
// Range requests column projection depends on.
func projectionSupported(ctx context.Context, client *http.Client, url string) (int64, bool) {
	size, ok := probeDumpSize(ctx, client, url)
	if !ok || size <= 0 {
		return 0, false
	}

	return size, true
}

var errProjectionUnsupported = errors.New("column projection unavailable")
