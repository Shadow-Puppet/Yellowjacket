package main

import (
	"context"
	"embed"
	"fmt"

	"yellowjacket/backend/config"
	"yellowjacket/backend/player"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/logger"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
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
			Assets: assets,
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
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
