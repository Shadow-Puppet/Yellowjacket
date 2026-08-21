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

// envTempDir is the variable Go's os.TempDir() reads, and through it
// every library in the process that asks for a temporary file.
const envTempDir = "TMPDIR"

// tempDirName is the subdirectory of the app's own storage that
// becomes that answer.
const tempDirName = "tmp"

// UseTempDir gives the process a temporary directory that exists.
//
// **Android has no /tmp and hands an app no TMPDIR**, and Go's
// os.TempDir() falls back to "/tmp" when the variable is unset -- so
// every library in the process that wants scratch space is handed a
// path that has never existed. SQLite is the one that noticed: an
// INSERT ... SELECT large enough to spill returned
// SQLITE_IOERR_GETTEMPPATH (disk I/O error 6410), which is how the
// champion search index came to fail its rebuild on every launch while
// the app otherwise looked healthy (#190).
//
// It is the *class* that is fixed here rather than that statement.
// Anything that spills fails the same way on that platform -- large
// sorts, large joins, VACUUM -- so the repair belongs at the process's
// one answer to the question rather than at each caller. The
// alternative considered was PRAGMA temp_store = MEMORY, which is
// cheaper and more local and is a promise that every future spill fits
// in RAM on a phone; the catalog is the largest thing in this app and
// that is not a promise worth making silently.
//
// The rules are UseHomeOverride's, for the same reasons. **An empty
// base is a no-op**, because that is what
// application.Mobile.StoragePath() returns on desktop -- so this needs
// no build tag and changes nothing off mobile, where /tmp is real. And
// **an explicit TMPDIR wins**, so anyone who set one deliberately gets
// what they asked for; nothing sets it on the platform this exists for.
//
// It returns its error rather than swallowing it because a temp
// directory that could not be created is the same silent failure one
// step earlier, and since #160 a log line on that platform is
// something a person can actually read.
func UseTempDir(base string) error {
	if base == "" || os.Getenv(envTempDir) != "" {
		return nil
	}

	dir := filepath.Join(base, tempDirName)

	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return fmt.Errorf("could not make the temp directory %s: %w", dir, err)
	}

	// Writability is checked rather than assumed: the whole failure
	// this repairs is a directory that is named and cannot be used, and
	// MkdirAll on an existing unwritable directory succeeds.
	probe, err := os.CreateTemp(dir, "probe")
	if err != nil {
		return fmt.Errorf("temp directory %s is not writable: %w", dir, err)
	}

	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)

	if err := os.Setenv(envTempDir, dir); err != nil {
		return fmt.Errorf("could not set %s: %w", envTempDir, err)
	}

	return nil
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
