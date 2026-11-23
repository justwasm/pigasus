// Package graphic provides terminal inline graphics capability detection,
// Kitty Graphics Protocol and iTerm2 OSC 1337 encoders, and 24-bit ANSI
// half-block fallback rendering.
package graphic

import (
	"fmt"
	"os"
	"strings"
)

// Protocol identifies the inline graphics protocol supported by a terminal.
type Protocol int

const (
	// ProtocolNone indicates no native inline image protocol; callers should
	// fall back to Unicode half-block / ASCII rendering.
	ProtocolNone Protocol = iota
	// ProtocolKitty indicates the Kitty Graphics Protocol (\x1b_G...\x1b\\).
	ProtocolKitty
	// ProtocolITerm2 indicates iTerm2 Inline Images (OSC 1337).
	ProtocolITerm2
)

// String returns the canonical lowercase name of the protocol.
func (p Protocol) String() string {
	switch p {
	case ProtocolKitty:
		return "kitty"
	case ProtocolITerm2:
		return "iterm2"
	default:
		return "none"
	}
}

// Capabilities describes the detected terminal inline graphics capabilities.
type Capabilities struct {
	Protocol      Protocol
	Forced        bool
	InMultiplexer bool
	TermProgram   string
	Term          string
}

// DetectEnv inspects the current process environment using os.Getenv.
func DetectEnv() Capabilities {
	return Detect(os.Getenv)
}

// Detect determines terminal inline graphics capabilities from environment
// variables provided by getenv.
//
// Precedence:
//  1. CLI_GRAPHICS override ("kitty", "iterm2", "none"/"off"/"ascii").
//  2. Multiplexer guard (TMUX, STY, ZELLIJ, or TERM starting with screen/tmux)
//     suppresses native graphics unless CLI_GRAPHICS explicitly overrides.
//  3. Explicit non-Kitty TERM_PROGRAM exclusions (vscode, WezTerm, Apple_Terminal).
//  4. Known terminal identifiers (iTerm.app -> ProtocolITerm2;
//     ghostty/GHOSTTY_RESOURCES_DIR/xterm-ghostty and
//     kitty/KITTY_WINDOW_ID/xterm-kitty -> ProtocolKitty).
func Detect(getenv func(string) string) Capabilities {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}

	termProgram := strings.TrimSpace(getenv("TERM_PROGRAM"))
	term := strings.TrimSpace(getenv("TERM"))
	inMux := isMultiplexer(getenv, term)

	caps := Capabilities{
		Protocol:      ProtocolNone,
		InMultiplexer: inMux,
		TermProgram:   termProgram,
		Term:          term,
	}

	// 1. Explicit CLI_GRAPHICS override takes highest precedence.
	switch strings.ToLower(strings.TrimSpace(getenv("CLI_GRAPHICS"))) {
	case "kitty":
		caps.Protocol = ProtocolKitty
		caps.Forced = true
		return caps
	case "iterm2":
		caps.Protocol = ProtocolITerm2
		caps.Forced = true
		return caps
	case "none", "off", "ascii":
		caps.Protocol = ProtocolNone
		caps.Forced = true
		return caps
	}

	// 2. Multiplexer safety guard: never emit or probe native graphics inside
	// tmux, GNU screen, or zellij unless explicitly forced via CLI_GRAPHICS.
	if inMux {
		return caps
	}

	// 3. Known terminals that must NOT be assumed to support Kitty graphics.
	switch strings.ToLower(termProgram) {
	case "vscode", "wezterm", "apple_terminal":
		return caps
	case "iterm.app":
		caps.Protocol = ProtocolITerm2
		return caps
	case "ghostty", "kitty":
		caps.Protocol = ProtocolKitty
		return caps
	}

	// 4. Environment markers for Kitty and Ghostty.
	termLower := strings.ToLower(term)
	if strings.TrimSpace(getenv("KITTY_WINDOW_ID")) != "" ||
		termLower == "xterm-kitty" ||
		strings.HasPrefix(termLower, "xterm-kitty-") {
		caps.Protocol = ProtocolKitty
		return caps
	}
	if strings.TrimSpace(getenv("GHOSTTY_RESOURCES_DIR")) != "" ||
		termLower == "xterm-ghostty" ||
		strings.HasPrefix(termLower, "xterm-ghostty-") {
		caps.Protocol = ProtocolKitty
		return caps
	}

	return caps
}

func isMultiplexer(getenv func(string) string, term string) bool {
	if strings.TrimSpace(getenv("TMUX")) != "" ||
		strings.TrimSpace(getenv("STY")) != "" ||
		strings.TrimSpace(getenv("ZELLIJ")) != "" {
		return true
	}
	termLower := strings.ToLower(term)
	return strings.HasPrefix(termLower, "screen") || strings.HasPrefix(termLower, "tmux")
}

// ShouldProbe reports whether it is safe to send a Kitty graphics query
// sequence to the terminal. It returns false inside multiplexers (unless
// CLI_GRAPHICS=kitty explicitly forces Kitty mode) and false for terminals
// known not to support Kitty graphics (e.g. WezTerm, VS Code, Apple Terminal,
// iTerm2).
func ShouldProbe(getenv func(string) string) bool {
	caps := Detect(getenv)
	if caps.InMultiplexer && !(caps.Forced && caps.Protocol == ProtocolKitty) {
		return false
	}
	return caps.Protocol == ProtocolKitty
}

// KittyProbeSequence builds the safe 1x1 RGBA Kitty graphics query sequence
// for the given image ID when ShouldProbe(os.Getenv) is true. If probing is
// unsafe (e.g. inside tmux/screen/zellij or on an unsupported terminal), it
// returns an empty string.
func KittyProbeSequence(id uint32) string {
	return KittyProbeSequenceWithEnv(id, os.Getenv)
}

// KittyProbeSequenceWithEnv builds the safe 1x1 RGBA Kitty graphics query
// sequence using the supplied getenv function.
func KittyProbeSequenceWithEnv(id uint32, getenv func(string) string) string {
	if !ShouldProbe(getenv) {
		return ""
	}
	return fmt.Sprintf("\x1b_Ga=q,i=%d,s=1,v=1,f=32,t=d;AAAA\x1b\\", id)
}
