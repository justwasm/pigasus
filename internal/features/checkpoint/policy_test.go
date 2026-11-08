package checkpoint

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/actor"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/compaction"
)

type summaryBackend struct {
	requests []backend.StreamRequest
	text     string
}

func (b *summaryBackend) Stream(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
	b.requests = append(b.requests, req)
	ch := make(chan backend.StreamChunk, 2)
	ch <- backend.StreamChunk{Text: b.text}
	ch <- backend.StreamChunk{Done: true, Meta: &backend.ChunkMeta{PromptTokens: 10, CompletionTokens: 5}}
	close(ch)
	return ch, nil
}

func toolTurn() *actor.TurnRecord {
	return &actor.TurnRecord{
		ID:     "t1",
		Input:  "prior request",
		Status: "done",
		Steps: []actor.Step{
			{
				Index:       0,
				Status:      actor.StepDone,
				ToolCalls:   []actor.ToolCall{{ID: "c1", Name: "noop", Input: "{}"}},
				ToolResults: []actor.ToolResult{{ToolCallID: "c1", Output: "tool output"}},
			},
			{Index: 1, Status: actor.StepDone, Delta: "answer"},
		},
		FinalOutput: "answer",
	}
}

func testActor(t *testing.T) *actor.Actor {
	t.Helper()
	act, err := actor.Spawn(actor.Config{
		Default:      actor.TurnConfig{Provider: actor.NewMockProvider()},
		InitialTurns: []*actor.TurnRecord{toolTurn()},
	})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	t.Cleanup(func() { _ = act.Close() })
	return act
}

func probeFor(t *testing.T, act *actor.Actor) actor.CheckpointProbe {
	t.Helper()
	turns := act.Trajectory().Turns()
	if len(turns) == 0 || len(turns[0].Steps) < 2 {
		t.Fatal("fixture lost its steps")
	}
	last := turns[0].Steps[1]
	return actor.CheckpointProbe{
		TurnID:          "t1",
		StepIndex:       1,
		GlobalStepIndex: 2,
		Model:           "model",
		LastStep:        &last,
	}
}

func TestBeforeStep_SyncSameModelUsesNoOnDemandTail(t *testing.T) {
	be := &summaryBackend{text: "<summary>ok</summary>"}
	p := New(Config{
		Model:            "gemini-3.8-flash-high",
		SystemPrompt:     "sys",
		ToolDeclarations: bytes.Repeat([]byte("x"), 750_000), // ~250k tokens, above the 240k hard limit
		Backend:          be,
	})
	defer p.Close()
	act := testActor(t)
	probe := probeFor(t, act)
	// The fixture's last step is a model response; make it a tool step so
	// the reference's gate allows a soft checkpoint.
	probe.LastStep = &actor.Step{ToolResults: []actor.ToolResult{{ToolCallID: "c1", Output: "x"}}}

	decision, err := p.BeforeStep(context.Background(), act, probe)
	if err != nil {
		t.Fatalf("BeforeStep: %v", err)
	}
	if !decision.Apply {
		t.Fatal("expected a checkpoint")
	}
	if decision.Model != "gemini-3.8-flash-high" {
		t.Errorf("summary model = %q, want the session model", decision.Model)
	}
	if !decision.Sync {
		t.Error("SAME_MODEL profiles must be synchronous")
	}
	if len(be.requests) != 1 {
		t.Fatalf("summary requests = %d, want 1", len(be.requests))
	}
	last := be.requests[0].Messages[len(be.requests[0].Messages)-1].Content
	if strings.Contains(last, "ON-DEMAND COMPACTION") {
		t.Error("the automatic checkpoint must not carry the on-demand tail")
	}
	if !strings.Contains(decision.ResumeText, "# Resuming from a compaction") {
		t.Errorf("resume text = %q", decision.ResumeText)
	}
	if decision.KeepRecent != 1 {
		t.Errorf("keep recent = %d, want 1", decision.KeepRecent)
	}
}

