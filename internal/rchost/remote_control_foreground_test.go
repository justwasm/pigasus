// Tests for `wagyu remote-control-foreground` (cmd/wagyu/remote_control_foreground.go).
//
// The hidden subcommand is the long-running daemon process the
// launchd LaunchAgent / --session-detached path invokes.  Two
// surfaces are tested:
//
//   - argv validation (--addr, --name, --log-file rejection
//     paths; loopback refusal) — pure validation, no I/O.
//   - listener bring-up + graceful shutdown via SIGTERM — uses
//     `signal.Notify` in the test itself so SIGTERM does NOT
//     kill the test process when sent.
//
// The signal-disarm trick (step ② in TestForegroundHandler_BindsAndShutsDown)
// is the standard idiom for "send SIGTERM to test-process group"
// in Go's stdlib; `signal.Notify` registered on the channel
// suppresses the runtime's default-kill behavior for that
// signal, so SIGTERM is delivered to all Notify channels and
// the test process keeps running.
//
// Cycle rc-persistent-daemon pinned the signal-channel plumbing
// into a package-level var so the test rig can inject a
// deterministic channel that fires on demand.  Production
// always uses the var-default (`signalChFromNotify`).
package rchost

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/justwasm/pigasus/internal/cli"
)

// newJetskiCLIForTest returns a minimal JetskiCLI populated with
// in-memory stream buffers so the handler's stdout/stderr writes
// can be asserted without touching the real terminal.
func newJetskiCLIForTest(t *testing.T) (*cli.JetskiCLI, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var out, errb bytes.Buffer
	j := &cli.JetskiCLI{
		Name:   "wagyu",
		Out:    &out,
		ErrOut: &errb,
	}
	return j, &out, &errb
}

// TestForegroundHandler_MissingAddr: --addr is required.
func TestForegroundHandler_MissingAddr(t *testing.T) {
	j, _, _ := newJetskiCLIForTest(t)
	err := RunForeground(j, []string{"--name", "test"})
	if err == nil {
		t.Fatalf("RunForeground(name=test) = nil; want error")
	}
	if !strings.Contains(err.Error(), "--addr is required") {
		t.Errorf("err = %v; want --addr is required", err)
	}
}

// TestForegroundHandler_MissingName: --name is required.
func TestForegroundHandler_MissingName(t *testing.T) {
	j, _, _ := newJetskiCLIForTest(t)
	err := RunForeground(j, []string{"--addr", "0.0.0.0:8787"})
	if err == nil {
		t.Fatalf("RunForeground(addr=0.0.0.0:8787) = nil; want error")
	}
	if !strings.Contains(err.Error(), "--name is required") {
		t.Errorf("err = %v; want --name is required", err)
	}
}

// TestForegroundHandler_RejectsLoopbackAddr: the persistentrc
// layer refuses loopback binds (the LAN-reachable daemon must
// not be loopback-only).
func TestForegroundHandler_RejectsLoopbackAddr(t *testing.T) {
	j, _, _ := newJetskiCLIForTest(t)
	err := RunForeground(j, []string{
		"--addr", "127.0.0.1:8787",
		"--name", "test",
	})
	if err == nil {
		t.Fatalf("RunForeground with loopback addr returned nil; want error")
	}
	if !strings.Contains(err.Error(), "loopback refused") {
		t.Errorf("err = %v; want loopback refused", err)
	}
}

// TestForegroundHandler_RejectsInvalidName: --name must match
// the conservative [A-Za-z0-9._-]+ character set.
func TestForegroundHandler_RejectsInvalidName(t *testing.T) {
	j, _, _ := newJetskiCLIForTest(t)
	err := RunForeground(j, []string{
		"--addr", "0.0.0.0:8787",
		"--name", "evil name with spaces",
	})
	if err == nil {
		t.Fatalf("RunForeground with invalid name returned nil; want error")
	}
	if !strings.Contains(err.Error(), "invalid --name") {
		t.Errorf("err = %v; want invalid --name", err)
	}
}

