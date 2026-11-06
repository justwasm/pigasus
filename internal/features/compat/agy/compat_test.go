package agy

import "testing"

func TestIsAgyBackend(t *testing.T) {
	agyBackends := []string{"", "local", "pigasus", "agy", "antigravity", "LOCAL", "Pigasus"}
	for _, b := range agyBackends {
		if !IsAgyBackend(b) {
			t.Errorf("IsAgyBackend(%q) = false, want true", b)
		}
	}

	nonAgyBackends := []string{"codex", "claude", "acp", "codex-acp", "claude-acp", "opencode", "grok"}
	for _, b := range nonAgyBackends {
		if IsAgyBackend(b) {
			t.Errorf("IsAgyBackend(%q) = true, want false", b)
		}
	}
}

func TestFormatUnknownCommand(t *testing.T) {
	if got := FormatUnknownCommand("test"); got != "Unknown command: /test" {
		t.Errorf("FormatUnknownCommand('test') = %q, want 'Unknown command: /test'", got)
	}
	if got := FormatUnknownCommand("/foo"); got != "Unknown command: /foo" {
		t.Errorf("FormatUnknownCommand('/foo') = %q, want 'Unknown command: /foo'", got)
	}
}

func TestUserRequestEnvelope(t *testing.T) {
	got := UserRequestEnvelope("hello world")
	want := "<USER_REQUEST>\nhello world\n</USER_REQUEST>"
	if got != want {
		t.Errorf("UserRequestEnvelope = %q, want %q", got, want)
	}
}
