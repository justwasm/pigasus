// Package checkpoint implements the reference's automatic context
// checkpoint (auto-compaction) policy.
//
// The actor asks this package before every model call. The policy decides
// whether the next call needs a fresh context, writes the continuation
// summary with the profile's strategy, and returns the reference's
// "# Resuming from a compaction" block. It never cancels a turn: a checkpoint
// is a step-boundary context replacement, and the turn continues.
package checkpoint

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/justwasm/pigasus/actor"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/compaction"
	"github.com/justwasm/pigasus/internal/features/models/ctx"
)

// EventPhase identifies the checkpoint progress an emitter receives.
type EventPhase string

const (
	// EventStart announces that a summary call began.
	EventStart EventPhase = "start"
	// EventEnd announces that the summary finished or was aborted.
	EventEnd EventPhase = "end"
)

// Event is one checkpoint progress event for a host's UI/wire layer.
type Event struct {
	Phase        EventPhase
	Reason       string
	Summary      string
	Resume       string
	Strategy     string
	Model        string
	Steps        int
	KeepRecent   int
	TokensBefore int
	TokensAfter  int
	ElapsedMS    int64
	Aborted      bool
	Sync         bool
}

// Emitter is a mutable event sink. A persistent policy keeps one emitter
// while the host swaps the callback for the current turn.
type Emitter struct {
	mu sync.Mutex
	fn func(Event)
}

// NewEmitter returns an emitter with no callback yet.
func NewEmitter() *Emitter { return &Emitter{} }

// Set installs the callback for the current turn.
func (e *Emitter) Set(fn func(Event)) {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.fn = fn
	e.mu.Unlock()
}

// Emit forwards one event.
func (e *Emitter) Emit(ev Event) {
	if e == nil {
		return
	}
	e.mu.Lock()
	fn := e.fn
	e.mu.Unlock()
	if fn != nil {
		fn(ev)
	}
}

// Snapshot is the dynamic half of a checkpoint's resume block.
//
// The host supplies it per turn because the three sections describe live
// session state: the artifacts written so far, the background tasks still
// running, and the subagents spawned during the conversation. A host that has
// no such state leaves the fields empty and the sections are omitted.
type Snapshot struct {
	// Artifacts are the conversation's artifacts, in the order they should
	// be listed.
	Artifacts []compaction.ArtifactRef
	// RunningTasks are the background tasks still running.
	RunningTasks []compaction.TaskRef
	// Subagents is the conversation's subagent roster. Nil omits the
	// section.
	Subagents *compaction.SubagentSection
	// ParentMessages is a subagent's parent half, already rendered in the
	// reference's one-line message form. Non-empty replaces "# User
	// Requests" with "# Parent Agent Messages".
	ParentMessages []string
	// ParentMessagesMostRecent selects the capped preamble for
	// ParentMessages.
	ParentMessagesMostRecent bool
}

