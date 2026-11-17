// oto_player_android.go — the speaker player for an android build without cgo.
//
// oto's android backend drives Oboe through `internal/oboe`, and those symbols
// (oboe.Play, oboe.Suspend, oboe.Resume) exist only with cgo. The release
// matrix cross-compiles every GOOS with CGO_ENABLED=0, so `go build ./cmd/...`
// for android/arm64 failed with `undefined: oboe.Play` the moment the live
// voice session gained a speaker.
//
// The speaker is not something a cgo-free android build can reach, and the live
// voice session already tolerates a silent device: NewOtoPlayer answers a
// disabled player when there is no sound card (headless CI, a container). This
// file gives that same answer without the driver, and keeps the type's surface
// identical so every caller compiles unchanged. Nothing is queued, and every
// method is safe to call.
//
//go:build android && !cgo

package voice

import "sync"

// OtoPlayer is the disabled speaker handle a cgo-free android build carries.
// The field set matches the driven player's so a value built here cannot be
// mistaken for one that owns a driver.
type OtoPlayer struct {
	mu       sync.Mutex
	stream   *audioBuffer
	disabled bool
	volume   float64
}

// NewOtoPlayer returns a disabled player: the platform backend needs cgo, so
// this build has no sound card to write to. The signature matches the driven
// player's, and no error is returned — a device the build cannot reach is the
// same state as a machine without one, which the caller already handles.
func NewOtoPlayer(sampleRate int, channels int) (*OtoPlayer, error) {
	return &OtoPlayer{
		stream:   newAudioBuffer(),
		disabled: true,
		volume:   1.0,
	}, nil
}

// Write discards the samples and reports them accepted, the contract the
// driven player keeps for a disabled device.
func (p *OtoPlayer) Write(pcm []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(pcm), nil
}

// Clear resets the buffer — a barge-in on a silent device still has to drop
// whatever the session had queued.
func (p *OtoPlayer) Clear() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stream != nil {
		p.stream.Clear()
	}
}

// Flush releases the pre-buffering gate.
func (p *OtoPlayer) Flush() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stream != nil {
		p.stream.Flush()
	}
}

// Buffered reports the queued bytes, which is always none here.
func (p *OtoPlayer) Buffered() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return 0
}

// IsPlaying reports whether audio is queued; a disabled player never is.
func (p *OtoPlayer) IsPlaying() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return false
}

// SetVolume records the level so Volume round-trips, clamped the way the
// driven player clamps it.
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
}

// Volume returns the recorded level.
func (p *OtoPlayer) Volume() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.volume
}

// Close releases the buffer.
func (p *OtoPlayer) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stream != nil {
		return p.stream.Close()
	}
	return nil
}
