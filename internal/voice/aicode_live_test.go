package voice

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/justwasm/pigasus/internal/voice/voicepb"
)

type mockAICodeStream struct {
	mu        sync.Mutex
	sentMsgs  []any
	recvMsgs  []any
	sendCalls int
	sendErr   error
	recvErr   error
	closed    bool
	done      chan struct{}
}

func (m *mockAICodeStream) getDone() chan struct{} {
	if m.done == nil {
		m.done = make(chan struct{})
	}
	return m.done
}

func (m *mockAICodeStream) SendMsg(msg any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return io.ErrClosedPipe
	}
	m.sendCalls++
	if m.sendErr != nil && m.sendCalls > 1 {
		return m.sendErr
	}
	m.sentMsgs = append(m.sentMsgs, msg)
	return nil
}

func (m *mockAICodeStream) RecvMsg(target any) error {
	m.mu.Lock()
	if m.recvErr != nil {
		defer m.mu.Unlock()
		return m.recvErr
	}
	if len(m.recvMsgs) > 0 {
		msg := m.recvMsgs[0]
		m.recvMsgs = m.recvMsgs[1:]
		m.mu.Unlock()

		if resp, ok := target.(*voicepb.StreamVoiceResponse); ok {
			if src, ok := msg.(*voicepb.StreamVoiceResponse); ok {
				proto.Reset(resp)
				proto.Merge(resp, src)
				return nil
			}
		}
		return nil
	}
	if m.closed {
		m.mu.Unlock()
		return io.EOF
	}
	done := m.getDone()
	m.mu.Unlock()

	<-done
	return io.EOF
}

func (m *mockAICodeStream) CloseSend() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.closed {
		m.closed = true
		if m.done != nil {
			close(m.done)
		}
	}
	return nil
}

func TestAICodeLiveBackend_SetupSerialization(t *testing.T) {
	mockStream := &mockAICodeStream{}
	backend := NewAICodeLiveBackend("ya29.test-token")
	backend.SetStreamFactory(func(ctx context.Context, endpoint string, token string) (AICodeStream, func(), error) {
		if token != "ya29.test-token" {
			t.Fatalf("unexpected token: %q", token)
		}
		if endpoint != DefaultAICodeEndpoint {
			t.Fatalf("unexpected endpoint: %q", endpoint)
		}
		return mockStream, func() {}, nil
	})

	cfg := LiveSessionConfig{
		CascadeID:  "test-cascade-123",
		Voice:      "Finn",
		SampleRate: 16000,
	}

	sess, err := backend.CreateSession(context.Background(), cfg)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	defer sess.Close()

	mockStream.mu.Lock()
	if len(mockStream.sentMsgs) == 0 {
		mockStream.mu.Unlock()
		t.Fatalf("expected setup message sent to stream")
	}
	first := mockStream.sentMsgs[0]
	mockStream.mu.Unlock()

	req, ok := first.(*voicepb.StreamVoiceRequest)
	if !ok {
		t.Fatalf("expected *voicepb.StreamVoiceRequest, got %T", first)
	}

	setup := req.GetMessage().GetSetup()
	if setup == nil {
		t.Fatalf("expected Setup message inside first request")
	}

	if setup.CascadeId != "test-cascade-123" {
		t.Errorf("expected CascadeId == 'test-cascade-123', got %q", setup.CascadeId)
	}
	if setup.Voice != "Finn" {
		t.Errorf("expected Voice == 'Finn', got %q", setup.Voice)
	}
	if setup.SampleRateHertz != 16000 {
		t.Errorf("expected SampleRateHertz == 16000, got %d", setup.SampleRateHertz)
	}
	if setup.RuntimeEnvironment != voicepb.RuntimeEnvironment_RUNTIME_ENVIRONMENT_ELECTRON {
		t.Errorf("expected RuntimeEnvironment ELECTRON, got %v", setup.RuntimeEnvironment)
	}
	if setup.Backend != voicepb.BackendType_BACKEND_TYPE_CLOUD {
		t.Errorf("expected Backend CLOUD, got %v", setup.Backend)
	}
	if setup.ClientVersion != DefaultAICodeClientVersion {
		t.Errorf("expected ClientVersion %q, got %q", DefaultAICodeClientVersion, setup.ClientVersion)
	}
}

