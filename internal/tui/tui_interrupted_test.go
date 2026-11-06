package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tas "charm.land/bubbles/v2/textarea"

	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
)

func TestInterruptedHintLineMatchesTheReference(t *testing.T) {
	got := agentturn.InterruptedHintLine("Antigravity CLI")
	want := "  ⎿  Interrupted · What should Antigravity CLI do instead?"
	if got != want {
		t.Fatalf("interrupted hint drifted:\n got %q\nwant %q", got, want)
	}
}

func TestEscCancelAnswersWithTheComposerHint(t *testing.T) {
	cancelled := false
	m := &runMode{
		engine: engineState{
			streaming:  true,
			turnCancel: func() { cancelled = true },
		},
	}
	_, _ = m.handleEscKeyMsg()
	if !cancelled {
		t.Fatal("Esc did not cancel the in-flight turn")
	}
	if !m.engine.turnInterrupted {
		t.Fatal("Esc did not mark the turn as interrupted")
	}
	if row := m.interruptedHintRow(); !strings.Contains(row, "What should") {
		t.Fatalf("the composer hint row is missing: %q", row)
	}
}

func TestCancelledTurnLeavesNoErrorRow(t *testing.T) {
	m := &runMode{engine: engineState{streaming: true, turnCancel: func() {}}}
	_, _ = m.handleEscKeyMsg()
	m.recordTurnCompletion(toolsteps.TurnDoneMsg{Err: context.Canceled})
	for _, line := range m.engine.response {
		if strings.Contains(line, "Error:") {
			t.Fatalf("a cancelled turn left an error row behind: %q", line)
		}
	}
}

func TestFailedTurnStillReportsTheError(t *testing.T) {
	m := &runMode{}
	m.recordTurnCompletion(toolsteps.TurnDoneMsg{Err: errors.New("the model hung up")})
	found := false
	for _, line := range m.engine.response {
		if strings.Contains(line, "Error: the model hung up") {
			found = true
		}
	}
	if !found {
		t.Fatalf("a failed turn lost its error row: %v", m.engine.response)
	}
}

func TestCancelledTurnSuppressesTheCancellationChunk(t *testing.T) {
	m := &runMode{engine: engineState{turnInterrupted: true}}
	m.updateBackendChunkMsg(backendChunkMsg(context.Canceled.Error()))
	if len(m.engine.response) != 0 {
		t.Fatalf("the cancellation reached the transcript: %v", m.engine.response)
	}
}

func TestTheComposerHintDisappearsWithTheNextTurn(t *testing.T) {
	m := &runMode{engine: engineState{turnInterrupted: true}}
	m.composer.input = tas.New()
	m.resetLiveTurnState("do the thing")
	if m.engine.turnInterrupted {
		t.Fatal("the interrupted flag survived the next turn")
	}
	if row := m.interruptedHintRow(); row != "" {
		t.Fatalf("the composer hint survived the next turn: %q", row)
	}
}
