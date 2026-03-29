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

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"yellowjacket/backend/assets"
	"yellowjacket/backend/config"
	"yellowjacket/backend/coverart"
	"yellowjacket/backend/database"
	"yellowjacket/backend/explore"
	"yellowjacket/backend/frontendutil"
	"yellowjacket/backend/library"
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
	mediaControls mediacontrols.Handler
	tagWriter     *tagwriter.TagWriter
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

	yjApp.FEBindings = []any{
		yjApp.FrontendUtil,
		yjApp.appConfig,
		yjApp.library,
		yjApp.playlist,
		yjApp.queue,
		yjApp.player,
		yjApp.tagWriter,
		yjApp.explore,
	}

	return yjApp, nil
}

// playerAdapter wraps *player.Player to satisfy the tagwriter.PlayerStopper
// interface, breaking the import cycle between tagwriter and player.
type playerAdapter struct{ p *player.Player }

func (a *playerAdapter) CurrentFilePath() string {
	return a.p.GetCurrentTrackInfo().FilePath
}

func (a *playerAdapter) StopAndRelease() { a.p.UnloadTrack() }

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
		ResolvePhantoms: yj.playlist.ResolvePhantomTracksAfterScan,
		OnAllScansComplete: func() {
			// Start the index build after scans finish.
			// Tiers 2-4 are incremental — filterUnindexed
			// already skips artists that are already indexed,
			// so new library artists with freshly-populated
			// MBIDs get picked up without invalidating the
			// entire discography cache.
			yj.explore.StartIndexBuild()
		},
	})

	// Wire removal hooks so the library can stop playback and
	// compact the queue during library removal without depending
	// on the player or queue packages directly.
	yj.library.SetRemovalHooks(library.RemovalHooks{
		StopPlayback: func() { yj.player.UnloadTrack() },
		CompactQueue: yj.queue.CompactAfterLibraryRemoval,
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
			yj.explore.StartIndexBuild()
		}
	}()
}
