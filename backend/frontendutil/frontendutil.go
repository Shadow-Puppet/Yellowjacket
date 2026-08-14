// Package frontendutil provides Go functions bound to the frontend.
package frontendutil

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// ErrNoRuntime is returned when a dialog is asked for with no running
// application to parent it to — under test, or after shutdown.
var ErrNoRuntime = errors.New("no Wails runtime to show a dialog")

// FrontendUtil provides frontend-bound Go functions.
type FrontendUtil struct {
	ctx context.Context
}

// NewFrontendUtil creates a new FrontendUtil instance.
func NewFrontendUtil() (*FrontendUtil, error) {
	return &FrontendUtil{}, nil
}

// ServiceStartup is v3's service lifecycle hook: it runs once the
// runtime exists, and ctx is cancelled when the app shuts down.  It
// replaces v2's SetContext, which had to be called by hand from
// OnStartup and was exported, so it was also bound to the frontend.
func (fe *FrontendUtil) ServiceStartup(
	ctx context.Context,
	_ application.ServiceOptions,
) error {
	fe.ctx = ctx

	return nil
}

// DirectoryPicker opens a directory selection dialog.
//
// v3 has no separate directory dialog: it is the file dialog told to
// choose directories and not files.
func (fe *FrontendUtil) DirectoryPicker() (string, error) {
	app := application.Get()
	if app == nil {
		return "", ErrNoRuntime
	}

	slog.Default().Info("selecting a directory")

	dir, err := app.Dialog.OpenFile().
		CanChooseDirectories(true).
		CanChooseFiles(false).
		PromptForSingleSelection()
	if err != nil {
		return "", fmt.Errorf(
			"could not open directory dialog\n%w", err,
		)
	}

	return dir, nil
}

// PlaylistFilePicker opens a file selection dialog filtered
// to M3U/M3U8 playlist files. Multiple files may be selected.
func (fe *FrontendUtil) PlaylistFilePicker() (
	[]string,
	error,
) {
	app := application.Get()
	if app == nil {
		return nil, ErrNoRuntime
	}

	slog.Default().Info("selecting playlist files")

	files, err := app.Dialog.OpenFile().
		SetTitle("Import Playlist").
		AddFilter("Playlist Files (*.m3u, *.m3u8)", "*.m3u;*.m3u8").
		PromptForMultipleSelection()
	if err != nil {
		return nil, fmt.Errorf(
			"could not open file dialog: %w", err,
		)
	}

	return files, nil
}

// ImageFilePicker opens a file selection dialog filtered to image
// files (JPEG, PNG).  Returns the selected file path, or empty
// string if the user cancelled.
func (fe *FrontendUtil) ImageFilePicker() (string, error) {
	app := application.Get()
	if app == nil {
		return "", ErrNoRuntime
	}

	file, err := app.Dialog.OpenFile().
		SetTitle("Select Cover Art").
		AddFilter("Image Files (*.jpg, *.jpeg, *.png)", "*.jpg;*.jpeg;*.png").
		PromptForSingleSelection()
	if err != nil {
		return "", fmt.Errorf("could not open file dialog: %w", err)
	}

	return file, nil
}

// ReadFile reads a file from disk and returns its contents.
// Used by the frontend to read cover art image files selected
// via ImageFilePicker.
func (fe *FrontendUtil) ReadFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file %q: %w", path, err)
	}

	return data, nil
}
