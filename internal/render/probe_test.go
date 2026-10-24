package render

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

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
			_ = ProbeHyperlinkCaps(&buf, c.env, c.tmuxProbeResult, c.altScreen)
			got := buf.String()
			if !strings.HasPrefix(got, HyperlinkLogTag) {
				t.Fatalf("log line missing prefix %q: %q", HyperlinkLogTag, got)
			}
			for _, want := range c.wantLineContains {
				if !strings.Contains(got, want) {
					t.Errorf("log line missing %q in: %q", want, got)
				}
			}
		})
	}
}

func TestHyperlinkCapsFromEnv_NilEnv(t *testing.T) {
	t.Setenv("TERM", "foo")
	t.Setenv("TERM_PROGRAM", "WezTerm")
	var buf bytes.Buffer
	caps := ProbeHyperlinkCaps(&buf, nil, false, false)
	if !strings.Contains(buf.String(), `TERM="foo"`) {
		t.Errorf("nil env: log line missing TERM=foo: %q", buf.String())
	}
	if !caps.OSC8 {
		t.Errorf("WezTerm should report OSC8=true, got %+v", caps)
	}
}

func TestHyperlinkCapsFromEnv_DiscardWriter(t *testing.T) {
	caps := ProbeHyperlinkCaps(io.Discard, envSlice("TERM=xterm-256color"), false, false)
	if caps.Terminal != "xterm-256color" {
		t.Errorf("expected Terminal=xterm-256color, got %q", caps.Terminal)
	}
	if !caps.OSC8 {
		t.Errorf("xterm-256color should be OSC8=true (modern xterm ≥ 379)")
	}
}

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
			if got := TmuxHyperlinkProbe(); got != c.want {
				t.Errorf("WAGYU_TMUX_HYPERLINK=%s → TmuxHyperlinkProbe()=%v, want %v", c.val, got, c.want)
			}
		})
	}
}

func TestTmuxHyperlinkProbe_NotInTmux(t *testing.T) {
	prev, hadPrev := os.LookupEnv("TMUX")
	if hadPrev {
		os.Unsetenv("TMUX")
		t.Cleanup(func() { os.Setenv("TMUX", prev) })
	}
	if got := TmuxHyperlinkProbe(); got != false {
		t.Errorf("not-in-tmux: TmuxHyperlinkProbe()=%v, want false", got)
	}
}

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
			if got := IsTmuxSession(); got != c.want {
				t.Errorf("TMUX=%q → IsTmuxSession()=%v, want %v", c.val, got, c.want)
			}
		})
	}
}

func envSlice(kvs ...string) []string {
	out := make([]string, len(kvs))
	copy(out, kvs)
	return out
}
