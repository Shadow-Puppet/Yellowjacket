// Package player provides audio playback functionality.
package player

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/effects"
	"github.com/gopxl/beep/v2/generators"
	"github.com/gopxl/beep/v2/speaker"
	"github.com/wailsapp/wails/v3/pkg/application"

	"yellowjacket/backend/coverart"
	"yellowjacket/backend/database"
	"yellowjacket/backend/database/sql/sqlcgen"
	"yellowjacket/backend/events"
	"yellowjacket/backend/mediacontrols"
	"yellowjacket/backend/metadata"
	"yellowjacket/backend/profiling"
)

// Player handles audio playback and state management.
//
// Lock ordering: always acquire p.mu BEFORE speaker.Lock().
// The beep playback-finished callback dispatches to a new goroutine
// so it never holds p.mu while the speaker lock is held.
type Player struct {
	// mu protects all mutable fields below from concurrent access.
	// It must be held by every public method and released before
	// calling the playbackFinishedHandler (which re-enters the player
	// via the queue).
	mu sync.Mutex

	ctx          context.Context
	logger       *slog.Logger
	db           *database.DB
	state        State
	currentFile  *os.File
	format       beep.Format
	baseStreamer beep.Streamer
	seeker       beep.StreamSeeker
	resampled    beep.Streamer
	buffered     *BufferedStreamer

	// lastUnderruns is the previous report, so the 1 Hz log can say
	// what happened in the last second and stay quiet when nothing did.
	// It is reset with the streamer, in loadFileLocked.
	lastUnderruns           UnderrunStats
	control                 *beep.Ctrl
	volume                  *effects.Volume
	speakerStreamer         beep.Streamer
	playbackFinishedHandler func(error)
	trackChangeID           uint64

	// chainID identifies the streamer chain currently registered with
	// the speaker.  updateStreamers bumps it, and the finished
	// callback carries the value it was registered with, so a callback
	// that queued for p.mu behind a LoadFile can tell that the player
	// has moved on and return rather than rewinding somebody else's
	// track.
	chainID       uint64
	mediaControls mediacontrols.Handler

	// duckAmount is the attenuation currently applied on top of the
	// user's volume, in the same base-2 exponent effects.Volume uses.
	// It is deliberately not persisted and emits no VolumeChanged: a
	// duck is something the OS did for the length of a notification,
	// not something the user chose.
	duckAmount float64

	// systemVolume is what SystemOwnsVolume answers: the platform's own
	// control is the only one, so ours neither acts nor persists.  It is
	// a field rather than the build constant read directly so that a
	// test can exercise both sides on any machine.  See systemvolume.go.
	systemVolume bool

	// storedVolume and storedMuted hold the persisted level as it was
	// found at restore, for a platform whose volume we do not own: the
	// maximum we then run at is not a level the user chose, so saveState
	// writes back what it read rather than overwriting it.
	storedVolume UserVolume
	storedMuted  bool

	// trackLengthMs holds the authoritative track duration in
	// milliseconds, sourced from the database (which uses the
	// custom header parser).  The go-mp3 decoder's Len() can be
	// inflated for files with multiple ID3v2 tags, so this value
	// is preferred for display and position calculations.
	trackLengthMs int64

	// positionTickerOnce guards the 1 Hz position ticker so repeated
	// SetContext calls (tests, re-init) cannot start a second one.
	positionTickerOnce sync.Once

	// positionSeq increments on every emitted position, so a
	// consumer can tell "the same second, again" from "a fresh
	// reading" and reset its interpolation on both.
	positionSeq uint64

	// persistCh carries state writes to the single goroutine that runs
	// them, so no path holds mu while waiting on SQLite's writer
	// connection.  See persistwriter.go.
	persistOnce sync.Once
	persistCh   chan func()
}

// PositionInfo is the payload of the PlaybackPositionChanged event:
// the player's own answer to "where are we", which the seek bar
// renders instead of counting.
type PositionInfo struct {
	PositionSeconds int    `json:"positionSeconds"`
	TrackLength     int    `json:"trackLength"`
	TrackChangeID   uint64 `json:"trackChangeId"`
	Seq             uint64 `json:"seq"`
	Playing         bool   `json:"playing"`
}

// State represents the current playback state.
type State string

// Playback state values.
const (
	Playing State = "playing"
	Paused  State = "paused"
	Stopped State = "stopped"
)

// TrackInfo contains metadata and playback state for the currently
// loaded track. It is emitted as the payload of the TrackChanged
// event and serialized as camelCase JSON to match the frontend
// TrackInfo interface in player-store.ts.
type TrackInfo struct {
	FileName         string `json:"fileName"`
	FilePath         string `json:"filePath"`
	State            State  `json:"state"`
	Title            string `json:"title"`
	Artist           string `json:"artist"`
	Album            string `json:"album"`
	CoverArt         string `json:"coverArt"`
	CoverArtSmall    string `json:"coverArtSmall"`
	CoverArtMedium   string `json:"coverArtMedium"`
	CoverArtLarge    string `json:"coverArtLarge"`
	TrackLength      int    `json:"trackLength"`
	SeekPosition     int    `json:"seekPosition"`
	TrackChangeID    uint64 `json:"trackChangeId"`
	ArtistMBID       string `json:"artistMbid"`
	ReleaseGroupMBID string `json:"releaseGroupMbid"`
	RecordingMBID    string `json:"recordingMbid"`
}

// Sentinel errors for player operations.
var (
	errNoControlStreamer = errors.New("no control streamer")
	errNoAudioFileLoaded = errors.New("no audio file loaded")
	errNoStreamerToPlay  = errors.New("no streamer to play")
	errNoAudioStream     = errors.New("no audio stream to pause")
	errSeekPanicked      = errors.New("seek panicked (go-mp3 bug)")
)

