package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/tui/panels"
	termcolor "github.com/justwasm/pigasus/internal/ui/render/color"
	"github.com/justwasm/pigasus/internal/ui/views/voice"
	voicebackend "github.com/justwasm/pigasus/internal/voice"
)

func TestVoiceLiveStream_UserSpeech(t *testing.T) {
	m := &RootModel{
		viewport: uiState{width: 80},
	}
	m.handleVoiceUserSpeech("hello world")

	if len(m.conversation.response) == 0 {
		t.Fatal("expected response rows after voice user speech")
	}
	joined := strings.Join(m.conversation.response, "\n")
	if !strings.Contains(joined, "> hello world") {
		t.Errorf("expected standard user prompt '> hello world' in response, got %q", joined)
	}
	if strings.Contains(joined, "🎤 You:") {
		t.Errorf("expected NO '🎤 You:' prefix in response, got %q", joined)
	}
}

func TestVoiceLiveStream_AgentStreamingAndCompletion(t *testing.T) {
	m := &RootModel{
		viewport: uiState{width: 80},
		conversation: ConversationModel{
			voiceReplyStart: -1,
		},
	}

	// 1. First delta opens streaming voice reply
	m.handleVoiceAgentDelta("Hello")
	if m.conversation.voiceReplyStart < 0 {
		t.Fatal("expected voiceReplyStart >= 0 during streaming")
	}
	if !strings.Contains(strings.Join(m.conversation.response, "\n"), "Hello") {
		t.Errorf("expected 'Hello' in response, got %v", m.conversation.response)
	}

	// 2. Second delta extends streaming reply
	m.handleVoiceAgentDelta(" there!")
	joined := strings.Join(m.conversation.response, "\n")
	if !strings.Contains(joined, "Hello there!") {
		t.Errorf("expected 'Hello there!' in response, got %q", joined)
	}
	if strings.Contains(joined, "🎤 Agent:") {
		t.Errorf("expected NO '🎤 Agent:' prefix in response, got %q", joined)
	}

	// 3. Turn completion finalizes the reply
	m.handleVoiceAgentDone("Hello there!")
	if m.conversation.voiceReplyStart != -1 {
		t.Errorf("expected voiceReplyStart to be reset to -1, got %d", m.conversation.voiceReplyStart)
	}
	finalJoined := strings.Join(m.conversation.response, "\n")
	if !strings.Contains(finalJoined, "Hello there!") {
		t.Errorf("expected finalized text in response, got %q", finalJoined)
	}
}

func TestVoiceLiveStream_BargeInInterruption(t *testing.T) {
	m := &RootModel{
		viewport: uiState{width: 80},
		conversation: ConversationModel{
			voiceReplyStart: -1,
		},
	}

	m.handleVoiceAgentDelta("I am speaking something long")
	m.handleVoiceInterrupted()

	if m.conversation.voiceReplyStart != -1 {
		t.Errorf("expected voiceReplyStart to be reset after interruption, got %d", m.conversation.voiceReplyStart)
	}
	joined := strings.Join(m.conversation.response, "\n")
	if !strings.Contains(joined, "[interrupted]") {
		t.Errorf("expected '[interrupted]' marker in response, got %q", joined)
	}
}

func TestVoiceLiveStream_ThinkerDone(t *testing.T) {
	m := &RootModel{
		viewport: uiState{width: 80},
		audio: AudioModel{
			thinking: true,
		},
	}

	m.handleVoiceThinkerDone(voiceThinkerDoneMsg{
		invocationID: "inv-1",
		answer:       "Successfully listed 42 project files.",
	})

	if m.audio.thinking {
		t.Errorf("expected m.audio.thinking to be false after thinker done")
	}
}

