package tui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/clirunner"
)

// TestSetupCLIParser_SubcommandsRegistered asserts that the full parser built by
// SetupCLIParser has server and tui registered.
func TestSetupCLIParser_SubcommandsRegistered(t *testing.T) {
	p := clirunner.SetupCLIParser("pigasus")
	specs := p.SubcommandSpecs()

	foundServer := false
	foundTUI := false
	for _, s := range specs {
		if s.Name == "server" {
			foundServer = true
		}
		if s.Name == "tui" {
			foundTUI = true
		}
	}
	if !foundServer {
		t.Errorf("server subcommand not found in SetupCLIParser specs")
	}
	if !foundTUI {
		t.Errorf("tui subcommand not found in SetupCLIParser specs")
	}
}

// TestSetupCLIParser_ServerHelp verifies that `pigasus server --help` outputs the server usage.
func TestSetupCLIParser_ServerHelp(t *testing.T) {
	p := clirunner.SetupCLIParser("pigasus")
	var out, errOut bytes.Buffer
	p.SetOut(&out)
	p.SetErr(&errOut)

	err := p.ExecuteArgs([]string{"server", "--help"})
	if err != nil {
		t.Fatalf("execute server --help error: %v", err)
	}
	if p.ExitCode() != 0 {
		t.Errorf("exit code = %d, want 0", p.ExitCode())
	}
	stderr := errOut.String()
	for _, flag := range []string{"--addr", "--port", "--host", "--workspace", "--model", "--token"} {
		if !strings.Contains(stderr, flag) {
			t.Errorf("server --help missing %s in output:\n%s", flag, stderr)
		}
	}
}

// TestSetupCLIParser_ServerDispatch verifies that `pigasus server` properly parses arguments and reaches the bridge handler.
func TestSetupCLIParser_ServerDispatch(t *testing.T) {
	p := clirunner.SetupCLIParser("pigasus")
	var captured cli.ServerArgs
	cli.RegisterServerHandler(func(j *cli.JetskiCLI, args cli.ServerArgs) error {
		captured = args
		return nil
	})
	t.Cleanup(func() { cli.RegisterServerHandler(runServer) })
	var out, errOut bytes.Buffer
	p.SetOut(&out)
	p.SetErr(&errOut)

	args := []string{
		"server",
		"--addr", "127.0.0.1:9090",
		"--port", "9090",
		"--host", "0.0.0.0",
		"--workspace", "/tmp/ws",
		"--model", "gemini-test",
		"--effort", "high",
		"--mode", "acceptEdits",
		"--dangerously-skip-permissions",
		"--token", "secret123",
		"--agent", "coder-agent",
		"--backend", "gateway",
		"--add-dir", "/tmp/extra",
	}

	if err := p.ExecuteArgs(args); err != nil {
		t.Fatalf("p.ExecuteArgs failed: %v (stderr=%q, stdout=%q)", err, errOut.String(), out.String())
	}
	if p.ExitCode() != 0 {
		t.Errorf("exit code = %d, want 0", p.ExitCode())
	}

	if captured.Addr != "127.0.0.1:9090" {
		t.Errorf("captured.Addr = %q, want %q", captured.Addr, "127.0.0.1:9090")
	}
	if captured.Port != 9090 {
		t.Errorf("captured.Port = %d, want 9090", captured.Port)
	}
	if captured.Host != "0.0.0.0" {
		t.Errorf("captured.Host = %q, want %q", captured.Host, "0.0.0.0")
	}
	if captured.Workspace != "/tmp/ws" {
		t.Errorf("captured.Workspace = %q, want %q", captured.Workspace, "/tmp/ws")
	}
	if captured.Model != "gemini-test" {
		t.Errorf("captured.Model = %q, want %q", captured.Model, "gemini-test")
	}
	if captured.Effort != "high" {
		t.Errorf("captured.Effort = %q, want %q", captured.Effort, "high")
	}
	if captured.Mode != "acceptEdits" {
		t.Errorf("captured.Mode = %q, want %q", captured.Mode, "acceptEdits")
	}
	if !captured.SkipPermissions {
		t.Error("captured.SkipPermissions = false, want true")
	}
	if captured.Token != "secret123" {
		t.Errorf("captured.Token = %q, want %q", captured.Token, "secret123")
	}
	if captured.Agent != "coder-agent" {
		t.Errorf("captured.Agent = %q, want %q", captured.Agent, "coder-agent")
	}
	if captured.Backend != "gateway" {
		t.Errorf("captured.Backend = %q, want %q", captured.Backend, "gateway")
	}
	if len(captured.AddDirs) != 1 || captured.AddDirs[0] != "/tmp/extra" {
		t.Errorf("captured.AddDirs = %v, want [/tmp/extra]", captured.AddDirs)
	}
}

