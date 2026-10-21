// slash_narration.go — cycle 250: the /narration slash command.
//
// AGY 1.4.2 feature-gap row (internal/cli/testdata/feature_diff_table.json):
//
//	"Added a narration start helper that prints an introductory line so
//	 the agent can begin a multi-step task with consistent context."
//
// The clone closes the gap as a TUI-only cosmetic slash command.
// The reference's actual StartNarration gRPC method (recovered
// indirectly via docs/decompiled-v1.3.4/strings-v1.3.4.txt:
// "narration is not available in external builds") is a backend RPC
// the clone does not implement; this cycle ships a local helper
// that selects a short intro paragraph and writes it to m.response.
//
// What /narration does:
//
//   - /narration (no args)          → catalogue default (time-deterministic)
//   - /narration <text>             → verbatim user-supplied text
//   - /narration off                → mute: write nothing, mark session flag
//   - /narration auto [taskType]    → catalogue pick driven by session
//     start time and (optionally) the
//     user-supplied taskType argument
//
// print mode (-p) is unaffected: this code path is wired only
// through handleSlashCommand in cmd/wagyu/slash_commands.go.
// The print path runs PiG's coding.Runtime and never reaches this
// handler.
//
// Crucially, narration does NOT enter m.session. The model
// history stays clean so a follow-up turn sees only what the user
// typed; the intro paragraph is visual feedback only. The test
// TestSlashNarration_DoesNotAffectSessionHistory pins this.
package main

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/cli"
)

// narrationSessionStartFallback is the session start time used when
// the runMode does not yet have one (older callers, direct-test
// invocations before NewRunMode has had a chance to seed it). The
// zero value is a stable choice — the FNV hash still produces a
// deterministic index off of it.
func narrationSessionStart(m *runMode) time.Time {
	if !m.narrationStart.IsZero() {
		return m.narrationStart
	}
	// m.narrationStart is seeded in NewRunMode at construction
	// time. Tests that drive runMode via direct construction (no
	// NewRunMode) fall back to a fixed seed so the catalogue pick
	// stays pinned; the test wraps that fallback rather than
	// depending on the constant.
	return time.Time{}
}

// narrationFeedback is the pure helper that resolves "/narration <args>"
// into a paragraph and a session-state delta. The slash dispatch in
// slash_commands.go calls it and writes the paragraph + a short
// feedback row to m.response.
//
// Returns:
//
//   - text        — the paragraph to echo to the transcript ("" for the
//     off branch and for the "we already muted" branch).
//   - echo        — the user-visible feedback string (e.g. "Narration
//     disabled." or the catalogue paragraph itself when
//     the user gave no echo override).
//   - mutedNow    — true when the call flipped m.narrationOff on;
//     false otherwise (already-muted calls count as
//     "already muted").
//   - already     — true when m.narrationOff was already set; lets the
//     caller avoid appending a redundant "already disabled"
//     row in the future. Today we only branch on
//     mutedNow — already- is reserved for a follow-up
//     cycle that may want to show "already disabled."
//
// The function is pure: same input → same output, no I/O. The runMode
// is passed by pointer so the session flag (m.narrationOff) can be
// toggled, but no other state is touched — narration MUST NOT
// append to m.session.
func narrationFeedback(m *runMode, args string) (text, echo string, mutedNow, already bool) {
	args = strings.TrimSpace(args)

	// First, the persistent-mute branch: /narration off, or any
	// subsequent invocation after m.narrationOff was already set
	// in this session. The session flag is sticky for the rest of
	// the TUI session, mirroring the v1.4.2 reference's behaviour
	// where /narration off disables the helper until the user
	// toggles it back on (cycle 250 ships only the off side; a
	// future cycle may add a /narration on form).
	switch args {
	case "off", "OFF", "Off":
		if m.narrationOff {
			return "", "Narration already disabled.", false, true
		}
		m.narrationOff = true
		return "", "Narration disabled.", true, false
	}

	// Already-muted session: every subsequent /narration is a
	// no-op, matching the off branch's UX. We deliberately do not
	// emit a "Narration is disabled" message every time because
	// that would spam the transcript — the user said off once.
	if m.narrationOff {
		return "", "", false, true
	}

	// Resolve the paragraph through the cycle-250 catalogue.
	sessionStart := narrationSessionStart(m)
	switch {
	case args == "":
		// /narration with no args → catalogue default.
		text = cli.SelectNarration("", "default", "", sessionStart)
		echo = text
	case args == "auto":
		// /narration auto → catalogue driven by session start only.
		text = cli.SelectNarration("", "auto", "", sessionStart)
		echo = text
	case strings.HasPrefix(args, "auto "):
		// /narration auto <taskType> → catalogue driven by
		// session start AND the user-supplied task type.
		task := strings.TrimSpace(args[len("auto "):])
		text = cli.SelectNarration("", "auto", task, sessionStart)
		echo = text
	default:
		// /narration <free text> → verbatim.
		text = cli.SelectNarration(args, "", "", sessionStart)
		echo = text
	}
	return text, echo, false, false
}

