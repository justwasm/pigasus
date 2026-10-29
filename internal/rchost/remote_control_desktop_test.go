// Tests for the `remote-control` family's `--desktop` switch.
//
// The switch is the remote-control twin of `pigasus web --desktop`:
// the daemon's Mendel read carries the hub identity instead of the
// CLI's, and the backend answers the hub's flag set — the one with
// the voice input, the jetbox terminal and the aux pane redesign on.
//
// 【一手】 2026-10-27: the same token, the same body, the same
// endpoint, only the User-Agent differing — `antigravity/hub/…` gets
// enable-audio-transcription: true, `antigravity/cli/…` gets false.
// See internal/auth's package comment.
package rchost

import (
	"context"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/remotecontrol"
)

// TestServeDispatcher_DefaultIdentityIsTheCLIs: without the
// switch the daemon's Mendel read carries the CLI's own identity —
// the clone's page is the CLI's page.
func TestServeDispatcher_DefaultIdentityIsTheCLIs(t *testing.T) {
	sess := remotecontrol.NewSessionState(cli.ReferenceVersion)
	disp, err := buildServeDispatcher(t.TempDir(), "127.0.0.1:0", sess, "inst", desktopMendelUserAgent(false))
	if err != nil {
		t.Fatalf("buildServeDispatcher: %v", err)
	}
	if got := disp.MendelUserAgent(); got != "" {
		t.Errorf("MendelUserAgent = %q, want the CLI's own identity (empty)", got)
	}
}

// TestServeDispatcher_DesktopIdentity: with the switch the
// dispatcher's Mendel read carries the hub identity.
func TestServeDispatcher_DesktopIdentity(t *testing.T) {
	sess := remotecontrol.NewSessionState(cli.ReferenceVersion)
	disp, err := buildServeDispatcher(t.TempDir(), "127.0.0.1:0", sess, "inst", desktopMendelUserAgent(true))
	if err != nil {
		t.Fatalf("buildServeDispatcher: %v", err)
	}
	got := disp.MendelUserAgent()
	if !strings.HasPrefix(got, "antigravity/hub/") {
		t.Errorf("MendelUserAgent = %q, want the hub identity", got)
	}
	if got == auth.MendelUserAgentCLI {
		t.Errorf("MendelUserAgent = %q, want the hub identity, not the CLI's", got)
	}
}

// TestForegroundDispatcher_DesktopIdentity: the persistent daemon's
// twin — the LaunchAgent plist carries `--desktop`, the foreground
// handler reads it, and the dispatcher answers with the hub's
// identity.
func TestForegroundDispatcher_DesktopIdentity(t *testing.T) {
	sess := remotecontrol.NewSessionState(cli.ReferenceVersion)
	disp, err := buildForegroundDispatcher(t.TempDir(), "", "0.0.0.0:8787", sess, true)
	if err != nil {
		t.Fatalf("buildForegroundDispatcher: %v", err)
	}
	got := disp.MendelUserAgent()
	if !strings.HasPrefix(got, "antigravity/hub/") {
		t.Errorf("MendelUserAgent = %q, want the hub identity", got)
	}
}

// TestForegroundDispatcher_DefaultIdentityIsTheCLIs: without the
// switch the persistent daemon answers with the CLI's own identity.
func TestForegroundDispatcher_DefaultIdentityIsTheCLIs(t *testing.T) {
	sess := remotecontrol.NewSessionState(cli.ReferenceVersion)
	disp, err := buildForegroundDispatcher(t.TempDir(), "", "0.0.0.0:8787", sess, false)
	if err != nil {
		t.Fatalf("buildForegroundDispatcher: %v", err)
	}
	if got := disp.MendelUserAgent(); got != "" {
		t.Errorf("MendelUserAgent = %q, want the CLI's own identity (empty)", got)
	}
}

// TestServeStack_DesktopIdentity pins the last link of the chain:
// the switch the foreground surfaces pass in — `remote-control serve
// --desktop`, `remote-control run --desktop`, and `remote-control
// start --desktop` on a platform without launchd — reaches the
// dispatcher's Mendel read as the hub's identity.
//
// The other tests pin the pieces; this one pins the seam between the
// flag and the identity, which is the one every foreground surface
// shares.
func TestServeStack_DesktopIdentity(t *testing.T) {
	for _, desktop := range []bool{false, true} {
		disp, srv, err := buildServeStack("127.0.0.1:0", "inst", "token", desktop)
		if err != nil {
			t.Fatalf("buildServeStack(desktop=%v): %v", desktop, err)
		}
		t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })
		got := disp.MendelUserAgent()
		if desktop {
			if !strings.HasPrefix(got, "antigravity/hub/") {
				t.Errorf("MendelUserAgent = %q, want the hub identity", got)
			}
			continue
		}
		if got != "" {
			t.Errorf("MendelUserAgent = %q, want the CLI's own identity (empty)", got)
		}
	}
}

// TestParseForegroundFlags_Desktop: the hidden foreground
// subcommand reads the switch the LaunchAgent plist hands it.
func TestParseForegroundFlags_Desktop(t *testing.T) {
	_, _, _, _, desktop, err := parseForegroundFlags([]string{
		"--addr", "0.0.0.0:8787",
		"--name", "fgtest",
		"--desktop",
	})
	if err != nil {
		t.Fatalf("parseForegroundFlags: %v", err)
	}
	if !desktop {
		t.Errorf("--desktop did not reach the parser")
	}

	_, _, _, _, desktop, err = parseForegroundFlags([]string{
		"--addr", "0.0.0.0:8787",
		"--name", "fgtest",
	})
	if err != nil {
		t.Fatalf("parseForegroundFlags (no switch): %v", err)
	}
	if desktop {
		t.Errorf("desktop = true without --desktop")
	}
}
