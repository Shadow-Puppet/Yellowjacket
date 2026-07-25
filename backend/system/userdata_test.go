package system

import (
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
