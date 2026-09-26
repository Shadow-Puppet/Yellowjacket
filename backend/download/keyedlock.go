package download

import (
	"context"
	"sync"
)

// keyedLock is a set of mutexes created on demand, one per key, that
// honour a context while waiting.  An entry lives only while someone
// holds or waits on it, so a key per Soulseek peer or per folder name
// does not accumulate for the life of the process.
type keyedLock[K comparable] struct {
	mu   sync.Mutex
	held map[K]*keyedEntry
}

type keyedEntry struct {
	ch chan struct{}

	// refs counts holders and waiters; the entry is dropped at zero.
	refs int
}

// acquire blocks until k is free or ctx ends, and returns the function
// that frees it.
func (l *keyedLock[K]) acquire(ctx context.Context, k K) (func(), error) {
	l.mu.Lock()

	if l.held == nil {
		l.held = map[K]*keyedEntry{}
	}

	e, ok := l.held[k]
	if !ok {
		e = &keyedEntry{ch: make(chan struct{}, 1)}
		l.held[k] = e
	}

	e.refs++

	l.mu.Unlock()

	select {
	case e.ch <- struct{}{}:
	case <-ctx.Done():
		l.drop(k, e)

		return nil, ctx.Err()
	}

	var once sync.Once

	return func() {
		once.Do(func() {
			<-e.ch
			l.drop(k, e)
		})
	}, nil
}

func (l *keyedLock[K]) drop(k K, e *keyedEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()

	e.refs--
	if e.refs == 0 {
		delete(l.held, k)
	}
}

// size reports how many keys are held or awaited, for tests.
func (l *keyedLock[K]) size() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	return len(l.held)
}