// Config is the per-turn checkpoint configuration.
type Config struct {
	// Model is the session model whose profile drives the policy.
	Model string
	// SystemPrompt is the normal Antigravity instruction set. The reference
	// keeps it as the summariser's system prompt (it sends no separate one).
	SystemPrompt string
	// ToolDeclarations is the serialized tool catalogue, used only for the
	// base-context estimate.
	ToolDeclarations []byte
	// Backend is the session transport. It is the fallback summariser.
	Backend backend.Client
	// BackendFor returns the transport for another model (SINGLE_PROMPT).
	// Nil means Backend.
	BackendFor func(model string) backend.Client
	// Artifacts are the conversation's current artifacts, rendered in the
	// checkpoint's resume block. A host that has no artifact layer leaves
	// this empty and the section is omitted.
	Artifacts []compaction.ArtifactRef
	// RunningTasks are the background tasks running at checkpoint time.
	// Empty omits the "# Running Background Tasks" section.
	RunningTasks []compaction.TaskRef
	// Subagents is the conversation's subagent snapshot section. Nil omits
	// "# Subagents".
	Subagents *compaction.SubagentSection
	// ParentMessages is a subagent's parent half (see Snapshot).
	ParentMessages []string
	// ParentMessagesMostRecent selects the capped preamble.
	ParentMessagesMostRecent bool
	// Snapshot supplies the dynamic sections per turn. Nil keeps the static
	// Artifacts/RunningTasks/Subagents above.
	Snapshot func() Snapshot
	// EnableSoftThreshold enables the token_threshold trigger. It is off by
	// default: the live 1.5.3 checkpoints observed on this machine were
	// hard-limit checkpoints at ~258k, not 50k soft checkpoints. The
	// threshold's consumer is still unlocated.
	EnableSoftThreshold bool
	// Emit receives start/end events. Nil is allowed.
	Emit func(Event)
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

// Policy is one session's checkpoint policy.
type Policy struct {
	cfg     Config
	profile ctx.Profile
	limit   ctx.Limit

	mu            sync.Mutex
	pending       *job
	priorRequests []string
	closed        bool
	baseCtx       context.Context
	baseCancel    context.CancelFunc

	snapMu sync.Mutex
	snap   Snapshot
}

// job is an in-flight summary. The snapshot is fixed at plan time so steps
// generated after it are never folded into a summary that lands later.
type job struct {
	model            string
	transcript       []backend.Message
	userRequests     []string
	included         int
	keepRecent       int
	keepInput        bool
	artifacts        []compaction.ArtifactRef
	runningTasks     []compaction.TaskRef
	subagents        *compaction.SubagentSection
	parentMessages   []string
	parentMostRecent bool
	includeLog       bool
	steps            int
	tokensBefore     int
	tokensAfter      int
	strategy         string
	sync             bool
	started          time.Time
	done             chan struct{}
	summary          compaction.Summary
	resume           string
	err              error
}

// New builds a policy for a model. A model without a catalogue profile gets a
// conservative fallback: hard-limit checkpoints only, with the session model.
func New(cfg Config) *Policy {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	profile, ok := ctx.ProfileFor(cfg.Model)
	if !ok {
		limit := ctx.For(cfg.Model)
		profile = ctx.Profile{
			Strategy:                   ctx.StrategySameModel,
			MaxTokenLimit:              limit.ContextWindow,
			MaxOutputTokens:            limit.ReserveTokens,
			Enabled:                    true,
			CheckpointModel:            cfg.Model,
			IsSync:                     true,
			MaxUserRequests:            compaction.MaxUserRequests,
			MovingWindowSize:           1,
			UseModelReportedTokenCount: false,
		}
	}
	baseCtx, cancel := context.WithCancel(context.Background())
	return &Policy{
		cfg:        cfg,
		profile:    profile,
		limit:      ctx.For(cfg.Model),
		baseCtx:    baseCtx,
		baseCancel: cancel,
		snap: Snapshot{
			Artifacts:                cfg.Artifacts,
			RunningTasks:             cfg.RunningTasks,
			Subagents:                cfg.Subagents,
			ParentMessages:           cfg.ParentMessages,
			ParentMessagesMostRecent: cfg.ParentMessagesMostRecent,
		},
	}
}

// SetSnapshot replaces the dynamic sections the next checkpoint renders.
//
// A persistent policy lives across turns and its host's session state changes
// between them; the host calls this before each turn (or at plan time) with
// the current artifacts, running tasks and subagent roster. The values are
// copied into the job when a checkpoint is planned, so a summary that lands
// later reports the state the checkpoint was taken at.
func (p *Policy) SetSnapshot(s Snapshot) {
	if p == nil {
		return
	}
	p.snapMu.Lock()
	p.snap = s
	p.snapMu.Unlock()
}

// SetPriorRequests seeds the request list a checkpoint carries.
//
// A host that rebuilt the actor after a manual compaction starts from a
// transcript whose only user message is the resume block. The original
// requests live in the persisted checkpoint, and the reference keeps them in
// every later checkpoint; this is the door the host uses to carry them across
// the rebuild.
func (p *Policy) SetPriorRequests(requests []string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.priorRequests = append([]string(nil), requests...)
	p.mu.Unlock()
}

// currentSnapshot copies the current dynamic sections.
func (p *Policy) currentSnapshot() Snapshot {
	p.snapMu.Lock()
	defer p.snapMu.Unlock()
	return Snapshot{
		Artifacts:                append([]compaction.ArtifactRef(nil), p.snap.Artifacts...),
		RunningTasks:             append([]compaction.TaskRef(nil), p.snap.RunningTasks...),
		Subagents:                p.snap.Subagents,
		ParentMessages:           append([]string(nil), p.snap.ParentMessages...),
		ParentMessagesMostRecent: p.snap.ParentMessagesMostRecent,
	}
}

// Close cancels an in-flight asynchronous summary.
func (p *Policy) Close() {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
	p.baseCancel()
}

// Profile returns the resolved profile (for tests and diagnostics).
func (p *Policy) Profile() ctx.Profile { return p.profile }

// Limit returns the resolved context budget.
func (p *Policy) Limit() ctx.Limit { return p.limit }

// BeforeStep is the actor's checkpoint callback.
func (p *Policy) BeforeStep(ctx context.Context, act *actor.Actor, probe actor.CheckpointProbe) (actor.CheckpointDecision, error) {
	if p == nil || !p.profile.Enabled || act == nil {
		return actor.CheckpointDecision{}, nil
	}

	view := act.Trajectory().CheckpointView()
	p.refreshRequests(act)
	projected := p.projected(probe, view)
	hard := p.hard(projected)

	// Apply an asynchronous summary that finished since the last call.
	if decision, handled, err := p.takePending(ctx, hard, projected); handled {
		return decision, err
	}

	if !p.should(probe, projected, hard) {
		return actor.CheckpointDecision{}, nil
	}

	plan, ok := p.plan(view, probe, projected)
	if !ok {
		return actor.CheckpointDecision{}, nil
	}

	// The reference's SAME_MODEL profiles are synchronous; a hard-limit call
	// must be synchronous regardless of the profile.
	if p.profile.IsSync || hard {
		return p.runSync(ctx, plan, hard)
	}
	return p.startAsync(plan), nil
}

// takePending applies (or reports) an asynchronous summary.
func (p *Policy) takePending(ctx context.Context, hard bool, projected int) (actor.CheckpointDecision, bool, error) {
	p.mu.Lock()
	j := p.pending
	if j == nil {
		p.mu.Unlock()
		return actor.CheckpointDecision{}, false, nil
	}
	select {
	case <-j.done:
		p.pending = nil
		p.mu.Unlock()
		if j.err != nil {
			p.emitEnd(j, j.err)
			if hard {
				return actor.CheckpointDecision{}, true, fmt.Errorf("%w: %v", actor.ErrCheckpointRequired, j.err)
			}
			return actor.CheckpointDecision{}, true, nil
		}
		decision := p.decision(j, projected)
		p.emitEnd(j, nil)
		return decision, true, nil
	default:
		p.mu.Unlock()
		if hard {
			select {
			case <-j.done:
				return p.takePending(ctx, hard, projected)
			case <-ctx.Done():
				return actor.CheckpointDecision{}, true, ctx.Err()
			case <-p.baseCtx.Done():
				return actor.CheckpointDecision{}, true, actor.ErrCheckpointRequired
			}
		}
		return actor.CheckpointDecision{}, true, nil
	}
}

// runSync writes the summary on the turn's context.
func (p *Policy) runSync(ctx context.Context, j *job, hard bool) (actor.CheckpointDecision, error) {
	j.sync = true
	p.emitStart(j)
	p.summarize(ctx, j)
	if j.err != nil {
		p.emitEnd(j, j.err)
		if hard {
			return actor.CheckpointDecision{}, fmt.Errorf("%w: %v", actor.ErrCheckpointRequired, j.err)
		}
		return actor.CheckpointDecision{}, nil
	}
	decision := p.decision(j, j.tokensBefore)
	p.emitEnd(j, nil)
	return decision, nil
}

// startAsync runs the summary in the background and returns no decision yet.
func (p *Policy) startAsync(j *job) actor.CheckpointDecision {
	p.mu.Lock()
	if p.pending != nil || p.closed {
		p.mu.Unlock()
		return actor.CheckpointDecision{}
	}
	p.pending = j
	p.mu.Unlock()

	p.emitStart(j)
	go func() {
		p.summarize(p.baseCtx, j)
		close(j.done)
	}()
	return actor.CheckpointDecision{}
}

func (p *Policy) summarize(ctx context.Context, j *job) {
	client := p.cfg.Backend
	if p.cfg.BackendFor != nil {
		if c := p.cfg.BackendFor(j.model); c != nil {
			client = c
		}
	}
	if client == nil {
		j.err = errors.New("checkpoint: no backend")
		return
	}
	attempts := 0
	if p.profile.RetryMaxRetries > 0 {
		attempts = p.profile.RetryMaxRetries
	}
	delay := time.Duration(p.profile.RetryInitialSleepMS) * time.Millisecond
	if delay <= 0 {
		delay = time.Second
	}
	mult := p.profile.RetryMultiplier
	if mult <= 0 {
		mult = 2
	}
	for attempt := 0; ; attempt++ {
		summary, err := compaction.ComputeSummaryAuto(ctx, client, j.model, p.cfg.SystemPrompt, j.transcript)
		if err == nil {
			j.summary = summary
			j.resume = compaction.ResumeBlock(compaction.ResumeOptions{
				UserRequests:             j.userRequests,
				Summary:                  summary.Text,
				Artifacts:                j.artifacts,
				IncludeConversationLog:   j.includeLog,
				RunningTasks:             j.runningTasks,
				Subagents:                j.subagents,
				ParentMessages:           j.parentMessages,
				ParentMessagesMostRecent: j.parentMostRecent,
			})
			j.tokensAfter = compaction.EstimateTokens(j.resume) +
				compaction.ContextOverheadTokens(p.cfg.SystemPrompt, p.cfg.ToolDeclarations)
			return
		}
		j.err = err
		if attempt >= attempts || ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = time.Duration(float64(delay) * mult)
	}
}

// plan snapshots the transcript and the cut. It returns false when there is
// nothing to summarize.
func (p *Policy) plan(view actor.CheckpointView, probe actor.CheckpointProbe, projected int) (*job, bool) {
	keep := p.profile.MovingWindowSize
	if keep < 0 {
		keep = 0
	}
	cut := len(view.Steps) - keep
	if cut <= 0 {
		// A hard limit cannot keep the moving window if that leaves
		// nothing to summarize: fold every completed step instead.
		if p.hard(projected) {
			cut = len(view.Steps)
		}
		if cut <= 0 {
			return nil, false
		}
	}
	folded := view.Steps[:cut]
	transcript := make([]actor.ConversationMessage, 0, len(folded)+1)
	if view.Resume != "" {
		transcript = append(transcript, actor.ConversationMessage{Role: actor.RoleUser, Content: view.Resume})
	}
	transcript = append(transcript, flatten(folded)...)
	if len(transcript) == 0 {
		return nil, false
	}
	msgs := toBackend(transcript)
	limit := p.profile.MaxUserRequests
	if limit <= 0 {
		limit = compaction.MaxUserRequests
	}
	requests := append([]string(nil), p.priorRequests...)
	requests = append(requests, compaction.UserRequests(toBackend(flatten(folded)), limit)...)
	if len(requests) > limit {
		requests = requests[len(requests)-limit:]
	}
	summaryModel := p.summaryModel()
	steps := cut
	snap := p.currentSnapshot()
	return &job{
		model:            summaryModel,
		transcript:       msgs,
		userRequests:     requests,
		included:         folded[len(folded)-1].GlobalIndex,
		keepRecent:       keep,
		keepInput:        len(view.Pending) > 0,
		artifacts:        snap.Artifacts,
		runningTasks:     snap.RunningTasks,
		subagents:        snap.Subagents,
		parentMessages:   snap.ParentMessages,
		parentMostRecent: snap.ParentMessagesMostRecent,
		includeLog:       p.profile.IncludeConversationLog,
		steps:            steps,
		tokensBefore:     projected,
		strategy:         p.profile.Strategy,
		started:          p.cfg.Now(),
		done:             make(chan struct{}),
	}, true
}

// refreshRequests carries the user requests a previous checkpoint already
// captured. The trajectory keeps them; a new policy (or a new turn) must not
// drop them from the next resume block.
func (p *Policy) refreshRequests(act *actor.Actor) {
	cps := act.Trajectory().Checkpoints()
	if len(cps) == 0 {
		return
	}
	p.mu.Lock()
	p.priorRequests = append([]string(nil), cps[len(cps)-1].UserRequests...)
	p.mu.Unlock()
}

func (p *Policy) summaryModel() string {
	if p.profile.UseLastPlannerModel || p.profile.Strategy == ctx.StrategySameModel {
		return p.cfg.Model
	}
	if p.profile.CheckpointModel != "" {
		if id, ok := ctx.ModelForEnum(p.profile.CheckpointModel); ok {
			return id
		}
	}
	return p.cfg.Model
}

func (p *Policy) decision(j *job, projected int) actor.CheckpointDecision {
	p.mu.Lock()
	p.priorRequests = append([]string(nil), j.userRequests...)
	p.mu.Unlock()
	resume := j.resume
	if resume == "" {
		resume = compaction.ResumeBlock(compaction.ResumeOptions{
			UserRequests:             j.userRequests,
			Summary:                  j.summary.Text,
			Artifacts:                j.artifacts,
			IncludeConversationLog:   j.includeLog,
			RunningTasks:             j.runningTasks,
			Subagents:                j.subagents,
			ParentMessages:           j.parentMessages,
			ParentMessagesMostRecent: j.parentMostRecent,
		})
	}
	after := j.tokensAfter
	if after <= 0 {
		after = compaction.EstimateTokens(resume)
	}
	return actor.CheckpointDecision{
		Apply:               true,
		ResumeText:          resume,
		Summary:             j.summary.Text,
		Model:               j.model,
		Strategy:            j.strategy,
		IncludedThroughStep: j.included,
		KeepRecent:          j.keepRecent,
		KeepInput:           j.keepInput,
		TokensBefore:        projected,
		TokensAfter:         after,
		StepsSummarized:     j.steps,
		Sync:                j.sync,
	}
}

func (p *Policy) projected(probe actor.CheckpointProbe, view actor.CheckpointView) int {
	viewMsgs := make([]actor.ConversationMessage, 0, len(view.Steps)+1)
	if view.Resume != "" {
		viewMsgs = append(viewMsgs, actor.ConversationMessage{Role: actor.RoleUser, Content: view.Resume})
	}
	viewMsgs = append(viewMsgs, flatten(view.Steps)...)
	msgs := toBackend(viewMsgs)
	msgs = append(msgs, toBackend(view.Pending)...)
	estimated := compaction.TranscriptTokens(msgs) +
		compaction.ContextOverheadTokens(p.cfg.SystemPrompt, p.cfg.ToolDeclarations)
	if p.profile.UseModelReportedTokenCount && probe.LastStep != nil && probe.LastStep.Usage != nil {
		reported := probe.LastStep.Usage.PromptTokens + probe.LastStep.Usage.OutputTokens
		if reported > estimated {
			return reported
		}
	}
	return estimated
}

func (p *Policy) hard(projected int) bool {
	threshold := p.limit.ContextWindow - p.limit.ReserveTokens
	return threshold > 0 && projected > threshold
}

func (p *Policy) should(probe actor.CheckpointProbe, projected int, hard bool) bool {
	// The reference's step filter refuses to checkpoint right after a
	// user-input or model-response step. A hard limit overrides it: without
	// the override the next call cannot fit.
	gate := probe.LastStep != nil &&
		(len(probe.LastStep.ToolCalls) > 0 || len(probe.LastStep.ToolResults) > 0)
	if !gate && !hard {
		return false
	}
	if hard {
		return true
	}
	if !p.cfg.EnableSoftThreshold {
		return false
	}
	// token_threshold's consumer was not located in the binary. Live 1.5.3
	// checkpoints were hard-limit checkpoints (~258k), so the soft trigger is
	// opt-in; the guide records the inference.
	return p.profile.TokenThreshold > 0 && projected >= p.profile.TokenThreshold
}

func (p *Policy) emitStart(j *job) {
	if p.cfg.Emit == nil {
		return
	}
	p.cfg.Emit(Event{
		Phase:        EventStart,
		Reason:       "threshold",
		Strategy:     j.strategy,
		Model:        j.model,
		Steps:        j.steps,
		KeepRecent:   j.keepRecent,
		TokensBefore: j.tokensBefore,
		Sync:         j.sync,
	})
}

func (p *Policy) emitEnd(j *job, err error) {
	if p.cfg.Emit == nil {
		return
	}
	elapsed := p.cfg.Now().Sub(j.started).Milliseconds()
	if elapsed <= 0 {
		elapsed = 1
	}
	ev := Event{
		Phase:        EventEnd,
		Reason:       "threshold",
		Strategy:     j.strategy,
		Model:        j.model,
		Steps:        j.steps,
		KeepRecent:   j.keepRecent,
		TokensBefore: j.tokensBefore,
		TokensAfter:  j.tokensAfter,
		ElapsedMS:    elapsed,
		Sync:         j.sync,
	}
	if err != nil {
		ev.Aborted = true
	} else {
		ev.Summary = j.summary.Text
		ev.Resume = j.resume
	}
	p.cfg.Emit(ev)
}

func flatten(steps []actor.CheckpointStep) []actor.ConversationMessage {
	var out []actor.ConversationMessage
	for _, st := range steps {
		out = append(out, st.Messages...)
	}
	return out
}

func toBackend(msgs []actor.ConversationMessage) []backend.Message {
	out := make([]backend.Message, 0, len(msgs))
	for _, m := range msgs {
		b := backend.Message{
			Role:       string(m.Role),
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
			Name:       m.ToolName,
		}
		for _, tc := range m.ToolCalls {
			b.ToolCalls = append(b.ToolCalls, backend.ToolCall{
				ID:        tc.ID,
				Name:      tc.Name,
				Arguments: json.RawMessage(tc.Input),
			})
		}
		out = append(out, b)
	}
	return out
}
