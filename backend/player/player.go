package player

import (
	"context"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/effects"
	"github.com/gopxl/beep/generators"
	"github.com/gopxl/beep/mp3"
	"github.com/gopxl/beep/speaker"
)

type Player struct {
	ctx             context.Context
	state           PlayerState
	currentFile     *os.File
	format          beep.Format
	baseStreamer    beep.Streamer
	seeker          beep.StreamSeeker
	resampled       beep.Streamer
	control         *beep.Ctrl
	volume          *effects.Volume
	speakerStreamer beep.Streamer
}

type PlayerState int

const (
	Playing PlayerState = iota
	Paused
	Stopped
)

var speakerSampleRate = beep.SampleRate(44100)

func NewPlayer() (*Player, error) {
	return &Player{
		state:        Stopped,
		baseStreamer: generators.Silence(-1),
		format: beep.Format{
			SampleRate: speakerSampleRate,
		},
	}, nil
}

func (p *Player) Init(ctx context.Context) error {
	p.ctx = ctx

	// Initialize speaker
	// TODO: allow user to change buffer size and speaker sample rate
	err := speaker.Init(p.format.SampleRate, p.format.SampleRate.N(time.Second/10))
	if err != nil {
		return fmt.Errorf("failed to initialize speaker %w", err)
	}

	return nil
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

	// wrap in volume streamer
	p.volume = &effects.Volume{
		Streamer: p.control,
		Base:     2,
		Volume:   0,
		Silent:   false,
	}

	// set "final" streamer
	p.speakerStreamer = p.volume

	return nil
}

// TODO: proper state management extracted to function
func (p *Player) changeState(desiredState PlayerState) error {
	return nil
}

// reads a file and creates a streamer, also wraps necessary streamers
func (p *Player) LoadFile(filePath string) error {
	// opening file
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file %w", err)
	}

	// attempt to decode mp3 file and create streamer and format data
	streamer, format, err := mp3.Decode(f)
	if err != nil {
		return fmt.Errorf("failed to decode mp3 %w", err)
	}

	p.currentFile = f

	p.updateStreamers(streamer, format.SampleRate)

	return nil
}

func (p *Player) Play() error {
	p.state = Playing
	// hangs until song finishes playing
	done := make(chan bool)
	speaker.Play(beep.Seq(p.speakerStreamer, beep.Callback(func() {
		p.state = Paused //called when streamer finishes
		done <- true
	})))

	<-done
	return nil
}

// TODO: reduce dupilcation in pause/resume functions
func (p *Player) Pause() error {
	speaker.Lock()
	p.control.Paused = true
	speaker.Unlock()
	return nil
}

// TODO: reduce dupilcation in pause/resume functions
func (p *Player) Resume() error {
	speaker.Lock()
	p.control.Paused = false
	speaker.Unlock()
	return nil
}

//Paraprasing info from the beep docs here:
/*
To INCREASE volume by 1 means to multiply the signal by Base.
Volume = 0 means unchanged volume.
Positive Volume value means increasing volume
Negative Volume value means decreasing volume
*/
func (p *Player) SetVolume(desiredVolume UserVolume) error {
	speaker.Lock()
	// clamp value between 1 and 100
	volume := clampVolume(desiredVolume)

	// Apply the volume settings
	p.volume.Volume = float64(volume.ToPlayerVolume())
	p.volume.Silent = volume == MinUserVol
	speaker.Unlock()

	return nil
}
func (p *Player) ChangeVolume(deltaVolume int) error {
	return p.SetVolume(p.getUserVolume() + UserVolume(deltaVolume))
}

func (p *Player) getUserVolume() UserVolume {
	return PlayerVolume(p.volume.Volume).ToUserVolume()
}

func (p *Player) MuteToggle() error {
	p.volume.Silent = !p.volume.Silent
	return nil
}

// return the current position as an int between 0 and 100 to work with progress bar easily.
func (p *Player) CurrentPosition() (int, error) {
	pos := math.Round(100.0 * float64(p.seeker.Position()) / float64(p.seeker.Len()))
	return int(pos), nil
}

// TODO: double check best type for percentage parameter
// percentage comes from the progress bar as a value between 0 and 100
func (p *Player) Seek(percentage int) error {
	//take percentage value (0-100), make 0-1, multiply by total number of samples in stream
	samples := int(math.Round((float64(percentage) / 100.0) * float64(p.seeker.Len())))
	p.seeker.Seek(samples)
	return nil
}

func (p *Player) TrackLengthInSeconds() (int, error) {
	len := p.seeker.Len() / int(p.format.SampleRate)
	return len, nil
}
