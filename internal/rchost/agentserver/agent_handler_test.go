package agentserver

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/infra/mockremoteagent"
)

func TestResolveWorkspace(t *testing.T) {
	tmpDir := t.TempDir()
	specialDir := filepath.Join(tmpDir, "path with spaces")
	if err := os.MkdirAll(specialDir, 0755); err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	// 1. file:// with URL encoding
	encodedURI := "file://" + url.PathEscape(specialDir)
	session := &mockremoteagent.CascadeSession{
		WorkspaceURIs: []string{encodedURI},
	}
	got := resolveWorkspace(session, "")
	if got != specialDir {
		t.Errorf("resolveWorkspace(file://) = %q, want %q", got, specialDir)
	}

	// 2. Fallback to defaultWS
	sessionEmpty := &mockremoteagent.CascadeSession{}
	fallback := resolveWorkspace(sessionEmpty, tmpDir)
	if fallback != tmpDir {
		t.Errorf("resolveWorkspace(defaultWS) = %q, want %q", fallback, tmpDir)
	}

	// 3. Invalid URI fallback
	sessionInvalid := &mockremoteagent.CascadeSession{
		WorkspaceURIs: []string{"file:///non/existent/path/9999"},
	}
	fallbackInvalid := resolveWorkspace(sessionInvalid, tmpDir)
	if fallbackInvalid != tmpDir {
		t.Errorf("resolveWorkspace(invalidURI) = %q, want %q", fallbackInvalid, tmpDir)
	}
}

func TestExtractHistory(t *testing.T) {
	session := &mockremoteagent.CascadeSession{}

	// Single step (just current user input) -> no earlier history
	session.AddStep("USER_INPUT", "Hello", "", nil)
	hist := extractHistory(session)
	if len(hist) != 0 {
		t.Errorf("expected 0 history turns for single input, got %d", len(hist))
	}

	// Previous turn complete
	session.AddStep("PLANNER_RESPONSE", "Hi! How can I help?", "", nil)
	session.AddStep("USER_INPUT", "What is 2+2?", "", nil)

	hist2 := extractHistory(session)
	if len(hist2) != 2 {
		t.Fatalf("expected 2 history turns, got %d", len(hist2))
	}
	if hist2[0].Role != "user" || hist2[0].Text != "Hello" {
		t.Errorf("hist[0] mismatch: %+v", hist2[0])
	}
	if hist2[1].Role != "assistant" || hist2[1].Text != "Hi! How can I help?" {
		t.Errorf("hist[1] mismatch: %+v", hist2[1])
	}
}

func TestAgentTurnHandler_MockBackend(t *testing.T) {
	// The test seam replaces the real CloudCode transport with the mock.
	oldOverride := agentBackendOverride
	agentBackendOverride = func(string) backend.Client {
		return backend.NewMock()
	}
	defer func() {
		agentBackendOverride = oldOverride
	}()

	tmpDir := t.TempDir()
	session := mockremoteagent.NewCascadeSession("test-cascade-id", "test-project", "test-model", []string{tmpDir}, nil)

	handler := NewPigasusAgentTurnHandler(AgentHandlerConfig{
		Model:     "gemini-3.8-flash-high",
		Workspace: tmpDir,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	session.AddStep("USER_INPUT", "echo test", "", nil)
	handler(ctx, session, "echo test")

	// Wait for turn to finish (status transitions from RUNNING -> IDLE)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		steps := session.StepsSnapshot()
		if len(steps) >= 2 && session.Status == "IDLE" {
			lastStep := steps[len(steps)-1]
			if lastStep.Type == "PLANNER_RESPONSE" && lastStep.Status == "CORTEX_STEP_STATUS_DONE" {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("turn did not complete in time, status: %s, steps: %+v", session.Status, session.StepsSnapshot())
}

// TestResolveAgentModel pins the Pigasus-side model setting: a reference
// id/label resolves to the CloudCode wire id, OPENAGY_MODEL is the fallback,
// and an unknown name is rejected instead of being sent to a mock.
func TestResolveAgentModel(t *testing.T) {
	cases := []struct {
		in        string
		want      string
		reference bool
		ok        bool
	}{
		{"gemini-3.8-flash-high", "gemini-3.8-flash-high", true, true},
		{"Gemini 3.8 Flash (High)", "gemini-3.8-flash-high", true, true},
		{"definitely-not-a-model", "definitely-not-a-model", false, false},
	}
	for _, tc := range cases {
		got, reference, ok := resolveAgentModel(tc.in)
		if got != tc.want || reference != tc.reference || ok != tc.ok {
			t.Errorf("resolveAgentModel(%q) = (%q, %v, %v), want (%q, %v, %v)",
				tc.in, got, reference, ok, tc.want, tc.reference, tc.ok)
		}
	}

	t.Setenv("OPENAGY_MODEL", "Gemini 3.8 Flash (High)")
	if got, _, ok := resolveAgentModel(""); !ok || got != "gemini-3.8-flash-high" {
		t.Errorf("OPENAGY_MODEL fallback = (%q, %v)", got, ok)
	}
}

// TestTokenSourcePrefersTheClientToken pins the credential priority: the
// bearer the remote client sent wins, and an unauthenticated turn reads the
// unified credential rather than returning an empty string.
func TestTokenSourcePrefersTheClientToken(t *testing.T) {
	session := &mockremoteagent.CascadeSession{AuthToken: "client-token"}
	got, err := tokenSource(session)(context.Background())
	if err != nil || got != "client-token" {
		t.Fatalf("tokenSource(client) = (%q, %v)", got, err)
	}

	t.Setenv("JETSKI_OAUTH_TOKEN", "env-token")
	got, err = tokenSource(&mockremoteagent.CascadeSession{})(context.Background())
	if err != nil || got != "env-token" {
		t.Fatalf("tokenSource(unified) = (%q, %v), want env-token", got, err)
	}
}
