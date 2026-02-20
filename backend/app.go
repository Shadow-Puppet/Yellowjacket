// Package backend contains the main application logic.
package backend

//go:generate go tool templ generate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"yellowjacket/backend/assets"
	"yellowjacket/backend/config"
	"yellowjacket/backend/database"
	"yellowjacket/backend/frontendutil"
	"yellowjacket/backend/library"
	"yellowjacket/backend/player"
	"yellowjacket/backend/playlist"
	"yellowjacket/backend/queue"
)

// YellowJacketApp is the main application struct for Wails.
type YellowJacketApp struct {
	FEBindings   []any
	FrontendUtil *frontendutil.FrontendUtil

	logger       *slog.Logger
	assetHandler *assets.Handler
	database     *database.DB
	library      *library.Library
	player       *player.Player
	playlist     *playlist.Service
	queue        *queue.Queue
	appContext   context.Context
	appConfig    *config.Config
}

// NewYellowJacketApp creates and initializes the application.
func NewYellowJacketApp(
	logger *slog.Logger,
	assetHandler *assets.Handler,
) (*YellowJacketApp, error) {
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
	coverHandler, err := library.NewCoverArtHandler()
	if err != nil {
		return nil, fmt.Errorf("could not create cover art handler: %w", err)
	}

	yjApp.assetHandler.RegisterHandler("/covers/", coverHandler)

	// create playlist service
	yjApp.playlist = playlist.NewService(
		yjApp.logger, yjApp.database, yjApp.appConfig,
	)

	yjApp.FEBindings = []any{
		yjApp.FrontendUtil,
		yjApp.appConfig,
		yjApp.library,
		yjApp.playlist,
	}

	return yjApp, nil
}

// WindowConfig returns the window configuration for use by the host.
func (yj *YellowJacketApp) WindowConfig() *config.WindowConfig {
	return yj.appConfig.Window
}

var startupErr error

// OnStartup initializes components that require the Wails runtime context.
func (yj *YellowJacketApp) OnStartup(ctx context.Context) {
	// initialize anything that needs to use the wails runtime AFTER its been initialized
	// you CANNOT use the wails runtime during this function
	yj.appContext = ctx

	// Set context for components that need Wails runtime for events
	yj.appConfig.SetContext(ctx)
	yj.FrontendUtil.SetContext(ctx)
	yj.library.SetContext(ctx)
	yj.playlist.SetContext(ctx)

	var err error
	// create player
	yj.player, err = player.NewPlayer(ctx, yj.logger.WithGroup("player"), yj.database)
	if err != nil {
		startupErr = errors.Join(startupErr, fmt.Errorf("could not create player: %w", err))
	}

	yj.player.SetContext(ctx)

	// create queue
	yj.queue = queue.NewQueue(yj.logger, yj.database)
	yj.queue.SetContext(ctx)
	yj.queue.SetPlayer(yj.player)
	yj.queue.RestoreState()

	// Give the library a reference to the queue so FullRescan can
	// clear the queue and stop playback before wiping data.
	yj.library.SetQueue(yj.queue)

	// Give the library a reference to the playlist service so
	// FullRescan can restore playlists from M3U8 files.
	yj.library.SetPlaylistRestorer(yj.playlist)

	// Register playback finished handler to drive queue auto-advance.
	yj.player.SetPlaybackFinishedHandler(yj.queue.OnPlaybackFinished)

	// Add player to frontend bindings
	yj.FEBindings = append(yj.FEBindings, yj.player)
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
}

// OnDomReady handles post-DOM initialization and startup error reporting.
func (yj *YellowJacketApp) OnDomReady(ctx context.Context) {
	if startupErr != nil {
		yj.logger.Error("startup error", "err", startupErr.Error())
		wailsruntime.Quit(ctx)
	}

	// Push current player and queue state to the frontend. The heavy lifting
	// (file load, seek, volume) already happened during OnStartup via
	// RestoreState; this just emits events. A short delay ensures the
	// frontend JS modules have loaded and registered their event listeners.
	go func() {
		time.Sleep(200 * time.Millisecond)

		if yj.player != nil {
			yj.player.EmitCurrentState()
		}

		if yj.queue != nil {
			yj.queue.EmitCurrentState()
		}
	}()
}
