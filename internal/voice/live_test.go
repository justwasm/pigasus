package voice

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/websocket"
	"google.golang.org/protobuf/proto"

	"github.com/justwasm/pigasus/internal/voice/voicepb"
)

func TestMapVoiceName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"fola", "Aoede"},
		{"Fola", "Aoede"},
		{"arlo", "Puck"},
		{"Arlo", "Puck"},
		{"cleo", "Kore"},
		{"Cleo", "Kore"},
		{"finn", "Fenrir"},
		{"Finn", "Fenrir"},
		{"gero", "Puck"},
		{"lora", "Aoede"},
		{"neno", "Charon"},
		{"olin", "Fenrir"},
		{"tari", "Kore"},
		{"Aoede", "Aoede"},
		{"aoede", "Aoede"},
		{"Puck", "Puck"},
		{"puck", "Puck"},
		{"Kore", "Kore"},
		{"kore", "Kore"},
		{"Fenrir", "Fenrir"},
		{"fenrir", "Fenrir"},
		{"Charon", "Charon"},
		{"charon", "Charon"},
		{"Zephyr", "Zephyr"},
		{"zephyr", "Zephyr"},
		{"Leda", "Leda"},
		{"Orus", "Orus"},
		{"unknown", "Aoede"},
		{"", "Aoede"},
	}

	for _, tc := range cases {
		got := MapVoiceName(tc.in)
		if got != tc.want {
			t.Errorf("MapVoiceName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	// Verify FindVoicePersona
	if p, ok := FindVoicePersona("Arlo"); !ok || p.GeminiVoice != "Puck" {
		t.Errorf("FindVoicePersona(Arlo) = %+v, ok = %v, want Puck", p, ok)
	}
	if p, ok := FindVoicePersona("Zephyr"); !ok || p.GeminiVoice != "Zephyr" {
		t.Errorf("FindVoicePersona(Zephyr) = %+v, ok = %v, want Zephyr", p, ok)
	}
	if _, ok := FindVoicePersona("NoSuchVoice"); ok {
		t.Errorf("FindVoicePersona(NoSuchVoice) expected false, got true")
	}
}

func TestGeminiLiveBackend_BearerToken(t *testing.T) {
	b := NewGeminiLiveBackendWithBearer("test-oauth-token")
	if b.BearerToken() != "test-oauth-token" {
		t.Fatalf("expected BearerToken() == test-oauth-token, got %q", b.BearerToken())
	}
	b.SetBearerToken("updated-token")
	if b.BearerToken() != "updated-token" {
		t.Fatalf("expected updated bearerToken, got %q", b.BearerToken())
	}

	apiKeyBackend := NewGeminiLiveBackend("test-api-key")
	if apiKeyBackend.APIKey() != "test-api-key" {
		t.Fatalf("expected APIKey() == test-api-key, got %q", apiKeyBackend.APIKey())
	}
}

func TestMockLiveSession(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	backend := NewMockLiveBackend()
	backend.SetAutoEcho(true)
	sess, err := backend.CreateSession(ctx, LiveSessionConfig{
		CascadeID:  "conv-123",
		Voice:      "Fola",
		SampleRate: 16000,
	})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	defer sess.Close()

	// Send audio chunk
	pcm16k := make([]byte, 3200) // 100ms of 16kHz 16-bit mono
	if err := sess.SendAudio(pcm16k); err != nil {
		t.Fatalf("SendAudio failed: %v", err)
	}

	// Should receive UserSpeech first
	ev1, err := sess.Receive()
	if err != nil {
		t.Fatalf("Receive 1 failed: %v", err)
	}
	if ev1.UserSpeech == "" {
		t.Errorf("expected UserSpeech, got: %+v", ev1)
	}

	// Should receive VoiceTranscript and 24kHz Audio next
	ev2, err := sess.Receive()
	if err != nil {
		t.Fatalf("Receive 2 failed: %v", err)
	}
	if ev2.VoiceTranscript == "" || len(ev2.Audio) == 0 {
		t.Errorf("expected VoiceTranscript and 24k audio, got: %+v", ev2)
	}

	// While speaking, user sends new audio -> barge-in!
	if err := sess.SendAudio(pcm16k); err != nil {
		t.Fatalf("SendAudio barge-in failed: %v", err)
	}

	ev3, err := sess.Receive()
	if err != nil {
		t.Fatalf("Receive 3 failed: %v", err)
	}
	if !ev3.Interrupted {
		t.Errorf("expected barge-in Interrupted=true, got: %+v", ev3)
	}
}

func TestFullDuplexWebSocketVoiceSessionWithLiveBackend(t *testing.T) {
	mockBackend := NewMockLiveBackend()
	mockBackend.SetAutoEcho(true)
	svc := NewService(Config{
		WorkspaceDir: t.TempDir(),
		LiveBackend:  mockBackend,
	})

	ts := httptest.NewServer(svc)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/voice/ws?conversation_id=live-test&sample_rate=16000"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	opts := &websocket.DialOptions{
		Subprotocols: []string{Subprotocol},
	}
	c, _, err := websocket.Dial(ctx, wsURL, opts)
	if err != nil {
		t.Fatalf("WebSocket Dial failed: %v", err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	// 1. First server message should be Status LISTENING
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("failed to read initial status: %v", err)
	}
	var initStatus voicepb.VoiceServerMessage
	if err := proto.Unmarshal(data, &initStatus); err != nil {
		t.Fatalf("unmarshal initStatus error: %v", err)
	}
	status := initStatus.GetStatus()
	if status == nil || status.State != voicepb.VoiceServerMessage_SessionStatus_LISTENING {
		t.Fatalf("expected LISTENING status, got: %+v", status)
	}

	// 2. Client sends Setup
	setupMsg := &voicepb.VoiceClientMessage{
		Kind: &voicepb.VoiceClientMessage_Setup_{
			Setup: &voicepb.VoiceClientMessage_Setup{
				CascadeId:       "live-test",
				Voice:           "Fola",
				SampleRateHertz: 16000,
				PlaybackSpeed:   1.0,
			},
		},
	}
	bSetup, _ := proto.Marshal(setupMsg)
	if err := c.Write(ctx, websocket.MessageBinary, bSetup); err != nil {
		t.Fatalf("Write setup failed: %v", err)
	}

	// Read setup ACK
	_, data, err = c.Read(ctx)
	if err != nil {
		t.Fatalf("read setup ACK error: %v", err)
	}

	// 3. Client sends 16kHz MicAudio chunk
	pcmChunk := make([]byte, 3200)
	micMsg := &voicepb.VoiceClientMessage{
		Kind: &voicepb.VoiceClientMessage_MicAudio{
			MicAudio: pcmChunk,
		},
	}
	bMic, _ := proto.Marshal(micMsg)
	if err := c.Write(ctx, websocket.MessageBinary, bMic); err != nil {
		t.Fatalf("Write mic audio failed: %v", err)
	}

	// 4. Server should stream back UserSpeech, AudioTiming, VoiceAudio, VoiceTranscript
	var receivedAudio bool
	var receivedTranscript bool
	var receivedUserSpeech bool

	for i := 0; i < 4; i++ {
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("read response %d error: %v", i, err)
		}
		var srvMsg voicepb.VoiceServerMessage
		if err := proto.Unmarshal(data, &srvMsg); err != nil {
			t.Fatalf("unmarshal server response error: %v", err)
		}

		if srvMsg.GetVoiceAudio() != nil {
			receivedAudio = true
		}
		if srvMsg.GetVoiceTranscript() != nil {
			receivedTranscript = true
		}
		if srvMsg.GetUserSpeech() != nil {
			receivedUserSpeech = true
		}
	}

	if !receivedAudio {
		t.Errorf("expected VoiceAudio (24kHz PCM) from live backend")
	}
	if !receivedTranscript {
		t.Errorf("expected VoiceTranscript from live backend")
	}
	if !receivedUserSpeech {
		t.Errorf("expected UserSpeech from live backend")
	}

	// 5. Test Barge-In: client sends audio while speaking -> server emits ClearAudio!
	if err := c.Write(ctx, websocket.MessageBinary, bMic); err != nil {
		t.Fatalf("Write second mic audio for barge-in failed: %v", err)
	}

	// Read until ClearAudio is received
	var receivedClearAudio bool
	for i := 0; i < 3; i++ {
		_, data, err := c.Read(ctx)
		if err != nil {
			break
		}
		var srvMsg voicepb.VoiceServerMessage
		if err := proto.Unmarshal(data, &srvMsg); err != nil {
			continue
		}
		if srvMsg.GetClearAudio() != nil {
			receivedClearAudio = true
			break
		}
	}

	if !receivedClearAudio {
		t.Errorf("expected ClearAudio (barge-in) from server")
	}
}
