package main

import (
	"context"
	"embed"
	"fmt"
	"yellowjacket/backend/config"
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
	config, err := config.GetCurrentConfig()
	if err != nil {
		panic(fmt.Errorf("could not get config: %w", err))
	}

	app := NewApp(config)
	player := player.NewPlayer()

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
			app.startup(ctx)
			player.Init(ctx)
		},
		Bind: []interface{}{
			app,
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
			return
		},
		OnShutdown: func(ctx context.Context) {
			return
		},
		OnBeforeClose: func(ctx context.Context) bool {
			return false
		},
		EnumBind:                         []interface{}{},
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
