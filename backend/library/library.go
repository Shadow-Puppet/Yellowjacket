package library

import (
	"context"
	"fmt"
	"os"
)

type Config struct {
	DirectoryPath string
	SaveFunc      func() error `toml:"-"`
}

func (c *Config) Validate() error {
	return nil
}

var DefaultConfig *Config = &Config{
	DirectoryPath: "",
}

type Library struct {
	ctx  context.Context
	conf *Config
}

func NewLibrary(conf *Config) (*Library, error) {
	if conf == nil {
		return nil, fmt.Errorf("nil config for library")
	}
	if err := conf.Validate(); err != nil {
		return nil, fmt.Errorf("invalid library config %#v: %w", conf, err)
	}
	return &Library{
		conf: conf,
	}, nil
}

func (l *Library) Init(ctx context.Context) error {
	l.ctx = ctx
	return nil
}

func (l *Library) GetDir() (string, error) {
	return l.conf.DirectoryPath, nil
}

func (l *Library) SetDir(dirPath string) error {
	fileInfo, err := os.Stat(dirPath)
	if err != nil {
		return fmt.Errorf("could not stat %s: %w", dirPath, err)
	}
	if !fileInfo.IsDir() {
		return fmt.Errorf("dirPath is not a directory: %s", dirPath)
	}

	l.conf.DirectoryPath = dirPath
	err = l.conf.SaveFunc()
	if err != nil {
		return fmt.Errorf("could not save library dir config: %w", err)
	}
	return nil
}
