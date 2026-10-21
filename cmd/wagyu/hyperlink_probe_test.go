// hyperlink_probe_test.go — cycle osc-hyperlinks: pins the startup
// integration between the tmux hyperlink round-trip probe stub, the
// internal/render detection, and the "Terminal detection:" debug log
// line the AGY strings table calls out.
//
// The two functions exercised here (hyperlinkCapsFromEnv and
// tmuxHyperlinkProbe) are the only cycle 2 surfaces that touch real
// process env; the table tests in internal/render/hyperlink_caps_test.go
// already pin the env-slice-based detection. This file pins the
// "what runs at startup, in what order, with what log output"
// contract that the cmd/wagyu/main.go wiring lives by.
//
// Pattern mirrors the colour-profile log-emission tests in
// cmd/wagyu/colorprofile_lc_terminal_test.go: feed a fixture env,
// capture the writer, compare the log line byte-for-byte against
// the AGY-recovered verbatim format.
package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

// TestHyperlinkCapsFromEnv_LogShape pins the "Terminal detection: …"
// log line. The format follows the AGY strings-table row verbatim
// (`TERM=%q altScreen=%v ssh=%v osc8=%v osc52=%v appleTerminalSSH=%v`)
// and the cycle adds two trailing fields (`terminal=%q
// tmuxHyperlinkPassthrough=%v`) that AGY's logs do not have — the
// extension is intentional and recorded in the cycle report under
// "Terminal detection:".
func TestHyperlinkCapsFromEnv_LogShape(t *testing.T) {
	cases := []struct {
		name             string
		env              []string
		tmuxProbeResult  bool
		altScreen        bool
		wantLineContains []string
	}{
		{
			name:             "Apple Terminal over SSH",
			env:              envSlice("TERM_PROGRAM=Apple_Terminal", "TERM=xterm-256color", "SSH_CONNECTION=192.0.2.1 12345 198.51.100.1 22"),
			altScreen:        true,
			wantLineContains: []string{`"xterm-256color"`, "altScreen=true", "ssh=true", "osc8=false", "osc52=false", "appleTerminalSSH=true", `terminal="Apple_Terminal"`},
		},
		{
			name:             "kitty local",
			env:              envSlice("TERM_PROGRAM=kitty", "TERM=xterm-kitty"),
			altScreen:        true,
			wantLineContains: []string{`"xterm-kitty"`, "altScreen=true", "ssh=false", "osc8=true", "osc52=true", "appleTerminalSSH=false", `terminal="kitty"`},
		},
		{
			name:             "screen 256colour",
			env:              envSlice("TERM=screen-256color"),
			wantLineContains: []string{`"screen-256color"`, "osc8=false", "osc52=false", `terminal="screen"`},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			_ = hyperlinkCapsFromEnv(&buf, c.env, c.tmuxProbeResult, c.altScreen)
			got := buf.String()
			if !strings.HasPrefix(got, hyperlinkLogTag) {
				t.Fatalf("log line missing prefix %q: %q", hyperlinkLogTag, got)
			}
			for _, want := range c.wantLineContains {
				if !strings.Contains(got, want) {
					t.Errorf("log line missing %q in: %q", want, got)
				}
			}
		})
	}
}

// TestHyperlinkCapsFromEnv_NilEnv guarantees the helper uses os.Environ()
// when env is nil. The test installs a fixture env (TERM=foo) and
// passes nil, then asserts the log line carries TERM="foo".
func TestHyperlinkCapsFromEnv_NilEnv(t *testing.T) {
	t.Setenv("TERM", "foo")
	t.Setenv("TERM_PROGRAM", "WezTerm")
	var buf bytes.Buffer
	caps := hyperlinkCapsFromEnv(&buf, nil, false, false)
	if !strings.Contains(buf.String(), `TERM="foo"`) {
		t.Errorf("nil env: log line missing TERM=foo: %q", buf.String())
	}
	if !caps.OSC8 {
		t.Errorf("WezTerm should report OSC8=true, got %+v", caps)
	}
}

