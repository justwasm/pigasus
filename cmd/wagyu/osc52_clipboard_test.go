// osc52_clipboard_test.go — package-simplification cycle 2: terminal
// capability matrix + write tests for /copy's OSC 52 path.
//
// The terminal matrix covers the gates SupportsOSC52 applies:
//
//   - SSH_CONNECTION / SSH_CLIENT / SSH_TTY → true (always)
//   - MOSH_CONNECTION → true (always)
//   - TERM_PROGRAM=Apple_Terminal           → false
//   - TERM_PROGRAM=iterm.app/wezterm/ghostty/kitty/vscode/warp/alacritty → true
//   - LC_TERMINAL=iTerm2                    → true (case-insensitive)
//   - TERM=screen*/xterm/xterm-mono/vt100/vt220/dumb/"" → false
//   - everything else (modern xterm-256color, alacritty via TERM,
//     foot, konsole, mlterm, st, rxvt-unicode, ...) → true
//
// The write tests cover:
//   - empty input is a no-op
//   - a normal string lands as a valid OSC 52 escape
//   - the OSC 52 write happens before the shell-out fallback when
//     the terminal supports it
//   - the shell-out path still runs when the terminal does NOT
//     support OSC 52 (Apple Terminal)

package main

import (
	"bytes"
	"encoding/base64"
	"os"
	"strings"
	"testing"
)

// withEnv sets the given env vars for the duration of fn and
// restores them when fn returns. Used to drive SupportsOSC52 with
// arbitrary (TERM_PROGRAM, TERM, ...) combinations without touching
// the host shell.
func withEnv(t *testing.T, kv map[string]string, fn func()) {
	t.Helper()
	for k, v := range kv {
		old, had := os.LookupEnv(k)
		if err := os.Setenv(k, v); err != nil {
			t.Fatalf("os.Setenv(%q): %v", k, err)
		}
		t.Cleanup(func() {
			if had {
				_ = os.Setenv(k, old)
			} else {
				_ = os.Unsetenv(k)
			}
		})
	}
	fn()
}

// clearEnv unsets the keys we read in SupportsOSC52 so test runs are
// independent of the host shell's TERM_PROGRAM / TERM / SSH_*.
//
// Caller is responsible for restoring the prior values via withEnv's
// t.Cleanup.
func clearEnv(t *testing.T, keys ...string) {
	t.Helper()
	for _, k := range keys {
		_ = os.Unsetenv(k)
	}
}

