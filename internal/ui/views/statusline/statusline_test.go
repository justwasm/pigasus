package statusline_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/justwasm/pigasus/internal/statusline"
)

func TestPricingForModel(t *testing.T) {
	if got := statusline.PricingForModel("claude-opus-4.7", 1_000_000, 0); got != 15.0 {
		t.Errorf("claude-opus input = %v, want 15.0", got)
	}
	if got := statusline.PricingForModel("claude-sonnet-4.5", 1_000_000, 0); got != 3.0 {
		t.Errorf("claude-sonnet input = %v, want 3.0", got)
	}
	if got := statusline.PricingForModel("MiniMax M3", 1_000_000, 1_000_000); got != 0 {
		t.Errorf("minimax = %v, want 0", got)
	}
	if got := statusline.PricingForModel("unknown-model", 1000, 1000); got != 0 {
		t.Errorf("unknown model = %v, want 0", got)
	}
}

func TestSplitCommand(t *testing.T) {
	cmd, args := statusline.SplitCommand("/bin/echo hello world")
	if cmd != "/bin/echo" || len(args) != 2 || args[0] != "hello" || args[1] != "world" {
		t.Errorf("unexpected split: %s, %v", cmd, args)
	}
	emptyCmd, emptyArgs := statusline.SplitCommand("   ")
	if emptyCmd != "" || len(emptyArgs) != 0 {
		t.Errorf("unexpected empty split: %s, %v", emptyCmd, emptyArgs)
	}
}

func TestState(t *testing.T) {
	var s statusline.State
	if s.MarkTick() != 1 {
		t.Errorf("expected tick count 1")
	}
	if s.SnapshotTick() != 1 {
		t.Errorf("expected snapshot tick 1")
	}
	s.ResetTick()
	if s.SnapshotTick() != 0 {
		t.Errorf("expected snapshot tick 0 after reset")
	}
	s.SetRendered("hello", nil)
	out, err := s.Current()
	if out != "hello" || err != nil {
		t.Errorf("got %q, %v; want %q, nil", out, err, "hello")
	}
}

func TestRunScript(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "echo.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nread -r line\necho \"got: $line\"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	res := statusline.RunScript(script, []byte("test-payload\n"))
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	if res.Output != "got: test-payload" {
		t.Errorf("got output %q, want %q", res.Output, "got: test-payload")
	}
}
