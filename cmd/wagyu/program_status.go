// program_status.go — cycle osc-7501: report wagyu's state to terminals that
// support the Program Status Protocol (OSC 7501); cycle 239 adds the
// AGY 1.4.0 environment variables that gate the report.
//
// The reference (AGY 1.4.0 changelog) ships terminal status reporting over
// OSC 7501: terminals like kitty, ghostty, WezTerm ≥ 2024 and foot display a
// "Working…/Blocked/Done/Error" indicator in their tab or status line. The
// clone did the same as all prior cycles: emitted no such report, so users in
// those terminals lost the "the model is still thinking" cue.
//
// This file mirrors crush's internal/ui/model/program_status.go (the
// reference layout in this repo: `/Users/gear/Documents/GitHub/crush/internal/
// ui/model/program_status.go`). The mapping is intentionally small:
//
//	state mapping            crush                 wagyu
//	─────────────            ─────                 ─────
//	permission blocked       dialog.PermissionsID  m.pendingPermission (test
//	                                              seam; the wagyu 1:1
//	                                              permissionRunner soft-denies
//	                                              without a dialog today)
//	question blocked         hasPendingQuestion    m.hasPendingQuestion
//	                                              (subAgentQuestionPickerOpen
//	                                              || subAgentQuestionAnswerTarget)
//	auth blocked             dialog.AWSSSOID       m.pendingAuth (test seam;
//	                                              wagyu today logs the user's
//	                                              existing token out via
//	                                              /logout, not a modal)
//	working                  agentBusyCache        m.isAgentBusy
//	                                              (streaming || turnCancel ||
//	                                              thinking)
//	done / error from turn   turnOutcome           m.lastTurnOutcome, set by
//	                                              recordTurnOutcome() on
//	                                              turnDoneMsg / error routes
//	idle (default)           (default)             (default)
//
// Cycle 239 — AGY 1.4.0 release-note parity:
//   - AGY_CLI_OSC7501=off        — suppress the report entirely (return
//     nil from programStatus so the runtime emits no OSC 7501 sequence).
//     Highest-priority gate: even when the terminal supports OSC 7501,
//     =off wins. Accepts any case-insensitive match of off / false /
//     0 / no after whitespace trim.
//   - AGY_CLI_OSC7501=on         — fall through the existing emit path.
//     In the AGY binary this env var flips an internal capability gate
//     that suppresses OSC 7501 when the startup probe (the query the
//     terminal answers with ProgramStatusSupportMsg in bubbletea/v2)
//     comes back negative. The clone does NOT gate on that probe —
//     bubbletea/v2's cursed_renderer writes SetProgramStatus on every
//     frame where v.ProgramStatus != nil regardless of capability —
//     so the clone is already at "always emit" and =on is satisfied
//     for free. We parse and accept the value so users on terminals
//     that do answer the check but want to retry with stronger force
//     don't see a syntax error, and so the env var is mirrored
//     exactly. Documented under 硬约束 in cycle-239's deliverable.
//   - AGY_CLI_OSC7501_MESSAGES=brief — strip the human-readable message
//     from Blocked-state reports (the message field is where AGY would
//     surface "running /foo/bar — allow?", the pending question
//     summary, or "re-auth required for project foo"). Working
//     and Error states are not stripped: Working has no message
//     in the wagyu implementation anyway, and Error states carry
//     actionable context the user must see (Error and Done are the
//     information-bearers of the protocol; brief suppresses detail,
//     not state). Accepts any case-insensitive match of "brief".
//
// Two test-only seams — `pendingPermission` and `pendingAuth` — give the test
// file a way to drive the Blocked branches without inventing modal dialogs
// the production code does not ship. See program_status_test.go.
//
// Three methods — programStatus, hasPendingQuestion, isAgentBusy — and two
// hook helpers — recordTurnOutcome, clearLastTurnOutcome — are exported only
// within the `main` package. The matching field declarations live on runMode
// in tui_run.go; they are intentionally kept next to the streaming and turn-
// cancel fields because that is where their lifecycle hooks fire.
package main

