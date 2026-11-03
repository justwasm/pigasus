// Tests for `pigasus server` and `pigasus tui --server` — the
// client/server pair.
//
// The two subcommands are the clone's own: the reference (`agy` 1.4.11)
// has no client/server mode of this shape, so there is nothing to be
// byte-identical with. What *is* pinned here is the contract between the
// parser and the implementation: the flags a user types reach the
// handler, the session's own flags survive the trip, and a flag the
// subcommand does not own is not silently swallowed.
package cli

import (
	"bytes"
	"strings"
	"testing"
)

// TestServerSubcommand_ReachesTheBridge pins the whole path: the flags
// travel from the argv into the typed ServerArgs the handler body reads.
func TestServerSubcommand_ReachesTheBridge(t *testing.T) {
	var captured ServerArgs
	RegisterServerHandler(func(j *JetskiCLI, args ServerArgs) error {
		captured = args
		return nil
	})
	t.Cleanup(func() { RegisterServerHandler(nil) })

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	j := &JetskiCLI{Name: "pigasus", Out: out, ErrOut: errOut}

	err := runServer(j, []string{
		"--addr", "127.0.0.1:9999",
		"--port", "8080",
		"--host", "0.0.0.0",
		"--workspace", "/tmp",
		"--model", "test-model",
		"--effort", "high",
		"--mode", "plan",
		"--dangerously-skip-permissions",
		"--token", "secret",
		"--agent", "custom-agent",
		"--backend", "gateway",
		"--add-dir", "/tmp/one",
		"--add-dir", "/tmp/two",
	})
	if err != nil {
		t.Fatalf("runServer failed: %v", err)
	}

	if captured.Addr != "127.0.0.1:9999" {
		t.Errorf("addr = %q, want %q", captured.Addr, "127.0.0.1:9999")
	}
	if captured.Port != 8080 {
		t.Errorf("port = %d, want 8080", captured.Port)
	}
	if captured.Host != "0.0.0.0" {
		t.Errorf("host = %q, want %q", captured.Host, "0.0.0.0")
	}
	if captured.Workspace != "/tmp" {
		t.Errorf("workspace = %q, want %q", captured.Workspace, "/tmp")
	}
	if captured.Model != "test-model" {
		t.Errorf("model = %q, want %q", captured.Model, "test-model")
	}
	if captured.Effort != "high" {
		t.Errorf("effort = %q, want %q", captured.Effort, "high")
	}
	if captured.Mode != "plan" {
		t.Errorf("mode = %q, want %q", captured.Mode, "plan")
	}
	if !captured.SkipPermissions {
		t.Error("the handler did not get --dangerously-skip-permissions")
	}
	if captured.Token != "secret" {
		t.Errorf("token = %q, want %q", captured.Token, "secret")
	}
	if captured.Agent != "custom-agent" {
		t.Errorf("agent = %q, want %q", captured.Agent, "custom-agent")
	}
	if captured.Backend != "gateway" {
		t.Errorf("backend = %q, want %q", captured.Backend, "gateway")
	}
	if len(captured.AddDirs) != 2 || captured.AddDirs[0] != "/tmp/one" || captured.AddDirs[1] != "/tmp/two" {
		t.Errorf("addDirs = %v, want the two directories in order", captured.AddDirs)
	}
}

// TestServerSubcommand_RejectsAnUnknownFlag pins that the parser
// rejects a flag it does not know, with the usage block.
func TestServerSubcommand_RejectsAnUnknownFlag(t *testing.T) {
	called := false
	RegisterServerHandler(func(j *JetskiCLI, args ServerArgs) error {
		called = true
		return nil
	})
	t.Cleanup(func() { RegisterServerHandler(nil) })

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	j := &JetskiCLI{Name: "pigasus", Out: out, ErrOut: errOut}

	if err := runServer(j, []string{"--bogus"}); err == nil {
		t.Fatal("server --bogus returned nil, want an error")
	}
	if called {
		t.Error("the handler ran despite the unknown flag")
	}
	if !strings.Contains(errOut.String(), "bogus") {
		t.Errorf("stderr does not name the flag: %q", errOut.String())
	}
}

// TestServerSubcommand_RejectsABadPort pins that the port is a number
// in range, and that a nonsense one is refused rather than bound.
func TestServerSubcommand_RejectsABadPort(t *testing.T) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	j := &JetskiCLI{Name: "pigasus", Out: out, ErrOut: errOut}

	for _, port := range []string{"0", "70000", "http"} {
		if err := runServer(j, []string{"--port", port}); err == nil {
			t.Errorf("server --port %s returned nil, want an error", port)
		}
	}
}

