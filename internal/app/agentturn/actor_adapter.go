package agentturn

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"

	"github.com/justwasm/pigasus/actor"
	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/app/toolrunner"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	"github.com/justwasm/pigasus/internal/features/checkpoint"
)

// backendProviderAdapter adapts a backend.Client into an actor.Provider.
type backendProviderAdapter struct {
	client backend.Client
	model  string
}

func newBackendProviderAdapter(client backend.Client, model string) *backendProviderAdapter {
	return &backendProviderAdapter{
		client: client,
		model:  model,
	}
}

// NewBackendProviderAdapter adapts a backend.Client into an actor.Provider.
func NewBackendProviderAdapter(client backend.Client, model string) actor.Provider {
	return newBackendProviderAdapter(client, model)
}

func (p *backendProviderAdapter) Generate(ctx context.Context, req actor.GenerateRequest) (actor.ResponseStream, error) {
	if p.client == nil {
		return nil, ErrNoTransport
	}

	model := req.Model
	if model == "" {
		model = p.model
	}

	// Convert actor messages to backend messages
	backendMsgs := make([]backend.Message, 0, len(req.Messages))
	for _, m := range req.Messages {
		bm := backend.Message{
			Role:       string(m.Role),
			Content:    m.Content,
			Name:       m.ToolName,
			ToolCallID: m.ToolCallID,
		}
		if len(m.ToolCalls) > 0 {
			bm.ToolCalls = make([]backend.ToolCall, 0, len(m.ToolCalls))
			for _, tc := range m.ToolCalls {
				bm.ToolCalls = append(bm.ToolCalls, backend.ToolCall{
					ID:        tc.ID,
					Name:      tc.Name,
					Arguments: json.RawMessage(tc.Input),
				})
			}
		}
		backendMsgs = append(backendMsgs, bm)
	}

	// Convert actor tools to backend tool declarations
	backendTools := make([]backend.ToolDecl, 0, len(req.Tools))
	for _, t := range req.Tools {
		var rawParams json.RawMessage
		if t.Parameters != nil {
			if b, err := json.Marshal(t.Parameters); err == nil {
				rawParams = b
			}
		}
		backendTools = append(backendTools, backend.ToolDecl{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  rawParams,
		})
	}

	streamReq := backend.StreamRequest{
		Model:        model,
		SystemPrompt: req.SystemPrompt,
		Messages:     backendMsgs,
		Tools:        backendTools,
		MaxTokens:    req.MaxTokens,
	}
	if req.Temperature != nil {
		streamReq.Temperature = float64(*req.Temperature)
	}

	streamCh, err := p.client.Stream(ctx, streamReq)
	if err != nil {
		return nil, err
	}

	sanitizer := NewSanitizer()
	return &backendStreamAdapter{
		ch:        streamCh,
		ctx:       ctx,
		sanitizer: sanitizer,
	}, nil
}

type backendStreamAdapter struct {
	ch        <-chan backend.StreamChunk
	ctx       context.Context
	sanitizer *Sanitizer
	done      bool
}

