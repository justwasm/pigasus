package voice

import (
	"fmt"
	"sync"
	"sync/atomic"
)

type captureDriver interface {
	Start() error
	Stop() error
	Close() error
}

// AudioCapture records 16kHz mono 16-bit PCM audio from the microphone.
type AudioCapture struct {
	mu       sync.Mutex
	driver   captureDriver
	onAudio  func(pcm16k []byte)
	muted    atomic.Bool
	running  atomic.Bool
	disabled bool
}

// NewAudioCapture creates a microphone capture device sampling at sampleRate (default 16000Hz).
func NewAudioCapture(sampleRate int, onAudio func(pcm16k []byte)) (*AudioCapture, error) {
	if sampleRate <= 0 {
		sampleRate = 16000
	}

	cap := &AudioCapture{
		onAudio: onAudio,
	}

	drv, err := initCaptureDriver(sampleRate, func(pcm []byte) {
		if !cap.running.Load() || cap.muted.Load() {
			return
		}
		if cap.onAudio != nil && len(pcm) > 0 {
			cap.onAudio(pcm)
		}
	})
	if err != nil {
		cap.disabled = true
		return cap, nil
	}
	cap.driver = drv
	return cap, nil
}

// Start begins capturing microphone input.
func (c *AudioCapture) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.disabled || c.driver == nil {
		c.running.Store(true)
		return nil
	}
	if err := c.driver.Start(); err != nil {
		return fmt.Errorf("audio capture: start: %w", err)
	}
	c.running.Store(true)
	return nil
}

// Stop stops capturing audio.
func (c *AudioCapture) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.running.Store(false)
	if c.disabled || c.driver == nil {
		return nil
	}
	return c.driver.Stop()
}

// SetMuted controls whether microphone packets are forwarded.
func (c *AudioCapture) SetMuted(muted bool) {
	c.muted.Store(muted)
}

// ToggleMute toggles the mute state and returns the new state.
func (c *AudioCapture) ToggleMute() bool {
	newVal := !c.muted.Load()
	c.muted.Store(newVal)
	return newVal
}

// IsMuted reports whether the microphone is muted.
func (c *AudioCapture) IsMuted() bool {
	return c.muted.Load()
}

// IsRunning reports whether recording is active.
func (c *AudioCapture) IsRunning() bool {
	return c.running.Load()
}

// Close releases microphone audio devices and resources.
func (c *AudioCapture) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.running.Store(false)
	if c.driver != nil {
		return c.driver.Close()
	}
	return nil
}
