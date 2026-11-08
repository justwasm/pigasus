package actor_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/justwasm/pigasus/actor"
)

func TestActor_UserTurn(t *testing.T) {
	mock := actor.NewMockProvider()
	mock.EnqueueChunks([]actor.StreamChunk{
		{Thought: "Thinking through the user question..."},
		{Delta: "Hello "},
		{Delta: "world!"},
		{
			FinishReason: "stop",
			Usage: &actor.TokenUsage{
				PromptTokens: 10,
				OutputTokens: 5,
				TotalTokens:  15,
			},
		},
	})

	act, err := actor.Spawn(actor.Config{
		Default: actor.TurnConfig{
			SystemPrompt: "You are a helpful test assistant.",
			Provider:     mock,
		},
	})
	if err != nil {
		t.Fatalf("failed to spawn actor: %v", err)
	}
	defer act.Close()

	turn, err := act.Prompt(context.Background(), "Hi there")
	if err != nil {
		t.Fatalf("failed to start turn: %v", err)
	}
	events := turn.Events()

	var thoughts strings.Builder
	var output strings.Builder
	var completed bool

	timeout := time.After(2 * time.Second)
	for !completed {
		select {
		case <-timeout:
			t.Fatal("timed out waiting for turn completion")
		case ev := <-events:
			switch ev.Type {
			case actor.EventThought:
				thoughts.WriteString(ev.Thought)
			case actor.EventDelta:
				output.WriteString(ev.Delta)
			case actor.EventTurnCompleted:
				completed = true
				if ev.TurnSummary.Output != "Hello world!" {
					t.Errorf("expected summary output 'Hello world!', got %q", ev.TurnSummary.Output)
				}
				if ev.TurnSummary.TotalTokens != 15 {
					t.Errorf("expected 15 total tokens, got %d", ev.TurnSummary.TotalTokens)
				}
			}
		}
	}

	if thoughts.String() != "Thinking through the user question..." {
		t.Errorf("unexpected thought: %q", thoughts.String())
	}
	if output.String() != "Hello world!" {
		t.Errorf("unexpected output: %q", output.String())
	}

	// Verify trajectory
	traj := act.Trajectory()
	if traj.TotalTurns() != 1 {
		t.Fatalf("expected 1 turn in trajectory, got %d", traj.TotalTurns())
	}
	record := traj.LastTurn()
	if record.FinalOutput != "Hello world!" {
		t.Errorf("expected turn final output 'Hello world!', got %q", record.FinalOutput)
	}
}

func TestActor_StartTurnHandle(t *testing.T) {
	mock := actor.NewMockProvider()
	mock.EnqueueChunks([]actor.StreamChunk{
		{Delta: "Direct ask answer"},
		{FinishReason: "stop"},
	})

	act, err := actor.Spawn(actor.Config{
		Default: actor.TurnConfig{
			Provider: mock,
		},
	})
	if err != nil {
		t.Fatalf("failed to spawn actor: %v", err)
	}
	defer act.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	stream, err := act.StartTurn(ctx, actor.TurnRequest{Prompt: "Tell me something"})
	if err != nil {
		t.Fatalf("failed to ask: %v", err)
	}

	var deltas strings.Builder
	var completed bool
	for ev := range stream.Events() {
		if ev.Type == actor.EventDelta {
			deltas.WriteString(ev.Delta)
		}
		if ev.Type == actor.EventTurnCompleted {
			completed = true
		}
	}

	if !completed {
		t.Error("expected turn completion in the handle stream")
	}
	if deltas.String() != "Direct ask answer" {
		t.Errorf("got %q, want 'Direct ask answer'", deltas.String())
	}
}

