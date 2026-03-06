//go:build linux

package mediacontrols

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

const (
	busName    = "org.mpris.MediaPlayer2.yellowjacket"
	objectPath = "/org/mpris/MediaPlayer2"
	playerIf   = "org.mpris.MediaPlayer2.Player"
	rootIf     = "org.mpris.MediaPlayer2"

	usPerSec = 1_000_000

	// updateChanSize is the buffer size for the async update
	// channel. A small buffer avoids blocking callers while the
	// D-Bus goroutine processes updates.
	updateChanSize = 64
)

var errNotPrimaryOwner = errors.New(
	"failed to become primary owner of bus name",
)

// mprisRoot handles the org.mpris.MediaPlayer2 interface methods.
type mprisRoot struct{}

// Raise is a no-op; YellowJacket does not support raising via MPRIS.
func (r *mprisRoot) Raise() *dbus.Error { return nil }

// Quit is a no-op; shutdown is managed by the Wails lifecycle.
func (r *mprisRoot) Quit() *dbus.Error { return nil }

// mprisPlayer handles the org.mpris.MediaPlayer2.Player
// interface methods. Every D-Bus method callback dispatches to a
// goroutine so that the godbus handler goroutine returns
// immediately and never blocks on player/queue mutexes.
type mprisPlayer struct {
	callbacks Callbacks
}

// Play requests playback start/resume.
func (p *mprisPlayer) Play() *dbus.Error {
	if p.callbacks.OnPlay != nil {
		go p.callbacks.OnPlay()
	}

	return nil
}

// Pause requests playback pause.
func (p *mprisPlayer) Pause() *dbus.Error {
	if p.callbacks.OnPause != nil {
		go p.callbacks.OnPause()
	}

	return nil
}

// PlayPause toggles between play and pause.
func (p *mprisPlayer) PlayPause() *dbus.Error {
	if p.callbacks.OnPlayPause != nil {
		go p.callbacks.OnPlayPause()
	}

	return nil
}

// Stop requests playback stop.
func (p *mprisPlayer) Stop() *dbus.Error {
	if p.callbacks.OnStop != nil {
		go p.callbacks.OnStop()
	}

	return nil
}

// Next requests skipping to the next track.
func (p *mprisPlayer) Next() *dbus.Error {
	if p.callbacks.OnNext != nil {
		go p.callbacks.OnNext()
	}

	return nil
}

// Previous requests skipping to the previous track.
func (p *mprisPlayer) Previous() *dbus.Error {
	if p.callbacks.OnPrevious != nil {
		go p.callbacks.OnPrevious()
	}

	return nil
}

// SeekTo requests a relative seek by offset microseconds.
// Exported on D-Bus as "Seek" via ExportWithMap; renamed in Go
// to avoid a false positive from go vet's stdmethods checker.
func (p *mprisPlayer) SeekTo(offsetUs int64) *dbus.Error {
	if p.callbacks.OnSeek != nil {
		secs := int(offsetUs / usPerSec)

		go p.callbacks.OnSeek(secs)
	}

	return nil
}

// SetPosition requests an absolute seek to positionUs on the
// given track.
func (p *mprisPlayer) SetPosition(
	_ dbus.ObjectPath,
	positionUs int64,
) *dbus.Error {
	if p.callbacks.OnSeek != nil {
		secs := int(positionUs / usPerSec)

		go p.callbacks.OnSeek(secs)
	}

	return nil
}

// OpenUri is required by the MPRIS2 spec but not supported.
//
//nolint:revive // D-Bus requires this exact method name.
func (p *mprisPlayer) OpenUri(_ string) *dbus.Error {
	return nil
}

// MPRISHandler is the Linux MPRIS2 implementation of Handler.
//
// All public update methods (UpdateMetadata, UpdatePlaybackState,
// NotifySeek, UpdateVolume) send work to a buffered channel that a
// dedicated goroutine drains. This avoids calling into godbus
// (which acquires props.mut and does D-Bus I/O) while the caller
// holds the player mutex, preventing a deadlock between p.mu and
// props.mut.
type MPRISHandler struct {
	logger  *slog.Logger
	conn    *dbus.Conn
	props   *prop.Properties
	player  *mprisPlayer
	updates chan func()
	done    chan struct{}
	mu      sync.Mutex
	trackID uint64
}

