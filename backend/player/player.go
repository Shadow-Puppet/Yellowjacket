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
	"github.com/wailsapp/wails/v2/pkg/runtime"

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

	ctx                     context.Context
	logger                  *slog.Logger
	db                      *database.DB
	state                   State
	currentFile             *os.File
	format                  beep.Format
	baseStreamer            beep.Streamer
	seeker                  beep.StreamSeeker
	resampled               beep.Streamer
	buffered                *BufferedStreamer
	control                 *beep.Ctrl
	volume                  *effects.Volume
	speakerStreamer         beep.Streamer
	playbackFinishedHandler func()
	trackChangeID           uint64
	mediaControls           mediacontrols.Handler

	// trackLengthMs holds the authoritative track duration in
	// milliseconds, sourced from the database (which uses the
	// custom header parser).  The go-mp3 decoder's Len() can be
	// inflated for files with multiple ID3v2 tags, so this value
	// is preferred for display and position calculations.
	trackLengthMs int64
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
	FileName       string `json:"fileName"`
	FilePath       string `json:"filePath"`
	State          State  `json:"state"`
	Title          string `json:"title"`
	Artist         string `json:"artist"`
	Album          string `json:"album"`
	CoverArt       string `json:"coverArt"`
	CoverArtSmall  string `json:"coverArtSmall"`
	CoverArtMedium string `json:"coverArtMedium"`
	CoverArtLarge  string `json:"coverArtLarge"`
	TrackLength    int    `json:"trackLength"`
	SeekPosition   int    `json:"seekPosition"`
	TrackChangeID  uint64 `json:"trackChangeId"`
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
// finishes naturally. This allows the queue to drive auto-advance
// without circular imports.
func (p *Player) SetPlaybackFinishedHandler(handler func()) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.playbackFinishedHandler = handler
}

// SetMediaControls provides an OS media controls handler. When set,
// the player pushes metadata, playback state, volume, and seek
// notifications to the OS media overlay.
func (p *Player) SetMediaControls(h mediacontrols.Handler) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.mediaControls = h
}

// SetContext sets the Wails runtime context and restores persisted
// state.
func (p *Player) SetContext(ctx context.Context) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.ctx = ctx
	p.restoreStateLocked()
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

	runtime.EventsEmit(
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
	runtime.EventsEmit(p.ctx, events.PlaybackFinished, nil)
}

