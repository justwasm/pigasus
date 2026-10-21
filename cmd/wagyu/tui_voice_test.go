package main

import (
	"errors"
	"flag"
	"net"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/voice"
)

func newVoiceTestMode(t *testing.T) *runMode {
	t.Helper()
	m := newSlashModel(t)
	m.width = 100
	m.height = 30
	return m
}

func TestVoice_PrintModeRejection(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, cmd := range []string{"voice", "record"} {
		res := dispatchPrintSlash("/"+cmd, fs)
		if !res.Handled {
			t.Fatalf("dispatchPrintSlash(/%s) should be handled", cmd)
		}
		if res.Err == nil {
			t.Fatalf("dispatchPrintSlash(/%s) should return an error", cmd)
		}
		code, ok := cli.ExitCodeOf(res.Err)
		if !ok || code != 2 {
			t.Errorf("expected exit code 2, got %d (ok=%v)", code, ok)
		}
		want := "error: /" + cmd + " is not available in print mode (it starts interactive dictation and records from a microphone); pass --disable-slash-commands to send /" + cmd + " to the model as literal text"
		if strings.TrimSpace(res.Err.Error()) != want {
			t.Errorf("dispatchPrintSlash(/%s) = %q, want %q", cmd, res.Err.Error(), want)
		}
	}
}

func TestVoice_StartConnectingBanner(t *testing.T) {
	m := newVoiceTestMode(t)
	cmd := m.startVoiceDictation("/voice")
	if cmd == nil {
		t.Fatal("startVoiceDictation returned nil cmd")
	}
	if !m.voiceMode {
		t.Fatal("expected voiceMode == true")
	}
	if m.voicePhase != voicePhaseConnecting {
		t.Fatalf("expected voicePhase == %d (Connecting), got %d", voicePhaseConnecting, m.voicePhase)
	}

	banner := m.voiceView()
	plain := stripSGR(banner)
	if !strings.Contains(plain, "Connecting...") {
		t.Errorf("banner should contain 'Connecting...', got %q", plain)
	}
	if !strings.Contains(plain, "esc to cancel") {
		t.Errorf("banner should contain 'esc to cancel', got %q", plain)
	}
}

func TestVoice_EscCancelsConnecting(t *testing.T) {
	m := newVoiceTestMode(t)
	_ = m.startVoiceDictation("/voice")

	handled, _ := m.handleVoiceKey(tea.Key{Code: tea.KeyEsc})
	if !handled {
		t.Fatal("expected Esc to be handled by voice mode")
	}
	if m.voiceMode {
		t.Fatal("expected voiceMode == false after Esc")
	}
	if !m.voiceCancel {
		t.Fatal("expected voiceCancel == true")
	}

	// Simulating delayed error after cancellation: should NOT output error to response
	respLen := len(m.response)
	m.handleVoiceConnectDone(voiceConnectDoneMsg{err: errors.New("network failure")})
	if len(m.response) != respLen {
		t.Errorf("response should not be appended after cancellation; got %v", m.response)
	}
}

func TestVoice_FailedConnectionSurfacesPermissionDenied(t *testing.T) {
	m := newVoiceTestMode(t)
	_ = m.startVoiceDictation("/voice")

	// Trigger connection failure
	m.handleVoiceConnectDone(voiceConnectDoneMsg{
		err: errors.New("transcription failed: failed to receive setup response: rpc error: code = PermissionDenied desc = Request had insufficient authentication scopes."),
	})

	if m.voiceMode {
		t.Fatal("voiceMode should be false after connection failure")
	}
	if len(m.response) < 2 {
		t.Fatalf("expected response rows for /voice failure, got %v", m.response)
	}
	msgText := strings.Join(m.response, "\n")
	if !strings.Contains(msgText, "Could not start transcription") {
		t.Errorf("response missing 'Could not start transcription': %s", msgText)
	}
	if !strings.Contains(msgText, "Voice dictation needs a permission your current sign-in does not carry") {
		t.Errorf("response missing hint text: %s", msgText)
	}
}

func TestVoice_RecordingBannerAndTimer(t *testing.T) {
	m := newVoiceTestMode(t)
	m.voiceConnectFn = func() error { return nil }
	_ = m.startVoiceDictation("/voice")

	// Simulate successful connection
	m.handleVoiceConnectDone(voiceConnectDoneMsg{err: nil})

	if !m.voiceMode || m.voicePhase != voicePhaseRecording {
		t.Fatalf("expected voicePhase == %d (Recording), got %d", voicePhaseRecording, m.voicePhase)
	}

	banner := m.voiceView()
	plain := stripSGR(banner)
	if !strings.Contains(plain, "Recording 00:00") {
		t.Errorf("banner missing 'Recording 00:00': %q", plain)
	}
	if !strings.Contains(plain, "enter or f5 to insert") {
		t.Errorf("banner missing 'enter or f5 to insert': %q", plain)
	}
	if !strings.Contains(plain, "esc to discard") {
		t.Errorf("banner missing 'esc to discard': %q", plain)
	}

	// Tick 5 seconds
	for i := 0; i < 5; i++ {
		m.handleVoiceTick()
	}
	banner5s := stripSGR(m.voiceView())
	if !strings.Contains(banner5s, "Recording 00:05") {
		t.Errorf("banner missing 'Recording 00:05' after 5 ticks: %q", banner5s)
	}
}