// TestServerSubcommand_Help pins the usage block: the flags a user
// needs to point the server somewhere are all listed.
func TestServerSubcommand_Help(t *testing.T) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	j := &JetskiCLI{Name: "pigasus", Out: out, ErrOut: errOut}

	for _, helpArg := range []string{"--help", "-help", "-h"} {
		errOut.Reset()
		if err := runServer(j, []string{helpArg}); err != nil {
			t.Fatalf("server %s failed: %v", helpArg, err)
		}
		for _, flag := range []string{"--addr", "--port", "--host", "--workspace", "--model", "--token", "--agent", "--backend", "--add-dir"} {
			if !strings.Contains(errOut.String(), flag) {
				t.Errorf("server %s usage missing %s; stderr=%q", helpArg, flag, errOut.String())
			}
		}
	}
	if out.String() != "" {
		t.Errorf("server --help wrote to stdout: %q", out.String())
	}
}

// TestTUISubcommand_ReachesTheBridge pins the split of ownership: the
// subcommand owns --server and --token, and everything else is the
// session's own and travels through untouched.
func TestTUISubcommand_ReachesTheBridge(t *testing.T) {
	var captured TUIArgs
	RegisterTUIHandler(func(j *JetskiCLI, args TUIArgs) error {
		captured = args
		return nil
	})
	t.Cleanup(func() { RegisterTUIHandler(nil) })

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	j := &JetskiCLI{Name: "pigasus", Out: out, ErrOut: errOut}

	err := runTUI(j, []string{
		"--server", "ws://127.0.0.1:7777",
		"--token", "secret",
		"--protocol", "acp",
		"--model", "test-model",
		"--effort=high",
	})
	if err != nil {
		t.Fatalf("runTUI failed: %v", err)
	}

	if captured.Server != "ws://127.0.0.1:7777" {
		t.Errorf("server = %q, want %q", captured.Server, "ws://127.0.0.1:7777")
	}
	if captured.Token != "secret" {
		t.Errorf("token = %q, want %q", captured.Token, "secret")
	}
	if captured.Protocol != "acp" {
		t.Errorf("protocol = %q, want %q", captured.Protocol, "acp")
	}
	// The session's own flags survive the trip, in order, untouched.
	want := []string{"--model", "test-model", "--effort=high"}
	if len(captured.Args) != len(want) {
		t.Fatalf("the session's args = %v, want %v", captured.Args, want)
	}
	for i := range want {
		if captured.Args[i] != want[i] {
			t.Errorf("the session's arg %d = %q, want %q", i, captured.Args[i], want[i])
		}
	}
}

// TestTUISubcommand_DefaultsToACP asserts that attaching to a server defaults to ACP.
func TestTUISubcommand_DefaultsToACP(t *testing.T) {
	var captured TUIArgs
	RegisterTUIHandler(func(j *JetskiCLI, args TUIArgs) error {
		captured = args
		return nil
	})
	t.Cleanup(func() { RegisterTUIHandler(nil) })

	j := &JetskiCLI{Name: "pigasus", Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}}
	err := runTUI(j, []string{
		"--server", "ws://127.0.0.1:7777",
	})
	if err != nil {
		t.Fatalf("runTUI failed: %v", err)
	}

	if captured.Protocol != "acp" {
		t.Errorf("default protocol = %q, want %q", captured.Protocol, "acp")
	}
	if captured.ACPVersion != "auto" {
		t.Errorf("default acp-version = %q, want %q", captured.ACPVersion, "auto")
	}

	// Test explicit version
	err = runTUI(j, []string{
		"--server", "ws://127.0.0.1:7777",
		"--acp-version", "2",
	})
	if err != nil {
		t.Fatalf("runTUI failed: %v", err)
	}
	if captured.ACPVersion != "2" {
		t.Errorf("acp-version = %q, want 2", captured.ACPVersion)
	}

	// Test --backend codex and --codex shortcut
	err = runTUI(j, []string{"--backend", "codex"})
	if err != nil {
		t.Fatalf("runTUI --backend codex failed: %v", err)
	}
	if captured.Backend != "codex" {
		t.Errorf("backend = %q, want codex", captured.Backend)
	}

	err = runTUI(j, []string{"--codex"})
	if err != nil {
		t.Fatalf("runTUI --codex failed: %v", err)
	}
	if captured.Backend != "codex" {
		t.Errorf("backend via --codex = %q, want codex", captured.Backend)
	}

	// Test --backend claude and --claude shortcut
	err = runTUI(j, []string{"--backend", "claude"})
	if err != nil {
		t.Fatalf("runTUI --backend claude failed: %v", err)
	}
	if captured.Backend != "claude" {
		t.Errorf("backend = %q, want claude", captured.Backend)
	}

	err = runTUI(j, []string{"--claude"})
	if err != nil {
		t.Fatalf("runTUI --claude failed: %v", err)
	}
	if captured.Backend != "claude" {
		t.Errorf("backend via --claude = %q, want claude", captured.Backend)
	}

	// Test --backend agy and --agy shortcut
	err = runTUI(j, []string{"--backend", "agy"})
	if err != nil {
		t.Fatalf("runTUI --backend agy failed: %v", err)
	}
	if captured.Backend != "agy" {
		t.Errorf("backend = %q, want agy", captured.Backend)
	}

	err = runTUI(j, []string{"--agy"})
	if err != nil {
		t.Fatalf("runTUI --agy failed: %v", err)
	}
	if captured.Backend != "agy" {
		t.Errorf("backend via --agy = %q, want agy", captured.Backend)
	}

	// Test ACP agent shortcuts: --codex-acp, --claude-acp, --agy-acp
	err = runTUI(j, []string{"--codex-acp"})
	if err != nil {
		t.Fatalf("runTUI --codex-acp failed: %v", err)
	}
	if captured.Backend != "codex-acp" {
		t.Errorf("backend via --codex-acp = %q, want codex-acp", captured.Backend)
	}

	err = runTUI(j, []string{"--claude-acp"})
	if err != nil {
		t.Fatalf("runTUI --claude-acp failed: %v", err)
	}
	if captured.Backend != "claude-acp" {
		t.Errorf("backend via --claude-acp = %q, want claude-acp", captured.Backend)
	}

	err = runTUI(j, []string{"--agy-acp"})
	if err != nil {
		t.Fatalf("runTUI --agy-acp failed: %v", err)
	}
	if captured.Backend != "agy-acp-server" {
		t.Errorf("backend via --agy-acp = %q, want agy-acp-server", captured.Backend)
	}

	// Test --acp-cmd and --acp-manifest
	err = runTUI(j, []string{"--acp-cmd", "npx -y @agentclientprotocol/codex-acp"})
	if err != nil {
		t.Fatalf("runTUI --acp-cmd failed: %v", err)
	}
	if captured.ACPCmd != "npx -y @agentclientprotocol/codex-acp" {
		t.Errorf("acp-cmd = %q, want npx command", captured.ACPCmd)
	}

	err = runTUI(j, []string{"--acp-manifest", "/tmp/agent.json"})
	if err != nil {
		t.Fatalf("runTUI --acp-manifest failed: %v", err)
	}
	if captured.ACPManifest != "/tmp/agent.json" {
		t.Errorf("acp-manifest = %q, want /tmp/agent.json", captured.ACPManifest)
	}
}

