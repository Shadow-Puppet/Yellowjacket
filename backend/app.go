// Package backend contains the main application logic.
package backend

//go:generate go tool templ generate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"yellowjacket/backend/assets"
	"yellowjacket/backend/autotagservice"
	"yellowjacket/backend/config"
	"yellowjacket/backend/coverart"
	"yellowjacket/backend/database"
	"yellowjacket/backend/download"
	"yellowjacket/backend/events"
	"yellowjacket/backend/explore"
	"yellowjacket/backend/frontendutil"
	"yellowjacket/backend/jobs"
	"yellowjacket/backend/library"
	"yellowjacket/backend/maintenance"
	"yellowjacket/backend/mediacontrols"
	"yellowjacket/backend/player"
	"yellowjacket/backend/playlist"
	"yellowjacket/backend/profiling"
	"yellowjacket/backend/queue"
	"yellowjacket/backend/system"
	"yellowjacket/backend/tagwriter"
)

// YellowJacketApp is the main application struct for Wails.
type YellowJacketApp struct {
	FEBindings   []any
	FrontendUtil *frontendutil.FrontendUtil

	logger        *slog.Logger
	assetHandler  *assets.Handler
	database      *database.DB
	library       *library.Library
	player        *player.Player
	playlist      *playlist.Service
	queue         *queue.Queue
	explore       *explore.Service
	autotag       *autotagservice.Service
	downloads     *download.Manager
	downloadSvc   *download.Service
	wanted        *download.Reconciler
	jobs          *jobs.Registry
	mediaControls mediacontrols.Handler
	tagWriter     *tagwriter.TagWriter
	janitor       *maintenance.Runner
	appContext    context.Context
	appConfig     *config.Config
	startupErr    error
}

// NewYellowJacketApp creates and initializes the application.
func NewYellowJacketApp(
	logger *slog.Logger,
	assetHandler *assets.Handler,
) (*YellowJacketApp, error) {
	defer profiling.TimeOp(logger, "app.NewYellowJacketApp")()

	// initialize anything that does not need access to the wails runtime here
	yjApp := &YellowJacketApp{
		logger:       logger,
		assetHandler: assetHandler,
		appContext:   context.Background(),
		janitor:      maintenance.NewRunner(logger),
	}

	// create database
	db, err := database.NewDB(logger)
	if err != nil {
		return nil, fmt.Errorf("could not connect to local database: %w", err)
	}

	yjApp.database = db

	// create config
	appConfig, err := config.NewConfig(yjApp.logger)
	if err != nil {
		return nil, fmt.Errorf("could not get config: %w", err)
	}

	yjApp.appConfig = appConfig

	// create frontendUtil
	feUtil, err := frontendutil.NewFrontendUtil()
	if err != nil {
		return nil, fmt.Errorf("could not create frontendUtil: %w", err)
	}

	yjApp.FrontendUtil = feUtil

	lib, err := library.NewLibrary(
		yjApp.appContext,
		yjApp.logger,
		yjApp.appConfig.Library,
		yjApp.database,
	)
	if err != nil {
		return nil, fmt.Errorf("could not create library: %w", err)
	}

	yjApp.library = lib

	// create cover art handler
	coverHandler, err := coverart.NewHandler()
	if err != nil {
		return nil, fmt.Errorf("could not create cover art handler: %w", err)
	}

	yjApp.assetHandler.RegisterHandler(coverart.PathPrefix, coverHandler)

	// Register artist image handler for serving cached artist photos.
	artistImgDir, err := system.GetUserDataDirPath()
	if err == nil {
		artistImgHandler := http.StripPrefix(
			"/artist-images/",
			http.FileServer(http.Dir(filepath.Join(artistImgDir, "artist-images"))),
		)

		yjApp.assetHandler.RegisterHandler("/artist-images/", artistImgHandler)
	}

	// create playlist service
	yjApp.playlist = playlist.NewService(
		yjApp.logger, yjApp.database, yjApp.appConfig,
	)
	yjApp.playlist.SetFavoritesConfig(yjApp.appConfig)

	// create queue (before wails.Run so it can be bound)
	yjApp.queue = queue.NewQueue(yjApp.logger, yjApp.database)

	// create player (before wails.Run so it can be bound;
	// speaker hardware is initialized later in OnStartup)
	yjApp.player = player.NewPlayer(
		yjApp.logger.WithGroup("player"), yjApp.database,
	)

	// create tag writer
	yjApp.tagWriter = tagwriter.NewTagWriter(
		yjApp.logger,
		yjApp.database,
		&playerAdapter{p: yjApp.player},
		yjApp.library,
	)

	// create explore service
	yjApp.explore = explore.NewExploreService(
		yjApp.logger.WithGroup("explore"), yjApp.database,
	)

	// create the background job registry and wire it into the
	// subsystems that run long jobs, so scans and index builds all
	// report through one surface.
	yjApp.jobs = jobs.NewRegistry(
		yjApp.logger.WithGroup("jobs"),
		jobs.NewStore(yjApp.database, yjApp.logger.WithGroup("jobs")),
	)
	yjApp.library.SetJobRegistry(yjApp.jobs)
	yjApp.explore.SetJobRegistry(yjApp.jobs)

	// create autotag service (depends on explore + tagWriter)
	yjApp.autotag = autotagservice.NewService(
		yjApp.logger.WithGroup("autotag"),
		yjApp.database,
		yjApp.explore,
		yjApp.tagWriter,
	)

	// Create the download subsystem.  Acquiring music is optional: a
	// failure here (unwritable data dir, say) must not stop the app
	// from playing the library the user already has, so it is logged
	// and the feature stays unavailable rather than fatal.
	if err := yjApp.initDownloads(); err != nil {
		yjApp.logger.Error(
			"download clients unavailable", "error", err,
		)
	}

	yjApp.FEBindings = []any{
		yjApp.FrontendUtil,
		yjApp.appConfig,
		yjApp.library,
		yjApp.playlist,
		yjApp.queue,
		yjApp.player,
		yjApp.tagWriter,
		yjApp.explore,
		yjApp.autotag,
		jobs.NewService(yjApp.jobs),
	}

	if yjApp.downloadSvc != nil {
		yjApp.FEBindings = append(yjApp.FEBindings, yjApp.downloadSvc)
	}

	return yjApp, nil
}

