package tui

// turn_service.go — the transport composition sub-model.
//
// TurnService owns the transport a turn runs on: the backend client, the
// factory that rebuilds it after a model change, the turn engine, the
// active EngineBridge and the running *tea.Program. It is the seam the
// host hands to EngineBridge; the transcript and the panels never need
// to know which transport answered.
//
// Upstream: the reference's RootModel composes its backend and engine in
// one place; the clone gives that composition a named owner so the
// engine bucket can become view state.

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/core/backend"
)

// TurnService owns the turn transport and its in-flight bridge.
type TurnService struct {
	backend    backend.Client
	backendFor func(model string) backend.Client
	engine     agentturn.Engine
	// activeBridge is the bridge running the current turn. It is nil
	// between turns.
	activeBridge *EngineBridge
	prog         *tea.Program
}

// Backend returns the active backend client.
func (t *TurnService) Backend() backend.Client {
	if t == nil {
		return nil
	}
	return t.backend
}

// SetBackend installs the active backend client.
func (t *TurnService) SetBackend(b backend.Client) {
	if t != nil {
		t.backend = b
	}
}

// BackendFor returns the model-change factory, or nil.
func (t *TurnService) BackendFor() func(model string) backend.Client {
	if t == nil {
		return nil
	}
	return t.backendFor
}

// SetBackendFor installs the model-change factory.
func (t *TurnService) SetBackendFor(f func(model string) backend.Client) {
	if t != nil {
		t.backendFor = f
	}
}

// RebuildBackend rebuilds the backend for model when a factory is
// wired, records it and returns it. It returns nil when there is no
// factory.
func (t *TurnService) RebuildBackend(model string) backend.Client {
	if t == nil || t.backendFor == nil {
		return nil
	}
	b := t.backendFor(model)
	if b != nil {
		t.backend = b
	}
	return b
}

// Engine returns the engine that runs the session's turns.
func (t *TurnService) Engine() agentturn.Engine {
	if t == nil {
		return nil
	}
	return t.engine
}

// SetEngine installs the turn engine.
func (t *TurnService) SetEngine(engine agentturn.Engine) {
	if t != nil {
		t.engine = engine
	}
}

// ActiveBridge returns the bridge running the current turn.
func (t *TurnService) ActiveBridge() *EngineBridge {
	if t == nil {
		return nil
	}
	return t.activeBridge
}

// SetActiveBridge records the bridge running the current turn.
func (t *TurnService) SetActiveBridge(bridge *EngineBridge) {
	if t != nil {
		t.activeBridge = bridge
	}
}

// ClearActiveBridge drops the current turn's bridge.
func (t *TurnService) ClearActiveBridge() {
	if t != nil {
		t.activeBridge = nil
	}
}

// Program returns the running Bubble Tea program.
func (t *TurnService) Program() *tea.Program {
	if t == nil {
		return nil
	}
	return t.prog
}

// SetProgram records the running Bubble Tea program.
func (t *TurnService) SetProgram(prog *tea.Program) {
	if t != nil {
		t.prog = prog
	}
}

// CancelTurn cancels the in-flight turn through its bridge.
func (t *TurnService) CancelTurn() bool {
	if t == nil || t.activeBridge == nil {
		return false
	}
	return t.activeBridge.CancelTurn()
}

// IsRunning reports whether a turn is in flight.
func (t *TurnService) IsRunning() bool {
	if t == nil || t.activeBridge == nil {
		return false
	}
	return t.activeBridge.IsRunning()
}
