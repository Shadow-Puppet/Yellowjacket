//go:build indexbuild

package explore

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Stage 1 driven by column projection.  The tar is never streamed: a
// walker reads member headers by Range request, and workers download and
// parse each parquet member's projected columns independently.  Results
// are applied in member order, so the checkpoint stays a contiguous
// prefix of the archive exactly as it is on the streamed path.

const (
	// projectMemberWorkers is how many members are fetched and parsed
	// concurrently.  Each holds one member-sized buffer, and each issues
	// projectFetchLanes concurrent Range requests, so the product is the
	// in-flight request count the dump server sees.
	projectMemberWorkers = 3

	// minParquetMemberSize is the smallest member that can hold a
	// parquet footer ("PAR1" + length + "PAR1").  Anything shorter is
	// not a parquet file whatever its name says.
	minParquetMemberSize = 12
)

// indexedMember is a parquet member with its position in the aggregation
// order, which is what the applier reassembles results by.
type indexedMember struct {
	idx int
	m   tarMember
}

// aggregateProjected runs stage 1 by downloading only the projected
// columns of each parquet member.  Returns errProjectionUnsupported
// (wrapped) when the dump's layout defeats projection, so the caller can
// fall back before any counts are applied.
func (imp *dumpImporter) aggregateProjected(
	ctx context.Context, st *countsState, total int64,
) error {
	imp.logger.Info("dump import: streaming listen counts with column projection",
		"columns", strings.Join(projectedColumns, ","),
		"workers", projectMemberWorkers,
		"lanesPerWorker", projectFetchLanes,
		"resumeOffset", st.Offset,
	)

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	fetcher := &rangeFetcher{ctx: runCtx, client: imp.httpClient, url: st.SparkURL}

	prog := &projectedProgress{total: total}
	prog.position.Store(st.Offset)

	stopReporter := imp.startCountsReporter(runCtx, prog, nil)
	defer stopReporter()

	progress := &countsLogger{imp: imp, stream: prog, started: time.Now()}

	// The walker runs ahead of the workers: each member costs it one
	// small request, so given a leash it never becomes the bottleneck.
	rawMembers := make(chan tarMember, walkAheadMembers)
	walkErr := make(chan error, 1)

	go func() {
		walkErr <- walkTarMembers(runCtx, fetcher, st.Offset, total, rawMembers)
	}()

	// Number the parquet members the aggregation actually consumes,
	// continuing from the checkpoint so indices stay stable on resume.
	work := make(chan indexedMember)

	go func() {
		defer close(work)

		idx := st.MemberIdx

		for m := range rawMembers {
			if !isProjectableMember(m) {
				continue
			}

			select {
			case work <- indexedMember{idx: idx, m: m}:
				idx++
			case <-runCtx.Done():
				return
			}
		}
	}()

	results := make(chan countParseResult, projectMemberWorkers)

	var workerWG sync.WaitGroup

	for range projectMemberWorkers {
		workerWG.Add(1)

		go func() {
			defer workerWG.Done()

			buf := []byte(nil)

			for job := range work {
				if cap(buf) < int(job.m.size) {
					buf = make([]byte, job.m.size)
				}

				buf = buf[:job.m.size]

				fetched, err := fetchProjectedMember(runCtx, fetcher, job.m, buf)
				if err == nil {
					prog.addFetched(fetched)
				}

				var deltas map[mbidKey]uint32

				if err == nil {
					deltas, err = parseListenParquet(buf)
				}

				results <- countParseResult{
					idx:       job.idx,
					endOffset: job.m.nextHeaderOffset(),
					deltas:    deltas,
					err:       err,
				}
			}
		}()
	}

	applier := newCountsApplier(imp, st, progress)
	applierDone := make(chan struct{})

	go func() {
		defer close(applierDone)

		for res := range results {
			applier.apply(res, prog)
		}
	}()

	workerWG.Wait()
	close(results)
	<-applierDone

	// Drain the walker so its error (if any) is observed and its
	// goroutine cannot outlive this call.
	cancel()

	for range rawMembers { //nolint:revive // draining
	}

	err := applier.err
	if err == nil {
		err = walkFailure(ctx, <-walkErr)
	} else {
		<-walkErr
	}

	if err != nil {
		// Best-effort checkpoint so even a cancelled run resumes where
		// it left off.
		_ = imp.writeCountsFile(st)

		return err
	}

	st.Done = true

	if err := imp.writeCountsFile(st); err != nil {
		return err
	}

	imp.logger.Info("dump import: listen counts complete",
		"members", st.MemberIdx,
		"gb", fmt.Sprintf("%.1f", float64(st.Offset)/(1<<30)),
		"downloadedGB", fmt.Sprintf("%.1f", float64(prog.Downloaded())/(1<<30)),
		"entities", len(st.counts),
		"elapsed", time.Since(progress.started).Truncate(time.Second).String(),
	)

	imp.logJob(fmt.Sprintf(
		"Listen counts complete — %s of listens read (%s downloaded), %s entities ranked",
		formatGB(st.Offset), formatGB(prog.Downloaded()), formatCount(len(st.counts)),
	))

	return nil
}