var speakerSampleRate = beep.SampleRate(44100)

// NewPlayer creates a player. Call InitSpeaker separately to
// initialize the audio output device.
func NewPlayer(logger *slog.Logger, db *database.DB) *Player {
	return &Player{
		logger:       logger,
		db:           db,
		state:        Stopped,
		systemVolume: platformOwnsVolume,
		storedVolume: DefaultUserVol,
		baseStreamer: generators.Silence(-1),
		format: beep.Format{
			SampleRate: speakerSampleRate,
		},
	}
}

// InitSpeaker initializes the audio output device. This is
// separated from NewPlayer so the player struct can be created
// before wails.Run (for binding registration) while deferring
// hardware initialization to OnStartup.
func (p *Player) InitSpeaker() error {
	defer profiling.TimeOp(p.logger, "player.InitSpeaker")()

	// TODO: allow user to change buffer size and speaker sample rate.
	// Speaker buffer is 200ms (~8820 samples at 44100 Hz), providing
	// secondary protection against underruns behind the read-ahead
	// BufferedStreamer.
	err := speaker.Init(
		p.format.SampleRate,
		p.format.SampleRate.N(time.Second/5),
	)
	if err != nil {
		return fmt.Errorf(
			"failed to initialize speaker: %w", err,
		)
	}

	return nil
}

// SetPlaybackFinishedHandler sets a callback invoked when a track
// stops streaming. This allows the queue to drive auto-advance
// without circular imports.
//
// The error says *why* the track stopped: nil for a track that
// reached its end, non-nil for one that broke partway through.  Both
// arrive here because both look identical to the speaker, and only
// the queue holds the metadata a PlaybackFailed needs -- but they are
// not the same event, and reporting a decode failure as a natural
// finish is how a broken file used to auto-advance in silence.
//
//wails:ignore // internal wiring, not part of the app's IPC surface.
func (p *Player) SetPlaybackFinishedHandler(handler func(error)) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.playbackFinishedHandler = handler
}

// SetMediaControls provides an OS media controls handler. When set,
// the player pushes metadata, playback state, volume, and seek
// notifications to the OS media overlay.
//
//wails:ignore // internal wiring, not part of the app's IPC surface.
func (p *Player) SetMediaControls(h mediacontrols.Handler) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.mediaControls = h
}

// ServiceStartup is v3's service lifecycle hook: it runs once the
// runtime exists, and ctx is cancelled when the app shuts down.  It
// replaces v2's SetContext, which had to be called by hand from
// OnStartup and was exported, so it was also bound to the frontend.
func (p *Player) ServiceStartup(
	ctx context.Context,
	_ application.ServiceOptions,
) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.ctx = ctx
	p.restoreStateLocked()
	p.startPositionTicker()

	return nil
}

// positionTickInterval is how often the backend reports its own
// playback position while playing.
const positionTickInterval = time.Second

// startPositionTicker runs the 1 Hz position report for the life of
// the Wails context.  Must be called with p.mu held.
func (p *Player) startPositionTicker() {
	if p.ctx == nil {
		return
	}

	ctx := p.ctx

	p.positionTickerOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(positionTickInterval)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					p.emitPositionIfPlaying()
				}
			}
		}()
	})
}

// emitPositionIfPlaying reports the position only while audio is
// actually moving; a paused or stopped player has already emitted its
// final position at the transition.
func (p *Player) emitPositionIfPlaying() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.state != Playing || p.currentFile == nil {
		return
	}

	p.reportUnderrunsLocked()
	p.emitPositionLocked()
}

// underrunDelta is what happened since the last report.
//
// It clamps at zero rather than subtracting blind, because the counter
// belongs to the *streamer* and the streamer is replaced on every
// track: a baseline carried across that boundary is the previous
// track's total subtracted from a fresh zero, which is negative. That
// is repaired at the load (lastUnderruns is reset with the streamer)
// and clamped here as well, because a negative count in a log line
// reads as a broken instrument and would discredit the measurement
// this exists to make.
func underrunDelta(now, last UnderrunStats) UnderrunStats {
	return UnderrunStats{
		Runs:    max(0, now.Runs-last.Runs),
		Calls:   max(0, now.Calls-last.Calls),
		Samples: max(0, now.Samples-last.Samples),
	}
}

// reportUnderrunsLocked logs what the ring buffer missed, at most once
// a second and only when the number moved.  Must be called with p.mu
// held.
//
// **An underrun is audible and nothing counted it** (#135). The ring
// serves silence when it is empty, so a run of zeros is spliced into
// the waveform and the step discontinuity at each edge is a click; a
// series of short ones is static. Everything that makes one likelier
// is worse on a phone than on a desktop -- slower storage, a governor
// that parks cores, background work, GC -- and no tier here can see it,
// since CI's audio device is a null sink chosen because it keeps time.
//
// Three things about the reporting are deliberate.
//
// **It is on the 1 Hz position ticker rather than in Stream.** Stream
// runs on the speaker callback's real-time deadline, and a log line
// there would allocate, format and write on the exact path whose
// missed deadline is the defect -- measuring by making it worse.
//
// **An unchanged count is not logged.** That is emitStatus' rule one
// package over: a healthy player is silent, so anything in the log is
// news, and the line appears exactly while it is popping. Reading it
// off a device means `make android-logs` with the audio audible.
//
// **It is Info rather than Debug**, because the default level is Info
// and a phone has no convenient way to set YJ_LOG_LEVEL -- a debug
// line here would be a counter nobody on the affected platform can
// read, which is the shape of the bug that made #160 necessary.
func (p *Player) reportUnderrunsLocked() {
	if p.buffered == nil {
		return
	}

	stats := p.buffered.Underruns()
	if stats == p.lastUnderruns {
		return
	}

	since := underrunDelta(stats, p.lastUnderruns)
	p.lastUnderruns = stats

	slog.Info("audio underrun",
		"runs", since.Runs,
		"calls", since.Calls,
		"silenceMs", speakerSampleRate.D(int(since.Samples)).Milliseconds(),
		"trackRuns", stats.Runs,
		"trackSilenceMs", speakerSampleRate.D(int(stats.Samples)).Milliseconds(),
	)
}