import (
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// programStatusApp is the stable program name reported to the terminal.
//
// The Program Status Protocol limits App to [A-Za-z0-9_.+-]{1,32}; "wagyu"
// matches and is reused across every report so the terminal can identify the
// record owner.
const programStatusApp = "wagyu"

// Cycle 239 — env-var names lifted verbatim from the AGY 1.4.0 changelog.
// Kept as named constants so the test file and the production reader can't
// drift on the spelling.
const (
	envOSC7501         = "AGY_CLI_OSC7501"
	envOSC7501Messages = "AGY_CLI_OSC7501_MESSAGES"
)

// envOSC7501Off reports whether AGY_CLI_OSC7501 is set to a falsy value
// (off / false / 0 / no, case-insensitive, whitespace-trimmed). This is
// the highest-priority gate for the OSC 7501 report: returning true here
// suppresses the report entirely, even when the terminal supports
// OSC 7501 and the rest of programStatus() would otherwise emit.
//
// An unset / unknown value returns false — the env var is opt-out only.
// Matches the falsy set used elsewhere in the codebase (see e.g.
// WAGYU_TMUX_HYPERLINK in hyperlink_probe.go:132-134).
func envOSC7501Off() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(envOSC7501)))
	switch v {
	case "off", "false", "0", "no":
		return true
	}
	return false
}

// envOSC7501On reports whether AGY_CLI_OSC7501 is set to a truthy value
// (on / true / 1 / yes, case-insensitive, whitespace-trimmed). The
// function exists so future callers can branch on the user's explicit
// opt-in; the current implementation does not (see the package doc
// comment and cycle-239's 硬约束 for why this is correctly a no-op
// against bubbletea/v2's behavior).
func envOSC7501On() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(envOSC7501)))
	switch v {
	case "on", "true", "1", "yes":
		return true
	}
	return false
}

// envOSC7501Brief reports whether AGY_CLI_OSC7501_MESSAGES=brief.
// Case-insensitive, whitespace-trimmed; unknown values are treated as
// "not brief" so misconfiguration degrades to "show full message".
func envOSC7501Brief() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(envOSC7501Messages)))
	return v == "brief"
}

