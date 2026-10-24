package clipboard_test

import (
	"bytes"
	"encoding/base64"
	"os"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/clipboard"
)

func clearEnv(t *testing.T, keys ...string) {
	t.Helper()
	for _, k := range keys {
		old, set := os.LookupEnv(k)
		_ = os.Unsetenv(k)
		if set {
			v := old
			key := k
			t.Cleanup(func() { _ = os.Setenv(key, v) })
		}
	}
}

func TestSupportsOSC52_PinsKnownTerminals(t *testing.T) {
	clearEnv(t, "SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY",
		"MOSH_CONNECTION", "TERM_PROGRAM", "LC_TERMINAL", "TERM")

	cases := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"ssh-connection", map[string]string{"SSH_CONNECTION": "1.2.3.4 5 6.7.8.9 22"}, true},
		{"ssh-client", map[string]string{"SSH_CLIENT": "1.2.3.4 22 6.7.8.9"}, true},
		{"ssh-tty", map[string]string{"SSH_TTY": "/dev/pts/0"}, true},
		{"mosh", map[string]string{"MOSH_CONNECTION": "mosh-1.3.2"}, true},

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

		{"iterm2-via-lc-terminal", map[string]string{"LC_TERMINAL": "iTerm2"}, true},
		{"iterm2-lowercase", map[string]string{"LC_TERMINAL": "iterm2"}, true},
		{"not-iterm2", map[string]string{"LC_TERMINAL": "xterm"}, false},

		{"screen", map[string]string{"TERM": "screen"}, false},
		{"screen-256color", map[string]string{"TERM": "screen-256color"}, false},
		{"tmux-screen", map[string]string{"TERM": "screen.xterm-256color"}, false},
		{"plain-xterm", map[string]string{"TERM": "xterm"}, false},
		{"plain-xterm-mono", map[string]string{"TERM": "xterm-mono"}, false},
		{"vt100", map[string]string{"TERM": "vt100"}, false},
		{"vt220", map[string]string{"TERM": "vt220"}, false},
		{"dumb", map[string]string{"TERM": "dumb"}, false},
		{"no-term", map[string]string{"TERM": ""}, false},

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
			got := clipboard.SupportsOSC52()
			if got != c.want {
				t.Errorf("SupportsOSC52() with env %v = %v, want %v", c.env, got, c.want)
			}
		})
	}
}

func TestSupportsOSC52_SSHTakesPrecedence(t *testing.T) {
	clearEnv(t, "SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY",
		"MOSH_CONNECTION", "TERM_PROGRAM", "LC_TERMINAL", "TERM")
	_ = os.Setenv("SSH_CONNECTION", "1.2.3.4 5 6.7.8.9 22")
	_ = os.Setenv("TERM_PROGRAM", "Apple_Terminal")
	if !clipboard.SupportsOSC52() {
		t.Error("SSH+Apple Terminal: expected OSC 52")
	}
}

func TestOsc52WriteText_EmptyInput(t *testing.T) {
	var buf bytes.Buffer
	if clipboard.WriteOSC52(&buf, "") {
		t.Error("WriteOSC52 on empty input returned true")
	}
	if buf.Len() != 0 {
		t.Errorf("expected zero bytes written, got %d: %q", buf.Len(), buf.String())
	}
}

func TestOsc52WriteText_EmitsEscape(t *testing.T) {
	var buf bytes.Buffer
	if !clipboard.WriteOSC52(&buf, "hello world") {
		t.Fatal("WriteOSC52 returned false")
	}
	got := buf.String()
	if !strings.HasPrefix(got, "\x1b]52;c;") {
		t.Errorf("OSC 52 escape missing prefix: %q", got)
	}
	if !strings.HasSuffix(got, "\x07") {
		t.Errorf("OSC 52 escape missing BEL terminator: %q", got)
	}
	inner := got[len("\x1b]52;c;") : len(got)-1]
	decoded, err := base64.StdEncoding.DecodeString(inner)
	if err != nil {
		t.Fatalf("base64 decode of %q: %v", inner, err)
	}
	if string(decoded) != "hello world" {
		t.Errorf("decoded payload = %q, want %q", decoded, "hello world")
	}
}

func TestOsc52WriteText_LargeInputRespectsLimit(t *testing.T) {
	large := strings.Repeat("A", 200*1024)
	var buf bytes.Buffer
	if clipboard.WriteOSC52(&buf, large) {
		t.Error("WriteOSC52 on 200 KB input returned true; expected false")
	}
	if buf.Len() != 0 {
		t.Errorf("oversized input: expected zero bytes written, got %d", buf.Len())
	}
}

func TestCopy_OSC52Preferred_WhenSupported(t *testing.T) {
	clearEnv(t, "SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY",
		"MOSH_CONNECTION", "TERM_PROGRAM", "LC_TERMINAL", "TERM")
	_ = os.Setenv("TERM_PROGRAM", "WezTerm")

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

	if err := clipboard.Write("hello world"); err != nil {
		t.Fatalf("clipboard.Write error: %v", err)
	}
	_ = w.Close()
	got, _ := readAll(r)
	gotStr := string(got)
	if !strings.HasPrefix(gotStr, "\x1b]52;c;") || !strings.HasSuffix(gotStr, "\x07") {
		t.Errorf("OSC 52 escape missing: %q", gotStr)
	}
}

func TestCopy_FallbackAtotto_WhenNotSupported(t *testing.T) {
	clearEnv(t, "SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY",
		"MOSH_CONNECTION", "TERM_PROGRAM", "LC_TERMINAL", "TERM")
	_ = os.Setenv("TERM_PROGRAM", "Apple_Terminal")

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

	_ = clipboard.Write("hello world")
	_ = w.Close()
	got, _ := readAll(r)
	gotStr := string(got)
	if strings.Contains(gotStr, "\x1b]52;") {
		t.Errorf("OSC 52 escape should NOT be emitted on Apple Terminal: %q", gotStr)
	}
}

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
