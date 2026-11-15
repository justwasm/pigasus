package mcp

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
)

func TestHeaderTransport_NoLeakOnRedirect(t *testing.T) {
	var mu sync.Mutex
	var serverAHeaders http.Header
	var serverBHeaders http.Header

	serverB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		serverBHeaders = r.Header.Clone()
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok from B"))
	}))
	defer serverB.Close()

	serverA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		serverAHeaders = r.Header.Clone()
		mu.Unlock()
		if r.URL.Path == "/redirect-to-b" {
			http.Redirect(w, r, serverB.URL+"/b-target", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok from A"))
	}))
	defer serverA.Close()

	originURL, err := url.Parse(serverA.URL)
	if err != nil {
		t.Fatalf("failed to parse serverA URL: %v", err)
	}

	client := &http.Client{
		Transport: &headerTransport{
			headers: map[string]string{
				"X-Secret-Token": "super-secret-123",
				"X-Custom-Env":   "production",
			},
			origin: originURL,
		},
	}

	resp, err := client.Get(serverA.URL + "/redirect-to-b")
	if err != nil {
		t.Fatalf("client.Get failed: %v", err)
	}
	defer resp.Body.Close()

	mu.Lock()
	aTok := serverAHeaders.Get("X-Secret-Token")
	aEnv := serverAHeaders.Get("X-Custom-Env")
	bTok := serverBHeaders.Get("X-Secret-Token")
	bEnv := serverBHeaders.Get("X-Custom-Env")
	mu.Unlock()

	if aTok != "super-secret-123" || aEnv != "production" {
		t.Errorf("Server A did not receive configured headers: X-Secret-Token=%q, X-Custom-Env=%q", aTok, aEnv)
	}

	if bTok != "" || bEnv != "" {
		t.Errorf("Security leak: Server B received configured headers on cross-origin redirect! X-Secret-Token=%q, X-Custom-Env=%q", bTok, bEnv)
	}
}

func TestHeaderTransport_PreservesOnSameOriginRedirect(t *testing.T) {
	var mu sync.Mutex
	var firstReqHeaders http.Header
	var secondReqHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		if r.URL.Path == "/step1" {
			firstReqHeaders = r.Header.Clone()
			http.Redirect(w, r, "/step2", http.StatusFound)
			mu.Unlock()
			return
		}
		secondReqHeaders = r.Header.Clone()
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	originURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("failed to parse server URL: %v", err)
	}

	client := &http.Client{
		Transport: &headerTransport{
			headers: map[string]string{
				"X-Secret-Token": "super-secret-123",
			},
			origin: originURL,
		},
	}

	resp, err := client.Get(server.URL + "/step1")
	if err != nil {
		t.Fatalf("client.Get failed: %v", err)
	}
	defer resp.Body.Close()

	mu.Lock()
	tok1 := firstReqHeaders.Get("X-Secret-Token")
	tok2 := secondReqHeaders.Get("X-Secret-Token")
	mu.Unlock()

	if tok1 != "super-secret-123" {
		t.Errorf("Step 1 did not receive header: got %q", tok1)
	}
	if tok2 != "super-secret-123" {
		t.Errorf("Step 2 (same-origin redirect) lost header: got %q", tok2)
	}
}