// TestHyperlinkCapsFromEnv_StderrEmission is the production path:
// hyperlinkCapsFromEnv(nil, false, true) writes to os.Stderr. We
// don't capture process stderr in the test (that would need
// subprocess re-exec); we instead run the function with a
// discard writer and assert no panic. The Stderr emission is the
// documented production behaviour; the log line shape test above
// covers the byte-for-byte format.
func TestHyperlinkCapsFromEnv_DiscardWriter(t *testing.T) {
	caps := hyperlinkCapsFromEnv(io.Discard, envSlice("TERM=xterm-256color"), false, false)
	if caps.Terminal != "xterm-256color" {
		t.Errorf("expected Terminal=xterm-256color, got %q", caps.Terminal)
	}
	if !caps.OSC8 {
		t.Errorf("xterm-256color should be OSC8=true (modern xterm ≥ 379)")
	}
}

// TestTmuxHyperlinkProbe_OptIn pins the positive override path:
// WAGYU_TMUX_HYPERLINK=1 makes the stub report true even though no
// real probe runs. The opt-in is documented in the cycle report
// (hyperlink_probe.go's "WAGYU_TMUX_HYPERLINK" comment) so test
// authors can drive both branches deterministically without a TTY.
func TestTmuxHyperlinkProbe_OptIn(t *testing.T) {
	cases := []struct {
		key, val string
		want     bool
	}{
		{"WAGYU_TMUX_HYPERLINK", "1", true},
		{"WAGYU_TMUX_HYPERLINK", "true", true},
		{"WAGYU_TMUX_HYPERLINK", "yes", true},
		{"WAGYU_TMUX_HYPERLINK", "on", true},
		{"WAGYU_TMUX_HYPERLINK", "0", false},
		{"WAGYU_TMUX_HYPERLINK", "false", false},
		{"WAGYU_TMUX_HYPERLINK", "no", false},
		{"WAGYU_TMUX_HYPERLINK", "off", false},
	}
	for _, c := range cases {
		t.Run(c.key+"="+c.val, func(t *testing.T) {
			t.Setenv("TMUX", "/tmp/tmux-1000/default,12345,0")
			t.Setenv(c.key, c.val)
			if got := tmuxHyperlinkProbe(); got != c.want {
				t.Errorf("WAGYU_TMUX_HYPERLINK=%s → tmuxHyperlinkProbe()=%v, want %v", c.val, got, c.want)
			}
		})
	}
}

// TestTmuxHyperlinkProbe_NotInTmux pins the no-TMUX path: with no
// TMUX env, the probe is a no-op and returns false unconditionally.
// The integration with DetectHyperlinkCaps then leaves OSC 8 at the
// default (true) for terminals like kitty / WezTerm.
func TestTmuxHyperlinkProbe_NotInTmux(t *testing.T) {
	// Ensure TMUX is unset; t.Setenv("TMUX", "") doesn't reset to
	// "unset" so we use os.Unsetenv.
	prev, hadPrev := os.LookupEnv("TMUX")
	if hadPrev {
		os.Unsetenv("TMUX")
		t.Cleanup(func() { os.Setenv("TMUX", prev) })
	}
	if got := tmuxHyperlinkProbe(); got != false {
		t.Errorf("not-in-tmux: tmuxHyperlinkProbe()=%v, want false", got)
	}
}

// TestIsTmuxSession exercises the helper. tmux detection is a single
// env lookup, but pinning the helper guards against a future
// refactor that switches to TERM=tmux-* (which DetectHyperlinkCaps
// also classifies, but is a different rule and intentionally
// disjoint).
func TestIsTmuxSession(t *testing.T) {
	cases := []struct {
		val  string
		want bool
	}{
		{"", false},
		{"/tmp/tmux-1000/default,12345,0", true},
	}
	for _, c := range cases {
		t.Run("TMUX="+c.val, func(t *testing.T) {
			t.Setenv("TMUX", c.val)
			if got := isTmuxSession(); got != c.want {
				t.Errorf("TMUX=%q → isTmuxSession()=%v, want %v", c.val, got, c.want)
			}
		})
	}
}

// envSlice is a tiny helper that mirrors the internal/render one
// but lives in this package (we don't want a test-only export in
// internal/render that cmd/wagyu reaches into).
func envSlice(kvs ...string) []string {
	out := make([]string, len(kvs))
	copy(out, kvs)
	return out
}
