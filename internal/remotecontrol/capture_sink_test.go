// The capture sink: a test double for the cascade domain's
// EventSink that records every event it is handed.
//
// It lives here, in the parent package, because the production
// PiG-backed backend lives here too: the backend's own tests
// (piagentbackend_test.go) drive a turn through this sink and
// assert on what the backend emitted.
//
// The cascade domain's own tests carry a copy of the same
// fixture (internal/remotecontrol/cascades/cascade_test.go):
// the domain package must not depend on the parent package, so
// the double cannot be shared across the boundary. The two
// copies answer the same interface — cascades.EventSink — and
// only the package they live in differs.
package remotecontrol

import (
	"sync"

	"github.com/justwasm/pigasus/internal/remotecontrol/cascades"
)

// captureSink captures every call the dispatcher drives through
// it. Nil-safe (every method is a no-op on a nil receiver) so a
// future test that omits construction doesn't crash.
type captureSink struct {
	mu           sync.Mutex
	states       []cascades.StateFrame
	deltas       []string
	tools        []cascades.ToolCall
	errors       []error
	interactions []*cascades.PendingInteraction
}

func (s *captureSink) OnState(_ *cascades.Cascade, f cascades.StateFrame) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.states = append(s.states, f)
	s.mu.Unlock()
}

func (s *captureSink) OnTextDelta(_ *cascades.Cascade, d string) {
	if s == nil || d == "" {
		return
	}
	s.mu.Lock()
	s.deltas = append(s.deltas, d)
	s.mu.Unlock()
}

func (s *captureSink) OnToolCall(_ *cascades.Cascade, c cascades.ToolCall) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.tools = append(s.tools, c)
	s.mu.Unlock()
}

func (s *captureSink) OnError(_ *cascades.Cascade, err error) {
	if s == nil || err == nil {
		return
	}
	s.mu.Lock()
	s.errors = append(s.errors, err)
	s.mu.Unlock()
}

// OnUserInteraction captures the interaction pointer so
// rc-handle-user-interaction tests can assert the sink
// saw it.
func (s *captureSink) OnUserInteraction(_ *cascades.Cascade, intr *cascades.PendingInteraction) {
	if s == nil || intr == nil {
		return
	}
	s.mu.Lock()
	s.interactions = append(s.interactions, intr)
	s.mu.Unlock()
}
