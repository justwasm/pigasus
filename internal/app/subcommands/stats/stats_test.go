package stats

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSpec(t *testing.T) {
	spec := Spec()
	if spec.Name != "stats" {
		t.Errorf("spec.Name = %q, want 'stats'", spec.Name)
	}
	if spec.Run == nil {
		t.Errorf("spec.Run is nil")
	}
	if spec.HelpFn == nil {
		t.Errorf("spec.HelpFn is nil")
	}
}

func TestUsage(t *testing.T) {
	u := Usage("pigasus")
	for _, flag := range []string{"--all", "-c, --cwd", "-D, --data-dir", "--crawl-dir", "-h, --help"} {
		if !strings.Contains(u, flag) {
			t.Errorf("usage missing flag %s", flag)
		}
	}
}

func TestRun_Help(t *testing.T) {
	var buf bytes.Buffer
	err := Run(&buf, []string{"--help"})
	if err != nil {
		t.Fatalf("Run(--help) returned error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Usage of pigasus stats") {
		t.Errorf("expected usage output, got: %s", out)
	}
}

func TestRun_NoSessions(t *testing.T) {
	tmpDir := t.TempDir()
	var buf bytes.Buffer
	err := Run(&buf, []string{"--data-dir", tmpDir, "--all"})
	if err == nil {
		t.Fatalf("expected error for empty store, got nil")
	}
	if !strings.Contains(err.Error(), "no data available: no sessions found in database") {
		t.Errorf("unexpected error string: %v", err)
	}
	out := buf.String()
	expectedOut := "ERROR\nNo data available: no sessions found in database.\n"
	if out != expectedOut {
		t.Errorf("output = %q, want %q", out, expectedOut)
	}
}

func TestRun_WithSessions(t *testing.T) {
	tmpDir := t.TempDir()
	brainDir := filepath.Join(tmpDir, "brain", "test-session-123", ".system_generated", "logs")
	if err := os.MkdirAll(brainDir, 0o755); err != nil {
		t.Fatalf("failed to create brainDir: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	transcript := strings.Join([]string{
		`{"step_index":0,"source":"USER","type":"USER_MESSAGE","created_at":"` + now + `","content":"test user message"}`,
		`{"step_index":1,"source":"MODEL","type":"MODEL_RESPONSE","created_at":"` + now + `","model":"gemini-2.0-flash","input_tokens":500,"output_tokens":100,"cache_read_tokens":0,"tool_calls":[{"name":"run_command"}]}`,
	}, "\n")

	if err := os.WriteFile(filepath.Join(brainDir, "transcript.jsonl"), []byte(transcript), 0o644); err != nil {
		t.Fatalf("failed to write transcript: %v", err)
	}

	t.Setenv("OPENAGY_NO_BROWSER", "1")

	var buf bytes.Buffer
	err := Run(&buf, []string{"--data-dir", tmpDir, "--all"})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	out := buf.String()
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "Stats generated: ") {
			htmlPath := strings.TrimSpace(strings.TrimPrefix(line, "Stats generated: "))
			if htmlPath != "" {
				defer os.Remove(htmlPath)
			}
		}
	}
	if !strings.Contains(out, "Stats generated:") {
		t.Errorf("expected 'Stats generated:' in output, got: %s", out)
	}
	if !strings.Contains(out, "Total Sessions: 1") {
		t.Errorf("expected 'Total Sessions: 1' in output, got: %s", out)
	}
	if !strings.Contains(out, "Total Messages: 2") {
		t.Errorf("expected 'Total Messages: 2' in output, got: %s", out)
	}
	if !strings.Contains(out, "Total Tokens: 600") {
		t.Errorf("expected 'Total Tokens: 600' in output, got: %s", out)
	}
}

func TestRun_NoBrowserFlagSkipsLauncher(t *testing.T) {
	tmpDir := t.TempDir()
	brainDir := filepath.Join(tmpDir, "brain", "test-session-456", ".system_generated", "logs")
	if err := os.MkdirAll(brainDir, 0o755); err != nil {
		t.Fatalf("failed to create brainDir: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	transcript := strings.Join([]string{
		`{"step_index":0,"source":"USER","type":"USER_MESSAGE","created_at":"` + now + `","content":"test"}`,
		`{"step_index":1,"source":"MODEL","type":"MODEL_RESPONSE","created_at":"` + now + `","model":"gemini-2.0-flash","input_tokens":10,"output_tokens":5}`,
	}, "\n")
	if err := os.WriteFile(filepath.Join(brainDir, "transcript.jsonl"), []byte(transcript), 0o644); err != nil {
		t.Fatalf("failed to write transcript: %v", err)
	}

	called := false
	orig := openBrowserFn
	openBrowserFn = func(string) { called = true }
	defer func() { openBrowserFn = orig }()

	var buf bytes.Buffer
	if err := Run(&buf, []string{"--data-dir", tmpDir, "--all", "--no-browser"}); err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.HasPrefix(line, "Stats generated: ") {
			htmlPath := strings.TrimSpace(strings.TrimPrefix(line, "Stats generated: "))
			if htmlPath != "" {
				defer os.Remove(htmlPath)
			}
		}
	}
	if called {
		t.Fatalf("expected --no-browser to skip openBrowserFn")
	}
}

func TestShouldSuppressBrowser(t *testing.T) {
	if !shouldSuppressBrowser() {
		t.Fatalf("expected shouldSuppressBrowser() to be true under go test")
	}
}
