package main

import (
	"context"
	"fmt"
	"yellowjacket/backend"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
	ctx    context.Context
	config *backend.Config
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	conf, err := backend.GetConfig()
	if err != nil {
		panic(fmt.Errorf("could not get config: %w", err))
	}
	a.config = conf
}

// trying to make a function
func (a *App) DirectoryPicker() (string, error) {
	dir, err := runtime.OpenDirectoryDialog(
		a.ctx,
		runtime.OpenDialogOptions{
			ShowHiddenFiles: true,
		})

	if err != nil {
		return "", fmt.Errorf("could not open directory dialog\n%w", err)
	}
	if dir == "" {
		return "No Library Directory Selected", nil
	}
	return fmt.Sprintf("Library Directory: %s", dir), nil

}
