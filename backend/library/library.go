package library

import (
	"context"
	"fmt"
)

type Config struct {
	DirectoryPath string
}

func (c *Config) validate() error {
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
	if err := conf.validate(); err != nil {
		return nil, fmt.Errorf("invalid library config %s\n%w", conf, err)
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
