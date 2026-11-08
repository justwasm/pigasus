// Package cliapp — bridge.go: bridges turn engine execution with Bubble Tea UI.
//
// EngineBridge wraps an agentturn.Engine and translates protocol frames
// (agentlink.Frame) into standard Bubble Tea messages (tea.Msg) while unifying
// token usage, progress tracking, and permission prompts across backends
// (Local, Remote, ACP, Codex, Claude).
package tui

import (
	"context"
	"errors"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/app/session"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
	"github.com/justwasm/pigasus/internal/ui/views/permissionprompt"
)

// EngineBridge abstracts and coordinates turn execution for the TUI.
type EngineBridge struct {
	turns        *session.TurnRuntime
	engine       agentturn.Engine
	prog         *tea.Program
	promptMgr    *PermissionPromptState
	onUsage      func(*backend.ChunkMeta)
	onFrame      func(agentlink.Frame)
	hookStart    func(context.Context)
	hookPre      func(context.Context)
	hookStop     func(context.Context)
	hookPost     func(context.Context)
	sessionEpoch func() uint64
}

// BridgeOptions configures an EngineBridge.
type BridgeOptions struct {
	Engine    agentturn.Engine
	Program   *tea.Program
	PromptMgr *PermissionPromptState
	OnUsage   func(*backend.ChunkMeta)
	OnFrame   func(agentlink.Frame)
	HookStart func(context.Context)
	HookPre   func(context.Context)
	HookStop  func(context.Context)
	HookPost  func(context.Context)
	// SessionEpoch identifies the host transcript the bridge's turns are
	// built from. The local engine uses it to keep its actor across a
	// checkpoint and to rebuild it after /clear or /resume.
	SessionEpoch func() uint64
}

// NewEngineBridge creates an EngineBridge instance.
func NewEngineBridge(opts BridgeOptions) *EngineBridge {
	b := &EngineBridge{
		turns:        session.NewTurnRuntime(),
		engine:       opts.Engine,
		prog:         opts.Program,
		promptMgr:    opts.PromptMgr,
		onUsage:      opts.OnUsage,
		onFrame:      opts.OnFrame,
		hookStart:    opts.HookStart,
		hookPre:      opts.HookPre,
		hookStop:     opts.HookStop,
		hookPost:     opts.HookPost,
		sessionEpoch: opts.SessionEpoch,
	}
	b.wirePermissionHandler()
	return b
}

// Steer forwards dynamic guidance into the active turn engine.
func (b *EngineBridge) Steer(instruction string) error {
	if b == nil || b.engine == nil {
		return errors.New("bridge: no active engine")
	}
	if s, ok := b.engine.(agentturn.Steerer); ok {
		return s.Steer(instruction)
	}
	return errors.New("bridge: engine does not support steering")
}

// PendingSteer returns a copy of pending steer instructions for the active turn.
func (b *EngineBridge) PendingSteer() []string {
	if b == nil || b.engine == nil {
		return nil
	}
	if sc, ok := b.engine.(agentturn.SteerController); ok {
		return sc.PendingSteer()
	}
	return nil
}

// PopSteer removes and returns the most recent pending steer instruction.
func (b *EngineBridge) PopSteer() (string, bool) {
	if b == nil || b.engine == nil {
		return "", false
	}
	if sc, ok := b.engine.(agentturn.SteerController); ok {
		return sc.PopSteer()
	}
	return "", false
}

// ClearSteer clears all pending steer instructions for the active turn.
func (b *EngineBridge) ClearSteer() []string {
	if b == nil || b.engine == nil {
		return nil
	}
	if sc, ok := b.engine.(agentturn.SteerController); ok {
		return sc.ClearSteer()
	}
	return nil
}

// wirePermissionHandler connects permission requests to the TUI prompt manager.
func (b *EngineBridge) wirePermissionHandler() {
	if b.engine == nil {
		return
	}
	if pa, ok := b.engine.(agentturn.PermissionAware); ok {
		pa.SetPermissionHandler(b.handleACPPermission)
	}
}

// handleACPPermission adapts an ACP PermissionRequest to the TUI permissionprompt modal.
func (b *EngineBridge) handleACPPermission(ctx context.Context, req agentturn.PermissionRequest) (agentturn.PermissionDecision, error) {
	mgr := b.promptMgr
	if mgr == nil {
		mgr = DefaultPermissionPrompts
	}

	opts := make([]permissionprompt.PermissionOption, 0, len(req.Options))
	for _, opt := range req.Options {
		opts = append(opts, permissionprompt.PermissionOption{
			ID:    opt.ID,
			Title: opt.Name,
			Kind:  opt.Kind,
		})
	}

	pReq := permissionprompt.Request{
		ID:          req.ToolCallID,
		Tool:        req.Title,
		Command:     req.RawInput,
		Options:     opts,
		Description: req.Title,
	}

	prompt := mgr.Raise(pReq)
	if b.prog != nil {
		b.prog.Send(PermissionPromptRaisedMsg{Prompt: prompt})
	}

	res, err := prompt.Wait(ctx)
	if err != nil {
		return agentturn.PermissionDecision{Approved: false}, err
	}

	if res.IsApproved() {
		return agentturn.PermissionDecision{
			Approved: true,
			OptionID: res.OptionID,
		}, nil
	}

	return agentturn.PermissionDecision{
		Approved: false,
		OptionID: res.OptionID,
		Feedback: res.Feedback,
	}, nil
}

