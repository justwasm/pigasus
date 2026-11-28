package tui

// Guard for /teamwork-preview's wire shape.
//
// Like /plan, this is not a prompt-sending command: the reference sends the
// turn verbatim. Three live agy 1.5.18 captures settled it — `/teamwork`
// and `/teamwork-preview` in the TUI, plus the same through `-p` — and every
// one went out as the command text with no expansion:
//
//	<USER_REQUEST>
//	/teamwork-preview build a parser
//	</USER_REQUEST>
//	<ADDITIONAL_METADATA>
//	The current local time is: …
//	</ADDITIONAL_METADATA>
//
// The command is absent from the reference's picker, and the system
// instruction for those turns (11,140 B) names teamwork nowhere. The
// <TEAMWORK> literal does exist in the binary but no captured path reaches
// it, so the clone must not send it either.

import (
	"strings"
	"testing"
)

// TestSlashTeamwork_SendsTheTurnVerbatim pins the turn and the canonical echo.
func TestSlashTeamwork_SendsTheTurnVerbatim(t *testing.T) {
	for _, typed := range []string{"/teamwork", "/teamwork-preview"} {
		m := newSlashModel(t)
		if _, cmd := m.handleSlashCommand(typed + " build a parser"); cmd == nil {
			t.Fatalf("%s must return a Cmd", typed)
		}
		// The alias and the command both echo the canonical name.
		if got, want := m.conversation.response[0], "> /teamwork-preview"; got != want {
			t.Errorf("%s echo = %q, want %q", typed, got, want)
		}
		if got, want := lastUserMessage(m), "/teamwork-preview build a parser"; got != want {
			t.Errorf("%s turn = %q, want %q", typed, headLine(got), want)
		}
	}
}

// TestSlashTeamwork_SendsNoInstructionBody is the regression this fixes: the
// <TEAMWORK> body must not appear in the request.
func TestSlashTeamwork_SendsNoInstructionBody(t *testing.T) {
	m := newSlashModel(t)
	m.handleSlashCommand("/teamwork-preview build a parser")
	wire := lastUserWire(m)
	for _, forbidden := range []string{
		"<TEAMWORK>",
		"invoke_subagent",
		"sentinel",
	} {
		if strings.Contains(wire, forbidden) {
			t.Errorf("/teamwork wire contains %q, which the reference never sends", forbidden)
		}
	}
	if !strings.Contains(wire, "<USER_REQUEST>\n/teamwork-preview build a parser\n</USER_REQUEST>") {
		t.Errorf("/teamwork wire turn block is not the verbatim command: %q", headLine(wire))
	}
}