// NewHandler creates a new MPRIS2 handler.
func NewHandler(logger *slog.Logger) Handler {
	return &MPRISHandler{
		logger: logger.WithGroup("mpris"),
	}
}

// Init connects to the D-Bus session bus, exports the MPRIS2
// interfaces, and registers the well-known bus name.
func (h *MPRISHandler) Init(callbacks Callbacks) error {
	conn, err := dbus.SessionBus()
	if err != nil {
		return fmt.Errorf(
			"failed to connect to session bus: %w", err,
		)
	}

	h.conn = conn
	h.player = &mprisPlayer{callbacks: callbacks}
	h.updates = make(chan func(), updateChanSize)
	h.done = make(chan struct{})

	go h.processUpdates()

	// Export properties for both interfaces.
	h.props, err = prop.Export(
		conn,
		objectPath,
		h.propertySpec(),
	)
	if err != nil {
		return fmt.Errorf(
			"failed to export properties: %w", err,
		)
	}

	// Export method handlers.
	root := &mprisRoot{}

	if err := conn.Export(
		root, objectPath, rootIf,
	); err != nil {
		return fmt.Errorf(
			"failed to export root interface: %w", err,
		)
	}

	if err := conn.ExportWithMap(
		h.player,
		map[string]string{"SeekTo": "Seek"},
		objectPath,
		playerIf,
	); err != nil {
		return fmt.Errorf(
			"failed to export player interface: %w", err,
		)
	}

	// Export introspection.
	if err := conn.Export(
		introspect.NewIntrospectable(h.introspectNode()),
		objectPath,
		"org.freedesktop.DBus.Introspectable",
	); err != nil {
		return fmt.Errorf(
			"failed to export introspection: %w", err,
		)
	}

	// Claim the well-known bus name.
	reply, err := conn.RequestName(
		busName, dbus.NameFlagReplaceExisting,
	)
	if err != nil {
		return fmt.Errorf(
			"failed to request bus name: %w", err,
		)
	}

	if reply != dbus.RequestNameReplyPrimaryOwner {
		return fmt.Errorf(
			"%w: %s (reply=%d)",
			errNotPrimaryOwner, busName, reply,
		)
	}

	h.logger.Info(
		"MPRIS2 registered on D-Bus", "name", busName,
	)

	return nil
}

// processUpdates drains the update channel on a dedicated
// goroutine. All props.SetMust and conn.Emit calls happen here,
// safely away from the player's mutex.
func (h *MPRISHandler) processUpdates() {
	for fn := range h.updates {
		fn()
	}

	close(h.done)
}

// enqueue sends a function to the update goroutine. If the
// channel is full the update is dropped to avoid blocking the
// caller (this is acceptable — the next update will overwrite
// stale state).
func (h *MPRISHandler) enqueue(fn func()) {
	select {
	case h.updates <- fn:
	default:
		h.logger.Debug("MPRIS update channel full, dropping")
	}
}

// UpdateMetadata pushes track metadata to D-Bus.
func (h *MPRISHandler) UpdateMetadata(meta Metadata) {
	h.mu.Lock()
	h.trackID++
	tid := h.trackID
	h.mu.Unlock()

	m := map[string]interface{}{
		"mpris:trackid": dbus.ObjectPath(
			fmt.Sprintf(
				"/org/yellowjacket/Track/%d", tid,
			),
		),
	}

	if meta.Title != "" {
		m["xesam:title"] = meta.Title
	}

	if meta.Artist != "" {
		m["xesam:artist"] = []string{meta.Artist}
	}

	if meta.Album != "" {
		m["xesam:album"] = meta.Album
	}

	if meta.ArtFilePath != "" {
		m["mpris:artUrl"] = "file://" + meta.ArtFilePath
	}

	if meta.DurationSec > 0 {
		m["mpris:length"] = int64(
			meta.DurationSec,
		) * usPerSec
	}

	h.enqueue(func() {
		h.props.SetMust(playerIf, "Metadata", m)
	})
}

