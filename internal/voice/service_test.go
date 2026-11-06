package voice

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/justwasm/pigasus/internal/voice/voicepb"
)

func TestHandleHealth(t *testing.T) {
	svc := NewService(Config{})

	req := httptest.NewRequest(http.MethodGet, "/voice/health", nil)
	rr := httptest.NewRecorder()
	svc.HandleHealth(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	ct := rr.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", ct)
	}

	cc := rr.Header().Get("Cache-Control")
	if cc == "" {
		t.Fatalf("expected Cache-Control header to be set")
	}

	var res map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if res["status"] != "ok" {
		t.Fatalf("expected status ok, got %q", res["status"])
	}
}

func TestHandleChats(t *testing.T) {
	svc := NewService(Config{})
	convID := "test-conv-123"

	// Initial request: empty chats
	req := httptest.NewRequest(http.MethodGet, "/voice/chats?conversation_id="+convID, nil)
	rr := httptest.NewRecorder()
	svc.HandleChats(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	var emptyResp struct {
		Chats []any `json:"chats"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &emptyResp); err != nil {
		t.Fatalf("failed to decode json: %v", err)
	}
	if len(emptyResp.Chats) != 0 {
		t.Fatalf("expected 0 chats, got %d", len(emptyResp.Chats))
	}

	// Add a chat
	now := time.Now()
	chat := &voicepb.VoiceChat{
		StartedAt: timestamppb.New(now.Add(-10 * time.Second)),
		StoppedAt: timestamppb.New(now),
		Lines: []*voicepb.VoiceChatLine{
			{
				Role:        voicepb.VoiceChatLine_ROLE_USER,
				Text:        "Hello assistant",
				Time:        timestamppb.New(now.Add(-8 * time.Second)),
				SpokenChars: 15,
			},
			{
				Role:        voicepb.VoiceChatLine_ROLE_VOICE,
				Text:        "Hello user",
				Time:        timestamppb.New(now.Add(-5 * time.Second)),
				SpokenChars: 10,
			},
		},
	}
	svc.AddVoiceChat(convID, chat)

	rr2 := httptest.NewRecorder()
	svc.HandleChats(rr2, req)
	if rr2.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr2.Code)
	}

	var populatedResp struct {
		Chats []struct {
			StartedAt string `json:"started_at"`
			StoppedAt string `json:"stopped_at"`
			Lines     []struct {
				Role string `json:"role"`
				Text string `json:"text"`
			} `json:"lines"`
		} `json:"chats"`
	}
	if err := json.Unmarshal(rr2.Body.Bytes(), &populatedResp); err != nil {
		t.Fatalf("failed to decode json: %v", err)
	}
	if len(populatedResp.Chats) != 1 {
		t.Fatalf("expected 1 chat, got %d", len(populatedResp.Chats))
	}
	lines := populatedResp.Chats[0].Lines
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if lines[0].Role != "ROLE_USER" || lines[0].Text != "Hello assistant" {
		t.Fatalf("unexpected line 0: %+v", lines[0])
	}
	if lines[1].Role != "ROLE_VOICE" || lines[1].Text != "Hello user" {
		t.Fatalf("unexpected line 1: %+v", lines[1])
	}
}

func TestRecordingsAndDeletion(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "voice-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	svc := NewService(Config{
		WorkspaceDir: tmpDir,
	})

	// Check empty recordings
	req := httptest.NewRequest(http.MethodGet, "/voice/recordings", nil)
	rr := httptest.NewRecorder()
	svc.HandleRecordings(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var recResp map[string]int64
	if err := json.Unmarshal(rr.Body.Bytes(), &recResp); err != nil {
		t.Fatalf("failed to decode recordings json: %v", err)
	}
	if recResp["recordings_bytes"] != 0 || recResp["recordings_count"] != 0 {
		t.Fatalf("expected 0 bytes/count, got %+v", recResp)
	}

	// Create a dummy recording file
	recDir := filepath.Join(tmpDir, ".antigravity", "voice_recordings")
	_ = os.MkdirAll(recDir, 0755)
	dummyData := []byte("audio-sample-data-12345")
	if err := os.WriteFile(filepath.Join(recDir, "rec_1.raw"), dummyData, 0644); err != nil {
		t.Fatalf("failed to write dummy recording: %v", err)
	}

	// Check recordings after creating file
	rr2 := httptest.NewRecorder()
	svc.HandleRecordings(rr2, req)
	if err := json.Unmarshal(rr2.Body.Bytes(), &recResp); err != nil {
		t.Fatalf("failed to decode recordings json: %v", err)
	}
	if recResp["recordings_count"] != 1 || recResp["recordings_bytes"] != int64(len(dummyData)) {
		t.Fatalf("expected 1 file and %d bytes, got %+v", len(dummyData), recResp)
	}

	// Delete recordings
	delReq := httptest.NewRequest(http.MethodPost, "/voice/recordings/delete", nil)
	delRR := httptest.NewRecorder()
	svc.HandleDeleteRecordings(delRR, delReq)
	if delRR.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", delRR.Code)
	}
	var delResp map[string]int64
	if err := json.Unmarshal(delRR.Body.Bytes(), &delResp); err != nil {
		t.Fatalf("failed to decode delete json: %v", err)
	}
	if delResp["recordings_count"] != 1 || delResp["recordings_bytes"] != int64(len(dummyData)) {
		t.Fatalf("expected deleted 1 file and %d bytes, got %+v", len(dummyData), delResp)
	}

	// Verify directory is empty now
	entries, _ := os.ReadDir(recDir)
	if len(entries) != 0 {
		t.Fatalf("expected recordings dir to be empty, found %d entries", len(entries))
	}
}

func TestWebSocketVoiceSession(t *testing.T) {
	svc := NewService(Config{})

	ts := httptest.NewServer(http.HandlerFunc(svc.ServeHTTP))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 1. Dial without subprotocol should fail
	wsURL := "ws" + ts.URL[4:] + "/voice/ws?conversation_id=conv-voice-test"
	_, _, err := websocket.Dial(ctx, wsURL, nil)
	if err == nil {
		t.Fatalf("expected dial without subprotocol to fail")
	}

	// 2. Dial with correct subprotocol
	opts := &websocket.DialOptions{
		Subprotocols: []string{Subprotocol},
	}
	c, _, err := websocket.Dial(ctx, wsURL, opts)
	if err != nil {
		t.Fatalf("websocket.Dial failed: %v", err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	if c.Subprotocol() != Subprotocol {
		t.Fatalf("expected subprotocol %q, got %q", Subprotocol, c.Subprotocol())
	}

	// 3. Read initial SessionStatus message from server
	msgType, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("failed to read initial message: %v", err)
	}
	if msgType != websocket.MessageBinary {
		t.Fatalf("expected binary message, got %v", msgType)
	}
	var serverMsg voicepb.VoiceServerMessage
	if err := proto.Unmarshal(data, &serverMsg); err != nil {
		t.Fatalf("failed to unmarshal VoiceServerMessage: %v", err)
	}
	status := serverMsg.GetStatus()
	if status == nil || status.State != voicepb.VoiceServerMessage_SessionStatus_LISTENING {
		t.Fatalf("expected LISTENING status, got %+v", status)
	}

	// 4. Send client Setup
	clientSetup := &voicepb.VoiceClientMessage{
		Kind: &voicepb.VoiceClientMessage_Setup_{
			Setup: &voicepb.VoiceClientMessage_Setup{
				CascadeId: "conv-voice-test",
				Voice:     "Aoede",
			},
		},
	}
	setupBytes, _ := proto.Marshal(clientSetup)
	if err := c.Write(ctx, websocket.MessageBinary, setupBytes); err != nil {
		t.Fatalf("failed to write setup message: %v", err)
	}

	// 5. Read Setup ack
	_, data, err = c.Read(ctx)
	if err != nil {
		t.Fatalf("failed to read setup ack: %v", err)
	}
	var ackMsg voicepb.VoiceServerMessage
	if err := proto.Unmarshal(data, &ackMsg); err != nil {
		t.Fatalf("failed to unmarshal ack: %v", err)
	}
	if ackMsg.GetStatus() == nil || ackMsg.GetStatus().CascadeId != "conv-voice-test" {
		t.Fatalf("unexpected ack status: %+v", ackMsg.GetStatus())
	}

	// 6. Send UserTranscript
	transcriptMsg := &voicepb.VoiceClientMessage{
		Kind: &voicepb.VoiceClientMessage_UserTranscript_{
			UserTranscript: &voicepb.VoiceClientMessage_UserTranscript{
				Text:    "Testing voice transcription",
				IsFinal: true,
			},
		},
	}
	transcriptBytes, _ := proto.Marshal(transcriptMsg)
	if err := c.Write(ctx, websocket.MessageBinary, transcriptBytes); err != nil {
		t.Fatalf("failed to write user transcript: %v", err)
	}

	// 7. Receive UserSpeech
	_, data, err = c.Read(ctx)
	if err != nil {
		t.Fatalf("failed to read user speech event: %v", err)
	}
	var speechMsg voicepb.VoiceServerMessage
	if err := proto.Unmarshal(data, &speechMsg); err != nil {
		t.Fatalf("failed to unmarshal speech message: %v", err)
	}
	userSpeech := speechMsg.GetUserSpeech()
	if userSpeech == nil || userSpeech.Text != "Testing voice transcription" {
		t.Fatalf("unexpected user speech message: %+v", userSpeech)
	}

	// Close websocket cleanly
	_ = c.Close(websocket.StatusNormalClosure, "done")
}