// initDownloads builds the download subsystem: staging area, secret
// store, importer and manager, plus the Wails-bound service.
func (yj *YellowJacketApp) initDownloads() error {
	logger := yj.logger.WithGroup("download")

	staging, err := download.NewStaging(logger)
	if err != nil {
		return fmt.Errorf("could not create download staging: %w", err)
	}

	secrets, err := download.NewFileSecretStore()
	if err != nil {
		return fmt.Errorf("could not create download secret store: %w", err)
	}

	store := download.NewStore(yj.database)

	importer := download.NewImporter(logger, staging, yj.tagWriter, yj.library)

	yj.downloads = download.NewManager(
		logger, store, secrets, staging, importer, yj.library,
	)
	yj.downloads.SetJobRegistry(yj.jobs)

	yj.downloadSvc = download.NewService(logger, yj.downloads, store, secrets)

	// The wanted list needs the explore index to know what an artist
	// released and what the library already owns, so it is wired here
	// where both exist.  The reconcile loop itself is not started until
	// the Wails runtime is up.
	yj.wanted = download.NewReconciler(
		logger, store, yj.downloads, newExploreCatalog(yj.explore),
	)
	yj.downloadSvc.SetReconciler(yj.wanted)

	return nil
}

// playerAdapter wraps *player.Player to satisfy the tagwriter.PlayerStopper
// interface, breaking the import cycle between tagwriter and player.
type playerAdapter struct{ p *player.Player }

func (a *playerAdapter) CurrentFilePath() string {
	return a.p.GetCurrentTrackInfo().FilePath
}

func (a *playerAdapter) StopAndRelease() { a.p.UnloadTrack() }

// initDownloadRuntime brings the download subsystem up once the Wails
// runtime exists: it applies the user's import layout, builds providers
// from stored config, and clears staging left by a previous run.
//
// Provider construction and the sweep both touch the network and the
// filesystem, so they run in the background — a slow or unreachable
// download client must not delay the window appearing.
func (yj *YellowJacketApp) initDownloadRuntime(ctx context.Context) {
	cfg := yj.appConfig.Downloads
	if cfg == nil {
		cfg = &download.UserConfig{}
		cfg.ApplyDefaults()
	}

	yj.downloads.SetImportOptions(download.ImportOptions{
		PathTemplate: cfg.PathTemplate,
	})
	yj.downloads.SetMaxConcurrent(cfg.MaxConcurrent)

	go func() {
		if err := yj.downloads.Reload(ctx); err != nil {
			yj.logger.Warn("could not load download providers", "error", err)
		}

		yj.downloads.Sweep(ctx)
	}()

	if yj.wanted == nil {
		return
	}

	yj.wanted.SetInterval(cfg.WantedInterval())
	yj.wanted.SetBatch(cfg.WantedBatch)
	yj.wanted.SetOnChange(func() {
		wailsruntime.EventsEmit(ctx, events.WantedListChanged)
	})
	yj.wanted.Start(ctx)
}