// walkFailure reports a walker error worth surfacing.  A walk cancelled
// because the workers finished first is not a failure.
func walkFailure(ctx context.Context, err error) error {
	if err == nil || (errors.Is(err, context.Canceled) && ctx.Err() == nil) {
		return nil
	}

	return err
}

// isProjectableMember reports whether a tar member is a parquet file the
// aggregator should consume.  This must match the streamed path's member
// selection exactly, or the two paths would produce different counts.
func isProjectableMember(m tarMember) bool {
	if m.typeflag != tar.TypeReg && m.typeflag != 0 {
		return false
	}

	return strings.HasSuffix(m.name, ".parquet") && m.size >= minParquetMemberSize
}

// ---------------------------------------------------------------------------
// Applier
// ---------------------------------------------------------------------------

// countsApplier merges per-member deltas into the counts map in member
// order, checkpointing every countsFlushEveryMembers members.  It owns
// st.counts, st.Offset and st.MemberIdx for the duration of a stage.
type countsApplier struct {
	imp      *dumpImporter
	st       *countsState
	progress *countsLogger

	pending     map[int]countParseResult
	next        int
	lastFlushed int

	err error
}

func newCountsApplier(
	imp *dumpImporter, st *countsState, progress *countsLogger,
) *countsApplier {
	return &countsApplier{
		imp:         imp,
		st:          st,
		progress:    progress,
		pending:     make(map[int]countParseResult),
		next:        st.MemberIdx,
		lastFlushed: st.MemberIdx,
	}
}

// apply buffers a result and folds in every member that is now
// contiguous with the checkpoint.  pos, when non-nil, is advanced to the
// archive offset the checkpoint has reached.
func (a *countsApplier) apply(res countParseResult, pos *projectedProgress) {
	if a.err != nil {
		return
	}

	a.pending[res.idx] = res

	for {
		r, ok := a.pending[a.next]
		if !ok {
			return
		}

		delete(a.pending, a.next)

		if r.err != nil {
			a.err = r.err

			return
		}

		for k, v := range r.deltas {
			a.st.counts[k] += v
		}

		a.next++
		a.st.MemberIdx = a.next
		a.st.Offset = r.endOffset

		if pos != nil {
			pos.position.Store(r.endOffset)
		}

		if a.next-a.lastFlushed >= countsFlushEveryMembers {
			if err := a.imp.writeCountsFile(a.st); err != nil {
				a.err = err

				return
			}

			a.lastFlushed = a.next

			a.progress.checkpoint(a.next, r.endOffset, len(a.st.counts))

			if err := a.imp.checkDiskHeadroom(); err != nil {
				a.err = err

				return
			}
		} else {
			a.progress.member(a.next, r.endOffset, len(a.st.counts))
		}
	}
}

// ---------------------------------------------------------------------------
// Progress
// ---------------------------------------------------------------------------

// projectedProgress presents the projected import to the stage-1
// reporter through the same interface a sequential stream uses.
//
// Position — not bytes downloaded — is what is reported as stream
// progress: with projection those diverge (under half the archive is
// downloaded), and it is position that gives a percentage and an ETA the
// user can act on.  Bytes actually downloaded are tracked separately and
// logged at the end.
type projectedProgress struct {
	total int64

	position   atomic.Int64
	downloaded atomic.Int64
}

func (p *projectedProgress) Read([]byte) (int, error) { return 0, errProjectionUnsupported }
func (p *projectedProgress) Close() error             { return nil }
func (p *projectedProgress) Pos() int64               { return p.position.Load() }
func (p *projectedProgress) Fetched() int64           { return p.position.Load() }
func (p *projectedProgress) Total() int64             { return p.total }

func (p *projectedProgress) addFetched(n int64) { p.downloaded.Add(n) }

// Downloaded is how many bytes actually crossed the wire.
func (p *projectedProgress) Downloaded() int64 { return p.downloaded.Load() }
