//go:build !linux

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
