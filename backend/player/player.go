package player

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/mp3"
	"github.com/gopxl/beep/speaker"
)

type Player struct {
	ctx        context.Context
	state      PlayerState
	streamer   beep.Streamer
	format     beep.Format
	sampleRate beep.SampleRate
}

type PlayerState int

const (
	Playing PlayerState = iota
	Paused
	Stopped
)

func NewPlayer() (*Player, error) {
	return &Player{
		// TODO: variable speaker sample rates
		sampleRate: beep.SampleRate(44100),
	}, nil
}

func (p *Player) Init(ctx context.Context) {
	p.ctx = ctx
	p.state = Stopped

	speaker.Init(p.sampleRate, p.sampleRate.N(time.Second/10))
}

func (p *Player) ChangeState(desiredState PlayerState) error {
	return nil
}

func (p *Player) Play(filePath string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file %w", err)
	}

	streamer, format, err := mp3.Decode(f)
	if err != nil {
		return fmt.Errorf("failed to decode mp3 %w", err)
	}
	defer streamer.Close()

	// initialize the speaker with the sample rate from the file
	err = speaker.Init(format.SampleRate, format.SampleRate.N(time.Second/10))
	if err != nil {
		return fmt.Errorf("failed to initialize speaker %w", err)
	}

	// hangs until song finishes playing
	done := make(chan bool)
	speaker.Play(beep.Seq(streamer, beep.Callback(func() {
		done <- true
	})))

	<-done
	return nil
}