func (p *Player) emitVolumeChanged() {
	if p.ctx == nil {
		p.logger.Error("Context is nil, cannot emit event")

		return
	}

	volume := int(p.getUserVolume())
	p.logger.Info(
		"Emitting VolumeChangedEvent", "volume", volume,
	)

	runtime.EventsEmit(p.ctx, events.VolumeChanged, volume)

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

	runtime.EventsEmit(
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

	// The beep.Callback runs with the speaker mutex held, so we
	// dispatch to a goroutine that can safely acquire p.mu.
	speaker.Play(beep.Seq(
		p.speakerStreamer,
		beep.Callback(func() {
			go p.onPlaybackFinished()
		}),
	))

	p.state = Paused
}

// onPlaybackFinished handles the natural end of a track. It is
// called on a new goroutine from the beep callback (which holds
// the speaker lock) so that it can safely acquire p.mu.
func (p *Player) onPlaybackFinished() {
	p.mu.Lock()
	p.state = Stopped
	handler := p.playbackFinishedHandler
	mc := p.mediaControls
	p.mu.Unlock()

	// Emit Wails events outside the lock — these are non-blocking
	// calls that don't need player state.
	p.emitPlaybackFinished()

	if p.ctx != nil {
		runtime.EventsEmit(
			p.ctx,
			events.PlaybackStateChanged,
			map[string]string{"state": string(Stopped)},
		)
	}

	// Notify media controls outside the lock. The track just
	// ended so position is 0.
	if mc != nil {
		mc.UpdatePlaybackState(
			mediacontrols.StateStopped, 0,
		)
	}

	p.logger.Info("Playback finished naturally")

	// Notify queue for auto-advance. Called without p.mu held
	// because it re-enters the player via LoadFile/Play.
	if handler != nil {
		handler()
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

	if err := p.updateStreamers(
		streamer, format.SampleRate,
	); err != nil {
		return fmt.Errorf("failed to update streamers: %w", err)
	}

	p.startPaused()
	p.emitPlaybackStateChanged(p.state)
	p.emitTrackChanged()
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
	runtime.EventsEmit(p.ctx, events.TrackChanged, nil)

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

	p.setVolumeLocked(desiredVolume)
	p.emitVolumeChanged()
	p.saveState()
}

func (p *Player) setVolumeLocked(desiredVolume UserVolume) {
	speaker.Lock()

	volume := clampVolume(desiredVolume)
	p.volume.Volume = float64(volume.ToVolume())
	p.volume.Silent = volume == MinUserVol

	speaker.Unlock()
}

// ChangeVolume adjusts the volume by a relative amount.
func (p *Player) ChangeVolume(deltaVolume int) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.setVolumeLocked(p.getUserVolume() + UserVolume(deltaVolume))

	return nil
}

func (p *Player) getUserVolume() UserVolume {
	return Volume(p.volume.Volume).ToUserVolume()
}

// MuteToggle toggles the mute state.
func (p *Player) MuteToggle() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.volume.Silent = !p.volume.Silent
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

func (p *Player) seekLocked(targetSeconds int) error {
	if p.seeker == nil {
		runtime.EventsEmit(p.ctx, events.SeekFailed)

		return errNoAudioFileLoaded
	}

	lengthSecs, err := p.trackLengthLocked()
	if err != nil {
		return fmt.Errorf("cannot get track length: %w", err)
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

		p.logger.Warn(
			"Seek failed, playback will start from "+
				"the beginning",
			"target-seconds", targetSeconds,
			"samples", samples,
			"err", seekErr,
		)

		return fmt.Errorf("failed to seek: %w", seekErr)
	}

	speaker.Unlock()

	if p.mediaControls != nil {
		p.mediaControls.NotifySeek(targetSeconds)
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
		meta, err := p.db.Queries.GetTrackMetadataByPath(
			p.ctx, info.FilePath,
		)
		if err == nil {
			if meta.Title != "" {
				info.Title = meta.Title
			}

			info.Artist = meta.Artist
			info.Album = meta.Album
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
		dbMeta, err := p.db.Queries.GetTrackMetadataByPath(
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

// SaveState persists the current player state to the database.
// This is called during shutdown to capture the final state.
func (p *Player) SaveState() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.saveState()
}

// saveState is the internal helper that writes the current player
// state to the database. Must be called with p.mu held.
func (p *Player) saveState() {
	if p.db == nil {
		p.logger.Warn(
			"No database available, cannot save player state",
		)

		return
	}

	volume := int64(DefaultUserVol)
	muted := false

	if p.volume != nil {
		volume = int64(p.getUserVolume())
		muted = p.volume.Silent
	}

	trackPath := ""
	if p.currentFile != nil {
		trackPath = p.currentFile.Name()
	}

	positionSeconds := int64(p.displayPositionSecsLocked())

	err := p.db.Queries.UpdatePlayerState(
		p.db.Ctx,
		sqlcgen.UpdatePlayerStateParams{
			Volume:              volume,
			Muted:               muted,
			LastTrackPath:       trackPath,
			LastPositionSeconds: positionSeconds,
		},
	)
	if err != nil {
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
	defer profiling.TimeOp(p.logger, "player.RestoreState")()

	if p.db == nil {
		p.logger.Warn(
			"No database available, cannot restore player state",
		)

		return
	}

	state, err := p.db.Queries.GetPlayerState(p.db.Ctx)
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

	vol := clampVolume(UserVolume(state.Volume))
	p.setVolumeLocked(vol)

	if state.Muted {
		p.volume.Silent = true
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
		"volume", vol,
		"muted", state.Muted,
		"trackPath", state.LastTrackPath,
		"positionSeconds", state.LastPositionSeconds,
	)
}
