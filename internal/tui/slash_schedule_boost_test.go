package tui

// Guards for /schedule's and /boost's wire shapes, both settled from live
// agy 1.5.18 captures.
//
// /schedule expands as a mention, like /learn and /goal. The capture
// (`/schedule run the tests every morning`) is a 437 B turn whose
// <ADDITIONAL_METADATA> carries:
//
//	/schedule is a [Slash Command]:
//	Please use the schedule tool to create a schedule as specified by the user
//
// — one line, no wrapper tag.
//
// /boost does not expand at all: `/boost analyze the retry layer` is a 160 B
// turn with nothing but the timestamp in <ADDITIONAL_METADATA>. It is absent
// from the reference's picker, and the binary carries a server-issued
// boostCommandDisabled control, so whether it expands is decided upstream.

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/app/session/slashactions"
)

// TestSlashSchedule_SendsTheReferenceExpansion pins the mention shape and the
// exact body.
func TestSlashSchedule_SendsTheReferenceExpansion(t *testing.T) {
	const arg = "run the tests every morning"
	m := newSlashModel(t)
	if _, cmd := m.handleSlashCommand("/schedule " + arg); cmd == nil {
		t.Fatal("/schedule must return a Cmd")
	}
	if got, want := lastUserMessage(m), "/schedule "+arg; got != want {
		t.Fatalf("/schedule turn = %q, want %q", headLine(got), want)
	}
	wire := lastUserWire(m)
	want := "/schedule is a [Slash Command]:\n" +
		"Please use the schedule tool to create a schedule as specified by the user"
	if !strings.Contains(wire, want) {
		t.Fatalf("/schedule wire is missing the reference expansion %q", want)
	}
	// The invented three-clause sentence must be gone.
	if strings.Contains(wire, "Tell me when it is registered") {
		t.Error("/schedule still sends the invented \"Tell me when it is registered…\" text")
	}
	if got := slashactions.SchedulePrompt(); got != "Please use the schedule tool to create a schedule as specified by the user" {
		t.Errorf("SchedulePrompt() = %q, want the 74 B reference line", got)
	}
}

// TestSlashSchedule_RequiresAnInstruction keeps the usage guard.
func TestSlashSchedule_RequiresAnInstruction(t *testing.T) {
	m := newSlashModel(t)
	handled, cmd := m.handleSlashCommand("/schedule")
	if !handled || cmd != nil {
		t.Fatalf("/schedule with no argument should answer with usage, got handled=%v cmd=%v", handled, cmd)
	}
}

// TestSlashBoost_SendsTheTurnVerbatim is the regression this fixes: the clone
// used to prepend its own <ORCHESTRATOR> body.
func TestSlashBoost_SendsTheTurnVerbatim(t *testing.T) {
	const arg = "analyze the retry layer"
	m := newSlashModel(t)
	if _, cmd := m.handleSlashCommand("/boost " + arg); cmd == nil {
		t.Fatal("/boost must return a Cmd")
	}
	if got, want := lastUserMessage(m), "/boost "+arg; got != want {
		t.Fatalf("/boost turn = %q, want %q", headLine(got), want)
	}
	wire := lastUserWire(m)
	for _, forbidden := range []string{"<ORCHESTRATOR>", "#### Current Task to Execute"} {
		if strings.Contains(wire, forbidden) {
			t.Errorf("/boost wire contains %q, which the reference never sends", forbidden)
		}
	}
	if !strings.Contains(wire, "<USER_REQUEST>\n/boost "+arg+"\n</USER_REQUEST>") {
		t.Errorf("/boost wire turn block is not the verbatim command: %q", headLine(wire))
	}
}