// emitPositionLocked pushes the current position to the frontend.
// Must be called with p.mu held.
func (p *Player) emitPositionLocked() {
	if p.ctx == nil {
		return
	}

	length, err := p.trackLengthLocked()
	if err != nil {
		length = 0
	}

	p.positionSeq++

	events.Emit(p.ctx, events.PlaybackPositionChanged, PositionInfo{
		PositionSeconds: p.displayPositionSecsLocked(),
		TrackLength:     length,
		TrackChangeID:   p.trackChangeID,
		Seq:             p.positionSeq,
		Playing:         p.state == Playing,
	})
}

// ---------------------------------------------------------------
// Emit helpers (must be called with p.mu held)
// ---------------------------------------------------------------

// emitPlaybackStateChanged emits a playback state change event.
func (p *Player) emitPlaybackStateChanged(state State) {
	if p.ctx == nil {
		p.logger.Error("Context is nil, cannot emit event")

		return
	}

	p.logger.Info(
		"Emitting PlaybackStateChangedEvent", "state", state,
	)

	events.Emit(
		p.ctx,
		events.PlaybackStateChanged,
		map[string]string{"state": string(state)},
	)

	if p.mediaControls != nil {
		p.mediaControls.UpdatePlaybackState(
			stateToMediaControls(state),
			p.currentPositionSecondsLocked(),
		)
	}
}

func (p *Player) emitPlaybackFinished() {
	if p.ctx == nil {
		p.logger.Error("Context is nil, cannot emit event")

		return
	}

	p.logger.Info("Emitting PlaybackFinishedEvent")
	events.Emit(p.ctx, events.PlaybackFinished, nil)
}

func (p *Player) emitVolumeChanged() {
	if p.ctx == nil {
		p.logger.Error("Context is nil, cannot emit event")

		return
	}

	volume := int(p.getUserVolume())
	muted := p.volume != nil && p.volume.Silent
	p.logger.Info(
		"Emitting VolumeChangedEvent", "volume", volume, "muted", muted,
	)

	events.Emit(p.ctx, events.VolumeChanged, volume)

	// Mute rides on its own event rather than widening the volume
	// payload: silence does not change the volume level, so a UI that
	// only watched VolumeChanged saw nothing happen when the user hit
	// the mute key.
	events.Emit(p.ctx, events.MuteChanged, muted)

	if p.mediaControls != nil {
		// MPRIS volume is 0.0–1.0 linear.
		p.mediaControls.UpdateVolume(
			float64(volume) / float64(MaxUserVol),
		)
	}
}

func (p *Player) emitTrackChanged() {
	if p.ctx == nil {
		p.logger.Error("Context is nil, cannot emit event")

		return
	}

	trackInfo := p.getCurrentTrackInfoLocked()

	trackLengthSecs, err := p.trackLengthLocked()
	if err != nil {
		p.logger.Error("Cannot get track length")
	}

	trackInfo.TrackLength = trackLengthSecs

	// Compute current seek position in display seconds.
	trackInfo.SeekPosition = p.displayPositionSecsLocked()

	// Increment track change ID so the frontend can detect changes
	// even when the same file plays consecutively.
	p.trackChangeID++
	trackInfo.TrackChangeID = p.trackChangeID

	events.Emit(
		p.ctx, events.TrackChanged, trackInfo,
	)

	p.logger.Info(
		"Emitting TrackChangedEvent with track info",
		"trackInfo", trackInfo,
	)

	if p.mediaControls != nil {
		p.mediaControls.UpdateMetadata(
			p.buildMediaMetadata(
				trackInfo, trackLengthSecs,
			),
		)
	}
}

// EmitCurrentState pushes the current player state to the frontend.
// This is intended to be called after the frontend is ready to
// receive events, separately from RestoreState which does the heavy
// lifting during OnStartup.
func (p *Player) EmitCurrentState() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.emitVolumeChanged()

	if p.currentFile != nil {
		p.emitPlaybackStateChanged(p.state)
		p.emitTrackChanged()
	}
}

// ---------------------------------------------------------------
// Streamer management (must be called with p.mu held)
// ---------------------------------------------------------------

func (p *Player) updateStreamers(
	newBaseStreamer beep.StreamSeeker,
	sr beep.SampleRate,
) error {
	// A new chain supersedes the old one, so any finished callback the
	// old one still owes is stale from here on.
	p.chainID++

	// The previous read-ahead goroutine reads the same decoder this
	// one is about to, under its own srcMu -- two goroutines, two
	// mutexes, one decoder that is not safe for concurrent use.  The
	// replay-after-finish path rebuilds from p.seeker without going
	// through LoadFile, which is where that pair could meet.
	if p.buffered != nil {
		p.buffered.Close()
	}

	// set base streamer
	p.baseStreamer = newBaseStreamer
	p.seeker = newBaseStreamer

	// resample file stream to match speaker
	// TODO: variable resample quality
	p.resampled = beep.Resample(
		4, sr, speakerSampleRate, p.baseStreamer,
	)

	// Buffer resampled audio to decouple disk I/O from speaker
	// timing. 2 seconds of read-ahead at speaker sample rate
	// absorbs I/O stalls and GC pauses without audible glitches.
	p.buffered = NewBufferedStreamer(
		p.resampled, int(speakerSampleRate)*2,
	)

	// The counter belongs to the streamer, so the baseline it is
	// reported against has to go with it -- otherwise the first report
	// of a new track is the previous track's total subtracted from
	// zero, which is negative and looks like the instrument is broken.
	p.lastUnderruns = UnderrunStats{}

	// wrap in ctrl streamer to allow play/pause
	p.control = &beep.Ctrl{Streamer: p.buffered}

	// Preserve existing volume settings across track changes.
	prevVolume := 0.0
	prevSilent := false

	if p.volume != nil {
		prevVolume = p.volume.Volume
		prevSilent = p.volume.Silent
	}

	// wrap in volume streamer
	p.volume = &effects.Volume{
		Streamer: p.control,
		Base:     2,
		Volume:   prevVolume,
		Silent:   prevSilent,
	}

	// set "final" streamer
	p.speakerStreamer = p.volume

	return nil
}

