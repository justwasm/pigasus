package tui

// Guard for the /grill-me contract.
//
// /grill-me is the design-interview command: the reference's instruction walks
// the design tree, resolves decision dependencies one at a time, recommends an
// answer per question, and asks through the `ask_question` tool. The clone used
// to send an invented sentence ("Interrogate my reasoning on the topic I just
// raised…") that is not in the 1.5.18 binary at all, and that asked for
// adversarial interrogation instead.
//
// The byte-for-byte pin against the recovered literal lives in
// devutils/wirealign/slashprompts.go. These are the TUI-side properties: the
// path uses that function, the argument survives, and the echo is canonical.

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/app/session/slashactions"
)

// TestSlashGrillMe_SendsTheReferenceInstruction pins the prompt body.
func TestSlashGrillMe_SendsTheReferenceInstruction(t *testing.T) {
	m := newSlashModel(t)
	if _, cmd := m.handleSlashCommand("/grill-me"); cmd == nil {
		t.Fatal("/grill-me must return a Cmd")
	}
	if got := lastUserMessage(m); got != "/grill-me" {
		t.Fatalf("/grill-me turn = %q, want the command verbatim", headLine(got))
	}
	got := lastUserWire(m)
	if !strings.Contains(got, "<GRILL_ME>\n"+slashactions.GrillMePrompt()+"</GRILL_ME>") {
		t.Fatalf("/grill-me wire must carry the instruction as the <GRILL_ME> expansion, got %d B", len(got))
	}
	for _, want := range []string{
		"shared understanding",
		"design tree",
		"resolving dependencies between decisions one-by-one",
		"provide your recommended answer",
		"Ask the questions one at a time.",
		"If a question can be answered by exploring the codebase, explore the codebase instead.",
		"Use the ask_question tool for asking questions to the user.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("/grill-me instruction is missing %q", want)
		}
	}
	// The invented sentence must be gone: it asked for something the
	// reference does not, and it was never in the binary.
	if strings.Contains(got, "Interrogate my reasoning") {
		t.Error("/grill-me still sends the invented \"Interrogate my reasoning…\" sentence")
	}
}

// TestSlashGrillMe_KeepsTheUsersArgument pins the argument contract, which
// /learn was missing.
func TestSlashGrillMe_KeepsTheUsersArgument(t *testing.T) {
	const arg = "the design of the retry layer"
	m := newSlashModel(t)
	if _, cmd := m.handleSlashCommand("/grill-me " + arg); cmd == nil {
		t.Fatal("/grill-me <arg> must return a Cmd")
	}
	if got, want := lastUserMessage(m), "/grill-me "+arg; got != want {
		t.Fatalf("/grill-me turn = %q, want %q", headLine(got), want)
	}
	wire := lastUserWire(m)
	if !strings.Contains(wire, "<USER_REQUEST>\n/grill-me "+arg+"\n</USER_REQUEST>") {
		t.Fatalf("/grill-me lost the user's argument on the wire; turn block is %q", headLine(wire))
	}
	if !strings.Contains(wire, "<GRILL_ME>\n"+slashactions.GrillMePrompt()+"</GRILL_ME>") {
		t.Fatal("/grill-me wire must carry the instruction as the <GRILL_ME> expansion")
	}
}

// TestSlashGrillMe_EchoesTheCanonicalToken pins the echo the TUI shows.
func TestSlashGrillMe_EchoesTheCanonicalToken(t *testing.T) {
	m := newSlashModel(t)
	m.handleSlashCommand("/grill-me")
	if len(m.conversation.response) == 0 || m.conversation.response[0] != "> /grill-me" {
		t.Fatalf("/grill-me echo = %v, want the canonical \"> /grill-me\"", m.conversation.response)
	}
}
