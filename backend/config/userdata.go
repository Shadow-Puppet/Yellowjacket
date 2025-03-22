package config

import (
	"fmt"
	"os"
	"os/user"
	"runtime"
)

// user config directories vary by operating system and purpose
// Linux/Mac: ~/.config
// Windows: C:\Users\<username>\AppData
func GetUserConfigDirPath() (string, error) {
	path := ""
	currentUser, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("could not get current user: %w", err)
	}
	switch currentOS := runtime.GOOS; currentOS {
	case "darwin":
		fallthrough
	case "linux":
		path = fmt.Sprintf("/home/%s/.config/yellowjacket", currentUser.Username)
	case "windows":
		path = fmt.Sprintf(`C:\Users\%s\AppData\local\yellowjacket\config`, currentUser.Username)
	default:
		return "", fmt.Errorf("unsupported OS: %s", currentOS)
	}
	err = os.MkdirAll(path, os.ModePerm)
	if err != nil {
		return "", fmt.Errorf("could not make user config directory: %w", err)
	}

	// final check
	dirInfo, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("could not stat the user config directory %s: %w", path, err)
	}

	if !dirInfo.IsDir() {
		return "", fmt.Errorf("not a directory: %s", path)
	}

	return path, nil
}

// user data directories vary by operating system and purpose
// Linux/Mac: ~/.local/share
// Windows: C:\Users\<username>\AppData\Local
func getUserDataDirPath() (string, error) {
	path := ""
	currentUser, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("could not get current user: %w", err)
	}
	switch currentOS := runtime.GOOS; currentOS {
	case "darwin":
		fallthrough
	case "linux":
		path = fmt.Sprintf("/home/%s/.local/share/yellowjacket", currentUser.Username)
	case "windows":
		path = fmt.Sprintf(`C:\Users\%s\AppData\local\yellowjacket\config`, currentUser.Username)
	default:
		return "", fmt.Errorf("unsupported OS: %s", currentOS)
	}
	err = os.MkdirAll(path, os.ModePerm)
	if err != nil {
		return "", fmt.Errorf("could not make user data directory: %w", err)
	}

	// final check
	dirInfo, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("could not stat the user data directory %s: %w", path, err)
	}

	if !dirInfo.IsDir() {
		return "", fmt.Errorf("not a directory: %s", path)
	}

	return path, nil
}
