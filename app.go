package main

import (
	"context"
	"fmt"
	"log/slog"
	"yellowjacket/backend/config"
	"yellowjacket/backend/library"
	"yellowjacket/backend/logging"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
	ctx     context.Context
	config  *config.Config
	library *library.Library
	logger  *slog.Logger
}

// NewApp creates a new App application struct
func NewApp(conf *config.Config) *App {
	if conf == nil {
		// TODO proper error handling
		panic(fmt.Errorf("conf was nil"))
	}
	return &App{
		config: conf,
	}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	runtime.LogInfo(a.ctx, fmt.Sprintf("starting app with config %s", logging.PrettyJSON(*a.config)))

	musicLib, err := library.GetNewLibrary(ctx, a.config.Library)
	if err != nil {
		panic(fmt.Errorf("could not initialize library: %w", err))
	}
	// config might have been updated, so lets propogate that to the app
	a.config.Library = musicLib.Conf

	runtime.LogInfo(a.ctx, fmt.Sprintf("using library %s", logging.PrettyJSON(musicLib)))
	a.library = musicLib
}

// trying to make a function
func (a *App) DirectoryPicker() (string, error) {
	runtime.LogInfo(a.ctx, "selecting a directory")
	dir, err := runtime.OpenDirectoryDialog(
		a.ctx,
		runtime.OpenDialogOptions{})
	if err != nil {
		return "", fmt.Errorf("could not open directory dialog\n%w", err)
	}
	if dir == "" {
		return "No Library Directory Selected", nil
	}

	// we got a directory, lets do something with it
	runtime.LogInfo(a.ctx, fmt.Sprintf("got directory %s assigning to library directory config %s", dir, logging.PrettyJSON(*a.config)))
	a.config.Library.DirectoryPath = dir
	a.config.WriteConfig()
	return dir, nil
}
