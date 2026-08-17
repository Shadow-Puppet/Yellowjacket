// The contract between the Android handler and the Java
// WailsForegroundService is two JSON documents -- one pushed out with
// the track and the state, one read back with a transport command --
// and neither side can check the other.
//
// It lives here, *without* the android build tag, so that `go test` on
// any platform exercises it. android.go itself can only be compiled by
// a cross-compiler and only be run by a phone, so anything left in it
// is untested by construction; this is the half worth not leaving
// there.

package mediacontrols

import "encoding/json"

// Media command names, as the Java side spells them.
const (
	cmdPlay      = "play"
	cmdPause     = "pause"
	cmdPlayPause = "playpause"
	cmdStop      = "stop"
	cmdNext      = "next"
	cmdPrevious  = "previous"
	cmdSeek      = "seek"
	cmdDuck      = "duck"
)

// stateNames are what the payload's "state" key carries. Words rather
// than the PlaybackState integers, because the Java side reads them as
// JSON and a renumbered constant would silently mean something else
// there.
var stateNames = map[PlaybackState]string{
	StateStopped: "stopped",
	StatePlaying: "playing",
	StatePaused:  "paused",
}

// mediaCommand is one transport command from the notification, the
// lock screen, a headset button or an audio-focus change.
type mediaCommand struct {
	name        string
	positionSec int
	duck        bool
}

// mediaPayload encodes the state the notification and MediaSession
// render.
func mediaPayload(
	meta Metadata,
	state PlaybackState,
	positionSec int,
) (string, error) {
	payload, err := json.Marshal(map[string]any{
		"title":       meta.Title,
		"artist":      meta.Artist,
		"album":       meta.Album,
		"artPath":     meta.ArtFilePath,
		"durationSec": meta.DurationSec,
		"positionSec": positionSec,
		"state":       stateNames[state],
	})
	if err != nil {
		return "", err
	}

	return string(payload), nil
}

// parseMediaCommand reads one command out of the event payload.
//
// The numbers arrive as float64 because they came through
// encoding/json as an untyped document -- asserting int here is the
// way a seek silently becomes a seek to zero.
func parseMediaCommand(data map[string]any) mediaCommand {
	cmd := mediaCommand{}
	cmd.name, _ = data["command"].(string)

	if position, ok := data["positionSec"].(float64); ok {
		cmd.positionSec = int(position)
	}

	cmd.duck, _ = data["on"].(bool)

	return cmd
}
