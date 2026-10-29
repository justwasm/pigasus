// Tests for `remote-control run` — the clone's platform-independent
// foreground command.
//
// `remote-control start` is the platform-dependent one: it registers a
// launchd agent on macOS and falls back to the foreground everywhere
// else. `remote-control run` is the same foreground daemon under a name
// that says "here, in this terminal", and it behaves identically on
// every platform.
//
// The subcommand is hidden: the reference's `remote-control --help`
// lists start, status and stop only, and that block is the reference's,
// byte for byte.
package cli

import (
	"bytes"
	"strings"
	"testing"
)

// withDarwinPlatform makes the rcIsDarwin seam report macOS for the
// duration of the test.
func withDarwinPlatform(t *testing.T) {
	t.Helper()
	orig := rcIsDarwin
	rcIsDarwin = true
	t.Cleanup(func() { rcIsDarwin = orig })
}

// TestRemoteControlRun_ReachesTheBridge pins the whole path: the
// flags travel from the argv into the typed ServeArgs the handler body
// reads, and the daemon's own progress lines carry the name the user
// typed.
func TestRemoteControlRun_ReachesTheBridge(t *testing.T) {
	var captured ServeArgs
	RegisterServeHandler(func(j *JetskiCLI, args ServeArgs) error {
		captured = args
		return nil
	})
	t.Cleanup(func() { RegisterServeHandler(nil) })

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	j := &JetskiCLI{Name: "pigasus", Out: out, ErrOut: errOut}

	if err := runRCRun(j, []string{"--name", "test-machine", "--addr", "127.0.0.1:9999", "--desktop"}); err != nil {
		t.Fatalf("runRCRun failed: %v", err)
	}

	if captured.Name != "test-machine" {
		t.Errorf("the handler got name %q, want %q", captured.Name, "test-machine")
	}
	if captured.Addr != "127.0.0.1:9999" {
		t.Errorf("the handler got addr %q, want %q", captured.Addr, "127.0.0.1:9999")
	}
	if !captured.Desktop {
		t.Errorf("the handler did not get --desktop")
	}
	if captured.NoLogin {
		t.Errorf("the handler got --no-login without being asked")
	}
	// The label names the subcommand the user typed.
	if captured.Label != "remote-control run" {
		t.Errorf("the handler got label %q, want %q", captured.Label, "remote-control run")
	}
	// The sentinel on stdout is the one `remote-control stop` prints
	// too.
	if !strings.Contains(out.String(), "Remote-control daemon stopped.") {
		t.Errorf("stdout missing the stop sentinel: %q", out.String())
	}
}

// TestRemoteControlRun_IsPlatformIndependent pins the property the
// command exists for: `run` runs the daemon in the foreground on every
// platform — including macOS, where `start` would have registered a
// launchd agent instead.
//
// The launchd branch is the platform-dependent one, and `run` must not
// take it.
func TestRemoteControlRun_IsPlatformIndependent(t *testing.T) {
	for _, darwin := range []bool{true, false} {
		darwin := darwin
		name := "linux"
		if darwin {
			name = "darwin"
		}
		t.Run(name, func(t *testing.T) {
			orig := rcIsDarwin
			rcIsDarwin = darwin
			t.Cleanup(func() { rcIsDarwin = orig })

			called := false
			RegisterServeHandler(func(j *JetskiCLI, args ServeArgs) error {
				called = true
				return nil
			})
			t.Cleanup(func() { RegisterServeHandler(nil) })

			out := &bytes.Buffer{}
			errOut := &bytes.Buffer{}
			j := &JetskiCLI{Name: "pigasus", Out: out, ErrOut: errOut}

			if err := runRCRun(j, []string{"--name", "test-machine"}); err != nil {
				t.Fatalf("runRCRun failed: %v", err)
			}
			if !called {
				t.Errorf("the foreground handler was not called on %s", name)
			}
			// No launchd note: `run` never registers an agent.
			if strings.Contains(errOut.String(), "no launchd") {
				t.Errorf("stderr mentions launchd on %s: %q", name, errOut.String())
			}
		})
	}
}