func TestVoiceLiveStream_InterimToCommittedSpeechParity(t *testing.T) {
	// Full simulated speech recognition flow aligned with decompiled AGY:
	// 1. Interim speech -> preview in status bar, no prompt in transcript yet
	// 2. Final speech -> preview clears, standard '> <text>' in transcript
	// 3. Spoken reply -> clean markdown streaming into transcript
	m := &RootModel{
		viewport: uiState{width: 80},
		conversation: ConversationModel{
			voiceReplyStart: -1,
		},
	}
	m.audio.Surface = panels.NewVoiceSurface(m)
	cmd := m.audio.Surface.Start("/voice live")
	if cmd != nil {
		m.audio.Surface.HandleConnectDone(voice.ConnectDoneMsg{})
	}

	// Step 1: User starts speaking (interim transcription)
	m.handleVoiceUserSpeechInterim("what is this repo?")
	if m.audio.Surface.Preview() != "what is this repo?" {
		t.Fatalf("expected preview to be 'what is this repo?', got %q", m.audio.Surface.Preview())
	}
	viewInterim := termcolor.StripSGR(m.audio.Surface.View())
	if !strings.Contains(viewInterim, "You: what is this repo?") {
		t.Fatalf("expected status bar to show 'You: what is this repo?', got %q", viewInterim)
	}
	if !strings.Contains(viewInterim, "esc hang up") {
		t.Fatalf("expected status bar on empty prompt to show 'esc hang up', got %q", viewInterim)
	}
	if strings.Contains(strings.Join(m.conversation.response, "\n"), "what is this repo?") {
		t.Fatalf("transcript should NOT contain user prompt during interim speech, got %v", m.conversation.response)
	}

	// Step 2: Speech committed / recognized
	m.handleVoiceUserSpeech("what is this repo?")
	if m.audio.Surface.Preview() != "" {
		t.Fatalf("expected preview to be cleared on committed speech, got %q", m.audio.Surface.Preview())
	}
	viewCommitted := termcolor.StripSGR(m.audio.Surface.View())
	if strings.Contains(viewCommitted, "You:") {
		t.Fatalf("expected status bar preview to be removed after commit, got %q", viewCommitted)
	}
	joinedTranscript := strings.Join(m.conversation.response, "\n")
	if !strings.Contains(joinedTranscript, "> what is this repo?") {
		t.Fatalf("expected transcript to contain standard user prompt '> what is this repo?', got %q", joinedTranscript)
	}
	if strings.Contains(joinedTranscript, "🎤 You:") {
		t.Fatalf("expected NO '🎤 You:' prefix in transcript, got %q", joinedTranscript)
	}

	// Step 3: Model streaming response
	m.handleVoiceAgentDelta("This is Pigasus, ")
	m.handleVoiceAgentDelta("an extensible clean-room agent engine.")
	streamingTranscript := strings.Join(m.conversation.response, "\n")
	if !strings.Contains(streamingTranscript, "This is Pigasus, an extensible clean-room agent engine.") {
		t.Fatalf("expected streaming markdown in transcript, got %q", streamingTranscript)
	}
	if strings.Contains(streamingTranscript, "🎤 Agent:") {
		t.Fatalf("expected NO '🎤 Agent:' prefix in transcript, got %q", streamingTranscript)
	}

	// Step 4: Model response finalized
	m.handleVoiceAgentDone("This is Pigasus, an extensible clean-room agent engine.")
	finalTranscript := strings.Join(m.conversation.response, "\n")
	if !strings.Contains(finalTranscript, "This is Pigasus, an extensible clean-room agent engine.") {
		t.Fatalf("expected finalized markdown in transcript, got %q", finalTranscript)
	}
}

type mockVoiceThinkerEngine struct {
	answer string
}

func (e *mockVoiceThinkerEngine) Turn(ctx context.Context, t agentturn.Turn) (<-chan agentlink.Frame, error) {
	ch := make(chan agentlink.Frame, 2)
	ch <- agentlink.Frame{Kind: agentlink.FrameText, Text: e.answer}
	ch <- agentlink.Frame{Kind: agentlink.FrameDone}
	close(ch)
	return ch, nil
}

func (e *mockVoiceThinkerEngine) Close() error { return nil }