// startPaused registers the current streamer chain with the speaker
// in a paused state. Must be called with p.mu held.
func (p *Player) startPaused() {
	speaker.Lock()
	p.control.Paused = true
	speaker.Unlock()

	// Captured, not read at callback time: by then p.chainID names
	// whatever is loaded *now*, which is the thing the guard exists to
	// distinguish this chain from.
	chainID := p.chainID
	buffered := p.buffered

	// The beep.Callback runs with the speaker mutex held, so we
	// dispatch to a goroutine that can safely acquire p.mu.
	speaker.Play(beep.Seq(
		p.speakerStreamer,
		beep.Callback(func() {
			// Asked here rather than under p.mu: this is the chain that
			// just ended, and by the time the goroutine holds the lock
			// p.buffered may be a different one.
			var err error
			if buffered != nil {
				err = buffered.Err()
			}

			go p.onPlaybackFinished(chainID, err)
		}),
	))

	p.state = Paused
}

// onPlaybackFinished handles a track that stopped streaming, whether
// it ended or broke. It is called on a new goroutine from the beep
// callback (which holds the speaker lock) so that it can safely
// acquire p.mu.
//
// chainID names the streamer chain the callback fired for and srcErr
// says why it stopped.
func (p *Player) onPlaybackFinished(chainID uint64, srcErr error) {
	p.mu.Lock()

	// The player has moved on while this callback queued for the lock
	// -- a user pressing Next during the last second of a track is
	// enough. Everything below is about the *current* track: rewinding
	// the decoder, saying playback stopped, asking the queue to
	// advance. Doing any of it now would do it to the wrong track.
	if chainID != p.chainID {
		p.mu.Unlock()
		p.logger.Debug(
			"Ignoring finished callback for a superseded chain",
			"chain", chainID, "current", p.chainID,
		)

		return
	}

	p.state = Stopped
	handler := p.playbackFinishedHandler
	mc := p.mediaControls

	// Rewind so the position the UI is told is the truth: a finished
	// track sits at 0:00, ready to play again, rather than reporting
	// its own length forever.  Play() rebuilds the streamer chain from
	// the Stopped state anyway, so this only moves the decoder.
	p.rewindLocked()
	p.emitPositionLocked()

	// Emitted under p.mu, like every other transition in this file.
	// Outside it, a Play() taking the lock in the gap emits `playing`
	// first and this stale `stopped` lands last -- leaving the button
	// showing play over a track that is audibly running.
	p.emitPlaybackFinished()

	events.Emit(
		p.ctx,
		events.PlaybackStateChanged,
		map[string]string{"state": string(Stopped)},
	)

	p.mu.Unlock()

	// Notify media controls outside the lock. The track just
	// ended so position is 0.
	if mc != nil {
		mc.UpdatePlaybackState(
			mediacontrols.StateStopped, 0,
		)
	}

	if srcErr != nil {
		p.logger.Error(
			"Playback stopped: the audio source failed",
			"err", srcErr,
		)
	} else {
		p.logger.Info("Playback finished naturally")
	}

	// Notify queue for auto-advance. Called without p.mu held
	// because it re-enters the player via LoadFile/Play.
	if handler != nil {
		handler(srcErr)
	}
}

// ---------------------------------------------------------------
// LoadFile
// ---------------------------------------------------------------

// LoadFile opens and decodes an audio file for playback.
func (p *Player) LoadFile(filePath string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.loadFileLocked(filePath)
}

func (p *Player) loadFileLocked(filePath string) error {
	defer profiling.TimeOp(p.logger, "player.LoadFile")()

	f, err := os.Open(filePath)
	if err != nil {
		p.logger.Error("Failed to open file")

		return fmt.Errorf("failed to open file: %w", err)
	}

	streamer, format, err := metadata.DecodeFile(f)
	if err != nil {
		p.logger.Error(
			"failed to decode audio file",
			"path", filePath, "err", err,
		)

		return fmt.Errorf("failed to decode audio file: %w", err)
	}

	// Stop existing playback before loading new file.
	speaker.Lock()
	if p.control != nil {
		p.control.Paused = true
	}

	p.state = Stopped
	speaker.Unlock()

	// Stop the read-ahead goroutine for the previous track.
	if p.buffered != nil {
		p.buffered.Close()
	}

	if p.currentFile != nil {
		if closeErr := p.currentFile.Close(); closeErr != nil {
			p.logger.Warn(
				"failed to close previous audio file",
				"err", closeErr,
			)
		}
	}

	p.currentFile = f

	// The decoder's own format, kept for the paths that rebuild the
	// chain later: Play()'s replay branch resamples from it, so a
	// stale rate there plays a finished track back at the wrong speed.
	p.format = format

	// The previous track's duration must not outlive it.  This is set
	// again by emitTrackChanged below, but only when the database has
	// a row for the file -- and every position this player reports is
	// scaled by it, so inheriting means every report is wrong by the
	// ratio between two unrelated tracks.
	p.trackLengthMs = 0

	if err := p.updateStreamers(
		streamer, format.SampleRate,
	); err != nil {
		return fmt.Errorf("failed to update streamers: %w", err)
	}

	p.startPaused()
	p.emitPlaybackStateChanged(p.state)
	p.emitTrackChanged()
	p.emitPositionLocked()
	p.saveState()
	p.logger.Info(
		"File loaded, state set to paused", "file", filePath,
	)

	return nil
}