func (s *backendStreamAdapter) Recv() (actor.StreamChunk, error) {
	if s.done {
		return actor.StreamChunk{}, io.EOF
	}

	select {
	case <-s.ctx.Done():
		return actor.StreamChunk{}, s.ctx.Err()
	case chunk, ok := <-s.ch:
		if !ok {
			s.done = true
			return actor.StreamChunk{}, io.EOF
		}

		if chunk.Error != nil {
			return actor.StreamChunk{}, chunk.Error
		}

		var thoughtBuilder strings.Builder
		if s.sanitizer != nil {
			s.sanitizer.OnThought = func(th string) {
				thoughtBuilder.WriteString(th)
			}
		}

		textDelta := chunk.Text
		if s.sanitizer != nil && textDelta != "" {
			textDelta = s.sanitizer.Push(textDelta)
		}

		if chunk.Done {
			s.done = true
			if s.sanitizer != nil {
				textDelta += s.sanitizer.Finish()
			}
		}

		var toolCalls []actor.ToolCall
		if len(chunk.ToolCalls) > 0 {
			toolCalls = make([]actor.ToolCall, 0, len(chunk.ToolCalls))
			for _, tc := range chunk.ToolCalls {
				toolCalls = append(toolCalls, actor.ToolCall{
					ID:    tc.ID,
					Name:  tc.Name,
					Input: string(tc.Arguments),
				})
			}
		}

		var usage *actor.TokenUsage
		if chunk.Meta != nil {
			usage = &actor.TokenUsage{
				PromptTokens:   chunk.Meta.PromptTokens,
				OutputTokens:   chunk.Meta.CompletionTokens,
				TotalTokens:    chunk.Meta.TotalTokens,
				CachedTokens:   chunk.Meta.CachedTokens,
				ThoughtsTokens: chunk.Meta.ReasoningTokens,
			}
		}

		finishReason := ""
		if chunk.Done {
			finishReason = "stop"
		}

		return actor.StreamChunk{
			Delta:        textDelta,
			Thought:      thoughtBuilder.String(),
			ToolCalls:    toolCalls,
			Usage:        usage,
			FinishReason: finishReason,
		}, nil
	}
}

func (s *backendStreamAdapter) Close() error {
	return nil
}

// GatedTool gates an actor.Tool through the toolrunner middleware pipeline.
type GatedTool struct {
	inner    actor.Tool
	policy   *permissions.Policy
	denials  *[]string
	pipeline *toolrunner.Pipeline
}

// NewGatedTool wraps an actor.Tool with permissions.Policy enforcement.
func NewGatedTool(inner actor.Tool, policy *permissions.Policy, denials *[]string) *GatedTool {
	var onDenial func(string)
	if denials != nil {
		onDenial = func(name string) {
			*denials = append(*denials, name)
		}
	}
	pipeline := toolrunner.NewPipeline(
		toolrunner.PermissionGateMiddleware(policy, nil, onDenial),
	)
	return &GatedTool{
		inner:    inner,
		policy:   policy,
		denials:  denials,
		pipeline: pipeline,
	}
}

func (g *GatedTool) Name() string                     { return g.inner.Name() }
func (g *GatedTool) Description() string              { return g.inner.Description() }
func (g *GatedTool) Definition() actor.ToolDefinition { return g.inner.Definition() }
func (g *GatedTool) RequiresApproval(input string) bool {
	if g.policy == nil {
		return g.inner.RequiresApproval(input)
	}
	target := toolrunner.ToolTarget(backend.ToolCall{Name: g.inner.Name(), Arguments: json.RawMessage(input)})
	if g.policy.Decide(permissions.Request{Tool: g.inner.Name(), Target: target}) == permissions.Ask {
		return true
	}
	return g.inner.RequiresApproval(input)
}

func (g *GatedTool) Execute(ctx context.Context, input string) (actor.ToolResult, error) {
	call := backend.ToolCall{
		Name:      g.inner.Name(),
		Arguments: json.RawMessage(input),
	}
	handler := g.pipeline.Wrap(func(ctx context.Context, c backend.ToolCall) (string, error) {
		res, err := g.inner.Execute(ctx, string(c.Arguments))
		if err != nil {
			return "", err
		}
		if res.Error != "" {
			return res.Error, nil
		}
		return res.Output, nil
	})

	out, err := handler(ctx, call)
	if err != nil {
		return actor.ToolResult{Error: err.Error()}, err
	}
	return actor.ToolResult{Output: out}, nil
}

// buildActorTools creates actor.Tool implementations bridging backend.ToolDecl and ToolRunner.
func buildActorTools(tools []backend.ToolDecl, runner backend.ToolRunner, emit func(agentlink.Frame) bool, steps *[]agentlink.Step) []actor.Tool {
	if len(tools) == 0 {
		return nil
	}

	actorTools := make([]actor.Tool, 0, len(tools))
	for _, decl := range tools {
		d := decl
		var params map[string]any
		if len(d.Parameters) > 0 {
			_ = json.Unmarshal(d.Parameters, &params)
		}

		at := actor.NewFuncTool(
			d.Name,
			d.Description,
			func(ctx context.Context, input string) (string, error) {
				if runner == nil {
					return "", errors.New("agentturn: the turn has no tool runner")
				}
				if emit != nil {
					fr := &frameRunner{
						inner: runner,
						emit:  emit,
						steps: steps,
					}
					return fr.Run(ctx, backend.ToolCall{
						Name:      d.Name,
						Arguments: json.RawMessage(input),
					})
				}
				return runner.Run(ctx, backend.ToolCall{
					Name:      d.Name,
					Arguments: json.RawMessage(input),
				})
			},
			actor.WithParameters(params),
		)
		actorTools = append(actorTools, at)
	}
	return actorTools
}

