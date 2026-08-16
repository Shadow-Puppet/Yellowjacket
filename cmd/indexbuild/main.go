//go:build indexbuild

// Command indexbuild maintains the explore search index outside the
// desktop app, so the catalog can be built once centrally instead of by
// every install.
//
// It decides what to do from the index's own state rather than needing
// the caller to know:
//
//	no completed import      → build   (first run, or resume a partial one)
//	import older than 3mo    → rebuild (re-import from the newest dump)
//	otherwise                → refresh (fold in new incremental listens)
//
// A full build streams ~89GB from the ListenBrainz spark dump — far
// more than one CI job should attempt — so builds are budgeted and
// resumable: the importer checkpoints its absolute stream offset, and
// each run continues where the last stopped. A refresh is cheap
// (~250MB incremental dumps) and finishes in one run.
//
// Usage:
//
//	YJ_HOME=/cache indexbuild -budget 3h
//
// Exit codes:
//
//	0  up to date — nothing left to do
//	3  build incomplete — schedule another run to resume
//	1  error
//
// When GITHUB_OUTPUT is set, `complete` and `changed` are appended to it
// so a workflow can decide whether to publish a new artifact.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"yellowjacket/backend/database"
	"yellowjacket/backend/explore"
	"yellowjacket/backend/system"
)

// exitIncomplete tells the caller the build made progress but has not
// finished, so another run should follow. Distinct from a failure: the
// checkpoint is valid and resuming is the correct action.
const exitIncomplete = 3

// mode is what this run decided to do.
type mode string

const (
	modeAuto    mode = "auto"
	modeBuild   mode = "build"
	modeRefresh mode = "refresh"
	modeRebuild mode = "rebuild"
)

// errNoHome is returned when YJ_HOME is unset. The default per-user data
// directory is deliberately not used: a build host should always write
// to an explicit, persistent location.
var errNoHome = errors.New(
	"YJ_HOME must be set to a persistent directory on real disk",
)

// errIncomplete signals a clean stop with work remaining.
var errIncomplete = errors.New("build incomplete")

var errBadMode = errors.New("unknown mode")

type opts struct {
	budget       time.Duration
	mode         mode
	rebuildAfter time.Duration
	refreshAfter time.Duration
	verbose      bool
}

func main() {
	var (
		budget = flag.Duration("budget", 3*time.Hour,
			"stop and checkpoint a build after this long (0 = no limit)")
		modeFlag = flag.String("mode", string(modeAuto),
			"auto | build | refresh | rebuild")
		rebuildAfter = flag.Duration("rebuild-after", 180*24*time.Hour,
			"re-import from a fresh dump once the last import is older than this")
		refreshAfter = flag.Duration("refresh-after", 7*24*time.Hour,
			"minimum gap between incremental refreshes (0 = always)")
		verbose = flag.Bool("v", false, "debug logging")
	)

	flag.Parse()

	err := run(opts{
		budget:       *budget,
		mode:         mode(*modeFlag),
		rebuildAfter: *rebuildAfter,
		refreshAfter: *refreshAfter,
		verbose:      *verbose,
	})
	if err != nil {
		if errors.Is(err, errIncomplete) {
			os.Exit(exitIncomplete)
		}

		fmt.Fprintln(os.Stderr, "indexbuild:", err)
		os.Exit(1)
	}
}

func run(o opts) error {
	logger := newLogger(o.verbose)

	if os.Getenv("YJ_HOME") == "" {
		return errNoHome
	}

	dataDir, err := system.GetUserDataDirPath()
	if err != nil {
		return fmt.Errorf("resolve data dir: %w", err)
	}

	// Before the schema is applied, not after: applying it over a table
	// whose shape has since changed is what fails, and this database's
	// non-catalog half is disposable.  See retireLibraryTables.
	if err := retireLibraryTables(context.Background(), logger); err != nil {
		return fmt.Errorf("retire stale tables: %w", err)
	}

	db, err := database.NewDB(logger)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}

	// NewExploreService is reused rather than reconstructing its
	// dependency graph here, so the headless path cannot drift from what
	// the app does. SetContext is never called: it installs the Wails
	// runtime context, and emitting Wails events against a non-Wails
	// context terminates the process.
	svc := explore.NewExploreService(logger.WithGroup("explore"), db)

	chosen, why := decide(o, svc)

	logger.Info("index maintenance",
		"mode", string(chosen),
		"reason", why,
		"dataDir", dataDir,
		"staging", filepath.Join(dataDir, "explore-staging"),
		"lastImported", stamp(svc.IndexLastImported()),
		"baselineSeries", svc.IndexBaselineSeries(),
	)

	seriesBefore := svc.IndexBaselineSeries()

	switch chosen {
	case modeRefresh:
		err = doRefresh(logger, svc, o.refreshAfter)
	case modeRebuild:
		svc.PrepareIndexRebuild()

		err = doBuild(logger, svc, o.budget)
	case modeBuild:
		err = doBuild(logger, svc, o.budget)
	case modeAuto:
		return fmt.Errorf("%w: auto should have resolved", errBadMode)
	default:
		return fmt.Errorf("%w: %q", errBadMode, chosen)
	}

	complete := svc.IndexImportComplete() && !errors.Is(err, errIncomplete)

	// "Changed" means there is something new worth publishing, so it is
	// only ever true for a finished import: a build stamps the listens
	// series early, long before its rows are assembled, and reporting a
	// change off that would be a lie about a half-built index.
	changed := complete &&
		(svc.IndexBaselineSeries() != seriesBefore || chosen != modeRefresh)

	report(logger, svc, chosen, complete, changed)

	if writeErr := writeOutputs(complete, changed); writeErr != nil {
		logger.Warn("could not write workflow outputs", "err", writeErr)
	}

	return err
}

