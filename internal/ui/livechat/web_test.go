package livechat

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
	accountrouting "github.com/justwasm/pigasus/internal/features/accounts/routing"
	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/internal/voice"
)

func TestEmbeddedWebAssets(t *testing.T) {
	fs, desc := resolveWebFileSystem("")
	if fs == nil {
		t.Fatal("expected valid http.FileSystem, got nil")
	}
	t.Logf("web file system resolved from: %s", desc)

	// Check index.html
	f, err := fs.Open("/index.html")
	if err != nil {
		f, err = fs.Open("index.html")
	}
	if err != nil {
		t.Fatalf("failed to open index.html from web fs: %v", err)
	}
	defer f.Close()

	content, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("failed to read index.html: %v", err)
	}
	if !strings.Contains(string(content), "LiveChat") && !strings.Contains(string(content), "Pigasus") {
		t.Errorf("index.html missing expected title text, length: %d", len(content))
	}

	// Check app.js
	f2, err := fs.Open("/app.js")
	if err != nil {
		f2, err = fs.Open("app.js")
	}
	if err != nil {
		t.Fatalf("failed to open app.js from web fs: %v", err)
	}
	f2.Close()

	// Check style.css
	f3, err := fs.Open("/style.css")
	if err != nil {
		f3, err = fs.Open("style.css")
	}
	if err != nil {
		t.Fatalf("failed to open style.css from web fs: %v", err)
	}
	f3.Close()
}

