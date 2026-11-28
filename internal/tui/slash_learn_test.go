package tui

// Guard for the /learn argument contract.
//
// /learn is a prompt-sending command: it starts a turn whose user message is
// the reference's instruction. Every other command in that family (/goal,
// /plan, /teamwork-preview) prepends the text typed after the command to the
// instruction, separated by a blank line. /learn took the argument and
// discarded it, so ` /learn the endpoint must come from config` sent the
// standing procedure alone — the user's actual correction, which is the whole
// point of the command, never reached the model.
//
// These tests pin the two halves: the instruction is the reference's, and the
// argument is not dropped.

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/app/session/slashactions"
)

// TestSlashLearn_SendsTheReferenceInstruction pins where the instruction
// travels. The full byte-for-byte pin lives in devutils/wirealign (which
// compares the reference literal to the live function); the envelope itself is
// pinned against a live capture in internal/core/backend's
// TestWrapUserContentWithMentions_MatchesCaptures.
//
// What this covers is the split the clone used to get wrong: the turn is the
// command text verbatim, and the instruction is an @-mention expansion in
// <ADDITIONAL_METADATA> — not the other way round.
func TestSlashLearn_SendsTheReferenceInstruction(t *testing.T) {
	m := newSlashModel(t)
	_, cmd := m.handleSlashCommand("/learn")
	if cmd == nil {
		t.Fatal("/learn must return a Cmd")
	}
	if got := lastUserMessage(m); got != "/learn" {
		t.Fatalf("/learn turn = %q, want the command verbatim", headLine(got))
	}
	wire := lastUserWire(m)
	for _, section := range []string{
		"<USER_REQUEST>\n/learn\n</USER_REQUEST>",
		"is a [Slash Command]:\n<LEARN>\n" + slashactions.LearnPrompt() + "</LEARN>",
		"## Identify What to Learn",
		"## Classify Rules vs. Skills",
		"## Create vs. Update",
		"## Mandatory Proposal Workflow",
		"learning_proposal.md",
	} {
		if !strings.Contains(wire, section) {
			t.Errorf("/learn wire is missing %q", section)
		}
	}
}

// TestSlashLearn_KeepsTheUsersArgument pins that the argument reaches the
// model. It rides in the turn, verbatim, where the reference puts it.
func TestSlashLearn_KeepsTheUsersArgument(t *testing.T) {
	const arg = "the API endpoint must be read from config, never hardcoded"
	m := newSlashModel(t)
	if _, cmd := m.handleSlashCommand("/learn " + arg); cmd == nil {
		t.Fatal("/learn <arg> must return a Cmd")
	}
	if got, want := lastUserMessage(m), "/learn "+arg; got != want {
		t.Fatalf("/learn turn = %q, want %q", headLine(got), want)
	}
	wire := lastUserWire(m)
	if !strings.Contains(wire, "<USER_REQUEST>\n/learn "+arg+"\n</USER_REQUEST>") {
		t.Fatalf("/learn lost the user's argument on the wire; turn block is %q", headLine(wire))
	}
	if !strings.Contains(wire, "<LEARN>\n"+slashactions.LearnPrompt()+"</LEARN>") {
		t.Fatal("/learn wire must carry the instruction as the <LEARN> expansion")
	}
}

// TestSlashLearn_EchoesTheCanonicalToken pins the echo, which is what the TUI
// scenario asserts on screen.
func TestSlashLearn_EchoesTheCanonicalToken(t *testing.T) {
	m := newSlashModel(t)
	m.handleSlashCommand("/learn the API is down")
	if len(m.conversation.response) == 0 || m.conversation.response[0] != "> /learn" {
		t.Fatalf("/learn echo = %v, want the canonical \"> /learn\"", m.conversation.response)
	}
}

func headLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
