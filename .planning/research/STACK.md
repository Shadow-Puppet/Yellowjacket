# Stack Research: Consolidation Patterns & Tools

**Domain:** Desktop music player consolidation — correctness, performance, testing, code quality
**Researched:** 2026-02-27
**Confidence:** HIGH (core Go/SQLite patterns) / MEDIUM (beep-specific, Lit optimization)

This document covers tools, patterns, and specific techniques for improving the quality of the existing YellowJacket codebase. It is organized by the five research questions, prioritized by impact.

---

## 1. Go Concurrency Safety — Priority: CRITICAL

**Confidence:** HIGH — based on Go standard library docs, race detector behavior, and codebase analysis.

### The Core Problem

YellowJacket has three documented data races, all following the same anti-pattern: a `SetContext()` method writes a struct field without holding the struct's mutex, while other methods read that field under the mutex. This is a textbook data race even if "it works in practice."

### Pattern: Fix SetContext Races

The `Queue.SetContext()`, `Library.SetContext()`, and `playlist.Service.SetContext()` all share the same bug. The fix is the same for all three:

```go
// BEFORE (race):
func (q *Queue) SetContext(ctx context.Context) {
    q.ctx = ctx  // ← no lock, but q.ctx is read under q.mu elsewhere
}

// AFTER (correct):
func (q *Queue) SetContext(ctx context.Context) {
    q.mu.Lock()
    defer q.mu.Unlock()
    q.ctx = ctx
}
```

**Why this matters:** The Go race detector (`-race` flag) will flag this in tests. Since `make test` already runs with `-race`, any test that exercises `SetContext` alongside event emission will fail. Fixing these races unblocks writing tests for queue, library, and playlist packages.

**Why not use `sync/atomic`:** `context.Context` is an interface (two words: type pointer + data pointer). `sync/atomic` only works on single-word types. Use the existing mutex.

### Pattern: Player Double-Lock Fix

The player's `SetContext` acquires and releases the mutex twice in succession:

```go
// BEFORE (window between locks):
func (p *Player) SetContext(ctx context.Context) {
    p.mu.Lock()
    p.ctx = ctx
    p.mu.Unlock()
    
    p.mu.Lock()
    p.restoreStateLocked()
    p.mu.Unlock()
}

// AFTER (single acquisition):
func (p *Player) SetContext(ctx context.Context) {
    p.mu.Lock()
    defer p.mu.Unlock()
    p.ctx = ctx
    p.restoreStateLocked()
}
```

**Why:** Between the two lock acquisitions, another goroutine can modify state. The combined lock makes the set-context-and-restore atomic.

### Pattern: Lock Ordering Documentation

The player already documents its lock ordering rule: "acquire `p.mu` BEFORE `speaker.Lock()`." This is correct and critical. The `go p.onPlaybackFinished()` dispatch from the beep callback is essential — removing the goroutine dispatch would deadlock because the beep callback holds `speaker.Lock()` and `onPlaybackFinished` acquires `p.mu`.

**Recommendation:** Add a `// Lock ordering:` comment block to the Queue and Library structs as well, even though they only have one lock each. Document what operations must NOT hold the lock (event emission, player callbacks).

```go
// Queue manages an ordered list of tracks for playback.
//
// Concurrency: q.mu protects all mutable fields. Event emission
// (emitQueueChanged, etc.) is called WITH q.mu held because the
// Wails EventsEmit is non-blocking. The playbackFinishedHandler
// (auto-advance) re-enters the queue via AddTrack/Next, so it
// must NOT be called while holding q.mu.
type Queue struct {
    mu sync.Mutex
    // ...
}
```

### Testing Pattern: Race Detector as Test Oracle

```bash
# Already in Makefile — verify this is the exact command:
make test  # → go test -tags webkit2_41 -race -count=1 -timeout 120s ./...
```

The race detector is the most valuable tool here. Every new test implicitly checks for races when run with `-race`. No additional tooling needed — just write tests that exercise concurrent paths:

```go
func TestQueueSetContextRace(t *testing.T) {
    q := NewQueue(slog.Default(), testDB)
    
    // Simulate Wails calling SetContext while queue operations run.
    var wg sync.WaitGroup
    wg.Add(2)
    go func() {
        defer wg.Done()
        q.SetContext(context.Background())
    }()
    go func() {
        defer wg.Done()
        q.GetState() // reads under lock
    }()
    wg.Wait()
}
```

