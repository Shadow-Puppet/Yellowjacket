package explore

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/parquet-go/parquet-go"
)

// Stage 1 of the dump import: stream the ListenBrainz spark listens
// dump (a plain tar of ~128MB parquet files) and aggregate listen
// counts per recording, release, and artist MBID.  Nothing is written
// to disk except counts.bin — each parquet member is buffered in RAM,
// parsed, and discarded.  The aggregate map lives in RAM (~40M entities
// ≈ 2GB) and is flushed atomically with the stream byte offset so an
// interrupted import resumes without re-downloading processed data.

const (
	// countKindRecording etc. tag entries in the counts map/file.
	countKindRecording = byte(1)
	countKindRelease   = byte(2)
	countKindArtist    = byte(3)

	// countsFlushEveryMembers controls checkpoint frequency.  Each
	// flush rewrites counts.bin (~1GB by the end), so this trades
	// checkpoint I/O against re-download on crash (~150 members ≈
	// 19GB of stream progress).
	countsFlushEveryMembers = 150

	// countsProgressEveryMembers controls progress log frequency.
	countsProgressEveryMembers = 50

	// parquetParseWorkers is the number of concurrent parquet
	// decoders.  Bounded to limit RAM: each worker holds one
	// ~128MB member buffer.
	parquetParseWorkers = 3

	// maxParquetMemberSize guards against unexpected dump format
	// changes blowing out RAM.
	maxParquetMemberSize = 1 << 30

	// countsFileMagic identifies + versions the counts file format.
	countsFileMagic = "YJCNTS01"
)

// ErrDumpFormat is returned when dump contents don't match the
// expected format.
var ErrDumpFormat = errors.New("unexpected dump format")

// mbidKey is a parsed UUID plus an entity-kind tag, used as the counts
// map key.  17 bytes instead of a 36-byte string keeps the ~40M-entry
// map around 2GB.
type mbidKey [17]byte

func makeMBIDKey(kind byte, mbid string) (mbidKey, bool) {
	var k mbidKey

	k[0] = kind

	if !parseUUID(mbid, k[1:]) {
		return k, false
	}

	return k, true
}

// parseUUID parses a canonical 36-char UUID string into 16 bytes.
// Returns false for anything malformed.
func parseUUID(s string, out []byte) bool {
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return false
	}

	j := 0

	for i := 0; i < 36; i++ {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}

		hi := hexNibble(s[i])
		i++

		lo := hexNibble(s[i])
		if hi == 0xFF || lo == 0xFF {
			return false
		}

		out[j] = hi<<4 | lo
		j++
	}

	return true
}

func hexNibble(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	default:
		return 0xFF
	}
}

func formatUUID(b []byte) string {
	const hexdigits = "0123456789abcdef"

	out := make([]byte, 36)
	j := 0

	for i := range 16 {
		if i == 4 || i == 6 || i == 8 || i == 10 {
			out[j] = '-'
			j++
		}

		out[j] = hexdigits[b[i]>>4]
		out[j+1] = hexdigits[b[i]&0x0F]
		j += 2
	}

	return string(out)
}

// countsState is the checkpointed stage-1 state: the counts map plus
// the stream position it corresponds to.
type countsState struct {
	// SparkURL pins the dump being processed so a resume never mixes
	// two different dumps.
	SparkURL string `json:"sparkUrl"`

	// Offset is the byte offset of the next unprocessed tar member
	// header (exact — includes the padding of the previous member).
	Offset int64 `json:"offset"`

	// MemberIdx is the index of the next unprocessed parquet member
	// (logging only; Offset is authoritative for resume).
	MemberIdx int `json:"memberIdx"`

	// Done marks stage 1 complete.
	Done bool `json:"done"`

	counts map[mbidKey]uint32
}

// sparkListenRow is the projection of the spark listens parquet schema
// that the aggregator reads.  All other columns are skipped.
type sparkListenRow struct {
	RecordingMBID string   `parquet:"recording_mbid,optional"`
	ReleaseMBID   string   `parquet:"release_mbid,optional"`
	ArtistMBIDs   []string `parquet:"artist_credit_mbids,optional,list"`
}

type countParseJob struct {
	idx       int
	endOffset int64 // exact offset of the next member header
	buf       []byte
}

type countParseResult struct {
	idx       int
	endOffset int64
	deltas    map[mbidKey]uint32
	err       error
}

