package voice

// audio_buffer.go — the jitter buffer behind the live-voice speaker.
//
// It is pure Go and carries no build constraint: the android stub player
// (oto_player_android.go) owns the same buffer, so its behaviour — pre-buffering,
// flush, barge-in clear — is identical on a platform whose sound card backend
// the cgo-free build cannot reach.

import (
	"io"
	"sync"
)

// audioBuffer is a thread-safe streaming buffer designed for real-time PCM audio playback.
// It features an adaptive jitter buffer to smooth out packet arrival latency and prevent
// audio underflow glitches, while supporting instant clearing for barge-in (interruption).
type audioBuffer struct {
	mu                sync.Mutex
	cond              *sync.Cond
	buf               []byte
	prebuffering      bool
	minPrebufferBytes int
	closed            bool
}

func newAudioBuffer() *audioBuffer {
	b := &audioBuffer{
		prebuffering:      true,
		minPrebufferBytes: 9600, // 200ms of 24kHz 16-bit mono audio (48,000 bytes/sec)
	}
	b.cond = sync.NewCond(&b.mu)
	return b
}

func (b *audioBuffer) Read(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed && len(b.buf) == 0 {
		return 0, io.EOF
	}

	if len(b.buf) == 0 {
		// Buffer ran dry; enter pre-buffering state for next burst to prevent stutter
		b.prebuffering = true
		clear(p)
		return len(p), nil
	}

	// While pre-buffering, accumulate a safety window to absorb network jitter
	if b.prebuffering {
		if len(b.buf) < b.minPrebufferBytes {
			clear(p)
			return len(p), nil
		}
		b.prebuffering = false
	}

	n := copy(p, b.buf)
	b.buf = b.buf[n:]
	if len(b.buf) == 0 {
		b.prebuffering = true
	}
	return n, nil
}

func (b *audioBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return 0, io.ErrClosedPipe
	}
	b.buf = append(b.buf, p...)
	b.cond.Signal()
	return len(p), nil
}

// Flush bypasses the pre-buffering gate so trailing audio is immediately drained.
func (b *audioBuffer) Flush() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.prebuffering = false
	b.cond.Signal()
}

func (b *audioBuffer) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = b.buf[:0]
	b.prebuffering = true
}

func (b *audioBuffer) Buffered() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.buf)
}

func (b *audioBuffer) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	b.cond.Broadcast()
	return nil
}