### What NOT to Do

| Anti-Pattern | Why It's Wrong | Instead |
|---|---|---|
| `sync.RWMutex` for Queue/Player | These structs have frequent writes AND reads from multiple goroutines on the same timeline. RWMutex only helps when reads vastly outnumber writes and are long-running. Desktop event-driven access patterns don't benefit. | Keep `sync.Mutex`. Simpler, fewer bugs. |
| Channel-based state management | Replacing mutexes with channels for Queue state would require rewriting all methods. The current mutex pattern is correct, just under-applied. | Fix the races by adding lock acquisitions to SetContext methods. |
| `sync.Map` for entityCache | `sync.Map` is optimized for concurrent reads from many goroutines. The entityCache is accessed from a single DB-writer goroutine. It would add overhead with zero benefit. | Keep plain maps (already correct). |
| Package-level mutex for startupErr | A package-level mutex is worse than the disease. | Move `startupErr` to a field on `YellowJacketApp` struct. |

---

## 2. SQLite WAL Mode Optimization — Priority: HIGH

**Confidence:** HIGH — based on SQLite official docs (sqlite.org/wal.html), modernc.org/sqlite driver docs, and codebase analysis.

### Current Setup Analysis

The database initialization is solid:
- WAL mode via `?_journal_mode=WAL` in DSN (**correct**)
- `_busy_timeout=5000` — 5 second busy wait (**correct**, prevents SQLITE_BUSY in most cases)
- `SetMaxOpenConns(1)` — single writer (**correct**, required for pure-Go driver)
- `PRAGMA foreign_keys = ON` (**correct**)

### Missing PRAGMAs to Add

```go
// Add after foreign_keys pragma in NewDB():
pragmas := []string{
    "PRAGMA foreign_keys = ON",
    "PRAGMA synchronous = NORMAL",       // WAL-safe, much faster
    "PRAGMA cache_size = -8000",          // 8MB page cache (default is -2000 = 2MB)
    "PRAGMA mmap_size = 67108864",        // 64MB memory-mapped I/O
    "PRAGMA temp_store = MEMORY",         // Temp tables in memory
    "PRAGMA optimize",                    // Run at connection open
}
```

**Why `synchronous = NORMAL`:** In WAL mode, NORMAL provides durability against process crashes (only power loss can cause data loss of the last transaction). FULL is the default and fsyncs the WAL on every commit, which is unnecessary for a desktop music player where the data can be rescanned from disk.

**Why `cache_size = -8000`:** The negative value means 8000 KiB (8MB). The default 2MB is fine for small databases but YellowJacket libraries can have 50k+ tracks. Larger cache reduces disk I/O for repeated queries (all-tracks, search, queue operations).

**Why `mmap_size`:** Memory-mapped I/O lets SQLite read pages directly from the OS page cache. 64MB covers most music library databases entirely. With modernc.org/sqlite (pure Go), mmap is handled by the underlying C translation and works on Linux/macOS/Windows.

**Why `PRAGMA optimize` at open:** Runs `ANALYZE` on tables where the optimizer thinks statistics are stale. Zero cost if stats are fresh.

### Add `PRAGMA optimize` at Shutdown

```go
// In app.go OnShutdown:
func (a *YellowJacketApp) OnShutdown(ctx context.Context) {
    // ... existing cleanup ...
    _, _ = a.db.ExecContext("PRAGMA optimize")  // Update query planner stats
}
```

SQLite docs recommend running `PRAGMA optimize` at close to ensure statistics are written for the next session.

### Query Consolidation: FTS5 JOIN Deduplication

The codebase has 5 copies of the same FTS5 JOIN pattern. Extract it:

```go
// backend/database/search.go

// ftsMetadataJoin is the common JOIN clause for resolving audio file
// metadata through the recording → artist_credit → release_group chain.
// Use with "FROM search_index si" or "FROM audio_files af" as the base.
const ftsMetadataJoin = `
    JOIN audio_files af ON af.id = si.rowid
    LEFT JOIN recordings r ON af.recording_id = r.id
    LEFT JOIN artist_credit ac ON r.artist_credit_id = ac.id
    LEFT JOIN (
        SELECT recording_id,
            MIN(release_group_id) AS release_group_id
        FROM release_group_recordings
        GROUP BY recording_id
    ) rgr ON r.id = rgr.recording_id
    LEFT JOIN release_groups rg ON rgr.release_group_id = rg.id
