//go:build dev

package profiling

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"runtime"
	"runtime/trace"
	"strconv"
	"time"
)

const (
	// pprofAddr is the address the pprof HTTP server listens on.
	pprofAddr = "localhost:6060"

	// defaultTraceSecs is the default trace capture duration.
	defaultTraceSecs = 5

	// blockProfileRate controls the fraction of goroutine blocking
	// events reported. 1 = every event (most detailed, slight overhead).
	blockProfileRate = 1

	// mutexProfileFraction controls the fraction of mutex contention
	// events reported. 5 = 1/5 of events.
	mutexProfileFraction = 5

	// serverShutdownTimeout is the maximum time to wait for the
	// pprof server to drain connections on shutdown.
	serverShutdownTimeout = 5 * time.Second
)

// Start launches the pprof HTTP server and enables block/mutex profiling.
// It returns a stop function that gracefully shuts down the server.
func Start(logger *slog.Logger) func() {
	plog := logger.WithGroup("profiling")

	// Enable block and mutex profiling so /debug/pprof/block and
	// /debug/pprof/mutex return useful data.
	runtime.SetBlockProfileRate(blockProfileRate)
	runtime.SetMutexProfileFraction(mutexProfileFraction)

	mux := http.NewServeMux()

	// Register the standard pprof handlers.
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	// Custom endpoint: capture a runtime/trace for a configurable
	// duration and stream it back. Usage:
	//   curl -o trace.out http://localhost:6060/debug/trace?seconds=5
	//   go tool trace trace.out
	mux.HandleFunc("/debug/trace", traceHandler(plog))

	srv := &http.Server{
		Addr:              pprofAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Use a listener so we can log the actual bound address.
	ln, err := net.Listen("tcp", pprofAddr)
	if err != nil {
		plog.Error(
			"Failed to start pprof server",
			"addr", pprofAddr, "err", err,
		)

		return func() {}
	}

	plog.Info(
		fmt.Sprintf(
			"pprof server listening on http://%s/debug/pprof/",
			ln.Addr().String(),
		),
	)

	go func() {
		if serveErr := srv.Serve(ln); serveErr != nil &&
			!errors.Is(serveErr, http.ErrServerClosed) {
			plog.Error("pprof server error", "err", serveErr)
		}
	}()

	return func() {
		plog.Info("Shutting down pprof server")

		ctx, cancel := context.WithTimeout(
			context.Background(), serverShutdownTimeout,
		)
		defer cancel()

		if shutErr := srv.Shutdown(ctx); shutErr != nil {
			plog.Error(
				"pprof server shutdown error",
				"err", shutErr,
			)
		}

		// Disable block/mutex profiling.
		runtime.SetBlockProfileRate(0)
		runtime.SetMutexProfileFraction(0)
	}
}

// traceHandler returns an HTTP handler that captures a runtime/trace
// for the requested number of seconds (default 5).
func traceHandler(logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		secs := defaultTraceSecs

		if s := r.URL.Query().Get("seconds"); s != "" {
			if v, err := strconv.Atoi(s); err == nil && v > 0 {
				secs = v
			}
		}

		logger.Info(
			"Starting trace capture",
			"seconds", secs,
		)

		w.Header().Set(
			"Content-Type", "application/octet-stream",
		)
		w.Header().Set(
			"Content-Disposition",
			"attachment; filename=trace.out",
		)

		if err := trace.Start(w); err != nil {
			http.Error(
				w,
				fmt.Sprintf("trace already in progress: %v", err),
				http.StatusConflict,
			)

			return
		}

		time.Sleep(time.Duration(secs) * time.Second)
		trace.Stop()

		logger.Info(
			"Trace capture complete",
			"seconds", secs,
		)
	}
}
