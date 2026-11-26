// turn_error_observer_test.go — the dispatcher→web-UI prepaid notice seam.
//
// The 1.5.17 bullet ends with "(with an `Organization prepaid wallet is empty`
// notice in the Remote Control browser view)". The notice already existed in
// rcfrontend and was already testable; what was missing was the wire between
// the place the failure is observed (the dispatcher's turn loop) and the place
// the notice lives (the web server's state).
//
// These tests pin the dispatcher half. rcfrontend's prepaid_balance_test.go
// pins the other end.

package remotecontrol

import (
	"errors"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/remotecontrol/cascades"
)

// TestSetTurnErrorObserver_InvokedOnFailedTurn pins that the dispatcher tells
// the observer about a failed turn, handing it the original error.
func TestSetTurnErrorObserver_InvokedOnFailedTurn(t *testing.T) {
	d := newTestDispatcher(t)
	var seen []error
	d.SetTurnErrorObserver(func(err error) { seen = append(seen, err) })

	boom := errors.New("backend exploded")
	d.renderSendTurnError(nil, "cascade-1", boom, nopSink{})

	if len(seen) != 1 {
		t.Fatalf("observer called %d times, want 1", len(seen))
	}
	if !errors.Is(seen[0], boom) {
		t.Errorf("observer saw %v, want the turn's own error", seen[0])
	}
}

// TestSetTurnErrorObserver_ClearingStopsCalls pins that a nil observer detaches.
//
// Without this a host that shuts the web server down keeps a closure writing
// into state nobody serves any more.
func TestSetTurnErrorObserver_ClearingStopsCalls(t *testing.T) {
	d := newTestDispatcher(t)
	calls := 0
	d.SetTurnErrorObserver(func(error) { calls++ })
	d.SetTurnErrorObserver(nil)

	d.renderSendTurnError(nil, "cascade-1", errors.New("boom"), nopSink{})

	if calls != 0 {
		t.Errorf("observer called %d times after being cleared, want 0", calls)
	}
}

// TestSetTurnErrorObserver_NilDispatcherIsSafe pins the nil guard, because the
// in-process and Unix-socket paths never construct a dispatcher with a web UI.
func TestSetTurnErrorObserver_NilDispatcherIsSafe(t *testing.T) {
	var d *Dispatcher
	d.SetTurnErrorObserver(func(error) {}) // must not panic
	if got := d.turnErrorObserver(); got != nil {
		t.Error("nil dispatcher returned a non-nil observer")
	}
}

// TestTurnErrorObserver_IsNotCalledOnSuccess pins that a working turn leaves
// the observer alone.
//
// The observer classifies an error; calling it on success would be harmless but
// would also mean a future wiring mistake - attaching the observer to the wrong
// call - could not be detected by this suite.
func TestTurnErrorObserver_IsNotCalledOnSuccess(t *testing.T) {
	d := newTestDispatcher(t)
	calls := 0
	d.SetTurnErrorObserver(func(error) { calls++ })

	_ = d.renderSendTurnResult(cascades.Result{}, time.Now())

	if calls != 0 {
		t.Errorf("observer called %d times on a successful turn, want 0", calls)
	}
}

// newTestDispatcher builds a dispatcher with the minimum config the error path
// needs, failing the test rather than returning the error.
func newTestDispatcher(t *testing.T) *Dispatcher {
	t.Helper()
	sess := NewSessionState("test")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	return d
}

// nopSink is the minimum EventSink the error path touches.
type nopSink struct{}

func (nopSink) OnState(*cascades.Cascade, cascades.StateFrame)       {}
func (nopSink) OnTextDelta(*cascades.Cascade, string)                {}
func (nopSink) OnToolCallStart(*cascades.Cascade, cascades.ToolCall) {}
func (nopSink) OnToolCall(*cascades.Cascade, cascades.ToolCall)      {}
func (nopSink) OnError(*cascades.Cascade, error)                     {}
func (nopSink) OnUserInteraction(*cascades.Cascade, *cascades.PendingInteraction) {
}
