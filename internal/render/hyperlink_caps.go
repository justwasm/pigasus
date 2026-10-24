// Package render: hyperlink_caps.go — terminal capability detection for
// OSC 8 hyperlinks.
//
// Cycle osc-hyperlinks (cycle 2 of the OSC pair): the prior cycle's
// render.Hyperlink always emitted OSC 8 escape sequences, which AGY 1.4.0
// changelog calls out as broken on three terminal classes — Apple Terminal,
// GNU screen, and tmux without hyperlink-passthrough enabled. The old
// helper also leaks the OSC 8 frame under any terminal that does not
// understand the sequence at all (Terminal.app over SSH is the canonical
// case).
//
// This file holds the detection. The two exported entry points are:
//
//   - HyperlinkCaps, a struct that records what the running terminal
//     supports. OSC8 is the single bool render.Hyperlink consults at
//     call time; the others are diagnostic and print in the
//     "Terminal detection:" debug log so users can audit the decision.
//   - DetectHyperlinkCaps(env, tmuxProbeResult), a pure function that
//     computes caps from a slice of "KEY=VALUE" env entries (matching
//     os.Environ's shape) and the result of the tmux hyperlink
//     round-trip probe. Returns the same value on identical inputs so
//     the rule table is unit-testable.
//
// The env slice is taken explicitly so a test can drive every branch
// without mutating the real process environment. Production callers
// (cmd/wagyu/main.go and tui_run.go) pass os.Environ().
//
// Detection rules are documented in the function body; the table-test
// counterpart (hyperlink_caps_test.go) pins each rule one row at a time.
package render

import "strings"

// HyperlinkCaps reports what the running terminal supports for OSC 8
// (and incidentally OSC 52) hyperlink emission. OSC8 is the single bool
// render.Hyperlink reads; the others exist for the "Terminal detection:"
// debug log so the user can audit why a given terminal received plain
// text instead of clickable hyperlinks.
//
// Lifecycle:
//
//   - Construct once at process start via DetectHyperlinkCaps.
//   - Treat as immutable thereafter; the detection is a pure function
//     of env vars and the tmux probe result.
//
// Concurrency: the struct is read-only after construction, so safe to
// share across goroutines without a lock.
type HyperlinkCaps struct {
	// OSC8 is the single bool the runtime reads. True means
	// "emit the OSC 8 escape sequence"; false means "fall back to
	// 'text (url)' plain-text rendering".
	OSC8 bool

	// OSC52 records whether the terminal supports OSC 52 clipboard
	// reads/writes. Currently diagnostic-only; the cross-cycle work on
	// OSC 52 gating (separate from OSC 8) lives elsewhere. False on
	// terminals that do not advertise the capability.
	OSC52 bool

	// AppleTerminalSSH records the SSH-from-Terminal.app case the
	// changelog calls out: SSH_CONNECTION set AND TERM_PROGRAM=
	// Apple_Terminal. Diagnostic; the OSC 8 disable already applies.
	AppleTerminalSSH bool

	// TmuxHyperlinkPassthrough records the round-trip probe result:
	//   true  — tmux is configured with `set-option -g set-clipboard on`
	//          AND tmux ≥ 3.3; OSC 8 sequences round-trip through it
	//          untouched.
	//   false — tmux is detected (TMUX env set) but the probe failed,
	//          meaning the OSC 8 sequence would be stripped before
	//          reaching the underlying terminal.
	//
	// Diagnostic only; the OSC 8 disable already applies when false
	// and tmux is present.
	TmuxHyperlinkPassthrough bool

	// Terminal is the recognised terminal name — one of "Apple_Terminal",
	// "WezTerm", "kitty", "ghostty", "iTerm2", "vscode", "konsole",
	// "alacritty", "foot", "screen", "tmux", "xterm-256color", or "" if
	// unknown. Diagnostic only.
	Terminal string

	// SSH is true when SSH_CONNECTION is in the environment (any
	// SSH-connected session, irrespective of which terminal the user
	// is on the other end of). The "Apple Terminal over SSH" disable
	// condition needs both SSH and TERM_PROGRAM=Apple_Terminal — see
	// DetectHyperlinkCaps.
	SSH bool
}

// TerminalAppMarker is the value TERM_PROGRAM carries on macOS Terminal.app.
// Apple Terminal does not implement OSC 8 hyperlinks at any version; the
// AGY 1.4.0 changelog row "Fixed raw OSC 8 escape sequences rendering in
// `Apple_Terminal` over SSH connections by disabling unsupported
// hyperlink sequences" is the cited evidence.
//
// AGY's strings table also ships the literal "appleTerminalSSH=" segment,
// pinning this exact mapping.
const terminalAppMarker = "Apple_Terminal"

// tmuxEnvVar is set when the program runs inside a tmux session. Together
// with the hyperlink-probe result it disambiguates "tmux passthrough
// works" from "tmux drops OSC 8 frames".
const tmuxEnvVar = "TMUX"

