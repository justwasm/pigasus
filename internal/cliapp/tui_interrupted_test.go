package cliapp

import (
	"context"
	"errors"
	"strings"
	"testing"

	tas "charm.land/bubbles/v2/textarea"

	"github.com/justwasm/pigasus/internal/agent"
)

func TestInterruptedHintLineMatchesTheReference(t *testing.T) {
	got := agent.InterruptedHintLine("Antigravity CLI")
	want := "  ⎿  Interrupted · What should Antigravity CLI do instead?"
	if got != want {
		t.Fatalf("interrupted hint drifted:\n got %q\nwant %q", got, want)
	}
}

func TestEscCancelAnswersWithTheComposerHint(t *testing.T) {
	cancelled := false
	m := &runMode{
		streaming:  true,
		turnCancel: func() { cancelled = true },
	}
	_, _ = m.handleEscKeyMsg()
	if !cancelled {
		t.Fatal("Esc did not cancel the in-flight turn")
	}
	if !m.turnInterrupted {
		t.Fatal("Esc did not mark the turn as interrupted")
	}
	if row := m.interruptedHintRow(); !strings.Contains(row, "What should") {
		t.Fatalf("the composer hint row is missing: %q", row)
	}
}

func TestCancelledTurnLeavesNoErrorRow(t *testing.T) {
	m := &runMode{streaming: true, turnCancel: func() {}}
	_, _ = m.handleEscKeyMsg()
	m.recordTurnCompletion(turnDoneMsg{err: context.Canceled})
	for _, line := range m.response {
		if strings.Contains(line, "Error:") {
			t.Fatalf("a cancelled turn left an error row behind: %q", line)
		}
	}
}

func TestFailedTurnStillReportsTheError(t *testing.T) {
	m := &runMode{}
	m.recordTurnCompletion(turnDoneMsg{err: errors.New("the model hung up")})
	found := false
	for _, line := range m.response {
		if strings.Contains(line, "Error: the model hung up") {
			found = true
		}
	}
	if !found {
		t.Fatalf("a failed turn lost its error row: %v", m.response)
	}
}

func TestCancelledTurnSuppressesTheCancellationChunk(t *testing.T) {
	m := &runMode{turnInterrupted: true}
	m.updateBackendChunkMsg(backendChunkMsg(context.Canceled.Error()))
	if len(m.response) != 0 {
		t.Fatalf("the cancellation reached the transcript: %v", m.response)
	}
}

func TestTheComposerHintDisappearsWithTheNextTurn(t *testing.T) {
	m := &runMode{turnInterrupted: true}
	m.input = tas.New()
	m.resetLiveTurnState("do the thing")
	if m.turnInterrupted {
		t.Fatal("the interrupted flag survived the next turn")
	}
	if row := m.interruptedHintRow(); row != "" {
		t.Fatalf("the composer hint survived the next turn: %q", row)
	}
}
