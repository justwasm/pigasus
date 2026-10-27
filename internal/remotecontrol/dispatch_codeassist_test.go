package remotecontrol

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// codeAssistTestDispatcher swaps the package seam for a fetcher that
// answers without a network, and restores it when the test ends.
func codeAssistTestDispatcher(t *testing.T, state string, err error) *Dispatcher {
	t.Helper()
	old := codeAssistStateFetcher
	codeAssistStateFetcher = func(context.Context) (json.RawMessage, error) {
		if err != nil {
			return nil, err
		}
		return json.RawMessage(state), nil
	}
	t.Cleanup(func() { codeAssistStateFetcher = old })
	// The cache is process-wide: a test must not read another test's
	// answer.
	codeAssistCache.mu.Lock()
	codeAssistCache.state = nil
	codeAssistCache.fetchedAt = time.Time{}
	codeAssistCache.mu.Unlock()
	t.Cleanup(func() {
		codeAssistCache.mu.Lock()
		codeAssistCache.state = nil
		codeAssistCache.fetchedAt = time.Time{}
		codeAssistCache.mu.Unlock()
	})
	d, err2 := NewDispatcher(Config{Session: NewSessionState("test")})
	if err2 != nil {
		t.Fatalf("NewDispatcher: %v", err2)
	}
	return d
}

// TestTheCreditsPanelGetsACodeAssistState pins the answer the Web UI's
// credits panel needs.
//
// 【一手】 the Web UI's service wrapper throws when the member is
// missing:
//
//	if(!r.response)throw Error("[CloudCodeService] loadCodeAssist returned no response")
//
// Without a route the transport answered `{}` and the panel reported
// "ui_client_credits_fetch_failed".
func TestTheCreditsPanelGetsACodeAssistState(t *testing.T) {
	d := codeAssistTestDispatcher(t, `{"currentTier":{"id":"free-tier","name":"Antigravity"},"paidTier":{"id":"g1-pro-tier","name":"Google AI Pro"}}`, nil)

	resText, err := d.DispatchDirect(context.Background(), "GetLoadCodeAssist", "{}")
	if err != nil {
		t.Fatalf("DispatchDirect GetLoadCodeAssist: %v", err)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(resText), &res); err != nil {
		t.Fatalf("Unmarshal result: %v", err)
	}
	response, ok := res["response"].(map[string]any)
	if !ok {
		t.Fatalf("expected a response object, got %v", res["response"])
	}
	current, ok := response["currentTier"].(map[string]any)
	if !ok {
		t.Fatalf("expected a currentTier, got %v", response["currentTier"])
	}
	if current["name"] != "Antigravity" {
		t.Errorf("currentTier.name = %v, want %q", current["name"], "Antigravity")
	}
	paid, ok := response["paidTier"].(map[string]any)
	if !ok {
		t.Fatalf("expected a paidTier, got %v", response["paidTier"])
	}
	if paid["name"] != "Google AI Pro" {
		t.Errorf("paidTier.name = %v, want %q", paid["name"], "Google AI Pro")
	}
}

// TestTheCreditsPanelSurvivesAFailedRead pins that a failed read is
// not an error: the panel is a read, and the Web UI reports a failure
// as "ui_client_credits_fetch_failed".
func TestTheCreditsPanelSurvivesAFailedRead(t *testing.T) {
	d := codeAssistTestDispatcher(t, "", errors.New("offline"))

	resText, err := d.DispatchDirect(context.Background(), "GetLoadCodeAssist", "{}")
	if err != nil {
		t.Fatalf("DispatchDirect GetLoadCodeAssist: %v", err)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(resText), &res); err != nil {
		t.Fatalf("Unmarshal result: %v", err)
	}
	if _, ok := res["response"].(map[string]any); !ok {
		t.Fatalf("expected a response object, got %v", res["response"])
	}
}

// TestTheCreditsPanelIsServedFromTheCache pins that the second read
// does not cost a round trip: the Web UI's own cache is five seconds,
// the clone's is longer, because the tier changes slowly.
func TestTheCreditsPanelIsServedFromTheCache(t *testing.T) {
	reads := 0
	old := codeAssistStateFetcher
	codeAssistStateFetcher = func(context.Context) (json.RawMessage, error) {
		reads++
		return json.RawMessage(`{"currentTier":{"name":"Antigravity"}}`), nil
	}
	t.Cleanup(func() { codeAssistStateFetcher = old })
	codeAssistCache.mu.Lock()
	codeAssistCache.state = nil
	codeAssistCache.fetchedAt = time.Time{}
	codeAssistCache.mu.Unlock()
	t.Cleanup(func() {
		codeAssistCache.mu.Lock()
		codeAssistCache.state = nil
		codeAssistCache.fetchedAt = time.Time{}
		codeAssistCache.mu.Unlock()
	})
	d, err := NewDispatcher(Config{Session: NewSessionState("test")})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	for i := 0; i < 3; i++ {
		if _, err := d.DispatchDirect(context.Background(), "GetLoadCodeAssist", "{}"); err != nil {
			t.Fatalf("DispatchDirect GetLoadCodeAssist: %v", err)
		}
	}
	if reads != 1 {
		t.Errorf("the cloud was asked %d times, want 1", reads)
	}
}
