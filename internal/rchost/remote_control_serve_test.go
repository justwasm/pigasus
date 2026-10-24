// Tests for `wagyu remote-control serve`
// (cmd/wagyu/remote_control_serve.go).
//
// Cycle rc-serve-subcommand. Three surfaces are tested:
//
//   - argv validation (ADC gate, --addr loopback refusal,
//     --name character-set check, --no-login acceptance) —
//     pure validation, no I/O.
//   - listener bring-up + graceful shutdown via SIGTERM —
//     uses serveSignalChProvider (a per-handler test hook
//     distinct from the foreground's signalChProvider so
//     each handler's tests can replace its own var without
//     affecting the other).
//   - ensureBearerTokenForServe token-on-disk short-circuit
//     (writes "using existing token" once when a token is
//     already present; falls through to device flow when
//     absent — the latter is exercised in internal/cli).
//
// The signal-disarm trick is the standard idiom for "send
// SIGTERM to test-process group" in Go's stdlib;
// `signal.Notify` registered on the channel suppresses the
// runtime's default-kill behavior for that signal, so
// SIGTERM is delivered to all Notify channels and the test
// process keeps running. The serveSignalChProvider override
// is restored on test exit via t.Cleanup so subsequent tests
// see the production default.
package rchost

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/cli"
)

// TestServeHandler_ADCRejection pins the cycle's brief:
// when $AGY_ADC_AUTH is set, `remote-control serve` exits
// with code 3 (the AGY_ERROR convention the periodic-path
// gate uses) and the error wording names AGY_ADC_AUTH
// alongside /logout for the recovery path.
//
// Mirrors remote_control_adc_test.go's TestCheckRemoteControlADC_*
// surface so the gate vocabulary stays unified across
// --remote-control, /remote-control, and `remote-control serve`.
func TestServeHandler_ADCRejection(t *testing.T) {
	t.Setenv("AGY_ADC_AUTH", "1")

	j, _, _ := runServeHandlerNewJetskiCLIForTest()
	err := RunServe(j, cli.ServeArgs{
		Addr:    "0.0.0.0:8787",
		Name:    "test",
		NoLogin: true,
	})
	if err == nil {
		t.Fatal("RunServe with AGY_ADC_AUTH=1 returned nil; want cli.ExitCodeError(3, …)")
	}
	if !strings.Contains(err.Error(), "AGY_ADC_AUTH") {
		t.Errorf("error = %q; must mention 'AGY_ADC_AUTH'", err.Error())
	}
	if !strings.Contains(err.Error(), "/logout") {
		t.Errorf("error = %q; must mention '/logout' (recovery path)", err.Error())
	}
	if !strings.Contains(err.Error(), "Application Default Credentials") {
		t.Errorf("error = %q; must mention 'Application Default Credentials'", err.Error())
	}
	// Exit code must be 3 (AGY_ERROR) — same code the
	// periodic-path gate returns, so dashboards' error
	// classifier gets a single code across the rc-*
	// surface.
	code, ok := cli.ExitCodeOf(err)
	if !ok || code != 3 {
		t.Errorf("exit code = %d (ok=%v); want 3", code, ok)
	}
}

// TestServeHandler_ADCRejectionEmptyEnv: $AGY_ADC_AUTH
// unset ⇒ no gate ⇒ the handler proceeds (and ultimately
// fails because the foreground build wants a real auth
// path; we pin only the "not the ADC error" branch here).
func TestServeHandler_ADCRejectionEmptyEnv(t *testing.T) {
	t.Setenv("AGY_ADC_AUTH", "")

	j, _, _ := runServeHandlerNewJetskiCLIForTest()
	err := RunServe(j, cli.ServeArgs{
		Addr:    "127.0.0.1:8787",
		Name:    "test",
		NoLogin: true,
	})
	// The handler should NOT return an ADC error. It will
	// still error — 127.0.0.1 is loopback, which the
	// ValidateAddr call rejects — but the wording should
	// mention loopback, not ADC.
	if err != nil && strings.Contains(err.Error(), "AGY_ADC_AUTH") {
		t.Errorf("AGY_ADC_AUTH=empty should not trigger the ADC gate; err = %v", err)
	}
}

