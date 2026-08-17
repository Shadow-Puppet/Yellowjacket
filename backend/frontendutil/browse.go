package frontendutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
)

// errNotADirectory is returned when a caller asks to list something
// that exists but is not a directory.
var errNotADirectory = errors.New("not a directory")

// DirEntry is one selectable directory in a listing.
type DirEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// DirListing is one level of the filesystem, as a folder picker needs
// it: where we are, what is above, and the directories below.
//
// Parent is empty at a root, which is what tells the UI not to draw an
// "up" affordance rather than having it compute that from the path
// separator.
type DirListing struct {
	Path    string     `json:"path"`
	Parent  string     `json:"parent"`
	Entries []DirEntry `json:"entries"`
}

// ListDirectories returns the directories directly inside path, so the
// frontend can draw a folder picker.
//
// **It exists because Android has no directory picker.** Wails' file
// dialog can choose directories on every desktop platform, and on
// Android it returns an error: the Storage Access Framework yields tree
// URIs rather than filesystem paths, and a path is what this app's
// entire library model is keyed on. Rather than teach the backend about
// tree URIs, the app browses the filesystem itself — which it can do
// because it holds all-files access (see the manifest).
//
// Three rules, each of which a picker gets wrong if it is not stated:
// only directories are returned, because the caller is choosing a
// library root and files are noise; unreadable children are skipped
// rather than failing the whole listing, since Android's storage root
// contains directories no app may enter; and hidden directories are
// omitted, because a music library is not in one and `.thumbnails`
// alone would swamp the list.
func (fe *FrontendUtil) ListDirectories(path string) (DirListing, error) {
	if path == "" {
		path = fe.DefaultBrowseRoot()
	}

	path = filepath.Clean(path)

	info, err := os.Stat(path)
	if err != nil {
		return DirListing{}, fmt.Errorf("could not open %s: %w", path, err)
	}

	if !info.IsDir() {
		return DirListing{}, fmt.Errorf("%w: %s", errNotADirectory, path)
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return DirListing{}, fmt.Errorf("could not read %s: %w", path, err)
	}

	dirs := make([]DirEntry, 0, len(entries))

	for _, e := range entries {
		name := e.Name()
		if name == "" || name[0] == '.' {
			continue
		}

		// A symlink reports itself, not its target, so ask the
		// filesystem: a symlinked music directory is ordinary and
		// skipping it would be a bug the user cannot explain.
		child := filepath.Join(path, name)

		info, err := os.Stat(child)
		if err != nil || !info.IsDir() {
			continue
		}

		dirs = append(dirs, DirEntry{Name: name, Path: child})
	}

	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name < dirs[j].Name })

	parent := filepath.Dir(path)
	if parent == path {
		parent = ""
	}

	return DirListing{Path: path, Parent: parent, Entries: dirs}, nil
}

// androidSharedStorage is where a user's music lives on Android. It is
// not derivable from the environment the way a desktop home directory
// is: HOME inside an Android app process is "/", so os.UserHomeDir()
// would start the picker at the filesystem root with nothing readable
// under it.
const androidSharedStorage = "/storage/emulated/0"

// DefaultBrowseRoot is where a folder picker should open.
func (fe *FrontendUtil) DefaultBrowseRoot() string {
	if runtime.GOOS == "android" {
		for _, candidate := range []string{androidSharedStorage, "/storage"} {
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				return candidate
			}
		}

		return "/"
	}

	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return home
	}

	return string(filepath.Separator)
}

// StorageAccess reports whether the app can actually read the place the
// user's music lives.
type StorageAccess struct {
	Root     string `json:"root"`
	Readable bool   `json:"readable"`
	Reason   string `json:"reason"`
}

// CheckStorageAccess asks the filesystem rather than the permission
// system.
//
// On Android this app holds MANAGE_EXTERNAL_STORAGE, which the user
// grants on a Settings screen rather than in a dialog — so it can be
// refused, revoked later, or simply never answered, and the permission
// API is one more thing that can disagree with reality. Reading the
// directory is the question the library scanner will actually ask, so
// it is the one worth answering.
//
// It is deliberately not an error return: "we cannot read your music
// yet" is a state the UI renders, not a failure of the call.
func (fe *FrontendUtil) CheckStorageAccess() StorageAccess {
	root := fe.DefaultBrowseRoot()

	if _, err := os.ReadDir(root); err != nil {
		return StorageAccess{
			Root:     root,
			Readable: false,
			Reason:   err.Error(),
		}
	}

	return StorageAccess{Root: root, Readable: true}
}

// HasNativeDirectoryPicker reports whether this platform can open a
// directory dialog at all.
//
// It is asked of the backend rather than tested in the frontend with
// `System.IsAndroid()`, for three reasons. The dialog *is* backend code
// — `DirectoryPicker` above — so this is the same package saying what
// it can do. It answers for iOS too without the frontend enumerating
// platforms. And it makes the frontend's fallback testable through the
// ordinary transport fake instead of a module mock of the Wails
// runtime, whose platform helpers read build constants.
func (fe *FrontendUtil) HasNativeDirectoryPicker() bool {
	switch runtime.GOOS {
	case "android", "ios":
		return false
	default:
		return true
	}
}
