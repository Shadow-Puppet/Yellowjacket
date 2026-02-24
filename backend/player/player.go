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

	"github.com/TheCodeOfCaleb/beep/v2"
	"github.com/TheCodeOfCaleb/beep/v2/effects"
	"github.com/TheCodeOfCaleb/beep/v2/generators"
	"github.com/TheCodeOfCaleb/beep/v2/speaker"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"yellowjacket/backend/database"
	"yellowjacket/backend/database/sql/sqlcgen"
	"yellowjacket/backend/events"
	"yellowjacket/backend/library"
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
	control                 *beep.Ctrl
	volume                  *effects.Volume
	speakerStreamer         beep.Streamer
	playbackFinishedHandler func()
	trackChangeID           uint64
}

// State represents the current playback state.
type State string

// Playback state values.
const (
	Playing State = "playing"
	Paused  State = "paused"
	Stopped State = "stopped"
)

// Sentinel errors for player operations.
var (
	errNoControlStreamer = errors.New("no control streamer")
	errNoAudioFileLoaded = errors.New("no audio file loaded")
	errNoStreamerToPlay  = errors.New("no streamer to play")
	errNoAudioStream     = errors.New("no audio stream to pause")
)

var speakerSampleRate = beep.SampleRate(44100)

// NewPlayer creates a player and initializes the audio speaker.
func NewPlayer(
	ctx context.Context,
	logger *slog.Logger,
	db *database.DB,
) (*Player, error) {
	defer profiling.TimeOp(logger, "player.NewPlayer")()

	player := &Player{
		ctx:          ctx,
		logger:       logger,
		db:           db,
		state:        Stopped,
		baseStreamer: generators.Silence(-1),
		format: beep.Format{
			SampleRate: speakerSampleRate,
		},
	}

	// TODO: allow user to change buffer size and speaker sample rate
	err := speaker.Init(
		player.format.SampleRate,
		player.format.SampleRate.N(time.Second/10),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to initialize speaker: %w", err,
		)
	}

	return player, nil
}

// SetPlaybackFinishedHandler sets a callback invoked when a track
// finishes naturally. This allows the queue to drive auto-advance
// without circular imports.
func (p *Player) SetPlaybackFinishedHandler(handler func()) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.playbackFinishedHandler = handler
}

// SetContext sets the Wails context, registers event handlers, and
// restores persisted state.
func (p *Player) SetContext(ctx context.Context) {
	p.mu.Lock()
	p.ctx = ctx
	p.mu.Unlock()

	p.registerEventHandlers()

	p.mu.Lock()
	p.restoreStateLocked()
	p.mu.Unlock()
}

func (p *Player) registerEventHandlers() {
	p.mu.Lock()
	ctx := p.ctx
	p.mu.Unlock()

	if ctx == nil {
		p.logger.Error(
			"Context is nil, cannot register event handlers",
		)

		return
	}

	runtime.EventsOn(
		ctx,
		events.RequestPause,
		func(_ ...any) {
			p.logger.Info("Received RequestPauseEvent")

			if err := p.Pause(); err != nil {
				p.logger.Error("failed to pause", "err", err)
			}
		},
	)

	runtime.EventsOn(
		ctx,
		events.RequestLoadFile,
		func(data ...any) {
			p.logger.Info("Received RequestLoadFileEvent")

			if len(data) < 1 {
				p.logger.Warn(
					"RequestLoadFile: missing file path argument",
				)

				return
			}

			filePath, ok := data[0].(string)
			if !ok {
				p.logger.Warn(
					"RequestLoadFile: invalid file path type",
					"got", fmt.Sprintf("%T", data[0]),
				)

				return
			}

			err := p.LoadFile(filePath)
			if err != nil {
				p.logger.Error(err.Error())
			}
		},
	)

	runtime.EventsOn(ctx, events.Seek, func(data ...any) {
		p.logger.Info("Received SeekEvent")

		if len(data) < 1 {
			p.logger.Warn("Seek: missing seek value argument")

			return
		}

		seekFloat, ok := data[0].(float64)
		if !ok {
			p.logger.Warn(
				"Seek: invalid seek value type",
				"got", fmt.Sprintf("%T", data[0]),
			)

			return
		}

		seekValue := int(seekFloat)

		err := p.Seek(seekValue)
		if err != nil {
			p.logger.Error("cannot seek", "error", err)
		}
	})

	runtime.EventsOn(
		ctx,
		events.RequestSetVolume,
		func(data ...any) {
			if len(data) < 1 {
				p.logger.Warn(
					"RequestSetVolume: missing volume argument",
				)

				return
			}

			volFloat, ok := data[0].(float64)
			if !ok {
				p.logger.Warn(
					"RequestSetVolume: invalid volume type",
					"got", fmt.Sprintf("%T", data[0]),
				)

				return
			}

			desiredVolume := UserVolume(volFloat)
			p.logger.Info(
				"Received RequestSetVolumeEvent",
				"volume", desiredVolume,
			)

			p.mu.Lock()
			p.setVolumeLocked(desiredVolume)
			p.emitVolumeChanged()
			p.saveState()
			p.mu.Unlock()
		},
	)
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
}

