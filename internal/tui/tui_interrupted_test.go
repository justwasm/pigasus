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
	bridge, ctx := armRunningBridge(t)
	m := &RootModel{
		turn:         TurnService{activeBridge: bridge},
		conversation: ConversationModel{streaming: true},
	}
	_, _ = m.handleEscKeyMsg()
	requireCancelled(t, ctx)
	if !m.conversation.turnInterrupted {
		t.Fatal("Esc did not mark the turn as interrupted")
	}
	if row := m.interruptedHintRow(); !strings.Contains(row, "What should") {
		t.Fatalf("the composer hint row is missing: %q", row)
	}
}

func TestCancelledTurnLeavesNoErrorRow(t *testing.T) {
	bridge, ctx := armRunningBridge(t)
	m := &RootModel{turn: TurnService{activeBridge: bridge}, conversation: ConversationModel{streaming: true}}
	_, _ = m.handleEscKeyMsg()
	requireCancelled(t, ctx)
	m.recordTurnCompletion(toolsteps.TurnDoneMsg{Err: context.Canceled})
	for _, line := range m.conversation.response {
		if strings.Contains(line, "Error:") {
			t.Fatalf("a cancelled turn left an error row behind: %q", line)
		}
	}
}

func TestFailedTurnStillReportsTheError(t *testing.T) {
	m := &RootModel{}
	m.recordTurnCompletion(toolsteps.TurnDoneMsg{Err: errors.New("the model hung up")})
	found := false
	for _, line := range m.conversation.response {
		if strings.Contains(line, "Error: the model hung up") {
			found = true
		}
	}
	if !found {
		t.Fatalf("a failed turn lost its error row: %v", m.conversation.response)
	}
}

func TestCancelledTurnSuppressesTheCancellationChunk(t *testing.T) {
	m := &RootModel{conversation: ConversationModel{turnInterrupted: true}}
	m.updateBackendChunkMsg(backendChunkMsg(context.Canceled.Error()))
	if len(m.conversation.response) != 0 {
		t.Fatalf("the cancellation reached the transcript: %v", m.conversation.response)
	}
}

func TestTheComposerHintDisappearsWithTheNextTurn(t *testing.T) {
	m := &RootModel{conversation: ConversationModel{turnInterrupted: true}}
	m.prompt.input = tas.New()
	m.resetLiveTurnState("do the thing")
	if m.conversation.turnInterrupted {
		t.Fatal("the interrupted flag survived the next turn")
	}
	if row := m.interruptedHintRow(); row != "" {
		t.Fatalf("the composer hint survived the next turn: %q", row)
	}
}

func TestEscCancelDuringTeardownKeepsInterruptedAndQueues(t *testing.T) {
	// AGY 1.5.14 Item 5: Esc during teardown preserves Interrupted hint and queues follow-up messages
	m := &RootModel{
		conversation: ConversationModel{streaming: true, turnInterrupted: true},
	}
	m.prompt.input = tas.New()

	// Pressing Esc while tearing down must keep turnInterrupted == true
	_, _ = m.handleEscKeyMsg()
	if !m.conversation.turnInterrupted {
		t.Fatal("Esc during teardown cleared the turnInterrupted flag")
	}

	// Submitting a follow-up message while tearing down must queue into StreamQueue
	handled, _ := m.checkStreamingQueue("follow up after interrupt")
	if !handled {
		t.Fatal("checkStreamingQueue did not handle follow up input during teardown")
	}
	if len(m.session.store.StreamQueue) == 0 || m.session.store.StreamQueue[0] != "follow up after interrupt" {
		t.Fatalf("follow up message was not queued into StreamQueue: %v", m.session.store.StreamQueue)
	}
}

func TestPreSendCancellationReportsMessageNotSent(t *testing.T) {
	// AGY 1.5.14 Item 4: Canceling in pre-send window aborts the send and shows
	// "Message not sent: canceled before it was sent"
	m := &RootModel{
		conversation: ConversationModel{streaming: true},
	}
	m.recordTurnCompletion(toolsteps.TurnDoneMsg{
		Err: errors.New("Message not sent: canceled before it was sent"),
	})
	found := false
	for _, line := range m.conversation.response {
		if strings.Contains(line, "Message not sent: canceled before it was sent") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("pre-send cancellation notice missing in response rows: %v", m.conversation.response)
	}
}
