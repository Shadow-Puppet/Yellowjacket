package coverart_test

import (
	"path/filepath"
	"strings"
	"testing"

	"yellowjacket/backend/coverart"
)

func TestCoversDir(t *testing.T) {
	t.Parallel()

	dir, err := coverart.CoversDir()
	if err != nil {
		t.Fatalf("CoversDir() returned error: %v", err)
	}

	if dir == "" {
		t.Fatal("CoversDir() returned empty string")
	}

	// The path must end with the "covers" directory name.
	if filepath.Base(dir) != "covers" {
		t.Errorf(
			"CoversDir() = %q, want path ending in %q",
			dir, "covers",
		)
	}

	// Must be an absolute path.
	if !filepath.IsAbs(dir) {
		t.Errorf("CoversDir() = %q, want absolute path", dir)
	}

	// Must contain the app name somewhere in the path.
	if !strings.Contains(dir, "yellowjacket") {
		t.Errorf(
			"CoversDir() = %q, expected to contain %q",
			dir, "yellowjacket",
		)
	}
}

func TestSizedFilename(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		filename string
		suffix   string
		want     string
	}{
		{
			name:     "jpg with _sm suffix",
			filename: "a1b2c3d4.jpg",
			suffix:   "_sm",
			want:     "a1b2c3d4_sm.jpg",
		},
		{
			name:     "jpg with _md suffix",
			filename: "a1b2c3d4.jpg",
			suffix:   "_md",
			want:     "a1b2c3d4_md.jpg",
		},
		{
			name:     "jpg with _lg suffix",
			filename: "a1b2c3d4.jpg",
			suffix:   "_lg",
			want:     "a1b2c3d4_lg.jpg",
		},
		{
			name:     "png source outputs jpg",
			filename: "abcdef01.png",
			suffix:   "_sm",
			want:     "abcdef01_sm.jpg",
		},
		{
			name:     "no extension",
			filename: "abcdef01",
			suffix:   "_md",
			want:     "abcdef01_md.jpg",
		},
		{
			name:     "empty suffix",
			filename: "a1b2c3d4.jpg",
			suffix:   "",
			want:     "a1b2c3d4.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := coverart.SizedFilename(tt.filename, tt.suffix)
			if got != tt.want {
				t.Errorf(
					"SizedFilename(%q, %q) = %q, want %q",
					tt.filename, tt.suffix, got, tt.want,
				)
			}
		})
	}
}

func TestResolveURLs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		wantOrig string
		wantSm   string
		wantMd   string
		wantLg   string
	}{
		{
			name:     "absolute path",
			path:     "/home/user/.local/share/yellowjacket/covers/a1b2c3d4.jpg",
			wantOrig: "/covers/a1b2c3d4.jpg",
			wantSm:   "/covers/a1b2c3d4_sm.jpg",
			wantMd:   "/covers/a1b2c3d4_md.jpg",
			wantLg:   "/covers/a1b2c3d4_lg.jpg",
		},
		{
			name:     "bare filename",
			path:     "abcdef01.png",
			wantOrig: "/covers/abcdef01.png",
			wantSm:   "/covers/abcdef01_sm.jpg",
			wantMd:   "/covers/abcdef01_md.jpg",
			wantLg:   "/covers/abcdef01_lg.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			urls := coverart.ResolveURLs(tt.path)

			if urls.Original != tt.wantOrig {
				t.Errorf(
					"Original = %q, want %q",
					urls.Original, tt.wantOrig,
				)
			}

			if urls.Small != tt.wantSm {
				t.Errorf(
					"Small = %q, want %q",
					urls.Small, tt.wantSm,
				)
			}

			if urls.Medium != tt.wantMd {
				t.Errorf(
					"Medium = %q, want %q",
					urls.Medium, tt.wantMd,
				)
			}

			if urls.Large != tt.wantLg {
				t.Errorf(
					"Large = %q, want %q",
					urls.Large, tt.wantLg,
				)
			}
		})
	}
}
