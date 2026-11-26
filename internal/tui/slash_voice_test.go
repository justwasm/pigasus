package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/internal/rcaudio"
	termcolor "github.com/justwasm/pigasus/internal/ui/render/color"
	"github.com/justwasm/pigasus/internal/ui/views/voice"
	voicebackend "github.com/justwasm/pigasus/internal/voice"
)

func TestSlashVoice_Subcommands(t *testing.T) {
	m := diffTestModel()
	m.audio.Surface = m.newVoiceSurface()

	// 1. /voice mute when idle
	handled, _ := m.slashVoice("mute")
	if !handled || m.audio.muted {
		t.Fatalf("expected audio.muted == false when live mode is not active")
	}

	// 2. /voice duplex
	handled, _ = m.slashVoice("duplex")
	if !handled || !m.audio.duplex {
		t.Fatalf("expected audio.duplex == true")
	}
	handled, _ = m.slashVoice("duplex off")
	if !handled || m.audio.duplex {
		t.Fatalf("expected audio.duplex == false")
	}
	handled, _ = m.slashVoice("duplex on")
	if !handled || !m.audio.duplex {
		t.Fatalf("expected audio.duplex == true")
	}

	// 3. /voice speed
	handled, _ = m.slashVoice("speed 1.25")
	if !handled || m.audio.speed != "1.25" {
		t.Fatalf("expected audio.speed == '1.25', got %q", m.audio.speed)
	}
	handled, _ = m.slashVoice("speed invalid")
	if !handled || m.audio.speed != "1.25" {
		t.Fatalf("expected audio.speed unchanged on invalid input")
	}

	// 4. /voice volume
	handled, _ = m.slashVoice("volume 80")
	if !handled || m.audio.volume != "80" {
		t.Fatalf("expected audio.volume == '80', got %q", m.audio.volume)
	}
	handled, _ = m.slashVoice("volume 150") // invalid, > 100
	if !handled || m.audio.volume != "80" {
		t.Fatalf("expected audio.volume unchanged on invalid input")
	}

	// 5. /voice speaker
	handled, _ = m.slashVoice("speaker finn")
	if !handled || m.audio.voice != "Finn" {
		t.Fatalf("expected audio.voice == 'Finn', got %q", m.audio.voice)
	}

	// 6. /voice <voice_name>
	voices := []string{"fola", "arlo", "cleo", "finn", "gero", "lora", "neno", "olin", "tari"}
	for _, v := range voices {
		handled, _ = m.slashVoice(v)
		wantCapital := strings.ToUpper(v[:1]) + v[1:]
		if !handled || m.audio.voice != wantCapital {
			t.Fatalf("expected audio.voice == %q, got %q", wantCapital, m.audio.voice)
		}
	}

	// 7. /voice stop when idle
	handled, _ = m.slashVoice("stop")
	if !handled {
		t.Fatalf("expected /voice stop handled")
	}

	// 8. Unknown option
	handled, _ = m.slashVoice("unknown_opt")
	if !handled {
		t.Fatalf("expected /voice unknown handled")
	}

	// 9. /voice preview routes into the recorded-sample audition path.
	handled, cmd := m.slashVoice("preview zephyr")
	if !handled {
		t.Fatal("expected /voice preview to be routed")
	}
	if cmd != nil {
		t.Fatalf("Zephyr has no recording, so no playback command should fire (got %T)", cmd())
	}
	if !strings.Contains(voiceResponseText(m), "no recorded preview") {
		t.Fatalf("expected the no-recording line, got %q", voiceResponseText(m))
	}
}

func TestSlashVoice_ThinkerHandoffDispatch(t *testing.T) {
	m := diffTestModel()
	mockBackend := voicebackend.NewMockLiveBackend()
	handler := m.NewLiveHandler()
	liveSess := voicebackend.NewNativeLiveSession(mockBackend, handler)
	m.setLiveSession(liveSess)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := liveSess.Start(ctx, voicebackend.LiveSessionConfig{Voice: "Finn"}); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	handler.OnThinkerHandoff("call_thinker_test_1", "Inspect code changes")

	// Allow background goroutine to execute
	time.Sleep(150 * time.Millisecond)

	lastSess := mockBackend.LastSession()
	if lastSess == nil {
		t.Fatal("expected mock session to exist")
	}

	// Verify tool response and notes were sent
	if err := liveSess.SendThinkerNote("Testing progress note"); err != nil {
		t.Fatalf("SendThinkerNote failed: %v", err)
	}

	if err := liveSess.SendThinkerAnswer("Testing answer"); err != nil {
		t.Fatalf("SendThinkerAnswer failed: %v", err)
	}
}