func TestWebOAuthSanitizeCode(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"4/0Abc123xyz", "4/0Abc123xyz"},
		{"https://antigravity.google/oauth-callback?code=4%2F0Abc123xyz&state=xyz", "4/0Abc123xyz"},
		{"https://localhost:8080/callback?state=abc&code=my-secret-code", "my-secret-code"},
		{"  code=trimmed_code&scope=cloud  ", "trimmed_code"},
	}

	for _, c := range cases {
		got := sanitizeCode(c.input)
		if got != c.want {
			t.Errorf("sanitizeCode(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestWebPKCESessionCreation(t *testing.T) {
	sess, err := globalAuthStore.createSession()
	if err != nil {
		t.Fatalf("createSession failed: %v", err)
	}
	if sess.ID == "" {
		t.Fatal("empty session id")
	}
	if !strings.HasPrefix(sess.AuthURL, "https://accounts.google.com") {
		t.Errorf("unexpected auth URL: %s", sess.AuthURL)
	}
	if !strings.Contains(sess.AuthURL, "code_challenge=") {
		t.Errorf("missing code_challenge in auth URL: %s", sess.AuthURL)
	}

	found, ok := globalAuthStore.getSession(sess.ID)
	if !ok || found.Verifier == "" {
		t.Fatalf("failed to retrieve stored session with verifier")
	}

	globalAuthStore.removeSession(sess.ID)
	if _, ok := globalAuthStore.getSession(sess.ID); ok {
		t.Fatalf("session was not removed")
	}
}

func TestWebResolveUpstream(t *testing.T) {
	// 1. With Proxy
	target, headers, err := resolveUpstream("http://127.0.0.1:8085", "token123", "")
	if err != nil {
		t.Fatalf("resolveUpstream proxy failed: %v", err)
	}
	if target != "ws://127.0.0.1:8085/v1/live" {
		t.Errorf("target = %q, want ws://127.0.0.1:8085/v1/live", target)
	}
	if headers.Get("Authorization") != "Bearer token123" {
		t.Errorf("header = %q, want Bearer token123", headers.Get("Authorization"))
	}

	// 2. Direct with API Key
	target, _, err = resolveUpstream("", "", "AIzaTestKey")
	if err != nil {
		t.Fatalf("resolveUpstream direct api key failed: %v", err)
	}
	if !strings.Contains(target, "key=AIzaTestKey") {
		t.Errorf("target missing key: %s", target)
	}

	// 3. Direct with OAuth Bearer token (no 8085 proxy required)
	target, headers, err = resolveUpstream("", "oauth-tok-123", "")
	if err != nil {
		t.Fatalf("resolveUpstream direct oauth failed: %v", err)
	}
	if strings.Contains(target, "8085") {
		t.Errorf("oauth upstream should not point to 8085, got %s", target)
	}
	if headers.Get("Authorization") != "Bearer oauth-tok-123" {
		t.Errorf("Authorization header = %q, want Bearer oauth-tok-123", headers.Get("Authorization"))
	}
}

func TestWebSessionsPersistenceAPI(t *testing.T) {
	tempDir := filepath.Join(os.TempDir(), "livechat_test_sessions_"+time.Now().Format("20060102150405"))
	_ = os.MkdirAll(tempDir, 0755)
	defer os.RemoveAll(tempDir)

	voice.SetLiveSessionDirOverride(tempDir)
	defer voice.SetLiveSessionDirOverride("")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		enableCORS(w)
		switch r.Method {
		case http.MethodGet:
			id := r.URL.Query().Get("id")
			if id != "" {
				sess, err := voice.FindLiveSession(id)
				if err != nil {
					writeJSONError(w, http.StatusNotFound, err.Error())
					return
				}
				writeJSON(w, http.StatusOK, sess)
				return
			}
			summaries, err := voice.ListLiveSessions()
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, summaries)
		case http.MethodPost:
			var data voice.LiveSessionData
			if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
				writeJSONError(w, http.StatusBadRequest, err.Error())
				return
			}
			if data.ID == "" {
				data.ID = voice.GenerateLiveSessionID()
			}
			data.UpdatedAt = time.Now()
			if err := voice.SaveLiveSession(&data); err != nil {
				writeJSONError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"status": "saved", "id": data.ID})
		case http.MethodDelete:
			id := r.URL.Query().Get("id")
			if err := voice.DeleteLiveSession(id); err != nil {
				writeJSONError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"status": "deleted", "id": id})
		}
	})

	// 1. Initial list should be empty
	req1 := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("GET /api/sessions status = %d", rec1.Code)
	}
	var list []voice.LiveSessionSummary
	_ = json.Unmarshal(rec1.Body.Bytes(), &list)
	if len(list) != 0 {
		t.Fatalf("expected 0 sessions initially, got %d", len(list))
	}

	// 2. POST save session
	newSession := voice.LiveSessionData{
		ID:        "session-test-123",
		Title:     "Test Session for Parity",
		Workspace: "/test/workspace",
		Model:     "models/gemini-3.8-live",
		Voice:     "Cleo",
		CreatedAt: time.Now(),
		Turns: []voice.LiveTurnRecord{
			{Role: "user", Content: "Hello LiveChat!", AudioID: "user_turn_1", AudioSampleRate: 16000},
			{Role: "agent", Content: "Hello! How can I assist you today?", AudioID: "turn_2", AudioSampleRate: 24000},
		},
	}
	payloadBytes, _ := json.Marshal(newSession)
	req2 := httptest.NewRequest(http.MethodPost, "/api/sessions", bytes.NewReader(payloadBytes))
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("POST /api/sessions status = %d", rec2.Code)
	}

	// 3. GET list should have 1 session
	req3 := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req3)
	var list2 []voice.LiveSessionSummary
	_ = json.Unmarshal(rec3.Body.Bytes(), &list2)
	if len(list2) != 1 {
		t.Fatalf("expected 1 session after post, got %d", len(list2))
	}
	if list2[0].ID != "session-test-123" {
		t.Errorf("session ID mismatch: got %s, want session-test-123", list2[0].ID)
	}

	// 4. GET specific session
	req4 := httptest.NewRequest(http.MethodGet, "/api/sessions?id=session-test-123", nil)
	rec4 := httptest.NewRecorder()
	handler.ServeHTTP(rec4, req4)
	var single voice.LiveSessionData
	_ = json.Unmarshal(rec4.Body.Bytes(), &single)
	if len(single.Turns) != 2 {
		t.Errorf("expected 2 turns, got %d", len(single.Turns))
	} else if single.Turns[0].AudioID != "user_turn_1" || single.Turns[0].AudioSampleRate != 16000 || single.Turns[1].AudioID != "turn_2" || single.Turns[1].AudioSampleRate != 24000 {
		t.Errorf("expected audio metadata round-trip, got %+v", single.Turns)
	}

	// 5. DELETE session
	req5 := httptest.NewRequest(http.MethodDelete, "/api/sessions?id=session-test-123", nil)
	rec5 := httptest.NewRecorder()
	handler.ServeHTTP(rec5, req5)
	if rec5.Code != http.StatusOK {
		t.Errorf("DELETE returned HTTP %d", rec5.Code)
	}

	// 6. List should be empty again
	req6 := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	rec6 := httptest.NewRecorder()
	handler.ServeHTTP(rec6, req6)
	var list4 []voice.LiveSessionSummary
	_ = json.Unmarshal(rec6.Body.Bytes(), &list4)
	if len(list4) != 0 {
		t.Errorf("expected 0 sessions after delete, got %d", len(list4))
	}
}

func TestWebSearchAPI(t *testing.T) {
	origSearch := performWebSearch
	defer func() { performWebSearch = origSearch }()

	performWebSearch = func(ctx context.Context, query, ws string) (string, error) {
		return "Mock search result for " + query, nil
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("q")
		if query == "" {
			writeJSONError(w, http.StatusBadRequest, "missing search query")
			return
		}
		result, err := performWebSearch(r.Context(), query, "")
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"query":  query,
			"result": result,
		})
	})

	// Empty query returns 400
	req1 := httptest.NewRequest(http.MethodGet, "/api/search", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty query, got %d", rec1.Code)
	}

	// Valid query returns 200 and mocked result
	req2 := httptest.NewRequest(http.MethodGet, "/api/search?q=golang+testing", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec2.Code)
	}
	var res map[string]interface{}
	_ = json.Unmarshal(rec2.Body.Bytes(), &res)
	if res["result"] != "Mock search result for golang testing" {
		t.Errorf("unexpected search result: %v", res["result"])
	}
}

