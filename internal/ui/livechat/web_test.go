package livechat

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
			{Role: "user", Content: "Hello LiveChat!"},
			{Role: "agent", Content: "Hello! How can I assist you today?"},
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
