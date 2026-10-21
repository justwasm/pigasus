// osc52_clipboard.go — package-simplification cycle 2: OSC 52
// clipboard writes for /copy.
//
// AGY 1.4.2 uses the OSC 52 escape sequence (`\x1b]52;c;<base64>\x07`)
// as the primary write path for /copy. The reference's `store.OSC
// 52Clipboard.WriteAll` (recovered at
// `google3/third_party/jetski/cli/store/store.OSC52Clipboard.WriteAll`)
// prefers the escape over the shell-out helper (atotto/clipboard)
// because OSC 52:
//
//   - works over SSH and Mosh — the client terminal interprets the
//     escape and writes its own clipboard, no agent-side child
//     process needed
//   - does not require xclip / xsel / wl-copy / pbcopy on PATH
//   - does not spawn a child, so the write is a single Write() call
//     (sub-millisecond) instead of a fork+exec+wait
//
// The reference's `detectOSC52Support` (`0x8aac460`) is a coarse
// allow-list built from TERM_PROGRAM, LC_TERMINAL, TERM, and the
// SSH_CONNECTION / SSH_CLIENT / MOSH_CONNECTION session markers. We
// layer the same gate in SupportsOSC52 below and fall through to the
// shell-out helper (`copyToClipboardShellOut` — the existing pbcopy /
// xclip / xsel / wl-copy path cycle 103 wired up) for terminals
// that don't pass OSC 52 (Apple Terminal, GNU screen, plain xterm,
// Mosh without remote hint).
//
// The OSC 52 write is emitted to stderr, not stdout. bubbletea's TUI
// owns stdout in alt-screen mode; writing raw escapes there can
// corrupt the frame in flight. stderr shares the same TTY and is
// outside bubbletea's render loop, so the terminal interprets the
// OSC 52 without disturbing the TUI's next repaint. This matches
// PiG's coding-agent clipboard helper (clipboard_copy.go:emitOSC52)
// in spirit, which writes to its stdout but the bytes land in the
// TTY the same way.
package main

import (
	"io"
	"os"
	"strings"

	"github.com/aymanbagabas/go-osc52/v2"
)

// SupportsOSC52 reports whether the host terminal can interpret an
// OSC 52 escape. The check is a coarse allow-list built from
// TERM_PROGRAM, LC_TERMINAL, TERM, and the SSH/Mosh session
// markers; anything we don't recognise is treated as "supported"
// because most modern terminals do, and the shell-out fallback
// works regardless of whether the OSC write succeeded.
//
// Disabling terminals (return false):
//
//   - Apple Terminal  (TERM_PROGRAM=Apple_Terminal) — does not pass OSC 52
//   - GNU screen      (TERM=screen*)                 — strips by default
//   - Plain xterm     (TERM=xterm, TERM=xterm-mono)  — original xterm, no OSC 52
//   - Mosh            (MOSH_CONNECTION set)          — does not propagate OSC 52
//   - VTxxx / dumb    (TERM=vt100, vt220, dumb)      — no OSC 52
//   - unset TERM      (cron, script capture)         — no TTY
//
// Enabled terminals (return true):
//
//   - SSH / Mosh sessions (the client terminal interprets the escape;
//     OSC 52 is the only viable write path on a remote connection)
//   - kitty / ghostty / WezTerm / iTerm2 / VS Code / Warp
//     (TERM_PROGRAM match)
//   - iTerm2 via LC_TERMINAL=iTerm2 (no TERM_PROGRAM — rare)
//   - any modern xterm-256color / alacritty / foot / konsole /
//     mlterm / st / rxvt-unicode (TERM glob match)
//   - everything else (default-yes)
//
// The matching is conservative: an unknown terminal is treated as
// "supported" because (a) the OSC 52 write is cheap and harmless
// when the terminal ignores it, and (b) false positives degrade
// gracefully — the fallback shell-out only runs when SupportsOSC52
// returns false, so an over-eager true means the user gets OSC 52
// instead of xclip, not an outright failure.
func SupportsOSC52() bool {
	return supportsOSC52With(os.Getenv)
}