func TestFormatThinkingInLogs(t *testing.T) {
	raw := "Let me consider this. <think>This is deep internal deliberation.</think> The answer is 42."
	formatted := formatThinkingInLogs(raw)

	if strings.Contains(formatted, "<think>") || strings.Contains(formatted, "</think>") {
		t.Errorf("raw tags should be removed, got: %s", formatted)
	}
	if !strings.Contains(formatted, "Extended Thinking Process") {
		t.Errorf("expected thinking process header, got: %s", formatted)
	}
	if !strings.Contains(formatted, "This is deep internal deliberation.") {
		t.Errorf("thought body missing, got: %s", formatted)
	}
}

type mockWebLiveBackend struct {
	createFn func(ctx context.Context, cfg voice.LiveSessionConfig) (voice.LiveSession, error)
}

func (m *mockWebLiveBackend) CreateSession(ctx context.Context, cfg voice.LiveSessionConfig) (voice.LiveSession, error) {
	return m.createFn(ctx, cfg)
}

type mockWebLiveSession struct {
	ctx     context.Context
	audioCh chan []byte
	textCh  chan string
	recvCh  chan *voice.LiveServerEvent
	closed  chan struct{}
}

func newMockWebLiveSession(ctx context.Context) *mockWebLiveSession {
	return &mockWebLiveSession{
		ctx:     ctx,
		audioCh: make(chan []byte, 8),
		textCh:  make(chan string, 8),
		recvCh:  make(chan *voice.LiveServerEvent, 8),
		closed:  make(chan struct{}),
	}
}

func (s *mockWebLiveSession) SendAudio(pcm16k []byte) error {
	if s.ctx != nil && s.ctx.Err() != nil {
		return s.ctx.Err()
	}
	s.audioCh <- append([]byte(nil), pcm16k...)
	return nil
}
func (s *mockWebLiveSession) SendText(text string) error {
	if s.ctx != nil && s.ctx.Err() != nil {
		return s.ctx.Err()
	}
	s.textCh <- text
	return nil
}
func (s *mockWebLiveSession) SendToolResponse(string, string, any) error { return nil }
func (s *mockWebLiveSession) SendThinkerNote(string) error               { return nil }
func (s *mockWebLiveSession) SendThinkerAnswer(string) error             { return nil }
func (s *mockWebLiveSession) SendWaitingOnUser(string) error             { return nil }
func (s *mockWebLiveSession) SendTaskStopped() error                     { return nil }
func (s *mockWebLiveSession) SendMedia(string, []byte) error             { return nil }
func (s *mockWebLiveSession) Receive() (*voice.LiveServerEvent, error) {
	var ctxDone <-chan struct{}
	if s.ctx != nil {
		ctxDone = s.ctx.Done()
	}
	select {
	case <-ctxDone:
		return nil, s.ctx.Err()
	case ev, ok := <-s.recvCh:
		if !ok {
			return nil, io.EOF
		}
		return ev, nil
	case <-s.closed:
		return nil, io.EOF
	}
}
func (s *mockWebLiveSession) Close() error {
	select {
	case <-s.closed:
	default:
		close(s.closed)
	}
	return nil
}