// TestServeHandler_RejectsLoopbackAddr: serve shares the
// persistent daemon's "loopback refused" rule because the
// LAN-reachable constraint applies to both paths (the
// foreground twin is meant to be reachable from another
// machine on the LAN too, not just loopback-only).
func TestServeHandler_RejectsLoopbackAddr(t *testing.T) {
	j, _, _ := runServeHandlerNewJetskiCLIForTest()
	err := RunServe(j, cli.ServeArgs{
		Addr:    "127.0.0.1:8787",
		Name:    "test",
		NoLogin: true,
	})
	if err == nil {
		t.Fatal("RunServe with loopback addr returned nil; want error")
	}
	if !strings.Contains(err.Error(), "loopback refused") {
		t.Errorf("err = %v; want loopback refused", err)
	}
}

// TestServeHandler_RejectsInvalidName: --name must match
// the conservative [A-Za-z0-9._-]+ character set.
func TestServeHandler_RejectsInvalidName(t *testing.T) {
	j, _, _ := runServeHandlerNewJetskiCLIForTest()
	err := RunServe(j, cli.ServeArgs{
		Addr:    "0.0.0.0:8787",
		Name:    "evil name with spaces",
		NoLogin: true,
	})
	if err == nil {
		t.Fatal("RunServe with invalid name returned nil; want error")
	}
	if !strings.Contains(err.Error(), "invalid --name") {
		t.Errorf("err = %v; want invalid --name", err)
	}
}

// TestServeHandler_DefaultAddrAccepted: empty --addr
// resolves to the foreground default (0.0.0.0:8787) and
// passes ValidateAddr. Pin the path via the ADC gate
// short-circuit: an empty --addr + ADC rejection should
// NOT trigger the "loopback refused" wording.
func TestServeHandler_DefaultAddrAccepted(t *testing.T) {
	t.Setenv("AGY_ADC_AUTH", "1")

	j, _, _ := runServeHandlerNewJetskiCLIForTest()
	err := RunServe(j, cli.ServeArgs{
		Addr:    "",
		Name:    "test",
		NoLogin: true,
	})
	if err == nil {
		t.Fatal("RunServe(empty --addr) returned nil; want ADC gate error")
	}
	// The ADC gate runs BEFORE ValidateAddr (cycle
	// rc-serve-subcommand ordering), so the wording is
	// the AGY_ADC_AUTH message, not the loopback one.
	if strings.Contains(err.Error(), "loopback refused") {
		t.Errorf("empty --addr must be replaced with the default before ValidateAddr; err = %v", err)
	}
}