// WindowConfig returns the window configuration for use by the host.
func (yj *YellowJacketApp) WindowConfig() *config.WindowConfig {
	return yj.appConfig.Window
}

// OnStartup initializes components that require the Wails runtime context.
func (yj *YellowJacketApp) OnStartup(ctx context.Context) {
	defer profiling.TimeOp(yj.logger, "app.OnStartup")()

	// initialize anything that needs to use the wails runtime AFTER its been initialized
	// you CANNOT use the wails runtime during this function
	yj.appContext = ctx

	// Set context for components that need Wails runtime for events
	yj.appConfig.SetContext(ctx)
	yj.FrontendUtil.SetContext(ctx)
	yj.library.SetContext(ctx)
	yj.playlist.SetContext(ctx)
	yj.playlist.EnsureDefaultPlaylist()
	// Recover playlists that lost tracks from a pre-fix FullRescan.
	go yj.playlist.RepopulateFromM3U()
	// Backfill snapshots for smart playlists created before
	// creation-time materialization existed.
	go yj.playlist.MaterializeUnmaterializedSmartPlaylists()

	// Initialize speaker hardware (player struct created in
	// NewYellowJacketApp for Wails binding registration).
	if err := yj.player.InitSpeaker(); err != nil {
		yj.startupErr = errors.Join(
			yj.startupErr,
			fmt.Errorf("could not initialize speaker: %w", err),
		)
	}

	yj.player.SetContext(ctx)
	yj.tagWriter.SetContext(ctx)
	yj.explore.SetContext(ctx)
	yj.autotag.SetContext(ctx)
	yj.jobs.SetContext(ctx)

	if yj.downloadSvc != nil {
		yj.downloadSvc.SetContext(ctx)
		yj.initDownloadRuntime(ctx)
	}

	// Bring back jobs the user paused before the last shutdown, still
	// paused.  Must run before the soft scan in OnDomReady, which
	// checks these records so it does not restart a paused library.
	yj.library.RestorePausedScans()
	yj.explore.AdoptPausedIndexBuild()

	// Wire queue (created in NewYellowJacketApp for Wails binding)
	yj.queue.SetContext(ctx)
	yj.queue.SetPlayer(yj.player)
	yj.queue.RestoreState()

	// Wire cross-cutting rescan hooks so the library can
	// orchestrate queue clearing and playlist restoration
	// without depending on those packages directly.
	yj.library.SetRescanHooks(library.RescanHooks{
		PreClear: func() {
			yj.queue.Clear()
			// Stop the search index build so it doesn't fight
			// with the rescan for DB access.
			yj.explore.StopIndexBuild()
		},
		PostScan: func() {
			yj.playlist.RestoreAllPlaylists()
			// DON'T restart the index build here — queued
			// library scans may still be running.  The index
			// build starts after ALL scans complete (via the
			// scan hooks below).
		},
	})

	// Wire scan hooks so the playlist service can resolve
	// phantom tracks after each library scan completes.
	yj.library.SetScanHooks(library.ScanHooks{
		RepopulatePlaylists: yj.playlist.RepopulateFromM3U,
		ResolvePhantoms:     yj.playlist.ResolvePhantomTracksAfterScan,
		OnAllScansComplete: func() {
			// Fold the local library into the search index: every
			// MB-verified owned artist/album/track is upserted and
			// flagged in_library, straight from the library tables
			// with no API calls.  Deep discographies stay lazy.
			yj.explore.PopulateLocalCrossReferences()

			// Enrich any owned artists whose discography hasn't been
			// fetched yet so their wider catalogue is searchable offline
			// right after the scan.  Background, bounded, resumable, and a
			// no-op once every owned artist is covered.
			yj.explore.BackfillLibraryDiscographies()

			// Start (or resume) the dump-based index build.  Skips
			// itself once the one-time import has completed, so this
			// is cheap on every startup.
			yj.explore.StartIndexBuild()

			// Fold in any new incremental listen dumps to keep
			// popularity fresh (weekly-gated, background, no API).
			// No-op while the full import above is still running.
			yj.explore.RefreshListenCounts()

			// Refresh the lyric-search FTS index from the just-scanned
			// library, then backfill any missing lyrics from LRCLIB in
			// the background (bounded, resumable, idempotent).
			yj.explore.RebuildLyricsIndex()
			yj.explore.BackfillLibraryLyrics()

			// Sweep the autotag queue for newly-discovered pending
			// items so the user sees match scores ready when they
			// next open the review page.  The worker is idempotent
			// (skips items that already have a score) and yields
			// to foreground review activity.
			yj.autotag.StartBackgroundPrefetch()
		},
	})

	// Wire removal hooks so the library can stop playback and
	// compact the queue during library removal without depending
	// on the player or queue packages directly.
	yj.library.SetRemovalHooks(library.RemovalHooks{
		StopPlayback: func() { yj.player.UnloadTrack() },
		CompactQueue: yj.queue.CompactAfterLibraryRemoval,
		// Removal deletes owned content outside a scan, so force the
		// gated library-sync steps to re-run on the next launch and
		// clear stale in_library flags / orphaned lyric-index rows.
		PostRemove: yj.explore.InvalidateLibrarySync,
	})

	// Register playback finished handler to drive queue auto-advance.
	yj.player.SetPlaybackFinishedHandler(yj.queue.OnPlaybackFinished)

	// Initialize OS media controls (MPRIS on Linux, no-op elsewhere).
	yj.mediaControls = mediacontrols.NewHandler(yj.logger)

	if err := yj.mediaControls.Init(mediacontrols.Callbacks{
		OnPlay: yj.queue.Play,
		OnPause: func() {
			if err := yj.player.Pause(); err != nil {
				yj.logger.Warn("MPRIS Pause failed", "err", err)
			}
		},
		OnPlayPause: func() {
			if yj.player.IsPlaying() {
				if err := yj.player.Pause(); err != nil {
					yj.logger.Warn("MPRIS PlayPause(pause) failed", "err", err)
				}
			} else {
				yj.queue.Play()
			}
		},
		OnStop: func() {
			if err := yj.player.Pause(); err != nil {
				yj.logger.Warn("MPRIS Stop failed", "err", err)
			}
		},
		OnNext:     yj.queue.Next,
		OnPrevious: yj.queue.Previous,
		OnSeek: func(positionSec int) {
			if err := yj.player.Seek(positionSec); err != nil {
				yj.logger.Warn("MPRIS Seek failed", "err", err)
			}
		},
		OnVolume: func(vol float64) {
			yj.player.SetVolume(
				player.UserVolume(
					vol * float64(player.MaxUserVol),
				),
			)
		},
	}); err != nil {
		yj.logger.Error(
			"Failed to initialize media controls",
			"err", err,
		)
	}

	yj.player.SetMediaControls(yj.mediaControls)
}