func (p *Player) emitTrackChanged() {
	if p.ctx == nil {
		p.logger.Error("Context is nil, cannot emit event")

		return
	}

	trackLengthSecs, err := p.trackLengthLocked()
	if err != nil {
		p.logger.Error("Cannot get track length")
	}

	trackInfo, err := p.getCurrentTrackInfoLocked()
	if err != nil {
		p.logger.Error("Cannot get track info")

		trackInfo = map[string]interface{}{
			"fileName": "",
			"filePath": "",
			"state":    string(p.state),
		}
	}

	// Compute current seek position in seconds.
	seekPosition := 0

	if p.seeker != nil {
		speaker.Lock()
		seekPosition = p.seeker.Position() /
			int(p.format.SampleRate)
		speaker.Unlock()
	}

	// Increment track change ID so the frontend can detect changes
	// even when the same file plays consecutively.
	p.trackChangeID++

	// Emit comprehensive track info.
	trackInfo["trackLength"] = trackLengthSecs
	trackInfo["seekPosition"] = seekPosition
	trackInfo["trackChangeId"] = p.trackChangeID
	runtime.EventsEmit(p.ctx, events.TrackChanged, trackInfo)

	p.logger.Info(
		"Emitting TrackChangedEvent with track info",
		"trackInfo", trackInfo,
	)
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

	// wrap in ctrl streamer to allow play/pause
	p.control = &beep.Ctrl{Streamer: p.resampled}

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
	p.mu.Unlock()

	// Emit events outside the lock — these are non-blocking Wails
	// calls that don't need player state.
	p.emitPlaybackStateChanged(Stopped)
	p.emitPlaybackFinished()
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

	// Release streamer chain. Volume is intentionally kept so the
	// user's volume setting persists across tracks.
	p.baseStreamer = nil
	p.seeker = nil
	p.resampled = nil
	p.control = nil
	p.speakerStreamer = nil

	p.state = Stopped

	// Notify frontend that there is no longer a current track.
	p.emitPlaybackStateChanged(p.state)
	runtime.EventsEmit(p.ctx, events.TrackChanged, nil)
	p.saveState()

	p.logger.Info("Track unloaded")
}

// ---------------------------------------------------------------
// Volume
// ---------------------------------------------------------------

// SetVolume sets the playback volume (0-100).
func (p *Player) SetVolume(desiredVolume UserVolume) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.setVolumeLocked(desiredVolume)

	return nil
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
// seconds.
func (p *Player) CurrentPositionSeconds() (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.seeker == nil {
		return 0, errNoAudioFileLoaded
	}

	speaker.Lock()
	pos := p.seeker.Position() / int(p.format.SampleRate)
	speaker.Unlock()

	return pos, nil
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

	p.logger.Debug(
		"attempting to seek",
		"target-seconds", targetSeconds,
		"song-length", lengthSecs,
		"samples", samples,
	)

	if seekErr := p.seeker.Seek(samples); seekErr != nil {
		speaker.Unlock()

		return fmt.Errorf("failed to seek: %w", seekErr)
	}

	speaker.Unlock()

	return nil
}

// ---------------------------------------------------------------
// Track info
// ---------------------------------------------------------------

// GetCurrentTrackInfo returns information about the currently
// loaded track.
func (p *Player) GetCurrentTrackInfo() (
	map[string]interface{}, error,
) {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.getCurrentTrackInfoLocked()
}

func (p *Player) getCurrentTrackInfoLocked() (
	map[string]interface{}, error,
) {
	if p.currentFile == nil {
		return map[string]interface{}{
			"fileName": "",
			"filePath": "",
			"state":    string(p.state),
			"title":    "",
			"artist":   "",
			"album":    "",
			"coverArt": "",
		}, nil
	}

	fileName := filepath.Base(p.currentFile.Name())
	filePath := p.currentFile.Name()

	// Default values.
	title := fileName
	artist := ""
	album := ""
	coverArt := ""
	coverArtSmall := ""
	coverArtMedium := ""
	coverArtLarge := ""

	// Try to get metadata from database.
	if p.db != nil {
		meta, err := p.db.Queries.GetTrackMetadataByPath(
			p.ctx, filePath,
		)
		if err == nil {
			if meta.Title != "" {
				title = meta.Title
			}

			artist = meta.Artist
			album = meta.Album

			if meta.CoverArtPath != "" {
				base := filepath.Base(meta.CoverArtPath)
				coverArt = "/covers/" + base
				coverArtSmall = "/covers/" +
					library.SizedFilename(base, "_sm")
				coverArtMedium = "/covers/" +
					library.SizedFilename(base, "_md")
				coverArtLarge = "/covers/" +
					library.SizedFilename(base, "_lg")
			}
		} else {
			p.logger.Debug(
				"Could not get track metadata from database",
				"path", filePath, "err", err,
			)
		}
	}

	return map[string]interface{}{
		"fileName":       fileName,
		"filePath":       filePath,
		"state":          string(p.state),
		"title":          title,
		"artist":         artist,
		"album":          album,
		"coverArt":       coverArt,
		"coverArtSmall":  coverArtSmall,
		"coverArtMedium": coverArtMedium,
		"coverArtLarge":  coverArtLarge,
	}, nil
}

// TrackLengthInSeconds returns the duration of the current track.
func (p *Player) TrackLengthInSeconds() (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.trackLengthLocked()
}

func (p *Player) trackLengthLocked() (int, error) {
	if p.seeker == nil {
		return 0, errNoAudioFileLoaded
	}

	speaker.Lock()
	length := p.seeker.Len() / int(p.format.SampleRate)
	speaker.Unlock()

	return length, nil
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

	volume := int64(MaxUserVol)
	muted := false

	if p.volume != nil {
		volume = int64(p.getUserVolume())
		muted = p.volume.Silent
	}

	trackPath := ""
	if p.currentFile != nil {
		trackPath = p.currentFile.Name()
	}

	positionSeconds := int64(0)

	if p.seeker != nil {
		speaker.Lock()
		positionSeconds = int64(p.seeker.Position()) /
			int64(p.format.SampleRate)
		speaker.Unlock()
	}

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
