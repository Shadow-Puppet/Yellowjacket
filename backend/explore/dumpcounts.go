//go:build indexbuild

package explore

import (
	"archive/tar"
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
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

	// countsFlushEveryMembers controls checkpoint frequency.  Each
	// flush rewrites counts.bin (~1GB by the end), so this trades
	// checkpoint I/O against re-download on crash (~150 members ≈
	// 19GB of stream progress).
	countsFlushEveryMembers = 150

	// countsUIRefreshInterval is how often the live download line is
	// pushed to the UI.  A parquet member is ~128MB, so member
	// boundaries are minutes apart on a typical connection — sampling
	// the stream position instead keeps the stage visibly moving.
	countsUIRefreshInterval = 3 * time.Second

	// countsLogInterval and countsJobLogInterval throttle the two log
	// surfaces: the app log gets a line every few minutes, the jobs
	// panel a coarser one.  Checkpoints always log to both.
	countsLogInterval    = 2 * time.Minute
	countsJobLogInterval = 15 * time.Minute

	// countsStallAfter is how long the stream position may stand still
	// before progress is reported as stalled rather than as a rate.
	countsStallAfter = 45 * time.Second

	// countsRateSmoothing is the EWMA weight given to the newest
	// throughput sample, trading responsiveness against jitter.
	countsRateSmoothing = 0.25

	// parquetParseWorkers is the number of concurrent parquet
	// decoders.  Bounded to limit RAM: each worker holds one
	// ~128MB member buffer.
	parquetParseWorkers = 3

	// countsFileMagic identifies + versions the counts file format.
	countsFileMagic = "YJCNTS01"
)

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
//
// Column projection is tried first: it downloads only the three MBID
// columns the aggregator reads, which is well under half the archive.
// It needs a Range-serving origin, so a server that won't range falls
// back to streaming the whole tar.
func (imp *dumpImporter) aggregateListenCounts(ctx context.Context, st *countsState) error {
	if st.counts == nil {
		st.counts = make(map[mbidKey]uint32, 1<<20)
	}

	if size, ok := projectionSupported(ctx, imp.httpClient, st.SparkURL); ok {
		err := imp.aggregateProjected(ctx, st, size)
		if !errors.Is(err, errProjectionUnsupported) {
			return err
		}

		imp.logger.Warn("dump import: column projection unavailable, streaming whole dump",
			"error", err,
		)
	}

	return imp.aggregateStreamed(ctx, st)
}

