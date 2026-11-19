// Package voice implements Antigravity 1.5.0 parity for real-time voice mode.
// It provides HTTP endpoints:
//   - GET  /voice/health
//   - GET  /voice/chats
//   - GET  /voice/recordings
//   - POST /voice/recordings/delete
//   - GET  /voice/ws (WebSocket upgraded with subprotocol "jetski-voice-proto")
package voice

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/justwasm/pigasus/endpoints"
	"github.com/justwasm/pigasus/internal/voice/voicepb"
	"github.com/justwasm/pigasus/rctoken"
	"github.com/justwasm/pigasus/websocket"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	// Subprotocol required by Jetski Web UI for voice WebSocket.
	Subprotocol = "jetski-voice-proto"
)

// Service provides voice routing, storage, and WebSocket session handling.
type Service struct {
	mu           sync.RWMutex
	workspaceDir string
	csrfToken    string

	// Storage for recordings
	recordingsDir string
	recordings    atomic.Int64 // total bytes

	// LiveBackend provides real-time upstream voice synthesis & conversation
	liveBackend LiveBackend

	// Chats by conversation_id
	chatsMu sync.RWMutex
	chats   map[string][]*voicepb.VoiceChat
}

// Config configures the voice Service.
type Config struct {
	WorkspaceDir string
	CSRFToken    string
	LiveBackend  LiveBackend
}

// NewService creates a new voice Service.
func NewService(cfg Config) *Service {
	recDir := ""
	if cfg.WorkspaceDir != "" {
		recDir = filepath.Join(cfg.WorkspaceDir, ".antigravity", "voice_recordings")
	}
	liveB := cfg.LiveBackend
	if liveB == nil {
		if apiKey := endpoints.CurrentGeminiAPIKey(); apiKey != "" {
			liveB = NewGeminiLiveBackend(apiKey)
		} else if apiKey := os.Getenv("GEMINI_API_KEY"); apiKey != "" {
			liveB = NewGeminiLiveBackend(apiKey)
		} else if ep := endpoints.Current().Voice; ep != "" && ep != endpoints.DefaultVoice {
			liveB = NewGeminiLiveBackend("")
		} else {
			liveB = NewMockLiveBackend()
		}
	}
	s := &Service{
		workspaceDir:  cfg.WorkspaceDir,
		csrfToken:     cfg.CSRFToken,
		recordingsDir: recDir,
		liveBackend:   liveB,
		chats:         make(map[string][]*voicepb.VoiceChat),
	}
	s.initRecordings()
	return s
}

// SetLiveBackend sets the backend used for real-time live voice sessions.
func (s *Service) SetLiveBackend(b LiveBackend) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.liveBackend = b
}

// getLiveBackend retrieves the current LiveBackend safely.
func (s *Service) getLiveBackend() LiveBackend {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.liveBackend
}

func (s *Service) initRecordings() {
	if s.recordingsDir == "" {
		return
	}
	_ = os.MkdirAll(s.recordingsDir, 0755)
	var totalBytes int64
	entries, err := os.ReadDir(s.recordingsDir)
	if err == nil {
		for _, e := range entries {
			if info, err := e.Info(); err == nil && !info.IsDir() {
				totalBytes += info.Size()
			}
		}
	}
	s.recordings.Store(totalBytes)
}

// applyNoCacheHeaders sets standard anti-caching headers expected by the reference.
func applyNoCacheHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
}

// verifyToken checks CSRF authorization.
func (s *Service) verifyToken(r *http.Request) bool {
	if s.csrfToken == "" {
		return true
	}
	presented := rctoken.Presented(r)
	return rctoken.Verify(s.csrfToken, presented)
}

// ServeHTTP dispatches all /voice/* requests.
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case path == "/voice/health":
		s.HandleHealth(w, r)
	case path == "/voice/chats":
		s.HandleChats(w, r)
	case path == "/voice/recordings":
		s.HandleRecordings(w, r)
	case path == "/voice/recordings/delete":
		s.HandleDeleteRecordings(w, r)
	case path == "/voice/ws":
		s.HandleWS(w, r)
	default:
		http.NotFound(w, r)
	}
}

