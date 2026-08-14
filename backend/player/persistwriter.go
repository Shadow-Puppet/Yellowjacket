package player

// The player saves its state — volume, mute, the loaded track and its
// position — from inside every path that changes any of them, and every
// one of those paths holds p.mu.  The write goes through SQLite's
// single writer connection (database.DB sets MaxOpenConns(1)), which a
// background pass can hold for seconds at a time.
//
// Inline, that made LoadFile block on an unrelated backfill while
// holding p.mu, so the queue's own SetQueue — which calls it under q.mu
// — froze the transport and the queue panel with it: the track changed,
// the state stayed paused, and Play() had not been reached yet.
//
// So the write is submitted and runs in submission order on one
// goroutine, off the lock.  A job must not touch p; it gets a snapshot.
// SaveState (shutdown) is the one caller that waits.

// persistQueueDepth is how many pending writes are buffered before a
// submission runs inline.  Player state is written a few times per
// track, so the buffer is for a stalled writer, not for throughput.
const persistQueueDepth = 64

// writer returns the persistence goroutine's channel, starting it on
// first use.
func (p *Player) writer() chan func() {
	p.persistOnce.Do(func() {
		p.persistCh = make(chan func(), persistQueueDepth)

		go func() {
			for job := range p.persistCh {
				job()
			}
		}()
	})

	return p.persistCh
}

// submitWrite queues a database write to run off the caller's lock.
// The caller may hold p.mu; the job may not touch p's state.
func (p *Player) submitWrite(job func()) {
	select {
	case p.writer() <- job:
	default:
		// The buffer is full, so the writer connection has been held
		// for a long time.  Running inline is the old behaviour and
		// blocks the caller, but losing the state is worse: it is what
		// the app reopens to.
		p.logger.Warn("Player write buffer full, persisting inline")
		job()
	}
}

// flushWrites blocks until every write submitted so far has run.
func (p *Player) flushWrites() {
	done := make(chan struct{})

	p.writer() <- func() { close(done) }

	<-done
}
