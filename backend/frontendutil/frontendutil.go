// Package frontendutil provides Go functions bound to the frontend.
package frontendutil

import (
	"context"
	"fmt"
	"os"

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
// to M3U/M3U8 playlist files. Multiple files may be selected.
func (fe *FrontendUtil) PlaylistFilePicker() (
	[]string,
	error,
) {
	runtime.LogInfo(fe.ctx, "selecting playlist files")

	files, err := runtime.OpenMultipleFilesDialog(
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
	file, err := runtime.OpenFileDialog(
		fe.ctx,
		runtime.OpenDialogOptions{
			Title: "Select Cover Art",
			Filters: []runtime.FileFilter{
				{
					DisplayName: "Image Files (*.jpg, *.jpeg, *.png)",
					Pattern:     "*.jpg;*.jpeg;*.png",
				},
			},
		},
	)
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