func TestHandleLiveWebSocketOAuthBridge(t *testing.T) {
	origFactory := oauthLiveBackendFactory
	origKeyFn := currentGeminiAPIKeyFn
	defer func() {
		oauthLiveBackendFactory = origFactory
		currentGeminiAPIKeyFn = origKeyFn
	}()
	currentGeminiAPIKeyFn = func() string { return "" }

	var mockSess *mockWebLiveSession
	var gotToken string
	var gotCfg voice.LiveSessionConfig

	oauthLiveBackendFactory = func(token string) voice.LiveBackend {
		gotToken = token
		return &mockWebLiveBackend{
			createFn: func(ctx context.Context, cfg voice.LiveSessionConfig) (voice.LiveSession, error) {
				gotCfg = cfg
				mockSess = newMockWebLiveSession(ctx)
				return mockSess, nil
			},
		}
	}

	srv := httptest.NewServer(handleLiveWebSocketWithDefaultProxy(""))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/live-ws?mode=oauth&token=oauth-secret-tok"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket.Dial failed: %v", err)
	}
	defer conn.CloseNow()

	setupPayload := `{"setup":{"model":"models/gemini-3.8-live","generationConfig":{"speechConfig":{"voiceConfig":{"prebuiltVoiceConfig":{"voiceName":"Aoede"}}}},"systemInstruction":{"parts":[{"text":"Be concise."}]}}}`
	if err := conn.Write(ctx, websocket.MessageText, []byte(setupPayload)); err != nil {
		t.Fatalf("write setup failed: %v", err)
	}

	_, setupAck, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read setupComplete failed: %v", err)
	}
	if !strings.Contains(string(setupAck), "setupComplete") {
		t.Fatalf("expected setupComplete, got %s", string(setupAck))
	}
	if gotToken != "oauth-secret-tok" {
		t.Errorf("gotToken = %q, want oauth-secret-tok", gotToken)
	}
	if gotCfg.Model != "models/gemini-3.8-live" || gotCfg.Voice != "Aoede" || gotCfg.SystemPrompt != "Be concise." {
		t.Errorf("unexpected LiveSessionConfig: %+v", gotCfg)
	}

	// Send audio chunk from browser -> mock session
	pcmB64 := base64.StdEncoding.EncodeToString([]byte{1, 2, 3, 4})
	audioMsg := `{"realtimeInput":{"audio":{"mimeType":"audio/pcm;rate=16000","data":"` + pcmB64 + `"}}}`
	if err := conn.Write(ctx, websocket.MessageText, []byte(audioMsg)); err != nil {
		t.Fatalf("write audio failed: %v", err)
	}
	select {
	case gotAudio := <-mockSess.audioCh:
		if !bytes.Equal(gotAudio, []byte{1, 2, 3, 4}) {
			t.Errorf("gotAudio = %v, want [1 2 3 4]", gotAudio)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for SendAudio")
	}

	// Emit server event -> browser WebSocket
	mockSess.recvCh <- &voice.LiveServerEvent{
		VoiceTranscript: "Hello from OAuth bridge",
		TurnComplete:    true,
	}

	var sawTranscript, sawTurnComplete bool
	for i := 0; i < 2; i++ {
		_, msg, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read server event %d failed: %v", i, err)
		}
		s := string(msg)
		if strings.Contains(s, "Hello from OAuth bridge") {
			sawTranscript = true
		}
		if strings.Contains(s, "turnComplete") {
			sawTurnComplete = true
		}
	}
	if !sawTranscript || !sawTurnComplete {
		t.Errorf("missing expected server events: transcript=%v turnComplete=%v", sawTranscript, sawTurnComplete)
	}
}

func TestHandleLiveWebSocketOAuthPrefersMachineAPIKey(t *testing.T) {
	origFactory := oauthLiveBackendFactory
	origKeyFn := currentGeminiAPIKeyFn
	defer func() {
		oauthLiveBackendFactory = origFactory
		currentGeminiAPIKeyFn = origKeyFn
	}()

	oauthCalled := false
	oauthLiveBackendFactory = func(token string) voice.LiveBackend {
		oauthCalled = true
		return &mockWebLiveBackend{}
	}
	currentGeminiAPIKeyFn = func() string { return "AIzaMachineKey123" }

	var gotUpstreamKey string
	upstreamSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUpstreamKey = r.URL.Query().Get("key")
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer c.CloseNow()
		_, msg, err := c.Read(r.Context())
		if err != nil {
			return
		}
		if strings.Contains(string(msg), "setup") {
			_ = c.Write(r.Context(), websocket.MessageText, []byte(`{"setupComplete":{}}`))
		}
		_, _, _ = c.Read(r.Context())
	}))
	defer upstreamSrv.Close()

	t.Setenv("OPENAGY_VOICE_URL", "ws"+strings.TrimPrefix(upstreamSrv.URL, "http"))

	srv := httptest.NewServer(handleLiveWebSocketWithDefaultProxy(""))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/live-ws?mode=oauth&token=oauth-secret-tok"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket.Dial failed: %v", err)
	}
	defer conn.CloseNow()

	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"setup":{"model":"models/gemini-3.8-live"}}`)); err != nil {
		t.Fatalf("write setup failed: %v", err)
	}
	_, ack, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read setupComplete failed: %v", err)
	}
	if !strings.Contains(string(ack), "setupComplete") {
		t.Fatalf("expected setupComplete, got %s", string(ack))
	}
	if oauthCalled {
		t.Errorf("expected machine Gemini API key to take priority over AICode OAuth bridge")
	}
	if gotUpstreamKey != "AIzaMachineKey123" {
		t.Errorf("gotUpstreamKey = %q, want AIzaMachineKey123", gotUpstreamKey)
	}
}

func TestHandleLiveWebSocketOAuthPermissionDeniedClosesCleanly(t *testing.T) {
	origFactory := oauthLiveBackendFactory
	origKeyFn := currentGeminiAPIKeyFn
	defer func() {
		oauthLiveBackendFactory = origFactory
		currentGeminiAPIKeyFn = origKeyFn
	}()
	currentGeminiAPIKeyFn = func() string { return "" }

	permSess := &permDeniedLiveSession{err: errors.New("rpc error: code = PermissionDenied desc = The caller does not have permission")}
	oauthLiveBackendFactory = func(token string) voice.LiveBackend {
		return &mockWebLiveBackend{
			createFn: func(ctx context.Context, cfg voice.LiveSessionConfig) (voice.LiveSession, error) {
				return permSess, nil
			},
		}
	}

	srv := httptest.NewServer(handleLiveWebSocketWithDefaultProxy(""))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/live-ws?mode=oauth&token=oauth-secret-tok"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket.Dial failed: %v", err)
	}
	defer conn.CloseNow()

	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"setup":{"model":"models/gemini-3.8-live"}}`)); err != nil {
		t.Fatalf("write setup failed: %v", err)
	}
	_, _, err = conn.Read(ctx) // setupComplete
	if err != nil {
		t.Fatalf("read setupComplete failed: %v", err)
	}

	_, _, err = conn.Read(ctx)
	if err == nil {
		t.Fatal("expected close error on PermissionDenied")
	}
	if status := websocket.CloseStatus(err); status != websocket.StatusPolicyViolation {
		t.Errorf("close status = %v, want StatusPolicyViolation (%v)", status, websocket.StatusPolicyViolation)
	}
}

