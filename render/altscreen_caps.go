package render

import (
	"fmt"
	"os"
	"strings"
)

// AltScreenCaps reports whether the running terminal supports the alternate
// screen buffer and carries the metadata needed to format the /config
// Rendering Mode warning hint.
type AltScreenCaps struct {
	SupportsAltScreen bool
	IsRemoteSession   bool
	TermLabel         string
}

var unsupportedAltScreenTerms = map[string]bool{
	"dumb":   true,
	"linux":  true,
	"vt100":  true,
	"vt102":  true,
	"vt220":  true,
	"ansi":   true,
	"cons25": true,
	"emacs":  true,
	"cygwin": true,
}

// DetectAltScreenCapsFromEnv detects alternate-screen capabilities from the
// current process environment and platform console probe.
func DetectAltScreenCapsFromEnv() AltScreenCaps {
	return DetectAltScreenCaps(os.Environ(), probeWindowsConsoleVT)
}

// DetectAltScreenCaps computes AltScreenCaps from an os.Environ()-shaped slice
// and an optional Windows console VT probe callback.
func DetectAltScreenCaps(env []string, probeWindowsVT func() bool) AltScreenCaps {
	getenv := func(key string) string {
		return lookupEnvFold(env, key)
	}
	return AltScreenCaps{
		SupportsAltScreen: detectTermSupportsAltScreen(getenv, probeWindowsVT),
		IsRemoteSession:   detectRemoteSession(getenv),
		TermLabel:         detectTermLabel(getenv),
	}
}

// AltScreenHint returns the warning hint shown under /config Rendering Mode
// when the terminal does not support the alternate screen buffer, or "" when
// alt screen is supported.
func AltScreenHint(caps *AltScreenCaps) string {
	if caps == nil || caps.SupportsAltScreen {
		return ""
	}
	if caps.IsRemoteSession {
		return "⚠ alt screen unavailable — run ssh -t"
	}
	label := strings.TrimSpace(caps.TermLabel)
	if label == "" || strings.EqualFold(label, "unknown") {
		return "⚠ terminal does not support alt screen"
	}
	return fmt.Sprintf("⚠ terminal %q does not support alt screen", label)
}

func detectRemoteSession(getenv func(string) string) bool {
	return getenv("SSH_CLIENT") != "" ||
		getenv("SSH_TTY") != "" ||
		getenv("SSH_CONNECTION") != ""
}

func isLocalWindowsConsole(getenv func(string) string) bool {
	if !strings.EqualFold(getenv("OS"), "Windows_NT") {
		return false
	}
	if getenv("WSL_DISTRO_NAME") != "" || getenv("WSL_INTEROP") != "" {
		return false
	}
	if detectRemoteSession(getenv) {
		return false
	}
	return getenv("PSModulePath") != "" ||
		getenv("COMSPEC") != "" ||
		getenv("PROMPT") != ""
}

func isLocalAppleTerminal(getenv func(string) string) bool {
	return strings.EqualFold(strings.TrimSpace(getenv("TERM_PROGRAM")), "apple_terminal")
}

func getTerminalName(getenv func(string) string) string {
	if isLocalAppleTerminal(getenv) {
		return "apple_terminal"
	}
	if tp := strings.ToLower(getenv("TERM_PROGRAM")); tp != "" && tp != "tmux" && tp != "screen" {
		return tp
	}
	if strings.Contains(getenv("TERM"), "kitty") {
		return "kitty"
	}
	if getenv("VTE_VERSION") != "" {
		return "vte"
	}
	if getenv("WT_SESSION") != "" {
		return "windows_terminal"
	}
	if isLocalWindowsConsole(getenv) {
		return "conhost"
	}
	return "unknown"
}

func detectTermLabel(getenv func(string) string) string {
	if t := getenv("TERM"); t != "" {
		return t
	}
	name := getTerminalName(getenv)
	if name == "unknown" {
		return ""
	}
	return name
}

func detectTermSupportsAltScreen(getenv func(string) string, probeWindowsVT func() bool) bool {
	term := strings.ToLower(getenv("TERM"))
	if term == "" {
		if getenv("WT_SESSION") != "" || getenv("TERM_PROGRAM") != "" {
			return true
		}
		if probeWindowsVT != nil {
			return probeWindowsVT()
		}
		return false
	}
	return !unsupportedAltScreenTerms[term]
}

func lookupEnvFold(env []string, key string) string {
	for _, e := range env {
		k, v, ok := strings.Cut(e, "=")
		if ok && strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}
