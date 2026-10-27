package remotecontrol

import (
	"context"
	"testing"

	"github.com/justwasm/pigasus/internal/convstore"
)

// promptRecorder is a backend that keeps every prompt it was asked to
// run, so a test can look at what the model was given.
type promptRecorder struct {
	prompts []MessagePrompt
	reply   string
}

// Send records the prompt and answers with the canned reply.
func (p *promptRecorder) Send(_ context.Context, prompt MessagePrompt) (MessageResult, error) {
	p.prompts = append(p.prompts, prompt)
	return MessageResult{Reply: p.reply, StopReason: StopReasonEndTurn}, nil
}

// SendStream records the prompt and streams the canned reply, so the
// turn's steps land in the conversation store the way they do in
// production.
func (p *promptRecorder) SendStream(_ context.Context, prompt MessagePrompt, sink EventSink, _ MessageSendOptions) (MessageResult, error) {
	p.prompts = append(p.prompts, prompt)
	if sink != nil {
		sink.OnState(nil, StateFrame{Phase: StatePhaseStarting})
		if p.reply != "" {
			sink.OnTextDelta(nil, p.reply)
		}
		sink.OnState(nil, StateFrame{Phase: StatePhaseDone, StopReason: StopReasonEndTurn})
	}
	return MessageResult{Reply: p.reply, StopReason: StopReasonEndTurn}, nil
}

// TestTheWebChatIsOneConversation checks that the second message of a
// conversation is answered against the first one.
//
// The Web UI's composer sends one message at a time, so the turn before
// this one is not in the prompt the server receives. The conversation
// store holds it, and the turn has to be replayed into the model —
// otherwise every message is its own context.
func TestTheWebChatIsOneConversation(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())

	rec := &promptRecorder{reply: "the first answer"}
	disp, err := NewDispatcher(Config{
		Session:        NewSessionState("1.4.7"),
		MessageBackend: rec,
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	ctx := context.Background()
	if _, err := disp.DispatchDirect(ctx, "StartCascade", `{"cascadeId":"c1"}`); err != nil {
		t.Fatalf("StartCascade: %v", err)
	}
	if _, err := disp.DispatchDirect(ctx, "SendUserCascadeMessage",
		`{"cascadeId":"c1","items":[{"text":"hi"}]}`); err != nil {
		t.Fatalf("the first message: %v", err)
	}
	if _, err := disp.DispatchDirect(ctx, "SendUserCascadeMessage",
		`{"cascadeId":"c1","items":[{"text":"and then?"}]}`); err != nil {
		t.Fatalf("the second message: %v", err)
	}

	if len(rec.prompts) != 2 {
		t.Fatalf("the backend ran %d turns, want 2", len(rec.prompts))
	}
	if len(rec.prompts[0].History) != 0 {
		t.Errorf("the first turn carries a history: %v", rec.prompts[0].History)
	}
	want := []MessageHistoryTurn{
		{Role: "user", Text: "hi"},
		{Role: "assistant", Text: "the first answer"},
	}
	got := rec.prompts[1].History
	if len(got) != len(want) {
		t.Fatalf("the second turn's history = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("the second turn's history[%d] = %v, want %v", i, got[i], want[i])
		}
	}
	if rec.prompts[1].Text != "and then?" {
		t.Errorf("the second turn's prompt = %q, want %q", rec.prompts[1].Text, "and then?")
	}
}

// TestAResumedConversationCarriesItsHistory checks the other way in:
// a conversation the daemon has no cascade for — the one the Web UI
// opens from the sidebar — is replayed too.
//
// 【一手】 the reference's own language server loads the trajectory from
// the db for such a conversation (CascadeConfig's `useDbTrajectory`),
// so the model sees the earlier turns.
func TestAResumedConversationCarriesItsHistory(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())

	stored := []convstore.Step{
		{Idx: 0, Type: convstore.StepUserInput, Text: "the first question"},
		{Idx: 1, Type: convstore.StepModelResponse, Text: "the first answer"},
	}
	if err := convstore.AppendConversationSteps("c-resumed", "/workspace/openagy", stored); err != nil {
		t.Fatalf("AppendConversationSteps: %v", err)
	}

	rec := &promptRecorder{reply: "the second answer"}
	disp, err := NewDispatcher(Config{
		Session:        NewSessionState("1.4.7"),
		MessageBackend: rec,
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	ctx := context.Background()
	if _, err := disp.DispatchDirect(ctx, "SendUserCascadeMessage",
		`{"cascadeId":"c-resumed","items":[{"text":"and then?"}]}`); err != nil {
		t.Fatalf("the message: %v", err)
	}

	if len(rec.prompts) != 1 {
		t.Fatalf("the backend ran %d turns, want 1", len(rec.prompts))
	}
	want := []MessageHistoryTurn{
		{Role: "user", Text: "the first question"},
		{Role: "assistant", Text: "the first answer"},
	}
	got := rec.prompts[0].History
	if len(got) != len(want) {
		t.Fatalf("the turn's history = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("the turn's history[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

// TestTheFirstMessageHasNoHistory checks that a conversation that does
// not exist yet replays nothing: the first turn of a new conversation
// has nothing before it.
func TestTheFirstMessageHasNoHistory(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())

	if got := lsConversationHistory("c-unknown"); got != nil {
		t.Errorf("the history of an unknown conversation = %v, want none", got)
	}
}