// programStatus reports Wagyu's current state to terminals that support the
// Program Status Protocol. Terminals that don't implement OSC 7501 (or the
// subset that listens for state reports) simply ignore the report.
//
// Returns nil when AGY_CLI_OSC7501 is set to a falsy value (off / false /
// 0 / no). A nil return is honoured by bubbletea/v2's renderer
// (cursed_renderer.go:159, 868) — the writer skips emitting OSC 7501
// for a frame whose ProgramStatus is nil. This is how =off actually
// suppresses emission; everything else stays unchanged.
//
// State precedence, highest → lowest:
//
//  1. Blocked / Permission — test-only seam (pendingPermission). Mirrors
//     crush's dialog.PermissionsID branch; the wagyu clone's permissionRunner
//     soft-denies in print mode today and the TUI has no interactive
//     permission dialog, so production code never trips this branch — but
//     the seam is in place so a future cycle can flip it without changing
//     this method's precedence.
//  2. Blocked / Question — hasPendingQuestion() returns true while the
//     sub-agent question picker is open OR the answer textarea is focused
//     on a queued question. Mirrors crush's hasPendingQuestion.
//  3. Blocked / Auth — test-only seam (pendingAuth). Same shape as the
//     permission seam; production code does not block on auth prompts in
//     this release because wagyu routes sign-in through the headless
//     /logout and keyring replace flow, not a TUI modal.
//  4. Working — isAgentBusy() is true while a model turn is in flight
//     (streaming, thinking, or a turn-cancel context is still installed).
//     Working takes precedence over the last-turn outcome so a turn that
//     just started overrides the previous turn's Done/Error — same as
//     crush's "working wins over the last outcome" precedence.
//  5. Done or Error from the last turn outcome — recorded by
//     recordTurnOutcome(), cleared by clearLastTurnOutcome().
//  6. Idle — the default.
func (m *runMode) programStatus() *tea.ProgramStatus {
	if envOSC7501Off() {
		return nil
	}
	ps := &tea.ProgramStatus{State: tea.ProgramStateIdle, App: programStatusApp}
	switch {
	case m.pendingPermission:
		ps.State = tea.ProgramStateBlocked
		ps.Kind = tea.ProgramStatusKindPermission
		ps.Message = "Permission required"
	case m.hasPendingQuestion():
		ps.State = tea.ProgramStateBlocked
		ps.Kind = tea.ProgramStatusKindQuestion
		ps.Message = "Questions need your input"
	case m.pendingAuth:
		ps.State = tea.ProgramStateBlocked
		ps.Kind = tea.ProgramStatusKindAuth
		ps.Message = "Sign-in required"
	case m.isAgentBusy():
		ps.State = tea.ProgramStateWorking
	case m.lastTurnOutcome != "":
		ps.State = m.lastTurnOutcome
	}
	if envOSC7501Brief() && ps.State == tea.ProgramStateBlocked {
		// Brief suppresses the *detail* of a Blocked state — the
		// pending-command / pending-question / re-auth-context message
		// that the terminal would otherwise surface in its status bar.
		// The State and Kind stay so the user's terminal still shows
		// "Blocked: Permission" / "Blocked: Question" / "Blocked: Auth",
		// it just doesn't include the prose. Error messages are kept
		// (Error is the actionable state); Working has no message
		// in this implementation so the check is moot.
		ps.Message = ""
	}
	return ps
}

// hasPendingQuestion is true while the sub-agent question picker is on
// screen OR the answer textarea is focused on a queued question. Mirrors
// crush's `(*UI).hasPendingQuestion` exactly — both reads the inline
// picker state and the focused-answer state — so the OSC 7501 status
// matches in shape.
func (m *runMode) hasPendingQuestion() bool {
	if m.subAgentQuestionPickerOpen {
		return true
	}
	if m.subAgentQuestionAnswerTarget != "" {
		return true
	}
	return false
}

// isAgentBusy is true while a model turn is in flight. The streaming flag
// flips at streamPrompt start and again at every backendChunk / turnDone
// handler; the turnCancel context survives transient flag flips so the OSC
// 7501 status doesn't stutter between Working and Idle during the chunk →
// done transition.
//
// The thinking flag is added because the cycle-100 "⏺ Thinking... (Ns)"
// frame is part of the same in-flight turn from the terminal's perspective.
func (m *runMode) isAgentBusy() bool {
	return m.streaming || m.turnCancel != nil || m.thinking
}

// recordTurnOutcome records how the open turn ended. Called from the
// turnDoneMsg and error handlers (cycle osc-7501 stashes the future
// chunk-Error route here too); the OSC 7501 status reads the value on the
// next View() and reports Done / Error until the next turn begins or
// clearLastTurnOutcome() fires.
//
// Mirrors crush's `(*UI).trackTurnOutcome` (which listens for pubsub
// events); wagyu does not have a pubsub layer, so this is called directly
// from the Update() handlers that own the message routing.
func (m *runMode) recordTurnOutcome(state tea.ProgramState) {
	m.lastTurnOutcome = state
}

// clearLastTurnOutcome clears the recorded outcome. Called when the user
// sends a new prompt (so the new turn's "Working" state isn't reported as
// "Done" for a frame) and on session reset. Mirrors crush's
// `TestSendMessageClearsTurnOutcome` semantics.
func (m *runMode) clearLastTurnOutcome() {
	m.lastTurnOutcome = ""
}