// UpdatePlaybackState pushes the playback state and position
// anchor.
func (h *MPRISHandler) UpdatePlaybackState(
	state PlaybackState,
	positionSec int,
) {
	var status string

	switch state {
	case StatePlaying:
		status = "Playing"
	case StatePaused:
		status = "Paused"
	default:
		status = "Stopped"
	}

	posUs := int64(positionSec) * usPerSec

	h.enqueue(func() {
		// Update Position silently (EmitFalse) then
		// PlaybackStatus loudly (EmitTrue). The DE
		// re-anchors on the status change.
		h.props.SetMust(playerIf, "Position", posUs)
		h.props.SetMust(
			playerIf, "PlaybackStatus", status,
		)
	})
}

// NotifySeek emits the MPRIS Seeked signal.
func (h *MPRISHandler) NotifySeek(positionSec int) {
	posUs := int64(positionSec) * usPerSec

	h.enqueue(func() {
		h.props.SetMust(playerIf, "Position", posUs)

		if err := h.conn.Emit(
			objectPath,
			playerIf+".Seeked",
			posUs,
		); err != nil {
			h.logger.Error(
				"Failed to emit Seeked signal",
				"err", err,
			)
		}
	})
}

// UpdateVolume pushes the current volume (0.0-1.0) to D-Bus.
func (h *MPRISHandler) UpdateVolume(volume float64) {
	h.enqueue(func() {
		h.props.SetMust(playerIf, "Volume", volume)
	})
}

// Close signals the update goroutine to stop, waits for it to
// drain, and closes the D-Bus connection.
func (h *MPRISHandler) Close() {
	if h.updates != nil {
		close(h.updates)
		<-h.done
	}

	if h.conn != nil {
		if err := h.conn.Close(); err != nil {
			h.logger.Error(
				"Failed to close D-Bus connection",
				"err", err,
			)
		}

		h.logger.Info("MPRIS2 D-Bus connection closed")
	}
}

// onVolumeChanged is called when an external D-Bus client sets
// the Volume property. The callback runs under props.mut (held by
// godbus), so we dispatch to a goroutine to avoid acquiring p.mu
// under props.mut — which would invert the lock order with the
// update goroutine's SetMust calls.
func (h *MPRISHandler) onVolumeChanged(
	c *prop.Change,
) *dbus.Error {
	vol, ok := c.Value.(float64)
	if !ok {
		return nil
	}

	if h.player.callbacks.OnVolume != nil {
		go h.player.callbacks.OnVolume(vol)
	}

	return nil
}

// onLoopStatusChanged is called when an external D-Bus client
// sets the LoopStatus property.
func (h *MPRISHandler) onLoopStatusChanged(
	_ *prop.Change,
) *dbus.Error {
	// LoopStatus changes via D-Bus are acknowledged but not
	// actively wired to the queue's CycleRepeat. The queue
	// cycles through modes and MPRIS reflects the result.
	return nil
}

// onShuffleChanged is called when an external D-Bus client sets
// the Shuffle property.
func (h *MPRISHandler) onShuffleChanged(
	_ *prop.Change,
) *dbus.Error {
	// Shuffle changes via D-Bus are acknowledged but not
	// actively wired to the queue's ToggleShuffle. The queue
	// toggles and MPRIS reflects the result.
	return nil
}

// propertySpec builds the full property map for both MPRIS
// interfaces.
func (h *MPRISHandler) propertySpec() map[string]map[string]*prop.Prop {
	noTrack := map[string]interface{}{
		"mpris:trackid": dbus.ObjectPath(
			"/org/mpris/MediaPlayer2/TrackList/NoTrack",
		),
	}

	return map[string]map[string]*prop.Prop{
		rootIf: {
			"CanQuit":      newReadOnlyProp(false),
			"CanRaise":     newReadOnlyProp(false),
			"HasTrackList": newReadOnlyProp(false),
			"Identity":     newReadOnlyProp("YellowJacket"),
			"DesktopEntry": newReadOnlyProp(
				"yellowjacket",
			),
			"SupportedUriSchemes": newReadOnlyProp(
				[]string{},
			),
			"SupportedMimeTypes": newReadOnlyProp(
				[]string{},
			),
		},
		playerIf: {
			"PlaybackStatus": newReadOnlyProp("Stopped"),
			"LoopStatus": {
				Value:    "None",
				Writable: true,
				Emit:     prop.EmitTrue,
				Callback: h.onLoopStatusChanged,
			},
			"Rate":        newReadOnlyProp(1.0),
			"MinimumRate": newReadOnlyProp(1.0),
			"MaximumRate": newReadOnlyProp(1.0),
			"Shuffle": {
				Value:    false,
				Writable: true,
				Emit:     prop.EmitTrue,
				Callback: h.onShuffleChanged,
			},
			"Metadata": newReadOnlyProp(noTrack),
			"Volume": {
				Value:    1.0,
				Writable: true,
				Emit:     prop.EmitTrue,
				Callback: h.onVolumeChanged,
			},
			"Position": {
				Value:    int64(0),
				Writable: false,
				Emit:     prop.EmitFalse,
			},
			"CanGoNext":     newReadOnlyProp(true),
			"CanGoPrevious": newReadOnlyProp(true),
			"CanPlay":       newReadOnlyProp(true),
			"CanPause":      newReadOnlyProp(true),
			"CanSeek":       newReadOnlyProp(true),
			"CanControl":    newReadOnlyProp(true),
		},
	}
}

