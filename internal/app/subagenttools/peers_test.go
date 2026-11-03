package subagenttools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	pi "github.com/MichaelKinsy/PiG/ai"
)

// TestListPeersWithoutAParent pins the reference's answer for a
// conversation that has no parent conversation.
//
// 【反编译】 agy 1.4.8's
// `handlers.(*ManageSubagentsSubHandler).handleListPeers` (0x101db9560) —
// the literal is 66 bytes at 0x10365bc95.
func TestListPeersWithoutAParent(t *testing.T) {
	registry := NewRegistry("parent-1")
	registry.Add(NewChild("child-1", "research", "", t.TempDir(), func() {}))

	// The owner is the conversation that owns the registry: it has no
	// parent conversation, so it has no peers.
	tool := &manageSubagentsTool{registry: registry, owner: "parent-1"}
	args, _ := json.Marshal(manageSubagentsArgs{Action: "list_peers"})
	res, err := tool.Execute(context.Background(), "call-1", args, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	body := res.Content[0].(pi.TextContent)
	want := "You have no parent conversation, so you have no peer subagents:\n[]"
	if !strings.HasSuffix(body.Text, want) {
		t.Fatalf("list_peers result %q does not end with %q", body.Text, want)
	}
}

// TestListPeersFromASubagent pins the reference's peer list.
//
// 【反编译】 agy 1.4.8 — the format is
// "You have %d active peer subagent(s) (parent %s):\n%s" (51 bytes at
// 0x1036265c4).
func TestListPeersFromASubagent(t *testing.T) {
	registry := NewRegistry("parent-1")
	registry.Add(NewChild("child-1", "research", "", t.TempDir(), func() {}))
	registry.Add(NewChild("child-2", "self", "Tester", t.TempDir(), func() {}))

	// The owner is a subagent: its peers are the parent's subagents.
	tool := &manageSubagentsTool{registry: registry, owner: "child-2"}
	args, _ := json.Marshal(manageSubagentsArgs{Action: "list_peers"})
	res, err := tool.Execute(context.Background(), "call-1", args, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	body := res.Content[0].(pi.TextContent)
	want := `You have 2 active peer subagent(s) (parent parent-1):
[{"type":"research","role":"research","conversationId":"child-1","state":"running"},` +
		`{"type":"self","role":"Tester","conversationId":"child-2","state":"running"}]`
	if !strings.HasSuffix(body.Text, want) {
		t.Fatalf("list_peers result %q does not end with %q", body.Text, want)
	}
}

// TestSendMessageBroadcastsToTheChildren pins the reference's broadcast
// summary.
//
// 【反编译】 agy 1.4.8's `sendmessage.(*Delivery).Summary` (0x10155c580) —
// "Message sent to %d subagents: %s." with the ids joined with ", ".
func TestSendMessageBroadcastsToTheChildren(t *testing.T) {
	registry := NewRegistry("parent-1")
	childA := NewChild("child-a", "research", "", t.TempDir(), func() {})
	childB := NewChild("child-b", "research", "", t.TempDir(), func() {})
	registry.Add(childA)
	registry.Add(childB)

	tool := &sendMessageTool{registry: registry, owner: "parent-1"}
	args, _ := json.Marshal(sendMessageArgs{Recipient: "@children", Message: "hello"})
	res, err := tool.Execute(context.Background(), "call-1", args, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	body := res.Content[0].(pi.TextContent)
	want := "Message sent to 2 subagents: child-a, child-b."
	if !strings.HasSuffix(body.Text, want) {
		t.Fatalf("broadcast result %q does not end with %q", body.Text, want)
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
//
// 【反编译】 agy 1.4.8 — "Message sent to %d of %d subagents: %s." and
// " Failed to deliver to %q: %v." (the leading space is the reference's).
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
	res, err := tool.Execute(context.Background(), "call-1", args, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	body := res.Content[0].(pi.TextContent)
	if !strings.Contains(body.Text, "Message sent to 1 of 2 subagents: child-a.") {
		t.Fatalf("broadcast result %q does not carry the partial sentence", body.Text)
	}
	if !strings.Contains(body.Text, ` Failed to deliver to "child-b":`) {
		t.Fatalf("broadcast result %q does not carry the failure sentence", body.Text)
	}
}

// TestTheFailedSubagentSentence pins the reference's own failure notice.
//
// 【反编译】 agy 1.4.8 — the literal is at 0x10368c746.
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
