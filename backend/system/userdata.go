// Package system provides OS-specific system utilities.
package system

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
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

// envHomeOverride is the environment variable that, when set, relocates
// all YellowJacket config and data under a single base directory. It
// exists so a development build can run against an isolated sandbox
// without touching the current user's real config.toml or yj.db.
const envHomeOverride = "YJ_HOME"

// UseHomeOverride points every config and data path at base, by setting
// the same override a development sandbox uses.
//
// It exists for mobile, where the switch in buildUserDirPath has no
// answer: there is no home directory and no XDG, only a per-app private
// directory the OS hands out at runtime. The caller is main(), which is
// the only place that can ask the platform for it — deliberately, so
// this package stays free of the Wails application package that knows
// (see backend/events' indexbuild split for why that matters).
//
// Two rules. An empty base is a no-op, because that is exactly what
// application.Mobile.StoragePath() returns on desktop. And an override
// that is already set wins, so YJ_HOME on the command line still
// relocates a sandbox on a platform that would otherwise decide for
// itself.
func UseHomeOverride(base string) {
	if base == "" || os.Getenv(envHomeOverride) != "" {
		return
	}

	_ = os.Setenv(envHomeOverride, base)
}

// getUserDirPath returns and creates the path for a user directory.
func getUserDirPath(dt dirType) (string, error) {
	path, err := resolveUserDirPath(dt)
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

// resolveUserDirPath picks the base path for a user directory. When
// YJ_HOME is set it wins for every OS, mapping to <YJ_HOME>/<dirType>;
// otherwise the standard OS-specific location is used.
func resolveUserDirPath(dt dirType) (string, error) {
	if home := os.Getenv(envHomeOverride); home != "" {
		return filepath.Join(home, string(dt)), nil
	}

	currentUser, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("could not get current user: %w", err)
	}

	return buildUserDirPath(currentUser.Username, dt)
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
