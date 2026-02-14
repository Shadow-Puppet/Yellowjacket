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
	"time"

	"github.com/TheCodeOfCaleb/beep/v2"
	"github.com/TheCodeOfCaleb/beep/v2/effects"
	"github.com/TheCodeOfCaleb/beep/v2/generators"
	"github.com/TheCodeOfCaleb/beep/v2/speaker"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"yellowjacket/backend/database"
	"yellowjacket/backend/database/sql/sqlcgen"
	"yellowjacket/backend/events"
	"yellowjacket/backend/metadata"
)

// Player handles audio playback and state management.
type Player struct {
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
func NewPlayer(ctx context.Context, logger *slog.Logger, db *database.DB) (*Player, error) {
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
	err := speaker.Init(player.format.SampleRate, player.format.SampleRate.N(time.Second/10))
	if err != nil {
		return nil, fmt.Errorf("failed to initialize speaker %w", err)
	}

	return player, nil
}

// SetPlaybackFinishedHandler sets a callback that is invoked when a track finishes naturally.
// This allows the queue to drive auto-advance without circular imports.
func (p *Player) SetPlaybackFinishedHandler(handler func()) {
	p.playbackFinishedHandler = handler
}

// SetContext sets the Wails context, registers event handlers, and restores persisted state.
func (p *Player) SetContext(ctx context.Context) {
	p.ctx = ctx
	p.registerEventHandlers()
	p.RestoreState()
}

func (p *Player) registerEventHandlers() {
	if p.ctx == nil {
		p.logger.Error("Context is nil, cannot register event handlers")

		return
	}

	runtime.EventsOn(p.ctx, events.RequestPlay, func(_ ...any) {
		p.logger.Info("Received RequestPlayEvent")

		if err := p.Play(); err != nil {
			p.logger.Error("failed to play", "err", err)
		}
	})
	runtime.EventsOn(p.ctx, events.RequestPause, func(_ ...any) {
		p.logger.Info("Received RequestPauseEvent")

		if err := p.Pause(); err != nil {
			p.logger.Error("failed to pause", "err", err)
		}
	})
	runtime.EventsOn(p.ctx, events.RequestLoadFile, func(data ...any) {
		p.logger.Info("Received RequestLoadFileEvent")

		filePath := data[0].(string)
		p.logger.Info(filePath)

		err := p.LoadFile(filePath)
		if err != nil {
			p.logger.Error(err.Error())
		} else {
			p.logger.Info(p.currentFile.Name())
		}
	})
	runtime.EventsOn(p.ctx, events.Seek, func(data ...any) {
		p.logger.Info("Received SeekEvent", "Data", data[0])
		seekValue := int(data[0].(float64))

		err := p.Seek(seekValue)
		if err != nil {
			p.logger.Error("cannot seek", "error", err)
		}
	})
	runtime.EventsOn(p.ctx, events.RequestSetVolume, func(data ...any) {
		desiredVolume := UserVolume(data[0].(float64))
		p.logger.Info("Received RequestSetVolumeEvent", "volume", desiredVolume)

		err := p.SetVolume(desiredVolume)
		if err != nil {
			p.logger.Error("cannot set volume", "error", err)

			return
		}

		p.emitVolumeChanged()
	})
}

// emitPlaybackStateChanged emits a playback state change event.
func (p *Player) emitPlaybackStateChanged(state State) {
	if p.ctx == nil {
		p.logger.Error("Context is nil, cannot emit event")

		return
	}

	p.logger.Info("Emitting PlaybackStateChangedEvent", "state", state)
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
	p.logger.Info("Emitting VolumeChangedEvent", "volume", volume)
	runtime.EventsEmit(p.ctx, events.VolumeChanged, volume)
}

func (p *Player) emitTrackChanged() {
	if p.ctx == nil {
		p.logger.Error("Context is nil, cannot emit event")

		return
	}

	trackLengthSecs, err := p.TrackLengthInSeconds()
	if err != nil {
		p.logger.Error("Cannot get track length")
	}

	trackInfo, err := p.GetCurrentTrackInfo()
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
		seekPosition = p.seeker.Position() / int(p.format.SampleRate)
		speaker.Unlock()
	}

	// Emit comprehensive track info
	trackInfo["trackLength"] = trackLengthSecs
	trackInfo["seekPosition"] = seekPosition
	runtime.EventsEmit(p.ctx, events.TrackChanged, trackInfo)

	p.logger.Info("Emitting TrackChangedEvent with track info", "trackInfo", trackInfo)
}

// EmitCurrentState pushes the current player state to the frontend.
// This is intended to be called after the frontend is ready to receive events,
// separately from RestoreState which does the heavy lifting during OnStartup.
func (p *Player) EmitCurrentState() {
	p.emitVolumeChanged()

	if p.currentFile != nil {
		p.emitPlaybackStateChanged(p.state)
		p.emitTrackChanged()
	}
}

