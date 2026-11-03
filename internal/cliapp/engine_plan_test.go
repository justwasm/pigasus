package cliapp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/app/agentturn"
	taskregistry "github.com/justwasm/pigasus/internal/app/task"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
)

// mockPlanEngine implements agentturn.Engine and PlanLister.
type mockPlanEngine struct {
	plan []PlanEntry
}

func (m *mockPlanEngine) Turn(ctx context.Context, t agentturn.Turn) (<-chan agentlink.Frame, error) {
	ch := make(chan agentlink.Frame)
	close(ch)
	return ch, nil
}

func (m *mockPlanEngine) Close() error {
	return nil
}

func (m *mockPlanEngine) Plan() []PlanEntry {
	return m.plan
}

// mockReflectionPlanEngine tests duck-typed engines with custom struct types.
type customPlanItem struct {
	Title     string
	Status    string
	Completed bool
}

type mockReflectionPlanEngine struct {
	items []customPlanItem
}

func (m *mockReflectionPlanEngine) Turn(ctx context.Context, t agentturn.Turn) (<-chan agentlink.Frame, error) {
	ch := make(chan agentlink.Frame)
	close(ch)
	return ch, nil
}

func (m *mockReflectionPlanEngine) Close() error {
	return nil
}

func (m *mockReflectionPlanEngine) Plan() []customPlanItem {
	return m.items
}

// mockUsageEngine emits test frames with Usage telemetry.
type mockUsageEngine struct {
	frames []agentlink.Frame
}

func (m *mockUsageEngine) Turn(ctx context.Context, t agentturn.Turn) (<-chan agentlink.Frame, error) {
	ch := make(chan agentlink.Frame, len(m.frames))
	for _, f := range m.frames {
		ch <- f
	}
	close(ch)
	return ch, nil
}

func (m *mockUsageEngine) Close() error {
	return nil
}

func TestEnginePlan(t *testing.T) {
	t.Run("ImplementsPlanLister", func(t *testing.T) {
		m := &runMode{
			engine: &mockPlanEngine{
				plan: []PlanEntry{
					{Title: "Explore files", Completed: true},
					{Title: "Edit code", Completed: false},
					{Title: "Run tests", Status: "pending"},
				},
			},
		}

		got := m.enginePlan()
		want := "Plan: 1/3 completed"
		if got != want {
			t.Errorf("enginePlan() = %q, want %q", got, want)
		}

		// Also check statusLinePlanProgress helper
		if prog := m.statusLinePlanProgress(); prog != want {
			t.Errorf("statusLinePlanProgress() = %q, want %q", prog, want)
		}
	})

	t.Run("StatusKeywords", func(t *testing.T) {
		m := &runMode{
			engine: &mockPlanEngine{
				plan: []PlanEntry{
					{Title: "Step 1", Status: "completed"},
					{Title: "Step 2", Status: "done"},
					{Title: "Step 3", Status: "finished"},
					{Title: "Step 4", Status: "in_progress"},
				},
			},
		}

		got := m.enginePlan()
		want := "Plan: 3/4 completed"
		if got != want {
			t.Errorf("enginePlan() = %q, want %q", got, want)
		}
	})

	t.Run("EmptyPlan", func(t *testing.T) {
		m := &runMode{
			engine: &mockPlanEngine{
				plan: []PlanEntry{},
			},
		}

		got := m.enginePlan()
		if got != "" {
			t.Errorf("enginePlan() = %q, want empty", got)
		}
	})

	t.Run("ReflectionDuckType", func(t *testing.T) {
		m := &runMode{
			engine: &mockReflectionPlanEngine{
				items: []customPlanItem{
					{Title: "Step 1", Completed: true},
					{Title: "Step 2", Status: "done"},
					{Title: "Step 3", Status: "working"},
				},
			},
		}

		got := m.enginePlan()
		want := "Plan: 2/3 completed"
		if got != want {
			t.Errorf("enginePlan() via reflection = %q, want %q", got, want)
		}
	})

	t.Run("NilEngineOrNoPlan", func(t *testing.T) {
		var nilM *runMode
		if got := nilM.enginePlan(); got != "" {
			t.Errorf("nil runMode enginePlan() = %q, want empty", got)
		}

		m := &runMode{}
		// local engine has no Plan() method
		if got := m.enginePlan(); got != "" {
			t.Errorf("default enginePlan() = %q, want empty", got)
		}
	})

	t.Run("TasksStatusSuffixIntegration", func(t *testing.T) {
		m := &runMode{
			taskReg: taskregistry.New(),
			engine: &mockPlanEngine{
				plan: []PlanEntry{
					{Title: "Step 1", Completed: true},
					{Title: "Step 2", Completed: false},
				},
			},
		}

		// Zero running tasks + plan
		got := m.tasksStatusSuffix()
		want := " · Plan: 1/2 completed"
		if got != want {
			t.Errorf("tasksStatusSuffix() = %q, want %q", got, want)
		}
	})

	t.Run("PayloadDocJSONEnrichment", func(t *testing.T) {
		m := &runMode{
			cwd:            "/test/cwd",
			conversationID: "test-convo-id",
			model:          "claude-4.5-sonnet",
			engine: &mockPlanEngine{
				plan: []PlanEntry{
					{Title: "Step 1", Completed: true},
					{Title: "Step 2", Completed: false},
				},
			},
		}

		doc := m.buildStatusLinePayloadDoc()
		raw, err := json.Marshal(doc)
		if err != nil {
			t.Fatalf("failed to marshal payload doc: %v", err)
		}

		var decoded map[string]any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("failed to unmarshal JSON: %v", err)
		}

		if decoded["plan"] != "Plan: 1/2 completed" {
			t.Errorf("decoded['plan'] = %v, want 'Plan: 1/2 completed'", decoded["plan"])
		}
		if decoded["cwd"] != "/test/cwd" {
			t.Errorf("decoded['cwd'] = %v, want '/test/cwd'", decoded["cwd"])
		}
	})
}

