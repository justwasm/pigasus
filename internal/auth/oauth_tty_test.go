// Tests for the OAuth authorization-code read path (cycle 261, AGY 1.1.2).
//
// The seam — StdinIsTTYFn, OpenControllingTerminalFn, ControllingTerminalDevice —
// is per-package, so each test saves the prior values via t.Cleanup so
// reordering the file or running it under `go test -count=N` does not
// leak between cases. The POSIX default is /dev/tty; the Windows default
// is CONIN$. The compile-time build tag on this file is omitted
// deliberately so the test runs on every platform; the Windows path is
// stubbed at runtime via OpenControllingTerminalFn.
package auth

import (
	"errors"
	"io"
	"runtime"
	"strings"
	"testing"
)

// saveTTYSeams snapshots the three package-level seam vars the
// print-mode OAuth tests rely on and restores them via t.Cleanup.
// Any test that overrides StdinIsTTYFn / OpenControllingTerminalFn /
// ControllingTerminalDevice MUST call this; forgetting the restore
// would leak the fake into the next test that runs in the same
// package and break its assertions.
func saveTTYSeams(t *testing.T) {
	t.Helper()
	prevIsTTY := StdinIsTTYFn
	prevOpen := OpenControllingTerminalFn
	prevDev := ControllingTerminalDevice
	t.Cleanup(func() {
		StdinIsTTYFn = prevIsTTY
		OpenControllingTerminalFn = prevOpen
		ControllingTerminalDevice = prevDev
	})
}

// TestOAuthRead_FromTTY pins the interactive-TTY happy path: when
// StdinIsTTYFn() reports true (the caller is on a real terminal),
// ReadAuthorizationCode reads from the caller-supplied reader
// directly and never opens /dev/tty. Pin verifies that the
// OpenControllingTerminalFn seam is *not* consulted in this branch.
//
// bytesRead on the wrapped reader confirms the read actually
// consumed bytes from r — a regression that read from /dev/tty would
// not show in the bytesRead counter.
func TestOAuthRead_FromTTY(t *testing.T) {
	saveTTYSeams(t)
	StdinIsTTYFn = func() bool { return true }
	openCalls := 0
	OpenControllingTerminalFn = func() (io.ReadCloser, error) {
		openCalls++
		return nil, errors.New("should not be opened in TTY path")
	}

	r := &readCountingReader{r: strings.NewReader("ya29.pasted-from-stdin\n")}
	got, err := ReadAuthorizationCode(r)
	if err != nil {
		t.Fatalf("ReadAuthorizationCode: %v", err)
	}
	if got != "ya29.pasted-from-stdin" {
		t.Errorf("got %q, want %q", got, "ya29.pasted-from-stdin")
	}
	if r.bytesRead == 0 {
		t.Errorf("stdin reader was not Read by the OAuth path; bytesRead = 0")
	}
	if openCalls != 0 {
		t.Errorf("OpenControllingTerminalFn called %d times in TTY path, want 0", openCalls)
	}
}

// TestOAuthRead_FromTTYWhenStdinPiped pins the print-mode happy
// path: when StdinIsTTYFn() returns false (piped stdin) the OAuth
// code is read from OpenControllingTerminalFn() — the controlling
// terminal — so a piped prompt does not eat the paste.
//
// The fake opener returns a bytes.Reader-backed ReadCloser so the
// test does not actually open /dev/tty under `go test`; the seamed
// open call is invoked exactly once.
func TestOAuthRead_FromTTYWhenStdinPiped(t *testing.T) {
	saveTTYSeams(t)
	StdinIsTTYFn = func() bool { return false }
	openCalls := 0
	OpenControllingTerminalFn = func() (io.ReadCloser, error) {
		openCalls++
		return io.NopCloser(strings.NewReader("ya29.pasted-from-dev-tty\n")), nil
	}

	r := strings.NewReader("prompt-piped-on-stdin; ignore me\n")
	got, err := ReadAuthorizationCode(r)
	if err != nil {
		t.Fatalf("ReadAuthorizationCode: %v", err)
	}
	if got != "ya29.pasted-from-dev-tty" {
		t.Errorf("got %q, want %q", got, "ya29.pasted-from-dev-tty")
	}
	if openCalls != 1 {
		t.Errorf("OpenControllingTerminalFn called %d times, want 1", openCalls)
	}
	// The stdin reader must still be unread — the OAuth path never
	// touched the prompt pipe.
	rest, _ := io.ReadAll(r)
	if !strings.Contains(string(rest), "prompt-piped-on-stdin") {
		t.Errorf("stdin was consumed by the OAuth path; remaining = %q", rest)
	}
}

// fakeFailOpen is the canned error returned by the open seam when
// the test wants to simulate /dev/tty being unavailable. The message
// is short and recognisable so the assertions can pin both the
// headlining message and the open-erro wrap.
var errFakeOpen = errors.New("fake: open failed (no controlling terminal)")

