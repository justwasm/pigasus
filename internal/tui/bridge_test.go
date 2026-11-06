package tui

import (
	"context"
	"testing"
	"time"

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
