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
