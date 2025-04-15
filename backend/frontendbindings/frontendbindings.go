package frontendbindings

import (
	"context"
	"fmt"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// FrontendBindings contain any Go functions that are specific to the frontend only and need to be bound
type FrontendBindings struct {
	ctx context.Context
}

func NewFrontendBindings() (*FrontendBindings, error) {
	return &FrontendBindings{}, nil
}

func (fe *FrontendBindings) Init(ctx context.Context) error {
	fe.ctx = ctx
	return nil
}

// Open a directory picker
func (fe *FrontendBindings) DirectoryPicker() (string, error) {
	runtime.LogInfo(fe.ctx, "selecting a directory")
	dir, err := runtime.OpenDirectoryDialog(
		fe.ctx,
		runtime.OpenDialogOptions{})
	if err != nil {
		return "", fmt.Errorf("could not open directory dialog\n%w", err)
	}
	if dir == "" {
		return "No Library Directory Selected", nil
	}

	return dir, nil
}