// EventSink consumes streaming events from an actor.Actor turn execution.
type EventSink interface {
	OnThought(thought string)
	OnDelta(delta string)
	OnToolStart(call actor.ToolCall)
	OnToolFinished(call actor.ToolCall, res actor.ToolResult)
	OnApprovalRequired(ctx context.Context, act *actor.Actor, call actor.ToolCall)
	OnError(err error)
	OnTurnCompleted(summary *actor.TurnSummary)
	OnCancelled()
	OnSteerApplied(instruction string)
}

// FrameSink adapts Actor turn events into agentlink.Frames.
type FrameSink struct {
	emit        func(agentlink.Frame) bool
	permHandler PermissionHandler

	mu        sync.Mutex
	fullReply strings.Builder
	reasoning strings.Builder
	steps     []agentlink.Step
	metaUsage *actor.TokenUsage
	turnErr   string
}

// NewFrameSink creates a FrameSink that emits protocol frames.
func NewFrameSink(emit func(agentlink.Frame) bool, permHandler PermissionHandler) *FrameSink {
	return &FrameSink{
		emit:        emit,
		permHandler: permHandler,
	}
}

func (s *FrameSink) safeEmit(f agentlink.Frame) bool {
	if s == nil || s.emit == nil {
		return false
	}
	defer func() {
		_ = recover()
	}()
	return s.emit(f)
}

func (s *FrameSink) OnThought(thought string) {
	if thought == "" {
		return
	}
	s.mu.Lock()
	s.reasoning.WriteString(thought)
	s.mu.Unlock()
	s.safeEmit(agentlink.Frame{Kind: agentlink.KindThought, Text: thought})
}

func (s *FrameSink) OnDelta(delta string) {
	if delta == "" {
		return
	}
	s.mu.Lock()
	s.fullReply.WriteString(delta)
	s.mu.Unlock()
	s.safeEmit(agentlink.Frame{Kind: agentlink.KindText, Text: delta})
}

func (s *FrameSink) OnToolStart(call actor.ToolCall) {
	s.safeEmit(agentlink.Frame{
		Kind: agentlink.KindTool,
		ID:   call.ID,
		Tool: &agentlink.Tool{
			Phase: agentlink.ToolStart,
			Name:  call.Name,
			Args:  call.Input,
		},
	})
}

func (s *FrameSink) OnToolFinished(call actor.ToolCall, res actor.ToolResult) {
	result := res.Output
	if result == "" && res.Error != "" {
		result = res.Error
	}
	s.safeEmit(agentlink.Frame{
		Kind: agentlink.KindTool,
		ID:   call.ID,
		Tool: &agentlink.Tool{
			Phase:  agentlink.ToolEnd,
			Name:   call.Name,
			Args:   call.Input,
			Result: result,
			Err:    res.Error != "",
		},
	})
	s.mu.Lock()
	s.steps = append(s.steps, agentlink.Step{
		Name:   call.Name,
		Args:   call.Input,
		Result: result,
		Err:    res.Error != "",
	})
	s.mu.Unlock()
}

func (s *FrameSink) OnApprovalRequired(ctx context.Context, act *actor.Actor, call actor.ToolCall) {
	if s.permHandler != nil {
		go func() {
			dec, err := s.permHandler(ctx, PermissionRequest{
				ToolCallID: call.ID,
				Title:      call.Name,
				RawInput:   call.Input,
			})
			if err == nil && dec.Approved {
				_ = act.Approve(call.ID, true, dec.Feedback)
				return
			}
			_ = act.Approve(call.ID, false, dec.Feedback)
		}()
		return
	}
	_ = act.Approve(call.ID, false, "execution denied: no interactive permission handler")
}