// ---------------------------------------------------------------
// Play / Pause
// ---------------------------------------------------------------

func (p *Player) validateReadyToPlay() error {
	if p.control == nil {
		return errNoControlStreamer
	}

	if p.currentFile == nil {
		return errNoAudioFileLoaded
	}

	if p.speakerStreamer == nil {
		return errNoStreamerToPlay
	}

	return nil
}

// Play starts or resumes audio playback.
func (p *Player) Play() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.validateReadyToPlay(); err != nil {
		return err
	}

	if p.state == Playing {
		p.logger.Info("Already playing")

		return nil
	}

	// Track finished naturally — seek to the beginning and
	// re-register a paused stream with the speaker so the unpause
	// below starts it.
	if p.state == Stopped && p.seeker != nil {
		speaker.Lock()
		err := p.seeker.Seek(0)
		speaker.Unlock()

		if err != nil {
			return fmt.Errorf(
				"failed to seek to beginning: %w", err,
			)
		}

		if err := p.updateStreamers(
			p.seeker, p.format.SampleRate,
		); err != nil {
			return fmt.Errorf(
				"failed to update streamers for replay: %w", err,
			)
		}

		p.startPaused()
		p.logger.Info("Rebuilt streamers for replay")
	}

	// Unpause — works for both resume-from-pause and
	// replay-from-stopped.
	speaker.Lock()
	p.control.Paused = false
	speaker.Unlock()

	p.state = Playing
	p.emitPlaybackStateChanged(p.state)
	p.emitPositionLocked()
	p.logger.Info("Started playback")

	return nil
}

// Pause pauses the current playback.
func (p *Player) Pause() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.control == nil {
		return errNoAudioStream
	}

	if p.state == Paused {
		p.logger.Info("Already paused")

		return nil
	}

	if p.state == Playing {
		speaker.Lock()
		p.control.Paused = true
		speaker.Unlock()

		p.state = Paused
		p.logger.Info("Paused playback")
		p.emitPlaybackStateChanged(p.state)
		p.emitPositionLocked()
		p.saveState()
	} else {
		p.logger.Info("Already paused or not playing")
	}

	return nil
}

// IsPlaying reports whether the player is currently playing audio.
func (p *Player) IsPlaying() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.state == Playing
}

// ---------------------------------------------------------------
// UnloadTrack
// ---------------------------------------------------------------

// UnloadTrack tears down the current track, releasing the file and
// streamer chain. The player returns to the initial "no track
// loaded" state and emits events so the frontend clears its
// current-track display.
func (p *Player) UnloadTrack() {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Stop audio output.
	if p.control != nil {
		speaker.Lock()
		p.control.Paused = true
		speaker.Unlock()
	}

	// Close the open audio file.
	if p.currentFile != nil {
		if err := p.currentFile.Close(); err != nil {
			p.logger.Warn(
				"Failed to close audio file during unload",
				"err", err,
			)
		}

		p.currentFile = nil
	}

	// Stop the read-ahead goroutine before releasing the chain.
	if p.buffered != nil {
		p.buffered.Close()
	}

	// Release streamer chain. Volume is intentionally kept so the
	// user's volume setting persists across tracks.
	p.baseStreamer = nil
	p.seeker = nil
	p.resampled = nil
	p.buffered = nil
	p.control = nil
	p.speakerStreamer = nil
	p.trackLengthMs = 0

	p.state = Stopped

	// Notify frontend that there is no longer a current track.
	p.emitPlaybackStateChanged(p.state)
	events.Emit(p.ctx, events.TrackChanged, nil)

	if p.mediaControls != nil {
		p.mediaControls.UpdateMetadata(mediacontrols.Metadata{})
	}

	p.saveState()

	p.logger.Info("Track unloaded")
}

// ---------------------------------------------------------------
// Volume
// ---------------------------------------------------------------

// SetVolume sets the playback volume (0-100), emits a
// VolumeChanged event, and persists the new level.
func (p *Player) SetVolume(desiredVolume UserVolume) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.systemVolume {
		return
	}

	p.setVolumeLocked(desiredVolume)
	p.emitVolumeChanged()
	p.saveState()
}

func (p *Player) setVolumeLocked(desiredVolume UserVolume) {
	speaker.Lock()

	volume := clampVolume(desiredVolume)
	p.volume.Volume = float64(volume.ToVolume()) - p.duckAmount
	p.volume.Silent = volume == MinUserVol

	speaker.Unlock()
}

// SetDuck attenuates playback (or restores it) without changing the
// user's volume, for an OS that has asked us to get out of the way of
// something short -- a navigation prompt, a notification tone.
//
// It re-applies the *user's* level through setVolumeLocked rather than
// nudging the effect directly, so the offset cannot accumulate across
// repeated ducks, and it neither emits nor persists: the level the user
// set has not changed and the UI must not claim it has.
//
//wails:ignore // driven by OS audio focus, not by the frontend.
func (p *Player) SetDuck(ducked bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	amount := 0.0
	if ducked {
		amount = duckAttenuation
	}

	if p.volume == nil || amount == p.duckAmount {
		return
	}

	current := p.getUserVolume()
	p.duckAmount = amount
	p.setVolumeLocked(current)
}