type permDeniedLiveSession struct {
	err error
}

func (s *permDeniedLiveSession) SendAudio([]byte) error                     { return s.err }
func (s *permDeniedLiveSession) SendText(string) error                      { return s.err }
func (s *permDeniedLiveSession) SendToolResponse(string, string, any) error { return nil }
func (s *permDeniedLiveSession) SendThinkerNote(string) error               { return nil }
func (s *permDeniedLiveSession) SendThinkerAnswer(string) error             { return nil }
func (s *permDeniedLiveSession) SendWaitingOnUser(string) error             { return nil }
func (s *permDeniedLiveSession) SendTaskStopped() error                     { return nil }
func (s *permDeniedLiveSession) SendMedia(string, []byte) error             { return nil }
func (s *permDeniedLiveSession) Receive() (*voice.LiveServerEvent, error) {
	time.Sleep(20 * time.Millisecond)
	return nil, s.err
}
func (s *permDeniedLiveSession) Close() error { return nil }

func TestHandleLiveWebSocketProxyModeRequiresURL(t *testing.T) {
	srv := httptest.NewServer(handleLiveWebSocketWithDefaultProxy(""))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/live-ws?mode=proxy"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket.Dial failed: %v", err)
	}
	defer conn.CloseNow()

	_, _, err = conn.Read(ctx)
	if err == nil {
		t.Fatal("expected close error when proxy mode has no URL")
	}
	if status := websocket.CloseStatus(err); status != websocket.StatusPolicyViolation {
		t.Errorf("close status = %v, want StatusPolicyViolation (%v)", status, websocket.StatusPolicyViolation)
	}
}

