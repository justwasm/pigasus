package agentturn

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/actor"
	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/core/backend"
)

// mockMultiStepClient simulates a model that first calls a tool, and then produces a final answer.
type mockMultiStepClient struct {
	turn int
}

func (m *mockMultiStepClient) Stream(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
	out := make(chan backend.StreamChunk, 8)
	go func() {
		defer close(out)
		m.turn++
		if m.turn == 1 {
			// Step 1: Model emits thought and calls a tool
			out <- backend.StreamChunk{
				Text: "<think>I should multiply 10 by 2</think>",
			}
			out <- backend.StreamChunk{
				ToolCalls: []backend.ToolCall{
					{
						ID:        "call_calc_1",
						Name:      "double",
						Arguments: json.RawMessage(`{"val":10}`),
					},
				},
				Done: true,
			}
			return
		}

		// Step 2: Model receives tool result and produces final answer
		out <- backend.StreamChunk{
			Text: "The doubled value is 20.",
		}
		out <- backend.StreamChunk{
			Done: true,
			Meta: &backend.ChunkMeta{
				PromptTokens:     15,
				CompletionTokens: 8,
				TotalTokens:      23,
			},
		}
	}()
	return out, nil
}

func TestLocalTurn_ActorExecution(t *testing.T) {
	mockClient := &mockMultiStepClient{}

	engine := NewLocal(func(_ context.Context, model string) (Env, func()) {
		return Env{
			Backend: mockClient,
			Model:   "test-actor-model",
			Tools: []backend.ToolDecl{
				{
					Name:        "double",
					Description: "doubles a value",
				},
			},
			ToolRunner: runnerFunc(func(ctx context.Context, call backend.ToolCall) (string, error) {
				var args struct {
					Val int `json:"val"`
				}
				_ = json.Unmarshal(call.Arguments, &args)
				return "20", nil
			}),
		}, func() {}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	frames, err := engine.Turn(ctx, Turn{
		History: []agentlink.Message{
			{Role: "user", Content: "Please double 10"},
		},
	})
	if err != nil {
		t.Fatalf("Turn error: %v", err)
	}

	got := collect(t, frames)

	var thoughtText strings.Builder
	var outputText strings.Builder
	var toolStartSeen bool
	var toolEndSeen bool
	var resultFrame *agentlink.Frame

	for _, f := range got {
		switch f.Kind {
		case agentlink.KindThought:
			thoughtText.WriteString(f.Text)
		case agentlink.KindText:
			outputText.WriteString(f.Text)
		case agentlink.KindTool:
			if f.Tool != nil {
				if f.Tool.Phase == agentlink.ToolStart {
					toolStartSeen = true
				}
				if f.Tool.Phase == agentlink.ToolEnd {
					toolEndSeen = true
				}
			}
		case agentlink.KindResult:
			resultFrame = &f
		}
	}

	if !strings.Contains(thoughtText.String(), "multiply 10 by 2") {
		t.Errorf("expected thought to contain 'multiply 10 by 2', got: %q", thoughtText.String())
	}
	if !toolStartSeen {
		t.Error("expected ToolStart frame")
	}
	if !toolEndSeen {
		t.Error("expected ToolEnd frame")
	}
	if !strings.Contains(outputText.String(), "The doubled value is 20.") {
		t.Errorf("expected text output 'The doubled value is 20.', got: %q", outputText.String())
	}
	if resultFrame == nil {
		t.Fatal("expected result frame")
	}
	if resultFrame.Result == nil || !strings.Contains(resultFrame.Result.Reply, "The doubled value is 20.") {
		t.Errorf("result reply mismatch: %+v", resultFrame.Result)
	}
	if resultFrame.Usage == nil || resultFrame.Usage.TotalTokens != 23 {
		t.Errorf("expected 23 total tokens, got: %+v", resultFrame.Usage)
	}
}

func TestLocalTurn_ActorMultiTurnHistory(t *testing.T) {
	mockClient := &mockMultiStepClient{}

	engine := NewLocal(func(_ context.Context, model string) (Env, func()) {
		return Env{
			Backend: mockClient,
			Model:   "test-actor-model",
			Tools: []backend.ToolDecl{
				{
					Name:        "double",
					Description: "doubles a value",
				},
			},
			ToolRunner: runnerFunc(func(ctx context.Context, call backend.ToolCall) (string, error) {
				return "20", nil
			}),
		}, func() {}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Multi-turn history: previous user and assistant turns
	frames, err := engine.Turn(ctx, Turn{
		History: []agentlink.Message{
			{Role: "user", Content: "Hello assistant"},
			{Role: "assistant", Content: "Hello! How can I help?"},
			{Role: "user", Content: "Please double 10"},
		},
	})
	if err != nil {
		t.Fatalf("Turn error: %v", err)
	}

	got := collect(t, frames)
	var outputText strings.Builder
	var resultFrame *agentlink.Frame
	for _, f := range got {
		if f.Kind == agentlink.KindText {
			outputText.WriteString(f.Text)
		} else if f.Kind == agentlink.KindResult {
			resultFrame = &f
		}
	}

	if !strings.Contains(outputText.String(), "The doubled value is 20.") {
		t.Fatalf("expected text output 'The doubled value is 20.', got: %q", outputText.String())
	}
	if resultFrame == nil || resultFrame.Result == nil || !strings.Contains(resultFrame.Result.Reply, "The doubled value is 20.") {
		t.Fatalf("expected result frame with reply, got: %+v", resultFrame)
	}
}

func TestLocal_PersistentActorSessionLifecycle(t *testing.T) {
	mockClient := &mockMultiStepClient{}

	engine := NewLocal(func(_ context.Context, model string) (Env, func()) {
		return Env{
			Backend:      mockClient,
			Model:        model,
			SystemPrompt: "test system prompt",
		}, func() {}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Turn 1
	frames1, err := engine.Turn(ctx, Turn{
		Model: "model-v1",
		History: []agentlink.Message{
			{Role: "user", Content: "First turn"},
		},
	})
	if err != nil {
		t.Fatalf("Turn 1 failed: %v", err)
	}
	collect(t, frames1)

	// Check persistent actor after Turn 1
	engine.mu.Lock()
	act1 := engine.activeActor
	engine.mu.Unlock()

	if act1 == nil {
		t.Fatal("expected engine.activeActor to be retained after Turn 1")
	}
	if act1.State().State != actor.StateIdle {
		t.Errorf("expected actor to be idle after Turn 1, got %v", act1.State().State)
	}
	if act1.Trajectory().TotalTurns() != 1 {
		t.Errorf("expected 1 turn in trajectory, got %d", act1.Trajectory().TotalTurns())
	}
	if act1.Model() != "model-v1" {
		t.Errorf("actor.Model = %q, want 'model-v1'", act1.Model())
	}

	// Turn 2 on same engine with updated model
	frames2, err := engine.Turn(ctx, Turn{
		Model: "model-v2",
		History: []agentlink.Message{
			{Role: "user", Content: "First turn"},
			{Role: "assistant", Content: "Reply to first turn"},
			{Role: "user", Content: "Second turn"},
		},
	})
	if err != nil {
		t.Fatalf("Turn 2 failed: %v", err)
	}
	collect(t, frames2)

	// Verify actor was REUSED across turns
	engine.mu.Lock()
	act2 := engine.activeActor
	engine.mu.Unlock()

	if act2 != act1 {
		t.Errorf("expected actor to be reused across turns (act1 == act2), got different instances")
	}
	if act2.Model() != "model-v2" {
		t.Errorf("actor.Model after Turn 2 = %q, want 'model-v2'", act2.Model())
	}
	if act2.Trajectory().TotalTurns() != 2 {
		t.Errorf("expected 2 turns in trajectory, got %d", act2.Trajectory().TotalTurns())
	}

	// Turn 3: history reset (cleared conversation)
	frames3, err := engine.Turn(ctx, Turn{
		Model: "model-v3",
		History: []agentlink.Message{
			{Role: "user", Content: "Fresh new session prompt"},
		},
	})
	if err != nil {
		t.Fatalf("Turn 3 failed: %v", err)
	}
	collect(t, frames3)

	// Verify old actor was closed and replaced with a fresh actor
	engine.mu.Lock()
	act3 := engine.activeActor
	engine.mu.Unlock()

	if act3 == act1 {
		t.Errorf("expected old actor to be replaced on conversation reset, but was identical")
	}
	if act1.State().State != actor.StateClosed {
		t.Errorf("expected old actor to be closed after reset, got state %v", act1.State().State)
	}
	if act3.Trajectory().TotalTurns() != 1 {
		t.Errorf("expected 1 turn in new actor trajectory, got %d", act3.Trajectory().TotalTurns())
	}

	// Close engine
	if err := engine.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if act3.State().State != actor.StateClosed {
		t.Errorf("expected active actor to be closed after engine.Close(), got %v", act3.State().State)
	}
}

type testApprovalTool struct{}

func (a *testApprovalTool) Name() string        { return "double" }
func (a *testApprovalTool) Description() string { return "doubles" }
func (a *testApprovalTool) Definition() actor.ToolDefinition {
	return actor.ToolDefinition{Name: "double"}
}
func (a *testApprovalTool) RequiresApproval(input string) bool { return true }
func (a *testApprovalTool) Execute(ctx context.Context, input string) (actor.ToolResult, error) {
	return actor.ToolResult{Output: "20"}, nil
}

func TestLocal_PermissionHandlerApproval(t *testing.T) {
	mockClient := &mockMultiStepClient{}

	engine := NewLocal(func(_ context.Context, model string) (Env, func()) {
		return Env{
			Backend:    mockClient,
			Model:      "test-actor-model",
			ExtraTools: []actor.Tool{&testApprovalTool{}},
		}, func() {}
	})

	var approvalCalled bool
	engine.SetPermissionHandler(func(ctx context.Context, req PermissionRequest) (PermissionDecision, error) {
		approvalCalled = true
		return PermissionDecision{
			Approved: true,
			OptionID: "allow",
		}, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	frames, err := engine.Turn(ctx, Turn{
		History: []agentlink.Message{
			{Role: "user", Content: "Please double 10"},
		},
	})
	if err != nil {
		t.Fatalf("Turn error: %v", err)
	}

	got := collect(t, frames)
	if len(got) == 0 {
		t.Fatal("expected frames")
	}
	if !approvalCalled {
		t.Fatal("expected permission handler to be invoked for tool requiring approval")
	}
}

func TestChunkSink_Execution(t *testing.T) {
	var gotText strings.Builder
	var doneSeen bool
	sink := NewChunkSink(ChunkSinkOptions{
		Handler: func(chunk backend.StreamChunk) error {
			gotText.WriteString(chunk.Text)
			if chunk.Done {
				doneSeen = true
			}
			return nil
		},
	})

	sink.OnDelta("hello ")
	sink.OnDelta("world")
	sink.OnTurnCompleted(&actor.TurnSummary{
		Status: "done",
	})
	_ = sink.Finish()

	if gotText.String() != "hello world" {
		t.Errorf("got %q, want 'hello world'", gotText.String())
	}
	if !doneSeen {
		t.Error("expected done chunk to be delivered")
	}
}

func TestLocalTurn_CancelDuringToolExecution_NoPanic(t *testing.T) {
	toolStarted := make(chan struct{})
	continueTool := make(chan struct{})

	mockClient := &mockMultiStepClient{}

	engine := NewLocal(func(_ context.Context, model string) (Env, func()) {
		return Env{
			Backend: mockClient,
			Model:   "test-model",
			Tools: []backend.ToolDecl{
				{
					Name:        "double",
					Description: "doubles a value",
				},
			},
			ToolRunner: runnerFunc(func(ctx context.Context, call backend.ToolCall) (string, error) {
				close(toolStarted)
				// Wait until cancelled
				<-continueTool
				return "20", nil
			}),
		}, func() {}
	})

	ctx, cancel := context.WithCancel(context.Background())

	frames, err := engine.Turn(ctx, Turn{
		History: []agentlink.Message{
			{Role: "user", Content: "Please double 10"},
		},
	})
	if err != nil {
		t.Fatalf("Turn error: %v", err)
	}

	// Wait for tool to start
	<-toolStarted

	// Cancel the turn context while tool is running!
	cancel()

	// Drain frames until channel closes
	for range frames {
	}

	// Now unblock the tool execution. When it finishes, frameRunner.Run
	// will call r.emit(...) on the now-closed frame channel.
	// This must NOT panic!
	close(continueTool)

	// Give a moment for any background goroutine to execute tool return logic
	time.Sleep(50 * time.Millisecond)
}

func TestLocalTurn_SteerBufferingAndForwarding(t *testing.T) {
	mockClient := &mockMultiStepClient{}

	engine := NewLocal(func(_ context.Context, model string) (Env, func()) {
		return Env{
			Backend: mockClient,
			Model:   "test-model",
		}, func() {}
	})

	// Steer before turn starts (buffered in engine)
	if err := engine.Steer("guidance 1"); err != nil {
		t.Fatalf("Steer failed: %v", err)
	}
	pending := engine.PendingSteer()
	if len(pending) != 1 || pending[0] != "guidance 1" {
		t.Fatalf("expected pending steer ['guidance 1'], got %v", pending)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	frames, err := engine.Turn(ctx, Turn{
		History: []agentlink.Message{
			{Role: "user", Content: "Hello"},
		},
	})
	if err != nil {
		t.Fatalf("Turn failed: %v", err)
	}
	for range frames {
	}

	// Now steer active actor
	if err := engine.Steer("guidance 2"); err != nil {
		t.Fatalf("Steer failed: %v", err)
	}
	popped, ok := engine.PopSteer()
	if !ok || popped != "guidance 2" {
		t.Fatalf("expected popped 'guidance 2', got (%q, %v)", popped, ok)
	}
}

type thresholdTestClient struct {
	promptTokens int
	summaryText  string
}

func (c *thresholdTestClient) Stream(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
	out := make(chan backend.StreamChunk, 4)
	go func() {
		defer close(out)
		// If this is a summary request (marked by continuation summary instructions in the user message)
		if len(req.Messages) > 0 && strings.Contains(req.Messages[len(req.Messages)-1].Content, "continuation summary") {
			out <- backend.StreamChunk{
				Text: c.summaryText,
			}
			out <- backend.StreamChunk{
				Done: true,
			}
			return
		}

		out <- backend.StreamChunk{
			Text: "Hello there!",
		}
		out <- backend.StreamChunk{
			Done: true,
			Meta: &backend.ChunkMeta{
				PromptTokens:     c.promptTokens,
				CompletionTokens: 10,
				TotalTokens:      c.promptTokens + 10,
			},
		}
	}()
	return out, nil
}

func TestLocalTurn_ThresholdAutoCompaction(t *testing.T) {
	// gpt-oss-120b-medium has ckptLimit: 80_000, ckptOutput: 8_192
	// Threshold = 80_000 - 8_192 = 71_808 tokens.
	client := &thresholdTestClient{
		promptTokens: 75_000, // Exceeds threshold!
		summaryText:  "<summary>\n1. Primary Request: Solve math.\n2. Key Technical Concepts: Algebra.\n</summary>",
	}

	engine := NewLocal(func(_ context.Context, model string) (Env, func()) {
		return Env{
			Backend:      client,
			Model:        "gpt-oss-120b-medium",
			SystemPrompt: "test system prompt",
		}, func() {}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	frames, err := engine.Turn(ctx, Turn{
		Model: "gpt-oss-120b-medium",
		History: []agentlink.Message{
			{Role: "user", Content: "Calculate everything"},
		},
	})
	if err != nil {
		t.Fatalf("Turn failed: %v", err)
	}

	got := collect(t, frames)

	var compactStartSeen bool
	var compactEndSeen bool
	var endSummary string
	var tokensBefore int
	var tokensAfter int

	for _, f := range got {
		if f.Kind == agentlink.KindCompact && f.Compact != nil {
			if f.Compact.Phase == agentlink.CompactStart {
				compactStartSeen = true
				if f.Compact.Reason != "threshold" {
					t.Errorf("expected reason 'threshold', got %q", f.Compact.Reason)
				}
			}
			if f.Compact.Phase == agentlink.CompactEnd {
				compactEndSeen = true
				endSummary = f.Compact.Summary
				tokensBefore = f.Compact.TokensBefore
				tokensAfter = f.Compact.TokensAfter
			}
		}
	}

	if !compactStartSeen {
		t.Error("expected KindCompact Start frame to be emitted")
	}
	if !compactEndSeen {
		t.Error("expected KindCompact End frame to be emitted")
	}
	if !strings.Contains(endSummary, "Primary Request") {
		t.Errorf("expected endSummary to contain 'Primary Request', got %q", endSummary)
	}
	if tokensBefore != 75_000 {
		t.Errorf("expected tokensBefore 75000, got %d", tokensBefore)
	}
	if tokensAfter <= 0 {
		t.Errorf("expected positive tokensAfter, got %d", tokensAfter)
	}

	// Verify the Actor's history was reset with the resuming block
	engine.mu.Lock()
	act := engine.activeActor
	engine.mu.Unlock()

	if act == nil {
		t.Fatal("expected activeActor to be present")
	}

	turns := act.Trajectory().Turns()
	if len(turns) != 1 {
		t.Fatalf("expected 1 turn in actor trajectory after compaction reset, got %d", len(turns))
	}
	userMsg, ok := turns[0].Input.(*actor.UserMessage)
	if !ok {
		t.Fatalf("expected turn input to be UserMessage, got %T", turns[0].Input)
	}
	if !strings.HasPrefix(userMsg.Text, "# Resuming from a compaction") {
		t.Errorf("expected resuming block header in actor history, got: %q", userMsg.Text)
	}
	if !strings.Contains(userMsg.Text, "Calculate everything") {
		t.Errorf("expected user request 'Calculate everything' preserved in resuming block, got: %q", userMsg.Text)
	}
}
