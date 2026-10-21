// hyperlink_caps_test.go — cycle osc-hyperlinks (cycle 2 of the OSC pair):
// pins the terminal capability detection for OSC 8 hyperlinks.
//
// Tests live in two halves:
//
//   - TestDetectHyperlinkCaps — table-driven, one row per terminal /
//     condition the AGY 1.4.0 changelog calls out. The pin format mirrors
//     cmd/wagyu/colorprofile_lc_terminal_test.go's LC_TERMINAL table;
//     each row declares an env slice and a tmux probe result, plus a
//     want HyperlinkCaps value.
//
//   - TestHyperlinkPlainTextFallback — pins the OS-Cycle-2 helper shape
//     for caps.OSC8 == false. The fallback is "<label> (<url>)", which
//     matches AGY's strings table row "Fixed links printing as raw escape
//     sequences on terminals that do not implement OSC 8 hyperlinks".
//
//   - TestHyperlinkOSCEnabled — pins the OSC 8 enabled branch using caps
//     produced by DetectHyperlinkCaps, so the two halves agree on what
//     "OSC 8 enabled" means at the bytes level.
//
// The tests do not touch process env (env is taken as a slice argument);
// environment variability is the main reason terminal detection has
// historically shipped flaky.

package render

import (
	"strings"
	"testing"
)

// envFrom builds an os.Environ()-shaped slice from key/value pairs.
// Convenience for keeping the table rows readable.
func envFrom(kvs ...string) []string {
	if len(kvs)%2 != 0 {
		panic("envFrom: odd number of arguments")
	}
	out := make([]string, 0, len(kvs)/2)
	for i := 0; i < len(kvs); i += 2 {
		out = append(out, kvs[i]+"="+kvs[i+1])
	}
	return out
}