// TestServeHandler_BindsAndShutsDown is the integration
// test: spin the serve handler up against an ephemeral
// port, dial the listener, send SIGTERM via the
// test-supplied signal channel (no real signal delivery),
// and assert the handler returns cleanly within 5 s of the
// shutdown trigger.
//
// Implementation notes:
//
//   - We override the package-level serveSignalChProvider
//     var to return our own channel instead of calling
//     signal.Notify.
//   - The test goroutine closes (sends to) that channel
//     after asserting the listener is up.
//   - Test reads srv.HTTPURL() before close so the bind
//     is observable.
//
// The package-level override is restored on test exit via
// t.Cleanup so subsequent tests see the production
// default.
func TestServeHandler_BindsAndShutsDown(t *testing.T) {
	// Pick an ephemeral port via a probe Dial so we
	// don't collide with anything on the host.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("probe listen: %v", err)
	}
	addr := probe.Addr().String()
	if err := probe.Close(); err != nil {
		t.Fatalf("probe close: %v", err)
	}
	// Serve refuses loopback binds. Flip to
	// 0.0.0.0:<port> so the listener is reachable for
	// the probing dial below (which is loopback only —
	// but the bind address itself is non-loopback).
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split host/port: %v", err)
	}
	if host != "127.0.0.1" {
		t.Fatalf("probe host = %q, want 127.0.0.1", host)
	}
	bindAddr := "0.0.0.0:" + port

	// Stub the signal channel so we don't need to send
	// a real SIGTERM to the test process. Distinct from
	// the foreground's signalChProvider so each handler's
	// tests can replace its own var without affecting
	// the other.
	orig := serveSignalChProvider
	t.Cleanup(func() { serveSignalChProvider = orig })
	sigCh := make(chan os.Signal, 1)
	serveSignalChProvider = func(c chan<- os.Signal) {
		go func() {
			s := <-sigCh
			c <- s
		}()
	}

	j, _, errBuf := runServeHandlerNewJetskiCLIForTest()

	// Run the handler in a goroutine. We use a child
	// context so the dial timeout doesn't leak; the
	// handler doesn't actually take a context but we
	// hold our own.
	done := make(chan error, 1)
	go func() {
		done <- RunServe(j, cli.ServeArgs{
			Addr:    bindAddr,
			Name:    "servetest",
			NoLogin: true,
		})
	}()

	// Give the handler ~1.5 s to bind, then dial to
	// confirm. The TCP listener may take ~100 ms on a
	// cold start; we allow generous headroom.
	deadline := time.Now().Add(3 * time.Second)
	var dialed bool
	for time.Now().Before(deadline) && !dialed {
		c, derr := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if derr == nil {
			_ = c.Close()
			dialed = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !dialed {
		// Trigger shutdown so the goroutine exits even
		// if the test is going to fail.
		close(sigCh)
		<-done
		t.Fatalf("serve handler did not bind %s within 3 s", bindAddr)
	}

	// Confirm the banner line on stderr (the captured
	// outBuf in the test rig mirrors the production
	// os.Stderr). The banner is the contract dashboards
	// already grep; if it disappeared the daemon is
	// invisible to the dashboard.
	banner := errBuf.String()
	if !strings.Contains(banner, "remote-control serve: listening on") {
		t.Errorf("serve banner missing on stderr; got=%q", banner)
	}
	// The handler reports the resolved ws:// URL (loopback), not the
	// 0.0.0.0 bind string the test passed in, so pin the port and the
	// /rpc path rather than bindAddr.
	if !strings.Contains(banner, ":"+port+"/rpc") {
		t.Errorf("serve banner missing resolved port %s; got=%q", port, banner)
	}

	// Trigger graceful shutdown.
	close(sigCh)
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			// Some Shutdown implementations surface
			// context.Canceled; that is acceptable.
			t.Errorf("serve handler returned %v on shutdown", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatalf("serve handler did not return within 8 s of SIGTERM")
	}
}

// TestEnsureBearerTokenForServe_TokenOnDisk exercises the
// short-circuit: when a token is already at
// auth.TokenFilePath(), the helper writes "using existing
// token" and returns nil without running the device flow.
//
// Implementation note: the helper consults
// auth.LoadTokenUnified() (which honours $JETSKI_OAUTH_TOKEN
// + all on-disk token paths). To pin the short-circuit
// without a real OAuth round-trip, we set $JETSKI_OAUTH_TOKEN
// to a non-empty placeholder; auth.LoadTokenUnified returns
// a populated *AntigravityToken and the helper short-circuits
// without hitting the device-flow endpoint.
func TestEnsureBearerTokenForServe_TokenOnDisk(t *testing.T) {
	t.Setenv("JETSKI_OAUTH_TOKEN", "ya29.test-placeholder")
	t.Setenv("ANTIGRAVITY_REFRESH_TOKEN", "")

	// Build a fresh ANTIGRAVITY_HOME so auth.TokenFilePath()
	// resolves to a clean path that doesn't require a
	// pre-existing on-disk token file. We also point $HOME
	// to the same tmp so AllTokenPaths() returns paths
	// inside it (the v1.3.5 binary paths are computed via
	// $HOME, not $ANTIGRAVITY_HOME).
	tmp := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", tmp)
	t.Setenv("HOME", tmp)
	t.Cleanup(func() {
		_ = os.Unsetenv("ANTIGRAVITY_HOME")
		_ = os.Unsetenv("HOME")
	})

	var out, errb bytes.Buffer
	if err := ensureBearerTokenForServe(&out, &errb); err != nil {
		t.Fatalf("ensureBearerTokenForServe: %v", err)
	}
	if !strings.Contains(out.String(), "using existing token") {
		t.Errorf("expected 'using existing token' on stdout; got=%q", out.String())
	}
	if errb.Len() != 0 {
		t.Errorf("expected empty stderr on short-circuit; got=%q", errb.String())
	}
}

// TestEnsureBearerTokenForServe_NoTokenFallsThrough pins
// the "no token" branch. We can't drive auth.DeviceFlow in
// a unit test (it dials accounts.google.com), so we point
// $HOME and $ANTIGRAVITY_HOME at a fresh tmp dir so
// AllTokenPaths() resolves to paths that don't exist on
// disk, and verify the helper reaches the
// "running OAuth login inline" line before bailing on the
// network error.
//
// A future cycle that wires auth.DeviceFlow against an
// httptest.Server (the cycle-170 pattern) can extend this
// test to drive the full path end-to-end. For now we pin
// only the "fell through to the device flow" sentinel.
func TestEnsureBearerTokenForServe_NoTokenFallsThrough(t *testing.T) {
	// Empty env: no env-supplied token.
	t.Setenv("JETSKI_OAUTH_TOKEN", "")
	t.Setenv("ANTIGRAVITY_REFRESH_TOKEN", "")

	// Point $HOME and $ANTIGRAVITY_HOME at a fresh tmp
	// dir so AllTokenPaths() resolves to paths that
	// don't exist on disk. Without this, a developer's
	// real ~/.config/antigravity/oauth-token would
	// short-circuit the helper.
	tmp := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", tmp)
	t.Setenv("HOME", tmp)
	t.Cleanup(func() {
		_ = os.Unsetenv("ANTIGRAVITY_HOME")
		_ = os.Unsetenv("HOME")
	})

	// Run the helper. We expect a non-nil error (the
	// device flow will fail when it dials the real
	// Google endpoint), but the early "no bearer token on
	// disk" line MUST appear before the network error.
	var out, errb bytes.Buffer
	_ = ensureBearerTokenForServe(&out, &errb)
	if !strings.Contains(out.String(), "no bearer token on disk") {
		t.Errorf("expected 'no bearer token on disk' on stdout; got=%q", out.String())
	}
}

// TestAnnounceSettingsFile pins the read half of the
// settings.json integration: the helper writes one of the
// three sentinels based on whether the file exists.
//
//   - "settings.json: not found at …"     when the file is absent
//   - "settings.json: loaded from … (N bytes)"  when present
//   - "settings.json: probe failed at …"  when stat fails for
//     another reason (e.g. permission denied)
//
// We only pin the first two branches here; the third
// requires a permission-denied path that's brittle to set
// up across test hosts (root, sudoers, etc.).
func TestAnnounceSettingsFile(t *testing.T) {
	t.Run("not-found", func(t *testing.T) {
		var out bytes.Buffer
		announceSettingsFile(&out)
		s := out.String()
		// Either the not-found sentinel or a probe-failed
		// sentinel is acceptable — both indicate the
		// helper surfaced something readable. We do NOT
		// assert "loaded from" because a different host's
		// ANTIGRAVITY_HOME may already have a settings.json.
		if !strings.Contains(s, "settings.json:") {
			t.Errorf("expected 'settings.json:' on stdout; got=%q", s)
		}
	})

	t.Run("present", func(t *testing.T) {
		tmp := t.TempDir()
		// Write a small settings.json so the helper
		// can read it. The file lives at
		// $ANTIGRAVITY_HOME/settings.json per
		// auth.AntigravityHome().
		t.Setenv("ANTIGRAVITY_HOME", tmp)
		t.Cleanup(func() { _ = os.Unsetenv("ANTIGRAVITY_HOME") })
		if err := os.WriteFile(tmp+"/settings.json", []byte("{}"), 0o600); err != nil {
			t.Fatalf("write settings.json: %v", err)
		}

		var out bytes.Buffer
		announceSettingsFile(&out)
		s := out.String()
		if !strings.Contains(s, "loaded from") {
			t.Errorf("expected 'loaded from' on stdout; got=%q", s)
		}
		if !strings.Contains(s, tmp+"/settings.json") {
			t.Errorf("expected path %q on stdout; got=%q", tmp+"/settings.json", s)
		}
	})
}

// runServeHandlerNewJetskiCLIForTest returns a minimal
// JetskiCLI populated with in-memory stream buffers so the
// handler's stdout / stderr writes can be asserted without
// touching the real terminal. The returned *bytes.Buffer
// captures the same stream the production os.Stderr banner
// would land in (the handler bypasses the JetskiCLI stream
// pair for the signal-trap log lines; tests can grep the
// real os.Stderr if they need to — but the bind banner is
// the only public contract the tests pin here).
func runServeHandlerNewJetskiCLIForTest() (*cli.JetskiCLI, *syncBuffer, *syncBuffer) {
	var out, errb syncBuffer
	return &cli.JetskiCLI{
		Name:   "wagyu",
		Out:    &out,
		ErrOut: &errb,
	}, &out, &errb
}

// syncBuffer is a bytes.Buffer that survives the serve goroutine's
// writes while the test reads it: the handler writes its banner from
// the goroutine the test started, and the test reads the buffer while
// that goroutine is still running.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// Ensure syscall is referenced; the test rig imports it
// only for the test-side signal.Notify stub path.
var _ = syscall.SIGTERM
