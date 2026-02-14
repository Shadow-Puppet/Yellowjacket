// Package main is the entry point for the Yellowjacket application.
package main

import (
	"embed"
	"log/slog"
	"os"

	"github.com/golang-cz/devslog"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"yellowjacket/backend"
	"yellowjacket/backend/assets"
	"yellowjacket/backend/logging"
	"yellowjacket/internal/dev"
)

//go:embed all:frontend/dist
var frontendDistAssets embed.FS

func main() {
	isDev := dev.IsDev
	// create sLogger
	var loglevel slog.Level
	if isDev {
		loglevel = slog.LevelDebug
	} else {
		loglevel = slog.LevelInfo
	}

	sLogger := slog.New(devslog.NewHandler(os.Stdout, &devslog.Options{
		HandlerOptions: &slog.HandlerOptions{
			Level: loglevel,
		},
	}))
	slog.SetDefault(sLogger)
	sLogger.Info("starting yellowjacket")

	// create asset handler
	assetHandler, err := assets.NewAssetHandler(sLogger, frontendDistAssets)
	if err != nil {
		sLogger.Error("could not create asset handler", "err", err.Error())
		os.Exit(1)
	}

	yjApp, err := backend.NewYellowJacketApp(sLogger, assetHandler)
	if err != nil {
		sLogger.Error("problem initializing yellowjacket", "err", err.Error())
		os.Exit(1)
	}

	// Create application with options
	err = wails.Run(&options.App{
		Title:  "yellowjacket",
		Width:  512,
		Height: 384,
		Logger: logging.NewLogger(
			sLogger,
			[]string{},
		),
		AssetServer:      assetHandler.Options,
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:        yjApp.OnStartup,
		OnDomReady:       yjApp.OnDomReady,
		OnShutdown:       yjApp.OnShutdown,
		Bind:             yjApp.FEBindings,
		MinWidth:         512,
		MinHeight:        384,
		MaxWidth:         0,
		MaxHeight:        0,
	})
	if err != nil {
		sLogger.Error("application error", "err", err.Error())
		os.Exit(1)
	}
}
