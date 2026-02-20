// Package frontendutil provides Go functions bound to the frontend.
package frontendutil

import (
	"context"
	"fmt"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// FrontendUtil provides frontend-bound Go functions.
type FrontendUtil struct {
	ctx context.Context
}

// NewFrontendUtil creates a new FrontendUtil instance.
func NewFrontendUtil() (*FrontendUtil, error) {
	return &FrontendUtil{}, nil
}

// SetContext sets the Wails runtime context.
func (fe *FrontendUtil) SetContext(ctx context.Context) {
	fe.ctx = ctx
}

// DirectoryPicker opens a directory selection dialog.
func (fe *FrontendUtil) DirectoryPicker() (string, error) {
	runtime.LogInfo(fe.ctx, "selecting a directory")

	dir, err := runtime.OpenDirectoryDialog(
		fe.ctx,
		runtime.OpenDialogOptions{})
	if err != nil {
		return "", fmt.Errorf(
			"could not open directory dialog\n%w", err,
		)
	}

	return dir, nil
}

// PlaylistFilePicker opens a file selection dialog filtered
// to M3U/M3U8 playlist files.
func (fe *FrontendUtil) PlaylistFilePicker() (
	string,
	error,
) {
	runtime.LogInfo(fe.ctx, "selecting a playlist file")

	file, err := runtime.OpenFileDialog(
		fe.ctx,
		runtime.OpenDialogOptions{
			Title: "Import Playlist",
			Filters: []runtime.FileFilter{
				{
					DisplayName: "Playlist Files (*.m3u, *.m3u8)",
					Pattern:     "*.m3u;*.m3u8",
				},
			},
		},
	)
	if err != nil {
		return "", fmt.Errorf(
			"could not open file dialog: %w", err,
		)
	}

	return file, nil
}