func (s *FrameSink) OnError(err error) {
	s.mu.Lock()
	if s.turnErr == "" {
		s.turnErr = err.Error()
	}
	s.mu.Unlock()
	emitError(err, s.emit)
}

func (s *FrameSink) OnTurnCompleted(summary *actor.TurnSummary) {
	s.mu.Lock()
	// A turn may report only reasoning tokens (a thinking-only provider
	// answer) and no total, so a zero total is not "no usage".
	if summary != nil && (summary.TotalTokens > 0 ||
		summary.PromptTokens > 0 || summary.OutputTokens > 0 || summary.ThoughtsTokens > 0) {
		s.metaUsage = &actor.TokenUsage{
			PromptTokens:   summary.PromptTokens,
			OutputTokens:   summary.OutputTokens,
			TotalTokens:    summary.TotalTokens,
			ThoughtsTokens: summary.ThoughtsTokens,
		}
	}
	s.mu.Unlock()
}

func (s *FrameSink) OnCancelled() {
	s.mu.Lock()
	if s.turnErr == "" {
		s.turnErr = "turn cancelled"
	}
	s.mu.Unlock()
}

func (s *FrameSink) OnSteerApplied(instruction string) {
	s.safeEmit(agentlink.Frame{
		Kind: agentlink.KindNotice,
		Notice: &agentlink.Notice{
			Kind: agentlink.NoticeSteerApplied,
			Text: instruction,
		},
	})
}

func (s *FrameSink) ResultFrame(act *actor.Actor, ctxErr error) agentlink.Frame {
	s.mu.Lock()
	replyStr := s.fullReply.String()
	reasoningStr := s.reasoning.String()
	steps := append([]agentlink.Step(nil), s.steps...)
	usage := s.metaUsage
	caughtErr := s.turnErr
	s.mu.Unlock()

	resultFrame := agentlink.Frame{
		Kind: agentlink.KindResult,
		Result: &agentlink.Result{
			Reply:     replyStr,
			Reasoning: reasoningStr,
			Steps:     steps,
		},
	}

	if act != nil {
		trajTurns := act.Trajectory().Turns()
		if len(trajTurns) > 0 {
			lastTurn := trajTurns[len(trajTurns)-1]
			if lastTurn.Status == "error" && lastTurn.Error != "" {
				resultFrame.Error = lastTurn.Error
			}
		}
	}
	if resultFrame.Error == "" && caughtErr != "" {
		resultFrame.Error = caughtErr
	}
	if ctxErr != nil {
		resultFrame.Error = ""
	}

	if usage != nil {
		resultFrame.Usage = &agentlink.Usage{
			PromptTokens:     usage.PromptTokens,
			CompletionTokens: usage.OutputTokens,
			TotalTokens:      usage.TotalTokens,
			ReasoningTokens:  usage.ThoughtsTokens,
		}
	}
	return resultFrame
}

// ChunkSinkOptions configures a ChunkSink.
type ChunkSinkOptions struct {
	Handler func(backend.StreamChunk) error
}

// ChunkSink adapts Actor turn events into backend.StreamChunks.
type ChunkSink struct {
	handler func(backend.StreamChunk) error
	turnErr error
	summary *actor.TurnSummary
}

// NewChunkSink creates a ChunkSink for streaming output chunks.
func NewChunkSink(opts ChunkSinkOptions) *ChunkSink {
	return &ChunkSink{
		handler: opts.Handler,
	}
}

func (s *ChunkSink) OnThought(_ string) {}

func (s *ChunkSink) OnDelta(delta string) {
	if s.handler != nil && delta != "" {
		_ = s.handler(backend.StreamChunk{Text: delta})
	}
}

func (s *ChunkSink) OnToolStart(_ actor.ToolCall)                        {}
func (s *ChunkSink) OnToolFinished(_ actor.ToolCall, _ actor.ToolResult) {}
func (s *ChunkSink) OnApprovalRequired(_ context.Context, act *actor.Actor, call actor.ToolCall) {
	_ = act.Approve(call.ID, false, "execution denied in headless mode")
}

