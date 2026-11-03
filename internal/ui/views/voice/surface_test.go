package voice

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// surface_test.go — the state machine's own behaviour.
//
// The bytes are pinned in surface_golden_test.go; this file pins the
// transitions.

// TestSurface_NilReceiverIsSafe pins that a surface that was never
// built answers every question without a panic.
//
// The host builds its surface once, but a dozen host tests build a
// runMode by hand, and a runMode without a surface must still render,
// take a key and answer "no session".
func TestSurface_NilReceiverIsSafe(t *testing.T) {
	var s *Surface
	if s.Active() {
		t.Error("a nil surface must not be active")
	}
	if s.Phase() != PhaseNone {
		t.Errorf("a nil surface must be in phase %d, got %d", PhaseNone, s.Phase())
	}
	if s.Cancelled() {
		t.Error("a nil surface must not be cancelled")
	}
	if s.Recorder() != nil {
		t.Error("a nil surface must have no recorder")
	}
	if cmd := s.Start("/voice"); cmd != nil {
		t.Error("a nil surface must not arm anything")
	}
	if handled, cmd := s.HandleKey(tea.Key{Code: tea.KeyEsc}); handled || cmd != nil {
		t.Error("a nil surface must not claim a key")
	}
	if cmd := s.HandleConnectDone(ConnectDoneMsg{Err: errors.New("x")}); cmd != nil {
		t.Error("a nil surface must not answer a connection result")
	}
	if cmd := s.HandleTick(); cmd != nil {
		t.Error("a nil surface must not tick")
	}
	if s.View() != "" {
		t.Errorf("a nil surface must draw nothing, got %q", s.View())
	}
}

// TestSurface_PhaseTransitions pins the phase the reference walks:
// idle → connecting → recording → finishing.
func TestSurface_PhaseTransitions(t *testing.T) {
	s := New(Seams{Connect: func() error { return nil }})
	if s.Phase() != PhaseNone {
		t.Fatalf("a fresh surface is in phase %d, want %d", s.Phase(), PhaseNone)
	}
	if s.Active() {
		t.Error("a fresh surface must not be active")
	}

	cmd := s.Start("/voice")
	if cmd == nil {
		t.Fatal("Start returned no command")
	}
	if s.Phase() != PhaseConnecting {
		t.Fatalf("after Start the phase is %d, want %d", s.Phase(), PhaseConnecting)
	}
	if !s.Active() {
		t.Error("after Start the surface must be active")
	}

	msg, ok := cmd().(ConnectDoneMsg)
	if !ok {
		t.Fatalf("the command produced %T, want ConnectDoneMsg", cmd())
	}
	if msg.Err != nil {
		t.Fatalf("the connect command failed: %v", msg.Err)
	}
	if tick := s.HandleConnectDone(msg); tick == nil {
		t.Error("entering recording must arm the timer")
	}
	if s.Phase() != PhaseRecording {
		t.Fatalf("after the connection the phase is %d, want %d", s.Phase(), PhaseRecording)
	}

	// The recorder's cap moves the session to finishing.
	s.Recorder().Tick(MaxRecordingDuration)
	s.HandleTick()
	if s.Phase() != PhaseFinishing {
		t.Fatalf("after the cap the phase is %d, want %d", s.Phase(), PhaseFinishing)
	}
}

// TestSurface_TickOnlyWhileRecording pins that the timer only advances
// during a recording.
func TestSurface_TickOnlyWhileRecording(t *testing.T) {
	s := New(Seams{Connect: func() error { return nil }})
	if cmd := s.HandleTick(); cmd != nil {
		t.Error("an idle surface must not tick")
	}
	_ = s.Start("/voice")
	if cmd := s.HandleTick(); cmd != nil {
		t.Error("a connecting surface must not tick")
	}
	s.HandleConnectDone(ConnectDoneMsg{})
	if cmd := s.HandleTick(); cmd == nil {
		t.Error("a recording surface must tick")
	}
}

// TestSurface_CancelDropsLateConnection pins that a connection result
// that arrives after the user walked away is dropped.
func TestSurface_CancelDropsLateConnection(t *testing.T) {
	var replied []string
	s := New(Seams{
		Connect: func() error { return nil },
		Respond: func(msg string, extra ...string) { replied = append(replied, msg) },
		Width:   func() int { return 100 },
	})
	_ = s.Start("/voice")
	_, _ = s.HandleKey(tea.Key{Code: tea.KeyEsc})
	if !s.Cancelled() {
		t.Error("Esc must mark the session cancelled")
	}
	s.HandleConnectDone(ConnectDoneMsg{Err: errors.New("network failure")})
	if len(replied) != 0 {
		t.Errorf("a cancelled session must not report a failure, got %v", replied)
	}
	if s.Active() {
		t.Error("a cancelled session must not be active")
	}
}

// TestSurface_StartWhileActiveConfirms pins that arming the dictation
// again confirms it — the way the reference's KeyToggleDictation does.
func TestSurface_StartWhileActiveConfirms(t *testing.T) {
	var inserted []string
	s := New(Seams{
		Connect:     func() error { return nil },
		InsertDraft: func(text string) { inserted = append(inserted, text) },
	})
	_ = s.Start("/voice")
	s.HandleConnectDone(ConnectDoneMsg{})
	_ = s.Recorder().AppendDraft("hello")
	_ = s.Start("/voice")
	if s.Active() {
		t.Error("the second arm must confirm the session")
	}
	if len(inserted) != 1 || inserted[0] != "hello" {
		t.Errorf("the draft was inserted as %v, want [hello]", inserted)
	}
}

// TestSurface_FailureEchoesTheReferenceNotice pins the failure notice.
//
// The mic failure names the server it could not reach; every other
// failure carries the reference's own hint about the permission.
func TestSurface_FailureEchoesTheReferenceNotice(t *testing.T) {
	var replies [][]string
	s := New(Seams{
		Connect: func() error { return nil },
		Respond: func(msg string, extra ...string) {
			replies = append(replies, append([]string{msg}, extra...))
		},
	})

	_ = s.Start("/voice")
	s.HandleConnectDone(ConnectDoneMsg{Err: errors.New("cannot reach the mic server at 127.0.0.1:4713 (tcp): refused")})
	if len(replies) != 1 || len(replies[0]) != 1 {
		t.Fatalf("the mic failure produced %v, want one line", replies)
	}
	if !strings.HasPrefix(replies[0][0], "Could not start transcription: ") {
		t.Errorf("the mic failure reads %q", replies[0][0])
	}

	replies = nil
	_ = s.Start("/voice")
	s.HandleConnectDone(ConnectDoneMsg{Err: errors.New("rpc error: code = PermissionDenied")})
	if len(replies) != 1 || len(replies[0]) != 2 {
		t.Fatalf("the permission failure produced %v, want two lines", replies)
	}
	if !strings.Contains(replies[0][1], "Voice dictation needs a permission your current sign-in does not carry") {
		t.Errorf("the hint reads %q", replies[0][1])
	}
}
