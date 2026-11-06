package rchost

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/remotecontrol/cascades"
)

// TurnLock arbitrates turn execution between local TUI and remote control.
type TurnLock interface {
	TryLock() bool
	Unlock()
}

// SimpleTurnLock is a basic mutex-backed TurnLock.
type SimpleTurnLock struct {
	mu sync.Mutex
}

func (l *SimpleTurnLock) TryLock() bool {
	return l.mu.TryLock()
}

func (l *SimpleTurnLock) Unlock() {
	l.mu.Unlock()
}

// EngineCascadeBackend wraps an agentturn.Engine into a cascades.StreamingBackend.
// It is completely stateless and performs in-memory streaming without touching disk or convstore.
type EngineCascadeBackend struct {
	engine   agentturn.Engine
	model    string
	turnLock TurnLock
	mu       sync.Mutex
}

// NewEngineCascadeBackend creates a new stateless bridge from agentturn.Engine to cascades.Backend.
func NewEngineCascadeBackend(engine agentturn.Engine, model string) *EngineCascadeBackend {
	return &EngineCascadeBackend{
		engine: engine,
		model:  model,
	}
}

// SetTurnLock sets an optional TurnLock for mutual exclusion with the TUI.
func (b *EngineCascadeBackend) SetTurnLock(tl TurnLock) {
	b.turnLock = tl
}

// Send implements cascades.Backend (synchronous fall-through).
func (b *EngineCascadeBackend) Send(ctx context.Context, prompt cascades.Prompt) (cascades.Result, error) {
	return b.SendStream(ctx, prompt, nil, cascades.SendOptions{})
}

// SendStream implements cascades.StreamingBackend.
func (b *EngineCascadeBackend) SendStream(ctx context.Context, prompt cascades.Prompt, sink cascades.EventSink, opts cascades.SendOptions) (cascades.Result, error) {
	if b.engine == nil {
		return cascades.Result{}, fmt.Errorf("engine backend: underlying engine is nil")
	}

	if b.turnLock != nil {
		if !b.turnLock.TryLock() {
			err := fmt.Errorf("a turn is already in flight in the local session")
			if sink != nil {
				sink.OnError(nil, err)
			}
			return cascades.Result{StopReason: cascades.StopReasonError}, err
		}
		defer b.turnLock.Unlock()
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	model := b.model
	if model == "" {
		if dm, ok := b.engine.(agentturn.DefaultModeler); ok {
			model = dm.DefaultModel()
		}
	}

	turnReq := agentturn.Turn{
		Model: model,
		History: []agentlink.Message{
			{
				Role:    "user",
				Content: prompt.Text,
			},
		},
	}

	frames, err := b.engine.Turn(ctx, turnReq)
	if err != nil {
		if sink != nil {
			sink.OnError(nil, err)
		}
		return cascades.Result{StopReason: cascades.StopReasonError}, err
	}

	var replyBuilder strings.Builder
	var toolCalls []cascades.ToolCall

	if sink != nil {
		sink.OnState(nil, cascades.StateFrame{Phase: cascades.StatePhaseStarting})
	}

	for f := range frames {
		switch f.Kind {
		case agentlink.KindText:
			replyBuilder.WriteString(f.Text)
			if sink != nil {
				sink.OnTextDelta(nil, f.Text)
			}
		case agentlink.KindThought:
			// Stream thoughts as text delta
			if sink != nil {
				sink.OnTextDelta(nil, f.Text)
			}
		case agentlink.KindTool:
			if f.Tool != nil {
				tc := cascades.ToolCall{
					Name:   f.Tool.Name,
					Args:   []byte(f.Tool.Args),
					Result: f.Tool.Result,
					OK:     !f.Tool.Err,
				}
				toolCalls = append(toolCalls, tc)
				if sink != nil {
					sink.OnToolCall(nil, tc)
				}
			}
		case agentlink.KindResult:
			if f.Error != "" {
				err := fmt.Errorf("%s", f.Error)
				if sink != nil {
					sink.OnError(nil, err)
				}
				return cascades.Result{StopReason: cascades.StopReasonError}, err
			}
		}
	}

	if sink != nil {
		sink.OnState(nil, cascades.StateFrame{Phase: cascades.StatePhaseDone, StopReason: cascades.StopReasonEndTurn})
	}

	return cascades.Result{
		Reply:      replyBuilder.String(),
		ToolCalls:  toolCalls,
		StopReason: cascades.StopReasonEndTurn,
	}, nil
}