// PermissionPromptRaisedMsg notifies Bubble Tea that a permission prompt needs to render.
type PermissionPromptRaisedMsg struct {
	Prompt *Prompt
}

// RunTurn executes one turn through the bridge and returns the terminal tea.Msg.
func (b *EngineBridge) RunTurn(ctx context.Context, model string, msgs []backend.Message) tea.Msg {
	turnCtx, endTurn := b.turns.Begin(ctx)
	defer endTurn()
	return b.runTurnOn(turnCtx, model, msgs)
}

// runTurnOn runs a turn whose runtime has already begun. The local stream
// path uses it to begin the runtime on the Update goroutine, so a cancel
// that arrives before the command starts still finds a running turn.
func (b *EngineBridge) runTurnOn(turnCtx context.Context, model string, msgs []backend.Message) tea.Msg {
	if b.hookStart != nil {
		b.hookStart(turnCtx)
	}
	if b.hookPre != nil {
		b.hookPre(turnCtx)
	}
	if b.hookStop != nil {
		defer b.hookStop(turnCtx)
	}
	if b.hookPost != nil {
		defer b.hookPost(turnCtx)
	}

	if b.engine == nil {
		return backendDoneMsg{}
	}

	var epoch uint64
	if b.sessionEpoch != nil {
		epoch = b.sessionEpoch()
	}
	frames, err := b.engine.Turn(turnCtx, agentturn.Turn{
		Model:        model,
		History:      agentturn.ToWireMessages(msgs),
		SessionEpoch: epoch,
	})
	if err != nil {
		return backendDoneMsg{}
	}

	c := &turnCollector{prog: b.prog}
	for f := range frames {
		if b.onFrame != nil {
			b.onFrame(f)
		}
		if f.Usage != nil {
			c.usage(f.Usage)
		}
		switch f.Kind {
		case agentlink.KindText:
			c.text(f.Text)
		case agentlink.KindThought:
			c.thoughtDelta(f.Text)
		case agentlink.KindTool:
			c.tool(f.Tool)
		case agentlink.KindCompact:
			c.compact(f.Compact)
		case agentlink.KindNotice:
			c.notice(f.Notice)
		case agentlink.KindResult:
			c.result(f)
		}
	}

	if c.meta != nil && b.onUsage != nil {
		b.onUsage(c.meta)
	}

	var reasoningTokens int
	if c.meta != nil {
		reasoningTokens = c.meta.ReasoningTokens
	}
	c.steps = toolsteps.AppendThoughtStep(c.steps, c.reasoning.String(), time.Since(c.thoughtStart), reasoningTokens)

	out := c.reply.String()
	if out == "" && len(c.steps) == 0 && c.err == nil {
		return backendDoneMsg{}
	}

	var tm toolsteps.TokenMetrics
	if c.meta != nil {
		tm = toolsteps.TokenMetrics{
			Input:     c.meta.PromptTokens,
			Output:    c.meta.CompletionTokens,
			Reasoning: c.meta.ReasoningTokens,
		}
	}

	return toolsteps.TurnDoneMsg{
		Reply:        out,
		Steps:        c.steps,
		Reasoning:    c.reasoning.String(),
		Err:          c.err,
		TokenMetrics: tm,
	}
}

// CancelTurn cancels an in-flight turn if one is currently executing.
// It returns true if a turn was active and cancelled.
func (b *EngineBridge) CancelTurn() bool {
	if b == nil {
		return false
	}
	return b.turns.Cancel()
}

// IsRunning reports whether a turn is currently in flight through this bridge.
func (b *EngineBridge) IsRunning() bool {
	if b == nil {
		return false
	}
	return b.turns.Running()
}

// ActiveEngine returns the underlying turn engine.
func (b *EngineBridge) ActiveEngine() agentturn.Engine {
	if b == nil {
		return nil
	}
	return b.engine
}

// BridgeAction represents a domain or control operation dispatched to the bridge.
type BridgeAction interface {
	bridgeAction()
}

// ActionCancel requests cancellation of an active turn.
type ActionCancel struct{}

func (ActionCancel) bridgeAction() {}

// ActionSteer delivers mid-turn guidance to the engine.
type ActionSteer struct {
	Instruction string
}

func (ActionSteer) bridgeAction() {}

// ActionSwitchModel notifies the engine of a model switch.
type ActionSwitchModel struct {
	Model string
}

func (ActionSwitchModel) bridgeAction() {}

// Dispatch routes a BridgeAction to the appropriate handler on the bridge or engine.
func (b *EngineBridge) Dispatch(act BridgeAction) error {
	if b == nil {
		return errors.New("bridge: nil bridge")
	}
	switch a := act.(type) {
	case ActionCancel:
		b.CancelTurn()
		return nil
	case ActionSteer:
		return b.Steer(a.Instruction)
	case ActionSwitchModel:
		if ah, ok := b.engine.(agentturn.ActionHandler); ok {
			_, err := ah.ExecuteAction(context.Background(), "model", a.Model)
			return err
		}
		return nil
	default:
		return fmt.Errorf("bridge: unhandled action %T", act)
	}
}