func newLogger(verbose bool) *slog.Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}

	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	}))
}

// indexState is the slice of the index that the mode decision depends
// on. Narrowing it to an interface keeps the decision table testable
// without standing up a database.
type indexState interface {
	IndexImportComplete() bool
	IndexLastImported() time.Time
}

// decide picks the mode from the index's own state, so callers (a cron,
// a push hook, a human) need no knowledge of where the index stands.
func decide(o opts, svc *explore.Service) (mode, string) {
	return decideFrom(o, svc)
}

func decideFrom(o opts, state indexState) (mode, string) {
	if o.mode != modeAuto {
		return o.mode, "explicitly requested"
	}

	if !state.IndexImportComplete() {
		return modeBuild, "no completed import yet"
	}

	last := state.IndexLastImported()
	if last.IsZero() {
		// Marker present but unparseable — treat as due rather than
		// letting a malformed timestamp wedge the rebuild cadence.
		return modeRebuild, "import timestamp unreadable"
	}

	if age := time.Since(last); age >= o.rebuildAfter {
		return modeRebuild, fmt.Sprintf("last import %s ago (>= %s)",
			age.Round(time.Hour), o.rebuildAfter)
	}

	return modeRefresh, "import current, folding in new listens"
}

func doBuild(
	logger *slog.Logger, svc *explore.Service, budget time.Duration,
) error {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	defer signal.Stop(stop)

	var timer <-chan time.Time

	if budget > 0 {
		t := time.NewTimer(budget)
		defer t.Stop()

		timer = t.C
	}

	// finished is closed by this function alone, so the watchdog only
	// reads it — no double close, and it exits whether the build ended
	// on its own or was stopped.
	finished := make(chan struct{})

	go func() {
		select {
		case <-finished:
		case sig := <-stop:
			logger.Info("build: signal received, checkpointing",
				"signal", sig.String())
			svc.StopIndexBuild()
		case <-timer:
			logger.Info("build: budget reached, checkpointing")
			svc.StopIndexBuild()
		}
	}()

	start := time.Now()

	svc.StartIndexBuild()
	svc.WaitForIndexIdle()
	close(finished)

	logger.Info("build stopped", "elapsed", time.Since(start).Round(time.Second))

	if !svc.IndexImportComplete() {
		return errIncomplete
	}

	return nil
}

func doRefresh(
	logger *slog.Logger, svc *explore.Service, minInterval time.Duration,
) error {
	start := time.Now()

	svc.RefreshIndexNow(minInterval)

	logger.Info("refresh finished",
		"elapsed", time.Since(start).Round(time.Second))

	return nil
}

func report(
	logger *slog.Logger, svc *explore.Service,
	chosen mode, complete, changed bool,
) {
	status := svc.GetIndexStatus()

	logger.Info("index state",
		"mode", string(chosen),
		"complete", complete,
		"changed", changed,
		"artists", status.Artists,
		"releaseGroups", status.ReleaseGroups,
		"recordings", status.Recordings,
		"totalRows", status.TotalRows,
		"baselineSeries", svc.IndexBaselineSeries(),
	)

	for _, tier := range status.Tiers {
		logger.Info("  stage",
			"name", tier.Name,
			"state", tier.State,
			"completed", tier.Completed,
			"total", tier.Total,
			"error", tier.Error,
		)
	}

	if !complete {
		logger.Info("build incomplete — rerun to resume from checkpoint")
	}
}

// writeOutputs appends step outputs when running under a workflow, so
// the caller can publish only when something actually changed.
func writeOutputs(complete, changed bool) error {
	path := os.Getenv("GITHUB_OUTPUT")
	if path == "" {
		return nil
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open outputs: %w", err)
	}

	defer func() { _ = f.Close() }()

	_, err = fmt.Fprintf(f, "complete=%s\nchanged=%s\n",
		strconv.FormatBool(complete), strconv.FormatBool(changed))
	if err != nil {
		return fmt.Errorf("write outputs: %w", err)
	}

	return nil
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return "never"
	}

	return t.UTC().Format(time.RFC3339)
}