// ChangeVolume adjusts the volume by a relative amount.
func (p *Player) ChangeVolume(deltaVolume int) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.systemVolume {
		return nil
	}

	p.setVolumeLocked(p.getUserVolume() + UserVolume(deltaVolume))
	p.emitVolumeChanged()
	p.saveState()

	return nil
}

func (p *Player) getUserVolume() UserVolume {
	// Undo any duck, so every caller -- the event, the persisted
	// state, a relative change -- sees the level the user chose.
	return Volume(p.volume.Volume + p.duckAmount).ToUserVolume()
}

// Muted reports whether playback is currently silenced.
func (p *Player) Muted() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.volume != nil && p.volume.Silent
}

// MuteToggle toggles the mute state.
func (p *Player) MuteToggle() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.volume == nil {
		return errNoAudioFileLoaded
	}

	// Mute is a level of zero by another name, so it goes with the rest
	// of the volume where the system owns it -- and it would be the one
	// state on such a platform the user could not get out of, since with
	// no control rendered there is nothing left to un-mute with.
	if p.systemVolume {
		return nil
	}

	speaker.Lock()
	p.volume.Silent = !p.volume.Silent
	speaker.Unlock()

	p.emitVolumeChanged()
	p.saveState()

	return nil
}

// ---------------------------------------------------------------
// Position / Seek
// ---------------------------------------------------------------

// CurrentPositionSeconds returns the current playback position in
// display seconds.
func (p *Player) CurrentPositionSeconds() (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.seeker == nil {
		return 0, errNoAudioFileLoaded
	}

	return p.displayPositionSecsLocked(), nil
}

// CurrentPosition returns the playback position as a percentage
// (0-100).
func (p *Player) CurrentPosition() (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.seeker == nil {
		return 0, errNoAudioFileLoaded
	}

	defer p.lockSourceLocked()()

	speaker.Lock()
	pos := math.Round(
		100.0 * float64(p.seeker.Position()) /
			float64(p.seeker.Len()),
	)
	speaker.Unlock()

	return int(pos), nil
}

// Seek jumps to a specific position in seconds.
func (p *Player) Seek(targetSeconds int) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.seekLocked(targetSeconds)
}

// lockSourceLocked blocks the read-ahead goroutine from touching the
// decoder and returns the function that releases it, so a caller can
// `defer p.lockSourceLocked()()`.
//
// Reading the decoder's position is a read *of the decoder*, and the
// speaker lock does not exclude the read-ahead goroutine -- it never
// takes it.  That was a genuine data race on every position emit,
// once a second for the whole of playback.
//
// srcMu is not reentrant, so nothing that already holds it may call
// this; seekSourceLocked exists to keep that region free of emits.
// Must be called with p.mu held.
func (p *Player) lockSourceLocked() func() {
	if p.buffered == nil {
		return func() {}
	}

	p.buffered.LockSource()

	return p.buffered.UnlockSource
}

// rewindLocked returns the decoder to the start of the track without
// touching playback state.  Must be called with p.mu held.
func (p *Player) rewindLocked() {
	if p.seeker == nil {
		return
	}

	// Same source lock the seek path takes: the read-ahead goroutine
	// must not be inside Read while the decoder seeks.
	if p.buffered != nil {
		p.buffered.LockSource()
		defer p.buffered.UnlockSource()
	}

	speaker.Lock()
	err := p.seeker.Seek(0)
	speaker.Unlock()

	if err != nil {
		p.logger.Warn("Failed to rewind finished track", "err", err)
	}
}

func (p *Player) seekLocked(targetSeconds int) error {
	if p.seeker == nil {
		events.Emit(p.ctx, events.SeekFailed)

		return errNoAudioFileLoaded
	}

	lengthSecs, err := p.trackLengthLocked()
	if err != nil {
		return fmt.Errorf("cannot get track length: %w", err)
	}

	// The source lock is released before anything below is emitted:
	// emitPositionLocked reads the decoder's position and takes the
	// same lock, which is not reentrant.
	seekErr := p.seekSourceLocked(targetSeconds, lengthSecs)
	if seekErr != nil {
		p.logger.Warn(
			"Seek failed, playback will start from "+
				"the beginning",
			"target-seconds", targetSeconds,
			"err", seekErr,
		)

		// The optimistic move the UI already made has to be taken
		// back, and only the backend knows it did not happen.
		events.Emit(p.ctx, events.SeekFailed)
		p.emitPositionLocked()

		return fmt.Errorf("failed to seek: %w", seekErr)
	}

	if p.mediaControls != nil {
		p.mediaControls.NotifySeek(targetSeconds)
	}

	// Report the landing position immediately rather than leaving the
	// UI to guess until the next tick — this is the half of H-3 that
	// desynced the seek bar by 30 s over four keyboard seeks.
	p.emitPositionLocked()

	return nil
}

