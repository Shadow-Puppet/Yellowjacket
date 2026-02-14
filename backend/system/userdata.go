// Package system provides OS-specific system utilities.
package system

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"runtime"
)

var (
	errNotDirectory  = errors.New("path is not a directory")
	errUnsupportedOS = errors.New("unsupported operating system")
)

// dirType represents the type of user directory.
type dirType string

const (
	dirTypeConfig dirType = "config"
	dirTypeData   dirType = "data"
)

// getUserDirPath returns and creates the path for a user directory.
func getUserDirPath(dt dirType) (string, error) {
	currentUser, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("could not get current user: %w", err)
	}

	path, err := buildUserDirPath(currentUser.Username, dt)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(path, os.ModePerm); err != nil {
		return "", fmt.Errorf("could not make user %s directory: %w", dt, err)
	}

	dirInfo, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("could not stat the user %s directory %s: %w", dt, path, err)
	}

	if !dirInfo.IsDir() {
		return "", fmt.Errorf("%w: %s", errNotDirectory, path)
	}

	return path, nil
}

// buildUserDirPath constructs the OS-specific path for a user directory.
func buildUserDirPath(username string, dt dirType) (string, error) {
	// Map directory types to their Unix subdirectory paths
	unixSubdirs := map[dirType]string{
		dirTypeConfig: ".config",
		dirTypeData:   ".local/share",
	}

	switch currentOS := runtime.GOOS; currentOS {
	case "darwin":
		return fmt.Sprintf("/Users/%s/%s/yellowjacket", username, unixSubdirs[dt]), nil
	case "linux":
		return fmt.Sprintf("/home/%s/%s/yellowjacket", username, unixSubdirs[dt]), nil
	case "windows":
		return fmt.Sprintf(`C:\Users\%s\AppData\local\yellowjacket\%s`, username, dt), nil
	default:
		return "", fmt.Errorf("%w: %s", errUnsupportedOS, currentOS)
	}
}

// GetUserConfigDirPath returns the user config directory path.
func GetUserConfigDirPath() (string, error) {
	return getUserDirPath(dirTypeConfig)
}

// GetUserDataDirPath returns the user data directory path.
func GetUserDataDirPath() (string, error) {
	return getUserDirPath(dirTypeData)
}