`
```

Then each search function references the constant instead of duplicating the SQL. This ensures schema changes only need one update.

**Alternative:** Move these to sqlc queries where possible. The `SearchFTS` and `SearchFTSByFilename` functions can't easily use sqlc because the FTS5 `MATCH` syntax isn't well-supported by sqlc's parser. Keep them as hand-crafted SQL with the shared constant. Document why with a comment.

### Queue Persistence: Incremental Updates

The current `persistTracks()` does `DELETE ALL + INSERT ALL` on every mutation. For a queue with 1000 tracks, every add/remove/move rewrites all 1000 rows.

**Pattern: Differential persistence for single-track operations:**

```go
// For AddTrack — single INSERT instead of full rewrite:
func (q *Queue) persistAddTrack(track Track) {
    err := q.db.Queries.InsertQueueTrack(q.db.Ctx, sqlcgen.InsertQueueTrackParams{
        AudioFileID: track.AudioFileID,
        Position:    track.Position,
    })
    if err != nil {
        q.logger.Error("Failed to persist added track", "err", err)
    }
}

// For RemoveTrack — single DELETE:
func (q *Queue) persistRemoveTrack(position int64) {
    err := q.db.Queries.DeleteQueueTrackByPosition(q.db.Ctx, position)
    if err != nil {
        q.logger.Error("Failed to persist removed track", "err", err)
    }
}
```

**Keep full rewrite for:** `SetQueue`, `RestoreState`, shuffle reordering — cases where the entire queue changes at once.

**Estimated impact:** Reduces O(n) per-mutation writes to O(1) for the common case (add/remove single track). For a 5000-track queue, this eliminates ~10,000 unnecessary row writes per track operation.

### What NOT to Do

| Anti-Pattern | Why It's Wrong | Instead |
|---|---|---|
| Connection pooling (`SetMaxOpenConns > 1`) | modernc.org/sqlite is a single-writer database. Multiple connections cause SQLITE_BUSY errors. The current `SetMaxOpenConns(1)` is correct. | Keep `SetMaxOpenConns(1)`. |
| `_txlock=immediate` on all transactions | Immediate locking blocks all readers during writes. The default deferred locking only acquires a write lock when needed. For a desktop app with infrequent writes, deferred is fine. | Use immediate locking ONLY for critical write transactions (queue persistence) where you want to fail fast on contention. |
| Switching to `mattn/go-sqlite3` (CGo) | Adds CGo dependency, complicates cross-compilation, and the project constraint explicitly prohibits it. modernc.org/sqlite v1.45+ performance is within 10-20% of CGo for most workloads. | Stay on modernc.org/sqlite. |
| WAL2 mode | WAL2 is experimental in SQLite. Not available through any Go driver. | Stay on standard WAL. |

---

## 3. Lit Web Component Performance — Priority: MEDIUM

**Confidence:** MEDIUM — based on Lit official docs and @lit-labs/virtualizer usage in the codebase.

### Current State

The codebase already uses `@lit-labs/virtualizer` v2.1.1 in all list views (track-list, cover-grid, artists-view, genres-view, queue-panel). The virtualizer handles DOM recycling for large datasets. The main performance concerns are:

1. **Eager full-library fetch on startup** — `libraryStore.eagerFetch()` loads all tracks, albums, artists, genres simultaneously
2. **Large component files** — 1400-2600 lines mixing concerns (though this is a code quality issue, not a performance issue per se)
3. **Rendering cost of metadata-heavy rows** — each track row has 16+ fields

### Pattern: Lazy Loading Per View

Replace `eagerFetch()` with on-demand loading:

```typescript
class LibraryStore {
    // Instead of fetching all four collections at construction:
    constructor() {
        EventsOn(Events.LibraryScanComplete, () => {
            this.invalidate();
        });
        this.loadCoverSize();
        // Remove: this.eagerFetch();
    }
    
    // The existing getTracks/getAlbums already support lazy loading —
    // they check for null and fetch if needed. The only change needed
    // is removing eagerFetch() from the constructor.
}
```