func TestSlashVoice_LiveComposerActiveAndKeybindings(t *testing.T) {
	t.Setenv("PIGASUS_MOCK_VOICE", "1")
	m := diffTestModel()
	initRootModelInput(m)
	m.prompt.input.Focus()
	m.audio.Surface = m.newVoiceSurface()

	cmd := m.audio.Surface.Start("/voice live")
	if cmd == nil {
		t.Fatal("expected non-nil cmd from Start('/voice live')")
	}
	msg := cmd()
	m.audio.Surface.HandleConnectDone(msg.(voice.ConnectDoneMsg))

	if !m.audio.Surface.Active() || m.audio.Surface.Phase() != voice.PhaseLive {
		t.Fatalf("expected PhaseLive, got active=%v, phase=%v", m.audio.Surface.Active(), m.audio.Surface.Phase())
	}

	// 1. Cursor must remain visible in composer during Live voice
	if !m.shouldShowInputCursor(0, 0) {
		t.Fatal("expected input cursor to be visible during PhaseLive")
	}

	// 2. Banner output format matches upstream agy (StatusRow decompiled ground truth)
	// On empty prompt: hint is "f6 mute · esc hang up"
	view := termcolor.StripSGR(m.audio.Surface.View())
	if !strings.Contains(view, "Live") || !strings.Contains(view, "listening") || !strings.Contains(view, "f6 mute") || !strings.Contains(view, "esc hang up") {
		t.Fatalf("expected upstream agy Live banner with 'f6 mute · esc hang up', got %q", view)
	}

	// Interim speech preview inside banner
	model, _ := m.Update(voiceUserSpeechInterimMsg{text: "previewing voice command"})
	m = model.(*RootModel)
	previewView := termcolor.StripSGR(m.audio.Surface.View())
	if !strings.Contains(previewView, "You: previewing voice command") {
		t.Fatalf("expected interim speech preview inside status bar, got %q", previewView)
	}

	// Committed user speech clears preview and adds to transcript
	model, _ = m.Update(voiceUserSpeechMsg{text: "previewing voice command"})
	m = model.(*RootModel)
	if m.audio.Surface.Preview() != "" {
		t.Fatalf("expected committed user speech to clear preview, got %q", m.audio.Surface.Preview())
	}

	// 3. Typed prompts still work: deliver 'h' and 'i' keys
	model, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: 'h', Text: "h"}))
	m = model.(*RootModel)
	model, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: 'i', Text: "i"}))
	m = model.(*RootModel)

	if !strings.Contains(m.prompt.input.Value(), "hi") {
		t.Fatalf("expected composer to receive typed keys 'hi', got %q", m.prompt.input.Value())
	}

	// 4. F6 mutes the microphone
	model, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyF6}))
	m = model.(*RootModel)
	if !m.IsLiveMuted() {
		t.Fatal("expected F6 to mute microphone")
	}
	mutedView := termcolor.StripSGR(m.audio.Surface.View())
	if !strings.Contains(mutedView, "muted") || !strings.Contains(mutedView, "f6 unmute") || !strings.Contains(mutedView, "f7 hang up") {
		t.Fatalf("expected muted banner on F6 with 'f6 unmute · f7 hang up', got %q", mutedView)
	}

	// F6 unmutes the microphone
	model, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyF6}))
	m = model.(*RootModel)
	if m.IsLiveMuted() {
		t.Fatal("expected F6 to unmute microphone")
	}

	// 5. Esc on non-empty prompt does NOT hang up: it arms or clears prompt
	model, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	m = model.(*RootModel)
	if !m.audio.Surface.Active() {
		t.Fatal("Esc on non-empty prompt should NOT hang up live voice session")
	}

	// Double Esc clears prompt
	model, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	m = model.(*RootModel)
	if m.prompt.input.Value() != "" {
		t.Fatalf("expected prompt to be cleared, got %q", m.prompt.input.Value())
	}
	if !m.audio.Surface.Active() {
		t.Fatal("clearing prompt should not hang up live session")
	}

	// 6. F7 hangs up the call!
	model, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyF7}))
	m = model.(*RootModel)
	if m.audio.Surface.Active() {
		t.Fatal("F7 key must hang up live session")
	}

	// 7. Verify transcript messages
	var hasStarted, hasStopped bool
	for _, row := range m.conversation.response {
		if strings.Contains(row, "Started voice chat") {
			hasStarted = true
		}
		if strings.Contains(row, "Stopped voice chat") {
			hasStopped = true
		}
	}
	if !hasStarted {
		t.Fatal("expected transcript to contain 'Started voice chat'")
	}
	if !hasStopped {
		t.Fatal("expected transcript to contain 'Stopped voice chat'")
	}
}

