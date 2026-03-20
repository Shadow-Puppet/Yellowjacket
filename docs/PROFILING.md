# Performance Profiling Guide

Practical guide for diagnosing performance issues in YellowJacket. This covers backend (Go/pprof), frontend (Chrome DevTools), and specific diagnostic workflows.

## 1. Backend Profiling (Go / pprof)

### Setup

`make dev` starts the app with a pprof server on `:6060`. No extra configuration needed — block and mutex profiling are enabled automatically in dev builds.

### Quick Start

```bash
./scripts/profile.sh              # Interactive menu
./scripts/profile.sh cpu          # 30s CPU profile (flame graph in browser)
./scripts/profile.sh heap         # Current memory usage
./scripts/profile.sh health       # Goroutine count, heap, GC stats
```

### Profile Types

| Profile | Use When | What It Shows |
|---------|----------|---------------|
| CPU | Something is slow | Time spent in each function (flame graph) |
| Heap | Memory growing | Current allocations by location |
| Allocs | GC pressure | Where allocations happen (even freed) |
| Goroutine | Hangs/deadlocks | All goroutines and their stack traces |
| Block | Lock contention | Where goroutines block on mutexes/channels |
| Mutex | Mutex bottleneck | Mutex contention hotspots |
| Trace | Scheduling issues | Timeline of goroutine scheduling, GC pauses, syscalls |

### Reading Flame Graphs

- **Wide bars** = more time spent in that function
- Look for unexpectedly wide bars (functions taking more time than they should)
- **Bottom** of the stack = entry points; **top** = leaf functions where time is actually spent
- Use the search box to filter by package (e.g. `library`, `queue`, `database`)
- Click a bar to zoom into that subtree; click "Root" to zoom back out

### Common YellowJacket Hotspots

| Function | What to Check |
|----------|---------------|
| `database.GetAllTracks` | Large library — check SQL query time, consider pagination |
| `library.extractMetadata` | Scan performance — check per-format timing in scan metrics |
| `queue.SetQueue` | Large queues — Phase 1/2 dedup and index rebuild |
| `coverart.Generate*` | Thumbnail generation — check per-tier timing |
| `database.SearchTracks` | FTS5 query performance — check query complexity |

### Manual pprof Access

If you prefer direct access without the script:

```bash
# CPU profile (30 seconds, opens flame graph)
go tool pprof -http=:8080 http://localhost:6060/debug/pprof/profile?seconds=30

# Heap profile
go tool pprof -http=:8080 http://localhost:6060/debug/pprof/heap

# Goroutine dump (text)
curl http://localhost:6060/debug/pprof/goroutine?debug=1

# Execution trace (5 seconds)
curl -o trace.out http://localhost:6060/debug/trace?seconds=5
go tool trace trace.out
```

## 2. Frontend Profiling (Chrome DevTools)

YellowJacket uses Wails (WebView2 on Linux/Windows, WebKit on macOS). On dev builds, Chrome DevTools is available for frontend profiling.

### Opening DevTools

Press `Ctrl+Shift+I` in a Wails dev build (or right-click → Inspect).

### Performance Panel (Scrolling & Rendering)

1. Open the **Performance** panel
2. Click **Record** (circle icon)
3. Perform the action you want to profile (scroll, navigate, etc.)
4. Click **Stop**
5. Analyze the timeline:

| Bar Color | Meaning | Target |
|-----------|---------|--------|
| Yellow | JavaScript execution | < 5ms per frame |
| Purple | Rendering / layout | Minimal during scroll |
| Green | Painting | Thin bars = composited (good) |
| Grey | Idle | Expected between frames |

**Target: each frame should complete in < 16ms for 60fps scrolling.**

### Key Metrics for Scroll Smoothness

- **Frame time:** Should be consistently < 16ms. Check the FPS row at the top.
- **Layout recalculation:** Should NOT happen during scrolling. If it does, CSS `contain` isn't working or a style is being read/written in a scroll handler.
- **Paint area:** Should be minimal. Large paint rects during scroll indicate missing `will-change: transform` on the scroll container.
- **JS execution during scroll:** Should be minimal. lit-virtualizer handles virtualization, but `renderItem` callbacks run for each new item entering the viewport.

### Memory Panel

1. Take a **heap snapshot** before an action
2. Perform the action (navigate views, scroll extensively)
3. Take another heap snapshot
4. Switch to **Comparison** view between the two snapshots
5. Look for growing arrays or detached DOM nodes

### What to Look For

| Symptom | Likely Cause | How to Check |
|---------|-------------|--------------|
| Scroll jank | Layout thrashing | Performance panel → look for "Layout" bars during scroll |
| Slow navigation | View recreation | Performance panel → long constructors after navigate event |
| Memory growth | Listener leaks | Memory panel → compare snapshots, filter "Detached" |
| Slow initial load | Blocking JS | Performance panel → gap between DOMContentLoaded and first paint |
| Flickering on navigate | View not cached | Check viewCache in app-layout — should be cached for primary views |

## 3. Profiling Workflow for Specific Issues

### "Scrolling feels janky"

1. Open DevTools → **Performance** panel
2. Click Record → scroll the problematic view for 3-5 seconds → Stop
3. Look at frame times — are any > 16ms?
4. **If JS is the bottleneck:** Check `renderItem` callback time. Are closures being created per-render? (Phase 14-03 eliminated this pattern)
5. **If Layout is the bottleneck:** Check if CSS `contain` is present on scroll containers. The app uses `contain: layout style` on the main panel and `contain: paint` on virtualizers.
6. **If Paint is the bottleneck:** Check if `will-change: transform` is on the scroll container. All scroll-heavy components should have this after Phase 14-01.

### "Navigation is slow"

1. Open DevTools → **Performance** panel
2. Click Record → navigate between views rapidly → Stop
3. Look for long JS tasks between the navigate event and first paint
4. Check if the view is being destroyed/recreated (look for constructor calls)
5. After Phase 14-02 view caching: navigation between primary views (Tracks, Albums, Artists, Genres, Playlists, Settings) should show almost no JS activity — the cached DOM is simply shown/hidden

### "Library operations feel slow"

1. Run `./scripts/profile.sh cpu` — capture for the duration of the operation
2. Check the flame graph for the specific Go function
3. For database operations: check if SQL queries are using indexes
4. For scan operations: scan metrics are already logged — check per-file timing
5. Run `./scripts/profile.sh trace` for detailed goroutine scheduling during the operation

### "Memory keeps growing"

1. Run `./scripts/profile.sh heap` at baseline
2. Perform the suspect operation repeatedly
3. Run `./scripts/profile.sh heap` again — compare the flame graphs
4. For frontend: use DevTools Memory panel heap snapshot comparison
5. Common causes: event listeners not cleaned up in `disconnectedCallback`, store subscriptions not unsubscribed, large arrays held by closed-over references

### "App feels sluggish after running for a while"

1. `./scripts/profile.sh health` — check goroutine count (should be stable, not growing)
2. `./scripts/profile.sh heap` — check if heap is much larger than expected
3. Check GC stats: high GC cycle count with growing heap = possible memory leak
4. Frontend: check for detached DOM nodes in DevTools Memory panel