func TestBeforeStep_SinglePromptUsesCheckpointModel(t *testing.T) {
	be := &summaryBackend{text: "<summary>ok</summary>"}
	p := New(Config{
		Model:               "gpt-oss-120b-medium",
		SystemPrompt:        "sys",
		ToolDeclarations:    bytes.Repeat([]byte("x"), 180_000),
		EnableSoftThreshold: true,
		Backend:             be,
	})
	defer p.Close()
	act := testActor(t)
	probe := probeFor(t, act)
	probe.LastStep = &actor.Step{ToolResults: []actor.ToolResult{{ToolCallID: "c1", Output: "x"}}}

	decision, err := p.BeforeStep(context.Background(), act, probe)
	if err != nil {
		t.Fatalf("BeforeStep: %v", err)
	}
	if decision.Apply {
		t.Fatal("asynchronous profiles must not apply on the call that starts the summary")
	}
	p.mu.Lock()
	pending := p.pending
	p.mu.Unlock()
	if pending == nil {
		t.Fatal("the async summary was not started")
	}
	select {
	case <-pending.done:
	case <-time.After(2 * time.Second):
		t.Fatal("async summary did not finish")
	}
	decision, err = p.BeforeStep(context.Background(), act, probe)
	if err != nil {
		t.Fatalf("second BeforeStep: %v", err)
	}
	if !decision.Apply {
		t.Fatal("expected the finished async checkpoint to apply")
	}
	if decision.Model != "gemini-3.1-flash-lite" {
		t.Errorf("summary model = %q, want the checkpoint model", decision.Model)
	}
	if len(be.requests) == 0 || be.requests[0].Model != "gemini-3.1-flash-lite" {
		t.Errorf("summary request model = %+v", be.requests)
	}
	if decision.Sync {
		t.Error("the async profile must not report Sync")
	}
}

func TestBeforeStep_HardLimitOverridesStepGate(t *testing.T) {
	be := &summaryBackend{text: "<summary>ok</summary>"}
	p := New(Config{
		Model:            "gpt-oss-120b-medium",
		SystemPrompt:     "sys",
		ToolDeclarations: bytes.Repeat([]byte("x"), 300_000), // ~100k > 72k
		Backend:          be,
	})
	defer p.Close()
	act := testActor(t)
	probe := probeFor(t, act)
	probe.LastStep = &actor.Step{Delta: "model response"} // gate would refuse

	decision, err := p.BeforeStep(context.Background(), act, probe)
	if err != nil {
		t.Fatalf("BeforeStep: %v", err)
	}
	if !decision.Apply {
		t.Fatal("a hard-limit checkpoint must override the step gate")
	}
	if !decision.Sync {
		t.Error("a hard-limit checkpoint must be synchronous")
	}
}

func TestBeforeStep_SoftGateRefusesModelResponse(t *testing.T) {
	be := &summaryBackend{text: "<summary>ok</summary>"}
	p := New(Config{
		Model:               "gpt-oss-120b-medium",
		SystemPrompt:        "sys",
		ToolDeclarations:    bytes.Repeat([]byte("x"), 180_000),
		EnableSoftThreshold: true,
		Backend:             be,
	})
	defer p.Close()
	act := testActor(t)
	probe := probeFor(t, act)
	probe.LastStep = &actor.Step{Delta: "model response"}

	decision, err := p.BeforeStep(context.Background(), act, probe)
	if err != nil {
		t.Fatalf("BeforeStep: %v", err)
	}
	if decision.Apply || len(be.requests) != 0 {
		t.Fatal("a soft checkpoint must not fire after a model-response step")
	}
}

func TestAutoContinueMessageUnchanged(t *testing.T) {
	if compaction.SummarizePrompt == "" {
		t.Fatal("empty summary prompt")
	}
}

