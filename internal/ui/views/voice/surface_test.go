package voice

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	termcolor "github.com/justwasm/pigasus/internal/ui/render/color"
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

func TestSurface_LiveMode(t *testing.T) {
	var liveStarted, liveStopped bool
	var muted bool
	s := New(Seams{
		StartLive: func() error {
			liveStarted = true
			return nil
		},
		StopLive: func() error {
			liveStopped = true
			return nil
		},
		ToggleMute: func() bool {
			muted = !muted
			return muted
		},
		IsMuted: func() bool {
			return muted
		},
		Width: func() int { return 80 },
	})

	cmd := s.Start("/voice live")
	if cmd == nil {
		t.Fatal("expected non-nil tea.Cmd from Start('/voice live')")
	}
	msg := cmd()
	s.HandleConnectDone(msg.(ConnectDoneMsg))

	if !s.Active() {
		t.Fatal("expected surface to be active in live mode")
	}
	if s.Phase() != PhaseLive {
		t.Fatalf("expected phase == PhaseLive (%d), got %d", PhaseLive, s.Phase())
	}
	if !liveStarted {
		t.Fatal("expected StartLive seam to be called")
	}

	view := termcolor.StripSGR(s.View())
	if !strings.Contains(view, "Live") || !strings.Contains(view, "f6 mute") || !strings.Contains(view, "listening") {
		t.Fatalf("expected Live banner, got %q", view)
	}

	// In live mode, regular typing ('a') should fall through to composer (handled=false)
	handledText, _ := s.HandleKey(tea.Key{Code: 'a'})
	if handledText {
		t.Fatal("expected regular typing to pass through to composer (handled=false)")
	}

	// Toggle mute via F6 key
	handled, _ := s.HandleKey(tea.Key{Code: tea.KeyF6})
	if !handled || !muted {
		t.Fatalf("expected F6 key to toggle mute, handled=%v, muted=%v", handled, muted)
	}
	viewMuted := termcolor.StripSGR(s.View())
	if !strings.Contains(viewMuted, "muted") || !strings.Contains(viewMuted, "f6 unmute") || !strings.Contains(viewMuted, "esc hang up") {
		t.Fatalf("expected muted banner with esc hang up on empty prompt, got %q", viewMuted)
	}

	// Dynamic hint when prompt is non-empty
	promptEmpty := false
	s.seams.PromptEmpty = func() bool { return promptEmpty }
	viewNonEmpty := termcolor.StripSGR(s.View())
	if !strings.Contains(viewNonEmpty, "f7 hang up") {
		t.Fatalf("expected f7 hang up on non-empty prompt, got %q", viewNonEmpty)
	}

	// Preview ticker
	s.SetPreview("checking files in repo")
	viewPreview := termcolor.StripSGR(s.View())
	if !strings.Contains(viewPreview, "You: checking files in repo") {
		t.Fatalf("expected speech preview in banner, got %q", viewPreview)
	}

	// Hang up via F7 key
	handled, _ = s.HandleKey(tea.Key{Code: tea.KeyF7})
	if !handled || s.Active() {
		t.Fatalf("expected F7 to hang up live session, active=%v", s.Active())
	}
	if !liveStopped {
		t.Fatal("expected StopLive seam to be called")
	}
}

func TestSurface_BareVoiceLiveFallbackDictation(t *testing.T) {
	var liveAttempted, dictConnected bool
	s := New(Seams{
		IsLiveAvailable: func() bool { return true },
		StartLive: func() error {
			liveAttempted = true
			return errors.New("Live mode isn't available in this session.")
		},
		Connect: func() error {
			dictConnected = true
			return nil
		},
		Width: func() int { return 80 },
	})

	cmd := s.Start("/voice")
	if cmd == nil {
		t.Fatal("expected non-nil tea.Cmd from Start('/voice')")
	}
	msg := cmd()
	s.HandleConnectDone(msg.(ConnectDoneMsg))

	if !liveAttempted {
		t.Fatal("expected StartLive to be attempted first")
	}
	if !dictConnected {
		t.Fatal("expected Connect (dictation) to be called as fallback")
	}
	if s.Phase() != PhaseRecording {
		t.Fatalf("expected phase to be PhaseRecording after fallback, got %v", s.Phase())
	}
}
