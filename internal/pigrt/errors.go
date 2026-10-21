// errors.go — keeping the provider's error *type* across PiG's stream boundary.
//
// PiG copies only err.Error() out of a failing provider: the agent loop turns
// the transport error into an assistant message and the caller sees a fresh
// error whose concrete type is gone (agent_loop.go:494). The clone cares about
// the type: *backend.HTTPError is what api_error.go recovers with errors.As to
// pick the structured `AGY_ERROR: {...}` envelope and exit code 3.
//
// internal/agent solves this with an out-of-band errorSlot while it owns the
// loop (pig.go). Now that the turn can run on PiG's coding.Session instead, the
// same trick lives here: the provider is wrapped so a failed Stream records the
// original error before PiG flattens it, and Run reports that error rather than
// PiG's string.
package pigrt

import (
	"context"
	"sync"

	"github.com/MichaelKinsy/PiG/ai"
)

// ErrorSlot holds the most recent provider error, out of band. Zero value is
// ready to use. It is safe for concurrent use: the provider records from the
// session's goroutine and the caller reads after Send returns.
type ErrorSlot struct {
	mu  sync.Mutex
	err error
}

// Err returns the recorded error, or nil.
func (s *ErrorSlot) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *ErrorSlot) set(err error) {
	s.mu.Lock()
	s.err = err
	s.mu.Unlock()
}

// Reset clears the slot. Callers that run several turns can reset between them
// to tell a stale failure from a fresh one.
func (s *ErrorSlot) Reset() {
	s.mu.Lock()
	s.err = nil
	s.mu.Unlock()
}

// RecordProvider wraps inner so every failed Stream is recorded in slot. A nil
// inner or nil slot is returned unchanged, so callers can wrap unconditionally.
func RecordProvider(inner ai.Provider, slot *ErrorSlot) ai.Provider {
	if inner == nil || slot == nil {
		return inner
	}
	return &recordingProvider{inner: inner, slot: slot}
}

type recordingProvider struct {
	inner ai.Provider
	slot  *ErrorSlot
}

func (p *recordingProvider) ID() string   { return p.inner.ID() }
func (p *recordingProvider) Close() error { return p.inner.Close() }

func (p *recordingProvider) Stream(ctx context.Context, transcript ai.TranscriptContext, opts ai.StreamOptions) (*ai.AssistantMessageEventStream, error) {
	stream, err := p.inner.Stream(ctx, transcript, opts)
	if err != nil {
		p.slot.set(err)
	}
	return stream, err
}