**Why:** The existing `getTracks()`, `getAlbums()`, etc. already have null-check-and-fetch logic. The `eagerFetch()` in the constructor defeats this by loading everything upfront. Removing it means only the active view's data is fetched when first navigated to.

**Risk:** First navigation to each view will have a brief loading delay. Mitigate with loading indicators (the `tracksLoading`/`albumsLoading` flags already exist).

### Pattern: Minimize Re-renders with `guard` Directive

For expensive computed values in templates (like filtered/sorted track lists), use Lit's `guard` directive to avoid recomputation:

```typescript
import { guard } from 'lit/directives/guard.js';

// In render():
${guard([this.tracks, this.sortColumn, this.sortDirection], () => 
    this.sortedTracks()
)}
```

**When to use:** For any computed property that depends on reactive properties but is expensive to compute (sorting 50k tracks, filtering, etc.).

### Pattern: keyed Rendering for Virtualizer Lists

Ensure virtualizer items have stable keys so DOM nodes are reused correctly when the list changes:

```typescript
// The virtualizer uses index-based identity by default.
// For track lists that can be reordered (queue, playlists),
// provide a keyFunction:
<lit-virtualizer
    .items=${this.tracks}
    .keyFunction=${(track: Track) => track.filePath}
    .renderItem=${(track: Track) => html`...`}
></lit-virtualizer>
```

**Why:** Without stable keys, reordering a list causes the virtualizer to re-render every visible row. With keys, it reuses existing DOM nodes for rows that moved position.

### What NOT to Do

| Anti-Pattern | Why It's Wrong | Instead |
|---|---|---|
| Moving to React/Preact | The project uses Lit Web Components with Wails' WebView. Switching frameworks is explicitly out of scope and would require rewriting all 20+ components. | Stay on Lit 3.x. |
| Pre-rendering / SSR | Desktop app. No server. No need. | N/A |
| Replacing `@lit-labs/virtualizer` with a custom solution | The virtualizer is battle-tested and integrates with Lit's update lifecycle. A custom solution would need to handle the same edge cases (resize, scroll restoration, dynamic heights). | Keep `@lit-labs/virtualizer`. File bugs if issues are found. |
| `requestAnimationFrame` batching for store updates | Lit already batches updates at microtask timing. Adding rAF batching would add latency without benefit. | Let Lit handle batching. |

---

## 4. Go Testing Strategies — Priority: HIGH

**Confidence:** HIGH — based on Go standard library patterns and codebase-specific analysis.

### Strategy: In-Memory SQLite for Database Tests

modernc.org/sqlite supports in-memory databases. Use them for fast, isolated tests:

```go
// backend/database/testhelper_test.go (shared across test files in the package)

func newTestDB(t *testing.T) *database.DB {
    t.Helper()
    // Use ":memory:" with shared cache so the connection sees the same DB.
    // The query string params mirror production config.
    db, err := database.NewTestDB(":memory:?_journal_mode=WAL&_busy_timeout=5000")
    if err != nil {
        t.Fatal(err)
    }
    t.Cleanup(func() { db.Close() })
    return db
}
```

**For this to work, add a `NewTestDB` constructor to the database package** that accepts a custom DSN instead of computing one from the user data directory:

```go
// backend/database/database.go

// NewTestDB creates a database connection with a caller-provided DSN.
// Intended for unit tests that use in-memory databases.
func NewTestDB(dsn string) (*DB, error) {
    // Same initialization logic as NewDB but with custom DSN.
    // Runs migrations, sets pragmas, etc.
}
```

**Why in-memory:** Tests run in ~1ms instead of ~50ms. No filesystem cleanup. No conflict between parallel tests. Each test gets a fresh database.

**Important:** SQLite in-memory databases with `SetMaxOpenConns(1)` work correctly — the single connection sees a consistent view. No need for shared cache mode with a single connection.

### Strategy: Extract Pure Functions from Player

The player has testable logic that doesn't need audio hardware:

