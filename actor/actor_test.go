package actor_test

import (
	"context"
	"encoding/json"
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
		Name:         "test-assistant",
		SystemPrompt: "You are a helpful test assistant.",
		Provider:     mock,
	})
	if err != nil {
		t.Fatalf("failed to spawn actor: %v", err)
	}
	defer act.Close()

	if act.Name() != "test-assistant" {
		t.Errorf("expected name test-assistant, got %s", act.Name())
	}
	if act.SystemPrompt() != "You are a helpful test assistant." {
		t.Errorf("unexpected system prompt: %s", act.SystemPrompt())
	}

	events := act.Events()

	msg := actor.NewUserMessage("Hi there")
	if err := act.Send(msg); err != nil {
		t.Fatalf("failed to send message: %v", err)
	}

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
	turn := traj.LastTurn()
	if turn.FinalOutput != "Hello world!" {
		t.Errorf("expected turn final output 'Hello world!', got %q", turn.FinalOutput)
	}
}

func TestActor_Ask(t *testing.T) {
	mock := actor.NewMockProvider()
	mock.EnqueueChunks([]actor.StreamChunk{
		{Delta: "Direct ask answer"},
		{FinishReason: "stop"},
	})

	act, err := actor.Spawn(actor.Config{
		Provider: mock,
	})
	if err != nil {
		t.Fatalf("failed to spawn actor: %v", err)
	}
	defer act.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	stream, err := act.Ask(ctx, actor.NewUserMessage("Tell me something"))
	if err != nil {
		t.Fatalf("failed to ask: %v", err)
	}

	var deltas strings.Builder
	var completed bool
	for ev := range stream {
		if ev.Type == actor.EventDelta {
			deltas.WriteString(ev.Delta)
		}
		if ev.Type == actor.EventTurnCompleted {
			completed = true
		}
	}

	if !completed {
		t.Error("expected turn completion in Ask stream")
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
		Provider: mock,
		Tools:    []actor.Tool{addTool},
	})
	if err != nil {
		t.Fatalf("failed to spawn actor: %v", err)
	}
	defer act.Close()

	events := act.Events()

	if err := act.Send(actor.NewUserMessage("What is 17 + 25?")); err != nil {
		t.Fatalf("failed to send: %v", err)
	}

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
		Provider: mock,
		Tools:    []actor.Tool{deployTool},
	})
	if err != nil {
		t.Fatalf("failed to spawn: %v", err)
	}
	defer act.Close()

	events := act.Events()

	if err := act.Send(actor.NewUserMessage("Deploy to prod")); err != nil {
		t.Fatalf("failed to send: %v", err)
	}

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
		Provider: mock,
		Tools:    []actor.Tool{deleteTool},
	})
	if err != nil {
		t.Fatalf("failed to spawn: %v", err)
	}
	defer act.Close()

	events := act.Events()

	if err := act.Send(actor.NewUserMessage("Delete the database")); err != nil {
		t.Fatalf("failed to send: %v", err)
	}

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
		Provider: mock,
	})
	if err != nil {
		t.Fatalf("failed to spawn: %v", err)
	}
	defer act.Close()

	// Send steer instruction before turn
	_ = act.Send(actor.NewSteerMessage("Always format output as JSON"))
	_ = act.Send(actor.NewUserMessage("List users"))

	events := act.Events()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case <-timeout:
			t.Fatal("timeout waiting for steer turn")
		case ev := <-events:
			if ev.Type == actor.EventTurnCompleted {
				goto Done
			}
		}
	}
Done:
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
		Provider: mock,
	})
	if err != nil {
		t.Fatalf("failed to spawn: %v", err)
	}
	defer act.Close()
	defer close(blockCh)

	events := act.Events()

	_ = act.Send(actor.NewUserMessage("Slow computation"))

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

	cancelledReceived := false
	for !cancelledReceived {
		select {
		case <-timeout:
			t.Fatal("timeout waiting for cancellation event")
		case ev := <-events:
			if ev.Type == actor.EventCancelled {
				cancelledReceived = true
			}
		}
	}

	if act.State().State != actor.StateIdle {
		t.Errorf("expected StateIdle after cancellation, got %s", act.State().State)
	}
}