// aggregateListenCounts runs stage 1 to completion (or ctx cancel),
// checkpointing to the staging counts file as it goes.
func (imp *dumpImporter) aggregateListenCounts(ctx context.Context, st *countsState) error {
	if st.counts == nil {
		st.counts = make(map[mbidKey]uint32, 1<<20)
	}

	stream := newResumableReader(ctx, imp.httpClient, st.SparkURL, st.Offset)

	defer func() { _ = stream.Close() }()

	buffered := bufio.NewReaderSize(stream, 1<<20)
	tr := tar.NewReader(buffered)

	// consumedOffset is the absolute stream position of everything the
	// tar reader has consumed: bytes delivered by HTTP minus bytes
	// still sitting in the bufio buffer.
	consumedOffset := func() int64 {
		return stream.Offset - int64(buffered.Buffered())
	}

	jobs := make(chan countParseJob)
	results := make(chan countParseResult, parquetParseWorkers)
	applierDone := make(chan struct{})
	bufPool := sync.Pool{New: func() any { return []byte(nil) }}

	var workerWG sync.WaitGroup

	for range parquetParseWorkers {
		workerWG.Add(1)

		go func() {
			defer workerWG.Done()

			for job := range jobs {
				deltas, err := parseListenParquet(job.buf)
				// Buffer reuse across members is intentional.
				bufPool.Put(job.buf[:0]) //nolint:staticcheck

				results <- countParseResult{
					idx:       job.idx,
					endOffset: job.endOffset,
					deltas:    deltas,
					err:       err,
				}
			}
		}()
	}

	// The applier merges results into st in member order, so every
	// checkpoint is a contiguous prefix of the stream.  It owns
	// st.counts, st.Offset, and st.MemberIdx until applierDone closes;
	// on error it keeps draining results so nothing deadlocks.
	var applyErr error

	go func() {
		defer close(applierDone)

		pending := make(map[int]countParseResult)
		next := st.MemberIdx
		lastFlushed := st.MemberIdx

		for res := range results {
			if applyErr != nil {
				continue
			}

			pending[res.idx] = res

			for {
				r, ok := pending[next]
				if !ok {
					break
				}

				delete(pending, next)

				if r.err != nil {
					applyErr = r.err

					break
				}

				for k, v := range r.deltas {
					st.counts[k] += v
				}

				next++
				st.MemberIdx = next
				st.Offset = r.endOffset

				if next-lastFlushed >= countsFlushEveryMembers {
					if err := imp.writeCountsFile(st); err != nil {
						applyErr = err

						break
					}

					lastFlushed = next

					imp.logCountsProgress(next, r.endOffset, stream.Size, len(st.counts))

					if err := imp.checkDiskHeadroom(); err != nil {
						applyErr = err

						break
					}
				} else if next%countsProgressEveryMembers == 0 {
					imp.logCountsProgress(next, r.endOffset, stream.Size, len(st.counts))
				}
			}
		}
	}()

	memberIdx := st.MemberIdx
	readErr := error(nil)

readLoop:
	for {
		if err := ctx.Err(); err != nil {
			readErr = err

			break
		}

		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			readErr = fmt.Errorf("listens tar: %w", err)

			break
		}

		if hdr.Typeflag != tar.TypeReg || !strings.HasSuffix(hdr.Name, ".parquet") {
			continue
		}

		if hdr.Size > maxParquetMemberSize {
			readErr = fmt.Errorf("%w: parquet member %s is %d bytes", ErrDumpFormat, hdr.Name, hdr.Size)

			break
		}

		buf, _ := bufPool.Get().([]byte)
		if cap(buf) < int(hdr.Size) {
			buf = make([]byte, hdr.Size)
		}

		buf = buf[:hdr.Size]

		if _, err := io.ReadFull(tr, buf); err != nil {
			readErr = fmt.Errorf("listens tar member read: %w", err)

			break
		}

		// Exact next-header offset: position after the entry data plus
		// the entry's block padding.  Correct even when the next member
		// uses PAX extension headers (those start at its header offset).
		endOffset := consumedOffset() + tarPadding(hdr.Size)

		select {
		case jobs <- countParseJob{idx: memberIdx, endOffset: endOffset, buf: buf}:
		case <-ctx.Done():
			readErr = ctx.Err()

			break readLoop
		}

		memberIdx++
	}

	close(jobs)
	workerWG.Wait()
	close(results)
	<-applierDone

	if readErr == nil {
		readErr = applyErr
	}

	if readErr != nil {
		// Best-effort checkpoint of applied progress before bailing,
		// so even a cancelled run resumes where it left off.
		_ = imp.writeCountsFile(st)

		return readErr
	}

	st.Done = true

	if err := imp.writeCountsFile(st); err != nil {
		return err
	}

	imp.logger.Info("dump import: listen counts complete",
		"members", st.MemberIdx,
		"entities", len(st.counts),
	)

	return nil
}

// tarPadding returns the number of zero bytes following a tar entry of
// the given size (entries are padded to 512-byte blocks).
func tarPadding(size int64) int64 {
	const block = 512

	return (block - size%block) % block
}

