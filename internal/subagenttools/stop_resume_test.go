package subagenttools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"
	"github.com/justwasm/pigasus/internal/taskregistry"
)

func TestChildStopAndRevive(t *testing.T) {
	canceled := false
	child := NewChild("test-sub-1", "DeepCoder", "Role", "/tmp", func() {
		canceled = true
	})

	if !child.IsRunning() {
		t.Fatalf("new child should be running")
	}

	child.Stop()
	if child.State() != StateStopped {
		t.Fatalf("child state = %q, want %q", child.State(), StateStopped)
	}
	if !canceled {
		t.Fatalf("expected cancel func to have been called on stop")
	}

	revivedWithMsg := false
	child.SetLaunch(func(ctx context.Context, initialMsg *Message) {
		if initialMsg != nil && initialMsg.Content == "wake up" {
			revivedWithMsg = true
		}
		child.setState(StateRunning)
	})

	msg := Message{Content: "wake up"}
	ok := child.Revive(context.Background(), &msg)
	if !ok {
		t.Fatalf("child.Revive returned false")
	}
	if !revivedWithMsg {
		t.Fatalf("revive did not receive initial message")
	}
	if !child.IsRunning() {
		t.Fatalf("child should be running after revive")
	}
}

func TestTranscriptPersistAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	subID := "sub-persist-1"

	userMsg := piagent.AgentMessage{User: &piagent.UserMessage{
		Role:      "user",
		Content:   pi.UserText("Implement feature X"),
		Timestamp: time.Now().UnixMilli(),
	}}
	assistantMsg := piagent.AgentMessage{Assistant: &piagent.AssistantMessage{
		Role: "assistant",
		Content: []pi.AssistantContentBlock{
			pi.TextContent{Text: "I will implement feature X."},
		},
		Timestamp:  time.Now().UnixMilli(),
		StopReason: pi.StopReasonStop,
	}}

	msgs := []piagent.AgentMessage{userMsg, assistantMsg}
	persistTranscript(tmpDir, subID, msgs, nil)

	loaded, err := loadTranscript(tmpDir, subID)
	if err != nil {
		t.Fatalf("loadTranscript error: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("loaded %d messages, want 2", len(loaded))
	}
	if loaded[0].Role() != "user" {
		t.Errorf("loaded[0].Role = %q, want user", loaded[0].Role())
	}
	if loaded[1].Role() != "assistant" {
		t.Errorf("loaded[1].Role = %q, want assistant", loaded[1].Role())
	}
}

func TestSendMessageRevivesStoppedSubagent(t *testing.T) {
	reg := NewRegistry("parent-1")
	child := NewChild("sub-stopped-1", "DeepCoder", "Role", "/tmp", func() {})
	child.Stop()
	reg.Add(child)

	revived := false
	child.SetLaunch(func(ctx context.Context, initialMsg *Message) {
		revived = true
		if initialMsg != nil && initialMsg.Content == "please continue" {
			child.setState(StateRunning)
		}
	})

	sendTool := &sendMessageTool{registry: reg, owner: "parent-1"}
	res, err := sendTool.Execute(context.Background(), "call-send",
		json.RawMessage(`{"Recipient":"sub-stopped-1","Message":"please continue"}`), nil)
	if err != nil {
		t.Fatalf("sendTool.Execute error: %v", err)
	}
	if res.IsError {
		t.Fatalf("sendTool reported error result: %v", res)
	}
	if !revived {
		t.Fatalf("send_message did not revive the stopped subagent")
	}
}