func TestActor_AutoCompaction(t *testing.T) {
	mock := actor.NewMockProvider()
	// Return quick responses
	for i := 0; i < 5; i++ {
		mock.EnqueueResponse(fmt.Sprintf("Answer %d", i+1))
	}

	act, err := actor.Spawn(actor.Config{
		Provider:             mock,
		AutoCompactThreshold: 3, // compacts when steps exceed 3
	})
	if err != nil {
		t.Fatalf("failed to spawn: %v", err)
	}
	defer act.Close()

	events := act.Events()
	compactReceived := false

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for ev := range events {
			if ev.Type == actor.EventCompacted {
				compactReceived = true
			}
		}
	}()

	for i := 0; i < 5; i++ {
		_ = act.Send(actor.NewUserMessage(fmt.Sprintf("Turn %d", i+1)))
		time.Sleep(30 * time.Millisecond)
	}

	_ = act.Close()
	wg.Wait()

	if !compactReceived {
		t.Error("expected EventCompacted to be triggered")
	}
}

func TestActor_Closed_ErrActorClosed(t *testing.T) {
	mock := actor.NewMockProvider()
	act, err := actor.Spawn(actor.Config{
		Provider: mock,
	})
	if err != nil {
		t.Fatalf("failed to spawn: %v", err)
	}

	if err := act.Close(); err != nil {
		t.Fatalf("failed to close: %v", err)
	}

	// Sending message to closed actor returns ErrActorClosed
	err = act.Send(actor.NewUserMessage("Hello after close"))
	if err != actor.ErrActorClosed {
		t.Errorf("expected ErrActorClosed, got %v", err)
	}

	// Schedule on closed actor returns ErrActorClosed
	_, err = act.Schedule(time.Minute, "delayed")
	if err != actor.ErrActorClosed {
		t.Errorf("expected ErrActorClosed for Schedule, got %v", err)
	}
}

