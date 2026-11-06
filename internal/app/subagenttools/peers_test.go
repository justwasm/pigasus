package subagenttools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestListPeersWithoutAParent pins the reference's answer for a
// conversation that has no parent conversation.
func TestListPeersWithoutAParent(t *testing.T) {
	registry := NewRegistry("parent-1")
	registry.Add(NewChild("child-1", "research", "", t.TempDir(), func() {}))

	// The owner is the conversation that owns the registry: it has no
	// parent conversation, so it has no peers.
	tool := &manageSubagentsTool{registry: registry, owner: "parent-1"}
	args, _ := json.Marshal(manageSubagentsArgs{Action: "list_peers"})
	res, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "You have no parent conversation, so you have no peer subagents:\n[]"
	if !strings.HasSuffix(res.Output, want) {
		t.Fatalf("list_peers result %q does not end with %q", res.Output, want)
	}
}

// TestListPeersFromASubagent pins the reference's peer list.
func TestListPeersFromASubagent(t *testing.T) {
	registry := NewRegistry("parent-1")
	registry.Add(NewChild("child-1", "research", "", t.TempDir(), func() {}))
	registry.Add(NewChild("child-2", "self", "Tester", t.TempDir(), func() {}))

	// The owner is a subagent: its peers are the parent's subagents.
	tool := &manageSubagentsTool{registry: registry, owner: "child-2"}
	args, _ := json.Marshal(manageSubagentsArgs{Action: "list_peers"})
	res, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := `You have 2 active peer subagent(s) (parent parent-1):
[{"type":"research","role":"research","conversationId":"child-1","state":"running"},` +
		`{"type":"self","role":"Tester","conversationId":"child-2","state":"running"}]`
	if !strings.HasSuffix(res.Output, want) {
		t.Fatalf("list_peers result %q does not end with %q", res.Output, want)
	}
}

// TestSendMessageBroadcastsToTheChildren pins the reference's broadcast
// summary.
func TestSendMessageBroadcastsToTheChildren(t *testing.T) {
	registry := NewRegistry("parent-1")
	childA := NewChild("child-a", "research", "", t.TempDir(), func() {})
	childB := NewChild("child-b", "research", "", t.TempDir(), func() {})
	registry.Add(childA)
	registry.Add(childB)

	tool := &sendMessageTool{registry: registry, owner: "parent-1"}
	args, _ := json.Marshal(sendMessageArgs{Recipient: "@children", Message: "hello"})
	res, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "Message sent to 2 subagents: child-a, child-b."
	if !strings.HasSuffix(res.Output, want) {
		t.Fatalf("broadcast result %q does not end with %q", res.Output, want)
	}
	for _, child := range []*Child{childA, childB} {
		msg, ok := child.Wait(context.Background())
		if !ok {
			t.Fatalf("%s did not receive the broadcast", child.ConversationID)
		}
		if msg.Content != "hello" {
			t.Fatalf("%s received %q", child.ConversationID, msg.Content)
		}
	}
}

// TestSendMessageBroadcastReportsAPartialDelivery pins the reference's
// partial-delivery sentence.
func TestSendMessageBroadcastReportsAPartialDelivery(t *testing.T) {
	registry := NewRegistry("parent-1")
	live := NewChild("child-a", "research", "", t.TempDir(), func() {})
	registry.Add(live)
	// A child whose inbox is gone cannot be reached.
	gone := NewChild("child-b", "research", "", t.TempDir(), func() {})
	gone.inbox = nil
	registry.Add(gone)

	tool := &sendMessageTool{registry: registry, owner: "parent-1"}
	args, _ := json.Marshal(sendMessageArgs{Recipient: "@children", Message: "hello"})
	res, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Output, "Message sent to 1 of 2 subagents: child-a.") {
		t.Fatalf("broadcast result %q does not carry the partial sentence", res.Output)
	}
	if !strings.Contains(res.Output, ` Failed to deliver to "child-b":`) {
		t.Fatalf("broadcast result %q does not carry the failure sentence", res.Output)
	}
}

// TestTheFailedSubagentSentence pins the reference's own failure notice.
func TestTheFailedSubagentSentence(t *testing.T) {
	registry := NewRegistry("parent-1")
	var notified string
	registry.SetNotify(func(sender, content string) { notified = content })
	child := NewChild("child-1", "research", "", t.TempDir(), func() {})

	reportFailure(Deps{Registry: registry}, child, context.DeadlineExceeded)

	want := `The subagent research with id "child-1" encountered an error and has either stopped or failed to start execution: context deadline exceeded`
	if notified != want {
		t.Fatalf("failure notice = %q, want %q", notified, want)
	}
}
