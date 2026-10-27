package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestWebSubcommand_Help(t *testing.T) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	j := &JetskiCLI{
		Name:   "pigasus",
		Out:    out,
		ErrOut: errOut,
	}

	err := runWeb(j, []string{"--help"})
	if err != nil {
		t.Fatalf("runWeb --help failed: %v", err)
	}

	helpText := errOut.String()
	if !strings.Contains(helpText, "Usage: pigasus web [flags]") {
		t.Errorf("expected usage header, got:\n%s", helpText)
	}
	if !strings.Contains(helpText, "--addr") || !strings.Contains(helpText, "--port") {
		t.Errorf("expected --addr and --port in help, got:\n%s", helpText)
	}
}

func TestWebSubcommand_FlagsParsing(t *testing.T) {
	var captured WebArgs
	RegisterWebHandler(func(j *JetskiCLI, args WebArgs) error {
		captured = args
		return nil
	})
	defer RegisterWebHandler(nil)

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	j := &JetskiCLI{
		Name:   "pigasus",
		Out:    out,
		ErrOut: errOut,
	}

	err := runWeb(j, []string{
		"--port=8080",
		"--host", "192.168.1.100",
		"--workspace", "/tmp/my-proj",
		"--open",
	})
	if err != nil {
		t.Fatalf("runWeb failed: %v", err)
	}

	if captured.Port != 8080 {
		t.Errorf("expected Port=8080, got %d", captured.Port)
	}
	if captured.Host != "192.168.1.100" {
		t.Errorf("expected Host=192.168.1.100, got %s", captured.Host)
	}
	if captured.Workspace != "/tmp/my-proj" {
		t.Errorf("expected Workspace=/tmp/my-proj, got %s", captured.Workspace)
	}
	if !captured.Open {
		t.Errorf("expected Open=true")
	}
}

// TestWebSubcommand_DesktopFlag pins the --desktop switch.
//
// The flag asks for the reference's *desktop* UI: the page's Mendel
// read then carries the hub identity, and the backend serves the flag
// set that carries the desktop features. 【一手】 measured
// 2026-10-27 — see internal/auth's package comment.
func TestWebSubcommand_DesktopFlag(t *testing.T) {
	var captured WebArgs
	RegisterWebHandler(func(j *JetskiCLI, args WebArgs) error {
		captured = args
		return nil
	})
	defer RegisterWebHandler(nil)

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	j := &JetskiCLI{
		Name:   "pigasus",
		Out:    out,
		ErrOut: errOut,
	}

	err := runWeb(j, []string{"--desktop"})
	if err != nil {
		t.Fatalf("runWeb failed: %v", err)
	}
	if !captured.Desktop {
		t.Errorf("expected Desktop=true")
	}

	err = runWeb(j, []string{})
	if err != nil {
		t.Fatalf("runWeb failed: %v", err)
	}
	if captured.Desktop {
		t.Errorf("expected Desktop=false without the flag")
	}
}

func TestWebSubcommand_InvalidPort(t *testing.T) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	j := &JetskiCLI{
		Name:   "pigasus",
		Out:    out,
		ErrOut: errOut,
	}

	err := runWeb(j, []string{"--port", "999999"})
	if err == nil {
		t.Fatalf("expected error on invalid port, got nil")
	}
	if !strings.Contains(errOut.String(), "invalid port") {
		t.Errorf("expected 'invalid port' error, got:\n%s", errOut.String())
	}
}
