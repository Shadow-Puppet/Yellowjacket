package config

import (
	"fmt"
	"os"
	"path"
	"yellowjacket/backend/library"

	"github.com/BurntSushi/toml"
)

type Config struct {
	filePath string // required
	*configData
}

func newConfig(filePath string, data *configData) (*Config, error) {
	// TODO make sure required fields have SOMETHING in them
	// TODO Merge existing config with read in config
	if data == nil {
		data = defaultConfigData
	}
	return &Config{
		filePath:   filePath,
		configData: data,
	}, nil
}

type configData struct {
	Library *library.Config
}

// return errors if there is a *breaking* issue with the config
func (d *configData) validate() error {
	return nil
}

var defaultConfigData *configData = &configData{
	Library: library.DefaultConfig,
}

// GetCurrentConfig will load and return the config
// reading the config file in the user's config directory
func GetCurrentConfig() (*Config, error) {
	// get the config file location
	configDir, err := GetUserConfigDirPath()
	if err != nil {
		return nil, fmt.Errorf("could not get user config directory path: %w", err)
	}
	configFilePath := path.Join(configDir, "config.toml")
	config, err := newConfig(configFilePath, nil)
	if err != nil {
		return nil, fmt.Errorf("could not create new config: %w", err)
	}
	config.filePath = configFilePath

	_, err = os.Stat(configFilePath)
	if os.IsNotExist(err) {
		config.WriteConfig()
	}
	config, err = config.loadConfig()
	if err != nil {
		return nil, fmt.Errorf("could not load config file %s: %w", configFilePath, err)
	}
	return config, nil
}

func (c *Config) loadConfig() (*Config, error) {
	// read in the file
	confFileData, err := os.ReadFile(c.filePath)
	if err != nil {
		return nil, fmt.Errorf("problem reading config file %s: %w", c.filePath, err)
	}

	// parse it into the config struct
	var confData configData
	_, err = toml.Decode(string(confFileData), &confData)
	if err != nil {
		return nil, fmt.Errorf("problem parsing config file %s: %w", c.filePath, err)
	}

	// validate the config
	if err = confData.validate(); err != nil {
		return nil, fmt.Errorf("invalid config file at %s: %w", c.filePath, err)
	}

	config, err := newConfig(c.filePath, &confData)
	if err != nil {
		return nil, fmt.Errorf("could not create config from config file data at %s: %w", c.filePath, err)
	}

	return config, nil
}

func (c *Config) WriteConfig() error {
	confFileData, err := toml.Marshal(c)
	if err != nil {
		return fmt.Errorf("could not marshal config struct: %w", err)
	}

	err = os.WriteFile(c.filePath, confFileData, os.FileMode(int(0666)))
	if err != nil {
		return fmt.Errorf("could not write config file: %w", err)
	}
	return nil
}