func TestVoice_FinishingAfterMaxDuration(t *testing.T) {
	m := newVoiceTestMode(t)
	m.voiceConnectFn = func() error { return nil }
	_ = m.startVoiceDictation("/voice")
	m.handleVoiceConnectDone(voiceConnectDoneMsg{err: nil})

	// Advance past 3m30s
	m.voiceRecorder.Tick(voice.MaxRecordingDuration)
	m.handleVoiceTick()

	if m.voicePhase != voicePhaseFinishing {
		t.Fatalf("expected voicePhase == %d (Finishing), got %d", voicePhaseFinishing, m.voicePhase)
	}

	banner := stripSGR(m.voiceView())
	if !strings.Contains(banner, "Finishing up...") {
		t.Errorf("banner missing 'Finishing up...': %q", banner)
	}
	if !strings.Contains(banner, "enter or f5 to insert what you have") {
		t.Errorf("banner missing 'enter or f5 to insert what you have': %q", banner)
	}
}

func TestVoice_EnterOrF5InsertsDraft(t *testing.T) {
	m := newVoiceTestMode(t)
	m.voiceConnectFn = func() error { return nil }
	_ = m.startVoiceDictation("f5")
	m.handleVoiceConnectDone(voiceConnectDoneMsg{err: nil})

	// Append transcribed text to draft
	_ = m.voiceRecorder.AppendDraft("hello from voice dictation")

	// Press F5 to confirm insertion
	handled, _ := m.handleVoiceKey(tea.Key{Code: tea.KeyF5})
	if !handled {
		t.Fatal("expected F5 to be handled")
	}
	if m.voiceMode {
		t.Fatal("expected voiceMode == false after confirmation")
	}

	got := m.input.Value()
	if got != "hello from voice dictation" {
		t.Errorf("input value = %q, want 'hello from voice dictation'", got)
	}
}

func TestVoice_F5KeyDispatchesVoiceDictation(t *testing.T) {
	m := newVoiceTestMode(t)
	if m.voiceMode {
		t.Fatal("voiceMode should be false initially")
	}

	// Send F5 key to Update
	model, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyF5}))
	updated := model.(*runMode)
	if !updated.voiceMode {
		t.Fatal("pressing F5 should activate voiceMode")
	}
	if cmd == nil {
		t.Fatal("expected connect cmd returned by F5 press")
	}
}

func TestVoice_AntigravityMicSSHRemote(t *testing.T) {
	// Case 1: ANTIGRAVITY_MIC set to unreachable address
	t.Setenv("ANTIGRAVITY_MIC", "127.0.0.1:54321")
	m := &runMode{}
	cmd := m.connectVoiceCmd()
	msg := cmd().(voiceConnectDoneMsg)
	if msg.err == nil || !strings.Contains(msg.err.Error(), "cannot reach the mic server at 127.0.0.1:54321 (tcp)") {
		t.Fatalf("expected cannot reach mic server error, got: %v", msg.err)
	}

	// Case 2: ANTIGRAVITY_MIC set to active TCP listener (e.g. forwarded mic-serve over SSH)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	t.Setenv("ANTIGRAVITY_MIC", ln.Addr().String())
	cmd2 := m.connectVoiceCmd()
	msg2 := cmd2().(voiceConnectDoneMsg)
	// Connected to mic server, now proceeds into recording mode
	if msg2.err != nil {
		t.Fatalf("expected nil error after reaching mic server, got: %v", msg2.err)
	}
}

func TestVoice_LiveRecordingFlow(t *testing.T) {
	m := newVoiceTestMode(t)
	t.Setenv("ANTIGRAVITY_MIC", "")

	// 1. Trigger /voice
	cmd := m.startVoiceDictation("/voice")
	if !m.voiceMode || m.voicePhase != voicePhaseConnecting {
		t.Fatalf("expected voicePhaseConnecting, got phase=%d", m.voicePhase)
	}

	// 2. Connect succeeds
	msg := cmd().(voiceConnectDoneMsg)
	if msg.err != nil {
		t.Fatalf("expected nil connect error, got: %v", msg.err)
	}
	_, tickCmd := m.handleVoiceConnectDone(msg)
	if !m.voiceMode || m.voicePhase != voicePhaseRecording {
		t.Fatalf("expected voicePhaseRecording, got phase=%d", m.voicePhase)
	}
	if tickCmd == nil {
		t.Fatal("expected non-nil tickCmd after entering recording")
	}

	// 3. Render banner
	banner := m.voiceView()
	if !strings.Contains(banner, "Recording 00:00") {
		t.Errorf("banner missing initial timer: %q", banner)
	}
	if !strings.Contains(banner, "enter or f5 to insert") {
		t.Errorf("banner missing instructions: %q", banner)
	}

	// 4. Tick timer
	m.handleVoiceTick()
	bannerTick := m.voiceView()
	if !strings.Contains(bannerTick, "Recording 00:01") {
		t.Errorf("banner missing updated timer: %q", bannerTick)
	}

	// 5. Press Enter or F5 to confirm
	handled, _ := m.handleVoiceKey(tea.Key{Code: tea.KeyEnter})
	if !handled || m.voiceMode {
		t.Fatal("expected Enter to confirm and leave voiceMode")
	}
}

