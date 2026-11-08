package tui

import (
	"context"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
	"github.com/justwasm/pigasus/internal/ui/views/permissionprompt"
)

type mockBridgeEngine struct {
	frames []agentlink.Frame
}

func (m *mockBridgeEngine) Turn(ctx context.Context, t agentturn.Turn) (<-chan agentlink.Frame, error) {
	ch := make(chan agentlink.Frame, len(m.frames))
	for _, f := range m.frames {
		ch <- f
	}
	close(ch)
	return ch, nil
}

func (m *mockBridgeEngine) Close() error {
	return nil
}

func TestEngineBridge_RunTurn(t *testing.T) {
	eng := &mockBridgeEngine{
		frames: []agentlink.Frame{
			{Kind: agentlink.KindText, Text: "Hello, "},
			{Kind: agentlink.KindText, Text: "world!"},
			{
				Kind: agentlink.KindResult,
				Result: &agentlink.Result{
					Reply: "Hello, world!",
				},
				Usage: &agentlink.Usage{
					PromptTokens:     10,
					CompletionTokens: 5,
					TotalTokens:      15,
				},
			},
		},
	}

	var recordedMeta *backend.ChunkMeta
	var started, stopped bool

	bridge := NewEngineBridge(BridgeOptions{
		Engine: eng,
		OnUsage: func(m *backend.ChunkMeta) {
			recordedMeta = m
		},
		HookStart: func(ctx context.Context) { started = true },
		HookStop:  func(ctx context.Context) { stopped = true },
	})

	msg := bridge.RunTurn(context.Background(), "test-model", []backend.Message{
		{Role: "user", Content: "hi"},
	})

	turnDone, ok := msg.(toolsteps.TurnDoneMsg)
	if !ok {
		t.Fatalf("expected toolsteps.TurnDoneMsg, got %T", msg)
	}

	if turnDone.Reply != "Hello, world!" {
		t.Errorf("got reply %q, want %q", turnDone.Reply, "Hello, world!")
	}
	if !started || !stopped {
		t.Errorf("hooks not called properly: started=%v, stopped=%v", started, stopped)
	}
	if recordedMeta == nil || recordedMeta.TotalTokens != 15 {
		t.Errorf("usage not recorded: got %+v", recordedMeta)
	}
}

func TestEngineBridge_HandleACPPermission(t *testing.T) {
	mgr := NewPermissionPromptState()
	bridge := NewEngineBridge(BridgeOptions{
		PromptMgr: mgr,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Simulate async user approval
	go func() {
		time.Sleep(50 * time.Millisecond)
		active := mgr.Active()
		if active != nil {
			active.Resolve(permissionprompt.Result{
				Decision: permissionprompt.DecisionApproveOnce,
				OptionID: "opt-allow",
			})
		}
	}()

	dec, err := bridge.handleACPPermission(ctx, agentturn.PermissionRequest{
		ToolCallID: "call-1",
		Title:      "Run Command",
		RawInput:   "ls -la",
		Options: []agentturn.PermissionOption{
			{ID: "opt-allow", Name: "Allow Once", Kind: "allow_once"},
			{ID: "opt-deny", Name: "Deny", Kind: "deny"},
		},
	})

	if err != nil {
		t.Fatalf("handleACPPermission failed: %v", err)
	}
	if !dec.Approved || dec.OptionID != "opt-allow" {
		t.Errorf("got decision %+v, want Approved: true, OptionID: opt-allow", dec)
	}
}

type blockingMockEngine struct {
	steerReceived string
}

func (b *blockingMockEngine) Turn(ctx context.Context, t agentturn.Turn) (<-chan agentlink.Frame, error) {
	ch := make(chan agentlink.Frame)
	go func() {
		defer close(ch)
		<-ctx.Done()
	}()
	return ch, nil
}

func (b *blockingMockEngine) Close() error {
	return nil
}

func (b *blockingMockEngine) Steer(instruction string) error {
	b.steerReceived = instruction
	return nil
}

func TestEngineBridge_CancelAndRunning(t *testing.T) {
	eng := &blockingMockEngine{}
	bridge := NewEngineBridge(BridgeOptions{
		Engine: eng,
	})

	if bridge.IsRunning() {
		t.Fatal("bridge should not be running before RunTurn")
	}
	if bridge.ActiveEngine() != eng {
		t.Fatal("ActiveEngine did not return the configured engine")
	}

	done := make(chan tea.Msg, 1)
	go func() {
		msg := bridge.RunTurn(context.Background(), "test-model", []backend.Message{
			{Role: "user", Content: "test"},
		})
		done <- msg
	}()

	// Wait for turn to start
	var running bool
	for i := 0; i < 20; i++ {
		time.Sleep(10 * time.Millisecond)
		if bridge.IsRunning() {
			running = true
			break
		}
	}
	if !running {
		t.Fatal("bridge failed to enter running state")
	}

	// Dispatch Cancel
	err := bridge.Dispatch(ActionCancel{})
	if err != nil {
		t.Fatalf("dispatch cancel failed: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("turn did not terminate after cancel")
	}

	if bridge.IsRunning() {
		t.Fatal("bridge should not be running after cancel")
	}
}

func TestEngineBridge_DispatchSteer(t *testing.T) {
	eng := &blockingMockEngine{}
	bridge := NewEngineBridge(BridgeOptions{
		Engine: eng,
	})

	err := bridge.Dispatch(ActionSteer{Instruction: "steer hint"})
	if err != nil {
		t.Fatalf("dispatch steer failed: %v", err)
	}
	if eng.steerReceived != "steer hint" {
		t.Errorf("steer received = %q, want 'steer hint'", eng.steerReceived)
	}
}