// aggregateStreamed is the fallback stage-1 path: read the tar end to
// end and parse every parquet member in full.
func (imp *dumpImporter) aggregateStreamed(ctx context.Context, st *countsState) error {
	stream := imp.openDumpStream(ctx, st.SparkURL, st.Offset)

	defer func() { _ = stream.Close() }()

	// A live reporter samples the stream position on a timer; without
	// it the stage would sit unchanged for minutes at a time between
	// parquet members, which reads as "hung" rather than "downloading".
	var awaitingWorkers atomic.Bool

	stopReporter := imp.startCountsReporter(ctx, stream, &awaitingWorkers)
	defer stopReporter()

	progress := &countsLogger{imp: imp, stream: stream, started: time.Now()}

	buffered := bufio.NewReaderSize(stream, 1<<20)
	tr := tar.NewReader(buffered)

	// consumedOffset is the absolute stream position of everything the
	// tar reader has consumed: bytes delivered by HTTP minus bytes
	// still sitting in the bufio buffer.
	consumedOffset := func() int64 {
		return stream.Pos() - int64(buffered.Buffered())
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
	applier := newCountsApplier(imp, st, progress)

	go func() {
		defer close(applierDone)

		for res := range results {
			applier.apply(res, nil)
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

		awaitingWorkers.Store(true)

		select {
		case jobs <- countParseJob{idx: memberIdx, endOffset: endOffset, buf: buf}:
			awaitingWorkers.Store(false)
		case <-ctx.Done():
			awaitingWorkers.Store(false)

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
		readErr = applier.err
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
		"gb", fmt.Sprintf("%.1f", float64(st.Offset)/(1<<30)),
		"entities", len(st.counts),
		"elapsed", time.Since(progress.started).Truncate(time.Second).String(),
	)

	imp.logJob(fmt.Sprintf(
		"Listen counts complete — %s of listens read, %s entities ranked",
		formatGB(st.Offset), formatCount(len(st.counts)),
	))

	return nil
}

// tarPadding returns the number of zero bytes following a tar entry of
// the given size (entries are padded to 512-byte blocks).
func tarPadding(size int64) int64 {
	const block = 512

	return (block - size%block) % block
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

// ---------------------------------------------------------------------------
// progress reporting
// ---------------------------------------------------------------------------

// startCountsReporter runs a goroutine that samples the listens stream
// position every few seconds and publishes it as the stage's UI
// progress.  The returned function stops the reporter and waits for it
// to exit, so no stale "running" update can land after the stage is
// marked complete.
func (imp *dumpImporter) startCountsReporter(
	ctx context.Context, stream dumpStream, backlog *atomic.Bool,
) func() {
	stop := make(chan struct{})
	exited := make(chan struct{})

	rep := &countsReporter{
		imp:        imp,
		stream:     stream,
		backlog:    backlog,
		lastSample: time.Now(),
		lastOffset: stream.Fetched(),
		lastMoved:  time.Now(),
	}

	go func() {
		defer close(exited)

		ticker := time.NewTicker(countsUIRefreshInterval)
		defer ticker.Stop()

		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				rep.tick(now)
			}
		}
	}()

	return func() {
		close(stop)
		<-exited
	}
}

// countsReporter turns stream position samples into a percentage and a
// human-readable throughput line.  Only its own goroutine touches it.
type countsReporter struct {
	imp    *dumpImporter
	stream dumpStream

	// backlog is set while the reader is blocked handing a member to
	// the parquet workers.  The stream stops moving then too, and
	// calling that a network stall would be wrong.
	backlog *atomic.Bool

	lastSample time.Time
	lastOffset int64
	lastMoved  time.Time
	rate       float64 // EWMA bytes/sec
}

func (rep *countsReporter) tick(now time.Time) {
	// Track the downloader, not the consumer: parallel lanes buffer a
	// chunk at a time, so delivery stands still for a minute at the
	// start of a stream while the network is in fact saturated.  Watching
	// Pos here would report that as a stall.
	offset := rep.stream.Fetched()
	size := rep.stream.Total()

	if elapsed := now.Sub(rep.lastSample).Seconds(); elapsed > 0 {
		sample := float64(offset-rep.lastOffset) / elapsed
		if rep.rate == 0 {
			rep.rate = sample
		} else {
			rep.rate = countsRateSmoothing*sample + (1-countsRateSmoothing)*rep.rate
		}
	}

	if offset != rep.lastOffset {
		rep.lastMoved = now
	}

	rep.lastSample = now
	rep.lastOffset = offset

	// A stream that has stopped moving is either reconnecting or held
	// up by the parsers; either way, reporting a rate that is really
	// just an average of nothing would be misleading.
	var detail string

	switch {
	case now.Sub(rep.lastMoved) <= countsStallAfter:
		rep.imp.countsRate.Store(uint64(max(rep.rate, 0)))

		detail = formatStreamProgress(offset, size, rep.rate)
	case rep.backlog != nil && rep.backlog.Load():
		rep.imp.countsRate.Store(0)

		detail = formatGB(offset) + " downloaded · parsing, download paused"
	default:
		rep.imp.countsRate.Store(0)

		detail = formatGB(offset) + " downloaded · stalled, retrying…"
	}

	rep.imp.setStageDetail(dumpStageCounts, streamPercent(offset, size), 100, detail)
}

// countsLogger writes stage-1 progress to the app log and the jobs
// panel on independent time-based schedules.  Per-member lines would be
// too sparse to reassure and too noisy to read; checkpoints, which are
// the points a crash would resume from, always log to both.
type countsLogger struct {
	imp    *dumpImporter
	stream dumpStream

	started    time.Time
	lastLog    time.Time
	lastJobLog time.Time
}

// member reports an applied parquet member, logging only if enough time
// has passed since the last line.
func (l *countsLogger) member(members int, offset int64, entities int) {
	now := time.Now()

	if now.Sub(l.lastLog) >= countsLogInterval {
		l.lastLog = now

		l.logApp("dump import: listen counts progress", members, offset, entities)
	}

	if now.Sub(l.lastJobLog) >= countsJobLogInterval {
		l.lastJobLog = now

		l.logJob("Listen counts", members, offset, entities)
	}
}

// checkpoint reports a counts.bin flush, which always logs — it is the
// point an interrupted import would resume from.
func (l *countsLogger) checkpoint(members int, offset int64, entities int) {
	now := time.Now()

	l.lastLog = now
	l.lastJobLog = now

	l.logApp("dump import: listen counts checkpoint", members, offset, entities)
	l.logJob("Listen counts checkpointed", members, offset, entities)
}

func (l *countsLogger) logApp(msg string, members int, offset int64, entities int) {
	size := l.stream.Total()

	l.imp.logger.Info(msg,
		"members", members,
		"gb", fmt.Sprintf("%.1f", float64(offset)/(1<<30)),
		"pct", streamPercent(offset, size),
		"rate", formatRate(l.imp.streamRate()),
		"eta", formatETA(offset, size, l.imp.streamRate()),
		"entities", entities,
	)
}

func (l *countsLogger) logJob(prefix string, members int, offset int64, entities int) {
	l.imp.logJob(fmt.Sprintf("%s: %s · %s members · %s entities",
		prefix,
		formatStreamProgress(offset, l.stream.Total(), l.imp.streamRate()),
		formatCount(members),
		formatCount(entities),
	))
}

// streamRate returns the listens stream throughput most recently
// measured by the reporter, in bytes/sec.
func (imp *dumpImporter) streamRate() float64 {
	return float64(imp.countsRate.Load())
}

// streamPercent is the whole-percent position in a stream of known
// size; 0 when the size is not yet known.
func streamPercent(offset, size int64) int {
	if size <= 0 {
		return 0
	}

	return int(float64(offset) / float64(size) * 100)
}

// formatStreamProgress renders "42.3 / 205.1 GB (20%) · 18 MB/s ·
// ~3h20m left", degrading gracefully when the size or rate is unknown.
func formatStreamProgress(offset, size int64, rate float64) string {
	parts := make([]string, 0, 3)

	if size > 0 {
		parts = append(parts, fmt.Sprintf("%s / %s (%d%%)",
			formatGB(offset), formatGB(size), streamPercent(offset, size)))
	} else {
		parts = append(parts, formatGB(offset)+" downloaded")
	}

	if rate > 0 {
		parts = append(parts, formatRate(rate))
	}

	if eta := formatETA(offset, size, rate); eta != "" {
		parts = append(parts, "~"+eta+" left")
	}

	return strings.Join(parts, " · ")
}

func formatRate(bytesPerSec float64) string {
	if bytesPerSec <= 0 {
		return "—"
	}

	return fmt.Sprintf("%.1f MB/s", bytesPerSec/(1<<20))
}

// formatETA estimates remaining time at the current rate.  Returns ""
// when the total size or the rate is unknown.
func formatETA(offset, size int64, rate float64) string {
	if size <= 0 || rate <= 0 || offset >= size {
		return ""
	}

	remaining := time.Duration(float64(size-offset)/rate) * time.Second

	switch {
	case remaining < time.Minute:
		return "<1m"
	case remaining < time.Hour:
		return fmt.Sprintf("%dm", int(remaining.Minutes()))
	default:
		return fmt.Sprintf("%dh%02dm", int(remaining.Hours()), int(remaining.Minutes())%60)
	}
}
