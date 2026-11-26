package voicebridge

import (
	"context"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/internal/ui/views/voice"
	voicebackend "github.com/justwasm/pigasus/internal/voice"
)

// ReadEnv samples the process environment.
func ReadEnv() Env {
	return Env{
		MicAddr:     strings.TrimSpace(os.Getenv("ANTIGRAVITY_MIC")),
		ForceAPIKey: os.Getenv("OPENAGY_VOICE_FORCE_API_KEY") != "",
		AltBackend:  strings.TrimSpace(os.Getenv("OPENAGY_VOICE_ALT_BACKEND")),
		MockVoice:   os.Getenv("PIGASUS_MOCK_VOICE") != "" || os.Getenv("OPENAGY_MOCK_VOICE") != "",
	}
}

// capture is the dictation recorder: either the local device or a remote
// microphone server. Both are interfaces so a test drives PCM through the
// bridge without touching real audio hardware.
type capture interface {
	Start() error
	Stop() error
	Close() error
}

type remoteCapture interface {
	Close() error
}

// DialRemoteAudioFunc dials a JETSKI-AUDIO/1 microphone server and streams
// its audio into onAudio. It is injectable so tests never open a socket.
type DialRemoteAudioFunc func(addr string, onAudio func([]byte)) (remoteCapture, error)

// NewAudioCaptureFunc opens the local microphone at the given sample rate.
type NewAudioCaptureFunc func(rate int, onAudio func([]byte)) (capture, error)

// DialFunc probes a remote microphone server before capture starts.
type DialFunc func(network, addr string, timeout time.Duration) (net.Conn, error)

// PCM is the recorded audio. The capture callbacks run on the audio
// goroutine while the update loop reads and clears the buffer, so the buffer
// is guarded.
type PCM struct {
	mu   sync.Mutex
	data []byte
}

func (p *PCM) append(b []byte) {
	p.mu.Lock()
	p.data = append(p.data, b...)
	p.mu.Unlock()
}

func (p *PCM) take() []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := p.data
	p.data = nil
	return out
}

func (p *PCM) drop() {
	p.mu.Lock()
	p.data = nil
	p.mu.Unlock()
}

// Recorder owns the dictation capture and the PCM buffer.
//
// It is the half of the bridge that touches audio devices; the ports stay
// free of them, so a scenario test swaps the two constructors and drives the
// same state machine with synthetic PCM.
type Recorder struct {
	// Dial probes the remote microphone server. Defaults to
	// net.DialTimeout.
	Dial DialFunc
	// DialRemoteAudio connects to a remote microphone server. Defaults to
	// voicebackend.DialRemoteAudio.
	DialRemoteAudio DialRemoteAudioFunc
	// NewAudioCapture opens the local microphone. Defaults to
	// voicebackend.NewAudioCapture.
	NewAudioCapture NewAudioCaptureFunc
}

func (r *Recorder) dial() DialFunc {
	if r != nil && r.Dial != nil {
		return r.Dial
	}
	return net.DialTimeout
}

func (r *Recorder) dialRemote() DialRemoteAudioFunc {
	if r != nil && r.DialRemoteAudio != nil {
		return r.DialRemoteAudio
	}
	return func(addr string, onAudio func([]byte)) (remoteCapture, error) {
		return voicebackend.DialRemoteAudio(addr, onAudio)
	}
}

func (r *Recorder) newCapture() NewAudioCaptureFunc {
	if r != nil && r.NewAudioCapture != nil {
		return r.NewAudioCapture
	}
	return func(rate int, onAudio func([]byte)) (capture, error) {
		return voicebackend.NewAudioCapture(rate, onAudio)
	}
}

// ProbeMic checks that the remote microphone server at addr answers. An empty
// address is not an error: there is no remote server to reach.
func (r *Recorder) ProbeMic(addr string) error {
	if addr == "" {
		return nil
	}
	conn, err := r.dial()("tcp", addr, 2*time.Second)
	if err != nil {
		return err
	}
	return conn.Close()
}

// Start begins buffering audio into pcm, preferring the remote microphone
// server when micAddr names one. A remote server that refuses the connection
// falls back to the local device; a local device that cannot open is
// reported. The opened device is returned so the caller can close it.
func (r *Recorder) Start(micAddr string, pcm *PCM) (device, error) {
	pcm.drop()

	if micAddr != "" {
		stream, err := r.dialRemote()(micAddr, pcm.append)
		if err == nil {
			return device{remote: stream}, nil
		}
	}
	c, err := r.newCapture()(16000, pcm.append)
	if err != nil {
		return device{}, err
	}
	if err := c.Start(); err != nil {
		_ = c.Close()
		return device{}, err
	}
	return device{cap: c}, nil
}

// Stop closes the open device.
func (r *Recorder) Stop(d device) { d.close() }

// device is the open capture the bridge holds between Start and Stop.
type device struct {
	cap    capture
	remote remoteCapture
}

func (d device) close() {
	if d.remote != nil {
		_ = d.remote.Close()
	}
	if d.cap != nil {
		_ = d.cap.Stop()
		_ = d.cap.Close()
	}
}

// Dictation is the finished-recording command: it transcribes off the update
// loop and answers the surface's completion message.
type Dictation func() voice.DictationDoneMsg

// TranscriberFunc adapts a function to the Transcriber port.
type TranscriberFunc func(ctx context.Context, audio []byte, mimeType string) (string, error)

// Transcribe calls the wrapped function.
func (f TranscriberFunc) Transcribe(ctx context.Context, audio []byte, mimeType string) (string, error) {
	return f(ctx, audio, mimeType)
}

// Transcribe turns recorded PCM into text under a two-minute bound.
func Transcribe(ctx context.Context, t Transcriber, pcm []byte) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	return t.Transcribe(ctx, pcm, "audio/pcm;rate=16000")
}