func (s *ChunkSink) OnError(err error) {
	if s.turnErr == nil {
		s.turnErr = err
	}
}

func (s *ChunkSink) OnTurnCompleted(summary *actor.TurnSummary) {
	s.summary = summary
	if summary != nil && summary.Status == "error" && s.turnErr == nil {
		s.turnErr = errors.New("turn completed with error")
	}
	if s.turnErr == nil && s.handler != nil {
		var meta *backend.ChunkMeta
		if summary != nil {
			meta = &backend.ChunkMeta{
				PromptTokens:     summary.PromptTokens,
				CompletionTokens: summary.OutputTokens,
				TotalTokens:      summary.TotalTokens,
			}
		}
		_ = s.handler(backend.StreamChunk{Done: true, Meta: meta})
	}
}

func (s *ChunkSink) OnCancelled() {
	if s.turnErr == nil {
		s.turnErr = errors.New("turn cancelled")
	}
}

func (s *ChunkSink) OnSteerApplied(_ string) {}

// Finish reports any turn error and marks the stream chunk done.
func (s *ChunkSink) Finish() error {
	turnErr := s.turnErr
	if turnErr != nil {
		if s.handler != nil {
			_ = s.handler(backend.StreamChunk{Error: turnErr, Done: true})
		}
		return turnErr
	}
	return nil
}

// ExecuteActorTurn executes a prompt on an actor instance and routes all lifecycle events to sink.
func ExecuteActorTurn(ctx context.Context, act *actor.Actor, prompt string, sink EventSink) error {
	return ExecuteActorTurnRequest(ctx, act, actor.TurnRequest{Prompt: prompt}, sink)
}

// ExecuteActorTurnRequest executes one turn request and routes its events to sink.
func ExecuteActorTurnRequest(ctx context.Context, act *actor.Actor, req actor.TurnRequest, sink EventSink) error {
	if act == nil {
		return errors.New("agentturn: actor is nil")
	}

	turn, err := act.StartTurn(ctx, req)
	if err != nil {
		if sink != nil {
			sink.OnError(err)
		}
		return err
	}

	var turnErr error
	for ev := range turn.Events() {
		switch ev.Type {
		case actor.EventThought:
			if sink != nil {
				sink.OnThought(ev.Thought)
			}
		case actor.EventDelta:
			if sink != nil {
				sink.OnDelta(ev.Delta)
			}
		case actor.EventToolCallStarted:
			if sink != nil && ev.ToolCall != nil {
				sink.OnToolStart(*ev.ToolCall)
			}
		case actor.EventToolCallFinished:
			if sink != nil && ev.ToolResult != nil {
				call := actor.ToolCall{}
				if ev.ToolCall != nil {
					call = *ev.ToolCall
				}
				sink.OnToolFinished(call, *ev.ToolResult)
			}
		case actor.EventApprovalRequired:
			if sink != nil && ev.ToolCall != nil {
				sink.OnApprovalRequired(ctx, act, *ev.ToolCall)
			}
		case actor.EventSteerApplied:
			if sink != nil {
				sink.OnSteerApplied(ev.SteerInstruction)
			}
		case actor.EventError:
			errVal := ev.Err
			if errVal == nil && ev.Error != "" {
				errVal = errors.New(ev.Error)
			}
			if errVal != nil && turnErr == nil {
				turnErr = errVal
			}
			if sink != nil && errVal != nil {
				sink.OnError(errVal)
			}
		case actor.EventTurnCompleted:
			if turnErr == nil {
				if ev.Err != nil {
					turnErr = ev.Err
				} else if ev.Error != "" {
					turnErr = errors.New(ev.Error)
				}
			}
			if sink != nil {
				sink.OnTurnCompleted(ev.TurnSummary)
			}
		case actor.EventCancelled:
			if turnErr == nil {
				turnErr = errors.New("turn cancelled")
			}
			if sink != nil {
				sink.OnCancelled()
			}
		}
	}
	if ctx.Err() != nil {
		_ = turn.Cancel("context cancelled")
	}
	if turnErr == nil {
		turnErr = turn.Err()
	}
	return turnErr
}