func TestActor_ToolCalling_Autonomous(t *testing.T) {
	mock := actor.NewMockProvider()

	// Step 1: Model decides to call adder tool
	mock.EnqueueChunks([]actor.StreamChunk{
		{
			ToolCalls: []actor.ToolCall{
				{
					ID:    "call_add_1",
					Name:  "add",
					Input: `{"a": 17, "b": 25}`,
				},
			},
		},
	})

	// Step 2: Model receives tool result 42 and returns final answer
	mock.EnqueueChunks([]actor.StreamChunk{
		{Delta: "The sum of 17 and 25 is 42."},
		{FinishReason: "stop"},
	})

	// Register add tool
	addTool := actor.NewFuncTool("add", "adds two numbers", func(ctx context.Context, input string) (string, error) {
		var args struct {
			A int `json:"a"`
			B int `json:"b"`
		}
		if err := json.Unmarshal([]byte(input), &args); err != nil {
			return "", err
		}
		return fmt.Sprintf("%d", args.A+args.B), nil
	})

	act, err := actor.Spawn(actor.Config{
		Default: actor.TurnConfig{
			Provider: mock,
			Tools:    []actor.Tool{addTool},
		},
	})
	if err != nil {
		t.Fatalf("failed to spawn actor: %v", err)
	}
	defer act.Close()

	turn, err := act.Prompt(context.Background(), "What is 17 + 25?")
	if err != nil {
		t.Fatalf("failed to start turn: %v", err)
	}
	events := turn.Events()

	var toolStarted bool
	var toolFinished bool
	var toolResultOutput string
	var finalOutput strings.Builder
	var completed bool

	timeout := time.After(2 * time.Second)
	for !completed {
		select {
		case <-timeout:
			t.Fatal("timed out waiting for multi-step tool call turn")
		case ev := <-events:
			switch ev.Type {
			case actor.EventToolCallStarted:
				if ev.ToolCall.Name == "add" {
					toolStarted = true
				}
			case actor.EventToolCallFinished:
				if ev.ToolResult.ToolCallID == "call_add_1" {
					toolFinished = true
					toolResultOutput = ev.ToolResult.Output
				}
			case actor.EventDelta:
				finalOutput.WriteString(ev.Delta)
			case actor.EventTurnCompleted:
				completed = true
			}
		}
	}

	if !toolStarted {
		t.Error("EventToolCallStarted was not received")
	}
	if !toolFinished {
		t.Error("EventToolCallFinished was not received")
	}
	if toolResultOutput != "42" {
		t.Errorf("expected tool output '42', got %q", toolResultOutput)
	}
	if finalOutput.String() != "The sum of 17 and 25 is 42." {
		t.Errorf("unexpected final output: %q", finalOutput.String())
	}

	// Verify request history passed to mock in step 2 contains the tool output
	if len(mock.Requests) < 2 {
		t.Fatalf("expected at least 2 requests to provider, got %d", len(mock.Requests))
	}
	step2Req := mock.Requests[1]
	hasToolMsg := false
	for _, m := range step2Req.Messages {
		if m.Role == actor.RoleTool && m.Content == "42" && m.ToolCallID == "call_add_1" {
			hasToolMsg = true
			break
		}
	}
	if !hasToolMsg {
		t.Errorf("step 2 request did not contain expected tool result: %+v", step2Req.Messages)
	}
}

func TestActor_ToolApproval_Approved(t *testing.T) {
	mock := actor.NewMockProvider()

	// Step 1: Model attempts to run deploy_service
	mock.EnqueueChunks([]actor.StreamChunk{
		{
			ToolCalls: []actor.ToolCall{
				{
					ID:    "call_deploy",
					Name:  "deploy_service",
					Input: `{"env": "production"}`,
				},
			},
		},
	})

	// Step 2: Model finishes after tool executes
	mock.EnqueueChunks([]actor.StreamChunk{
		{Delta: "Production deployment complete."},
		{FinishReason: "stop"},
	})

	// Tool with approval requirement
	deployTool := actor.NewFuncTool(
		"deploy_service",
		"deploys service to environment",
		func(ctx context.Context, input string) (string, error) {
			return "deployed successfully", nil
		},
		actor.WithApprovalGate(func(input string) bool {
			return true // Always requires approval
		}),
	)

	act, err := actor.Spawn(actor.Config{
		Default: actor.TurnConfig{
			Provider: mock,
			Tools:    []actor.Tool{deployTool},
		},
	})
	if err != nil {
		t.Fatalf("failed to spawn: %v", err)
	}
	defer act.Close()

	turn, err := act.Prompt(context.Background(), "Deploy to prod")
	if err != nil {
		t.Fatalf("failed to start turn: %v", err)
	}
	events := turn.Events()

	approvalRequiredReceived := false
	turnCompleted := false

	timeout := time.After(2 * time.Second)
	for !turnCompleted {
		select {
		case <-timeout:
			t.Fatal("timed out in approval test")
		case ev := <-events:
			if ev.Type == actor.EventApprovalRequired {
				approvalRequiredReceived = true
				if act.State().State != actor.StateWaitingApproval {
					t.Errorf("expected actor in StateWaitingApproval, got %s", act.State().State)
				}
				// Approve the tool execution!
				go func(callID string) {
					time.Sleep(10 * time.Millisecond)
					if err := act.Approve(callID, true, "approved by admin"); err != nil {
						t.Errorf("failed to approve: %v", err)
					}
				}(ev.ToolCall.ID)
			}
			if ev.Type == actor.EventTurnCompleted {
				turnCompleted = true
				if ev.TurnSummary.Output != "Production deployment complete." {
					t.Errorf("unexpected output: %q", ev.TurnSummary.Output)
				}
			}
		}
	}

	if !approvalRequiredReceived {
		t.Error("expected EventApprovalRequired")
	}
	if act.State().State != actor.StateIdle {
		t.Errorf("expected final state StateIdle, got %s", act.State().State)
	}
}