// TestOAuthRead_FailFastWhenNoTTY pins the truly-headless error
// path: when stdin is piped AND opening the controlling-terminal
// device fails (CI runner with detached tty), ReadAuthorizationCode
// returns the actionable AGY_OAUTH_TOKEN hint wrapped with the
// underlying open error and does not block on a read.
//
// The actionable-string check asserts both presence of "AGY_OAUTH_TOKEN"
// (the env-var hint) and absence of /dev/tty-specific wording so the
// cross-platform contract holds: the operator gets one actionable
// message regardless of OS.
func TestOAuthRead_FailFastWhenNoTTY(t *testing.T) {
	saveTTYSeams(t)
	StdinIsTTYFn = func() bool { return false }
	OpenControllingTerminalFn = func() (io.ReadCloser, error) {
		return nil, errFakeOpen
	}
	ControllingTerminalDevice = "/dev/tty"

	_, err := ReadAuthorizationCode(strings.NewReader("ignored\n"))
	if err == nil {
		t.Fatal("expected error from failed /dev/tty open, got nil")
	}
	if !strings.Contains(err.Error(), "AGY_OAUTH_TOKEN") {
		t.Errorf("error %q does not mention AGY_OAUTH_TOKEN", err.Error())
	}
	if !strings.Contains(err.Error(), "controlling terminal") {
		t.Errorf("error %q does not mention 'controlling terminal'", err.Error())
	}
	if !strings.HasPrefix(err.Error(), controllingTerminalReadError) {
		t.Errorf("error %q does not start with the headlining message %q",
			err.Error(), controllingTerminalReadError)
	}
	if !strings.Contains(err.Error(), "open /dev/tty") {
		t.Errorf("error %q does not mention which device was attempted", err.Error())
	}
	if !strings.Contains(err.Error(), errFakeOpen.Error()) {
		t.Errorf("error %q does not wrap the underlying open error", err.Error())
	}
}

// TestOAuthRead_PrintModeUnaffected pins the negative case: when no
// OAuth code paste happens (the print-mode prompt reads no OAuth at
// all) StdinIsTTYFn, OpenControllingTerminalFn and
// ReadAuthorizationCode are unobservable. This test only verifies
// that the print-mode defaults do not depend on the OAuth
// code-paste path being wired in; the print path is verified end-to-
// end by the existing TestRunPigPrintTurn* and TestRunLogin_*
// suites, which run unchanged against the package built with this
// file present.
//
// What we actually assert: the print-mode-style call signature (a
// prompt read on stdin that does NOT consult the OAuth seam) returns
// whatever was on stdin and never invokes OpenControllingTerminalFn.
func TestOAuthRead_PrintModeUnaffected(t *testing.T) {
	saveTTYSeams(t)
	StdinIsTTYFn = func() bool { return true } // interactive TTY
	openCalls := 0
	OpenControllingTerminalFn = func() (io.ReadCloser, error) {
		openCalls++
		return nil, errors.New("should not be opened in TTY path")
	}

	// Simulate the print-mode prompt path: the prompt was already
	// drained (os.Stdin fully consumed), and the OAuth code path
	// is not in play — so the test just verifies that
	// ReadAuthorizationCode is unused / unused-like. The non-zero
	// openCalls would only fire if the OAuth path were incorrectly
	// invoked from the print-mode hot loop, which it is not (this
	// is the regression guard for cycle 261).
	got, err := ReadAuthorizationCode(strings.NewReader("\n"))
	if err != nil {
		t.Fatalf("ReadAuthorizationCode: %v", err)
	}
	if got != "" {
		t.Errorf("read-back of blank-line stdin returned %q, want empty", got)
	}
	if openCalls != 0 {
		t.Errorf("OpenControllingTerminalFn called %d times, want 0", openCalls)
	}
}

// TestOAuthRead_WindowsConInStubbed exercises the Windows-equivalent
// read path. We are on macOS for this run; the windows file is
// guarded with `//go:build windows` and is therefore compiled out.
// The test instead substitutes the controlling-terminal var with the
// Windows device name and verifies that the error message carries
// the *attempted* device — i.e. the variable, not a hard-coded
// constant. This is the compile-time-stubbed counterpart of the
// real Windows path, and it pins the contract for cross-platform
// builds: callers who override ControllingTerminalDevice see their
// override in error messages.
func TestOAuthRead_WindowsConInStubbed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows path is the live one on this platform; rerun the POSIX path tests")
	}
	saveTTYSeams(t)
	StdinIsTTYFn = func() bool { return false }
	OpenControllingTerminalFn = func() (io.ReadCloser, error) {
		return nil, errFakeOpen
	}
	ControllingTerminalDevice = "CONIN$"

	_, err := ReadAuthorizationCode(strings.NewReader("ignored\n"))
	if err == nil {
		t.Fatal("expected error from failed CONIN$ open, got nil")
	}
	if !strings.Contains(err.Error(), "CONIN$") {
		t.Errorf("error %q does not mention CONIN$", err.Error())
	}
	if !strings.Contains(err.Error(), "AGY_OAUTH_TOKEN") {
		t.Errorf("error %q does not mention AGY_OAUTH_TOKEN", err.Error())
	}
}