// TestSetupCLIParser_TUIHelp verifies that `pigasus tui --help` outputs the tui usage.
func TestSetupCLIParser_TUIHelp(t *testing.T) {
	p := clirunner.SetupCLIParser("pigasus")
	var out, errOut bytes.Buffer
	p.SetOut(&out)
	p.SetErr(&errOut)

	err := p.ExecuteArgs([]string{"tui", "--help"})
	if err != nil {
		t.Fatalf("execute tui --help error: %v", err)
	}
	if p.ExitCode() != 0 {
		t.Errorf("exit code = %d, want 0", p.ExitCode())
	}
	stderr := errOut.String()
	for _, flag := range []string{"--server", "--token"} {
		if !strings.Contains(stderr, flag) {
			t.Errorf("tui --help missing %s in output:\n%s", flag, stderr)
		}
	}
}

// TestSetupCLIParser_TUIDispatch verifies that `pigasus tui --server` parses server and token,
// and preserves the session's flags.
func TestSetupCLIParser_TUIDispatch(t *testing.T) {
	p := clirunner.SetupCLIParser("pigasus")
	var captured cli.TUIArgs
	cli.RegisterTUIHandler(func(j *cli.JetskiCLI, args cli.TUIArgs) error {
		captured = args
		return nil
	})
	t.Cleanup(func() { cli.RegisterTUIHandler(runTUIHandler) })
	var out, errOut bytes.Buffer
	p.SetOut(&out)
	p.SetErr(&errOut)

	args := []string{
		"tui",
		"--server", "ws://127.0.0.1:7777",
		"--token", "mytoken",
		"--model", "gemini-pro",
		"--effort=high",
	}

	if err := p.ExecuteArgs(args); err != nil {
		t.Fatalf("p.ExecuteArgs failed: %v (stderr=%q, stdout=%q)", err, errOut.String(), out.String())
	}
	if p.ExitCode() != 0 {
		t.Errorf("exit code = %d, want 0", p.ExitCode())
	}

	if captured.Server != "ws://127.0.0.1:7777" {
		t.Errorf("captured.Server = %q, want %q", captured.Server, "ws://127.0.0.1:7777")
	}
	if captured.Token != "mytoken" {
		t.Errorf("captured.Token = %q, want %q", captured.Token, "mytoken")
	}
	if captured.Protocol != "acp" {
		t.Errorf("captured.Protocol = %q, want default %q", captured.Protocol, "acp")
	}
	wantRest := []string{"--model", "gemini-pro", "--effort=high"}
	if len(captured.Args) != len(wantRest) {
		t.Fatalf("captured.Args = %v, want %v", captured.Args, wantRest)
	}
	for i := range wantRest {
		if captured.Args[i] != wantRest[i] {
			t.Errorf("captured.Args[%d] = %q, want %q", i, captured.Args[i], wantRest[i])
		}
	}
}

// TestRunEntrypoint_PassesArgs verifies that cliapp.Run correctly passes arguments
// to the parser and does not ignore them in favor of os.Args.
func TestRunEntrypoint_PassesArgs(t *testing.T) {
	// Calling Run with ["pigasus", "server", "--help"] should succeed (exit 0)
	code := Run([]string{"pigasus", "server", "--help"})
	if code != 0 {
		t.Errorf("Run with server --help returned %d, want 0", code)
	}

	// Calling Run with ["pigasus", "tui", "--help"] should succeed (exit 0)
	code = Run([]string{"pigasus", "tui", "--help"})
	if code != 0 {
		t.Errorf("Run with tui --help returned %d, want 0", code)
	}
}
