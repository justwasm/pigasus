package slashactions

import (
	"testing"

	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/features/auth"
)

func TestPlannerResponses(t *testing.T) {
	msgs := []backend.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "response 1"},
		{Role: "assistant", Content: "   "},
		{Role: "user", Content: "followup"},
		{Role: "assistant", Content: "response 2"},
	}
	got := PlannerResponses(msgs)
	if len(got) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(got))
	}
	if got[0] != "response 1" || got[1] != "response 2" {
		t.Errorf("unexpected responses: %+v", got)
	}
}

func TestResolveCopy(t *testing.T) {
	responses := []string{"first", "second", "third"}

	// Empty / default copy
	text, msg := ResolveCopy("", responses)
	if text != "third" || msg != "Copied last response to clipboard." {
		t.Errorf("default copy = (%q, %q)", text, msg)
	}

	// 1-based index from most recent
	text, msg = ResolveCopy("1", responses)
	if text != "third" || msg != "Copied response 1 to clipboard." {
		t.Errorf("copy 1 = (%q, %q)", text, msg)
	}

	text, msg = ResolveCopy("2", responses)
	if text != "second" || msg != "Copied response 2 to clipboard." {
		t.Errorf("copy 2 = (%q, %q)", text, msg)
	}

	text, msg = ResolveCopy("3", responses)
	if text != "first" || msg != "Copied response 3 to clipboard." {
		t.Errorf("copy 3 = (%q, %q)", text, msg)
	}

	// Empty list
	text, msg = ResolveCopy("", nil)
	if text != "" || msg != "No response to copy." {
		t.Errorf("empty copy = (%q, %q)", text, msg)
	}

	// Out of bounds
	text, msg = ResolveCopy("4", responses)
	if text != "" || msg != "No response to copy." {
		t.Errorf("oob copy = (%q, %q)", text, msg)
	}

	// Non-numeric fallback to last
	text, msg = ResolveCopy("foo", responses)
	if text != "third" || msg != "Copied last response to clipboard." {
		t.Errorf("non-numeric copy = (%q, %q)", text, msg)
	}
}

func TestConversationTitle(t *testing.T) {
	session := []backend.Message{
		{Role: "user", Content: "  My First  User  Message  \n"},
		{Role: "assistant", Content: "hi"},
	}

	// Configured name wins
	if got := ConversationTitle("  Custom Title  ", session, "fallback"); got != "Custom Title" {
		t.Errorf("got %q, want 'Custom Title'", got)
	}

	// Fallback to first user message
	if got := ConversationTitle("", session, "fallback"); got != "My First User Message" {
		t.Errorf("got %q, want 'My First User Message'", got)
	}

	// Fallback to executable / fallback
	if got := ConversationTitle("", nil, "/path/to/mybin"); got != "mybin" {
		t.Errorf("got %q, want 'mybin'", got)
	}
}

func TestMCPServersFeedback(t *testing.T) {
	if got := MCPServersFeedback(nil); got != "No MCP servers configured." {
		t.Errorf("nil config = %q", got)
	}
	cfg := &auth.MCPConfig{
		Servers: map[string]auth.MCPServer{
			"alpha": {
				Type:    "stdio",
				Enabled: true,
				Command: "node",
				Args:    []string{"server.js"},
			},
		},
	}
	got := MCPServersFeedback(cfg)
	if got == "" || got == "No MCP servers configured." {
		t.Errorf("expected server table, got %q", got)
	}
}