func TestHandleProxyChatRequiresURL(t *testing.T) {
	rec := httptest.NewRecorder()
	err := HandleChat(context.Background(), rec, &ChatRequest{
		AuthMode: "proxy",
		Prompt:   "hello",
	})
	if err == nil {
		t.Fatal("expected error when proxy mode has empty ProxyURL")
	}
	if !strings.Contains(err.Error(), "no Pigasus proxy URL") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestResolveBrowserCommandsTermux(t *testing.T) {
	lookAll := func(name string) (string, error) {
		return "/data/data/com.termux/files/usr/bin/" + name, nil
	}
	cmds := resolveBrowserCommands("linux", "0.118.0", "/data/data/com.termux/files/usr", "http://127.0.0.1:8090", lookAll)
	if len(cmds) == 0 || cmds[0][0] != "termux-open-url" {
		t.Fatalf("expected termux-open-url first, got %v", cmds)
	}

	lookOnlyAM := func(name string) (string, error) {
		if name == "am" {
			return "/system/bin/am", nil
		}
		return "", errors.New("not found")
	}
	cmds2 := resolveBrowserCommands("android", "", "", "http://127.0.0.1:8090", lookOnlyAM)
	if len(cmds2) != 1 || cmds2[0][0] != "am" {
		t.Fatalf("expected am fallback, got %v", cmds2)
	}
}

func TestWebAssetsNoHardcoded8085AndMicGuard(t *testing.T) {
	fs, _ := resolveWebFileSystem("")

	readAsset := func(name string) string {
		f, err := fs.Open("/" + name)
		if err != nil {
			f, err = fs.Open(name)
		}
		if err != nil {
			t.Fatalf("open %s: %v", name, err)
		}
		defer f.Close()
		b, err := io.ReadAll(f)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		return string(b)
	}

	appJS := readAsset("app.js")
	if strings.Contains(appJS, "127.0.0.1:8085") || strings.Contains(appJS, "localhost:8085") {
		t.Errorf("app.js should not contain hardcoded 8085 fallback URLs")
	}
	if !strings.Contains(appJS, "isMicSessionActive") || !strings.Contains(appJS, "micCtx.sampleRate") {
		t.Errorf("app.js missing micCtx lifecycle guard before reading sampleRate")
	}

	indexHTML := readAsset("index.html")
	if strings.Contains(indexHTML, "maximum-scale=1.0") {
		t.Errorf("index.html should not disable pinch-to-zoom with maximum-scale=1.0")
	}
	if strings.Contains(indexHTML, `value="http://127.0.0.1:8085"`) {
		t.Errorf("index.html should not prefill cfg-proxy-url with 127.0.0.1:8085")
	}
	for _, id := range []string{"st-mic-tab", "btn-mic-test-toggle", "btn-mic-test-record", "mic-test-waveform", "mic-test-device-select", "cli-auth-auto-banner", "oauth-fallback-details", "composer-audio-popover", `popovertarget="composer-audio-popover"`, "composer-live-strip", "composer-voice-controls", "btn-header-new-chat", "live-status-pill", "composer-unified-label", "composer-model-label", "voice-persona-grid", "audition-samples-group", `data-sample="1"`, `data-sample="2"`, `data-sample="3"`, `rel="manifest" href="manifest.webmanifest"`, `name="theme-color"`, `value="aggregate"`, "popover-opt-aec", "popover-opt-ns", "popover-opt-agc", "popover-aec-badge"} {
		if !strings.Contains(indexHTML, id) {
			t.Errorf("index.html missing expected element %q", id)
		}
	}
	for _, persona := range []string{"Fola", "Arlo", "Cleo", "Finn", "Gero", "Lora", "Neno", "Olin", "Tari"} {
		if !strings.Contains(indexHTML, `data-voice="`+persona+`"`) {
			t.Errorf("index.html missing Antigravity persona chip %q", persona)
		}
	}
	if strings.Contains(indexHTML, "header-model-pill") || strings.Contains(indexHTML, `class="live-visualizer-bar`) {
		t.Errorf("index.html should keep top bar compact without header-model-pill or standalone live-visualizer-bar banner")
	}
	if strings.Contains(indexHTML, "btn-clear-transcript") || strings.Contains(indexHTML, "mic-status-text") || strings.Contains(indexHTML, "audio-state-indicator") {
		t.Errorf("index.html should not include btn-clear-transcript or duplicate status labels")
	}
	styleCSS := readAsset("style.css")
	if strings.Contains(styleCSS, "rgba(16, 21, 36, 0.7)") {
		t.Errorf("style.css should use theme CSS variables for sidebar-search-input instead of hardcoded dark rgba")
	}
	for _, cls := range []string{".msg-translation-divider", ".msg-translation-wrap", ".btn-translate-msg", ".popover-switch-row", ".popover-switch-input"} {
		if !strings.Contains(styleCSS, cls) {
			t.Errorf("style.css missing translation style %q", cls)
		}
	}
	manifestRaw := readAsset("manifest.webmanifest")
	var manifest struct {
		Name      string `json:"name"`
		ShortName string `json:"short_name"`
		StartURL  string `json:"start_url"`
		Display   string `json:"display"`
		Icons     []struct {
			Src   string `json:"src"`
			Sizes string `json:"sizes"`
			Type  string `json:"type"`
		} `json:"icons"`
	}
	if err := json.Unmarshal([]byte(manifestRaw), &manifest); err != nil {
		t.Fatalf("manifest.webmanifest invalid JSON: %v", err)
	}
	if manifest.Display != "standalone" || manifest.StartURL == "" || len(manifest.Icons) < 3 {
		t.Errorf("manifest.webmanifest incomplete: %+v", manifest)
	}
	for _, iconFile := range []string{"icon-192.png", "icon-512.png", "icon-maskable-512.png", "icon.svg"} {
		if len(readAsset(iconFile)) == 0 {
			t.Errorf("missing PWA icon asset %q", iconFile)
		}
	}
	for _, fn := range []string{"setupMicTestSection", "startMicTest", "stopMicTest", "refreshMicDiagnostics", "pigasus_oauth_manual_clear", "setupToolbarRovingTabindex", "translateTextWithGeminiFlash", "ensureBubbleActionBar", "gemini-3.8-flash", "pigasus_live_audio_v1", "saveTurnAudioToCache", "loadTurnAudioFromCache", "hasTurnAudioInCache", "serverAggregateEnabled", "new URLSearchParams({ mode: state.authMode })", "authMode: localStorage.getItem('pigasus_auth_mode') || 'aggregate'", "syncMicConstraintUI", "applyLiveMicConstraints", "setMicConstraint"} {
		if !strings.Contains(appJS, fn) {
			t.Errorf("app.js missing expected symbol %q", fn)
		}
	}
}

func setupAggregateTestPool(t *testing.T, accs ...accounts.Account) *accountrouting.Router {
	t.Helper()
	dir := t.TempDir()
	registry := filepath.Join(dir, "accounts.json")
	config := filepath.Join(dir, "config.json")

	active := ""
	if len(accs) > 0 {
		active = accs[0].Email
	}
	doc := map[string]any{
		"active":   active,
		"accounts": accs,
	}
	regBytes, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshal accounts: %v", err)
	}
	if err := os.WriteFile(registry, regBytes, 0o600); err != nil {
		t.Fatalf("write accounts.json: %v", err)
	}
	cfgDoc := map[string]any{
		"account_pool": map[string]any{
			"enabled":               true,
			"max_attempts":          3,
			"switch_on_quota_error": true,
			"aggregate":             map[string]any{"enabled": true},
		},
	}
	cfgBytes, err := json.MarshalIndent(cfgDoc, "", "  ")
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(config, cfgBytes, 0o600); err != nil {
		t.Fatalf("write config.json: %v", err)
	}

	t.Setenv("OPENAGY_ACCOUNTS_CONFIG", registry)
	t.Setenv("OPENAGY_CONFIG", config)
	t.Setenv("OPENAGY_CONFIG_HOME", dir)

	r := accountrouting.NewRouter()
	r.SetPolicy(effectiveAggregatePolicy)
	r.SetLoadShared(func() (*auth.AntigravityToken, string, error) {
		if len(accs) > 0 && accs[0].Token != nil {
			return accs[0].Token, accs[0].Email, nil
		}
		return nil, "", auth.ErrNoToken
	})
	r.SetFallback(func(context.Context) (string, error) {
		if len(accs) > 0 && accs[0].Token != nil {
			return accs[0].Token.Token.AccessToken, nil
		}
		return "", auth.ErrNoToken
	})
	resetAggregateRouterForTest(r)
	t.Cleanup(func() {
		resetAggregateRouterForTest(nil)
	})
	return r
}