// seekSourceLocked moves the decoder and flushes the stale read-ahead
// behind it.  It owns the source lock for exactly that long and
// emits nothing, so its caller is free to read the position
// afterwards.  Must be called with p.mu held.
func (p *Player) seekSourceLocked(
	targetSeconds int,
	lengthSecs int,
) error {
	// Block the read-ahead goroutine from reading the source while
	// we seek it. The decoder (e.g. FLAC's bufseekio.ReadSeeker) is
	// not safe for concurrent Read+Seek, and read-ahead runs on its
	// own goroutine — without this it can panic with a slice-bounds
	// error, especially right after load when the buffer is empty
	// and read-ahead is filling at full speed.
	if p.buffered != nil {
		p.buffered.LockSource()
		defer p.buffered.UnlockSource()
	}

	speaker.Lock()

	samples := int(
		math.Round(
			(float64(targetSeconds) / float64(lengthSecs)) *
				float64(p.seeker.Len()),
		),
	)

	// Clamp the seek position to valid bounds. The underlying
	// go-mp3 library (v0.3.4) has a bug where seeking to
	// positions near the end of certain files causes a slice
	// bounds panic. Clamping reduces the likelihood of hitting
	// this, and the recover below catches it if it still occurs.
	if maxPos := p.seeker.Len() - 1; samples > maxPos {
		samples = maxPos
	}

	if samples < 0 {
		samples = 0
	}

	p.logger.Debug(
		"attempting to seek",
		"target-seconds", targetSeconds,
		"song-length", lengthSecs,
		"samples", samples,
	)

	// Wrap the seek in a recover to catch panics from the
	// go-mp3 library's buggy Seek implementation. See:
	// github.com/hajimehoshi/go-mp3@v0.3.4/decode.go:111
	seekErr := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("%w: %v", errSeekPanicked, r)
			}
		}()

		return p.seeker.Seek(samples)
	}()
	if seekErr != nil {
		speaker.Unlock()

		p.logger.Debug(
			"seek rejected by the decoder",
			"samples", samples, "err", seekErr,
		)

		return fmt.Errorf("failed to seek: %w", seekErr)
	}

	speaker.Unlock()

	// Flush the read-ahead buffer so the speaker immediately
	// plays audio from the new position instead of draining
	// up to 2 seconds of stale pre-seek samples.
	if p.buffered != nil {
		p.buffered.Flush()
	}

	return nil
}

// ---------------------------------------------------------------
// Track info
// ---------------------------------------------------------------

// GetCurrentTrackInfo returns information about the currently
// loaded track.
func (p *Player) GetCurrentTrackInfo() TrackInfo {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.getCurrentTrackInfoLocked()
}

func (p *Player) getCurrentTrackInfoLocked() TrackInfo {
	info := TrackInfo{
		State: p.state,
	}

	if p.currentFile == nil {
		return info
	}

	info.FileName = filepath.Base(p.currentFile.Name())
	info.FilePath = p.currentFile.Name()
	info.Title = info.FileName // default title is the filename

	// Try to get metadata from database.
	if p.db != nil {
		meta, err := p.db.ReadQueries.GetTrackByPath(
			p.ctx, info.FilePath,
		)
		if err == nil {
			if meta.Title != "" {
				info.Title = meta.Title
			}

			info.Artist = meta.ArtistName
			info.Album = meta.Album
			info.ArtistMBID = meta.ArtistMbid
			info.ReleaseGroupMBID = meta.ReleaseGroupMbid
			info.RecordingMBID = meta.RecordingMbid
			p.trackLengthMs = meta.LengthMilliseconds

			if meta.CoverArtPath != "" {
				urls := coverart.ResolveURLs(meta.CoverArtPath)
				info.CoverArt = urls.Original
				info.CoverArtSmall = urls.Small
				info.CoverArtMedium = urls.Medium
				info.CoverArtLarge = urls.Large
			}
		} else {
			p.logger.Debug(
				"Could not get track metadata from database",
				"path", info.FilePath, "err", err,
			)
		}
	}

	return info
}

// TrackLengthInSeconds returns the duration of the current track.
func (p *Player) TrackLengthInSeconds() (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.trackLengthLocked()
}

func (p *Player) trackLengthLocked() (int, error) {
	// Prefer the database duration — the custom header parser
	// handles multiple ID3v2 tags correctly, whereas go-mp3's
	// Len() can be inflated by phantom frames.
	if p.trackLengthMs > 0 {
		return int(p.trackLengthMs / 1000), nil
	}

	return p.seekerLengthSecsLocked()
}

// seekerLengthSecsLocked returns the track length in seconds as
// reported by the beep decoder.  This may differ from the
// database duration for MP3 files with multiple ID3v2 tags.
// It is used internally for seek sample calculations.
func (p *Player) seekerLengthSecsLocked() (int, error) {
	if p.seeker == nil {
		return 0, errNoAudioFileLoaded
	}

	// Len is fixed for the life of the decoder, so unlike Position it
	// races with nothing and needs no source lock -- which it must not
	// take anyway: displayPositionSecsLocked calls this while holding
	// it, and srcMu is not reentrant.
	speaker.Lock()
	length := p.seeker.Len() / int(p.format.SampleRate)
	speaker.Unlock()

	return length, nil
}

// displayPositionSecsLocked converts the current seeker position to
// display seconds.  When the DB duration is available, the position
// is scaled from the (potentially inflated) seeker time scale to the
// correct display time scale.  Must be called with p.mu held.
func (p *Player) displayPositionSecsLocked() int {
	if p.seeker == nil {
		return 0
	}

	defer p.lockSourceLocked()()

	speaker.Lock()
	pos := p.seeker.Position()
	total := p.seeker.Len()
	speaker.Unlock()

	if total == 0 {
		return 0
	}

	displayLength, err := p.trackLengthLocked()
	if err != nil {
		return pos / int(p.format.SampleRate)
	}

	return int(
		math.Round(
			float64(pos) / float64(total) *
				float64(displayLength),
		),
	)
}

// ---------------------------------------------------------------
// Media controls helpers
// ---------------------------------------------------------------

// stateToMediaControls maps the player's State type to the
// mediacontrols PlaybackState.
func stateToMediaControls(s State) mediacontrols.PlaybackState {
	switch s {
	case Playing:
		return mediacontrols.StatePlaying
	case Paused:
		return mediacontrols.StatePaused
	default:
		return mediacontrols.StateStopped
	}
}

// currentPositionSecondsLocked returns the playback position in
// display seconds.  Must be called with p.mu held.
func (p *Player) currentPositionSecondsLocked() int {
	return p.displayPositionSecsLocked()
}