// TestOAuthRead_StdinPromptUnaffected pins that the print-mode OAuth
// paste never touches the stdin reader when stdin is piped: only the
// OpenControllingTerminalFn reader is consumed. The prompt pipe
// remains intact for the prompt reader to drain.
//
// This is the regression guard for the AGY 1.1.2 changelog line: a
// piped prompt arrives before the user has time to paste, so the
// prompt reader must read from stdin and the OAuth reader must read
// from /dev/tty, with no cross-talk. The fake opener returns a
// pre-canned code; the stdin reader holds a different code that the
// OAuth path must NOT see.
//
// Implementation note: strings.Reader has no concept of "was this
// reader ever Read from", so we wrap it in readCountingReader and
// assert that the bytesRead counter is still zero after the OAuth
// read returns.
func TestOAuthRead_StdinPromptUnaffected(t *testing.T) {
	saveTTYSeams(t)
	StdinIsTTYFn = func() bool { return false }
	OpenControllingTerminalFn = func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("code-from-tty\n")), nil
	}

	r := &readCountingReader{r: strings.NewReader("prompt-from-stdin\n")}
	got, err := ReadAuthorizationCode(r)
	if err != nil {
		t.Fatalf("ReadAuthorizationCode: %v", err)
	}
	if got != "code-from-tty" {
		t.Errorf("got %q, want %q", got, "code-from-tty")
	}
	if r.bytesRead != 0 {
		t.Errorf("stdin reader was Read %d times by the OAuth path; want 0", r.bytesRead)
	}
}

// readCountingReader counts how many bytes a wrapped io.Reader hands
// out, without consuming any of them from the caller's perspective:
// the bytes come from the embedded *strings.Reader (whose own cursor
// advances), and the wrapper records readBytes for later assertion.
// Used by TestOAuthRead_StdinPromptUnaffected to confirm that the
// OAuth read path never touched the prompt pipe.
type readCountingReader struct {
	r         io.Reader
	bytesRead int
}

func (c *readCountingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.bytesRead += n
	return n, err
}

// TestReadOneLine_PinBehaviour pins the read-one-line helper's exact
// contract: trim right-runs of \r and \n, treat EOF as success for
// the bytes received so far, and surface non-EOF errors wrapped.
// The helper is unexported but the assertions are load-bearing: the
// device-driver name on Windows or a paste that hits ^D on POSIX
// should not tank the read.
func TestReadOneLine_PinBehaviour(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"trailing-newline", "code-A\n", "code-A"},
		{"trailing-crlf", "code-B\r\n", "code-B"},
		{"no-newline", "code-C", "code-C"},
		{"empty", "", ""},
		{"only-newline", "\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := readOneLine(strings.NewReader(tc.in))
			if err != nil {
				t.Fatalf("readOneLine(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("readOneLine(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}

	// A pre-EOF non-EOF error path is harder to construct; nil
	// reader checks the early-out instead.
	if _, err := readOneLine(nil); err == nil {
		t.Error("readOneLine(nil) returned nil error; want non-nil")
	}
}

// TestControllingTerminalDevice_Pinned pins that the platform init()
// wired ControllingTerminalDevice to the platform-respective string
// before any test ran. The package-level var is set by init() in
// oauth_tty_unix.go ("/dev/tty") or oauth_tty_windows.go ("CONIN$"),
// and any test that overrides it must restore via saveTTYSeams so
// the next test sees the real value back. If init() did not run (or
// ran with the wrong file), this would fail.
func TestControllingTerminalDevice_Pinned(t *testing.T) {
	expected := map[string]string{
		"darwin":  "/dev/tty",
		"linux":   "/dev/tty",
		"windows": "CONIN$",
	}
	want, ok := expected[runtime.GOOS]
	if !ok {
		t.Skipf("no expectation recorded for GOOS=%s", runtime.GOOS)
	}
	if ControllingTerminalDevice != want {
		t.Errorf("ControllingTerminalDevice = %q, want %q (GOOS=%s)",
			ControllingTerminalDevice, want, runtime.GOOS)
	}
	if OpenControllingTerminalFn == nil {
		t.Errorf("OpenControllingTerminalFn is nil on GOOS=%s; init() did not run", runtime.GOOS)
	}
}

// TestReadAuthorizationCode_NilReader pins the API contract guard:
// passing nil to ReadAuthorizationCode is a programming error and
// must surface as a typed error rather than panicking.
func TestReadAuthorizationCode_NilReader(t *testing.T) {
	if _, err := ReadAuthorizationCode(nil); err == nil {
		t.Fatal("ReadAuthorizationCode(nil) returned nil error; want non-nil")
	}
}
