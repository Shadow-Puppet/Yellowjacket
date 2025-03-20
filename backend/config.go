package backend

import (
	"fmt"
	"os"
	"path"

	"github.com/BurntSushi/toml"
)

type Config struct {
	LibraryDirPath string
	MyConfigValue  string
}

var defaultConfig *Config = &Config{
	LibraryDirPath: "",
	MyConfigValue:  "testtesttest",
}

func GetConfig() (*Config, error) {

	configDir, err := GetUserConfigDirPath()
	if err != nil {
		return nil, fmt.Errorf("could not get user config directory path: %w", err)
	}
	configFilePath := path.Join(configDir, "config.toml")
	// create the config file and load defaults if it doesn't exist
	_, err = os.Stat(configFilePath)
	if os.IsNotExist(err) {
		defaultConfig.WriteConfig(configFilePath)
	}
	conf, err := LoadConfig(configFilePath)
	if err != nil {
		return nil, fmt.Errorf("could not load config file %s: %w", configFilePath, err)
	}
	return conf, nil
}

func LoadConfig(configFilePath string) (*Config, error) {
	// read in the file
	confFileData, err := os.ReadFile(configFilePath)
	if err != nil {
		return nil, fmt.Errorf("problem reading config file %s: %w", configFilePath, err)
	}

	// parse it into the config struct
	var conf Config
	_, err = toml.Decode(string(confFileData), &conf)
	if err != nil {
		return nil, fmt.Errorf("problem parsing config file %s: %w", configFilePath, err)
	}

	// validate the config
	if err = conf.validate(); err != nil {
		return nil, fmt.Errorf("invalid config file at %s: %w", configFilePath, err)
	}

	return &conf, nil
}

func (c *Config) WriteConfig(configFileWritePath string) error {
	confFileData, err := toml.Marshal(c)
	if err != nil {
		return fmt.Errorf("could not marshal config struct: %w", err)
	}
	err = os.WriteFile(configFileWritePath, confFileData, os.FileMode(int(0666)))
	if err != nil {
		return fmt.Errorf("could not write config file: %w", err)
	}
	return nil
}

// return errors if there is a *breaking* issue with the config
func (c *Config) validate() error {
	return nil
}
