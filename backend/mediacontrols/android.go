//go:build android

// Android's answer to MPRIS is a MediaSession, and reaching it needs no
// new JNI: Wails exports application.Android.StartForegroundService(json)
// going out, and Java's WailsBridge.emitEvent lands on the application
// event bus coming back. So this handler is one JSON payload pushed to
// the foreground service and one command event read from it. The Java
// half is
// build/android/app/src/main/java/com/wails/app/WailsForegroundService.java
// and the payload keys below are its contract.

package mediacontrols

import (
	"errors"
	"log/slog"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// commandEvent is the event name the Java side emits transport
// commands on. It is a plain string on both sides; changing it means
// changing WailsForegroundService too.
const commandEvent = "yj:media:command"

var errNoApplication = errors.New(
	"no running application to attach media controls to",
)

// androidHandler drives the media notification, the lock-screen
// transport and audio focus through the foreground service.
type androidHandler struct {
	logger *slog.Logger

	mu          sync.Mutex
	callbacks   Callbacks
	meta        Metadata
	state       PlaybackState
	positionSec int

	// running tracks whether the foreground service has been started.
	// Android 12+ forbids starting one from the background, so it is
	// started when playback starts -- a user action, in a visible app
	// -- and stopped only when playback stops, which is what keeps
	// queue auto-advance working with the screen off.
	running bool

	// lastPayload is the last JSON sent. An unchanged payload is not
	// an event here either: every push crosses JNI and re-delivers an
	// Intent, and the player pushes state on several paths that can
	// agree.
	lastPayload string

	unsubscribe func()
}

// NewHandler returns the Android media-session handler.
func NewHandler(logger *slog.Logger) Handler {
	return &androidHandler{logger: logger, state: StateStopped}
}

// Init subscribes to the transport commands the Java side emits.
func (a *androidHandler) Init(callbacks Callbacks) error {
	app := application.Get()
	if app == nil {
		return errNoApplication
	}

	a.mu.Lock()
	a.callbacks = callbacks
	a.mu.Unlock()

	a.unsubscribe = app.Event.On(commandEvent, a.onCommand)

	return nil
}

// onCommand dispatches one transport command from the notification,
// the lock screen, a headset button or an audio-focus change.
//
// Every callback runs on its own goroutine, for the reason the MPRIS
// handler does the same: they take the player and queue mutexes, and
// this runs on the event processor's dispatch goroutine.
func (a *androidHandler) onCommand(event *application.CustomEvent) {
	data, ok := event.Data.(map[string]any)
	if !ok {
		return
	}

	command := parseMediaCommand(data)

	a.mu.Lock()
	cb := a.callbacks
	a.mu.Unlock()

	switch command.name {
	case cmdPlay:
		run(cb.OnPlay)
	case cmdPause:
		run(cb.OnPause)
	case cmdPlayPause:
		run(cb.OnPlayPause)
	case cmdStop:
		run(cb.OnStop)
	case cmdNext:
		run(cb.OnNext)
	case cmdPrevious:
		run(cb.OnPrevious)
	case cmdSeek:
		if cb.OnSeek != nil {
			go cb.OnSeek(command.positionSec)
		}
	case cmdDuck:
		if cb.OnDuck != nil {
			go cb.OnDuck(command.duck)
		}
	default:
		a.logger.Warn("Unknown media command", "command", command.name)
	}
}

// run invokes a callback on its own goroutine, tolerating a nil one.
func run(fn func()) {
	if fn != nil {
		go fn()
	}
}

// UpdateMetadata pushes new track details to the notification.
func (a *androidHandler) UpdateMetadata(meta Metadata) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.meta = meta
	a.push()
}

// UpdatePlaybackState pushes the state and a fresh position anchor;
// the MediaSession interpolates from there while playing.
func (a *androidHandler) UpdatePlaybackState(
	state PlaybackState,
	positionSec int,
) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.state = state
	a.positionSec = positionSec
	a.push()
}

// NotifySeek re-anchors the position. Unlike MPRIS, a MediaSession has
// no separate seeked signal -- a new state with a new position is the
// whole mechanism.
func (a *androidHandler) NotifySeek(positionSec int) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.positionSec = positionSec
	a.push()
}

// UpdateVolume is deliberately a no-op. Android's volume keys act on
// the media stream, which the OS owns; an app that also moved its own
// volume in response would move it twice.
func (a *androidHandler) UpdateVolume(_ float64) {}

// Close stops the service and drops the command subscription.
func (a *androidHandler) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.unsubscribe != nil {
		a.unsubscribe()
		a.unsubscribe = nil
	}

	if a.running {
		application.Android.StopForegroundService()
		a.running = false
	}
}

// push sends the current state to the Java side, if it has changed.
// The caller holds a.mu.
func (a *androidHandler) push() {
	if a.state == StateStopped {
		// Nothing is playing, so nothing justifies an ongoing
		// notification or the process staying alive.
		if a.running {
			application.Android.StopForegroundService()
			a.running = false
			a.lastPayload = ""
		}

		return
	}

	payload, err := mediaPayload(a.meta, a.state, a.positionSec)
	if err != nil {
		a.logger.Error("Failed to encode media payload", "err", err)

		return
	}

	if payload == a.lastPayload {
		return
	}

	a.lastPayload = payload
	a.running = true

	application.Android.StartForegroundService(payload)
}
