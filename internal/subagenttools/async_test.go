package subagenttools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pi "github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/internal/agentmessage"
)

// TestSendMessageReachesALiveSubagent pins the reference's success text —
// which is only truthful when a recipient is live.
//
// 【一手】 agy 1.4.4, live capture
// (docs/cycles/subagent-async-framing-capture/deliverable.md):
//
//	Created At: 2026-10-22T11:24:38+08:00
//	Completed At: 2026-10-22T11:24:39+08:00
//	Message sent to "df83f9c4-ac60-4bce-86f8-9ddad4103f23".
func TestSendMessageReachesALiveSubagent(t *testing.T) {
	registry := NewRegistry("parent-1")
	child := NewChild("child-1", "research", "Tester", t.TempDir(), func() {})
	registry.Add(child)

	tool := &sendMessageTool{registry: registry, owner: "parent-1"}
	args, _ := json.Marshal(map[string]string{"Recipient": "child-1", "Message": "PONG"})
	res, err := tool.Execute(context.Background(), "call-1", args, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.IsError {
		t.Fatalf("Execute returned IsError=true: %+v", res)
	}
	body, ok := res.Content[0].(pi.TextContent)
	if !ok {
		t.Fatalf("content[0] = %T, want pi.TextContent", res.Content[0])
	}
	if !strings.HasSuffix(body.Text, `Message sent to "child-1".`) {
		t.Fatalf("result %q does not end with the reference's success text", body.Text)
	}
	if !strings.HasPrefix(body.Text, "Created At: ") {
		t.Fatalf("result %q does not carry the reference's stamp header", body.Text)
	}

	// The message reached the child.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	msg, ok := child.Wait(ctx)
	if !ok {
		t.Fatal("the child did not receive the message")
	}
	if msg.Content != "PONG" {
		t.Fatalf("the child received %q, want %q", msg.Content, "PONG")
	}
	if msg.From != "parent-1" {
		t.Fatalf("the message came from %q, want the parent's conversation id", msg.From)
	}
}

// TestSendMessageToAnUnknownRecipient pins the reference's failure text.
//
// 【一手】 agy 1.4.7, internal/tools/testdata/result_texts_1.4.7.txt:
//
//	Encountered error in tool execution: recipient "abc123" not found
//
// It is a normal result, not an error: the reference stamps it and
// returns it like any other.
func TestSendMessageToAnUnknownRecipient(t *testing.T) {
	registry := NewRegistry("parent-1")
	tool := &sendMessageTool{registry: registry, owner: "parent-1"}
	args, _ := json.Marshal(map[string]string{"Recipient": "abc123", "Message": "PONG"})
	res, err := tool.Execute(context.Background(), "call-1", args, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.IsError {
		t.Fatalf("Execute returned IsError=true: %+v", res)
	}
	body := res.Content[0].(pi.TextContent)
	if !strings.HasSuffix(body.Text, `Encountered error in tool execution: recipient "abc123" not found`) {
		t.Fatalf("result %q does not carry the reference's not-found text", body.Text)
	}
}

// TestSubagentMessageReachesTheParent pins the other direction: a
// subagent's own send_message reaches the parent conversation.
func TestSubagentMessageReachesTheParent(t *testing.T) {
	var gotSender, gotContent string
	registry := NewRegistry("parent-1")
	registry.SetNotify(func(sender, content string) {
		gotSender, gotContent = sender, content
	})
	child := NewChild("child-1", "research", "Tester", t.TempDir(), func() {})
	registry.Add(child)

	tool := &sendMessageTool{registry: registry, owner: "child-1"}
	args, _ := json.Marshal(map[string]string{"Recipient": "parent-1", "Message": "PONG"})
	res, err := tool.Execute(context.Background(), "call-1", args, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	body := res.Content[0].(pi.TextContent)
	if !strings.HasSuffix(body.Text, `Message sent to "parent-1".`) {
		t.Fatalf("result %q does not carry the reference's success text", body.Text)
	}
	if gotSender != "child-1" {
		t.Fatalf("the parent was notified by %q, want the child's conversation id", gotSender)
	}
	if gotContent != "PONG" {
		t.Fatalf("the parent was notified with %q, want %q", gotContent, "PONG")
	}
	// The child reported, so the parent's turn must not deliver a second
	// message for the same turn.
	if !child.Reported() {
		t.Fatal("the child is not marked as having reported")
	}
}

// TestManageSubagentsListsTheLiveChildren pins the reference's list text
// and the four fields of a list entry.
//
// 【反编译】 agy 1.4.8's
// `oneharness/internal/tools/tools.(*ManageSubagentsTool).list`
// @0x101deeef0:
// "You have %d active subagent(s):\n" followed by the marshalled states,
// each carrying the type, the role, the conversation id and the state.
func TestManageSubagentsListsTheLiveChildren(t *testing.T) {
	registry := NewRegistry("parent-1")
	registry.Add(NewChild("child-1", "research", "", t.TempDir(), func() {}))

	tool := &manageSubagentsTool{registry: registry}
	args, _ := json.Marshal(manageSubagentsArgs{Action: "list"})
	res, err := tool.Execute(context.Background(), "call-1", args, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	body := res.Content[0].(pi.TextContent)
	want := `You have 1 active subagent(s):
[{"type":"research","role":"research","conversationId":"child-1","state":"running"}]`
	if !strings.HasSuffix(body.Text, want) {
		t.Fatalf("list result %q does not end with %q", body.Text, want)
	}
}

// TestManageSubagentsKillsTheNamedChildren pins the reference's kill text
// and the rule that an unknown id kills nothing.
//
// 【反编译】 agy 1.4.8's
// `oneharness/internal/tools/tools.(*ManageSubagentsTool).kill`
// @0x101def5f0.
func TestManageSubagentsKillsTheNamedChildren(t *testing.T) {
	registry := NewRegistry("parent-1")
	killed := false
	child := NewChild("child-1", "research", "Tester", t.TempDir(), func() { killed = true })
	registry.Add(child)

	tool := &manageSubagentsTool{registry: registry}
	args, _ := json.Marshal(manageSubagentsArgs{Action: "kill", ConversationIds: []string{"child-1"}})
	res, err := tool.Execute(context.Background(), "call-1", args, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	body := res.Content[0].(pi.TextContent)
	want := "Successfully killed 1 subagent(s) and their descendants.\nKilled roles: Tester"
	if !strings.HasSuffix(body.Text, want) {
		t.Fatalf("kill result %q does not end with %q", body.Text, want)
	}
	if !killed {
		t.Fatal("the child was not killed")
	}

	// An unknown id kills nothing.
	args, _ = json.Marshal(manageSubagentsArgs{Action: "kill", ConversationIds: []string{"nope"}})
	res, err = tool.Execute(context.Background(), "call-2", args, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	body = res.Content[0].(pi.TextContent)
	want = `conversation "nope" is not an active subagent of this conversation, so nothing was killed; run the "list" action to see the active subagents and their ids`
	if !strings.HasSuffix(body.Text, want) {
		t.Fatalf("kill result %q does not end with %q", body.Text, want)
	}
}

// TestManageSubagentsKillAllWithoutChildren pins the reference's own
// sentence for the empty case.
func TestManageSubagentsKillAllWithoutChildren(t *testing.T) {
	registry := NewRegistry("parent-1")
	tool := &manageSubagentsTool{registry: registry}
	args, _ := json.Marshal(manageSubagentsArgs{Action: "kill_all"})
	res, err := tool.Execute(context.Background(), "call-1", args, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	body := res.Content[0].(pi.TextContent)
	if !strings.HasSuffix(body.Text, "No active subagents to kill.") {
		t.Fatalf("kill_all result %q does not carry the reference's sentence", body.Text)
	}
}

// TestRegistryBusyFollowsTheChildren pins the wait condition the print
// path uses: the parent waits for a report only while a child is working.
func TestRegistryBusyFollowsTheChildren(t *testing.T) {
	registry := NewRegistry("parent-1")
	if registry.Busy() {
		t.Fatal("an empty registry is busy")
	}
	child := NewChild("child-1", "research", "Tester", t.TempDir(), func() {})
	registry.Add(child)
	if !registry.Busy() {
		t.Fatal("a running child does not make the registry busy")
	}
	child.setState(StateIdle)
	if registry.Busy() {
		t.Fatal("an idle child still makes the registry busy")
	}
}

// TestLaunchDeliversTheChildsAnswerToTheParent is the end-to-end shape of
// the asynchronous delegation:
//
//   - the launch returns the acknowledgement at once;
//   - the child runs in the background;
//   - the child's answer reaches the parent as a message.
//
// 【一手】 the 1.4.4 capture: the delivery is a user-role content part
// carrying the child's answer (docs/cycles/subagent-async-framing-capture/deliverable.md).
func TestLaunchDeliversTheChildsAnswerToTheParent(t *testing.T) {
	dir := t.TempDir()
	registry := NewRegistry("parent-1")
	type delivery struct {
		sender  string
		content string
	}
	deliveries := make(chan delivery, 4)
	registry.SetNotify(func(sender, content string) {
		deliveries <- delivery{sender: sender, content: content}
	})
	defer registry.Close()

	deps := Deps{
		Services:       subagentTestServices(t),
		Model:          subagentTestModel(),
		CWD:            dir,
		AgentDir:       filepath.Join(dir, "agent"),
		Registry:       registry,
		ConversationID: "parent-1",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	launched, err := Launch(ctx, Invocation{Prompt: "What is 20+22?"}, deps)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if launched.Child == nil {
		t.Fatal("Launch returned no child")
	}
	if launched.Acknowledgement.ConversationID != launched.Child.ConversationID {
		t.Fatalf("the acknowledgement names %q, want the child's %q",
			launched.Acknowledgement.ConversationID, launched.Child.ConversationID)
	}

	select {
	case got := <-deliveries:
		if got.sender != launched.Child.ConversationID {
			t.Fatalf("the delivery came from %q, want the child's %q", got.sender, launched.Child.ConversationID)
		}
		if got.content != "42" {
			t.Fatalf("the delivery carried %q, want the child's answer %q", got.content, "42")
		}
	case <-ctx.Done():
		t.Fatal("the parent was never notified")
	}
}

// TestChildsFirstMessageIsTheEnvelope pins the reference's own framing: the
// child receives the task as a message from the agent that invoked it.
//
// 【一手】 the 1.4.4 capture: "The child sees the mirror image: its first
// user message is the envelope carrying the parent's Prompt, with sender =
// the parent's conversation id."
func TestChildsFirstMessageIsTheEnvelope(t *testing.T) {
	base := agentmessage.Render("parent-1", "What is 20+22?", time.Now(), agentmessage.PriorityHigh)
	if !strings.HasPrefix(base, "The following is a <SYSTEM_MESSAGE>") {
		t.Fatalf("the envelope %q does not start with the reference's lead-in", base)
	}
	if !strings.Contains(base, "sender=parent-1") {
		t.Fatalf("the envelope %q does not name the parent's conversation id", base)
	}
	if !strings.Contains(base, "content=What is 20+22?") {
		t.Fatalf("the envelope %q does not carry the task", base)
	}
}

// TestChildSystemPromptCarriesTheTaskInstruction pins the instruction the
// reference gives a delegated agent.
//
// 【反编译】 agy 1.4.4's `handlers.(*InvokeSubagentHandler).launchSubagent`
// carries this 224-byte literal as the subagent's system instruction:
//
//	Your task arrives as a system message from the agent that invoked you. Treat that message exactly as if the user had sent it to you directly: it is the user's complete request, and you respond to it as you would to the user.
//
// The `self` type inherits the parent's prompt as it is — the reference's
// own words are "inherits the parent agent's full configuration including
// tools, system prompt, and model" — so the instruction is not added there.
func TestChildSystemPromptCarriesTheTaskInstruction(t *testing.T) {
	got := childSystemPrompt("You are a subagent acting as: Tester.\n\nbase prompt", Invocation{})
	if !strings.HasPrefix(got, taskInstruction) {
		t.Fatalf("the child's prompt %q does not start with the task instruction", got)
	}
	if !strings.HasSuffix(got, "base prompt") {
		t.Fatalf("the child's prompt %q does not carry the base prompt", got)
	}

	// The `self` type keeps the parent's prompt as it is.
	got = childSystemPrompt("the parent's own prompt", Invocation{InheritPrompt: true})
	if got != "the parent's own prompt" {
		t.Fatalf("the inherited prompt = %q, want the parent's own prompt", got)
	}
}
