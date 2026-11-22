package voice

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/websocket"
	"google.golang.org/protobuf/proto"

	"github.com/justwasm/pigasus/endpoints"
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

func TestGeminiLive_RealRoundtrip(t *testing.T) {
	key := endpoints.CurrentGeminiAPIKey()
	if key == "" {
		t.Skip("skipping real Gemini Live test: no GEMINI_API_KEY")
	}

	b := NewGeminiLiveBackend(key)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	sess, err := b.CreateSession(ctx, LiveSessionConfig{
		Voice: "Finn",
		SystemPrompt: `You are Antigravity's real-time spoken voice agent.
Speak concisely, naturally, warmly in 1-2 spoken sentences.`,
	})
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	defer sess.Close()

	if err := sess.SendText("Check git status for me"); err != nil {
		t.Fatalf("SendText failed: %v", err)
	}

	var totalAudioBytes int
	var transcript string
	var toolCallsCount int

	for {
		ev, err := sess.Receive()
		if err != nil {
			t.Logf("Receive stopped with error: %v", err)
			break
		}
		if len(ev.ToolCalls) > 0 {
			toolCallsCount += len(ev.ToolCalls)
			for _, tc := range ev.ToolCalls {
				t.Logf("RECV ToolCall: %s (id=%s) %+v", tc.Name, tc.ID, tc.Args)
				if tc.Name == "call_thinker_agent" {
					t.Logf("Responding to call_thinker_agent...")
					if err := sess.SendToolResponse(tc.ID, tc.Name, map[string]any{
						"status": "working tree clean, on branch main",
					}); err != nil {
						t.Fatalf("SendToolResponse failed: %v", err)
					}
				}
			}
		}
		if ev.VoiceTranscript != "" {
			transcript += ev.VoiceTranscript
			t.Logf("RECV Transcript: %q", ev.VoiceTranscript)
		}
		if len(ev.Audio) > 0 {
			totalAudioBytes += len(ev.Audio)
			t.Logf("RECV Audio chunk: %d bytes", len(ev.Audio))
		}
		if ev.UserSpeech != "" {
			t.Logf("RECV UserSpeech: %q (interim=%v)", ev.UserSpeech, ev.Interim)
		}
		if ev.Interrupted {
			t.Logf("RECV Interrupted: true")
		}
		if ev.TurnComplete {
			t.Logf("RECV TurnComplete: true")
			if toolCallsCount > 0 && totalAudioBytes > 0 {
				break
			}
		}
	}

	t.Logf("Summary: transcript=%q, audioBytes=%d, toolCalls=%d", transcript, totalAudioBytes, toolCallsCount)
	if totalAudioBytes == 0 {
		t.Errorf("NO AUDIO RECEIVED! Audio bytes = 0")
	}
}

func TestGeminiLive_BargeInBehavior(t *testing.T) {
	key := endpoints.CurrentGeminiAPIKey()
	if key == "" {
		t.Skip("skipping real Gemini Live test: no GEMINI_API_KEY")
	}

	b := NewGeminiLiveBackend(key)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	sess, err := b.CreateSession(ctx, LiveSessionConfig{
		Voice: "Finn",
		SystemPrompt: `You are Antigravity's real-time spoken voice agent.
Speak concisely in 2 sentences.`,
	})
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	defer sess.Close()

	if err := sess.SendText("Tell me a two-sentence story."); err != nil {
		t.Fatalf("SendText failed: %v", err)
	}

	var audioChunks int
	var interrupted bool
	silence := make([]byte, 3200) // 100ms of 16kHz silence

	for {
		ev, err := sess.Receive()
		if err != nil {
			break
		}
		if len(ev.Audio) > 0 {
			audioChunks++
			// As soon as the first audio chunk arrives, send a silence chunk into the session
			if audioChunks == 1 {
				t.Logf("Sending silence audio chunk to simulate continuous mic stream...")
				_ = sess.SendAudio(silence)
			}
		}
		if ev.Interrupted {
			interrupted = true
			t.Logf("GEMINI SENT INTERRUPTED = TRUE!")
		}
		if ev.TurnComplete {
			break
		}
	}
	t.Logf("Total audio chunks: %d, Interrupted: %v", audioChunks, interrupted)
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
