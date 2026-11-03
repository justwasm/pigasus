// Package programstatus reports wagyu's state to terminals that support the
// Program Status Protocol (OSC 7501).
//
// The reference (AGY 1.4.0 changelog) ships terminal status reporting over
// OSC 7501: terminals like kitty, ghostty, WezTerm ≥ 2024 and foot display a
// "Working…/Blocked/Done/Error" indicator in their tab or status line.
//
// This file mirrors crush's internal/ui/model/program_status.go (the
// reference layout in this repo: `/Users/gear/Documents/GitHub/crush/internal/
// ui/model/program_status.go`).
package programstatus

import (
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// App is the stable program name reported to the terminal.
//
// The Program Status Protocol limits App to [A-Za-z0-9_.+-]{1,32}; "wagyu"
// matches and is reused across every report so the terminal can identify the
// record owner.
const App = "wagyu"

// Cycle 239 — env-var names lifted verbatim from the AGY 1.4.0 changelog.
// Kept as named constants so the test file and the production reader can't
// drift on the spelling.
const (
	EnvOSC7501         = "AGY_CLI_OSC7501"
	EnvOSC7501Messages = "AGY_CLI_OSC7501_MESSAGES"
)

// Inputs holds the model states that determine the OSC 7501 status report.
type Inputs struct {
	PendingPermission  bool
	HasPendingQuestion bool
	PendingAuth        bool
	IsAgentBusy        bool
	LastTurnOutcome    tea.ProgramState
}

// EnvOff reports whether AGY_CLI_OSC7501 is set to a falsy value
// (off / false / 0 / no, case-insensitive, whitespace-trimmed). This is
// the highest-priority gate for the OSC 7501 report: returning true here
// suppresses the report entirely, even when the terminal supports
// OSC 7501 and the rest of Resolve() would otherwise emit.
//
// An unset / unknown value returns false — the env var is opt-out only.
func EnvOff() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(EnvOSC7501)))
	switch v {
	case "off", "false", "0", "no":
		return true
	}
	return false
}

// EnvOn reports whether AGY_CLI_OSC7501 is set to a truthy value
// (on / true / 1 / yes, case-insensitive, whitespace-trimmed).
func EnvOn() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(EnvOSC7501)))
	switch v {
	case "on", "true", "1", "yes":
		return true
	}
	return false
}

// EnvBrief reports whether AGY_CLI_OSC7501_MESSAGES=brief.
// Case-insensitive, whitespace-trimmed; unknown values are treated as
// "not brief" so misconfiguration degrades to "show full message".
func EnvBrief() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(EnvOSC7501Messages)))
	return v == "brief"
}

// Resolve reports Wagyu's current state to terminals that support the
// Program Status Protocol. Terminals that don't implement OSC 7501 (or the
// subset that listens for state reports) simply ignore the report.
//
// Returns nil when AGY_CLI_OSC7501 is set to a falsy value (off / false /
// 0 / no). A nil return is honoured by bubbletea/v2's renderer
// (cursed_renderer.go:159, 868) — the writer skips emitting OSC 7501
// for a frame whose ProgramStatus is nil.
//
// State precedence, highest → lowest:
//  1. Blocked / Permission
//  2. Blocked / Question
//  3. Blocked / Auth
//  4. Working
//  5. Done or Error from the last turn outcome
//  6. Idle — the default.
func Resolve(in Inputs) *tea.ProgramStatus {
	if EnvOff() {
		return nil
	}
	ps := &tea.ProgramStatus{State: tea.ProgramStateIdle, App: App}
	switch {
	case in.PendingPermission:
		ps.State = tea.ProgramStateBlocked
		ps.Kind = tea.ProgramStatusKindPermission
		ps.Message = "Permission required"
	case in.HasPendingQuestion:
		ps.State = tea.ProgramStateBlocked
		ps.Kind = tea.ProgramStatusKindQuestion
		ps.Message = "Questions need your input"
	case in.PendingAuth:
		ps.State = tea.ProgramStateBlocked
		ps.Kind = tea.ProgramStatusKindAuth
		ps.Message = "Sign-in required"
	case in.IsAgentBusy:
		ps.State = tea.ProgramStateWorking
	case in.LastTurnOutcome != "":
		ps.State = in.LastTurnOutcome
	}
	if EnvBrief() && ps.State == tea.ProgramStateBlocked {
		ps.Message = ""
	}
	return ps
}