func TestSlashVoice_PreviewAuditionsRecordedSamples(t *testing.T) {
	var played []string
	restore := voicebackend.SetVoicePreviewPlayerForTest(func(ctx context.Context, path string) error {
		played = append(played, path)
		return nil
	})
	defer restore()

	m := diffTestModel()

	// 1. No argument lists the recorded personas and the usage line.
	handled, cmd := m.slashVoicePreview(nil)
	if !handled || cmd != nil {
		t.Fatalf("/voice preview with no args: handled=%v cmd=%v, want (true, nil)", handled, cmd)
	}
	last := voiceResponseText(m)
	if !strings.Contains(last, "Tari") || !strings.Contains(last, "/voice preview <name> [1-3|all]") {
		t.Fatalf("expected the recorded-persona listing, got %q", last)
	}

	// 2. A named persona plays exactly one clip and reports it.
	played = nil
	handled, cmd = m.slashVoicePreview([]string{"tari", "2"})
	if !handled || cmd == nil {
		t.Fatalf("/voice preview tari 2: handled=%v cmd=%v", handled, cmd)
	}
	if !strings.Contains(voiceResponseText(m), "Playing Tari (tari_2.mp3, sample 2/3)") {
		t.Fatalf("expected the playing line, got %q", voiceResponseText(m))
	}
	cmd()
	if len(played) != 1 || !strings.HasSuffix(played[0], "tari_2.mp3") {
		t.Fatalf("played = %v, want one tari_2.mp3 clip", played)
	}

	// 3. "all" plays every variant in order.
	played = nil
	handled, cmd = m.slashVoicePreview([]string{"finn", "all"})
	if !handled || cmd == nil {
		t.Fatalf("/voice preview finn all: handled=%v cmd=%v", handled, cmd)
	}
	cmd()
	if len(played) != voicebackend.VoicePreviewVariants {
		t.Fatalf("played %d clips, want %d", len(played), voicebackend.VoicePreviewVariants)
	}
	for i, want := range []string{"finn_1.mp3", "finn_2.mp3", "finn_3.mp3"} {
		if !strings.HasSuffix(played[i], want) {
			t.Fatalf("clip %d = %q, want suffix %q", i, played[i], want)
		}
	}

	// 4. A voice with no recording is refused without touching the player.
	played = nil
	handled, cmd = m.slashVoicePreview([]string{"zephyr"})
	if !handled || cmd != nil || len(played) != 0 {
		t.Fatalf("/voice preview zephyr: handled=%v cmd=%v played=%v", handled, cmd, played)
	}
	if !strings.Contains(voiceResponseText(m), "no recorded preview") {
		t.Fatalf("expected the no-recording error, got %q", voiceResponseText(m))
	}

	// 5. An out-of-range variant is refused.
	handled, cmd = m.slashVoicePreview([]string{"tari", "7"})
	if !handled || cmd != nil {
		t.Fatalf("variant 7 should be refused (handled=%v cmd=%v)", handled, cmd)
	}
	if !strings.Contains(voiceResponseText(m), "out of range") {
		t.Fatalf("expected an out-of-range line, got %q", voiceResponseText(m))
	}

	// 6. on/off is answered, not silently ignored.
	handled, cmd = m.slashVoicePreview([]string{"off"})
	if !handled || cmd != nil || !strings.Contains(voiceResponseText(m), "livechat") {
		t.Fatalf("/voice preview off: handled=%v cmd=%v msg=%q", handled, cmd, voiceResponseText(m))
	}
}

// voiceResponseText returns the slash responses accumulated on the model with
// SGR stripped, so assertions can match the spoken text.
func voiceResponseText(m *RootModel) string {
	var b strings.Builder
	for _, row := range m.conversation.response {
		b.WriteString(termcolor.StripSGR(row))
		b.WriteString("\n")
	}
	return b.String()
}

func TestLiveVoiceAvailability_OAuthToken(t *testing.T) {
	// Clear out any host environment variables that might influence the check.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("OPENAGY_CONFIG_HOME", t.TempDir())
	t.Setenv("OPENAGY_ACCOUNTS_CONFIG", filepath.Join(t.TempDir(), "accounts.json"))
	t.Setenv("ANTIGRAVITY_APP_DATA_DIR", t.TempDir())
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("OPENAGY_API_KEY", "")
	t.Setenv("PIGASUS_MOCK_VOICE", "")
	t.Setenv("OPENAGY_MOCK_VOICE", "")
	t.Setenv("JETSKI_OAUTH_TOKEN", "")
	t.Setenv("ANTIGRAVITY_ACCESS_TOKEN", "")

	m := diffTestModel()

	auth.LoadKeychainTokenFn = func() (*auth.AntigravityToken, error) {
		return nil, auth.ErrNoToken
	}
	defer func() { auth.LoadKeychainTokenFn = nil }()

	// 0. With no credentials and default voice endpoint, live voice is unavailable.
	if m.IsLiveVoiceAvailable() {
		t.Fatalf("expected IsLiveVoiceAvailable() == false with no credentials")
	}

	// 1. With OAuth token set via env, live voice should be available.
	t.Setenv("JETSKI_OAUTH_TOKEN", "ya29.test-oauth-token")
	if !m.IsLiveVoiceAvailable() {
		t.Fatalf("expected IsLiveVoiceAvailable() == true when JETSKI_OAUTH_TOKEN is set")
	}

	// 2. With API key set, live voice should be available.
	t.Setenv("JETSKI_OAUTH_TOKEN", "")
	t.Setenv("GEMINI_API_KEY", "test-api-key")
	if !m.IsLiveVoiceAvailable() {
		t.Fatalf("expected IsLiveVoiceAvailable() == true when GEMINI_API_KEY is set")
	}
}

