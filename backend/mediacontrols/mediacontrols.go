// Package mediacontrols provides OS media control integration.
//
// On Linux this registers a MPRIS2 D-Bus service so that desktop
// environments, playerctl, and media keys can control playback and
// see the currently playing track. Other platforms get a no-op stub.
package mediacontrols

// PlaybackState represents the current playback state for the OS.
type PlaybackState int

// Playback state values.
const (
	StateStopped PlaybackState = iota
	StatePlaying
	StatePaused
)

// Metadata holds track information to display in the OS media overlay.
type Metadata struct {
	Title       string
	Artist      string
	Album       string
	ArtFilePath string // Absolute filesystem path to cover art.
	DurationSec int
}

// Callbacks are invoked when the OS sends media commands.
type Callbacks struct {
	OnPlay      func()
	OnPause     func()
	OnPlayPause func()
	OnStop      func()
	OnNext      func()
	OnPrevious  func()
	OnSeek      func(positionSec int)
	OnVolume    func(volume float64) // 0.0–1.0 linear scale.
}

// Handler manages the OS media control integration.
type Handler interface {
	// Init registers with the OS and wires incoming commands to
	// the provided callbacks. It must be called once during startup.
	Init(callbacks Callbacks) error

	// UpdateMetadata pushes new track metadata to the OS overlay.
	UpdateMetadata(meta Metadata)

	// UpdatePlaybackState pushes the playback state and current
	// position. The position is used as a new anchor; the OS
	// interpolates from there while playing.
	UpdatePlaybackState(state PlaybackState, positionSec int)

	// NotifySeek signals that the user seeked to a new position.
	// This is separate from UpdatePlaybackState because MPRIS
	// emits a distinct Seeked signal for this.
	NotifySeek(positionSec int)

	// UpdateVolume pushes the current volume (0.0–1.0) to the OS.
	UpdateVolume(volume float64)

	// Close tears down the OS registration and releases resources.
	Close()
}