// OnBeforeClose captures window state while the window is still alive.
func (yj *YellowJacketApp) OnBeforeClose(ctx context.Context) bool {
	w, h := wailsruntime.WindowGetSize(ctx)

	// Guard against a bogus size clobbering a good saved one.  During
	// teardown / hot-reload the runtime can report a zero or below-
	// minimum size; persisting that would shrink the window to the
	// minimum on next launch.  Keep the previously-saved size instead.
	if w < config.MinWidth || h < config.MinHeight {
		yj.logger.Warn("OnBeforeClose: ignoring bogus window size",
			"width", w,
			"height", h,
			"kept_width", yj.appConfig.Window.Width,
			"kept_height", yj.appConfig.Window.Height,
		)

		return false
	}

	yj.logger.Info("OnBeforeClose: saving window state",
		"width", w,
		"height", h,
		"accentColor", yj.appConfig.Theme.AccentColor,
		"backgroundShade", yj.appConfig.Theme.BackgroundShade,
	)

	yj.appConfig.Window.Width = w
	yj.appConfig.Window.Height = h

	if err := yj.appConfig.Save(); err != nil {
		yj.logger.Error(
			"Failed to save window state",
			"err", err,
		)
	}

	return false
}

// OnShutdown saves player state and cleans up resources before the application exits.
func (yj *YellowJacketApp) OnShutdown(_ context.Context) {
	if yj.player != nil {
		yj.player.SaveState()
	}

	if yj.queue != nil {
		yj.queue.SaveState()
	}

	if yj.mediaControls != nil {
		yj.mediaControls.Close()
	}
}