// sshEnvVar is set when the program is connected over SSH. SSH + Terminal.app
// is the specific case the changelog warns against; SSH + a modern terminal
// (kitty, ghostty, WezTerm, alacritty, iTerm2) still emits OSC 8 because
// the bytes flow from the terminal to the program and back unchanged.
const sshEnvVar = "SSH_CONNECTION"

// DetectHyperlinkCaps computes HyperlinkCaps from the env vars and the
// tmux hyperlink-passthrough probe result. Pure function: identical
// inputs produce identical outputs, no I/O, no clock reads.
//
// Detection table (highest-priority rule wins):
//
//	TERM_PROGRAM=Apple_Terminal                                  → OSC8 off
//	TERM=screen*                                                → OSC8 off
//	SSH_CONNECTION + TERM_PROGRAM=Apple_Terminal                → OSC8 off
//	                                                           AppleTerminalSSH true
//	TMUX set + tmuxHyperlinkPassthrough probe result == false   → OSC8 off
//	otherwise                                                   → OSC8 on
//
// For the OSC 8 OFF rows, the underlying terminal labels are still
// recorded in caps.Terminal so the debug log can print what was
// detected. Terminals that explicitly advertise OSC 8 support (kitty,
// ghostty, WezTerm, foot, iTerm2, konsole, alacritty, vscode)
// short-circuit the SSH check to "on" because their OSC 8
// implementation works over SSH — the changelog's narrow "Apple
// Terminal over SSH" case is the only SSH+unsupported-terminal
// intersection.
//
// args:
//
//	env               — os.Environ()-shaped slice ("KEY=VALUE" each),
//	                    or nil to fall back to the empty env. Nil is
//	                    supported so a default-construction call site
//	                    (e.g. tests) doesn't have to plumb env.
//	tmuxProbeResult   — true iff the tmux hyperlink-round-trip probe
//	                    returned the sentinel OSC 8 sequence unchanged
//	                    (only meaningful when TMUX is set in env);
//	                    false in any other case. The probe itself
//	                    lives in cmd/wagyu/tmux_probe.go and is out of
//	                    scope for this cycle — see "Hard constraints".
func DetectHyperlinkCaps(env []string, tmuxProbeResult bool) HyperlinkCaps {
	caps := HyperlinkCaps{}
	if tmuxProbeResult {
		caps.TmuxHyperlinkPassthrough = true
	}
	if len(env) == 0 {
		// No env: safest default. Don't emit OSC 8 because we can't
		// prove the terminal understands it. Plain-text fallback.
		return caps
	}

	termProgram := lookupEnvValue(env, "TERM_PROGRAM")
	term := lookupEnvValue(env, "TERM")
	tmux := lookupEnvValue(env, tmuxEnvVar)
	ssh := lookupEnvValue(env, sshEnvVar)
	if ssh != "" {
		caps.SSH = true
	}

	caps.Terminal = detectTerminalLabel(termProgram, term, tmux)
	caps.OSC8 = detectOSC8(termProgram, term, tmux, tmuxProbeResult, &caps)
	caps.OSC52 = detectOSC52(termProgram, term)

	return caps
}

func detectTerminalLabel(termProgram, term, tmux string) string {
	switch {
	case termProgram == terminalAppMarker:
		return "Apple_Terminal"
	case termProgram == "WezTerm":
		return "WezTerm"
	case termProgram == "kitty":
		return "kitty"
	case termProgram == "ghostty":
		return "ghostty"
	case termProgram == "iTerm.app", termProgram == "iTerm2":
		return "iTerm2"
	case termProgram == "vscode":
		return "vscode"
	case termProgram == "konsole":
		return "konsole"
	case termProgram == "alacritty":
		return "alacritty"
	case termProgram == "foot":
		return "foot"
	case tmux != "":
		return "tmux"
	case strings.HasPrefix(term, "screen"):
		return "screen"
	case term != "":
		return term
	default:
		return ""
	}
}

func detectOSC8(termProgram, term, tmux string, tmuxProbeResult bool, caps *HyperlinkCaps) bool {
	switch {
	case termProgram == terminalAppMarker:
		caps.AppleTerminalSSH = caps.SSH
		return false
	case strings.HasPrefix(term, "screen"):
		return false
	case tmux != "" && !tmuxProbeResult:
		if !caps.TmuxHyperlinkPassthrough {
			caps.TmuxHyperlinkPassthrough = false
		}
		return false
	default:
		return true
	}
}

func detectOSC52(termProgram, term string) bool {
	switch {
	case termProgram == terminalAppMarker, strings.HasPrefix(term, "screen"):
		return false
	default:
		return true
	}
}

// lookupEnvValue returns the value side of the first "KEY=VALUE" entry
// whose key matches `key`, or "" if no entry matches. Mirrors the
// shape of os.LookupEnv's return without the bool second value; the
// single-bool helper exists separately in cmd/wagyu/colorprofile_lc_terminal.go
// (it returns the bare string, like our own lookupEnvValue here).
//
// Kept package-private and duplicate-by-design rather than exported
// so the file compiles without an internal-env helper dependency
// that other modules (mdrender in particular) might collide with.
func lookupEnvValue(env []string, key string) string {
	prefix := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return e[len(prefix):]
		}
	}
	return ""
}