// TestForegroundHandler_RedirectsLogFile verifies --log-file
// opens the file in append mode and that subsequent stderr
// writes land in it.  Uses a temp file.
func TestForegroundHandler_RedirectsLogFile(t *testing.T) {
	tmp := t.TempDir()
	lf := filepath.Join(tmp, "foreground.log")
	// Save the test runner's original stdout/stderr before
	// redirectToLogFile dup2's the log over fds 1/2. The earlier
	// version of this test closed fds 1/2 in its defer instead of
	// restoring them, which broke the test binary's output pipe for
	// every test that ran afterwards ("signal: broken pipe").
	origOut, err := unix.Dup(int(os.Stdout.Fd()))
	if err != nil {
		t.Fatalf("dup stdout: %v", err)
	}
	origErr, err := unix.Dup(int(os.Stderr.Fd()))
	if err != nil {
		_ = syscall.Close(origOut)
		t.Fatalf("dup stderr: %v", err)
	}
	defer func() {
		_ = unix.Dup2(origOut, int(os.Stdout.Fd()))
		_ = unix.Dup2(origErr, int(os.Stderr.Fd()))
		_ = syscall.Close(origOut)
		_ = syscall.Close(origErr)
	}()
	if err := redirectToLogFile(lf); err != nil {
		t.Fatalf("redirectToLogFile: %v", err)
	}
	// Now stdout (fd 1) and stderr (fd 2) point at lf.

	// After redirectToLogFile, a fmt.Fprintln(os.Stderr, …)
	// writes into lf.  Use a direct Write syscall to confirm.
	msg := []byte("test redirect " + t.Name() + "\n")
	if _, err := syscall.Write(int(os.Stderr.Fd()), msg); err != nil {
		t.Fatalf("syscall.Write to stderr: %v", err)
	}
	// The file is buffer-flushed by the OS at close; calling
	// f.Sync via os.Stat-followed-by-file-content check.
	data, err := os.ReadFile(lf)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", lf, err)
	}
	if !strings.Contains(string(data), string(msg)) {
		t.Errorf("log file %s contents = %q; want substring %q", lf, data, msg)
	}
}

// TestForegroundHandler_BindsAndShutsDown is the integration
// test: spin the foreground handler up against an ephemeral
// port, dial the listener, send SIGTERM via the test-supplied
// signal channel (no real signal delivery), and assert the
// handler returns cleanly within 5 s of the shutdown trigger.
//
// Implementation notes:
//
//   - We override the package-level signalChProvider var to
//     return our own channel instead of calling signal.Notify.
//   - The test goroutine closes (sends to) that channel after
//     asserting the listener is up.
//   - Test reads srv.HTTPURL() before close so the bind is
//     observable.
//
// The package-level override is restored on test exit via
// t.Cleanup so subsequent tests see the production default.
func TestForegroundHandler_BindsAndShutsDown(t *testing.T) {
	// Pick an ephemeral port via a probe Dial so we don't
	// collide with anything on the host.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("probe listen: %v", err)
	}
	addr := probe.Addr().String()
	if err := probe.Close(); err != nil {
		t.Fatalf("probe close: %v", err)
	}
	// Persistent daemon refuses loopback binds.  Flip to
	// 0.0.0.0:<port> so the listener is reachable for the
	// probing dial below (which is loopback only — but the
	// bind address itself is non-loopback).
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split host/port: %v", err)
	}
	if host != "127.0.0.1" {
		t.Fatalf("probe host = %q, want 127.0.0.1", host)
	}
	bindAddr := "0.0.0.0:" + port

	// Stub the signal channel so we don't need to send a
	// real SIGTERM to the test process.
	orig := signalChProvider
	t.Cleanup(func() { signalChProvider = orig })
	sigCh := make(chan os.Signal, 1)
	signalChProvider = func(c chan<- os.Signal) {
		// Forward the test signal onto c; the bridge
		// selects <-c in the foreground handler, so a single
		// Send into sigCh is enough.
		go func() {
			s := <-sigCh
			c <- s
		}()
	}

	// Build a flag.FlagSet to satisfy the rare code path
	// that reads a flag — currently the foreground handler
	// does NOT read any parent flags (it builds its own
	// FlagSet), so this is a placeholder.
	fs := flag.NewFlagSet("wagyu-foreground-test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	j, _, _ := newJetskiCLIForTest(t)
	_ = fs

	// Run the handler in a goroutine.  We use a child context
	// so the dial timeout doesn't leak; the handler doesn't
	// actually take a context but we hold our own.
	done := make(chan error, 1)
	go func() {
		done <- RunForeground(j, []string{
			"--addr", bindAddr,
			"--name", "fgtest",
		})
	}()

	// Give the handler ~1.5 s to bind, then dial to
	// confirm.  The TCP listener may take ~100 ms on a
	// cold start; we allow generous headroom.
	deadline := time.Now().Add(3 * time.Second)
	var dialed bool
	for time.Now().Before(deadline) && !dialed {
		c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = c.Close()
			dialed = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !dialed {
		// Trigger shutdown so the goroutine exits even if
		// the test is going to fail.
		close(sigCh)
		<-done
		t.Fatalf("foreground handler did not bind %s within 3 s", bindAddr)
	}

	// Trigger graceful shutdown.
	close(sigCh)
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			// Some Shutdown implementations surface
			// context.Canceled; that is acceptable.
			t.Errorf("foreground handler returned %v on shutdown", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatalf("foreground handler did not return within 8 s of SIGTERM")
	}
}

// The `signalChProvider` package-level var is declared in
// remote_control_foreground.go (production source) with the
// default `signalChFromNotify` setter.  This test file
// overrides it via t.Cleanup so we don't need to send real
// SIGTERM / SIGINT to the test process.