func TestTurnCollector_Usage(t *testing.T) {
	t.Run("TurnCollectorDirectResult", func(t *testing.T) {
		c := &turnCollector{}
		f := agentlink.Frame{
			Kind: agentlink.KindResult,
			Usage: &agentlink.Usage{
				PromptTokens:     1250,
				CompletionTokens: 350,
				TotalTokens:      1600,
				ReasoningTokens:  80,
			},
			Result: &agentlink.Result{
				Reply: "Turn completed successfully.",
			},
		}

		c.result(f)

		if c.meta == nil {
			t.Fatalf("c.meta is nil after result frame with usage")
		}
		if c.meta.PromptTokens != 1250 {
			t.Errorf("PromptTokens = %d, want 1250", c.meta.PromptTokens)
		}
		if c.meta.CompletionTokens != 350 {
			t.Errorf("CompletionTokens = %d, want 350", c.meta.CompletionTokens)
		}
		if c.meta.TotalTokens != 1600 {
			t.Errorf("TotalTokens = %d, want 1600", c.meta.TotalTokens)
		}
		if c.meta.ReasoningTokens != 80 {
			t.Errorf("ReasoningTokens = %d, want 80", c.meta.ReasoningTokens)
		}

		// Pass through to recordTurnUsage and verify contextUsage
		m := &runMode{
			model: "gpt-oss-120b",
		}
		m.recordTurnUsage(c.meta)

		prompt, completion := m.sessionTokenTotals()
		if prompt != 1250 {
			t.Errorf("SessionPromptTokens = %d, want 1250", prompt)
		}
		if completion != 350 {
			t.Errorf("SessionCompletionTokens = %d, want 350", completion)
		}

		pct := m.contextUsage()
		if pct <= 0 {
			t.Errorf("contextUsage() = %v, want > 0", pct)
		}
	})

	t.Run("TurnCollectorIntermediateUsageFrame", func(t *testing.T) {
		c := &turnCollector{}
		u := &agentlink.Usage{
			PromptTokens:     2000,
			CompletionTokens: 500,
			TotalTokens:      2500,
			ReasoningTokens:  100,
		}
		c.usage(u)

		if c.meta == nil {
			t.Fatalf("c.meta is nil after calling usage()")
		}
		if c.meta.PromptTokens != 2000 || c.meta.TotalTokens != 2500 {
			t.Errorf("unexpected meta: %+v", c.meta)
		}
	})

	t.Run("RunTurnEndToEndWithUsage", func(t *testing.T) {
		engine := &mockUsageEngine{
			frames: []agentlink.Frame{
				{Kind: agentlink.KindText, Text: "Thinking..."},
				{
					Kind: agentlink.KindResult,
					Usage: &agentlink.Usage{
						PromptTokens:     3000,
						CompletionTokens: 400,
						TotalTokens:      3400,
						ReasoningTokens:  150,
					},
					Result: &agentlink.Result{
						Reply: "Done!",
					},
				},
			},
		}

		m := &runMode{
			model:   "claude-4.5-sonnet",
			engine:  engine,
			backend: nil,
		}

		msg := m.runTurn(context.Background(), []backend.Message{
			{Role: "user", Content: "Hello"},
		})

		turnDone, ok := msg.(toolsteps.TurnDoneMsg)
		if !ok {
			t.Fatalf("expected toolsteps.TurnDoneMsg, got %T", msg)
		}

		if turnDone.Reply != "Done!" {
			t.Errorf("reply = %q, want 'Done!'", turnDone.Reply)
		}
		if turnDone.TokenMetrics.Input != 3000 {
			t.Errorf("TokenMetrics.Input = %d, want 3000", turnDone.TokenMetrics.Input)
		}
		if turnDone.TokenMetrics.Output != 400 {
			t.Errorf("TokenMetrics.Output = %d, want 400", turnDone.TokenMetrics.Output)
		}

		prompt, completion := m.sessionTokenTotals()
		if prompt != 3000 {
			t.Errorf("m.store.SessionPromptTokens = %d, want 3000", prompt)
		}
		if completion != 400 {
			t.Errorf("m.store.SessionCompletionTokens = %d, want 400", completion)
		}

		// contextUsage calculation
		usagePct := m.contextUsage()
		if usagePct <= 0 {
			t.Errorf("m.contextUsage() = %v, want > 0", usagePct)
		}
	})
}
