package rchost

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/internal/voice"
	"github.com/justwasm/pigasus/websocket"
)

func TestWebHandlers_VoiceEndpoints(t *testing.T) {
	tmpDir := t.TempDir()

	handler, cleanup, err := NewWebHandler(WebHandlerOptions{
		Workspace: tmpDir,
		CSRFToken: "test-csrf-token",
	})
	if err != nil {
		t.Fatalf("NewWebHandler failed: %v", err)
	}
	defer cleanup()

	ts := httptest.NewServer(handler)
	defer ts.Close()

	client := &http.Client{Timeout: 3 * time.Second}

	// 1. GET /voice/health
	resHealth, err := client.Get(ts.URL + "/voice/health")
	if err != nil {
		t.Fatalf("GET /voice/health failed: %v", err)
	}
	defer resHealth.Body.Close()
	if resHealth.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from /voice/health, got %d", resHealth.StatusCode)
	}
	ct := resHealth.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", ct)
	}
	var healthMap map[string]string
	if err := json.NewDecoder(resHealth.Body).Decode(&healthMap); err != nil || healthMap["status"] != "ok" {
		t.Fatalf("unexpected health response: %+v", healthMap)
	}

	// 2. GET /voice/chats
	resChats, err := client.Get(ts.URL + "/voice/chats?conversation_id=conv-test-123")
	if err != nil {
		t.Fatalf("GET /voice/chats failed: %v", err)
	}
	defer resChats.Body.Close()
	if resChats.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from /voice/chats, got %d", resChats.StatusCode)
	}
	body, _ := io.ReadAll(resChats.Body)
	var chatsResp struct {
		Chats []any `json:"chats"`
	}
	if err := json.Unmarshal(body, &chatsResp); err != nil {
		t.Fatalf("failed to decode chats json: %v", err)
	}

	// 3. GET /voice/recordings
	resRec, err := client.Get(ts.URL + "/voice/recordings")
	if err != nil {
		t.Fatalf("GET /voice/recordings failed: %v", err)
	}
	defer resRec.Body.Close()
	if resRec.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from /voice/recordings, got %d", resRec.StatusCode)
	}

	// 4. WebSocket /voice/ws
	wsURL := "ws" + ts.URL[4:] + "/voice/ws?conversation_id=conv-test-123"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Dial with subprotocol
	opts := &websocket.DialOptions{
		Subprotocols: []string{voice.Subprotocol},
	}
	c, _, err := websocket.Dial(ctx, wsURL, opts)
	if err != nil {
		t.Fatalf("websocket.Dial failed: %v", err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	if c.Subprotocol() != voice.Subprotocol {
		t.Fatalf("expected subprotocol %q, got %q", voice.Subprotocol, c.Subprotocol())
	}
}

func TestBuildHostVoiceBackend_Priorities(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("OPENAGY_CONFIG_HOME", t.TempDir())
	t.Setenv("ANTIGRAVITY_APP_DATA_DIR", t.TempDir())
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("OPENAGY_API_KEY", "")
	t.Setenv("PIGASUS_MOCK_VOICE", "")
	t.Setenv("OPENAGY_MOCK_VOICE", "")
	t.Setenv("JETSKI_OAUTH_TOKEN", "")
	t.Setenv("ANTIGRAVITY_ACCESS_TOKEN", "")

	auth.LoadKeychainTokenFn = func() (*auth.AntigravityToken, error) {
		return nil, auth.ErrNoToken
	}
	defer func() { auth.LoadKeychainTokenFn = nil }()

	// 1. With OAuth token, resolves to AICodeLiveBackend with fallback to GeminiLiveBackend
	t.Setenv("JETSKI_OAUTH_TOKEN", "ya29.host-oauth-token")
	be := buildHostVoiceBackend()
	ac, ok := be.(*voice.AICodeLiveBackend)
	if !ok {
		t.Fatalf("expected *voice.AICodeLiveBackend, got %T", be)
	}
	if ac.AccessToken() != "ya29.host-oauth-token" {
		t.Fatalf("expected AccessToken == 'ya29.host-oauth-token', got %q", ac.AccessToken())
	}
	gl, ok := ac.Fallback().(*voice.GeminiLiveBackend)
	if !ok {
		t.Fatalf("expected fallback to be *voice.GeminiLiveBackend, got %T", ac.Fallback())
	}
	if gl.BearerToken() != "ya29.host-oauth-token" {
		t.Fatalf("expected BearerToken == 'ya29.host-oauth-token', got %q", gl.BearerToken())
	}

	// 1b. When BOTH OAuth token and GEMINI_API_KEY are present, OAuth takes precedence!
	t.Setenv("GEMINI_API_KEY", "test-host-api-key")
	be = buildHostVoiceBackend()
	ac, ok = be.(*voice.AICodeLiveBackend)
	if !ok {
		t.Fatalf("expected OAuth to take precedence over GEMINI_API_KEY, got %T", be)
	}
	if ac.AccessToken() != "ya29.host-oauth-token" {
		t.Fatalf("expected AccessToken == 'ya29.host-oauth-token', got %q", ac.AccessToken())
	}
	gl, ok = ac.Fallback().(*voice.GeminiLiveBackend)
	if !ok {
		t.Fatalf("expected fallback to be *voice.GeminiLiveBackend, got %T", ac.Fallback())
	}
	if gl.APIKey() != "test-host-api-key" {
		t.Fatalf("expected fallback APIKey == 'test-host-api-key', got %q", gl.APIKey())
	}

	// 1c. With OPENAGY_VOICE_FORCE_API_KEY, forces GeminiLiveBackend even when OAuth is present
	t.Setenv("OPENAGY_VOICE_FORCE_API_KEY", "1")
	be = buildHostVoiceBackend()
	gl, ok = be.(*voice.GeminiLiveBackend)
	if !ok {
		t.Fatalf("expected *voice.GeminiLiveBackend when forced, got %T", be)
	}
	if gl.APIKey() != "test-host-api-key" {
		t.Fatalf("expected APIKey == 'test-host-api-key', got %q", gl.APIKey())
	}
	t.Setenv("OPENAGY_VOICE_FORCE_API_KEY", "")

	// 2. With GEMINI_API_KEY and no OAuth token, resolves to GeminiLiveBackend with API key
	t.Setenv("JETSKI_OAUTH_TOKEN", "")
	t.Setenv("GEMINI_API_KEY", "test-host-api-key")
	be = buildHostVoiceBackend()
	gl, ok = be.(*voice.GeminiLiveBackend)
	if !ok {
		t.Fatalf("expected *voice.GeminiLiveBackend, got %T", be)
	}
	if gl.APIKey() != "test-host-api-key" {
		t.Fatalf("expected APIKey == 'test-host-api-key', got %q", gl.APIKey())
	}

	// 3. Without credentials, falls back to MockLiveBackend
	t.Setenv("GEMINI_API_KEY", "")
	be = buildHostVoiceBackend()
	if _, ok := be.(*voice.MockLiveBackend); !ok {
		t.Fatalf("expected *voice.MockLiveBackend fallback, got %T", be)
	}
}
