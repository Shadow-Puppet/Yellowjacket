package player

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"time"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/effects"
	"github.com/gopxl/beep/generators"
	"github.com/gopxl/beep/mp3"
	"github.com/gopxl/beep/speaker"
)

type Player struct {
	ctx          context.Context
	state        PlayerState
	currentFile  os.File
	format       beep.Format
	baseStreamer beep.Streamer
	resampled    beep.Streamer
	control      beep.Ctrl
	volume       effects.Volume
	queue        []string
}

type PlayerState int

const (
	Playing PlayerState = iota
	Paused
	Stopped
)

const testQueue = []string{
	"test_data/music_library_test/gnomed.mp3",
	"test_data/music_library_test/01 Some Chords.mp3",
	"test_data/music_library_test/03 anything.mp3",
}

func NewPlayer() (*Player, error) {
	return &Player{
		state:        Stopped,
		baseStreamer: generatorsilence(-1),
		// Default sample rate
		// TODO: read this from config
		fileFormat: beep.Format{},
		queue:      testQueue,
	}, nil
}

func (p *Player) Init(ctx context.Context) error {
	p.ctx = ctx
	p.state = Stopped
	p.streamer = generators.Silence(-1)

	// Initialize speaker
	// TODO: allow user to change buffer size and speaker sample rate
	err := speaker.Init(p.speakerSampleRate, p.speakerSampleRate.N(time.Second/10))
	if err != nil {
		return fmt.Errorf("failed to initialize speaker %w", err)
	}

	return nil
}

// TODO: proper state management extracted to function
func (p *Player) changeState(desiredState PlayerState) error {
	return nil
}

// reads a file and creates a streamer resampled to match the speaker
func (p *Player) CreateStreamerFromFile(filePath string) (beep.Streamer, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %w", err)
	}

	streamer, format, err := mp3.Decode(f)
	p.fileFormat = format
	if err != nil {
		return nil, fmt.Errorf("failed to decode mp3 %w", err)
	}

	// resample file stream to match speaker
	// TODO: variable resample quality
	resampled := beep.Resample(4, p.fileFormat.SampleRate, p.speakerSampleRate, streamer)
	// wrap in ctrl streamer to allow play/pause
	ctrl := &beep.Ctrl{Streamer: resampled}
	// wrap in volume streamer
	volume := &effects.Volume{
		Streamer: ctrl,
		Base:     2,
		Volume:   0,
		Silent:   false,
	}

	return volume, nil
}

func (p *Player) Play(streamer beep.Streamer) error {
	p.streamer = streamer
	p.state = Playing
	// hangs until song finishes playing
	done := make(chan bool)
	speaker.Play(beep.Seq(p.streamer, beep.Callback(func() {
		p.state = Paused //called when streamer finishes
		done <- true
	})))

	<-done
	return nil
}

// TODO: reduce dupilcation in pause/resume functions
func (p *Player) Pause() error {
	ctrl := &beep.Ctrl{}
	// checking if current streamer is a ctrl type, if not, wrap it in one
	if (reflect.TypeOf(p.streamer) != reflect.TypeOf(beep.Ctrl{})) {
		ctrl = &beep.Ctrl{Streamer: p.streamer}
	}
	speaker.Lock()
	ctrl.Paused = true
	speaker.Unlock()
	return nil
}

// TODO: reduce dupilcation in pause/resume functions
func (p *Player) Resume() error {
	ctrl := &beep.Ctrl{}
	// checking if current streamer is a ctrl type, if not, wrap it in one
	if (reflect.TypeOf(p.streamer) != reflect.TypeOf(beep.Ctrl{})) {
		ctrl = &beep.Ctrl{Streamer: p.streamer}
	}
	speaker.Lock()
	ctrl.Paused = false
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
// TODO: implement max/ min volume values
func (p *Player) SetVolume() error {
	volume := &effects.Volume{
		Streamer: p.streamer,
		Base:     2,
		Volume:   0,
		Silent:   false,
	}
	p.Play(volume)
	return nil
}

func (p *Player) MuteToggle() error {

	p.streamer.Silent = !p.Streamer.
		p.Play(volume)
	return nil
}