func makeTestPoolAccount(email, accessToken string, remaining float64) accounts.Account {
	return accounts.Account{
		Email: email,
		Token: &auth.AntigravityToken{
			Token: auth.AntigravityAccessToken{
				AccessToken: accessToken,
				TokenType:   "Bearer",
				Expiry:      time.Now().Add(1 * time.Hour).Format(time.RFC3339),
			},
			IDTokenEmail: email,
		},
		Quota: &auth.QuotaSnapshot{
			FetchedAt: time.Now(),
			Groups: []auth.QuotaGroupSnapshot{
				{
					Name: "Gemini",
					Buckets: []auth.QuotaBucketSnapshot{
						{DisplayName: "Gemini 3.8 Flash", RemainingFraction: remaining},
					},
				},
			},
		},
	}
}

func TestHandleChatAggregateFailoverOn503AndStreamError(t *testing.T) {
	origBackoff := aggregateBackoffFn
	origKeyFn := currentGeminiAPIKeyFn
	defer func() {
		aggregateBackoffFn = origBackoff
		currentGeminiAPIKeyFn = origKeyFn
	}()
	aggregateBackoffFn = func(int, string) time.Duration { return 0 }
	currentGeminiAPIKeyFn = func() string { return "" }

	acc1 := makeTestPoolAccount("exhausted@example.com", "tok-acc1", 0.95)
	acc2 := makeTestPoolAccount("healthy@example.com", "tok-acc2", 0.80)
	acc3 := makeTestPoolAccount("backup@example.com", "tok-acc3", 0.70)
	router := setupAggregateTestPool(t, acc1, acc2, acc3)

	var seenAuths []string
	cloudcodeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHdr := r.Header.Get("Authorization")
		seenAuths = append(seenAuths, authHdr)
		switch authHdr {
		case "Bearer tok-acc1":
			// Simulate exact 503 MODEL_CAPACITY_EXHAUSTED error reported by user
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"code":503,"message":"No capacity available for model gemini-3.8-flash-high on the server","status":"UNAVAILABLE","details":[{"reason":"MODEL_CAPACITY_EXHAUSTED"}]}}`))
		case "Bearer tok-acc2":
			// Simulate immediate HTTP 200 SSE error frame before content
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("data: {\"error\":{\"code\":429,\"status\":\"RESOURCE_EXHAUSTED\",\"message\":\"quota exceeded\"}}\n\n"))
		case "Bearer tok-acc3":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"Translated via aggregate pool!\"}]}}]}}\n\n"))
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer cloudcodeSrv.Close()
	t.Setenv("CLOUD_CODE_URL", cloudcodeSrv.URL)

	rec := httptest.NewRecorder()
	err := HandleChat(context.Background(), rec, &ChatRequest{
		AuthMode: "aggregate",
		Model:    "gemini-3.8-flash",
		Prompt:   "Translate hello",
		Stream:   true,
	})
	if err != nil {
		t.Fatalf("HandleChat aggregate failed: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Translated via aggregate pool!") {
		t.Fatalf("expected translated output from acc3, got: %s", rec.Body.String())
	}
	if len(seenAuths) != 3 {
		t.Fatalf("expected 3 attempts across pool accounts, got %v", seenAuths)
	}
	if !router.IsExcluded("exhausted@example.com", "gemini") {
		t.Errorf("expected exhausted@example.com to be excluded for gemini family after 503")
	}
	if !router.IsExcluded("healthy@example.com", "gemini") {
		t.Errorf("expected healthy@example.com to be excluded for gemini family after immediate 429 SSE error")
	}
}

func TestHandleLiveWebSocketAggregateFailover(t *testing.T) {
	origFactory := oauthLiveBackendFactory
	origKeyFn := currentGeminiAPIKeyFn
	origBackoff := aggregateBackoffFn
	defer func() {
		oauthLiveBackendFactory = origFactory
		currentGeminiAPIKeyFn = origKeyFn
		aggregateBackoffFn = origBackoff
	}()
	currentGeminiAPIKeyFn = func() string { return "" }
	aggregateBackoffFn = func(int, string) time.Duration { return 0 }

	acc1 := makeTestPoolAccount("live1@example.com", "live-tok-1", 0.95)
	acc2 := makeTestPoolAccount("live2@example.com", "live-tok-2", 0.85)
	router := setupAggregateTestPool(t, acc1, acc2)

	var triedTokens []string
	oauthLiveBackendFactory = func(token string) voice.LiveBackend {
		triedTokens = append(triedTokens, token)
		return &mockWebLiveBackend{
			createFn: func(ctx context.Context, cfg voice.LiveSessionConfig) (voice.LiveSession, error) {
				if token == "live-tok-1" {
					return nil, errors.New("rpc error: code = Unavailable desc = MODEL_CAPACITY_EXHAUSTED")
				}
				sess := newMockWebLiveSession(ctx)
				sess.recvCh <- &voice.LiveServerEvent{VoiceTranscript: "Aggregate live ready", TurnComplete: true}
				return sess, nil
			},
		}
	}

	srv := httptest.NewServer(handleLiveWebSocketWithDefaultProxy(""))
	defer srv.Close()

	// Frontend connects with mode=aggregate and NO token or api_key
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/live-ws?mode=aggregate"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket.Dial failed: %v", err)
	}
	defer conn.CloseNow()

	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"setup":{"model":"models/gemini-3.8-live"}}`)); err != nil {
		t.Fatalf("write setup failed: %v", err)
	}
	_, ack, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read setupComplete failed: %v", err)
	}
	if !strings.Contains(string(ack), "setupComplete") {
		t.Fatalf("expected setupComplete, got %s", string(ack))
	}
	_, evMsg, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read server event failed: %v", err)
	}
	if !strings.Contains(string(evMsg), "Aggregate live ready") {
		t.Fatalf("expected transcript from acc2, got %s", string(evMsg))
	}
	if len(triedTokens) != 2 || triedTokens[0] != "live-tok-1" || triedTokens[1] != "live-tok-2" {
		t.Fatalf("unexpected token failover sequence: %v", triedTokens)
	}
	if !router.IsExcluded("live1@example.com", "gemini") {
		t.Errorf("expected live1@example.com to be excluded after Unavailable error")
	}
}