func TestDetectHyperlinkCaps(t *testing.T) {
	cases := []struct {
		name            string
		env             []string
		tmuxProbeResult bool
		want            HyperlinkCaps
	}{
		{
			name: "empty env defaults to OSC 8 off",
			env:  nil,
			want: HyperlinkCaps{
				OSC8:  false,
				OSC52: false,
			},
		},
		{
			name: "Apple Terminal disables OSC 8",
			env:  envFrom("TERM_PROGRAM", "Apple_Terminal"),
			want: HyperlinkCaps{
				OSC8:     false,
				OSC52:    false,
				Terminal: "Apple_Terminal",
			},
		},
		{
			name: "Apple Terminal over SSH marks the SSH+Terminal.app case",
			env: envFrom(
				"TERM_PROGRAM", "Apple_Terminal",
				"SSH_CONNECTION", "192.0.2.1 12345 198.51.100.1 22",
			),
			want: HyperlinkCaps{
				OSC8:             false,
				OSC52:            false,
				Terminal:         "Apple_Terminal",
				SSH:              true,
				AppleTerminalSSH: true,
			},
		},
		{
			name: "GNU screen 256colour disables OSC 8",
			env:  envFrom("TERM", "screen-256color"),
			want: HyperlinkCaps{
				OSC8:     false,
				OSC52:    false,
				Terminal: "screen",
			},
		},
		{
			name: "GNU screen-256color-bce disables OSC 8",
			env:  envFrom("TERM", "screen-256color-bce"),
			want: HyperlinkCaps{
				OSC8:     false,
				OSC52:    false,
				Terminal: "screen",
			},
		},
		{
			name: "tmux without passthrough disables OSC 8",
			env:  envFrom("TERM", "tmux-256color", "TMUX", "/tmp/tmux-1000/default,12345,0"),
			want: HyperlinkCaps{
				OSC8:     false,
				OSC52:    true,
				Terminal: "tmux",
			},
		},
		{
			name:            "tmux with passthrough enabled keeps OSC 8 on",
			env:             envFrom("TERM", "tmux-256color", "TMUX", "/tmp/tmux-1000/default,12345,0"),
			tmuxProbeResult: true,
			want: HyperlinkCaps{
				OSC8:                     true,
				OSC52:                    true,
				Terminal:                 "tmux",
				TmuxHyperlinkPassthrough: true,
			},
		},
		{
			name: "kitty enables OSC 8",
			env:  envFrom("TERM_PROGRAM", "kitty", "TERM", "xterm-kitty"),
			want: HyperlinkCaps{
				OSC8:     true,
				OSC52:    true,
				Terminal: "kitty",
			},
		},
		{
			name: "ghostty enables OSC 8",
			env:  envFrom("TERM_PROGRAM", "ghostty", "TERM", "xterm-ghostty"),
			want: HyperlinkCaps{
				OSC8:     true,
				OSC52:    true,
				Terminal: "ghostty",
			},
		},
		{
			name: "WezTerm enables OSC 8",
			env:  envFrom("TERM_PROGRAM", "WezTerm", "TERM", "wezterm"),
			want: HyperlinkCaps{
				OSC8:     true,
				OSC52:    true,
				Terminal: "WezTerm",
			},
		},
		{
			name: "foot enables OSC 8",
			env:  envFrom("TERM_PROGRAM", "foot", "TERM", "foot-extra"),
			want: HyperlinkCaps{
				OSC8:     true,
				OSC52:    true,
				Terminal: "foot",
			},
		},
		{
			name: "iTerm.app enables OSC 8",
			env:  envFrom("TERM_PROGRAM", "iTerm.app", "TERM", "xterm-256color"),
			want: HyperlinkCaps{
				OSC8:     true,
				OSC52:    true,
				Terminal: "iTerm2",
			},
		},
		{
			name: "iTerm2 (legacy) enables OSC 8",
			env:  envFrom("TERM_PROGRAM", "iTerm2", "TERM", "xterm-256color"),
			want: HyperlinkCaps{
				OSC8:     true,
				OSC52:    true,
				Terminal: "iTerm2",
			},
		},
		{
			name: "konsole enables OSC 8",
			env:  envFrom("TERM_PROGRAM", "konsole", "TERM", "xterm-256color"),
			want: HyperlinkCaps{
				OSC8:     true,
				OSC52:    true,
				Terminal: "konsole",
			},
		},
		{
			name: "alacritty enables OSC 8",
			env:  envFrom("TERM_PROGRAM", "alacritty", "TERM", "alacritty"),
			want: HyperlinkCaps{
				OSC8:     true,
				OSC52:    true,
				Terminal: "alacritty",
			},
		},
		{
			name: "VS Code integrated enables OSC 8 (≥ 1.79)",
			env:  envFrom("TERM_PROGRAM", "vscode", "TERM", "xterm-256color"),
			want: HyperlinkCaps{
				OSC8:     true,
				OSC52:    true,
				Terminal: "vscode",
			},
		},
		{
			name: "xterm-256color local enables OSC 8 (modern xterm ≥ 379 supports OSC 8)",
			env:  envFrom("TERM", "xterm-256color"),
			want: HyperlinkCaps{
				OSC8:     true,
				OSC52:    true,
				Terminal: "xterm-256color",
			},
		},
		{
			name: "SSH + xterm-256color + no TERM_PROGRAM keeps OSC 8 on (modern ssh passthrough)",
			env: envFrom(
				"TERM", "xterm-256color",
				"SSH_CONNECTION", "192.0.2.1 12345 198.51.100.1 22",
			),
			want: HyperlinkCaps{
				OSC8:     true,
				OSC52:    true,
				Terminal: "xterm-256color",
				SSH:      true,
			},
		},
		{
			name: "SSH + kitty keeps OSC 8 on",
			env: envFrom(
				"TERM_PROGRAM", "kitty",
				"TERM", "xterm-kitty",
				"SSH_CONNECTION", "192.0.2.1 12345 198.51.100.1 22",
			),
			want: HyperlinkCaps{
				OSC8:     true,
				OSC52:    true,
				Terminal: "kitty",
				SSH:      true,
			},
		},
		{
			name: "SSH + Apple_Terminal disables OSC 8 (the changelog case)",
			env: envFrom(
				"TERM_PROGRAM", "Apple_Terminal",
				"TERM", "xterm-256color",
				"SSH_CONNECTION", "192.0.2.1 12345 198.51.100.1 22",
			),
			want: HyperlinkCaps{
				OSC8:             false,
				OSC52:            false,
				Terminal:         "Apple_Terminal",
				SSH:              true,
				AppleTerminalSSH: true,
			},
		},
		{
			name: "SSH + GNU screen disables OSC 8",
			env: envFrom(
				"TERM", "screen-256color",
				"SSH_CONNECTION", "192.0.2.1 12345 198.51.100.1 22",
			),
			want: HyperlinkCaps{
				OSC8:     false,
				OSC52:    false,
				Terminal: "screen",
				SSH:      true,
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := DetectHyperlinkCaps(c.env, c.tmuxProbeResult)
			if got != c.want {
				t.Errorf("DetectHyperlinkCaps(%q, %v) = %+v, want %+v",
					c.env, c.tmuxProbeResult, got, c.want)
			}
		})
	}
}