// HandleHealth serves GET /voice/health.
// Returns HTTP 200 with application/json or text/plain. Crucially, cannot be text/html.
func (s *Service) HandleHealth(w http.ResponseWriter, r *http.Request) {
	applyNoCacheHeaders(w)
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}` + "\n"))
}

// HandleChats serves GET /voice/chats?conversation_id=...
func (s *Service) HandleChats(w http.ResponseWriter, r *http.Request) {
	applyNoCacheHeaders(w)
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	convID := r.URL.Query().Get("conversation_id")

	s.chatsMu.RLock()
	chatList := s.chats[convID]
	s.chatsMu.RUnlock()

	res := &voicepb.VoiceChats{
		Chats: chatList,
	}
	if res.Chats == nil {
		res.Chats = []*voicepb.VoiceChat{}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// Web UI expects JSON formatted VoiceChats
	type lineJSON struct {
		Role        string `json:"role"`
		Text        string `json:"text"`
		Time        string `json:"time,omitempty"`
		SpokenChars int32  `json:"spoken_chars,omitempty"`
	}
	type chatJSON struct {
		StartedAt string     `json:"started_at,omitempty"`
		StoppedAt string     `json:"stopped_at,omitempty"`
		Lines     []lineJSON `json:"lines"`
	}
	type respJSON struct {
		Chats []chatJSON `json:"chats"`
	}

	out := respJSON{Chats: make([]chatJSON, 0, len(res.Chats))}
	for _, c := range res.Chats {
		cj := chatJSON{
			Lines: make([]lineJSON, 0, len(c.Lines)),
		}
		if c.StartedAt != nil {
			cj.StartedAt = c.StartedAt.AsTime().Format(time.RFC3339)
		}
		if c.StoppedAt != nil {
			cj.StoppedAt = c.StoppedAt.AsTime().Format(time.RFC3339)
		}
		for _, l := range c.Lines {
			roleName := "ROLE_UNSPECIFIED"
			switch l.Role {
			case voicepb.VoiceChatLine_ROLE_USER:
				roleName = "ROLE_USER"
			case voicepb.VoiceChatLine_ROLE_VOICE:
				roleName = "ROLE_VOICE"
			case voicepb.VoiceChatLine_ROLE_HANDOFF:
				roleName = "ROLE_HANDOFF"
			}
			lj := lineJSON{
				Role:        roleName,
				Text:        l.Text,
				SpokenChars: l.SpokenChars,
			}
			if l.Time != nil {
				lj.Time = l.Time.AsTime().Format(time.RFC3339)
			}
			cj.Lines = append(cj.Lines, lj)
		}
		out.Chats = append(out.Chats, cj)
	}

	_ = json.NewEncoder(w).Encode(out)
}

// HandleRecordings serves GET /voice/recordings.
// Returns { "recordings_bytes": <int>, "recordings_count": <int> }.
func (s *Service) HandleRecordings(w http.ResponseWriter, r *http.Request) {
	applyNoCacheHeaders(w)
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	var count int
	var totalBytes int64
	if s.recordingsDir != "" {
		entries, err := os.ReadDir(s.recordingsDir)
		if err == nil {
			for _, e := range entries {
				if info, err := e.Info(); err == nil && !info.IsDir() {
					count++
					totalBytes += info.Size()
				}
			}
		}
	}
	s.recordings.Store(totalBytes)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	resp := map[string]any{
		"recordings_bytes": totalBytes,
		"recordings_count": count,
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// HandleDeleteRecordings serves POST /voice/recordings/delete.
func (s *Service) HandleDeleteRecordings(w http.ResponseWriter, r *http.Request) {
	applyNoCacheHeaders(w)
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	var count int
	var bytesDeleted int64
	if s.recordingsDir != "" {
		entries, err := os.ReadDir(s.recordingsDir)
		if err == nil {
			for _, e := range entries {
				if info, err := e.Info(); err == nil && !info.IsDir() {
					bytesDeleted += info.Size()
					count++
					_ = os.Remove(filepath.Join(s.recordingsDir, e.Name()))
				}
			}
		}
	}
	s.recordings.Store(0)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	resp := map[string]any{
		"recordings_bytes": bytesDeleted,
		"recordings_count": count,
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// HandleWS upgrades to WebSocket with subprotocol "jetski-voice-proto".
func (s *Service) HandleWS(w http.ResponseWriter, r *http.Request) {
	applyNoCacheHeaders(w)

	// Enforce jetski-voice-proto subprotocol requirement:
	// A client requesting without the subprotocol must be rejected.
	secProto := r.Header.Get("Sec-WebSocket-Protocol")
	if secProto == "" || !strings.Contains(secProto, Subprotocol) {
		http.Error(w, "voice needs the jetski-voice-proto subprotocol; reload or update Jetski", http.StatusBadRequest)
		return
	}

	// WebSocket options enforcing subprotocol
	opts := &websocket.AcceptOptions{
		Subprotocols:       []string{Subprotocol},
		InsecureSkipVerify: true,
	}

	c, err := websocket.Accept(w, r, opts)
	if err != nil {
		return
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	if c.Subprotocol() != Subprotocol {
		_ = c.Close(websocket.StatusPolicyViolation, "voice needs the jetski-voice-proto subprotocol")
		return
	}

	convID := r.URL.Query().Get("conversation_id")
	sampleRateStr := r.URL.Query().Get("sample_rate")
	sampleRate := 16000
	if sr, err := strconv.Atoi(sampleRateStr); err == nil && sr > 0 {
		sampleRate = sr
	}
	voiceName := r.URL.Query().Get("voice")

	s.runVoiceSession(r.Context(), c, convID, sampleRate, voiceName)
}

// wsSafeWriter serializes writes to a websocket connection.
type wsSafeWriter struct {
	c  *websocket.Conn
	mu sync.Mutex
}

func (w *wsSafeWriter) write(ctx context.Context, msg *voicepb.VoiceServerMessage) error {
	b, err := proto.Marshal(msg)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.c.Write(ctx, websocket.MessageBinary, b)
}

// runVoiceSession runs the full duplex real-time voice streaming loop.
func (s *Service) runVoiceSession(ctx context.Context, c *websocket.Conn, convID string, sampleRate int, voiceName string) {
	sessCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	writer := &wsSafeWriter{c: c}

	// Initial session state: LISTENING
	initStatus := &voicepb.VoiceServerMessage{
		Kind: &voicepb.VoiceServerMessage_Status{
			Status: &voicepb.VoiceServerMessage_SessionStatus{
				State:       voicepb.VoiceServerMessage_SessionStatus_LISTENING,
				CascadeId:   convID,
				Attempt:     1,
				MaxAttempts: 5,
			},
		},
	}
	_ = writer.write(sessCtx, initStatus)

	currentChat := &voicepb.VoiceChat{
		StartedAt: timestamppb.Now(),
		Lines:     make([]*voicepb.VoiceChatLine, 0),
	}

	defer func() {
		currentChat.StoppedAt = timestamppb.Now()
		if len(currentChat.Lines) > 0 && convID != "" {
			s.chatsMu.Lock()
			s.chats[convID] = append(s.chats[convID], currentChat)
			s.chatsMu.Unlock()
		}
	}()

	backend := s.getLiveBackend()
	if backend == nil {
		backend = NewMockLiveBackend()
	}

	liveSess, err := backend.CreateSession(sessCtx, LiveSessionConfig{
		CascadeID:  convID,
		SampleRate: sampleRate,
		Voice:      voiceName,
	})
	if err != nil {
		_ = writer.write(sessCtx, &voicepb.VoiceServerMessage{
			Kind: &voicepb.VoiceServerMessage_Status{
				Status: &voicepb.VoiceServerMessage_SessionStatus{
					State:         voicepb.VoiceServerMessage_SessionStatus_FAILED,
					FailureReason: voicepb.VoiceServerMessage_SessionStatus_CONNECTION_FAILED,
					CascadeId:     convID,
				},
			},
		})
	}
	if err == nil && liveSess != nil {
		defer liveSess.Close()

		var voiceSeq atomic.Int32
		go func() {
			for {
				ev, err := liveSess.Receive()
				if err != nil {
					return
				}
				if ev == nil {
					continue
				}

				if ev.Interrupted {
					_ = writer.write(sessCtx, &voicepb.VoiceServerMessage{
						Kind: &voicepb.VoiceServerMessage_ClearAudio_{
							ClearAudio: &voicepb.VoiceServerMessage_ClearAudio{},
						},
					})
				}

				if len(ev.Audio) > 0 {
					seq := voiceSeq.Add(1)
					audioMs := int32((len(ev.Audio) * 1000) / 48000) // 24kHz 16-bit mono = 48000 bytes/sec
					_ = writer.write(sessCtx, &voicepb.VoiceServerMessage{
						Kind: &voicepb.VoiceServerMessage_AudioTiming_{
							AudioTiming: &voicepb.VoiceServerMessage_AudioTiming{
								Seq:           seq,
								AudioMs:       audioMs,
								PlaybackSpeed: 1.0,
							},
						},
					})
					_ = writer.write(sessCtx, &voicepb.VoiceServerMessage{
						Kind: &voicepb.VoiceServerMessage_VoiceAudio{
							VoiceAudio: ev.Audio,
						},
					})
				}

				if ev.VoiceTranscript != "" {
					seq := voiceSeq.Load()
					s.chatsMu.Lock()
					currentChat.Lines = append(currentChat.Lines, &voicepb.VoiceChatLine{
						Role: voicepb.VoiceChatLine_ROLE_VOICE,
						Text: ev.VoiceTranscript,
						Time: timestamppb.Now(),
					})
					s.chatsMu.Unlock()

					_ = writer.write(sessCtx, &voicepb.VoiceServerMessage{
						Kind: &voicepb.VoiceServerMessage_VoiceTranscript_{
							VoiceTranscript: &voicepb.VoiceServerMessage_VoiceTranscript{
								Text:    ev.VoiceTranscript,
								Seq:     seq,
								AudioMs: int32((len(ev.Audio) * 1000) / 48000),
							},
						},
					})
				}

				if ev.UserSpeech != "" {
					seq := voiceSeq.Load()
					s.chatsMu.Lock()
					currentChat.Lines = append(currentChat.Lines, &voicepb.VoiceChatLine{
						Role: voicepb.VoiceChatLine_ROLE_USER,
						Text: ev.UserSpeech,
						Time: timestamppb.Now(),
					})
					s.chatsMu.Unlock()

					_ = writer.write(sessCtx, &voicepb.VoiceServerMessage{
						Kind: &voicepb.VoiceServerMessage_UserSpeech_{
							UserSpeech: &voicepb.VoiceServerMessage_UserSpeech{
								Text:    ev.UserSpeech,
								Seq:     seq,
								Interim: ev.Interim,
							},
						},
					})
				}
			}
		}()
	}

	for {
		msgType, data, err := c.Read(sessCtx)
		if err != nil {
			return
		}
		if msgType != websocket.MessageBinary {
			continue
		}

		var clientMsg voicepb.VoiceClientMessage
		if err := proto.Unmarshal(data, &clientMsg); err != nil {
			continue
		}

		switch k := clientMsg.Kind.(type) {
		case *voicepb.VoiceClientMessage_Setup_:
			status := &voicepb.VoiceServerMessage{
				Kind: &voicepb.VoiceServerMessage_Status{
					Status: &voicepb.VoiceServerMessage_SessionStatus{
						State:       voicepb.VoiceServerMessage_SessionStatus_LISTENING,
						CascadeId:   k.Setup.CascadeId,
						Attempt:     1,
						MaxAttempts: 5,
					},
				},
			}
			_ = writer.write(sessCtx, status)

		case *voicepb.VoiceClientMessage_MicAudio:
			if len(k.MicAudio) > 0 {
				if s.recordingsDir != "" {
					filename := fmt.Sprintf("rec_%d.raw", time.Now().UnixNano())
					filePath := filepath.Join(s.recordingsDir, filename)
					if err := os.WriteFile(filePath, k.MicAudio, 0644); err == nil {
						s.recordings.Add(int64(len(k.MicAudio)))
					}
				}
				if liveSess != nil {
					_ = liveSess.SendAudio(k.MicAudio)
				}
			}

		case *voicepb.VoiceClientMessage_UserTranscript_:
			if k.UserTranscript != nil && k.UserTranscript.Text != "" {
				s.chatsMu.Lock()
				currentChat.Lines = append(currentChat.Lines, &voicepb.VoiceChatLine{
					Role: voicepb.VoiceChatLine_ROLE_USER,
					Text: k.UserTranscript.Text,
					Time: timestamppb.Now(),
				})
				seq := int32(len(currentChat.Lines))
				s.chatsMu.Unlock()

				speechEvent := &voicepb.VoiceServerMessage{
					Kind: &voicepb.VoiceServerMessage_UserSpeech_{
						UserSpeech: &voicepb.VoiceServerMessage_UserSpeech{
							Text:    k.UserTranscript.Text,
							Seq:     seq,
							Interim: !k.UserTranscript.IsFinal,
						},
					},
				}
				_ = writer.write(sessCtx, speechEvent)

				if liveSess != nil {
					_ = liveSess.SendText(k.UserTranscript.Text)
				}
			}

		case *voicepb.VoiceClientMessage_SettingsUpdate_:
			// Acknowledge setting updates
		}
	}
}

// AddVoiceChat appends a VoiceChat entry programmatically.
func (s *Service) AddVoiceChat(convID string, chat *voicepb.VoiceChat) {
	s.chatsMu.Lock()
	defer s.chatsMu.Unlock()
	s.chats[convID] = append(s.chats[convID], chat)
}