func (p *Player) updateStreamers(newBaseStreamer beep.StreamSeeker, sr beep.SampleRate) error {
	// set base streamer
	p.baseStreamer = newBaseStreamer
	p.seeker = newBaseStreamer

	// resample file stream to match speaker
	// TODO: variable resample quality
	p.resampled = beep.Resample(4, sr, speakerSampleRate, p.baseStreamer)

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

// startPaused registers the current streamer chain with the speaker in a
// paused state. This keeps the speaker always active when a file is loaded,
// so Play() only ever needs to unpause the control gate.
func (p *Player) startPaused() {
	speaker.Lock()
	p.control.Paused = true
	speaker.Unlock()

	speaker.Play(beep.Seq(p.speakerStreamer, beep.Callback(func() {
		p.state = Stopped
		p.emitPlaybackStateChanged(p.state)
		p.emitPlaybackFinished()
		p.logger.Info("Playback finished naturally")

		// Notify queue for auto-advance.
		if p.playbackFinishedHandler != nil {
			p.playbackFinishedHandler()
		}
	})))

	p.state = Paused
}

// LoadFile opens and decodes an audio file for playback.
func (p *Player) LoadFile(filePath string) error {
	// opening file
	f, err := os.Open(filePath)
	if err != nil {
		p.logger.Error("Failed to open file")

		return fmt.Errorf("failed to open file %w", err)
	}

	streamer, format, err := metadata.DecodeFile(f)
	if err != nil {
		p.logger.Error("failed to decode audio file", "path", filePath, "err", err)

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
			p.logger.Warn("failed to close previous audio file", "err", closeErr)
		}
	}

	p.currentFile = f

	if err := p.updateStreamers(streamer, format.SampleRate); err != nil {
		return fmt.Errorf("failed to update streamers: %w", err)
	}

	p.startPaused()
	p.emitPlaybackStateChanged(p.state)
	p.emitTrackChanged()
	p.logger.Info("File loaded, state set to paused", "file", filePath)

	return nil
}

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
	if err := p.validateReadyToPlay(); err != nil {
		return err
	}

	if p.state == Playing {
		p.logger.Info("Already playing")

		return nil
	}

	// Track finished naturally — seek to the beginning and re-register
	// a paused stream with the speaker so the unpause below starts it.
	if p.state == Stopped && p.seeker != nil {
		speaker.Lock()
		err := p.seeker.Seek(0)
		speaker.Unlock()

		if err != nil {
			return fmt.Errorf("failed to seek to beginning: %w", err)
		}

		if err := p.updateStreamers(p.seeker, p.format.SampleRate); err != nil {
			return fmt.Errorf("failed to update streamers for replay: %w", err)
		}

		p.startPaused()
		p.logger.Info("Rebuilt streamers for replay")
	}

	// Unpause — works for both resume-from-pause and replay-from-stopped.
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
	} else {
		p.logger.Info("Already paused or not playing")
	}

	return nil
}

// SetVolume sets the playback volume (0-100).
func (p *Player) SetVolume(desiredVolume UserVolume) error {
	speaker.Lock()
	// clamp value between 1 and 100
	volume := clampVolume(desiredVolume)

	// Apply the volume settings
	p.volume.Volume = float64(volume.ToVolume())
	p.volume.Silent = volume == MinUserVol
	speaker.Unlock()

	return nil
}

// ChangeVolume adjusts the volume by a relative amount.
func (p *Player) ChangeVolume(deltaVolume int) error {
	return p.SetVolume(p.getUserVolume() + UserVolume(deltaVolume))
}

func (p *Player) getUserVolume() UserVolume {
	return Volume(p.volume.Volume).ToUserVolume()
}

// MuteToggle toggles the mute state.
func (p *Player) MuteToggle() error {
	p.volume.Silent = !p.volume.Silent

	return nil
}

// CurrentPositionSeconds returns the current playback position in seconds.
func (p *Player) CurrentPositionSeconds() (int, error) {
	if p.seeker == nil {
		return 0, errNoAudioFileLoaded
	}

	speaker.Lock()
	pos := p.seeker.Position() / int(p.format.SampleRate)
	speaker.Unlock()

	return pos, nil
}

// CurrentPosition returns the playback position as a percentage (0-100).
func (p *Player) CurrentPosition() (int, error) {
	if p.seeker == nil {
		return 0, errNoAudioFileLoaded
	}

	speaker.Lock()
	pos := math.Round(100.0 * float64(p.seeker.Position()) / float64(p.seeker.Len()))
	speaker.Unlock()

	return int(pos), nil
}

