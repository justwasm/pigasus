package remotecontrol

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestTheQueuedCardDeleteDropsTheMessage pins the RPC the Web UI's
// queued card calls when the user throws a queued message away.
//
// 【一手】 2026-10-26, the operator's log: the delete failed with
// "[internal] rpc error -32602: Invalid params (conversation_id is
// required)" and the message stayed in the queue.
//
// The card's delete action calls DeleteAgentMessage with the pending
// message's id and the conversation it waits in:
//
//	r(await p(c({messageId:m,recipient:a})))
//
// The clone's alias table (reference_surface.go) mapped the name onto
// the dashboard's DeleteRemoteAgentMessage, whose handler wants
// conversation_id — so the IDE's shape was rejected.
func TestTheQueuedCardDeleteDropsTheMessage(t *testing.T) {
	backend := newLSGateBackend()
	d := newLSQueueDispatcher(t, backend)
	lsStartConversation(t, d, "conv-card-delete")

	first := make(chan error, 1)
	go func() { first <- lsSendMessage(t, d, "conv-card-delete", "first", int(lsDeliveryWhenIdle)) }()
	lsWaitForPrompt(t, backend, "first")
	if err := lsSendMessage(t, d, "conv-card-delete", "second", int(lsDeliveryWhenIdle)); err != nil {
		t.Fatalf("the queued message reported an error: %v", err)
	}

	traj := d.trajectories.Get("conv-card-delete")
	pending := traj.Pending()
	if len(pending) != 1 {
		t.Fatalf("pending messages = %d, want 1", len(pending))
	}
	payload, err := json.Marshal(map[string]any{
		"messageId": pending[0].ID,
		"recipient": "conv-card-delete",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := d.DispatchDirect(context.Background(), "DeleteAgentMessage", string(payload)); err != nil {
		t.Fatalf("DeleteAgentMessage: %v", err)
	}
	if pending := traj.Pending(); len(pending) != 0 {
		t.Fatalf("pending messages = %d, want 0", len(pending))
	}

	backend.release()
	<-first
}

// TestTheQueuedCardDeleteTakesTheRightMessage pins that the delete
// takes the message the user clicked and not its neighbour: the id
// names the message, the queue's index does not.
func TestTheQueuedCardDeleteTakesTheRightMessage(t *testing.T) {
	backend := newLSGateBackend()
	d := newLSQueueDispatcher(t, backend)
	lsStartConversation(t, d, "conv-card-pick")

	first := make(chan error, 1)
	go func() { first <- lsSendMessage(t, d, "conv-card-pick", "first", int(lsDeliveryWhenIdle)) }()
	lsWaitForPrompt(t, backend, "first")
	for _, text := range []string{"second", "third"} {
		if err := lsSendMessage(t, d, "conv-card-pick", text, int(lsDeliveryWhenIdle)); err != nil {
			t.Fatalf("the queued message reported an error: %v", err)
		}
	}

	traj := d.trajectories.Get("conv-card-pick")
	pending := traj.Pending()
	if len(pending) != 2 {
		t.Fatalf("pending messages = %d, want 2", len(pending))
	}
	payload, err := json.Marshal(map[string]any{
		"messageId": pending[1].ID,
		"recipient": "conv-card-pick",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := d.DispatchDirect(context.Background(), "DeleteAgentMessage", string(payload)); err != nil {
		t.Fatalf("DeleteAgentMessage: %v", err)
	}
	pending = traj.Pending()
	if len(pending) != 1 {
		t.Fatalf("pending messages = %d, want 1", len(pending))
	}
	if pending[0].Text != "second" {
		t.Errorf("the remaining message is %q, want %q", pending[0].Text, "second")
	}

	backend.release()
	<-first
}

// TestTheQueuedCardDeleteOfAMessageThatRanIsNotAnError pins that a
// delete of a message that is not waiting anymore is answered, not
// refused: the reference's own answer is empty, and the Web UI only
// reports a failure when the RPC itself fails.
func TestTheQueuedCardDeleteOfAMessageThatRanIsNotAnError(t *testing.T) {
	backend := newLSGateBackend()
	d := newLSQueueDispatcher(t, backend)
	lsStartConversation(t, d, "conv-card-gone")

	payload, err := json.Marshal(map[string]any{
		"messageId": "conv-card-gone-q9",
		"recipient": "conv-card-gone",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := d.DispatchDirect(context.Background(), "DeleteAgentMessage", string(payload)); err != nil {
		t.Fatalf("DeleteAgentMessage: %v", err)
	}
}

// TestTheQueuedCardDeleteNeedsTheConversation pins the parameter
// check: without the conversation the message waits in, the clone
// cannot tell which queue the message is in.
func TestTheQueuedCardDeleteNeedsTheConversation(t *testing.T) {
	d := newLSQueueDispatcher(t, newLSGateBackend())
	payload, err := json.Marshal(map[string]any{"messageId": "conv-x-q1"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	_, err = d.DispatchDirect(context.Background(), "DeleteAgentMessage", string(payload))
	if err == nil {
		t.Fatal("the delete without a recipient was accepted")
	}
	if !strings.Contains(err.Error(), "recipient is required") {
		t.Errorf("the error is %v, want it to name the missing recipient", err)
	}
}

// TestTheReferenceCLIDeleteIsNotTheQueuedCard pins that the two
// dialects keep their own handler.
//
// The reference CLI's own DeleteAgentMessage
// (google.gca.aicode.v1main) names the conversation and the stored
// message; the IDE's names the pending message and its recipient.
// The alias table maps the CLI's name onto the clone's legacy
// DeleteRemoteAgentMessage, and the legacy handler owns it.
func TestTheReferenceCLIDeleteIsNotTheQueuedCard(t *testing.T) {
	d := newLSQueueDispatcher(t, newLSGateBackend())
	payload, err := json.Marshal(map[string]any{
		"conversation_id": "conv-legacy",
		"message_id":      "msg-1",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	_, err = d.DispatchDirect(context.Background(), "DeleteAgentMessage", string(payload))
	if err == nil {
		t.Fatal("the delete of an unknown conversation was accepted")
	}
	// The queued-card handler refuses a request without a recipient.
	// The legacy handler is the one that owns this shape, so its error
	// is the one that must come back.
	if strings.Contains(err.Error(), "recipient is required") {
		t.Errorf("the queued-card handler took the CLI's shape: %v", err)
	}
}