// TestBeforeStep_ResumeCarriesHostSnapshots pins the three host-supplied
// sections the 1.5.3 CHECKPOINT step appends. The session model is the one
// model this work validates against (Gemini 3.8 Flash High); its profile
// disables include_conversation_log, so the fixed tail must stay absent here
// (the tail itself is pinned by TestResumeBlock_ConversationLogsMatches153).
func TestBeforeStep_ResumeCarriesHostSnapshots(t *testing.T) {
	be := &summaryBackend{text: "<summary>ok</summary>"}
	p := New(Config{
		Model:               "gemini-3.8-flash-high", // include_conversation_log: false
		SystemPrompt:        "sys",
		ToolDeclarations:    bytes.Repeat([]byte("x"), 180_000),
		EnableSoftThreshold: true,
		Backend:             be,
		Artifacts: []compaction.ArtifactRef{{
			Name:       "plan",
			Path:       "file:///tmp/plan.md",
			LastEdited: "2026-11-07T13:55:31Z",
		}},
		RunningTasks: []compaction.TaskRef{{
			TaskID:   "task-4",
			ToolName: "run_command",
		}},
		Subagents: &compaction.SubagentSection{
			Total: 1,
			Max:   20,
			Entries: []compaction.SubagentRef{{
				TypeName: "DeepCoder",
				Role:     "worker",
			}},
		},
	})
	defer p.Close()
	act := testActor(t)
	probe := probeFor(t, act)
	probe.LastStep = &actor.Step{ToolResults: []actor.ToolResult{{ToolCallID: "c1", Output: "x"}}}

	decision, err := p.BeforeStep(context.Background(), act, probe)
	if err != nil {
		t.Fatalf("BeforeStep: %v", err)
	}
	if !decision.Apply {
		t.Fatal("expected a checkpoint")
	}
	if !strings.Contains(decision.ResumeText, "You have the 1 following artifacts written to the artifacts directory:") {
		t.Errorf("resume lacks the artifact header: %q", decision.ResumeText)
	}
	if !strings.Contains(decision.ResumeText, "[ARTIFACT: plan]\nPath: file:///tmp/plan.md\nLast Edited: 2026-11-07T13:55:31Z") {
		t.Errorf("resume lacks the artifact entry: %q", decision.ResumeText)
	}
	if strings.Contains(decision.ResumeText, "# Conversation Logs") {
		t.Errorf("the profile disabled the log tail, got %q", decision.ResumeText)
	}
	if !strings.Contains(decision.ResumeText, "# Running Background Tasks\n") {
		t.Errorf("resume lacks the running-task section: %q", decision.ResumeText)
	}
	if !strings.Contains(decision.ResumeText, "\"taskId\":  \"task-4\"") {
		t.Errorf("resume lacks the running-task entry: %q", decision.ResumeText)
	}
	if !strings.Contains(decision.ResumeText, "# Subagents\n") {
		t.Errorf("resume lacks the subagent section: %q", decision.ResumeText)
	}
	if !strings.Contains(decision.ResumeText, "\"typeName\":  \"DeepCoder\"") {
		t.Errorf("resume lacks the subagent entry: %q", decision.ResumeText)
	}
}

// TestBeforeStep_ResumeHonorsIncludeConversationLog pins that a profile
// which turns the log tail off does not get it.
func TestBeforeStep_ResumeHonorsIncludeConversationLog(t *testing.T) {
	be := &summaryBackend{text: "<summary>ok</summary>"}
	p := New(Config{
		Model:               "gemini-3.8-flash-high", // include_conversation_log: false
		SystemPrompt:        "sys",
		ToolDeclarations:    bytes.Repeat([]byte("x"), 180_000),
		EnableSoftThreshold: true,
		Backend:             be,
	})
	defer p.Close()
	act := testActor(t)
	probe := probeFor(t, act)
	probe.LastStep = &actor.Step{ToolResults: []actor.ToolResult{{ToolCallID: "c1", Output: "x"}}}

	decision, err := p.BeforeStep(context.Background(), act, probe)
	if err != nil {
		t.Fatalf("BeforeStep: %v", err)
	}
	if !decision.Apply {
		t.Fatal("expected a checkpoint")
	}
	if strings.Contains(decision.ResumeText, "# Conversation Logs") {
		t.Errorf("profile disabled the log tail, got %q", decision.ResumeText)
	}
}