func TestVoice_DictationTranscribeAndInsert(t *testing.T) {
	m := diffTestModel()
	initRootModelInput(m)
	m.prompt.input.Focus()
	m.audio.Surface = m.newVoiceSurface()

	// Mock transcriber
	m.audio.transcriber = rcaudio.TranscriberFunc(func(ctx context.Context, audio []byte, mimeType string) (string, error) {
		if len(audio) == 0 {
			return "", nil
		}
		return "transcribed test input", nil
	})

	// Start dictation
	cmd := m.audio.Surface.Start("/voice dictate")
	if cmd == nil {
		t.Fatal("expected non-nil cmd from Start('/voice dictate')")
	}
	msg := cmd()
	m.audio.Surface.HandleConnectDone(msg.(voice.ConnectDoneMsg))

	if !m.audio.Surface.Active() || m.audio.Surface.Phase() != voice.PhaseRecording {
		t.Fatalf("expected PhaseRecording, got active=%v phase=%v", m.audio.Surface.Active(), m.audio.Surface.Phase())
	}

	// Simulate recording some audio
	m.voiceBridge().SetDictationPCM([]byte{0x01, 0x02, 0x03, 0x04})

	// Confirm dictation (Enter or F5)
	handled, finishCmd := m.audio.Surface.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !handled || finishCmd == nil {
		t.Fatalf("expected Enter to trigger finish command: handled=%v cmd=%v", handled, finishCmd)
	}
	if m.audio.Surface.Phase() != voice.PhaseFinishing {
		t.Fatalf("expected PhaseFinishing during transcription, got %v", m.audio.Surface.Phase())
	}

	// Execute finishCmd to transcribe
	doneMsg := finishCmd()
	done, ok := doneMsg.(voice.DictationDoneMsg)
	if !ok {
		t.Fatalf("expected DictationDoneMsg, got %T", doneMsg)
	}
	if done.Text != "transcribed test input" {
		t.Fatalf("expected 'transcribed test input', got %q", done.Text)
	}

	// Update surface with DictationDoneMsg
	m.audio.Surface.HandleDictationDone(done)

	if m.audio.Surface.Active() {
		t.Fatal("expected surface to be inactive after dictation completes")
	}
	if got := m.prompt.input.Value(); got != "transcribed test input" {
		t.Fatalf("expected prompt input = %q, got %q", "transcribed test input", got)
	}
}

func TestVoice_DictationCancelOnEsc(t *testing.T) {
	m := diffTestModel()
	initRootModelInput(m)
	m.prompt.input.Focus()
	m.audio.Surface = m.newVoiceSurface()

	cmd := m.audio.Surface.Start("f5")
	if cmd == nil {
		t.Fatal("expected non-nil cmd from Start('f5')")
	}
	msg := cmd()
	m.audio.Surface.HandleConnectDone(msg.(voice.ConnectDoneMsg))

	if !m.audio.Surface.Active() || m.audio.Surface.Phase() != voice.PhaseRecording {
		t.Fatalf("expected PhaseRecording, got active=%v phase=%v", m.audio.Surface.Active(), m.audio.Surface.Phase())
	}

	// Esc cancels session and clears buffer
	m.voiceBridge().SetDictationPCM([]byte{0x01, 0x02})

	handled, _ := m.audio.Surface.HandleKey(tea.Key{Code: tea.KeyEsc})
	if !handled {
		t.Fatal("expected Esc to be handled")
	}
	if m.audio.Surface.Active() {
		t.Fatal("expected surface to be inactive after Esc")
	}
	if got := m.voiceBridge().DictationPCM(); len(got) != 0 {
		t.Fatalf("expected dictation PCM buffer to be cleared on Esc, got %d bytes", len(got))
	}
	if m.prompt.input.Value() != "" {
		t.Fatalf("expected empty prompt input, got %q", m.prompt.input.Value())
	}
}
