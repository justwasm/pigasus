package tui

// Guard for /plan's wire shape.
//
// /plan is not a prompt-sending command: it is an agent mode. The reference
// sets the mode and sends the turn verbatim — the command text plus whatever
// the user typed — and never sends the <PLAN> instruction. Twenty live 1.5.18
// captures (TUI, `--mode plan`, `-p`) contain zero occurrences of it, /plan is
// absent from the reference's own picker (typing it bare answers "Unknown
// command"), and plan mode's system instruction is byte-identical to the
// normal one.
//
// The clone used to prepend the 3,722 B <PLAN> body, which sent the model a
// block the reference never sends. This pins the corrected shape.

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/core/policy/permissions"
)

// TestSlashPlan_SendsTheTurnVerbatim pins that the turn is the command text
// and nothing else.
func TestSlashPlan_SendsTheTurnVerbatim(t *testing.T) {
	const arg = "refactor the auth middleware"
	m := newSlashModel(t)
	if _, cmd := m.handleSlashCommand("/plan " + arg); cmd == nil {
		t.Fatal("/plan <arg> must return a Cmd")
	}
	if got, want := lastUserMessage(m), "/plan "+arg; got != want {
		t.Fatalf("/plan turn = %q, want %q", headLine(got), want)
	}
	// The mode still changes: /plan is a mode, and the mode is what makes it
	// mean anything.
	if m.session.mode != permissions.ModePlan {
		t.Errorf("mode = %v, want ModePlan", m.session.mode)
	}
}

// TestSlashPlan_SendsNoInstructionBody is the regression this fixes: the
// <PLAN> body must not appear anywhere in the request.
func TestSlashPlan_SendsNoInstructionBody(t *testing.T) {
	m := newSlashModel(t)
	m.handleSlashCommand("/plan refactor the auth middleware")
	wire := lastUserWire(m)
	for _, forbidden := range []string{
		"<PLAN>",
		"Planning Mode Artifacts",
		"The user is requesting that you think and plan carefully",
		"request_feedback = true",
	} {
		if strings.Contains(wire, forbidden) {
			t.Errorf("/plan wire contains %q, which the reference never sends", forbidden)
		}
	}
	if !strings.Contains(wire, "<USER_REQUEST>\n/plan refactor the auth middleware\n</USER_REQUEST>") {
		t.Errorf("/plan wire turn block is not the verbatim command: %q", headLine(wire))
	}
}

// TestSlashPlan_BarePinsTheCommandAlone covers /plan with no argument.
func TestSlashPlan_BarePinsTheCommandAlone(t *testing.T) {
	m := newSlashModel(t)
	m.handleSlashCommand("/plan")
	if got := lastUserMessage(m); got != "/plan" {
		t.Fatalf("/plan turn = %q, want %q", headLine(got), "/plan")
	}
}