// buildMediaMetadata constructs a mediacontrols.Metadata from a
// TrackInfo and duration. It resolves the cover art filesystem path
// from the database for use by MPRIS (which needs file:// URIs).
// Must be called with p.mu held.
func (p *Player) buildMediaMetadata(
	info TrackInfo,
	durationSec int,
) mediacontrols.Metadata {
	meta := mediacontrols.Metadata{
		Title:       info.Title,
		Artist:      info.Artist,
		Album:       info.Album,
		DurationSec: durationSec,
	}

	// Resolve cover art filesystem path. The database stores the
	// full path; ResolveURLs converts it to relative HTTP paths
	// for the frontend, but MPRIS needs the actual file path.
	if p.db != nil && info.FilePath != "" {
		dbMeta, err := p.db.ReadQueries.GetTrackByPath(
			p.ctx, info.FilePath,
		)
		if err == nil && dbMeta.CoverArtPath != "" {
			meta.ArtFilePath = dbMeta.CoverArtPath
		}
	}

	return meta
}

// ---------------------------------------------------------------
// State persistence
// ---------------------------------------------------------------

// SaveState persists the current player state to the database and
// waits for the write.  This is called during shutdown to capture the
// final state, which is the one case that cannot be deferred.
func (p *Player) SaveState() {
	p.mu.Lock()
	p.saveState()
	p.mu.Unlock()

	p.flushWrites()
}

// saveState snapshots the current player state and hands it to the
// persistence goroutine.  Must be called with p.mu held.
//
// It does not write here: every caller holds p.mu (LoadFile, Play,
// Pause, Seek, the volume paths), the write goes through SQLite's
// single writer connection, and a background pass can hold that for
// seconds — which is how loading a track came to block the whole
// player behind a durability write. See persistwriter.go.
func (p *Player) saveState() {
	if p.db == nil {
		p.logger.Warn(
			"No database available, cannot save player state",
		)

		return
	}

	volume := int64(DefaultUserVol)
	muted := false

	switch {
	case p.systemVolume:
		// The maximum this platform runs at is not a level anybody
		// chose, so it is not one to remember.  Writing back what
		// restore found keeps the row a description of the user's
		// setting without needing a second query that omits the column.
		volume = int64(p.storedVolume)
		muted = p.storedMuted
	case p.volume != nil:
		volume = int64(p.getUserVolume())
		muted = p.volume.Silent
	}

	trackPath := ""
	if p.currentFile != nil {
		trackPath = p.currentFile.Name()
	}

	positionSeconds := int64(p.displayPositionSecsLocked())

	params := sqlcgen.UpdatePlayerStateParams{
		Volume:              volume,
		Muted:               muted,
		LastTrackPath:       trackPath,
		LastPositionSeconds: positionSeconds,
	}

	p.submitWrite(func() {
		if err := p.db.Queries.UpdatePlayerState(p.db.Ctx, params); err != nil {
			p.logger.Error(
				"Failed to save player state", "err", err,
			)

			return
		}

		p.logger.Info("Player state saved",
			"volume", volume,
			"muted", muted,
			"trackPath", trackPath,
			"positionSeconds", positionSeconds,
		)
	})
}

// ---------------------------------------------------------------
// State restoration
// ---------------------------------------------------------------

// RestoreState loads the persisted player state from the database.
func (p *Player) RestoreState() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.restoreStateLocked()
}

func (p *Player) restoreStateLocked() {
	// Reads back what saveState wrote, so it waits for anything still
	// in flight rather than restoring the state before last.
	p.flushWrites()

	defer profiling.TimeOp(p.logger, "player.RestoreState")()

	if p.db == nil {
		p.logger.Warn(
			"No database available, cannot restore player state",
		)

		return
	}

	state, err := p.db.ReadQueries.GetPlayerState(p.db.Ctx)
	if err != nil {
		p.logger.Error(
			"Failed to load player state", "err", err,
		)

		return
	}

	// Restore volume.
	// Ensure volume is initialized before restoring settings. The
	// volume effect is normally created by updateStreamers during
	// LoadFile, but RestoreState runs before any file is loaded.
	if p.volume == nil {
		p.volume = &effects.Volume{
			Streamer: p.control,
			Base:     2,
		}
	}

	if p.systemVolume {
		// Remembered, not applied: the device's keys are the volume
		// control here, so the player runs wide open and hands the
		// stored level back untouched at the next save.
		p.storedVolume = clampVolume(UserVolume(state.Volume))
		p.storedMuted = state.Muted
		p.setVolumeLocked(MaxUserVol)
	} else {
		vol := clampVolume(UserVolume(state.Volume))
		p.setVolumeLocked(vol)

		if state.Muted {
			p.volume.Silent = true
		}
	}

	// Restore last track if the file still exists.
	if state.LastTrackPath != "" {
		if _, statErr := os.Stat(state.LastTrackPath); statErr != nil {
			p.logger.Warn(
				"Last track file no longer exists, "+
					"skipping restore",
				"path", state.LastTrackPath,
				"err", statErr,
			)

			return
		}

		err = p.loadFileLocked(state.LastTrackPath)
		if err != nil {
			p.logger.Error(
				"Failed to restore last track",
				"path", state.LastTrackPath, "err", err,
			)

			return
		}

		// Restore playback position.
		if state.LastPositionSeconds > 0 {
			err = p.seekLocked(int(state.LastPositionSeconds))
			if err != nil {
				p.logger.Error(
					"Failed to restore playback position",
					"seconds", state.LastPositionSeconds,
					"err", err,
				)
			}
		}
	}

	p.logger.Info("Player state restored",
		"volume", p.getUserVolume(),
		"muted", p.volume.Silent,
		"systemVolume", p.systemVolume,
		"trackPath", state.LastTrackPath,
		"positionSeconds", state.LastPositionSeconds,
	)
}
