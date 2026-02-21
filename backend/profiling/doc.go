// Package profiling provides dev-only performance profiling via pprof and runtime/trace.
//
// In dev builds (build tag "dev"), Start launches an HTTP server on localhost:6060
// exposing the standard pprof endpoints and a /debug/trace endpoint for capturing
// execution traces. It also enables block and mutex profiling at reasonable sampling
// rates.
//
// In production builds, all exported functions are no-ops and the pprof/trace
// imports are excluded from the binary entirely.
//
// # Quick start
//
// Run the app in dev mode (pprof starts automatically):
//
//	make dev
//
// Then, in a separate terminal, use the interactive profiling helper:
//
//	./scripts/profile.sh
//
// The script provides a menu-driven interface that opens results in your
// browser as flame graphs. No pprof knowledge required. You can also
// invoke it directly:
//
//	./scripts/profile.sh cpu       # CPU profile
//	./scripts/profile.sh heap      # Heap (memory) profile
//	./scripts/profile.sh allocs    # Allocation profile
//	./scripts/profile.sh goroutine # Goroutine dump
//	./scripts/profile.sh block     # Block (sync) profile
//	./scripts/profile.sh mutex     # Mutex contention profile
//	./scripts/profile.sh trace     # Execution trace
//	./scripts/profile.sh health    # Quick runtime health check
//
// # Manual usage
//
// If you prefer the CLI directly:
//
//	go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30   # CPU
//	go tool pprof http://localhost:6060/debug/pprof/heap                 # Memory
//	go tool pprof http://localhost:6060/debug/pprof/goroutine            # Goroutines
//	curl -o trace.out http://localhost:6060/debug/trace?seconds=5        # Trace
//	go tool trace trace.out
//
// # Programmatic usage
//
//	stop := profiling.Start(logger)
//	defer stop()
//
// # Operation timing
//
// Use TimeOp to log the duration of any operation in dev builds:
//
//	defer profiling.TimeOp(logger, "player.LoadFile")()
//
// In production builds TimeOp is a no-op with zero overhead.
package profiling
