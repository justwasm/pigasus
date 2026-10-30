package main_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestPiggaNativeHelp(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "--help")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run . --help failed: %v, output: %s", err, string(out))
	}
	s := string(out)
	if !strings.Contains(s, "Usage of pigga:") {
		t.Fatalf("expected usage header, got: %s", s)
	}
	if !strings.Contains(s, "Available subcommands:") {
		t.Fatalf("expected subcommands section, got: %s", s)
	}

	// Verify subcommands present in native pigga:
	expectedSubs := []string{"accounts", "agent", "agents", "changelog", "halloween", "models", "model", "provider", "update"}
	for _, sub := range expectedSubs {
		if !strings.Contains(s, sub) {
			t.Errorf("expected subcommand %q in help output", sub)
		}
	}

	// Verify OS-dependent / unsupported subcommands are NOT exposed:
	osSubs := []string{"remote-control", "mic-serve", "install"}
	for _, sub := range osSubs {
		if strings.Contains(s, "  "+sub+" ") {
			t.Errorf("unsupported subcommand %q should not be exposed in pigga --help", sub)
		}
	}
}

func TestPiggaNativeVersion(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "--version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run . --version failed: %v, output: %s", err, string(out))
	}
	s := string(out)
	if !strings.Contains(s, "1.4.11-wasm") {
		t.Fatalf("expected 1.4.11-wasm, got: %s", s)
	}
}

func TestPiggaNativePrintShim(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "--backend", "shim", "--print", "hello from pigga")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run . --backend shim --print failed: %v, output: %s", err, string(out))
	}
	s := string(out)
	if !strings.Contains(s, "mock reply") || !strings.Contains(s, "hello from pigga") {
		t.Fatalf("expected mock reply containing prompt, got: %s", s)
	}
}

func TestPiggaNativePrintJSON(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "--backend", "shim", "--output-format", "json", "--print", "json test")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run . --backend shim --output-format json --print failed: %v, output: %s", err, string(out))
	}
	var resp struct {
		Status   string `json:"status"`
		Response string `json:"response"`
		NumTurns int    `json:"num_turns"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("failed to parse JSON response: %v, raw output: %s", err, string(out))
	}
	if resp.Status != "SUCCESS" {
		t.Fatalf("expected status SUCCESS, got: %s", resp.Status)
	}
	if !strings.Contains(resp.Response, "json test") {
		t.Fatalf("expected response containing prompt, got: %s", resp.Response)
	}
}

func TestPiggaNativeTUIEntryPointNoArgs(t *testing.T) {
	cmd := exec.Command("go", "run", ".")
	cmd.Stdin = strings.NewReader("")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run . failed: %v, output: %s", err, string(out))
	}
	s := string(out)
	if !strings.Contains(s, "Pigasus CLI") || !strings.Contains(s, ">") {
		t.Fatalf("expected TUI session output, got: %s", s)
	}
}

func TestPiggaNativeTUIEntryPointContinueFlag(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "-c")
	cmd.Stdin = strings.NewReader("")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run . -c failed: %v, output: %s", err, string(out))
	}
	s := string(out)
	if !strings.Contains(s, "Pigasus CLI") || !strings.Contains(s, ">") {
		t.Fatalf("expected TUI session output, got: %s", s)
	}
}

func TestPiggaNativeTUIEntryPointInteractiveFlag(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "-i", "initial prompt")
	cmd.Stdin = strings.NewReader("")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run . -i failed: %v, output: %s", err, string(out))
	}
	s := string(out)
	if !strings.Contains(s, "Pigasus CLI") || !strings.Contains(s, ">") {
		t.Fatalf("expected TUI session output, got: %s", s)
	}
}

func TestPiggaNativeMutualExclusion(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "--print", "hello", "-i", "world")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected failure when both --print and -i are supplied, got success: %s", string(out))
	}
	if !strings.Contains(string(out), "mutually exclusive") {
		t.Fatalf("expected mutually exclusive error message, got: %s", string(out))
	}
}

func TestPiggaNativeUpdateCommand(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "update")
	cmd.Env = append(os.Environ(), "OPENAGY_BRAND=antigravity")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run . update failed: %v, output: %s", err, string(out))
	}
	s := string(out)
	if !strings.Contains(s, "Checking for updates...") {
		t.Fatalf("expected checking for updates, got: %s", s)
	}
	if !strings.Contains(s, "You are already on the latest version.") {
		t.Fatalf("expected already on latest version, got: %s", s)
	}
}

func TestPiggaNativeUpdateCommand_CustomVersion(t *testing.T) {
	cmd := exec.Command("go", "run", ".", "update")
	cmd.Env = append(os.Environ(), "OPENAGY_BRAND=antigravity", "PIGGA_CURRENT_VERSION=2.0.0-custom")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run . update failed: %v, output: %s", err, string(out))
	}
	s := string(out)
	if !strings.Contains(s, "Checking for updates... (current version 2.0.0-custom)") {
		t.Fatalf("expected custom version in checking message, got: %s", s)
	}
}



