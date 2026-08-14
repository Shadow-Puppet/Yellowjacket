package queue

// Every queue write goes through SQLite's single writer connection
// (database.DB sets MaxOpenConns(1)), which a background pass can hold
// for seconds at a time — the discography backfill's per-row FTS
// maintenance is the measured example.  Doing these writes inline meant
// SetQueue held q.mu across that wait, so the queue panel, the play
// button and every other bound method blocked behind a durability write
// nobody was waiting for: the track changed, the transport sat at
// paused, and the queue never arrived.
//
// So a write is *submitted*, not performed.  Jobs run in submission
// order on one goroutine, each carrying its own snapshot, which is what
// keeps "clear and rewrite the queue" and "insert three tracks at 4"
// meaning the same thing they meant at the moment they were called.  A
// job must therefore never touch q's fields — it has no lock and the
// state has moved on.
//
// SaveState is the one caller that still waits: shutdown is exactly the
// case where the write has to have happened before we return.

// persistQueueDepth is how many pending writes are buffered before a
// submission runs inline.  These are user-driven mutations, so the
// buffer exists for a stalled writer rather than for throughput.
const persistQueueDepth = 256

// writer returns the persistence goroutine's channel, starting it on
// first use.  Lazy because a Queue is constructed in tests that never
// write, and an idle goroutine per instance is a cost with no reader.
func (q *Queue) writer() chan func() {
	q.persistOnce.Do(func() {
		q.persistCh = make(chan func(), persistQueueDepth)

		go func() {
			for job := range q.persistCh {
				job()
			}
		}()
	})

	return q.persistCh
}

// submitWrite queues a database write to run off the caller's lock.
// The caller may hold q.mu; the job may not touch q.
func (q *Queue) submitWrite(job func()) {
	select {
	case q.writer() <- job:
	default:
		// The buffer is full, which means the writer connection has
		// been held for a long time.  Running inline is the old
		// behaviour and blocks the caller — but a dropped write is a
		// queue that restores wrong, which is worse.
		q.logger.Warn("Queue write buffer full, persisting inline")
		job()
	}
}

// flushWrites blocks until every write submitted so far has run.
func (q *Queue) flushWrites() {
	done := make(chan struct{})

	q.writer() <- func() { close(done) }

	<-done
}
