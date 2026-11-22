package session

import (
	"errors"
	"testing"

	"github.com/justwasm/pigasus/internal/core/backend"
)

type mockVoiceRouter struct {
	active bool
	sent   []string
	media  [][]byte
	err    error
}

func (m *mockVoiceRouter) IsActive() bool {
	return m.active
}

func (m *mockVoiceRouter) SendText(text string) error {
	m.sent = append(m.sent, text)
	return m.err
}

func (m *mockVoiceRouter) SendMedia(mimeType string, data []byte) error {
	m.media = append(m.media, data)
	return m.err
}

func TestMultimodalBroker_RecordVoiceTurn(t *testing.T) {
	sess := New(Config{ID: "test-sess", Model: "gemini-3.8-flash-high"})
	broker := sess.Broker()
	if broker == nil {
		t.Fatal("expected non-nil broker on session")
	}

	var notified []backend.Message
	var modalities []TurnModality
	broker.Subscribe(func(turn backend.Message, mod TurnModality) {
		notified = append(notified, turn)
		modalities = append(modalities, mod)
	})

	// 1. Record user speech
	uMsg := sess.RecordVoiceTurn("user", "Hello from microphone")
	if uMsg.Role != "user" || uMsg.Content != "Hello from microphone" {
		t.Fatalf("unexpected user message: %+v", uMsg)
	}

	// 2. Record agent speech
	aMsg := sess.RecordVoiceTurn("assistant", "Hello from live agent")
	if aMsg.Role != "assistant" || aMsg.Content != "Hello from live agent" {
		t.Fatalf("unexpected agent message: %+v", aMsg)
	}

	// 3. Verify session messages
	msgs := sess.Messages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages in session, got %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "Hello from microphone" {
		t.Errorf("msg 0 mismatch: %+v", msgs[0])
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "Hello from live agent" {
		t.Errorf("msg 1 mismatch: %+v", msgs[1])
	}

	// 4. Verify subscriber notifications
	if len(notified) != 2 {
		t.Fatalf("expected 2 notifications, got %d", len(notified))
	}
	if modalities[0] != ModalityVoice || modalities[1] != ModalityVoice {
		t.Errorf("expected ModalityVoice notifications, got %v", modalities)
	}
}

func TestMultimodalBroker_DispatchPrompt(t *testing.T) {
	sess := New(Config{ID: "test-sess", Model: "gemini-3.8-flash-high"})

	// Case 1: Inactive voice -> DispatchPrompt returns handled=false
	handled, err := sess.DispatchPrompt("hello keyboard")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handled {
		t.Fatalf("expected handled=false when voice is inactive")
	}
	if len(sess.Messages()) != 0 {
		t.Fatalf("expected 0 messages in session when unhandled, got %d", len(sess.Messages()))
	}

	// Case 2: Active voice router registered -> DispatchPrompt routes to voice session
	router := &mockVoiceRouter{active: true}
	sess.SetLiveVoiceRouter(router)

	if !sess.IsLiveVoiceActive() {
		t.Fatal("expected IsLiveVoiceActive=true")
	}

	handled, err = sess.DispatchPrompt("typed while live")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !handled {
		t.Fatalf("expected handled=true when voice is active")
	}
	if len(router.sent) != 1 || router.sent[0] != "typed while live" {
		t.Fatalf("expected router to receive text, got %v", router.sent)
	}
	msgs := sess.Messages()
	if len(msgs) != 1 || msgs[0].Content != "typed while live" {
		t.Fatalf("expected session to record user prompt, got %v", msgs)
	}

	// Case 3: Router returns error
	router.err = errors.New("network send failed")
	handled, err = sess.DispatchPrompt("another message")
	if err == nil || err.Error() != "network send failed" {
		t.Fatalf("expected network send error, got %v", err)
	}
	if !handled {
		t.Fatalf("expected handled=true even on error since it targeted voice")
	}
}

func TestMultimodalBroker_DispatchVisionFrame(t *testing.T) {
	sess := New(Config{ID: "test-sess", Model: "gemini-3.8-flash-high"})
	router := &mockVoiceRouter{active: true}
	sess.SetLiveVoiceRouter(router)

	var notified []backend.Message
	var modalities []TurnModality
	sess.Broker().Subscribe(func(turn backend.Message, mod TurnModality) {
		notified = append(notified, turn)
		modalities = append(modalities, mod)
	})

	frameBytes := []byte{0xFF, 0xD8, 0xFF, 0xE0} // mock JPEG header
	err := sess.DispatchVisionFrame("image/jpeg", frameBytes, "Terminal error on line 42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(router.media) != 1 || len(router.media[0]) != 4 {
		t.Fatalf("expected router to receive media frame, got %v", router.media)
	}
	if len(router.sent) != 1 || router.sent[0] != "Terminal error on line 42" {
		t.Fatalf("expected router to receive text summary, got %v", router.sent)
	}

	if len(notified) != 1 || modalities[0] != ModalityVision {
		t.Fatalf("expected 1 vision notification, got %v with %v", notified, modalities)
	}
}