```go
// Volume math — currently inline in player methods:
func userVolumeToBeep(userVolume int) (volume float64, silent bool) {
    if userVolume <= 0 {
        return 0, true
    }
    // Convert 0-100 linear user volume to beep's logarithmic Volume field.
    // Base is 2, so Volume = log2(userVolume/MaxUserVol * range) 
    // This is the math currently embedded in Set/GetVolume methods.
    return math.Log2(float64(userVolume) / float64(MaxUserVol)), false
}

// State serialization — currently inline in persist/restore:
func serializePlayerState(state State, volume int, filePath string) PlayerStateRow { ... }
func deserializePlayerState(row PlayerStateRow) (State, int, string) { ... }
```

**Why:** These pure functions can be tested exhaustively (edge cases: volume 0, volume 100, max uint64 trackChangeID, empty filepath) without any speaker initialization or Wails context.

### Strategy: Interface-Based Mocking for Queue Tests

The `Queue` depends on `TrackLoader` (player) and `*database.DB`. The `TrackLoader` is already an interface — perfect for testing:

```go
// backend/queue/queue_test.go

type mockPlayer struct {
    loaded    []string
    playing   bool
    position  int
}

func (m *mockPlayer) LoadFile(path string) error {
    m.loaded = append(m.loaded, path)
    return nil
}
func (m *mockPlayer) Play() error            { m.playing = true; return nil }
func (m *mockPlayer) IsPlaying() bool         { return m.playing }
func (m *mockPlayer) CurrentPositionSeconds() (int, error) { return m.position, nil }
func (m *mockPlayer) UnloadTrack()            { m.playing = false }

func TestSetQueuePlaysFirstTrack(t *testing.T) {
    db := newTestDB(t)
    // Seed test tracks into db...
    
    q := queue.NewQueue(slog.Default(), db)
    player := &mockPlayer{}
    q.SetPlayer(player)
    q.SetContext(context.Background())
    
    q.SetQueue([]string{"/music/a.mp3", "/music/b.mp3"}, 0, false)
    
    if len(player.loaded) == 0 {
        t.Fatal("expected player to load a file")
    }
    if player.loaded[0] != "/music/a.mp3" {
        t.Errorf("expected first track, got %s", player.loaded[0])
    }
}
```

### Strategy: Config Round-Trip Testing

```go
func TestConfigRoundTrip(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "config.toml")
    
    original := config.DefaultConfig()
    original.Theme.AccentColor = "#ff0000"
    
    err := config.Save(path, original)
    if err != nil {
        t.Fatal(err)
    }
    
    loaded, err := config.Load(path)
    if err != nil {
        t.Fatal(err)
    }
    
    if loaded.Theme.AccentColor != "#ff0000" {
        t.Errorf("accent color not preserved: got %s", loaded.Theme.AccentColor)
    }
}
```

### Strategy: Event Name Parity Validation

Build-time check that Go and TypeScript event names match:

```go
// backend/events/events_test.go

func TestEventNameParity(t *testing.T) {
    // Read the Go events constants via reflection or by parsing the source.
    // Read frontend/src/events.ts.
    // Compare the sets.
    
    goEvents := extractGoEventNames(t)     // parse events.go
    tsEvents := extractTSEventNames(t)     // parse events.ts
    
    for name := range goEvents {
        if _, ok := tsEvents[name]; !ok {
            t.Errorf("Go event %q not found in TypeScript events.ts", name)
        }
    }
    for name := range tsEvents {
        if _, ok := goEvents[name]; !ok {
            t.Errorf("TypeScript event %q not found in Go events.go", name)
        }
    }
}
```

**Implementation note:** Parse events.go for `const ( ... )` block string values. Parse events.ts for the `Events` object literal values. This is a ~50-line test that prevents silent event name drift forever.

### Test Priority Order

| Package | Why First | Test Count Estimate |
|---|---|---|
| `queue` | Central to playback, most concurrency issues, persistence bugs | ~15-20 tests |
| `database` | FTS5 edge cases, migration correctness, search behavior | ~10-15 tests |
| `config` | Round-trip fidelity, defaults, validation, permissions | ~8-10 tests |
| `player` (pure logic only) | Volume math, state serialization | ~5-8 tests |
| `events` | Parity check | 1 test |
| `library` | Scan logic is complex but depends on filesystem fixtures | ~10 tests (lower priority) |

### What NOT to Do