// parseListenParquet decodes one parquet member and returns the
// per-entity listen-count deltas.
func parseListenParquet(buf []byte) (map[mbidKey]uint32, error) {
	reader := parquet.NewGenericReader[sparkListenRow](bytes.NewReader(buf))

	defer func() { _ = reader.Close() }()

	deltas := make(map[mbidKey]uint32, 1<<18)
	rows := make([]sparkListenRow, 4096)

	for {
		n, err := reader.Read(rows)

		for _, row := range rows[:n] {
			key, ok := makeMBIDKey(countKindRecording, row.RecordingMBID)
			if !ok {
				// Unmapped listen — no usable recording MBID.
				continue
			}

			deltas[key]++

			if relKey, relOK := makeMBIDKey(countKindRelease, row.ReleaseMBID); relOK {
				deltas[relKey]++
			}

			for _, artist := range row.ArtistMBIDs {
				if artKey, artOK := makeMBIDKey(countKindArtist, artist); artOK {
					deltas[artKey]++
				}
			}
		}

		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nil, fmt.Errorf("parquet read: %w", err)
		}

		if n == 0 {
			break
		}
	}

	return deltas, nil
}

// ---------------------------------------------------------------------------
// counts.bin persistence
// ---------------------------------------------------------------------------

// writeCountsFile atomically persists the counts map + stream position
// (write to temp file, fsync, rename).
func (imp *dumpImporter) writeCountsFile(st *countsState) error {
	tmp := imp.countsPath() + ".tmp"

	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("counts file create: %w", err)
	}

	w := bufio.NewWriterSize(f, 1<<20)

	meta, err := json.Marshal(st)
	if err != nil {
		_ = f.Close()

		return fmt.Errorf("counts meta marshal: %w", err)
	}

	_, _ = w.WriteString(countsFileMagic)

	var lenBuf [4]byte

	binary.LittleEndian.PutUint32(lenBuf[:], uint32(len(meta)))
	_, _ = w.Write(lenBuf[:])
	_, _ = w.Write(meta)

	var rec [21]byte

	for k, v := range st.counts {
		copy(rec[:17], k[:])
		binary.LittleEndian.PutUint32(rec[17:], v)

		if _, err := w.Write(rec[:]); err != nil {
			_ = f.Close()

			return fmt.Errorf("counts file write: %w", err)
		}
	}

	if err := w.Flush(); err != nil {
		_ = f.Close()

		return fmt.Errorf("counts file flush: %w", err)
	}

	if err := f.Sync(); err != nil {
		_ = f.Close()

		return fmt.Errorf("counts file sync: %w", err)
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("counts file close: %w", err)
	}

	if err := os.Rename(tmp, imp.countsPath()); err != nil {
		return fmt.Errorf("counts file rename: %w", err)
	}

	return nil
}

// readCountsFile loads a previously checkpointed counts file.  Returns
// (nil, nil) when no checkpoint exists.
func (imp *dumpImporter) readCountsFile() (*countsState, error) {
	f, err := os.Open(imp.countsPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil //nolint:nilnil // no checkpoint is a valid, non-error state
	}

	if err != nil {
		return nil, fmt.Errorf("counts file open: %w", err)
	}

	defer func() { _ = f.Close() }()

	r := bufio.NewReaderSize(f, 1<<20)

	magic := make([]byte, len(countsFileMagic))
	if _, err := io.ReadFull(r, magic); err != nil || string(magic) != countsFileMagic {
		return nil, fmt.Errorf("%w: bad counts file header", ErrDumpFormat)
	}

	var lenBuf [4]byte

	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return nil, fmt.Errorf("counts meta length: %w", err)
	}

	meta := make([]byte, binary.LittleEndian.Uint32(lenBuf[:]))
	if _, err := io.ReadFull(r, meta); err != nil {
		return nil, fmt.Errorf("counts meta read: %w", err)
	}

	st := &countsState{}
	if err := json.Unmarshal(meta, st); err != nil {
		return nil, fmt.Errorf("counts meta unmarshal: %w", err)
	}

	st.counts = make(map[mbidKey]uint32, 1<<20)

	var rec [21]byte

	for {
		if _, err := io.ReadFull(r, rec[:]); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}

			return nil, fmt.Errorf("counts record read: %w", err)
		}

		var k mbidKey

		copy(k[:], rec[:17])
		st.counts[k] = binary.LittleEndian.Uint32(rec[17:])
	}

	return st, nil
}

func (imp *dumpImporter) logCountsProgress(members int, offset, size int64, entities int) {
	pct := float64(0)
	if size > 0 {
		pct = float64(offset) / float64(size) * 100
	}

	imp.logger.Info("dump import: listen counts progress",
		"members", members,
		"gb", fmt.Sprintf("%.1f", float64(offset)/(1<<30)),
		"pct", fmt.Sprintf("%.1f", pct),
		"entities", entities,
	)

	imp.setStageProgress(dumpStageCounts, int(pct), 100)
}
