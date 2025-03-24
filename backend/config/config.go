package config

import (
	"errors"
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
		return nil, errors.New("nil config")
	}
	if err := data.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
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
	if d.Library == nil {
		return errors.New("nil library config")
	}
	if err := d.Library.Validate(); err != nil {
		return fmt.Errorf("invalid library config: %s\n%w", d.Library, err)
	}
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

	// create the config obj with the filepath we got
	config, err := newConfig(configFilePath, defaultConfigData)
	if err != nil {
		return nil, fmt.Errorf("could not create new config: %w", err)
	}
	config.filePath = configFilePath

	// does the config file alaeady exist?
	// if not, create it
	_, err = os.Stat(configFilePath)
	if os.IsNotExist(err) {
		if err := config.WriteConfig(); err != nil {
			return nil, fmt.Errorf("could not write config: %w", err)
		}
	}

	// now that we have our config file, load it in
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
	if err := c.validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}
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
