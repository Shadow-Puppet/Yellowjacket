package frontendutil

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// listing helper: a tree with the three shapes the picker has to get
// right — an ordinary directory, a file (never listed), and a hidden
// directory (never listed).
func browseFixture(t *testing.T) string {
	t.Helper()

	root := t.TempDir()

	for _, dir := range []string{"Music", "Podcasts", "aaa", ".thumbnails"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	if err := os.WriteFile(filepath.Join(root, "track.mp3"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	return root
}

func TestListDirectories(t *testing.T) {
	fe := &FrontendUtil{}
	root := browseFixture(t)

	got, err := fe.ListDirectories(root)
	if err != nil {
		t.Fatalf("ListDirectories: %v", err)
	}

	// Sorted, directories only, no file and no dotted entry.
	want := []string{"Music", "Podcasts", "aaa"}
	if len(got.Entries) != len(want) {
		t.Fatalf("got %d entries %v, want %v", len(got.Entries), got.Entries, want)
	}

	for i, w := range want {
		if got.Entries[i].Name != w {
			t.Errorf("entry %d = %q, want %q", i, got.Entries[i].Name, w)
		}

		if got.Entries[i].Path != filepath.Join(root, w) {
			t.Errorf("entry %d path = %q, want %q", i, got.Entries[i].Path, filepath.Join(root, w))
		}
	}

	if got.Path != root {
		t.Errorf("Path = %q, want %q", got.Path, root)
	}

	if got.Parent != filepath.Dir(root) {
		t.Errorf("Parent = %q, want %q", got.Parent, filepath.Dir(root))
	}
}

// A symlink reports itself rather than its target, so a listing that
// trusts DirEntry.IsDir() silently drops a symlinked music folder --
// which is an ordinary thing to have and an unexplainable thing to
// lose.
func TestListDirectoriesFollowsSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need elevation on Windows")
	}

	root := t.TempDir()
	target := filepath.Join(root, "real")

	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	link := filepath.Join(root, "linked")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	fe := &FrontendUtil{}

	got, err := fe.ListDirectories(root)
	if err != nil {
		t.Fatalf("ListDirectories: %v", err)
	}

	if len(got.Entries) != 2 {
		t.Fatalf("got %v, want both 'linked' and 'real'", got.Entries)
	}
}

// A dangling symlink, and anything else os.Stat refuses, must be
// skipped rather than failing the whole listing: Android's storage
// root holds directories no app may enter, and one of them must not
// cost the user the picker.
func TestListDirectoriesSkipsUnreadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need elevation on Windows")
	}

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "good"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	dangling := filepath.Join(root, "dangling")
	if err := os.Symlink(filepath.Join(root, "nowhere"), dangling); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	fe := &FrontendUtil{}

	got, err := fe.ListDirectories(root)
	if err != nil {
		t.Fatalf("ListDirectories: %v", err)
	}

	if len(got.Entries) != 1 || got.Entries[0].Name != "good" {
		t.Errorf("got %v, want only 'good'", got.Entries)
	}
}

func TestListDirectoriesRejectsFiles(t *testing.T) {
	root := t.TempDir()

	file := filepath.Join(root, "track.mp3")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	fe := &FrontendUtil{}

	if _, err := fe.ListDirectories(file); err == nil {
		t.Error("listing a file should be an error")
	}

	if _, err := fe.ListDirectories(filepath.Join(root, "missing")); err == nil {
		t.Error("listing a missing path should be an error")
	}
}

// An empty path means "start where the picker should open", so the
// frontend never has to know the platform.
func TestListDirectoriesDefaultsToBrowseRoot(t *testing.T) {
	fe := &FrontendUtil{}

	got, err := fe.ListDirectories("")
	if err != nil {
		t.Skipf("default root not listable in this environment: %v", err)
	}

	if got.Path != fe.DefaultBrowseRoot() {
		t.Errorf("Path = %q, want the default root %q", got.Path, fe.DefaultBrowseRoot())
	}
}

// Parent is empty at a root, which is what tells the picker not to draw
// an "up" control rather than making it reason about separators.
func TestListDirectoriesRootHasNoParent(t *testing.T) {
	fe := &FrontendUtil{}

	got, err := fe.ListDirectories(string(filepath.Separator))
	if err != nil {
		t.Skipf("filesystem root not listable: %v", err)
	}

	if got.Parent != "" {
		t.Errorf("Parent = %q at the root, want empty", got.Parent)
	}
}

func TestCheckStorageAccess(t *testing.T) {
	fe := &FrontendUtil{}

	got := fe.CheckStorageAccess()
	if got.Root == "" {
		t.Error("Root should never be empty")
	}

	// The developer machine running this test can read its own home
	// directory; the assertion is that the two fields agree, not that
	// access is granted.
	if got.Readable && got.Reason != "" {
		t.Errorf("readable but Reason = %q", got.Reason)
	}

	if !got.Readable && got.Reason == "" {
		t.Error("not readable but no Reason given")
	}
}

// The picker's fallback is chosen from this, so a platform that gains
// a working dialog must flip it here rather than in the frontend.
func TestHasNativeDirectoryPicker(t *testing.T) {
	fe := &FrontendUtil{}

	want := runtime.GOOS != "android" && runtime.GOOS != "ios"
	if got := fe.HasNativeDirectoryPicker(); got != want {
		t.Errorf("HasNativeDirectoryPicker() on %s = %v, want %v", runtime.GOOS, got, want)
	}
}