func TestAICodeLiveSession_SendAudioAndText(t *testing.T) {
	mockStream := &mockAICodeStream{}
	backend := NewAICodeLiveBackend("ya29.test-token")
	backend.SetStreamFactory(func(ctx context.Context, endpoint string, token string) (AICodeStream, func(), error) {
		return mockStream, func() {}, nil
	})

	sess, err := backend.CreateSession(context.Background(), LiveSessionConfig{Voice: "Arlo"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	defer sess.Close()

	// 1. Send Audio
	audioChunk := []byte{0x01, 0x02, 0x03, 0x04}
	if err := sess.SendAudio(audioChunk); err != nil {
		t.Fatalf("SendAudio: %v", err)
	}

	// 2. Send Text
	if err := sess.SendText("Hello voice"); err != nil {
		t.Fatalf("SendText: %v", err)
	}

	// 3. Send Thinker Note
	if err := sess.SendThinkerNote("Reading file"); err != nil {
		t.Fatalf("SendThinkerNote: %v", err)
	}

	// 4. Send Thinker Answer
	if err := sess.SendThinkerAnswer("Everything looks good"); err != nil {
		t.Fatalf("SendThinkerAnswer: %v", err)
	}

	mockStream.mu.Lock()
	defer mockStream.mu.Unlock()

	// [0] = Setup, [1] = Audio, [2] = Text, [3] = Thinker Note, [4] = Thinker Answer
	if len(mockStream.sentMsgs) != 5 {
		t.Fatalf("expected 5 sent messages, got %d", len(mockStream.sentMsgs))
	}

	// Check Audio message
	audioReq, ok := mockStream.sentMsgs[1].(*voicepb.StreamVoiceRequest)
	if !ok || len(audioReq.GetMessage().GetMicAudio()) != 4 {
		t.Fatalf("unexpected audio message: %#v", mockStream.sentMsgs[1])
	}

	// Check Text message
	textReq, ok := mockStream.sentMsgs[2].(*voicepb.StreamVoiceRequest)
	if !ok || textReq.GetMessage().GetUserTranscript().GetText() != "Hello voice" {
		t.Fatalf("unexpected text message: %#v", mockStream.sentMsgs[2])
	}
}

func TestAICodeLiveSession_ReceiveEvents(t *testing.T) {
	mockStream := &mockAICodeStream{
		recvMsgs: []any{
			&voicepb.StreamVoiceResponse{
				Message: &voicepb.VoiceServerMessage{
					Kind: &voicepb.VoiceServerMessage_VoiceAudio{
						VoiceAudio: []byte{0x10, 0x20, 0x30},
					},
				},
			},
			&voicepb.StreamVoiceResponse{
				Message: &voicepb.VoiceServerMessage{
					Kind: &voicepb.VoiceServerMessage_VoiceTranscript_{
						VoiceTranscript: &voicepb.VoiceServerMessage_VoiceTranscript{
							Text: "I can help with that.",
						},
					},
				},
			},
			&voicepb.StreamVoiceResponse{
				Message: &voicepb.VoiceServerMessage{
					Kind: &voicepb.VoiceServerMessage_UserSpeech_{
						UserSpeech: &voicepb.VoiceServerMessage_UserSpeech{
							Text:    "Partial recognition",
							Interim: true,
						},
					},
				},
			},
			&voicepb.StreamVoiceResponse{
				Message: &voicepb.VoiceServerMessage{
					Kind: &voicepb.VoiceServerMessage_ClearAudio_{
						ClearAudio: &voicepb.VoiceServerMessage_ClearAudio{},
					},
				},
			},
		},
	}

	backend := NewAICodeLiveBackend("ya29.test-token")
	backend.SetStreamFactory(func(ctx context.Context, endpoint string, token string) (AICodeStream, func(), error) {
		return mockStream, func() {}, nil
	})

	sess, err := backend.CreateSession(context.Background(), LiveSessionConfig{Voice: "Finn"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	defer sess.Close()

	// 1. VoiceAudio
	ev1, err := sess.Receive()
	if err != nil {
		t.Fatalf("Receive 1: %v", err)
	}
	if len(ev1.Audio) != 3 || ev1.Audio[0] != 0x10 {
		t.Errorf("unexpected audio: %v", ev1.Audio)
	}

	// 2. VoiceTranscript
	ev2, err := sess.Receive()
	if err != nil {
		t.Fatalf("Receive 2: %v", err)
	}
	if ev2.VoiceTranscript != "I can help with that." {
		t.Errorf("unexpected transcript: %q", ev2.VoiceTranscript)
	}

	// 3. UserSpeech (interim)
	ev3, err := sess.Receive()
	if err != nil {
		t.Fatalf("Receive 3: %v", err)
	}
	if ev3.UserSpeech != "Partial recognition" || !ev3.Interim {
		t.Errorf("unexpected user speech: %q (interim=%v)", ev3.UserSpeech, ev3.Interim)
	}

	// 4. ClearAudio (interrupted)
	ev4, err := sess.Receive()
	if err != nil {
		t.Fatalf("Receive 4: %v", err)
	}
	if !ev4.Interrupted {
		t.Errorf("expected Interrupted == true for ClearAudio")
	}
}

func TestAICodeLiveSession_FallbackActivation(t *testing.T) {
	// Configure mockStream that returns PermissionDenied on first RecvMsg
	mockStream := &mockAICodeStream{
		recvErr: errors.New("rpc error: code = PermissionDenied desc = The caller does not have permission"),
	}

	mockFallback := NewMockLiveBackend()
	mockFallback.SetCustomEvents([]*LiveServerEvent{
		{VoiceTranscript: "Hello from fallback session"},
	})

	backend := NewAICodeLiveBackendWithFallback("ya29.test-token", mockFallback)
	backend.SetStreamFactory(func(ctx context.Context, endpoint string, token string) (AICodeStream, func(), error) {
		return mockStream, func() {}, nil
	})

	sess, err := backend.CreateSession(context.Background(), LiveSessionConfig{Voice: "Finn"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	defer sess.Close()

	// Pre-send early audio chunk before receive
	if err := sess.SendAudio([]byte{0xaa, 0xbb}); err != nil {
		t.Fatalf("SendAudio: %v", err)
	}

	// When receiving, it should hit PermissionDenied, activate fallback, and return fallback event!
	ev, err := sess.Receive()
	if err != nil {
		t.Fatalf("Receive should have seamlessly fallen back, got error: %v", err)
	}
	if ev.VoiceTranscript != "Hello from fallback session" {
		t.Fatalf("expected fallback transcript, got %q", ev.VoiceTranscript)
	}

	// Ensure subsequent send routes to fallback session
	if err := sess.SendText("User message to fallback"); err != nil {
		t.Fatalf("SendText to fallback failed: %v", err)
	}
}

func TestAICodeLiveSession_StatusError(t *testing.T) {
	mockStream := &mockAICodeStream{
		recvMsgs: []any{
			&voicepb.StreamVoiceResponse{
				Message: &voicepb.VoiceServerMessage{
					Kind: &voicepb.VoiceServerMessage_Status{
						Status: &voicepb.VoiceServerMessage_SessionStatus{
							State:         voicepb.VoiceServerMessage_SessionStatus_FAILED,
							FailureReason: voicepb.VoiceServerMessage_SessionStatus_ACCESS_DENIED,
							Message:       "Remote session denied access",
						},
					},
				},
			},
		},
	}

	backend := NewAICodeLiveBackend("ya29.test-token")
	backend.SetStreamFactory(func(ctx context.Context, endpoint string, token string) (AICodeStream, func(), error) {
		return mockStream, func() {}, nil
	})

	sess, err := backend.CreateSession(context.Background(), LiveSessionConfig{Voice: "Cleo"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	defer sess.Close()

	_, err = sess.Receive()
	if err == nil {
		t.Fatalf("expected error from failed status, got nil")
	}
}

func TestAICodeLiveSession_ToolResponseAndClose(t *testing.T) {
	mockStream := &mockAICodeStream{}
	backend := NewAICodeLiveBackend("ya29.test-token")
	backend.SetStreamFactory(func(ctx context.Context, endpoint string, token string) (AICodeStream, func(), error) {
		return mockStream, func() {}, nil
	})

	sess, err := backend.CreateSession(context.Background(), LiveSessionConfig{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if err := sess.SendToolResponse("call-1", "read_file", map[string]any{"content": "hello"}); err != nil {
		t.Fatalf("SendToolResponse: %v", err)
	}

	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Sending audio after close should return error
	if err := sess.SendAudio([]byte{1, 2}); err == nil {
		t.Fatalf("expected error sending audio after close")
	}
}

func TestAICodeLiveBackend_FallbackLatchingAndSendAudio(t *testing.T) {
	mockStream := &mockAICodeStream{
		sendErr: io.EOF,
		recvErr: errors.New("rpc error: code = PermissionDenied desc = The caller does not have permission"),
	}

	mockFallback := NewMockLiveBackend()
	backend := NewAICodeLiveBackendWithFallback("ya29.test-token", mockFallback)
	backend.SetStreamFactory(func(ctx context.Context, endpoint string, token string) (AICodeStream, func(), error) {
		return mockStream, func() {}, nil
	})

	sess, err := backend.CreateSession(context.Background(), LiveSessionConfig{Voice: "Finn"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	defer sess.Close()

	// SendAudio with sendErr triggers fallback
	if err := sess.SendAudio(make([]byte, 3200)); err != nil {
		t.Fatalf("SendAudio should activate fallback, got err: %v", err)
	}

	if !backend.IsFallbackOnly() {
		t.Fatal("expected backend to latch FallbackOnly after fallback activation")
	}

	// Subsequent CreateSession should route directly to fallback
	sess2, err := backend.CreateSession(context.Background(), LiveSessionConfig{Voice: "Finn"})
	if err != nil {
		t.Fatalf("CreateSession 2: %v", err)
	}
	defer sess2.Close()

	if _, ok := sess2.(*MockLiveSession); !ok {
		t.Fatalf("expected *MockLiveSession from latched fallback, got %T", sess2)
	}
}