// TestTUISubcommand_Help pins the usage block.
func TestTUISubcommand_Help(t *testing.T) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	j := &JetskiCLI{Name: "pigasus", Out: out, ErrOut: errOut}

	for _, helpArg := range []string{"--help", "-help", "-h"} {
		errOut.Reset()
		if err := runTUI(j, []string{helpArg}); err != nil {
			t.Fatalf("tui %s failed: %v", helpArg, err)
		}
		for _, flag := range []string{"--server", "--token"} {
			if !strings.Contains(errOut.String(), flag) {
				t.Errorf("tui %s usage missing %s; stderr=%q", helpArg, flag, errOut.String())
			}
		}
	}
	if out.String() != "" {
		t.Errorf("tui --help wrote to stdout: %q", out.String())
	}
}

// TestNewFlagSet_MatchesTheRootFlags pins that the standalone flag set
// the `tui` subcommand parses with carries the root command's flags —
// the session's own flags are the root command's flags.
func TestNewFlagSet_MatchesTheRootFlags(t *testing.T) {
	fs := NewFlagSet("tui", append(StandardFlags(), ExtraFlags()...))
	for _, name := range []string{"model", "effort", "add-dir", "conversation"} {
		if fs.Lookup(name) == nil {
			t.Errorf("the standalone flag set has no --%s", name)
		}
	}
	// The set parses like the root command's.
	if err := fs.Parse([]string{"--model", "test-model"}); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := fs.Lookup("model").Value.String(); got != "test-model" {
		t.Errorf("model = %q, want %q", got, "test-model")
	}
}

// TestDispatchRunXxx_ServerAndTUI pins that dispatchRunXxx can dispatch
// server and tui subcommands via runXxxTable.
func TestDispatchRunXxx_ServerAndTUI(t *testing.T) {
	var capturedServer ServerArgs
	RegisterServerHandler(func(j *JetskiCLI, args ServerArgs) error {
		capturedServer = args
		return nil
	})
	t.Cleanup(func() { RegisterServerHandler(nil) })

	var capturedTUI TUIArgs
	RegisterTUIHandler(func(j *JetskiCLI, args TUIArgs) error {
		capturedTUI = args
		return nil
	})
	t.Cleanup(func() { RegisterTUIHandler(nil) })

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}

	if err := dispatchRunXxx("server", out, errOut, nil, nil, []string{"--addr", "127.0.0.1:8888"}); err != nil {
		t.Fatalf("dispatchRunXxx(server): %v", err)
	}
	if capturedServer.Addr != "127.0.0.1:8888" {
		t.Errorf("capturedServer.Addr = %q, want %q", capturedServer.Addr, "127.0.0.1:8888")
	}

	if err := dispatchRunXxx("tui", out, errOut, nil, nil, []string{"--server", "ws://127.0.0.1:9999", "--token", "abc"}); err != nil {
		t.Fatalf("dispatchRunXxx(tui): %v", err)
	}
	if capturedTUI.Server != "ws://127.0.0.1:9999" || capturedTUI.Token != "abc" {
		t.Errorf("capturedTUI = %+v, want server ws://127.0.0.1:9999 and token abc", capturedTUI)
	}
}

