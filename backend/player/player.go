package player

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/gopxl/beep/mp3"
	"github.com/gopxl/beep/speaker"
)

type Player struct {
	ctx   context.Context
	state PlayerState
}

type PlayerState int

const (
	Playing PlayerState = iota
	Paused
	Stopped
)

func NewPlayer() (*Player, error) {
	return &Player{}, nil
}

func (p *Player) Init(ctx context.Context) {
	p.ctx = ctx
	p.state = Stopped
}

func (p *Player) ChangeState(desiredState PlayerState) error {
	return nil
}

func (p *Player) Play() error {
	f, err := os.Open("test_data/music_library_test/03 anything.mp3")
	if err != nil {
		return fmt.Errorf("Failed to open file %w", err)
	}

	streamer, format, err := mp3.Decode(f)
	if err != nil {
		return fmt.Errorf("Failed to decode mp3 %w", err)
	}
	defer streamer.Close()

	err = speaker.Init(format.SampleRate, format.SampleRate.N(time.Second/10))
	if err != nil {
		return fmt.Errorf("Failed to initialize speaker %w", err)
	}
	speaker.Play(streamer)
	select {}
	return nil
}