// RunActorTurnOnActorWithPermHandler executes a Turn on an existing persistent Actor instance
// with an optional interactive permission handler.
func RunActorTurnOnActorWithPermHandler(
	ctx context.Context,
	act *actor.Actor,
	env Env,
	model string,
	t Turn,
	emit func(agentlink.Frame) bool,
	ch chan<- agentlink.Frame,
	permHandler PermissionHandler,
) {
	if act == nil {
		emit(agentlink.Frame{Kind: agentlink.KindResult, Error: "agentturn: actor is nil"})
		return
	}
	if model == "" {
		model = env.Model
	}
	if env.Backend == nil {
		emit(agentlink.Frame{Kind: agentlink.KindResult, Error: ErrNoTransport.Error()})
		return
	}

	// The turn carries its own configuration: a queued turn must not be
	// rewritten by the next submission's model, prompt, provider or tools.
	provider := newBackendProviderAdapter(env.Backend, model)

	actorTools := buildActorTools(env.Tools, env.ToolRunner, nil, nil)
	if actorTools == nil {
		// A nil tool slice means "inherit the actor's registry"; this turn
		// means "these are the turn's tools", even when it has none.
		actorTools = []actor.Tool{}
	}
	if len(env.ExtraTools) > 0 {
		actorTools = append(actorTools, env.ExtraTools...)
	}
	if env.Policy != nil {
		gated := make([]actor.Tool, 0, len(actorTools))
		for _, t := range actorTools {
			gated = append(gated, NewGatedTool(t, env.Policy, nil))
		}
		actorTools = gated
	}

	var lastPrompt string
	if len(t.History) > 0 {
		lastPrompt = t.History[len(t.History)-1].Content
	}

	policy := env.CheckpointPolicy
	if policy == nil {
		toolDecls, _ := json.Marshal(env.Tools)
		policy = checkpoint.New(checkpoint.Config{
			Model:            model,
			SystemPrompt:     env.SystemPrompt,
			ToolDeclarations: toolDecls,
			Backend:          env.Backend,
			BackendFor:       env.BackendFor,
			Emit:             func(ev checkpoint.Event) { emitCheckpointEvent(emit, ev) },
		})
		defer policy.Close()
	}

	sink := NewFrameSink(emit, permHandler)
	_ = ExecuteActorTurnRequest(ctx, act, actor.TurnRequest{
		Prompt: lastPrompt,
		TurnConfig: actor.TurnConfig{
			Model:        model,
			SystemPrompt: env.SystemPrompt,
			Provider:     provider,
			Tools:        actorTools,
			Checkpoint: func(stepCtx context.Context, probe actor.CheckpointProbe) (actor.CheckpointDecision, error) {
				return policy.BeforeStep(stepCtx, act, probe)
			},
		},
	}, sink)

	resultFrame := sink.ResultFrame(act, ctx.Err())
	emitResult(ch, resultFrame)
}

// emitCheckpointEvent maps a checkpoint progress event to a Compact frame.
func emitCheckpointEvent(emit func(agentlink.Frame) bool, ev checkpoint.Event) {
	if emit == nil {
		return
	}
	compact := &agentlink.Compact{
		Phase:           agentlink.CompactEnd,
		Reason:          ev.Reason,
		Summary:         ev.Summary,
		Resume:          ev.Resume,
		StepsSummarized: ev.Steps,
		KeepRecent:      ev.KeepRecent,
		Strategy:        ev.Strategy,
		SummaryModel:    ev.Model,
		Sync:            ev.Sync,
		TokensBefore:    ev.TokensBefore,
		TokensAfter:     ev.TokensAfter,
		ElapsedMS:       ev.ElapsedMS,
		Aborted:         ev.Aborted,
	}
	if ev.Phase == checkpoint.EventStart {
		compact.Phase = agentlink.CompactStart
	}
	emit(agentlink.Frame{Kind: agentlink.KindCompact, Compact: compact})
}