func TestAggregatePoolStatusAndDefaultAuthMode(t *testing.T) {
	origBackoff := aggregateBackoffFn
	origKeyFn := currentGeminiAPIKeyFn
	defer func() {
		aggregateBackoffFn = origBackoff
		currentGeminiAPIKeyFn = origKeyFn
	}()
	aggregateBackoffFn = func(int, string) time.Duration { return 0 }
	currentGeminiAPIKeyFn = func() string { return "" }

	acc1 := makeTestPoolAccount("pool1@example.com", "pool-tok-1", 0.90)
	acc2 := makeTestPoolAccount("pool2@example.com", "pool-tok-2", 0.80)
	router := setupAggregateTestPool(t, acc1, acc2)

	enabled, total, healthy := aggregatePoolStatus(context.Background())
	if !enabled || total != 2 || healthy != 2 {
		t.Fatalf("initial aggregatePoolStatus = (%v, %d, %d), want (true, 2, 2)", enabled, total, healthy)
	}

	router.Exclude("pool1@example.com")
	enabled, total, healthy = aggregatePoolStatus(context.Background())
	if !enabled || total != 2 || healthy != 1 {
		t.Fatalf("after exclude aggregatePoolStatus = (%v, %d, %d), want (true, 2, 1)", enabled, total, healthy)
	}

	// Verify HandleChat with empty AuthMode and no APIKey/Token routes via aggregate pool
	cloudcodeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer pool-tok-2" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"Default aggregate OK\"}]}}]}}\n\n"))
	}))
	defer cloudcodeSrv.Close()
	t.Setenv("CLOUD_CODE_URL", cloudcodeSrv.URL)

	rec := httptest.NewRecorder()
	err := HandleChat(context.Background(), rec, &ChatRequest{
		Model:  "gemini-3.8-flash",
		Prompt: "ping",
		Stream: true,
	})
	if err != nil {
		t.Fatalf("HandleChat default aggregate failed: %v", err)
	}
	if !strings.Contains(rec.Body.String(), "Default aggregate OK") {
		t.Fatalf("expected Default aggregate OK, got: %s", rec.Body.String())
	}
}