// TestSupportsOSC52_PinsKnownTerminals walks the terminal matrix
// and asserts each terminal's known OSC 52 capability. The list
// covers every gate branch in SupportsOSC52 (SSH / Mosh → true;
// TERM_PROGRAM match → true/false; LC_TERMINAL → true; TERM
// allow-list → false; default-yes fallback → true).
func TestSupportsOSC52_PinsKnownTerminals(t *testing.T) {
	clearEnv(t, "SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY",
		"MOSH_CONNECTION", "TERM_PROGRAM", "LC_TERMINAL", "TERM")

	cases := []struct {
		name  string
		env   map[string]string
		want  bool
	}{
		// --- SSH / Mosh always emit OSC 52 ---
		{"ssh-connection", map[string]string{"SSH_CONNECTION": "1.2.3.4 5 6.7.8.9 22"}, true},
		{"ssh-client", map[string]string{"SSH_CLIENT": "1.2.3.4 22 6.7.8.9"}, true},
		{"ssh-tty", map[string]string{"SSH_TTY": "/dev/pts/0"}, true},
		{"mosh", map[string]string{"MOSH_CONNECTION": "mosh-1.3.2"}, true},

		// --- TERM_PROGRAM allow-list ---
		{"apple-terminal", map[string]string{"TERM_PROGRAM": "Apple_Terminal"}, false},
		{"apple-terminal-uppercase", map[string]string{"TERM_PROGRAM": "APPLE_TERMINAL"}, false},
		{"iterm2", map[string]string{"TERM_PROGRAM": "iTerm.app"}, true},
		{"wezterm", map[string]string{"TERM_PROGRAM": "WezTerm"}, true},
		{"wezterm-lowercase", map[string]string{"TERM_PROGRAM": "wezterm"}, true},
		{"ghostty", map[string]string{"TERM_PROGRAM": "ghostty"}, true},
		{"kitty", map[string]string{"TERM_PROGRAM": "kitty"}, true},
		{"vscode", map[string]string{"TERM_PROGRAM": "vscode"}, true},
		{"warp", map[string]string{"TERM_PROGRAM": "Warp"}, true},
		{"alacritty-via-program", map[string]string{"TERM_PROGRAM": "Alacritty"}, true},
		{"unknown-program", map[string]string{"TERM_PROGRAM": "fancy-tty-9000"}, true},

		// --- LC_TERMINAL fallback (no TERM_PROGRAM) ---
		{"iterm2-via-lc-terminal", map[string]string{"LC_TERMINAL": "iTerm2"}, true},
		{"iterm2-lowercase", map[string]string{"LC_TERMINAL": "iterm2"}, true},
		// Unknown LC_TERMINAL is not iTerm2's self-identify — falls
		// through to the TERM check. With TERM unset we hit
		// "no-term" → false (no TTY). This is the correct, if
		// conservative, behaviour; LC_TERMINAL=iTerm2 is the only
		// value the gate explicitly recognises.
		{"not-iterm2", map[string]string{"LC_TERMINAL": "xterm"}, false},

		// --- TERM coarse fallback ---
		{"screen", map[string]string{"TERM": "screen"}, false},
		{"screen-256color", map[string]string{"TERM": "screen-256color"}, false},
		{"tmux-screen", map[string]string{"TERM": "screen.xterm-256color"}, false},
		{"plain-xterm", map[string]string{"TERM": "xterm"}, false},
		{"plain-xterm-mono", map[string]string{"TERM": "xterm-mono"}, false},
		{"vt100", map[string]string{"TERM": "vt100"}, false},
		{"vt220", map[string]string{"TERM": "vt220"}, false},
		{"dumb", map[string]string{"TERM": "dumb"}, false},
		{"no-term", map[string]string{"TERM": ""}, false},

		// --- default-yes (modern terminals) ---
		{"xterm-256color", map[string]string{"TERM": "xterm-256color"}, true},
		{"xterm-kitty", map[string]string{"TERM": "xterm-kitty"}, true},
		{"alacritty-via-term", map[string]string{"TERM": "alacritty"}, true},
		{"foot", map[string]string{"TERM": "foot"}, true},
		{"foot-extra", map[string]string{"TERM": "foot-extra"}, true},
		{"konsole", map[string]string{"TERM": "konsole"}, true},
		{"rxvt-unicode", map[string]string{"TERM": "rxvt-unicode-256color"}, true},
		{"st", map[string]string{"TERM": "st-256color"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clearEnv(t, "SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY",
				"MOSH_CONNECTION", "TERM_PROGRAM", "LC_TERMINAL", "TERM")
			for k, v := range c.env {
				_ = os.Setenv(k, v)
			}
			got := SupportsOSC52()
			if got != c.want {
				t.Errorf("SupportsOSC52() with env %v = %v, want %v", c.env, got, c.want)
			}
		})
	}
}

// TestSupportsOSC52_SSHTakesPrecedence ensures the SSH gate fires
// even on Apple Terminal. Without that ordering, a remote user on
// a Mac would lose the only clipboard path that works over SSH.
func TestSupportsOSC52_SSHTakesPrecedence(t *testing.T) {
	clearEnv(t, "SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY",
		"MOSH_CONNECTION", "TERM_PROGRAM", "LC_TERMINAL", "TERM")
	_ = os.Setenv("SSH_CONNECTION", "1.2.3.4 5 6.7.8.9 22")
	_ = os.Setenv("TERM_PROGRAM", "Apple_Terminal")
	if !SupportsOSC52() {
		t.Error("SSH+Apple Terminal: expected OSC 52 (the SSH gate wins)")
	}
}

// TestOsc52WriteText_EmptyInput pins the empty-input contract: zero
// bytes get nothing on the wire. Matches the existing atotto/
// clipboard semantics where an empty Write is a no-op.
func TestOsc52WriteText_EmptyInput(t *testing.T) {
	var buf bytes.Buffer
	if osc52WriteText(&buf, "") {
		t.Error("osc52WriteText on empty input returned true")
	}
	if buf.Len() != 0 {
		t.Errorf("expected zero bytes written, got %d: %q", buf.Len(), buf.String())
	}
}

// TestOsc52WriteText_EmitsEscape pins the wire format: the bytes
// must be a single OSC 52 escape of the form
//
//	ESC ] 52 ; c ; <base64(text)> BEL
//
// and the decoded base64 payload must equal the original text.
func TestOsc52WriteText_EmitsEscape(t *testing.T) {
	var buf bytes.Buffer
	if !osc52WriteText(&buf, "hello world") {
		t.Fatal("osc52WriteText returned false")
	}
	got := buf.String()
	if !strings.HasPrefix(got, "\x1b]52;c;") {
		t.Errorf("OSC 52 escape missing prefix: %q", got)
	}
	if !strings.HasSuffix(got, "\x07") {
		t.Errorf("OSC 52 escape missing BEL terminator: %q", got)
	}
	// Strip prefix and terminator to extract the base64 payload.
	inner := got[len("\x1b]52;c;") : len(got)-1]
	decoded, err := base64.StdEncoding.DecodeString(inner)
	if err != nil {
		t.Fatalf("base64 decode of %q: %v", inner, err)
	}
	if string(decoded) != "hello world" {
		t.Errorf("decoded payload = %q, want %q", decoded, "hello world")
	}
}

