package main

import (
	"context"
	"embed"
	"fmt"
	"yellowjacket/backend/config"
	"yellowjacket/backend/frontendbindings"
	"yellowjacket/backend/library"
	"yellowjacket/backend/player"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/logger"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	yjConf, err := config.GetCurrentConfig()
	if err != nil {
		panic(fmt.Errorf("could not get config: %w", err))
	}

	// Create objects that will be bound on the frontend
	frontendBindings, err := frontendbindings.NewFrontendBindings()
	if err != nil {
		panic(fmt.Errorf("could not create frontendBindings obj: %w", err))
	}
	player, err := player.NewPlayer()
	if err != nil {
		panic(fmt.Errorf("could not create player obj: %w", err))
	}
	library, err := library.NewLibrary(yjConf.Library)
	if err != nil {
		panic(fmt.Errorf("could not create library obj: %w", err))
	}

	// Create application with options
	err = wails.Run(&options.App{
		Title:  "yellowjacket",
		Width:  512,
		Height: 384,
		AssetServer: &assetserver.Options{
			Assets:     assets,
			Handler:    nil,
			Middleware: nil,
		},
		LogLevel:         logger.TRACE,
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup: func(ctx context.Context) {
			frontendBindings.Init(ctx)
			library.Init(ctx)
			player.Init(ctx)
		},
		Bind: []any{
			frontendBindings,
			library,
			player,
		},
		DisableResize:      false,
		Fullscreen:         false,
		Frameless:          false, // TODO look into this
		MinWidth:           512,
		MinHeight:          384,
		MaxWidth:           0,
		MaxHeight:          0,
		StartHidden:        false,
		HideWindowOnClose:  false,
		AlwaysOnTop:        false,
		Menu:               &menu.Menu{},
		Logger:             nil,
		LogLevelProduction: 0,
		OnDomReady: func(ctx context.Context) {
		},
		OnShutdown: func(ctx context.Context) {
		},
		OnBeforeClose: func(ctx context.Context) bool {
			return false
		},
		EnumBind:                         []any{},
		WindowStartState:                 0,
		ErrorFormatter:                   nil,
		EnableDefaultContextMenu:         false,
		EnableFraudulentWebsiteDetection: false,
		SingleInstanceLock:               &options.SingleInstanceLock{},
		Windows:                          &windows.Options{},
		Mac:                              &mac.Options{},
		Linux:                            &linux.Options{},
		Experimental:                     &options.Experimental{},
		Debug:                            options.Debug{},
		DragAndDrop:                      &options.DragAndDrop{},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