// TestRemoteControlRun_WithoutTheBridge pins the failure mode: a
// binary built without the serve bridge must say so instead of
// pretending to have started a daemon.
func TestRemoteControlRun_WithoutTheBridge(t *testing.T) {
	orig := serveHandler
	serveHandler = nil
	t.Cleanup(func() { serveHandler = orig })

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	j := &JetskiCLI{Name: "pigasus", Out: out, ErrOut: errOut}

	err := runRCRun(j, []string{"--name", "test-machine"})
	if err == nil {
		t.Fatal("runRCRun without the serve bridge returned nil, want an error")
	}
	if !strings.Contains(err.Error(), "no handler registered") {
		t.Errorf("error = %q, want it to mention the missing handler", err.Error())
	}
}

// TestRemoteControlRun_Help pins the usage block: the flags a user
// needs to point the daemon somewhere are all listed.
func TestRemoteControlRun_Help(t *testing.T) {
	out, errb, code := runJetski(t, "remote-control", "run", "--help")
	if code != 0 {
		t.Fatalf("run --help exit code = %d, want 0; stderr=%q", code, errb)
	}
	for _, flag := range []string{"--addr", "--name", "--no-login", "--desktop"} {
		if !strings.Contains(errb, flag) {
			t.Errorf("run --help usage missing %s; stderr=%q", flag, errb)
		}
	}
	if out != "" {
		t.Errorf("run --help wrote to stdout: %q", out)
	}
}

// TestRemoteControlRun_RejectsAnUnknownFlag pins that the parser
// rejects a flag it does not know, with the usage block.
func TestRemoteControlRun_RejectsAnUnknownFlag(t *testing.T) {
	_, errb, code := runJetski(t, "remote-control", "run", "--bogus")
	if code == 0 {
		t.Errorf("run --bogus exit code = 0, want a failure; stderr=%q", errb)
	}
	if !strings.Contains(errb, "bogus") {
		t.Errorf("run --bogus stderr does not name the flag: %q", errb)
	}
}

// TestRemoteControlRun_IsHidden pins that the subcommand is accepted
// but not advertised: the reference's `remote-control --help` block
// lists start, status and stop only, and it is the reference's, byte
// for byte.
//
// `serve` is hidden the same way — the reference has the subcommand
// but does not list it.
func TestRemoteControlRun_IsHidden(t *testing.T) {
	_, errb, code := runJetski(t, "remote-control", "--help")
	if code != 0 {
		t.Fatalf("remote-control --help exit code = %d, want 0; stderr=%q", code, errb)
	}
	if strings.Contains(errb, "run ") || strings.Contains(errb, "  run\n") {
		t.Errorf("the reference's usage block must not list `run`: %q", errb)
	}
	// The block itself is unchanged: three subcommands.
	for _, want := range []string{"start", "status", "stop"} {
		if !strings.Contains(errb, want) {
			t.Errorf("the usage block is missing %q: %q", want, errb)
		}
	}
}

// TestRemoteControlRun_MatchesServe pins that `run` and `serve` are
// the same operation: the same flags reach the handler, and only the
// label differs.
func TestRemoteControlRun_MatchesServe(t *testing.T) {
	var fromRun, fromServe ServeArgs
	RegisterServeHandler(func(j *JetskiCLI, args ServeArgs) error {
		fromRun = args
		return nil
	})
	t.Cleanup(func() { RegisterServeHandler(nil) })

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	j := &JetskiCLI{Name: "pigasus", Out: out, ErrOut: errOut}

	if err := runRCRun(j, []string{"--name", "test-machine", "--addr", "127.0.0.1:9999", "--no-login", "--desktop"}); err != nil {
		t.Fatalf("runRCRun failed: %v", err)
	}

	RegisterServeHandler(func(j *JetskiCLI, args ServeArgs) error {
		fromServe = args
		return nil
	})
	if err := runRCServe(j, []string{"--name", "test-machine", "--addr", "127.0.0.1:9999", "--no-login", "--desktop"}); err != nil {
		t.Fatalf("runRCServe failed: %v", err)
	}

	fromRun.Label = ""
	fromServe.Label = ""
	if fromRun != fromServe {
		t.Errorf("run and serve disagree: run=%+v serve=%+v", fromRun, fromServe)
	}
}
