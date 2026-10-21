// colorprofile_lc_terminal.go — v1.3.5 changelog gap: honor
// LC_TERMINAL=iTerm2 as a 24-bit color hint when COLORTERM is not
// forwarded (the SSH case described in the v1.3.5 changelog:
//
//	"Fixed colors being reduced to 256 colors in iTerm2 over SSH,
//	where COLORTERM is not forwarded; the CLI now uses full 24-bit
//	color when LC_TERMINAL=iTerm2 is set, unless COLORTERM or
//	NO_COLOR says otherwise."
//
// Upstream github.com/charmbracelet/colorprofile.Detect / Env only
// look at COLORTERM, TERM, tmux, terminfo, etc.; iTerm2's LC_TERMINAL
// hint is consulted nowhere in the library. resolveColorProfile
// below is the single place this policy lives — every code path that
// produces a color profile (lipgloss.Writer.Profile mutation in
// init(), tea.WithColorProfile in runLiveTUI) routes through it so
// the precedence is consistent.
//
// Precedence (top wins):
//
//  1. NO_COLOR (any truthy value) → ASCII (off). Hard opt-out; never
//     overridden.
//  2. COLORTERM=truecolor | 24bit | yes | true → TrueColor. Upstream
//     colorprofile already honours this; we keep its result.
//  3. LC_TERMINAL=iTerm2 → TrueColor upgrade. This is the new rule.
//  4. Otherwise upstream colorprofile.Env — ANSI, ANSI256, NoTTY, …
//     depending on TERM and friends.
package main

import (
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/colorprofile"
)

// resolveColorProfile returns the colour profile the CLI should use
// for the given environment. It is a thin, deterministic wrapper
// around the upstream colorprofile.Env plus the LC_TERMINAL upgrade
// rule above. env is parsed like os.Environ (each entry is
// "KEY=VALUE"); if it is nil or empty, os.Environ() is read.
func resolveColorProfile(env []string) colorprofile.Profile {
	if len(env) == 0 {
		env = os.Environ()
	}

	// Rule 1: NO_COLOR is a hard opt-out (https://no-color.org/). It
	// wins over every other rule, including LC_TERMINAL=iTerm2.
	if noColor, _ := strconv.ParseBool(lookupEnv(env, "NO_COLOR")); noColor {
		return colorprofile.ASCII
	}

	// Rule 2: delegate to upstream. If it already says TrueColor —
	// because COLORTERM=truecolor (etc.), TERM=alacritty/kitty/etc.,
	// tmux RGB, terminfo Tc/RGB, … — honour that and stop.
	base := colorprofile.Env(env)
	if base == colorprofile.TrueColor {
		return colorprofile.TrueColor
	}

	// Rule 3: iTerm2's LC_TERMINAL hint. iTerm2 sets LC_TERMINAL on
	// every shell it spawns, including shells launched over SSH where
	// COLORTERM is commonly dropped. Treat "iTerm2" (case-insensitive)
	// as a 24-bit upgrade.
	if strings.EqualFold(lookupEnv(env, "LC_TERMINAL"), "iTerm2") {
		return colorprofile.TrueColor
	}

	// Rule 4: fall back to whatever upstream decided (ANSI, ANSI256,
	// NoTTY, ASCII, …).
	return base
}

// lookupEnv returns the value of the first KEY=entry in env, or ""
// if the key is not set. Matches os.LookupEnv's "value, found"
// semantics on the value side; the caller uses strconv.ParseBool
// which already treats "" as "unset".
func lookupEnv(env []string, key string) string {
	prefix := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return e[len(prefix):]
		}
	}
	return ""
}
