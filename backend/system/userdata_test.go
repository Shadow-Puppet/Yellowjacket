package system

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveUserDirPath_HomeOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv(envHomeOverride, home)

	tests := []struct {
		name string
		dt   dirType
		want string
	}{
		{name: "config", dt: dirTypeConfig, want: filepath.Join(home, "config")},
		{name: "data", dt: dirTypeData, want: filepath.Join(home, "data")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveUserDirPath(tt.dt)
			if err != nil {
				t.Fatalf("resolveUserDirPath(%q) returned error: %v", tt.dt, err)
			}

			if got != tt.want {
				t.Errorf("resolveUserDirPath(%q) = %q, want %q", tt.dt, got, tt.want)
			}
		})
	}
}

func TestResolveUserDirPath_NoOverrideUsesOSPath(t *testing.T) {
	t.Setenv(envHomeOverride, "")

	got, err := resolveUserDirPath(dirTypeConfig)
	if err != nil {
		t.Fatalf("resolveUserDirPath returned error: %v", err)
	}

	// Without the override the path must fall back to the OS-specific
	// yellowjacket location, not a bare "<home>/config" base dir.
	if !filepath.IsAbs(got) || !strings.HasSuffix(got, "yellowjacket") {
		t.Errorf(
			"resolveUserDirPath fallback = %q, want absolute path ending in %q",
			got, "yellowjacket",
		)
	}
}

// UseHomeOverride carries two rules that a mobile launch depends on and
// that nothing else would notice breaking: an empty base must do
// nothing, because that is precisely what StoragePath() returns on
// desktop, and an override already set must win, or YJ_HOME would stop
// relocating a sandbox on the platform that decides for itself.
func TestUseHomeOverride(t *testing.T) {
	const (
		storage = "/data/user/0/app.yellowjacket/files"
		sandbox = "/tmp/sandbox"
	)

	tests := []struct {
		name    string
		already string
		base    string
		want    string
	}{
		{name: "empty base is a no-op", already: "", base: "", want: ""},
		{name: "sets the override when unset", already: "", base: storage, want: storage},
		{name: "an existing override wins", already: sandbox, base: storage, want: sandbox},
		{name: "empty base keeps an existing override", already: sandbox, base: "", want: sandbox},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(envHomeOverride, tt.already)

			UseHomeOverride(tt.base)

			if got := os.Getenv(envHomeOverride); got != tt.want {
				t.Errorf("%s = %q, want %q", envHomeOverride, got, tt.want)
			}
		})
	}
}

// UseTempDir carries UseHomeOverride's two rules for the same reasons,
// plus one of its own: the directory it names has to be usable.
//
// **The only tier that can compile the platform this exists for is a
// phone**, so everything decidable off one is decided here -- which is
// androidpayload.go's discipline, and is why the platform call is a
// parameter rather than something this package reaches for. The
// device's half is a single measurement: no /tmp, no TMPDIR (#190).
func TestUseTempDir(t *testing.T) {
	t.Run("an empty base is a no-op", func(t *testing.T) {
		// This is the desktop case in full: StoragePath() answers ""
		// off mobile, where /tmp is real and must be left alone.
		t.Setenv(envTempDir, "")

		if err := UseTempDir(""); err != nil {
			t.Fatalf("UseTempDir(\"\") = %v, want nil", err)
		}

		if got := os.Getenv(envTempDir); got != "" {
			t.Errorf("%s = %q, want it untouched", envTempDir, got)
		}
	})

	t.Run("an explicit TMPDIR wins", func(t *testing.T) {
		const chosen = "/somewhere/deliberate"

		// The base is taken before TMPDIR moves, because t.TempDir()
		// reads TMPDIR too -- which is the same fact this function is
		// about, met from the other side.
		base := t.TempDir()

		t.Setenv(envTempDir, chosen)

		if err := UseTempDir(base); err != nil {
			t.Fatalf("UseTempDir = %v, want nil", err)
		}

		if got := os.Getenv(envTempDir); got != chosen {
			t.Errorf("%s = %q, want the explicit %q", envTempDir, got, chosen)
		}
	})

	t.Run("points at a real directory under the base", func(t *testing.T) {
		base := t.TempDir()

		t.Setenv(envTempDir, "")

		if err := UseTempDir(base); err != nil {
			t.Fatalf("UseTempDir = %v, want nil", err)
		}

		got := os.Getenv(envTempDir)

		want := filepath.Join(base, tempDirName)
		if got != want {
			t.Fatalf("%s = %q, want %q", envTempDir, got, want)
		}

		// The whole failure being repaired is a temp directory that is
		// named and does not exist, so naming one is not enough.
		info, err := os.Stat(got)
		if err != nil {
			t.Fatalf("the temp directory was named but not created: %v", err)
		}

		if !info.IsDir() {
			t.Fatalf("%s is not a directory", got)
		}
	})

	t.Run("os.TempDir then answers with it", func(t *testing.T) {
		// The point of setting the variable at all: this is what every
		// library in the process reads, SQLite's driver included.
		base := t.TempDir()

		t.Setenv(envTempDir, "")

		if err := UseTempDir(base); err != nil {
			t.Fatalf("UseTempDir = %v, want nil", err)
		}

		if got := os.TempDir(); got != filepath.Join(base, tempDirName) {
			t.Errorf("os.TempDir() = %q, want the directory we made", got)
		}
	})

	t.Run("an unwritable directory is an error, not a silent success", func(t *testing.T) {
		if os.Getuid() == 0 {
			t.Skip("root can write anywhere, so there is nothing to refuse")
		}

		base := t.TempDir()

		// MkdirAll on an existing directory succeeds whatever its
		// mode, so without the write probe this case would set TMPDIR
		// to a directory nothing can use -- which is the bug again,
		// one directory over.
		if err := os.Mkdir(filepath.Join(base, tempDirName), 0o500); err != nil {
			t.Fatalf("prepare the unwritable directory: %v", err)
		}

		t.Setenv(envTempDir, "")

		if err := UseTempDir(base); err == nil {
			t.Fatal("UseTempDir accepted a directory it cannot write to")
		}

		if got := os.Getenv(envTempDir); got != "" {
			t.Errorf("%s was set to %q despite the failure", envTempDir, got)
		}
	})
}
