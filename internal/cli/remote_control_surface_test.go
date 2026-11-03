// remote_control_surface_test.go — the remote-control surface, pinned
// against the client/server pair.
//
// Cycle 374 added two visible subcommands — `server` and `tui` — at
// the cliapp parser level, next to `web` and `proxy`. The remote-control
// surface is older, bigger and load-bearing: it is the Web UI's channel,
// the terminal it serves and the cloud tunnel the dashboard reaches.
//
// A new sibling subcommand can shadow an old one in three ways, and
// none of them fails loudly:
//
//   - the parser resolves the wrong subcommand and the old one's handler
//     is never reached;
//   - the old command's help block changes — and the remote-control
//     block is the **reference's**, byte for byte;
//   - the old command's *hidden* spellings (`serve`, `run`, `rename`)
//     stop parsing.
//
// These tests pin the whole surface against a parser that carries the
// new pair, so a future subcommand cannot quietly take one of them.
package cli

import (
	"bytes"
	"strings"
	"testing"
)

// cloneParser builds the parser the way internal/cliapp does: the
// reference's own flags and subcommands, plus the clone's visible
// extensions. The client/server pair is in the list on purpose — the
// point of these tests is what the parser does *with* it.
func cloneParser(t *testing.T) (*Parser, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	p := NewParser(
		StandardFlags(),
		append(StandardSubcommands(), WebSubcommand(), ServerSubcommand(), TUISubcommand()),
		"pigasus", "recovered", "recovered",
	)
	p.SetOut(out)
	p.SetErr(errOut)
	return p, out, errOut
}

// TestRemoteControlHelp_IsStillTheReferencesBlock pins the reference's
// own `remote-control --help`: start, status and stop, and nothing
// else.
//
// The clone's own `serve`, `run` and `rename` are hidden — the
// reference's block does not mention them, and it must not.
func TestRemoteControlHelp_IsStillTheReferencesBlock(t *testing.T) {
	_, _, errOut := cloneParser(t)
	j := &JetskiCLI{Name: "pigasus", Out: &bytes.Buffer{}, ErrOut: errOut}

	if err := runRCParent(j, nil); err != nil {
		t.Fatalf("remote-control: %v", err)
	}
	got := errOut.String()
	for _, want := range []string{"start", "status", "stop"} {
		if !strings.Contains(got, want) {
			t.Errorf("the remote-control usage block does not list %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"serve", "run", "rename"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("the remote-control usage block lists the hidden %q:\n%s", unwanted, got)
		}
	}
}

// TestRemoteControlForegroundSpellings_StillReachTheServeBridge pins
// that the two hidden foreground spellings still reach the serve
// bridge — the one implementation behind `serve`, `run` and the
// non-darwin `start`.
func TestRemoteControlForegroundSpellings_StillReachTheServeBridge(t *testing.T) {
	cases := []struct {
		name  string
		label string
	}{
		{"serve", "remote-control serve"},
		{"run", "remote-control run"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var captured ServeArgs
			RegisterServeHandler(func(j *JetskiCLI, args ServeArgs) error {
				captured = args
				return nil
			})
			t.Cleanup(func() { RegisterServeHandler(nil) })

			p, _, errOut := cloneParser(t)
			if err := p.ExecuteArgs([]string{"remote-control", c.name, "--addr", "0.0.0.0:9911", "--no-login"}); err != nil {
				t.Fatalf("remote-control %s: %v (stderr=%q)", c.name, err, errOut.String())
			}
			if captured.Addr != "0.0.0.0:9911" {
				t.Errorf("addr = %q, want %q", captured.Addr, "0.0.0.0:9911")
			}
			if !captured.NoLogin {
				t.Error("the handler did not get --no-login")
			}
			// The label names the spelling the user typed, so the
			// daemon's own progress lines say which one it was.
			if captured.Label != c.label {
				t.Errorf("label = %q, want %q", captured.Label, c.label)
			}
		})
	}
}

// TestRemoteControlSurfaces_DoNotCollideWithTheClientServerPair pins
// that the two new subcommands and the remote-control one each reach
// their own handler, in a parser that carries all three.
//
// The collision this guards against is a name that resolves to the
// wrong RunJ: `tui` and `server` are close enough to the reference's
// own names that a future edit could route one of them into the
// remote-control handler — or the other way round.
func TestRemoteControlSurfaces_DoNotCollideWithTheClientServerPair(t *testing.T) {
	// The remote-control side.
	var serveArgs ServeArgs
	RegisterServeHandler(func(j *JetskiCLI, args ServeArgs) error {
		serveArgs = args
		return nil
	})
	t.Cleanup(func() { RegisterServeHandler(nil) })

	// The client/server side.
	var serverArgs ServerArgs
	RegisterServerHandler(func(j *JetskiCLI, args ServerArgs) error {
		serverArgs = args
		return nil
	})
	t.Cleanup(func() { RegisterServerHandler(nil) })

	var tuiArgs TUIArgs
	RegisterTUIHandler(func(j *JetskiCLI, args TUIArgs) error {
		tuiArgs = args
		return nil
	})
	t.Cleanup(func() { RegisterTUIHandler(nil) })

	p, _, errOut := cloneParser(t)
	if err := p.ExecuteArgs([]string{"remote-control", "serve", "--addr", "0.0.0.0:9922", "--no-login"}); err != nil {
		t.Fatalf("remote-control serve: %v (stderr=%q)", err, errOut.String())
	}
	if serveArgs.Addr != "0.0.0.0:9922" {
		t.Errorf("the remote-control handler got addr %q, want %q", serveArgs.Addr, "0.0.0.0:9922")
	}
	if serverArgs.Addr != "" || tuiArgs.Server != "" {
		t.Errorf("the remote-control call reached the client/server handlers: server=%+v tui=%+v", serverArgs, tuiArgs)
	}

	if err := p.ExecuteArgs([]string{"server", "--addr", "127.0.0.1:9933"}); err != nil {
		t.Fatalf("server: %v (stderr=%q)", err, errOut.String())
	}
	if serverArgs.Addr != "127.0.0.1:9933" {
		t.Errorf("the server handler got addr %q, want %q", serverArgs.Addr, "127.0.0.1:9933")
	}
	if serveArgs.Addr != "0.0.0.0:9922" {
		t.Errorf("the server call reached the remote-control handler: %+v", serveArgs)
	}

	if err := p.ExecuteArgs([]string{"tui", "--server", "ws://127.0.0.1:9944"}); err != nil {
		t.Fatalf("tui: %v (stderr=%q)", err, errOut.String())
	}
	if tuiArgs.Server != "ws://127.0.0.1:9944" {
		t.Errorf("the tui handler got server %q, want %q", tuiArgs.Server, "ws://127.0.0.1:9944")
	}
	if serveArgs.Addr != "0.0.0.0:9922" || serverArgs.Addr != "127.0.0.1:9933" {
		t.Errorf("the tui call reached another handler: serve=%+v server=%+v", serveArgs, serverArgs)
	}
}