| Anti-Pattern | Why It's Wrong | Instead |
|---|---|---|
| Test doubles for SQLite (full mock DB layer) | In-memory SQLite IS the test double. It runs the same SQL engine with the same behavior. Mocking at the `*sql.DB` level loses all SQL correctness checking. | Use `:memory:` SQLite databases. |
| `testify` or other assertion libraries | The project uses standard `testing` only. Adding assertion libraries creates style inconsistency and dependency bloat. | Use `t.Errorf`, `t.Fatal`, and `if` checks. |
| Integration tests in CI for player | The player requires an audio output device. CI runners don't have one. The existing skip mechanism (`YELLOWJACKET_INTEGRATION`) is correct. | Extract pure functions from player; leave hardware tests as opt-in integration tests. |
| Coverage targets | The PROJECT.md explicitly says "Tests support refactoring, not standalone goal." Coverage targets incentivize low-value tests. | Test critical paths: queue operations, search, config round-trip, event parity. |

---

## 5. beep/v2 Audio Library Patterns — Priority: MEDIUM

**Confidence:** MEDIUM — based on beep wiki docs, gopxl/beep v2 API, and codebase lock ordering analysis.

### Lock Ordering: The One Rule

beep/v2 has a global speaker lock (`speaker.Lock()/speaker.Unlock()`). The player has its own `sync.Mutex`. The existing documented rule is correct:

> **Always acquire `p.mu` BEFORE `speaker.Lock()`.**

The critical implementation detail: the beep callback (end-of-track) runs with `speaker.Lock()` held. The player dispatches to a goroutine (`go p.onPlaybackFinished()`) so that it can safely acquire `p.mu`. **This goroutine dispatch MUST NOT be removed.** Removing it causes deadlock:

```
Deadlock scenario without goroutine dispatch:
1. beep callback fires (speaker lock HELD)
2. onPlaybackFinished tries to acquire p.mu → blocks if another goroutine holds p.mu
3. That other goroutine calls speaker.Lock() → blocks because speaker lock is held by beep
4. DEADLOCK
```

### Pattern: Speaker Lock Scope Minimization

The current code correctly locks the speaker only when mutating streamer state:

```go
func (p *Player) startPaused() {
    speaker.Lock()
    p.control.Paused = true
    speaker.Unlock()
    // speaker.Play registers streamers — does its own locking.
    speaker.Play(beep.Seq(p.speakerStreamer, beep.Callback(func() {
        go p.onPlaybackFinished()
    })))
    p.state = Paused
}
```

**Keep speaker.Lock() regions as small as possible.** Never do I/O, logging, or event emission while holding the speaker lock.

### Pattern: Streamer Chain Lifecycle

The current `updateStreamers()` method correctly rebuilds the entire chain (base → resample → ctrl → volume) on each track load. This is the right pattern for beep — streamer chains are cheap to construct and shouldn't be reused across tracks.

**One improvement:** The `updateStreamers` method preserves volume state across track changes, which is correct. But it could also preserve the paused state:

```go
func (p *Player) updateStreamers(newBaseStreamer beep.StreamSeeker, sr beep.SampleRate) error {
    // ...existing code...
    
    // Preserve existing pause state across track changes.
    prevPaused := false
    if p.control != nil {
        prevPaused = p.control.Paused
    }
    
    p.control = &beep.Ctrl{Streamer: p.resampled, Paused: prevPaused}
    // ...
}
```

### Extractable Pure Logic from Player

These functions can be extracted and tested without audio hardware:

| Function | Current Location | Pure? | Test Value |
|---|---|---|---|
| Volume conversion (user 0-100 ↔ beep logarithmic) | Inline in `SetVolume`/`GetVolume` | Yes | Edge cases: 0, 1, 50, 100 |
| Display position calculation | `displayPositionSecsLocked()` | Yes (math only) | Seek position rounding, track length boundary |
| Track info construction | `getCurrentTrackInfoLocked()` | Mostly (reads state) | Null file, missing metadata |
| Resample quality mapping | Currently hardcoded `4` | Yes (when made configurable) | Quality 1-6 range validation |

### What NOT to Do

