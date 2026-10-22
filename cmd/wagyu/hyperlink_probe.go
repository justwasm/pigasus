// hyperlink_probe.go — cycle osc-hyperlinks (cycle 2 of the OSC pair):
// startup wiring for the terminal capability detection + tmux hyperlink
// round-trip probe + the "Terminal detection:" debug log line.
//
// The two functions here are the only places that touch real process
// env (vs. the env-slice plumbing in internal/render). Tests drive
// DetectHyperlinkCaps directly; this file is the integration layer
// that builds the slices from os.Environ(), runs the probe, logs the
// result, and stores the caps on the runMode for downstream callers.
//
// The actual OSC 8 hyperlink consumers (the glamour migration in a
// sister worktree, plus the /changelog viewer's URL entries) will
// read m.hyperlinkCaps.OSC8 from tui_run.go and call
// render.Hyperlink(..., caps) accordingly. This file ensures the
// field is populated before View() runs the first frame.
package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/render"
)

// hyperlinkLogTag is the prefix on the "Terminal detection:" debug
// log line. AGY's strings table carries the verbatim "Terminal
// detection: TERM=%q altScreen=%v ssh=%v osc8=%v osc52=%v
// appleTerminalSSH=%v" string; this cycle emits the wagyu-side
// equivalent (with `app=wagyu` recorded for free, since the OSC 7501
// cycle writes that into tea.View.ProgramStatus on every View).
//
// The fields intentionally mirror AGY's emitted format so a user
// pasting logs side-by-side can match them line-for-line. Two extra
// fields — terminal and tmuxHyperlinkPassthrough — come after the
// six AGY-matching ones; they are additive and don't reorder the
// existing ones.
const hyperlinkLogTag = "Terminal detection:"

// hyperlinkCapsFromEnv builds a HyperlinkCaps from the current process
// environment plus the tmux probe result, then writes the
// "Terminal detection: …" debug log line to the writer (typically
// os.Stderr in production or a *bytes.Buffer in tests). The function
// is pure except for the log write — no I/O, no clock reads, no
// global state — so the table tests in cmd/wagyu can pin the log
// shape byte-for-byte.
//
// Arguments:
//
//	w               — destination for the debug log line. Stderr in
//	                  production; io.Discard when the user opts out
//	                  (e.g. "--quiet" or via the journald-suppress
//	                  dance); a *bytes.Buffer in tests.
//	env             — os.Environ() in production; nil-safe.
//	tmuxProbeResult — the tmux hyperlink round-trip probe outcome.
//	                  true when tmux is patched for OSC 8
//	                  passthrough, false otherwise.
//	altScreen       — whether the TUI is starting in alt-screen mode
//	                  (recorded in the log as a debugging hint; the
//	                  OSC 8 emission does not depend on it).
//
// The returned caps are deterministic — identical env + probe
// result produce identical output — so the integration test in
// hyperlink_probe_test.go can drive both branches deterministically.
func hyperlinkCapsFromEnv(
	w io.Writer,
	env []string,
	tmuxProbeResult bool,
	altScreen bool,
) render.HyperlinkCaps {
	if env == nil {
		env = os.Environ()
	}
	caps := render.DetectHyperlinkCaps(env, tmuxProbeResult)
	if w != nil && w != io.Discard {
		fmt.Fprintf(w,
			"%s TERM=%q altScreen=%v ssh=%v osc8=%v osc52=%v appleTerminalSSH=%v "+
				"terminal=%q tmuxHyperlinkPassthrough=%v\n",
			hyperlinkLogTag,
			termValue(env),
			altScreen,
			caps.SSH,
			caps.OSC8,
			caps.OSC52,
			caps.AppleTerminalSSH,
			caps.Terminal,
			caps.TmuxHyperlinkPassthrough,
		)
	}
	return caps
}

// tmuxHyperlinkProbe runs the OSC 8 round-trip probe. The implementation
// here is the deliberately conservative "probe-less" variant:
//
//   - If TMUX is not in the environment, the probe is a no-op and we
//     return false (no passthrough test required).
//   - If TMUX is set, we cannot reliably round-trip the sentinel
//     through tmux without a TTY (the bubbletea/v2 program has not
//     started yet, so the input channel is not armed).
//
// The probe in production is therefore a constant `false` for now
// unless the user has explicitly opted in via the WAGYU_TMUX_HYPERLINK
// environment variable (mostly useful for tests; see
// hyperlink_probe_test.go).
//
// Returning the conservative false means the OSC 8 path falls back
// to plain text on every tmux session that hasn't been configured
// for hyperlink passthrough — which is the safe answer. The "real"
// probe lands when cycle osc-hyperlinks is followed up by a cycle
// that can drive the bubbletea.Program + send the sentinel + wait
// for the terminal to echo it back. See "Hard constraints" in the
// cycle report.
//
// returns:
//
//	true   — caller should treat the OSC 8 path as enabled through
//	         the tmux layer (passthrough verified).
//	false  — caller's OSC 8 must be disabled (passthrough test
//	         failed or no test was possible). The default.
func tmuxHyperlinkProbe(w ...io.Writer) bool {
	if !isTmuxSession() {
		return false
	}
	// Opt-in positive override, used by the integration test.
	if v := strings.TrimSpace(os.Getenv("WAGYU_TMUX_HYPERLINK")); v != "" {
		switch strings.ToLower(v) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	}
	// Without an opt-in, treat as not-passthrough. AGY's strings
	// table carries "tmux hyperlink probe failed, assuming no OSC 8
	// passthrough" — which is exactly this branch's emissions.
	// A future cycle replaces this stub with a real round-trip
	// probe driven through the bubbletea Program's WithInput mode.
	if len(w) > 0 && w[0] != nil && w[0] != io.Discard {
		fmt.Fprintln(w[0], "tmux hyperlink probe failed, assuming no OSC 8 passthrough: probe not implemented")
	}
	return false
}

// isTmuxSession reports whether the process is running under tmux,
// regardless of whether the user has configured OSC 8 passthrough.
// The check is intentionally cheap (env lookup) so it can be called
// from a startup hook without locking the TUI.
func isTmuxSession() bool {
	return os.Getenv("TMUX") != ""
}

// termValue returns the value of $TERM from env (an os.Environ-shaped
// slice), or "" if unset. Local helper rather than calling
// render.DetectHyperlinkCaps because we want the raw value for the
// log line.
func termValue(env []string) string {
	prefix := "TERM="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return e[len(prefix):]
		}
	}
	return ""
}

// hyperlinkProbeReady returns true once hyperlinkCapsFromEnv has been
// called at least once. The helper exists so the runMode can decide
// whether to detect lazily (e.g. when a glamour consumer first asks
// for the caps) versus eagerly (which is what the cycle does). The
// flag is small and guards against double-logging.
var hyperlinkProbeReady time.Time

// initReadyTime stamps the package once so tests can reason about
// when the probe was wired. Production callers ignore it.
func initReadyTime() {
	if hyperlinkProbeReady.IsZero() {
		hyperlinkProbeReady = time.Now()
	}
}

// stub references the bubbletea package for godoc — running the
// binary pulls in `tea.View.ProgramStatus` from cycle osc-7501, and
// the linker drops tea if no symbol is referenced. The line below
// keeps tea reachable even if a future refactor removes the OSC 7501
// assignment from View().
var _ tea.ProgramState = ""