// Seek jumps to a specific position in seconds.
func (p *Player) Seek(targetSeconds int) error {
	if p.seeker == nil {
		runtime.EventsEmit(p.ctx, events.SeekFailed)

		return errNoAudioFileLoaded
	}

	lengthSecs, err := p.TrackLengthInSeconds()
	if err != nil {
		return fmt.Errorf("cannot get track length: %w", err)
	}

	speaker.Lock()
	samples := int(
		math.Round((float64(targetSeconds) / float64(lengthSecs)) * float64(p.seeker.Len())),
	)
	p.logger.Debug(
		"attempting to seek",
		"target-seconds",
		targetSeconds,
		"song-length",
		lengthSecs,
		"samples",
		samples,
	)

	if seekErr := p.seeker.Seek(samples); seekErr != nil {
		speaker.Unlock()

		return fmt.Errorf("failed to seek: %w", seekErr)
	}

	speaker.Unlock()

	return nil
}

// GetCurrentTrackInfo returns information about the currently loaded track.
func (p *Player) GetCurrentTrackInfo() (map[string]interface{}, error) {
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

	// Default values
	title := fileName
	artist := ""
	album := ""
	coverArt := ""

	// Try to get metadata from database
	if p.db != nil {
		meta, err := p.db.Queries.GetTrackMetadataByPath(p.ctx, filePath)
		if err == nil {
			if meta.Title != "" {
				title = meta.Title
			}

			artist = meta.Artist
			album = meta.Album

			if meta.CoverArtPath != "" {
				coverArt = "/covers/" + filepath.Base(meta.CoverArtPath)
			}
		} else {
			p.logger.Debug("Could not get track metadata from database", "path", filePath, "err", err)
		}
	}

	return map[string]interface{}{
		"fileName": fileName,
		"filePath": filePath,
		"state":    string(p.state),
		"title":    title,
		"artist":   artist,
		"album":    album,
		"coverArt": coverArt,
	}, nil
}

// TrackLengthInSeconds returns the duration of the current track.
func (p *Player) TrackLengthInSeconds() (int, error) {
	if p.seeker == nil {
		return 0, errNoAudioFileLoaded
	}

	speaker.Lock()
	length := p.seeker.Len() / int(p.format.SampleRate)
	speaker.Unlock()

	return length, nil
}

// SaveState persists the current player state to the database.
func (p *Player) SaveState() {
	if p.db == nil {
		p.logger.Warn("No database available, cannot save player state")

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
		positionSeconds = int64(p.seeker.Position()) / int64(p.format.SampleRate)
		speaker.Unlock()
	}

	err := p.db.Queries.UpdatePlayerState(p.db.Ctx, sqlcgen.UpdatePlayerStateParams{
		Volume:              volume,
		Muted:               muted,
		LastTrackPath:       trackPath,
		LastPositionSeconds: positionSeconds,
	})
	if err != nil {
		p.logger.Error("Failed to save player state", "err", err)

		return
	}

	p.logger.Info("Player state saved",
		"volume", volume,
		"muted", muted,
		"trackPath", trackPath,
		"positionSeconds", positionSeconds,
	)
}

// RestoreState loads the persisted player state from the database.
func (p *Player) RestoreState() {
	if p.db == nil {
		p.logger.Warn("No database available, cannot restore player state")

		return
	}

	state, err := p.db.Queries.GetPlayerState(p.db.Ctx)
	if err != nil {
		p.logger.Error("Failed to load player state", "err", err)

		return
	}

	// Restore volume.
	// Ensure volume is initialized before restoring settings. The volume
	// effect is normally created by updateStreamers during LoadFile, but
	// RestoreState runs before any file is loaded.
	if p.volume == nil {
		p.volume = &effects.Volume{
			Streamer: p.control,
			Base:     2,
		}
	}

	vol := clampVolume(UserVolume(state.Volume))

	err = p.SetVolume(vol)
	if err != nil {
		p.logger.Error("Failed to restore volume", "err", err)
	}

	if state.Muted {
		p.volume.Silent = true
	}

	// Restore last track if the file still exists.
	if state.LastTrackPath != "" {
		if _, statErr := os.Stat(state.LastTrackPath); statErr != nil {
			p.logger.Warn("Last track file no longer exists, skipping restore",
				"path", state.LastTrackPath,
				"err", statErr,
			)

			return
		}

		err = p.LoadFile(state.LastTrackPath)
		if err != nil {
			p.logger.Error("Failed to restore last track", "path", state.LastTrackPath, "err", err)

			return
		}

		// Restore playback position.
		if state.LastPositionSeconds > 0 {
			err = p.Seek(int(state.LastPositionSeconds))
			if err != nil {
				p.logger.Error("Failed to restore playback position",
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