func TestActor_ToolApproval_Denied(t *testing.T) {
	mock := actor.NewMockProvider()

	// Step 1: Model calls dangerous tool
	mock.EnqueueChunks([]actor.StreamChunk{
		{
			ToolCalls: []actor.ToolCall{
				{
					ID:    "call_delete",
					Name:  "delete_database",
					Input: `{}`,
				},
			},
		},
	})

	// Step 2: Model responds to denial
	mock.EnqueueChunks([]actor.StreamChunk{
		{Delta: "I could not delete the database because approval was rejected."},
		{FinishReason: "stop"},
	})

	deleteTool := actor.NewFuncTool(
		"delete_database",
		"deletes database",
		func(ctx context.Context, input string) (string, error) {
			return "deleted", nil
		},
		actor.WithApprovalGate(func(input string) bool { return true }),
	)

	act, err := actor.Spawn(actor.Config{
		Default: actor.TurnConfig{
			Provider: mock,
			Tools:    []actor.Tool{deleteTool},
		},
	})
	if err != nil {
		t.Fatalf("failed to spawn: %v", err)
	}
	defer act.Close()

	turn, err := act.Prompt(context.Background(), "Delete the database")
	if err != nil {
		t.Fatalf("failed to start turn: %v", err)
	}
	events := turn.Events()

	turnCompleted := false
	timeout := time.After(2 * time.Second)
	for !turnCompleted {
		select {
		case <-timeout:
			t.Fatal("timed out in denial test")
		case ev := <-events:
			if ev.Type == actor.EventApprovalRequired {
				// Reject the tool call!
				go func(callID string) {
					time.Sleep(10 * time.Millisecond)
					_ = act.Approve(callID, false, "strictly forbidden")
				}(ev.ToolCall.ID)
			}
			if ev.Type == actor.EventTurnCompleted {
				turnCompleted = true
				if !strings.Contains(ev.TurnSummary.Output, "approval was rejected") {
					t.Errorf("unexpected final output: %q", ev.TurnSummary.Output)
				}
			}
		}
	}
}

func TestActor_DynamicSteering(t *testing.T) {
	mock := actor.NewMockProvider()
	var receivedPrompt string
	mock.EnqueueHandler(func(req actor.GenerateRequest) ([]actor.StreamChunk, error) {
		for _, m := range req.Messages {
			if m.Role == actor.RoleUser {
				receivedPrompt = m.Content
			}
		}
		return []actor.StreamChunk{
			{Delta: "Understood with guidance.", FinishReason: "stop"},
		}, nil
	})

	act, err := actor.Spawn(actor.Config{
		Default: actor.TurnConfig{
			Provider: mock,
		},
	})
	if err != nil {
		t.Fatalf("failed to spawn: %v", err)
	}
	defer act.Close()

	// Send steer instruction before turn
	_ = act.Steer("Always format output as JSON")
	turn, err := act.Prompt(context.Background(), "List users")
	if err != nil {
		t.Fatalf("failed to start turn: %v", err)
	}
	for range turn.Events() {
	}
	if !strings.Contains(receivedPrompt, "[Active Guidance]: Always format output as JSON") {
		t.Errorf("prompt did not receive guidance, got: %q", receivedPrompt)
	}
}