// TestPolicy_SetSnapshotReplacesSections pins the host supply point: a
// persistent policy reads the dynamic sections when it plans a checkpoint, so
// a turn that started a task or a subagent reports them in that checkpoint.
func TestPolicy_SetSnapshotReplacesSections(t *testing.T) {
	be := &summaryBackend{text: "<summary>ok</summary>"}
	p := New(Config{
		Model:               "gemini-3.8-flash-high",
		SystemPrompt:        "sys",
		ToolDeclarations:    bytes.Repeat([]byte("x"), 180_000),
		Backend:             be,
		EnableSoftThreshold: true,
		Artifacts: []compaction.ArtifactRef{{
			Name: "stale", Path: "file:///tmp/stale.md", LastEdited: "2026-01-01T00:00:00Z",
		}},
	})
	defer p.Close()
	p.SetSnapshot(Snapshot{
		Artifacts: []compaction.ArtifactRef{{
			Name: "fresh", Path: "file:///tmp/fresh.md", LastEdited: "2026-11-07T13:55:31Z",
		}},
		RunningTasks: []compaction.TaskRef{{TaskID: "conv/task-9", ToolName: "run_command"}},
		Subagents: &compaction.SubagentSection{
			Total: 1, Max: 20,
			Entries: []compaction.SubagentRef{{TypeName: "DeepCoder", Role: "worker"}},
		},
	})
	act := testActor(t)
	probe := probeFor(t, act)
	probe.LastStep = &actor.Step{ToolResults: []actor.ToolResult{{ToolCallID: "c1", Output: "x"}}}

	if _, err := p.BeforeStep(context.Background(), act, probe); err != nil {
		t.Fatalf("BeforeStep (start): %v", err)
	}
	var (
		decision actor.CheckpointDecision
		err      error
	)
	for i := 0; i < 100; i++ {
		decision, err = p.BeforeStep(context.Background(), act, probe)
		if err != nil {
			t.Fatalf("BeforeStep (apply): %v", err)
		}
		if decision.Apply {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !decision.Apply {
		t.Fatal("expected a checkpoint")
	}
	if strings.Contains(decision.ResumeText, "stale") {
		t.Errorf("resume kept the static artifact: %q", decision.ResumeText)
	}
	if !strings.Contains(decision.ResumeText, "[ARTIFACT: fresh]") {
		t.Errorf("resume lacks the supplied artifact: %q", decision.ResumeText)
	}
	if !strings.Contains(decision.ResumeText, "# Running Background Tasks") {
		t.Errorf("resume lacks the supplied task section: %q", decision.ResumeText)
	}
	if !strings.Contains(decision.ResumeText, "# Subagents") {
		t.Errorf("resume lacks the supplied subagent section: %q", decision.ResumeText)
	}
}

// TestPolicy_SetSnapshotParentMessages pins the subagent path: the host's
// parent messages replace "# User Requests" in the child's resume block.
func TestPolicy_SetSnapshotParentMessages(t *testing.T) {
	be := &summaryBackend{text: "<summary>ok</summary>"}
	p := New(Config{
		Model:               "gemini-3.8-flash-high",
		SystemPrompt:        "sys",
		ToolDeclarations:    bytes.Repeat([]byte("x"), 180_000),
		Backend:             be,
		EnableSoftThreshold: true,
	})
	defer p.Close()
	p.SetSnapshot(Snapshot{ParentMessages: []string{
		"[Message] timestamp=2026-11-03T21:30:22Z sender=parent priority=MESSAGE_PRIORITY_HIGH content=<original_task>\nTask: hello",
	}})
	act := testActor(t)
	probe := probeFor(t, act)
	probe.LastStep = &actor.Step{ToolResults: []actor.ToolResult{{ToolCallID: "c1", Output: "x"}}}

	decision, err := p.BeforeStep(context.Background(), act, probe)
	if err != nil {
		t.Fatalf("BeforeStep: %v", err)
	}
	if !decision.Apply {
		t.Fatal("expected a checkpoint")
	}
	if !strings.Contains(decision.ResumeText, "# Parent Agent Messages\n") {
		t.Errorf("resume lacks the parent section: %q", decision.ResumeText)
	}
	if strings.Contains(decision.ResumeText, "# User Requests") {
		t.Errorf("subagent resume must not carry # User Requests: %q", decision.ResumeText)
	}
}