// narrationDisabledMsg is the user-visible feedback for the
// "narration already disabled" path. Exported via a constant so
// the cycle-250 deliverable can pin the wording once.
const narrationDisabledMsg = "Narration disabled."

// narrationAlreadyDisabledMsg is the cycle-250 wording for the
// "narration is already off" branch.
const narrationAlreadyDisabledMsg = "Narration already disabled."

// narrationSampleSeeds is the list of cycle-250 test seeds. Two
// consecutive calls inside a single test use one of these so the
// same-input guarantee is exercised against a real, non-zero
// timestamp. The chosen value matches the cycle-250 deliverable's
// evidence section.
var narrationSampleSeeds = []time.Time{
	time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC),
	time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	time.Date(2024, 6, 15, 9, 30, 0, 0, time.UTC),
}

// narrationEchoToResponse writes echo into m.response as a single
// row, exactly like /btw and /context. The echo is plain text —
// no "⎿" wrapper, no slash command banner — because /narration
// is a cosmetic helper, not a content command.
//
// The function is a small inline helper (rather than reusing
// slashRespond) because slashRespond emits the
// `> /<cmd>` + `⎿` wrapper that every content-producing slash
// command uses; /narration wants a single styled row without any
// of that chrome.
func narrationEchoToResponse(m *runMode, echo string) {
	if echo == "" {
		return
	}
	m.response = append(m.response, panelGray238+echo+panelGray238C)
	// Trailing blank matches the cycle-195 echo convention so the
	// next rendered row sits one line down.
	m.response = append(m.response, "")
}

// handleNarrationSlash is the body the slash dispatch invokes.
// It mirrors the shape of runBtwSideQuestion / runIsolatedSlash —
// the handler is the bridge between the slash router and the
// pure narrationFeedback helper.
//
// Print mode (-p) does not reach this function: the slash
// command body is wired only through handleSlashCommand in the
// TUI path. Print mode's prompt dispatch (cmd/wagyu/print_slash.go)
// handles /narration separately — currently as a no-op, on the
// same "print mode is unaffected" principle documented at the
// top of this file.
//
// The off branch is intentionally silent — /narration off flips
// the session flag and writes NOTHING to m.response. The
// catalogue / verbatim / auto branches DO write a single styled
// paragraph row plus a trailing blank.
func (m *runMode) handleNarrationSlash(args string) (bool, tea.Cmd) {
	text, _, mutedNow, already := narrationFeedback(m, args)
	// The off branch is silent: mutedNow (the call flipped the
	// flag) and already (the flag was already on) both mean
	// "no transcript output". Pin: /narration off writes
	// nothing — TestSlashNarration_Off enforces this.
	if mutedNow || already {
		return true, nil
	}
	if text != "" {
		// Verbatim or catalogue paragraph: append as plain text
		// row(s). The catalogue's paragraphs are single-line, but
		// a user-supplied line could in principle contain "\n"
		// (the TUI does not strip user input). We split on "\n"
		// so multi-line input renders as multiple rows but never
		// contains the slash echo chrome.
		for _, ln := range strings.Split(text, "\n") {
			m.response = append(m.response, panelGray238+ln+panelGray238C)
		}
		// Trailing blank to keep rows from sticking together.
		m.response = append(m.response, "")
	}
	return true, nil
}

// _ keeps the unused-variable tripwire: if a future refactor drops
// narrationSampleSeeds the test surface still compiles cleanly.
// The variable is intentionally exported via a side effect (no
// callers in production code today; tests in narration_test.go
// reference it directly).
var _ = narrationSampleSeeds