// OnDomReady handles post-DOM initialization and startup error reporting.
// State synchronisation (player volume, track info, queue contents) is
// driven by the frontend: once its stores have registered their event
// listeners, index.ts calls Player.EmitCurrentState() and
// Queue.EmitCurrentState() via Wails bindings.
func (yj *YellowJacketApp) OnDomReady(ctx context.Context) {
	if yj.startupErr != nil {
		yj.logger.Error("startup error", "err", yj.startupErr.Error())
		wailsruntime.Quit(ctx)

		return
	}

	// Soft scan: compare file counts on disk vs DB for each library.
	// Only libraries with mismatched counts get a full scan — unchanged
	// libraries are silently skipped (no progress bar, no UI noise).
	go func() {
		if err := yj.library.SoftScanAllLibraries(); err != nil {
			yj.logger.Error("soft scan failed", "err", err)
		}

		// If no scans were queued (library unchanged), start the
		// index build directly.  If scans WERE queued, the
		// OnAllScansComplete hook starts it after they finish.
		if yj.library.GetScanQueueLength() == 0 && !yj.library.IsScanActive() {
			// Keep the search index's in_library flags and owned-entity
			// rows in sync.  Gated: the library is unchanged here, so
			// this only does work on the first launch after an upgrade
			// or index wipe — steady-state launches skip the write burst.
			yj.explore.PopulateLocalCrossReferencesIfNeeded()

			yj.explore.StartIndexBuild()

			// Weekly-gated incremental popularity refresh (background,
			// no API). No-op while the full import is running.
			yj.explore.RefreshListenCounts()

			// Same for the lyric-search index.  The backfill keeps it in
			// sync incrementally, so on an unchanged library the full
			// rebuild is redundant and gated out; the backfill still runs
			// to fill any remaining gaps.
			yj.explore.RebuildLyricsIndexIfNeeded()
			yj.explore.BackfillLibraryLyrics()

			// Continue enriching any owned artists still missing their
			// discography (e.g. a prior run was capped or interrupted).
			// Cheap no-op once every owned artist is covered.
			yj.explore.BackfillLibraryDiscographies()
		}

		// Kick off the autotag prefetch worker so any unscored
		// pending items get their match scores filled in while
		// the user does other things.  Idempotent — re-running on
		// every app launch is fine; previously-scored items are
		// skipped (the worker filters score IS NULL).
		yj.autotag.StartBackgroundPrefetch()

		// Start the janitor last: its sweeps compare against live data,
		// so running them after the scan and index work has settled
		// avoids deleting something a running import is about to
		// reference.  Each job enforces its own minimum interval, so the
		// daily tick is a cheap no-op most of the time.
		yj.startJanitor()
	}()
}

// janitorTick is how often the maintenance runner wakes up.  Individual
// jobs enforce their own minimum intervals, so most ticks do nothing.
const janitorTick = 6 * time.Hour

// startJanitor registers the maintenance jobs and starts the background
// runner.  Every job is registered here rather than at each package's
// init, so the full set of janitorial work is one visible list — a cache
// that forgets to register is missing from this function, which is
// harder to overlook than a function nobody calls.
func (yj *YellowJacketApp) startJanitor() {
	coversDir, err := coverart.CoversDir()
	if err != nil {
		yj.logger.Warn("janitor: could not resolve covers directory",
			"err", err)

		return
	}

	dataDir, err := system.GetUserDataDirPath()
	if err != nil {
		yj.logger.Warn("janitor: could not resolve user data directory",
			"err", err)

		return
	}

	yj.janitor.Register(maintenance.ExpiredHTTPCacheJob(yj.database))
	yj.janitor.Register(maintenance.OrphanedCoverFilesJob(
		yj.database, coversDir, library.CoverArtFileSet,
	))
	yj.janitor.Register(maintenance.OrphanedArtistImagesJob(
		yj.database, filepath.Join(dataDir, explore.ArtistImageDirName),
	))
	yj.janitor.Register(maintenance.ExpiredProxyCacheJob(
		filepath.Join(dataDir, explore.CoverArtCacheDirName),
	))

	yj.logger.Info("janitor started", "jobs", yj.janitor.JobNames())

	yj.janitor.Start(yj.appContext, janitorTick)
}
