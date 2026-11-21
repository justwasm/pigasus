package main

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSanitizeCode(t *testing.T) {
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

func TestPKCESessionCreation(t *testing.T) {
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

func TestResolveUpstream(t *testing.T) {
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

	// 3. With Proxy and Bearer Token
	target, headers, err = resolveUpstream("http://127.0.0.1:8085", "bearer_token_abc", "")
	if err != nil {
		t.Fatalf("resolveUpstream proxy bearer failed: %v", err)
	}
	if headers.Get("Authorization") != "Bearer bearer_token_abc" {
		t.Errorf("header = %q, want Bearer bearer_token_abc", headers.Get("Authorization"))
	}
}

func TestChatValidation(t *testing.T) {
	req := &ChatRequest{
		Prompt: "",
	}
	w := httptest.NewRecorder()
	err := HandleChat(context.Background(), w, req)
	if err == nil {
		t.Error("expected error on empty prompt, got nil")
	}

	req2 := &ChatRequest{
		Prompt: "hello",
	}
	err = HandleChat(context.Background(), w, req2)
	if err == nil {
		t.Error("expected error on missing credentials, got nil")
	}
}
