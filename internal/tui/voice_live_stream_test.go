package tui

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/tui/panels"
	termcolor "github.com/justwasm/pigasus/internal/ui/render/color"
	"github.com/justwasm/pigasus/internal/ui/views/voice"
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