func TestManageSubagentsStopAndResume(t *testing.T) {
	reg := NewRegistry("parent-1")
	child := NewChild("sub-target-1", "DeepCoder", "Role", "/tmp", func() {})
	reg.Add(child)

	resumed := false
	child.SetLaunch(func(ctx context.Context, initialMsg *Message) {
		resumed = true
		child.setState(StateRunning)
	})

	manageTool := &manageSubagentsTool{registry: reg, owner: "parent-1"}

	// 1. Action: stop
	res, err := manageTool.Execute(context.Background(), "call-stop",
		json.RawMessage(`{"Action":"stop","ConversationIds":["sub-target-1"]}`), nil)
	if err != nil {
		t.Fatalf("manageTool stop error: %v", err)
	}
	if child.State() != StateStopped {
		t.Fatalf("child state = %q, want %q", child.State(), StateStopped)
	}
	text := res.Content[0].(pi.TextContent).Text
	if text == "" {
		t.Fatalf("empty stop response")
	}

	// 2. Action: resume
	res, err = manageTool.Execute(context.Background(), "call-resume",
		json.RawMessage(`{"Action":"resume","ConversationIds":["sub-target-1"]}`), nil)
	if err != nil {
		t.Fatalf("manageTool resume error: %v", err)
	}
	if !resumed {
		t.Fatalf("manage_subagents resume did not trigger revive on child")
	}
	if child.State() != StateRunning {
		t.Fatalf("child state after resume = %q, want %q", child.State(), StateRunning)
	}
}

func TestRecordChildTurnPreservesStoppedState(t *testing.T) {
	taskReg := taskregistry.New()
	taskReg.Register(&taskregistry.Task{
		ID:    "task-sub-1",
		Name:  "Test Subagent",
		Kind:  taskregistry.KindSubagent,
		State: taskregistry.StateRunning,
	})

	child := NewChild("task-sub-1", "DeepCoder", "Role", "/tmp", func() {})
	child.Stop()

	recordChildTurn(taskReg, child, nil, nil)

	task := taskReg.Get("task-sub-1")
	if task == nil {
		t.Fatalf("task not found in registry")
	}
	if task.State != taskregistry.StateStopped {
		t.Fatalf("task.State = %q, want %q", task.State, taskregistry.StateStopped)
	}
}

func TestLoadTranscriptNonExistentFileReturnsNil(t *testing.T) {
	msgs, err := loadTranscript(t.TempDir(), "non-existent")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if msgs != nil {
		t.Fatalf("expected nil msgs, got %v", msgs)
	}
}

func TestChildKillDoesNotSetStopped(t *testing.T) {
	canceled := false
	child := NewChild("test-kill-1", "DeepCoder", "Role", "/tmp", func() {
		canceled = true
	})

	child.Kill()
	if child.State() != StateCanceling {
		t.Fatalf("child state after Kill() = %q, want %q", child.State(), StateCanceling)
	}
	if child.State() == StateStopped {
		t.Fatalf("child state after Kill() should NOT be StateStopped")
	}
	if !canceled {
		t.Fatalf("cancel func should have been called on kill")
	}
}

func TestStopWhileWaitingForInputRecordsStoppedInTaskRegistry(t *testing.T) {
	taskReg := taskregistry.New()
	taskReg.Register(&taskregistry.Task{
		ID:    "task-wait-stop",
		Name:  "Test Waiting Stop",
		Kind:  taskregistry.KindSubagent,
		State: taskregistry.StateRunning,
	})

	child := NewChild("task-wait-stop", "DeepCoder", "Role", "/tmp", func() {})
	child.setState(StateIdle)

	ctx, cancel := context.WithCancel(context.Background())
	child.SetCancel(cancel)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, ok := child.Wait(ctx)
		if !ok {
			if child.State() == StateStopped {
				recordChildTurn(taskReg, child, nil, nil)
			}
		}
	}()

	time.Sleep(10 * time.Millisecond)
	child.Stop()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("child.Wait did not return after Stop()")
	}

	task := taskReg.Get("task-wait-stop")
	if task == nil {
		t.Fatalf("task not found in registry")
	}
	if task.State != taskregistry.StateStopped {
		t.Fatalf("task.State = %q, want %q", task.State, taskregistry.StateStopped)
	}
}