func TestActor_Prompt_And_Run(t *testing.T) {
	mock := actor.NewMockProvider()
	mock.EnqueueChunks([]actor.StreamChunk{
		{Delta: "Direct prompt answer"},
		{FinishReason: "stop"},
	})
	mock.EnqueueChunks([]actor.StreamChunk{
		{Delta: "Synchronous run answer"},
		{FinishReason: "stop"},
	})

	act, err := actor.Spawn(actor.Config{
		Provider: mock,
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
	for ev := range evStream {
		if ev.Type == actor.EventDelta {
			promptText.WriteString(ev.Delta)
		}
	}
	if promptText.String() != "Direct prompt answer" {
		t.Errorf("Prompt output = %q, want 'Direct prompt answer'", promptText.String())
	}

	// Test Run
	runResult, err := act.Run(ctx, "hello from run")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if runResult != "Synchronous run answer" {
		t.Errorf("Run output = %q, want 'Synchronous run answer'", runResult)
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
		Provider:       mock,
		InitialHistory: history,
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

	res, err := act.Run(ctx, "And what is your purpose?")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if res != "response" {
		t.Errorf("unexpected res: %s", res)
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
			Input:       actor.NewUserMessage("first"),
			FinalOutput: "answer 1",
			Status:      "done",
		},
	}

	act, err := actor.Spawn(actor.Config{
		Provider:     mock,
		InitialTurns: initialTurns,
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
		Provider: mock,
		Tools:    []actor.Tool{pauseTool},
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
	for ev := range events {
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

func TestActor_DynamicConfiguration(t *testing.T) {
	mock := actor.NewMockProvider()
	mock.EnqueueChunks([]actor.StreamChunk{
		{Delta: "reply 1", FinishReason: "stop"},
	})
	mock.EnqueueChunks([]actor.StreamChunk{
		{Delta: "reply 2", FinishReason: "stop"},
	})

	act, err := actor.Spawn(actor.Config{
		Model:        "model-v1",
		SystemPrompt: "system-prompt-1",
		Provider:     mock,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	defer act.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Turn 1 with initial configuration
	_, err = act.Run(ctx, "hello 1")
	if err != nil {
		t.Fatalf("Run 1: %v", err)
	}
	if len(mock.Requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(mock.Requests))
	}
	if mock.Requests[0].Model != "model-v1" {
		t.Errorf("req.Model = %q, want 'model-v1'", mock.Requests[0].Model)
	}
	if mock.Requests[0].SystemPrompt != "system-prompt-1" {
		t.Errorf("req.SystemPrompt = %q, want 'system-prompt-1'", mock.Requests[0].SystemPrompt)
	}

	// Dynamic update: change model, system prompt, tools
	act.SetModel("model-v2")
	act.SetSystemPrompt("system-prompt-2")
	act.SetTools(actor.NewFuncTool("calc", "calculator", func(ctx context.Context, input string) (string, error) {
		return "42", nil
	}))

	if act.Model() != "model-v2" {
		t.Errorf("act.Model() = %q, want 'model-v2'", act.Model())
	}
	if act.SystemPrompt() != "system-prompt-2" {
		t.Errorf("act.SystemPrompt() = %q, want 'system-prompt-2'", act.SystemPrompt())
	}

	// Turn 2 with dynamically updated configuration on the same actor
	_, err = act.Run(ctx, "hello 2")
	if err != nil {
		t.Fatalf("Run 2: %v", err)
	}
	if len(mock.Requests) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(mock.Requests))
	}
	if mock.Requests[1].Model != "model-v2" {
		t.Errorf("req.Model = %q, want 'model-v2'", mock.Requests[1].Model)
	}
	if mock.Requests[1].SystemPrompt != "system-prompt-2" {
		t.Errorf("req.SystemPrompt = %q, want 'system-prompt-2'", mock.Requests[1].SystemPrompt)
	}
	if len(mock.Requests[1].Tools) != 1 || mock.Requests[1].Tools[0].Name != "calc" {
		t.Errorf("req.Tools = %+v, want 1 tool 'calc'", mock.Requests[1].Tools)
	}
}

func TestActor_SteerQueueManipulation(t *testing.T) {
	mock := actor.NewMockProvider()
	act, err := actor.Spawn(actor.Config{Provider: mock})
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

func TestActor_DefaultUnlimitedSteps(t *testing.T) {
	mock := actor.NewMockProvider()
	const stepCount = 30 // More than the previous 25 limit

	for i := 0; i < stepCount; i++ {
		mock.EnqueueChunks([]actor.StreamChunk{
			{
				ToolCalls: []actor.ToolCall{
					{
						ID:    fmt.Sprintf("call_%d", i),
						Name:  "ping",
						Input: "{}",
					},
				},
			},
		})
	}
	mock.EnqueueChunks([]actor.StreamChunk{
		{Delta: "All 30 steps completed successfully!"},
		{FinishReason: "stop"},
	})

	pingTool := actor.NewFuncTool("ping", "ping tool", func(ctx context.Context, input string) (string, error) {
		return "pong", nil
	})

	// Default config has MaxStepsPerTurn = 0 (unlimited)
	act, err := actor.Spawn(actor.Config{
		Provider: mock,
		Tools:    []actor.Tool{pingTool},
	})
	if err != nil {
		t.Fatalf("failed to spawn actor: %v", err)
	}
	defer act.Close()

	stream, err := act.Ask(context.Background(), actor.NewUserMessage("run long loop"))
	if err != nil {
		t.Fatalf("Ask failed with error: %v", err)
	}
	var output strings.Builder
	var turnCompleted bool
	for ev := range stream {
		if ev.Type == actor.EventDelta {
			output.WriteString(ev.Delta)
		}
		if ev.Type == actor.EventTurnCompleted {
			turnCompleted = true
		}
	}
	if !turnCompleted {
		t.Fatal("turn did not complete")
	}
	if output.String() != "All 30 steps completed successfully!" {
		t.Errorf("unexpected output: %q", output.String())
	}
	turn := act.Trajectory().LastTurn()
	if turn.Status != "done" {
		t.Errorf("turn status = %q, want 'done'", turn.Status)
	}
	if len(turn.Steps) != 31 {
		t.Errorf("len(turn.Steps) = %d, want 31", len(turn.Steps))
	}
}

func TestActor_ExplicitMaxStepsEnforced(t *testing.T) {
	mock := actor.NewMockProvider()
	for i := 0; i < 5; i++ {
		mock.EnqueueChunks([]actor.StreamChunk{
			{
				ToolCalls: []actor.ToolCall{
					{
						ID:    fmt.Sprintf("call_%d", i),
						Name:  "ping",
						Input: "{}",
					},
				},
			},
		})
	}

	pingTool := actor.NewFuncTool("ping", "ping tool", func(ctx context.Context, input string) (string, error) {
		return "pong", nil
	})

	act, err := actor.Spawn(actor.Config{
		Provider:        mock,
		Tools:           []actor.Tool{pingTool},
		MaxStepsPerTurn: 3,
	})
	if err != nil {
		t.Fatalf("failed to spawn actor: %v", err)
	}
	defer act.Close()

	stream, err := act.Ask(context.Background(), actor.NewUserMessage("run limited loop"))
	if err != nil {
		t.Fatalf("Ask failed with error: %v", err)
	}
	var turnErr string
	for ev := range stream {
		if ev.Type == actor.EventTurnCompleted && ev.TurnSummary != nil {
			turnErr = ev.Error
		}
	}
	if !strings.Contains(turnErr, "maximum steps per turn reached") {
		t.Errorf("expected maximum steps error, got %q", turnErr)
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
		Provider: provider,
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
	for ev := range events {
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

func TestActor_InterfacesAndMessageBoundaries(t *testing.T) {
	mock := actor.NewMockProvider()
	act, err := actor.Spawn(actor.Config{
		Provider: mock,
	})
	if err != nil {
		t.Fatalf("failed to spawn actor: %v", err)
	}
	defer act.Close()

	// Verify interface satisfaction
	var (
		_ actor.ActorHandle    = act
		_ actor.TurnRunner     = act
		_ actor.Steerer        = act
		_ actor.TurnController = act
		_ actor.SteerTracker   = act
		_ actor.EventStreamer  = act
	)

	// Verify SteerTracker operations
	if len(act.PendingSteer()) != 0 {
		t.Fatalf("expected empty pending steer, got %v", act.PendingSteer())
	}
	if err := act.Steer("rule 1"); err != nil {
		t.Fatalf("steer failed: %v", err)
	}
	if err := act.Steer("rule 2"); err != nil {
		t.Fatalf("steer failed: %v", err)
	}

	pending := act.PendingSteer()
	if len(pending) != 2 || pending[0] != "rule 1" || pending[1] != "rule 2" {
		t.Errorf("unexpected pending steer list: %v", pending)
	}

	popped, ok := act.PopSteer()
	if !ok || popped != "rule 2" {
		t.Errorf("expected popped rule 2, got %q (ok=%v)", popped, ok)
	}

	cleared := act.ClearSteer()
	if len(cleared) != 1 || cleared[0] != "rule 1" {
		t.Errorf("expected cleared [rule 1], got %v", cleared)
	}
	if len(act.PendingSteer()) != 0 {
		t.Errorf("expected pending steer to be empty after ClearSteer")
	}

	// Verify IsTurnInitiator boundary helper
	if !actor.IsTurnInitiator(actor.NewUserMessage("hello")) {
		t.Errorf("UserMessage must be a turn initiator")
	}
	if !actor.IsTurnInitiator(actor.NewScheduledWakeup("s1", "wake", "timer")) {
		t.Errorf("ScheduledWakeup must be a turn initiator")
	}
	if actor.IsTurnInitiator(actor.NewSteerMessage("guidance")) {
		t.Errorf("SteerMessage is out-of-band guidance, not a turn initiator")
	}
	if actor.IsTurnInitiator(actor.NewCancelSignal("t1", "stop")) {
		t.Errorf("CancelSignal is an out-of-band signal, not a turn initiator")
	}
	if actor.IsTurnInitiator(actor.NewApprovalDecision("tc1", true, "")) {
		t.Errorf("ApprovalDecision is an out-of-band authorization, not a turn initiator")
	}
	if actor.IsTurnInitiator(actor.NewSystemPromptUpdate("system")) {
		t.Errorf("SystemPromptUpdate is configuration, not a turn initiator")
	}
	if actor.IsTurnInitiator(actor.NewCustomMessage("custom", "data")) {
		t.Errorf("CustomMessage is not a turn initiator by default")
	}
	if actor.IsTurnInitiator(nil) {
		t.Errorf("nil message should return false")
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
		Provider: provider,
	})
	if err != nil {
		t.Fatalf("failed to spawn actor: %v", err)
	}
	defer act.Close()

	events := act.Events()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	turnEvents, err := act.Prompt(ctx, "Start generation")
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

	// Consume turn events to completion
	for range turnEvents {
	}

	// Verify events on general subscription
	var receivedEventFound, appliedEventFound bool
	timeout := time.After(2 * time.Second)

loop:
	for {
		select {
		case ev := <-events:
			if ev.Type == actor.EventSteerReceived {
				if ev.SteerInstruction != steerInstruction {
					t.Errorf("EventSteerReceived instruction mismatch: got %q, want %q", ev.SteerInstruction, steerInstruction)
				}
				receivedEventFound = true
			}
			if ev.Type == actor.EventSteerApplied {
				if ev.SteerInstruction != steerInstruction {
					t.Errorf("EventSteerApplied instruction mismatch: got %q, want %q", ev.SteerInstruction, steerInstruction)
				}
				appliedEventFound = true
			}
			if receivedEventFound && appliedEventFound {
				break loop
			}
		case <-timeout:
			break loop
		}
	}

	if !receivedEventFound {
		t.Errorf("did not observe EventSteerReceived on actor event stream")
	}
	if !appliedEventFound {
		t.Errorf("did not observe EventSteerApplied on actor event stream")
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
		Provider: mock,
	})
	if err != nil {
		t.Fatalf("failed to spawn actor: %v", err)
	}
	defer act.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	output, err := act.Run(ctx, "Tell me a long story")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	expected := "This is part one of the story, but then it continues into part two to finish cleanly."
	if output != expected {
		t.Errorf("got output %q, want %q", output, expected)
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