// supportsOSC52With is the env-lookup form of SupportsOSC52, factored
// out so tests can drive the gate with a fake env table without
// touching the host shell. Same logic; just takes a getenv func.
func supportsOSC52With(getenv func(string) string) bool {
	// SSH / Mosh: the client terminal — not the server-side
	// terminal — interprets the escape. Always emit OSC 52 so
	// the client receives it. This matches PiG's
	// coding-agent clipboard helper (clipboard_copy.go:copy),
	// which sets osc52Emitted=true on SSH_CONNECTION without
	// first consulting TERM_PROGRAM.
	if getenv("SSH_CONNECTION") != "" || getenv("SSH_CLIENT") != "" || getenv("SSH_TTY") != "" {
		return true
	}
	if getenv("MOSH_CONNECTION") != "" {
		return true
	}

	// TERM_PROGRAM is set by most modern macOS / cross-platform
	// terminals. Check it first because it's the most specific.
	// An unknown TERM_PROGRAM (not in the allow-list below) is
	// still treated as "supported" — the existence of TERM_PROGRAM
	// implies a real terminal that speaks OSC 52, even if it's not
	// one we've explicitly named.
	if termProgram := strings.ToLower(getenv("TERM_PROGRAM")); termProgram != "" {
		switch termProgram {
		case "apple_terminal":
			return false
		case "iterm.app", "wezterm", "ghostty", "kitty", "vscode", "warp", "alacritty":
			return true
		default:
			return true
		}
	}

	// LC_TERMINAL=iTerm2 is iTerm2's self-identify env (no
	// TERM_PROGRAM set in some iTerm2 builds). Match case-insensitively
	// — shells sometimes lowercase the value when forwarding.
	if strings.ToLower(getenv("LC_TERMINAL")) == "iterm2" {
		return true
	}

	// TERM env is set by every TTY. Coarse-grained fallback for
	// terminals that don't set TERM_PROGRAM.
	term := strings.ToLower(getenv("TERM"))
	switch term {
	case "":
		// No TERM at all — running under cron / script capture /
		// pipe; the output isn't a TTY. Don't emit OSC 52.
		return false
	case "screen", "screen-256color", "screen.xterm-256color":
		// GNU screen strips OSC 52 by default; tmux forwards it
		// only with `set-clipboard on`. The clone is conservative:
		// disable rather than rely on a tmux config we can't read.
		return false
	case "xterm", "xterm-mono":
		// The original xterm and the mono build don't implement
		// OSC 52. xterm-256color and xterm-kitty DO — those go
		// through the default-yes path below.
		return false
	case "vt100", "vt220", "dumb":
		// Real VTxxx and the "dumb" pseudo-terminal don't speak
		// OSC 52.
		return false
	}

	// Anything else (alacritty, foot, konsole, mlterm, rxvt-unicode,
	// st, Eterm, etc.) — assume supported.
	return true
}

// osc52WriteText writes text to the host terminal's system
// clipboard via the OSC 52 escape sequence. Returns true on a
// successful Write, false when the write itself failed (write to a
// closed pipe, ENOSPC, the input was empty, or the input exceeded
// the per-terminal OSC sequence cap). The escape is the standard
// `OSC 52 ; c ; <base64> BEL` form; terminals that don't speak it
// silently drop the bytes.
//
// go-osc52 handles the base64 encoding and the mode escape (tmux /
// screen wrappers) when configured; we use DefaultMode because the
// SSH_CONNECTION gate handles tmux passthrough on the assumption
// that the user has `set-clipboard on`.
//
// Limit(100000) caps the raw string length — go-osc52 truncates
// the base64 output to "no bytes" when the input exceeds the
// limit, which would silently look like success on the Write call.
// We pre-check the length here so callers get an honest false and
// fall back to the shell-out path. The 100 KB cap matches PiG's
// coding-agent clipboard helper
// (clipboard_copy.go:maxOSC52EncodedLength).
func osc52WriteText(w io.Writer, text string) bool {
	if text == "" {
		return false
	}
	if len(text) > 100000 {
		return false
	}
	seq := osc52.New(text).Limit(100000)
	_, err := seq.WriteTo(w)
	return err == nil
}