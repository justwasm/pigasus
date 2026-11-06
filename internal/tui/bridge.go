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
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
	"github.com/justwasm/pigasus/internal/ui/views/permissionprompt"
)

// EngineBridge abstracts and coordinates turn execution for the TUI.
type EngineBridge struct {
	engine    agentturn.Engine
	prog      *tea.Program
	promptMgr *PermissionPromptState
	onUsage   func(*backend.ChunkMeta)
	onFrame   func(agentlink.Frame)
	hookStart func(context.Context)
	hookPre   func(context.Context)
	hookStop  func(context.Context)
	hookPost  func(context.Context)
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
}

// NewEngineBridge creates an EngineBridge instance.
func NewEngineBridge(opts BridgeOptions) *EngineBridge {
	b := &EngineBridge{
		engine:    opts.Engine,
		prog:      opts.Program,
		promptMgr: opts.PromptMgr,
		onUsage:   opts.OnUsage,
		onFrame:   opts.OnFrame,
		hookStart: opts.HookStart,
		hookPre:   opts.HookPre,
		hookStop:  opts.HookStop,
		hookPost:  opts.HookPost,
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
	if b.hookStart != nil {
		b.hookStart(ctx)
	}
	if b.hookPre != nil {
		b.hookPre(ctx)
	}
	if b.hookStop != nil {
		defer b.hookStop(ctx)
	}
	if b.hookPost != nil {
		defer b.hookPost(ctx)
	}

	if b.engine == nil {
		return backendDoneMsg{}
	}

	frames, err := b.engine.Turn(ctx, agentturn.Turn{
		Model:   model,
		History: agentturn.ToWireMessages(msgs),
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
