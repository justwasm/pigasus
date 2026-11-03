// agentturn_test.go — the turn seam's own contract.
//
// The engine is what makes the local session and the remote one the same
// session: both answer with agentlink frames, so a turn renders the same
// way whichever end ran it. The tests below pin that contract from both
// ends.
package agentturn

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/core/backend"
)

// runnerFunc adapts a function to the tool runner interface.
type runnerFunc func(ctx context.Context, call backend.ToolCall) (string, error)

func (f runnerFunc) Run(ctx context.Context, call backend.ToolCall) (string, error) {
	return f(ctx, call)
}

// collect drains a turn's frames.
func collect(t *testing.T, frames <-chan agentlink.Frame) []agentlink.Frame {
	t.Helper()
	var out []agentlink.Frame
	timeout := time.After(20 * time.Second)
	for {
		select {
		case f, ok := <-frames:
			if !ok {
				return out
			}
			out = append(out, f)
		case <-timeout:
			t.Fatal("the turn did not finish in time")
			return out
		}
	}
}

// TestLocalTurnAnswers pins the local engine's contract: the answer
// arrives as text frames, and the turn ends with a result frame that
// carries the whole answer.
func TestLocalTurnAnswers(t *testing.T) {
	engine := NewLocal(func(_ context.Context, model string) (Env, func()) {
		return Env{
			Backend:      backend.NewMock(),
			Model:        "test-model",
			SystemPrompt: "you are a test",
		}, func() {}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	frames, err := engine.Turn(ctx, Turn{
		History: []agentlink.Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	got := collect(t, frames)

	var text strings.Builder
	var result *agentlink.Frame
	for i := range got {
		f := got[i]
		switch f.Kind {
		case agentlink.KindText:
			text.WriteString(f.Text)
		case agentlink.KindResult:
			result = &f
		}
	}
	if result == nil {
		t.Fatalf("the turn ended without a result frame; got %v", kinds(got))
	}
	if !strings.Contains(text.String(), "mock reply") {
		t.Errorf("the text frames carry %q, want the mock's answer", text.String())
	}
	if result.Result == nil {
		t.Fatal("the result frame carries no result")
	}
	if !strings.Contains(result.Result.Reply, "mock reply") {
		t.Errorf("the result's reply is %q, want the mock's answer", result.Result.Reply)
	}
	if result.Usage == nil {
		t.Error("the result frame carries no usage")
	}
}

// TestLocalTurnUsesTheTurnsModel pins that the turn's own model wins over
// the environment's: the client asks for a model per turn, and the
// environment's is only the default.
func TestLocalTurnUsesTheTurnsModel(t *testing.T) {
	var asked string
	engine := NewLocal(func(_ context.Context, model string) (Env, func()) {
		asked = model
		return Env{Backend: backend.NewMock(), Model: "environment-model"}, func() {}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	frames, err := engine.Turn(ctx, Turn{Model: "turn-model"})
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	collect(t, frames)
	if asked != "turn-model" {
		t.Errorf("the environment was built for %q, want %q", asked, "turn-model")
	}
}

// TestLocalTurnWithoutATransport pins that a turn with no transport
// fails before it starts, rather than answering with an empty turn.
func TestLocalTurnWithoutATransport(t *testing.T) {
	engine := NewLocal(func(_ context.Context, _ string) (Env, func()) {
		return Env{Model: "test-model"}, func() {}
	})
	if _, err := engine.Turn(context.Background(), Turn{}); err != ErrNoTransport {
		t.Errorf("Turn error = %v, want ErrNoTransport", err)
	}
}

// TestFrameRunnerReportsTheCall pins that a tool call is reported twice:
// once when it starts, so the reader can paint the row while the tool
// runs, and once when it ends, with the tool's own output.
func TestFrameRunnerReportsTheCall(t *testing.T) {
	var frames []agentlink.Frame
	emit := func(f agentlink.Frame) bool {
		frames = append(frames, f)
		return true
	}
	var steps []agentlink.Step
	r := &frameRunner{
		inner: runnerFunc(func(_ context.Context, _ backend.ToolCall) (string, error) {
			return "the output", nil
		}),
		emit:  emit,
		steps: &steps,
	}
	res, err := r.Run(context.Background(), backend.ToolCall{
		Name:      "run_command",
		Arguments: json.RawMessage(`{"CommandLine":"ls"}`),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res != "the output" {
		t.Errorf("the runner returned %q, want %q", res, "the output")
	}
	if len(frames) != 2 {
		t.Fatalf("the call was reported %d times, want 2", len(frames))
	}
	if frames[0].Kind != agentlink.KindTool || frames[0].Tool == nil || frames[0].Tool.Phase != agentlink.ToolStart {
		t.Errorf("the first frame is %+v, want a tool start", frames[0])
	}
	if frames[1].Kind != agentlink.KindTool || frames[1].Tool == nil || frames[1].Tool.Phase != agentlink.ToolEnd {
		t.Errorf("the second frame is %+v, want a tool end", frames[1])
	}
	if frames[1].Tool.Result != "the output" {
		t.Errorf("the tool end carries %q, want %q", frames[1].Tool.Result, "the output")
	}
	if len(steps) != 1 || steps[0].Name != "run_command" {
		t.Errorf("the finished steps are %+v, want one run_command step", steps)
	}
}

// TestToWireMessagesRoundTrip pins that the wire's messages carry the
// model's own: the role, the text, the tool calls and the images.
func TestToWireMessagesRoundTrip(t *testing.T) {
	in := []backend.Message{{
		Role:    "assistant",
		Content: "the answer",
		ToolCalls: []backend.ToolCall{{
			ID:        "call-1",
			Name:      "run_command",
			Arguments: json.RawMessage(`{"CommandLine":"ls"}`),
		}},
		Images: []backend.ImagePart{{MIMEType: "image/png", Data: []byte{1, 2, 3}}},
	}}
	wire := ToWireMessages(in)
	if len(wire) != 1 {
		t.Fatalf("the wire carries %d messages, want 1", len(wire))
	}
	if wire[0].Role != "assistant" || wire[0].Content != "the answer" {
		t.Errorf("the wire message is %+v, want the model's own", wire[0])
	}
	if len(wire[0].ToolCalls) != 1 || wire[0].ToolCalls[0].Name != "run_command" {
		t.Errorf("the wire message carries %+v, want the model's tool call", wire[0].ToolCalls)
	}
	if len(wire[0].Images) != 1 || wire[0].Images[0].MIMEType != "image/png" {
		t.Errorf("the wire message carries %+v, want the model's image", wire[0].Images)
	}
	back := ToBackendMessages(wire)
	if len(back) != 1 || back[0].Content != "the answer" {
		t.Errorf("the round trip lost the message: %+v", back)
	}
}

// kinds names a frame list for a failure message.
func kinds(frames []agentlink.Frame) []agentlink.Kind {
	out := make([]agentlink.Kind, 0, len(frames))
	for _, f := range frames {
		out = append(out, f.Kind)
	}
	return out
}
