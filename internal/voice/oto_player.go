// oto_player.go — the cgo sound-card player used by the live voice session.
//
// The android exception is the one platform where oto's driver needs cgo: its
// android backend drives Oboe through `internal/oboe`, whose symbols only exist
// with cgo enabled. The release matrix cross-compiles every GOOS with
// CGO_ENABLED=0, so an unguarded import broke the android leg of release builds
// (undefined: oboe.Play). oto_player_android.go supplies a disabled player for
// exactly that pair — android without cgo — and leaves the cgo (gomobile) build
// on the real driver.
//
//go:build !android || cgo

package voice

import (
	"errors"
	"sync"
	"time"

	"github.com/ebitengine/oto/v3"
)

// OtoPlayer provides low-latency, real-time PCM audio playback to speakers.
type OtoPlayer struct {
	mu       sync.Mutex
	stream   *audioBuffer
	otoCtx   *oto.Context
	player   *oto.Player
	ready    bool
	disabled bool
	volume   float64
}

// NewOtoPlayer creates a new 24kHz 16-bit PCM mono speaker player.
func NewOtoPlayer(sampleRate int, channels int) (*OtoPlayer, error) {
	if sampleRate <= 0 {
		sampleRate = 24000
	}
	if channels <= 0 {
		channels = 1
	}

	stream := newAudioBuffer()

	opts := &oto.NewContextOptions{
		SampleRate:   sampleRate,
		ChannelCount: channels,
		Format:       oto.FormatSignedInt16LE,
		BufferSize:   40 * time.Millisecond,
	}

	otoCtx, readyChan, err := oto.NewContext(opts)
	if err != nil {
		// Headless environment fallback (e.g. CI without soundcard)
		return &OtoPlayer{
			stream:   stream,
			disabled: true,
			volume:   1.0,
		}, nil
	}

	// Wait for context readiness or timeout
	select {
	case <-readyChan:
	case <-time.After(2 * time.Second):
		return &OtoPlayer{
			stream:   stream,
			disabled: true,
			volume:   1.0,
		}, nil
	}

	player := otoCtx.NewPlayer(stream)
	player.SetVolume(1.0)
	player.Play()

	return &OtoPlayer{
		stream: stream,
		otoCtx: otoCtx,
		player: player,
		ready:  true,
		volume: 1.0,
	}, nil
}

// Write enqueues 24kHz 16-bit PCM audio samples for speaker playback.
func (p *OtoPlayer) Write(pcm []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.disabled {
		return len(pcm), nil
	}
	if p.player != nil && !p.player.IsPlaying() {
		p.player.Play()
	}
	return p.stream.Write(pcm)
}

// Clear flushes the queued audio and resets the player hardware buffer immediately (Barge-in).
func (p *OtoPlayer) Clear() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.stream != nil {
		p.stream.Clear()
	}
	if p.player != nil {
		p.player.Reset()
	}
}

// Flush releases the pre-buffering gate so that any buffered audio immediately plays to completion.
func (p *OtoPlayer) Flush() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.stream != nil {
		p.stream.Flush()
	}
}

// Buffered returns the number of unplayed audio bytes currently queued in the stream.
func (p *OtoPlayer) Buffered() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stream != nil {
		return p.stream.Buffered()
	}
	return 0
}

// IsPlaying reports whether the player is currently outputting sound or has audio queued.
func (p *OtoPlayer) IsPlaying() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.disabled || p.player == nil {
		return false
	}
	if p.stream != nil && p.stream.Buffered() > 0 {
		return true
	}
	return p.player.IsPlaying()
}

// SetVolume adjusts the playback volume (0.0 to 1.0).
func (p *OtoPlayer) SetVolume(vol float64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if vol < 0 {
		vol = 0
	}
	if vol > 1.0 {
		vol = 1.0
	}
	p.volume = vol
	if p.player != nil {
		p.player.SetVolume(vol)
	}
}

// Volume returns the current volume level.
func (p *OtoPlayer) Volume() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.volume
}

// Close terminates audio playback and releases driver resources.
func (p *OtoPlayer) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var errs []error
	if p.stream != nil {
		if err := p.stream.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if p.player != nil {
		if err := p.player.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}
