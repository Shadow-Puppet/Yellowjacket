//go:build !linux || android

// Android is covered here rather than by mpris_linux.go: it satisfies
// the `linux` tag but has no D-Bus session bus. Its real equivalent is
// a MediaSession, which is Java-side work and not yet built -- so for
// now the app simply has no lock-screen transport there, which is a
// missing feature rather than a broken one.

package mediacontrols

import "log/slog"

// stubHandler is a no-op Handler for platforms without media control
// integration.
type stubHandler struct{}

// NewHandler returns a no-op handler on unsupported platforms.
func NewHandler(_ *slog.Logger) Handler {
	return &stubHandler{}
}

func (s *stubHandler) Init(_ Callbacks) error { return nil }

func (s *stubHandler) UpdateMetadata(_ Metadata) {}

func (s *stubHandler) UpdatePlaybackState(
	_ PlaybackState,
	_ int,
) {
}

func (s *stubHandler) NotifySeek(_ int) {}

func (s *stubHandler) UpdateVolume(_ float64) {}

func (s *stubHandler) Close() {}