func TestActor_CancelTurn(t *testing.T) {
	mock := actor.NewMockProvider()
	blockCh := make(chan struct{})
	mock.EnqueueContextHandler(func(ctx context.Context, req actor.GenerateRequest) ([]actor.StreamChunk, error) {
		select {
		case <-blockCh:
			return []actor.StreamChunk{{Delta: "finished"}}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})

	act, err := actor.Spawn(actor.Config{
		Default: actor.TurnConfig{
			Provider: mock,
		},
	})
	if err != nil {
		t.Fatalf("failed to spawn: %v", err)
	}
	defer act.Close()
	defer close(blockCh)

	turn, err := act.Prompt(context.Background(), "Slow computation")
	if err != nil {
		t.Fatalf("failed to start turn: %v", err)
	}
	events := turn.Events()

	// Wait for turn to be running
	timeout := time.After(time.Second)
	for act.State().State != actor.StateRunning {
		select {
		case <-timeout:
			t.Fatal("turn did not enter running state")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	// Cancel the turn!
	if err := act.CancelTurn("user stopped task"); err != nil {
		t.Fatalf("failed to cancel: %v", err)
	}

	// The handle closes immediately, even though the turn loop is still
	// inside the provider call.
	for range events {
	}
	if !errors.Is(turn.Err(), actor.ErrTurnCancelled) {
		t.Errorf("turn Err() = %v, want ErrTurnCancelled", turn.Err())
	}

	deadline := time.After(time.Second)
	for act.State().State != actor.StateIdle {
		select {
		case <-deadline:
			t.Fatalf("expected StateIdle after cancellation, got %s", act.State().State)
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
}

func TestActor_Closed_ErrActorClosed(t *testing.T) {
	mock := actor.NewMockProvider()
	act, err := actor.Spawn(actor.Config{
		Default: actor.TurnConfig{
			Provider: mock,
		},
	})
	if err != nil {
		t.Fatalf("failed to spawn: %v", err)
	}

	if err := act.Close(); err != nil {
		t.Fatalf("failed to close: %v", err)
	}

	// A turn on a closed actor returns ErrActorClosed
	if _, err = act.Prompt(context.Background(), "hello after close"); err != actor.ErrActorClosed {
		t.Errorf("expected ErrActorClosed for Prompt, got %v", err)
	}
}

func TestActor_Prompt(t *testing.T) {
	mock := actor.NewMockProvider()
	mock.EnqueueChunks([]actor.StreamChunk{
		{Delta: "Direct prompt answer"},
		{FinishReason: "stop"},
	})

	act, err := actor.Spawn(actor.Config{
		Default: actor.TurnConfig{
			Provider: mock,
		},
	})
	if err != nil {
		t.Fatalf("failed to spawn: %v", err)
	}
	defer act.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Test Prompt
	evStream, err := act.Prompt(ctx, "hello from prompt")
	if err != nil {
		t.Fatalf("Prompt failed: %v", err)
	}
	var promptText strings.Builder
	for ev := range evStream.Events() {
		if ev.Type == actor.EventDelta {
			promptText.WriteString(ev.Delta)
		}
	}
	if promptText.String() != "Direct prompt answer" {
		t.Errorf("Prompt output = %q, want 'Direct prompt answer'", promptText.String())
	}

}

func TestActor_InitialHistory(t *testing.T) {
	mock := actor.NewMockProvider()
	var capturedReq actor.GenerateRequest
	mock.EnqueueHandler(func(req actor.GenerateRequest) ([]actor.StreamChunk, error) {
		capturedReq = req
		return []actor.StreamChunk{{Delta: "response", FinishReason: "stop"}}, nil
	})

	history := []actor.ConversationMessage{
		{Role: actor.RoleUser, Content: "What is your name?"},
		{Role: actor.RoleAssistant, Content: "I am Pigasus."},
		{Role: actor.RoleUser, Content: "Where are you running?"},
		{Role: actor.RoleAssistant, Content: "Inside actor runtime."},
	}

	act, err := actor.Spawn(actor.Config{
		InitialHistory: history,
		Default: actor.TurnConfig{
			Provider: mock,
		},
	})
	if err != nil {
		t.Fatalf("failed to spawn with initial history: %v", err)
	}
	defer act.Close()

	if act.Trajectory().TotalTurns() != 2 {
		t.Fatalf("expected 2 seeded turns in trajectory, got %d", act.Trajectory().TotalTurns())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	turn, err := act.Prompt(ctx, "And what is your purpose?")
	if err != nil {
		t.Fatalf("Prompt failed: %v", err)
	}
	var res strings.Builder
	for ev := range turn.Events() {
		if ev.Type == actor.EventDelta {
			res.WriteString(ev.Delta)
		}
	}
	if res.String() != "response" {
		t.Errorf("unexpected res: %s", res.String())
	}

	// Verify captured request contains all history messages + current prompt
	if len(capturedReq.Messages) != 5 {
		t.Fatalf("expected 5 messages in LLM request (4 history + 1 prompt), got %d", len(capturedReq.Messages))
	}
	if capturedReq.Messages[0].Content != "What is your name?" {
		t.Errorf("message[0] = %q", capturedReq.Messages[0].Content)
	}
	if capturedReq.Messages[1].Content != "I am Pigasus." {
		t.Errorf("message[1] = %q", capturedReq.Messages[1].Content)
	}
	if capturedReq.Messages[4].Content != "And what is your purpose?" {
		t.Errorf("message[4] = %q", capturedReq.Messages[4].Content)
	}
}

func TestActor_InitialTurns(t *testing.T) {
	mock := actor.NewMockProvider()
	mock.EnqueueResponse("ok")

	initialTurns := []*actor.TurnRecord{
		{
			ID:          "turn_1",
			Input:       "first",
			FinalOutput: "answer 1",
			Status:      "done",
		},
	}

	act, err := actor.Spawn(actor.Config{
		InitialTurns: initialTurns,
		Default: actor.TurnConfig{
			Provider: mock,
		},
	})
	if err != nil {
		t.Fatalf("failed to spawn: %v", err)
	}
	defer act.Close()

	if act.Trajectory().TotalTurns() != 1 {
		t.Fatalf("expected 1 turn in trajectory, got %d", act.Trajectory().TotalTurns())
	}
	if act.Trajectory().LastTurn().FinalOutput != "answer 1" {
		t.Errorf("expected 'answer 1', got %q", act.Trajectory().LastTurn().FinalOutput)
	}
}

func TestActor_Steer_InFlight(t *testing.T) {
	mock := actor.NewMockProvider()

	// Step 1: Model issues a tool call
	mock.EnqueueChunks([]actor.StreamChunk{
		{
			ToolCalls: []actor.ToolCall{
				{ID: "call_1", Name: "pause_tool", Input: "{}"},
			},
		},
	})

	var step2Guidance string
	// Step 2: Model checks active guidance and finishes
	mock.EnqueueHandler(func(req actor.GenerateRequest) ([]actor.StreamChunk, error) {
		for _, m := range req.Messages {
			if m.Role == actor.RoleUser {
				step2Guidance = m.Content
			}
		}
		return []actor.StreamChunk{
			{Delta: "Done with camelCase", FinishReason: "stop"},
		}, nil
	})

	toolStarted := make(chan struct{})
	continueTool := make(chan struct{})

	pauseTool := actor.NewFuncTool("pause_tool", "Pauses execution", func(ctx context.Context, input string) (string, error) {
		close(toolStarted)
		select {
		case <-continueTool:
			return "tool finished", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	})

	act, err := actor.Spawn(actor.Config{
		Default: actor.TurnConfig{
			Provider: mock,
			Tools:    []actor.Tool{pauseTool},
		},
	})
	if err != nil {
		t.Fatalf("failed to spawn: %v", err)
	}
	defer act.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	events, err := act.Prompt(ctx, "Refactor code")
	if err != nil {
		t.Fatalf("Prompt failed: %v", err)
	}

	// Wait for tool to start executing mid-turn
	select {
	case <-toolStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for pause_tool to start")
	}

	// While tool is in-flight, call act.Steer out-of-band!
	if err := act.Steer("Use camelCase for all variable names"); err != nil {
		t.Fatalf("Steer failed: %v", err)
	}

	// Unblock tool so step 1 finishes and step 2 starts
	close(continueTool)

	var output strings.Builder
	for ev := range events.Events() {
		if ev.Type == actor.EventDelta {
			output.WriteString(ev.Delta)
		}
	}

	if output.String() != "Done with camelCase" {
		t.Errorf("output = %q, want 'Done with camelCase'", output.String())
	}

	if !strings.Contains(step2Guidance, "[Active Guidance]: Use camelCase for all variable names") {
		t.Errorf("expected step 2 to contain active guidance, got: %q", step2Guidance)
	}
}

func TestActor_SteerQueueManipulation(t *testing.T) {
	mock := actor.NewMockProvider()
	act, err := actor.Spawn(actor.Config{
		Default: actor.TurnConfig{
			Provider: mock,
		},
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	defer act.Close()

	if act.PendingSteer() != nil {
		t.Errorf("expected nil PendingSteer initially, got %v", act.PendingSteer())
	}

	_ = act.Steer("guidance 1")
	_ = act.Steer("guidance 2")

	pending := act.PendingSteer()
	if len(pending) != 2 || pending[0] != "guidance 1" || pending[1] != "guidance 2" {
		t.Fatalf("PendingSteer = %v, want ['guidance 1', 'guidance 2']", pending)
	}

	// Pop newest
	popped, ok := act.PopSteer()
	if !ok || popped != "guidance 2" {
		t.Errorf("PopSteer = (%q, %v), want ('guidance 2', true)", popped, ok)
	}

	if len(act.PendingSteer()) != 1 || act.PendingSteer()[0] != "guidance 1" {
		t.Errorf("PendingSteer after pop = %v, want ['guidance 1']", act.PendingSteer())
	}

	// Clear all
	cleared := act.ClearSteer()
	if len(cleared) != 1 || cleared[0] != "guidance 1" {
		t.Errorf("ClearSteer = %v, want ['guidance 1']", cleared)
	}
	if act.PendingSteer() != nil {
		t.Errorf("expected nil PendingSteer after ClearSteer, got %v", act.PendingSteer())
	}
}

type testSteerStream struct {
	firstDone chan struct{}
	steerDone chan struct{}
	sent      bool
	ctx       context.Context
}

func (s *testSteerStream) Recv() (actor.StreamChunk, error) {
	if !s.sent {
		s.sent = true
		close(s.firstDone)
		return actor.StreamChunk{Delta: "Starting generation 1..."}, nil
	}
	select {
	case <-s.steerDone:
		return actor.StreamChunk{}, io.EOF
	case <-s.ctx.Done():
		return actor.StreamChunk{}, s.ctx.Err()
	}
}

func (s *testSteerStream) Close() error { return nil }

type steerTestProvider struct {
	firstDone     chan struct{}
	steerDone     chan struct{}
	step2Guidance chan string
	step          int
	mu            sync.Mutex
}

func (p *steerTestProvider) Generate(ctx context.Context, req actor.GenerateRequest) (actor.ResponseStream, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.step++
	if p.step == 1 {
		return &testSteerStream{
			firstDone: p.firstDone,
			steerDone: p.steerDone,
			ctx:       ctx,
		}, nil
	}
	var lastUserMsg string
	for _, m := range req.Messages {
		if m.Role == actor.RoleUser {
			lastUserMsg = m.Content
		}
	}
	if lastUserMsg != "" {
		select {
		case p.step2Guidance <- lastUserMsg:
		default:
		}
	}
	return &testSliceStream{
		chunks: []actor.StreamChunk{
			{Delta: "Pivoted to JSON format.", FinishReason: "stop"},
		},
		ctx: ctx,
	}, nil
}

type testSliceStream struct {
	chunks []actor.StreamChunk
	idx    int
	ctx    context.Context
}

func (s *testSliceStream) Recv() (actor.StreamChunk, error) {
	select {
	case <-s.ctx.Done():
		return actor.StreamChunk{}, s.ctx.Err()
	default:
	}
	if s.idx >= len(s.chunks) {
		return actor.StreamChunk{}, io.EOF
	}
	c := s.chunks[s.idx]
	s.idx++
	return c, nil
}

func (s *testSliceStream) Close() error { return nil }

func TestActor_Steer_TextStream_InFlight(t *testing.T) {
	firstDone := make(chan struct{})
	steerDone := make(chan struct{})
	step2Guidance := make(chan string, 1)

	provider := &steerTestProvider{
		firstDone:     firstDone,
		steerDone:     steerDone,
		step2Guidance: step2Guidance,
	}

	act, err := actor.Spawn(actor.Config{
		Default: actor.TurnConfig{
			Provider: provider,
		},
	})
	if err != nil {
		t.Fatalf("failed to spawn actor: %v", err)
	}
	defer act.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	events, err := act.Prompt(ctx, "Write an essay")
	if err != nil {
		t.Fatalf("Prompt failed: %v", err)
	}

	<-firstDone

	// Steer mid-flight while step 1 text is generating
	if err := act.Steer("Stop and output JSON instead"); err != nil {
		t.Fatalf("Steer failed: %v", err)
	}
	close(steerDone)

	var output strings.Builder
	for ev := range events.Events() {
		if ev.Type == actor.EventDelta {
			output.WriteString(ev.Delta)
		}
	}

	select {
	case guidance := <-step2Guidance:
		if !strings.Contains(guidance, "Stop and output JSON instead") {
			t.Errorf("step 2 prompt did not receive steer guidance, got: %q", guidance)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for step 2 steer guidance")
	}

	if !strings.Contains(output.String(), "Pivoted to JSON format.") {
		t.Errorf("expected final output to contain second step output, got: %q", output.String())
	}
}

func TestActor_SteerLifecycleEvents(t *testing.T) {
	firstDone := make(chan struct{})
	steerDone := make(chan struct{})
	step2Guidance := make(chan string, 1)

	provider := &steerTestProvider{
		firstDone:     firstDone,
		steerDone:     steerDone,
		step2Guidance: step2Guidance,
	}

	act, err := actor.Spawn(actor.Config{
		Default: actor.TurnConfig{
			Provider: provider,
		},
	})
	if err != nil {
		t.Fatalf("failed to spawn actor: %v", err)
	}
	defer act.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	turn, err := act.Prompt(ctx, "Start generation")
	if err != nil {
		t.Fatalf("Prompt failed: %v", err)
	}

	<-firstDone

	// Mid-flight steer
	steerInstruction := "Focus on concise bullet points"
	if err := act.Steer(steerInstruction); err != nil {
		t.Fatalf("Steer failed: %v", err)
	}
	close(steerDone)

	var applied bool
	for ev := range turn.Events() {
		if ev.Type == actor.EventSteerApplied {
			if ev.SteerInstruction != steerInstruction {
				t.Errorf("EventSteerApplied instruction mismatch: got %q, want %q", ev.SteerInstruction, steerInstruction)
			}
			applied = true
		}
	}
	if !applied {
		t.Error("did not observe EventSteerApplied on the turn stream")
	}
}

func TestActor_AutoContinue_OnTruncation(t *testing.T) {
	mock := actor.NewMockProvider()

	// Step 1: output truncated by length
	mock.EnqueueChunks([]actor.StreamChunk{
		{Delta: "This is part one of the story, but then "},
		{FinishReason: "length"},
	})

	// Step 2: continued completion
	mock.EnqueueHandler(func(req actor.GenerateRequest) ([]actor.StreamChunk, error) {
		// Verify that active guidance contains StreamInterruptedMessage
		foundGuidance := false
		for _, m := range req.Messages {
			if strings.Contains(m.Content, actor.StreamInterruptedMessage) {
				foundGuidance = true
				break
			}
		}
		if !foundGuidance {
			t.Errorf("step 2 request did not contain auto-continue recovery message: %+v", req.Messages)
		}
		return []actor.StreamChunk{
			{Delta: "it continues into part two to finish cleanly."},
			{FinishReason: "stop"},
		}, nil
	})

	act, err := actor.Spawn(actor.Config{
		Default: actor.TurnConfig{
			Provider: mock,
		},
	})
	if err != nil {
		t.Fatalf("failed to spawn actor: %v", err)
	}
	defer act.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	turn, err := act.Prompt(ctx, "Tell me a long story")
	if err != nil {
		t.Fatalf("Prompt failed: %v", err)
	}
	var output strings.Builder
	for ev := range turn.Events() {
		if ev.Type == actor.EventDelta {
			output.WriteString(ev.Delta)
		}
	}

	expected := "This is part one of the story, but then it continues into part two to finish cleanly."
	if output.String() != expected {
		t.Errorf("got output %q, want %q", output.String(), expected)
	}

	// Verify that the trajectory recorded 2 steps in the turn
	turns := act.Trajectory().Turns()
	if len(turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(turns))
	}
	if len(turns[0].Steps) != 2 {
		t.Errorf("expected 2 steps in the turn, got %d", len(turns[0].Steps))
	}
}