// TestHyperlinkPlainTextFallback pins the OSC 8 disabled branch shape
// precisely: the helper returns "<label> (<url>)", with the URL
// enclosed in parentheses so the link text is still copy-pasteable.
// AGY 1.4.0 changelog (verbatim): "Fixed links printing as raw escape
// sequences on terminals that do not implement OSC 8 hyperlinks".
//
// The maxWidth clamp is not consulted on the disabled branch — the
// label is rendered verbatim so the user can copy it. The URL stays
// intact even when the label would otherwise be clamped; this
// matches the "the user asked us to print a link, not just a label"
// semantic AGY's emitted fallback carries.
func TestHyperlinkPlainTextFallback(t *testing.T) {
	cases := []struct {
		name     string
		url      string
		text     string
		maxWidth int
		want     string
	}{
		{
			name: "Apple Terminal fallback",
			url:  "https://example.com/sign-in",
			text: "Sign in",
			want: "Sign in (https://example.com/sign-in)",
		},
		{
			name: "GNU screen fallback",
			url:  "https://docs.example.com",
			text: "Docs",
			want: "Docs (https://docs.example.com)",
		},
		{
			name: "tmux-without-passthrough fallback ignores maxWidth",
			url:  "https://example.com/very/long/url/that/overflows",
			text: "label",
			// Without the OSC 8 frame, the helper returns the
			// label and the URL untouched; maxWidth is irrelevant
			// on the disabled branch.
			maxWidth: 5,
			want:     "label (https://example.com/very/long/url/that/overflows)",
		},
		{
			name: "empty URL still returns text unchanged",
			url:  "",
			text: "plain text",
			want: "plain text",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			caps := HyperlinkCaps{OSC8: false}
			got := Hyperlink(c.url, c.text, c.maxWidth, caps)
			if got != c.want {
				t.Errorf("Hyperlink(%q, %q, %d, OSC8=false) = %q, want %q",
					c.url, c.text, c.maxWidth, got, c.want)
			}
			// Crucially: no OSC 8 prefix should leak through. The
			// pre-cycle behaviour did leak (\x1b]8;;URL\x1b\\)
			// on plain text, breaking on Terminal.app and friends.
			if strings.Contains(got, "\x1b]8;;") {
				t.Errorf("plain-text fallback leaked OSC 8 prefix: %q", got)
			}
		})
	}
}

// TestHyperlinkOSC8EnabledFromDetect is the integration test: feed an
// env slice representing kitty, get caps from DetectHyperlinkCaps, and
// confirm Hyperlink emits the OSC 8 frame. Same for Apple Terminal in
// reverse — the helper emits the plain-text shape.
func TestHyperlinkOSC8EnabledFromDetect(t *testing.T) {
	t.Run("kitty env produces OSC 8 frame", func(t *testing.T) {
		env := envFrom("TERM_PROGRAM", "kitty", "TERM", "xterm-kitty")
		caps := DetectHyperlinkCaps(env, false)
		got := Hyperlink("https://example.com", "example", 20, caps)
		if !strings.HasPrefix(got, "\x1b]8;;https://example.com\x1b\\") {
			t.Errorf("missing OSC 8 opener: %q", got)
		}
		if !strings.HasSuffix(got, "\x1b]8;;\x1b\\") {
			t.Errorf("missing OSC 8 closer: %q", got)
		}
	})

	t.Run("Apple Terminal env produces plain text", func(t *testing.T) {
		env := envFrom("TERM_PROGRAM", "Apple_Terminal")
		caps := DetectHyperlinkCaps(env, false)
		got := Hyperlink("https://example.com", "example", 20, caps)
		if got != "example (https://example.com)" {
			t.Errorf("Apple Terminal fallback: got %q, want %q", got, "example (https://example.com)")
		}
	})

	t.Run("screen env produces plain text", func(t *testing.T) {
		env := envFrom("TERM", "screen-256color")
		caps := DetectHyperlinkCaps(env, false)
		got := Hyperlink("https://example.com", "example", 20, caps)
		if got != "example (https://example.com)" {
			t.Errorf("GNU screen fallback: got %q, want %q", got, "example (https://example.com)")
		}
	})
}

// TestDetectHyperlinkCapsEmptyEnvDefaults make the "empty env is safe"
// semantic a hard guarantee — DetectHyperlinkCaps(nil, false) returns
// caps with OSC 8 off. The reasoning is documented in the function
// body: with no env we cannot prove the terminal supports OSC 8, so we
// play it safe.
func TestDetectHyperlinkCapsEmptyEnvDefaults(t *testing.T) {
	got := DetectHyperlinkCaps(nil, false)
	if got.OSC8 {
		t.Errorf("empty env should default OSC8=false, got %+v", got)
	}
	if got.OSC52 {
		t.Errorf("empty env should default OSC52=false, got %+v", got)
	}
	// Document the expected default shape so a future cycle can
	// change it deliberately (e.g. if research shows we're being
	// too conservative with the default).
	want := HyperlinkCaps{OSC8: false, OSC52: false}
	if got != want {
		t.Errorf("empty env: got %+v, want %+v", got, want)
	}
}
