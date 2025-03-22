package library

import (
	"context"
	"fmt"
	"yellowjacket/backend/logging"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type Config struct {
	DirectoryPath string
}

var DefaultConfig *Config = &Config{
	DirectoryPath: "",
}

type Library struct {
	ctx  context.Context
	Conf *Config
}

func GetNewLibrary(ctx context.Context, conf *Config) (*Library, error) {
	if conf == nil {
		runtime.LogInfo(ctx, fmt.Sprintf("library config is nil, using default config: %s", logging.PrettyJSON(DefaultConfig)))
		conf = DefaultConfig
	}
	runtime.LogInfo(ctx, fmt.Sprintf("creating new library with config: %#v", conf))
	return &Library{
		ctx:  ctx,
		Conf: conf,
	}, nil
}
