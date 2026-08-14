package explore

import (
	"context"

	"yellowjacket/backend/jobs"
)

// discogBackfillJobID is the stable registry ID for the owned-artist
// discography backfill.  Only one runs at a time (the launch and
// post-scan triggers both go through the same bounded, resumable call),
// so the ID is a constant and a re-run reuses the handle and its log.
const discogBackfillJobID = "explore:discography-backfill"

// rgMBIDBackfillJobID is the same for the release-group MBID
// resolution pass.
const rgMBIDBackfillJobID = "explore:release-group-mbid-backfill"

// lyricsBackfillJobID is the same for the LRCLIB lyrics pass.
const lyricsBackfillJobID = "explore:lyrics-backfill"

// backfillJob is the registry side of a background catalog backfill:
// a handle, and the cancel func the registry's Cancel control trips.
//
// Every method is nil-safe, because a nil *backfillJob is the ordinary
// state in tests and in any build with no registry wired — the backfill
// itself must not care whether anyone is watching.
type backfillJob struct {
	h      *jobs.Handle
	cancel context.CancelFunc
}

// startBackfillJob registers a cancellable job and returns it alongside
// a context that the job's Cancel control cancels.  A nil registry (or
// a zero total) yields a nil job and the original context, so callers
// need no branch of their own.
//
// It is deliberately called *after* the work has been counted: a run
// with nothing to do must not put a job in the indicator, and this
// backfill is a no-op on every launch once the library is covered.
func startBackfillJob(
	ctx context.Context,
	reg *jobs.Registry,
	id, title, subtitle string,
	total int,
) (*backfillJob, context.Context) {
	if reg == nil || total <= 0 {
		return nil, ctx
	}

	jobCtx, cancel := context.WithCancel(ctx)

	b := &backfillJob{cancel: cancel}
	b.h = reg.Start(jobs.Spec{
		ID:       id,
		Kind:     jobs.KindCatalogEnrich,
		Title:    title,
		Subtitle: subtitle,
		Total:    int64(total),
		State:    jobs.StateRunning,
		Caps: jobs.Caps{
			// Not pausable: the run is bounded and resumable by
			// construction — each artist is marked as it completes, so
			// cancelling and re-running is exactly what a pause would
			// achieve, without a second checkpoint to keep honest.
			Cancellable: true,
		},
		Controls: jobs.Controls{Cancel: cancel},
	})

	return b, jobCtx
}

// progress reports how far the run has got.
func (b *backfillJob) progress(current, total int) {
	if b == nil || b.h == nil {
		return
	}

	b.h.SetProgress(int64(current), int64(total))
}

// logf appends a line to the job's log.
func (b *backfillJob) logf(level jobs.Level, message string) {
	if b == nil || b.h == nil {
		return
	}

	b.h.Logf(level, message)
}

// finish closes the job out, reporting cancellation when that is what
// stopped it, and always releases the context.
func (b *backfillJob) finish(ctx context.Context) {
	if b == nil {
		return
	}

	defer b.cancel()

	if b.h == nil {
		return
	}

	if ctx.Err() != nil {
		b.h.Cancelled()

		return
	}

	b.h.Complete()
}
