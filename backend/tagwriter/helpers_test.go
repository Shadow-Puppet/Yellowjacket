package tagwriter

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"log/slog"
	"os"
	"testing"
)

// testLogger returns a quiet logger suitable for tests.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
}

// tinyJPEG returns a small, valid JPEG image for cover art tests.
func tinyJPEG(t *testing.T) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode tiny jpeg: %v", err)
	}

	return buf.Bytes()
}

// makeMinimalJPEG is an alias for tinyJPEG used by mp3_test.go.
func makeMinimalJPEG(t *testing.T) []byte {
	t.Helper()

	return tinyJPEG(t)
}

// assertEqual is a generic test helper for comparing values.
func assertEqual[T comparable](t *testing.T, field string, want, got T) {
	t.Helper()

	if got != want {
		t.Errorf("%s: got %v, want %v", field, got, want)
	}
}

func assertStrField(t *testing.T, name, got, want string) {
	t.Helper()

	if got != want {
		t.Errorf("%s: got %q, want %q", name, got, want)
	}
}

func assertIntField(t *testing.T, name string, got, want int) {
	t.Helper()

	if got != want {
		t.Errorf("%s: got %d, want %d", name, got, want)
	}
}