| Anti-Pattern | Why It's Wrong | Instead |
|---|---|---|
| Replacing beep with a lower-level audio library (oto, portaudio) | beep provides the streamer composition model (Seq, Ctrl, Volume, Resample) that the player relies on. Dropping to oto means reimplementing all of this. | Stay on beep/v2. File issues for bugs. |
| Multiple speaker.Init calls | `speaker.Init` can only be called once (or after `speaker.Close()`). Calling it again is undefined behavior. The current "init once on startup" is correct. | Keep single Init on startup. If sample rate needs to change, the entire speaker must be closed and reinitialized. |
| Holding p.mu during speaker.Play() | `speaker.Play()` does its own internal locking. Holding p.mu during the call is safe but unnecessary — and if beep ever calls back synchronously (which it currently doesn't for `Play()`), could cause issues. | Release p.mu before speaker.Play() if possible, or document why it's held. |

---

## Development Tools: Existing Stack Assessment

### Already Correct — No Changes Needed

| Tool | Version | Assessment |
|---|---|---|
| golangci-lint v2 | v2.9.0 | Strict config already in place. Catches most issues. |
| Race detector | Go 1.25 | Already enabled in `make test`. |
| lefthook | v1.13.6 | Pre-commit hooks run vet, lint, codegen-check, typecheck. |
| govulncheck | v1.1.4 | Vulnerability scanning for Go dependencies. |
| sqlc | v1.30.0 | SQL-to-Go code generation for type-safe queries. |
| pprof profiling | Built-in | Dev-only pprof server on localhost:6060, block/mutex profiling enabled. |
| Vite + HMR | v7.0.0 | Fast frontend rebuilds during development. |

### Recommended Addition: `t.TempDir()` for Test Isolation

Go 1.15+ provides `t.TempDir()` which auto-cleans. Use for config tests and any test that needs filesystem:

```go
func TestConfigSave(t *testing.T) {
    dir := t.TempDir()  // cleaned up automatically
    path := filepath.Join(dir, "config.toml")
    // ...
}
```

### Recommended Addition: `t.Parallel()` for Independent Tests

Mark tests that don't share state as parallel to speed up the test suite:

```go
func TestQueueAddTrack(t *testing.T) {
    t.Parallel()  // runs concurrently with other parallel tests
    db := newTestDB(t)  // each test gets its own in-memory DB
    // ...
}
```

**Important:** Only use `t.Parallel()` when each test creates its own database and mock player. Tests that share state (global variables, singleton stores) cannot be parallel.

---

## Version Compatibility

| Package | Current Version | Compatible With | Notes |
|---|---|---|---|
| Go | 1.25.0 | All dependencies | Go 1.25 introduced `t.Context()`, tool directive in go.mod |
| modernc.org/sqlite | v1.45.0 | SQLite 3.51.x | Match modernc.org/libc version exactly per upstream warning |
| beep/v2 | v2.1.1 | ebitengine/oto v3.3.3 | oto is the audio backend; version locked through go.mod |
| Lit | ^3.2.1 | @lit-labs/virtualizer ^2.1.1 | Labs packages are experimental but stable for virtualizer |
| @lit-labs/signals | ^0.2.0 | Lit ^3.2.1 | Used for signal-based reactivity; experimental API may change |
| sqlc | v1.30.0 | modernc.org/sqlite | sqlc generates code for `database/sql` interface; driver-agnostic |

---

## Sources

- SQLite WAL documentation: https://www.sqlite.org/wal.html — **HIGH confidence** (official docs, updated 2025-05-31)
- SQLite PRAGMA documentation: https://www.sqlite.org/pragma.html — **HIGH confidence** (official docs)
- modernc.org/sqlite API: https://pkg.go.dev/modernc.org/sqlite@v1.46.1 — **HIGH confidence** (official Go package docs)
- gopxl/beep wiki — Composing and controlling: https://github.com/gopxl/beep/wiki/Composing-and-controlling — **HIGH confidence** (official beep docs)
- Lit rendering docs: https://lit.dev/docs/components/rendering/ — **HIGH confidence** (official Lit docs)
- Go race detector: https://go.dev/doc/articles/race_detector — **HIGH confidence** (official Go docs)
- Codebase analysis: `.planning/codebase/CONCERNS.md`, `.planning/codebase/STACK.md` — **HIGH confidence** (direct code inspection)
- beep speaker.Lock() behavior: inferred from beep wiki and codebase lock ordering comments — **MEDIUM confidence** (documented in code but not in beep's API docs)

---

*Stack research for: YellowJacket consolidation milestone*
*Researched: 2026-02-27*