func TestTuiLiveHandler_OnThinkerCancel(t *testing.T) {
	mockBackend := voicebackend.NewMockLiveBackend()
	sess := voicebackend.NewNativeLiveSession(mockBackend, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := sess.Start(ctx, voicebackend.LiveSessionConfig{Voice: "Aoede"}); err != nil {
		t.Fatalf("sess.Start failed: %v", err)
	}
	defer sess.HangUp()

	taskCtx, taskCancel := context.WithCancel(context.Background())
	defer taskCancel()

	m := &RootModel{
		viewport: uiState{width: 80},
		audio: AudioModel{
			liveSession: sess,
			thinking:    true,
		},
	}
	m.audio.setThinkerCancel("inv-tui-active", taskCancel)

	h := &tuiLiveHandler{m: m}
	h.OnThinkerCancel("inv-tui-cancel")

	if taskCtx.Err() != context.Canceled {
		t.Fatalf("expected active thinker context to be cancelled, got %v", taskCtx.Err())
	}

	last := mockBackend.LastSession()
	if last == nil {
		t.Fatal("expected mock backend session")
	}
	if last.TaskStoppedCount() != 1 {
		t.Fatalf("expected SendTaskStopped() count == 1, got %d", last.TaskStoppedCount())
	}
	resp, ok := last.ToolResponses()["inv-tui-cancel"]
	if !ok {
		t.Fatalf("expected CompleteHandoff tool response for inv-tui-cancel, got %+v", last.ToolResponses())
	}
	respMap, ok := resp.(map[string]any)
	if !ok || respMap["cancelled"] != true {
		t.Fatalf("expected cancelled=true in tool response, got %+v", resp)
	}
}

func TestVoiceLiveStream_NotifyWaitingOnUser(t *testing.T) {
	mockBackend := voicebackend.NewMockLiveBackend()
	sess := voicebackend.NewNativeLiveSession(mockBackend, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := sess.Start(ctx, voicebackend.LiveSessionConfig{Voice: "Aoede"}); err != nil {
		t.Fatalf("sess.Start failed: %v", err)
	}
	defer sess.HangUp()

	m := &RootModel{
		viewport: uiState{width: 80},
		audio: AudioModel{
			liveSession: sess,
		},
	}

	m.notifyVoiceWaitingOnUser("Approve running go test?")
	host := m.turnInteractionHost()
	if host.onWaiting != nil {
		host.onWaiting("Which environment should I target?")
	}

	last := mockBackend.LastSession()
	if last == nil {
		t.Fatal("expected mock backend session")
	}
	waiting := last.InWaitingOnUser()
	if len(waiting) != 2 {
		t.Fatalf("expected 2 SendWaitingOnUser calls, got %d: %v", len(waiting), waiting)
	}
	if waiting[0] != "Approve running go test?" || waiting[1] != "Which environment should I target?" {
		t.Fatalf("unexpected waiting prompts: %v", waiting)
	}
}

func TestVoiceLiveStream_ThinkerHandoffSendsBothCompleteHandoffAndAnswer(t *testing.T) {
	mockBackend := voicebackend.NewMockLiveBackend()
	sess := voicebackend.NewNativeLiveSession(mockBackend, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := sess.Start(ctx, voicebackend.LiveSessionConfig{Voice: "Aoede"}); err != nil {
		t.Fatalf("sess.Start failed: %v", err)
	}
	defer sess.HangUp()

	m := newRootModel(nil, "user@test", "gemini-2.5-pro", "tier1", t.TempDir(), false)
	m.turn.engine = &mockVoiceThinkerEngine{answer: "All 12 tests passed."}
	m.audio.liveSession = sess

	_ = m.handleVoiceThinkerHandoff("inv-handoff-1", "run the test suite")

	last := mockBackend.LastSession()
	if last == nil {
		t.Fatal("expected mock backend session")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(last.InAnswers()) > 0 && len(last.ToolResponses()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	answers := last.InAnswers()
	if len(answers) == 0 || !strings.Contains(answers[0], "All 12 tests passed.") {
		t.Fatalf("expected SendThinkerAnswer with 'All 12 tests passed.', got %v", answers)
	}
	resp, ok := last.ToolResponses()["inv-handoff-1"]
	if !ok {
		t.Fatalf("expected CompleteHandoff tool response for inv-handoff-1, got %+v", last.ToolResponses())
	}
	respMap, ok := resp.(map[string]any)
	if !ok || !strings.Contains(respMap["answer"].(string), "All 12 tests passed.") {
		t.Fatalf("expected answer in CompleteHandoff response, got %+v", resp)
	}
}