// newReadOnlyProp creates a read-only property with EmitTrue.
// Read-only here means external D-Bus clients cannot set it via
// the Properties.Set interface; the server updates it internally
// via SetMust.
func newReadOnlyProp(value interface{}) *prop.Prop {
	return &prop.Prop{
		Value:    value,
		Writable: false,
		Emit:     prop.EmitTrue,
	}
}

// introspectNode builds the introspection data for the MPRIS
// object.
func (h *MPRISHandler) introspectNode() *introspect.Node {
	return &introspect.Node{
		Name: busName,
		Interfaces: []introspect.Interface{
			introspect.IntrospectData,
			{
				Name: rootIf,
				Properties: introspectProps(
					roProp("CanQuit", "b"),
					roProp("CanRaise", "b"),
					roProp("HasTrackList", "b"),
					roProp("Identity", "s"),
					roProp("DesktopEntry", "s"),
					roProp(
						"SupportedUriSchemes", "as",
					),
					roProp(
						"SupportedMimeTypes", "as",
					),
				),
				Methods: []introspect.Method{
					{Name: "Raise"},
					{Name: "Quit"},
				},
			},
			{
				Name: playerIf,
				Properties: introspectProps(
					roProp("PlaybackStatus", "s"),
					rwProp("LoopStatus", "s"),
					rwProp("Rate", "d"),
					rwProp("Shuffle", "b"),
					roProp("Metadata", "a{sv}"),
					rwProp("Volume", "d"),
					roProp("Position", "x"),
					roProp("MinimumRate", "d"),
					roProp("MaximumRate", "d"),
					roProp("CanGoNext", "b"),
					roProp("CanGoPrevious", "b"),
					roProp("CanPlay", "b"),
					roProp("CanPause", "b"),
					roProp("CanSeek", "b"),
					roProp("CanControl", "b"),
				),
				Signals: []introspect.Signal{
					{
						Name: "Seeked",
						Args: []introspect.Arg{
							{
								Name: "Position",
								Type: "x",
							},
						},
					},
				},
				Methods: []introspect.Method{
					{Name: "Next"},
					{Name: "Previous"},
					{Name: "Pause"},
					{Name: "PlayPause"},
					{Name: "Stop"},
					{Name: "Play"},
					{
						Name: "Seek",
						Args: []introspect.Arg{
							{
								Name:      "Offset",
								Type:      "x",
								Direction: "in",
							},
						},
					},
					{
						Name: "SetPosition",
						Args: []introspect.Arg{
							{
								Name:      "TrackId",
								Type:      "o",
								Direction: "in",
							},
							{
								Name:      "Position",
								Type:      "x",
								Direction: "in",
							},
						},
					},
					{
						Name: "OpenUri",
						Args: []introspect.Arg{
							{
								Name:      "Uri",
								Type:      "s",
								Direction: "in",
							},
						},
					},
				},
			},
		},
	}
}

func roProp(name, typ string) introspect.Property {
	return introspect.Property{
		Name:   name,
		Type:   typ,
		Access: "read",
	}
}

func rwProp(name, typ string) introspect.Property {
	return introspect.Property{
		Name:   name,
		Type:   typ,
		Access: "readwrite",
	}
}

func introspectProps(
	props ...introspect.Property,
) []introspect.Property {
	return props
}