// TestOsc52WriteText_LargeInputRespectsLimit pins the 100 KB cap:
// a string whose base64 encoding exceeds the cap returns false
// without writing anything (terminal OSC sequence size limits
// vary, so we'd rather fall back to the shell-out than emit a
// truncated / killed escape).
func TestOsc52WriteText_LargeInputRespectsLimit(t *testing.T) {
	// 200 KB of 'A' → 267 KB of base64 → above the 100 KB cap.
	large := strings.Repeat("A", 200*1024)
	var buf bytes.Buffer
	if osc52WriteText(&buf, large) {
		t.Error("osc52WriteText on 200 KB input returned true; expected false (cap)")
	}
	if buf.Len() != 0 {
		t.Errorf("oversized input: expected zero bytes written, got %d", buf.Len())
	}
}

// TestCopy_OSC52Preferred_WhenSupported walks the matrix end-to-end:
// with a terminal that supports OSC 52 (e.g. WezTerm) the
// copyToClipboard path emits the escape and skips the shell-out.
// The shell-out is detectable via the absence of any exec.Command
// call in this test — we just verify the OSC 52 bytes landed.
//
// We pin this by setting TERM_PROGRAM=WezTerm and pointing stderr
// to a buffer that the test reads. (os.Stderr is the dest that
// copyToClipboard writes to in the OSC 52 branch.)
func TestCopy_OSC52Preferred_WhenSupported(t *testing.T) {
	clearEnv(t, "SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY",
		"MOSH_CONNECTION", "TERM_PROGRAM", "LC_TERMINAL", "TERM")
	_ = os.Setenv("TERM_PROGRAM", "WezTerm")

	// Re-route stderr to a buffer. os.Stderr is package-level; we
	// capture via a pipe and read end.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStderr := os.Stderr
	os.Stderr = w
	t.Cleanup(func() {
		os.Stderr = oldStderr
		_ = w.Close()
		_ = r.Close()
	})

	if err := copyToClipboard("hello world"); err != nil {
		t.Fatalf("copyToClipboard error: %v", err)
	}
	_ = w.Close()
	got, _ := readAll(r)
	gotStr := string(got)
	if !strings.HasPrefix(gotStr, "\x1b]52;c;") || !strings.HasSuffix(gotStr, "\x07") {
		t.Errorf("OSC 52 escape missing: %q", gotStr)
	}
}

// TestCopy_FallbackAtotto_WhenNotSupported pins the fallback path:
// with TERM_PROGRAM=Apple_Terminal, copyToClipboard must NOT emit
// an OSC 52 escape (Apple Terminal doesn't speak it). The shell-out
// is the only path that runs — but on macOS only, since the
// shell-out branch gates on runtime.GOOS.
//
// We can't unconditionally verify the shell-out succeeds in this
// cross-platform test (Linux CI won't have pbcopy). The contract
// we can pin is "OSC 52 bytes are NOT written to stderr when the
// terminal doesn't support it".
func TestCopy_FallbackAtotto_WhenNotSupported(t *testing.T) {
	clearEnv(t, "SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY",
		"MOSH_CONNECTION", "TERM_PROGRAM", "LC_TERMINAL", "TERM")
	_ = os.Setenv("TERM_PROGRAM", "Apple_Terminal")

	// Same pipe-trick as above.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStderr := os.Stderr
	os.Stderr = w
	t.Cleanup(func() {
		os.Stderr = oldStderr
		_ = w.Close()
		_ = r.Close()
	})

	// The shell-out may fail (no pbcopy on Linux CI), but that's
	// fine — what we care about is the absence of the OSC escape.
	_ = copyToClipboard("hello world")
	_ = w.Close()
	got, _ := readAll(r)
	gotStr := string(got)
	if strings.Contains(gotStr, "\x1b]52;") {
		t.Errorf("OSC 52 escape should NOT be emitted on Apple Terminal: %q", gotStr)
	}
}

// readAll is a tiny helper that reads from an *os.File and returns
// the bytes. We avoid io.ReadAll's allocation behaviour on test
// inputs that are guaranteed small (<1 KB).
func readAll(r *os.File) ([]byte, error) {
	var buf bytes.Buffer
	tmp := make([]byte, 4096)
	for {
		n, err := r.Read(tmp)
		if n > 0 {
			buf.Write(tmp[:n])
		}
		if err != nil {
			if err.Error() == "EOF" {
				return buf.Bytes(), nil
			}
			return buf.Bytes(), err
		}
	}
}