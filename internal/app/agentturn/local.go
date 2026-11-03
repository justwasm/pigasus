package agentturn

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/core/apierror"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/features/auth/eligibility"
)

// frameBuffer is how many frames a turn may run ahead of its reader.
//
// The turn's producer and its consumer are different goroutines, and a
// turn that produces faster than the reader consumes must not block the
// model's stream — the buffer absorbs the difference. A reader that
// falls further behind than the buffer waits, which is the right
// pressure: the turn slows down rather than the memory growing.
const frameBuffer = 64

// Local runs turns in this process.
//
// The local engine is the reference implementation of the protocol: it
// is what a server runs, and it is what the interactive session runs
// when it is not attached to a server. Both speak the same frames, so
// the two paths cannot drift apart.
type Local struct {
	env EnvFunc

	mu     sync.Mutex
	closed bool
}

// NewLocal returns an engine that runs the turns in this process.
func NewLocal(env EnvFunc) *Local {
	return &Local{env: env}
}

// Turn runs one turn and reports its events on the returned channel.
//
// The channel is closed when the turn ends — with a result frame, or
// with an error frame when the turn could not start at all.
func (l *Local) Turn(ctx context.Context, t Turn) (<-chan agentlink.Frame, error) {
	l.mu.Lock()
	closed := l.closed
	l.mu.Unlock()
	if closed {
		return nil, errors.New("agentturn: the engine is closed")
	}
	if l.env == nil {
		return nil, errors.New("agentturn: the engine has no environment")
	}
	env, cleanup := l.env(ctx, t.Model)
	if cleanup == nil {
		cleanup = func() {}
	}
	model := t.Model
	if model == "" {
		model = env.Model
	}
	if env.Backend == nil {
		cleanup()
		return nil, ErrNoTransport
	}

	ch := make(chan agentlink.Frame, frameBuffer)
	emit := func(f agentlink.Frame) bool {
		select {
		case ch <- f:
			return true
		case <-ctx.Done():
			return false
		}
	}
	go func() {
		defer close(ch)
		defer cleanup()
		l.run(ctx, env, model, t, emit, ch)
	}()
	return ch, nil
}

// resultTimeout bounds the terminator's write.
//
// The terminator is the one frame a turn always sends, so its write is
// bounded rather than cancelled: a reader that is gone must not hold
// the engine's goroutine, and a reader that is there must not be left
// waiting for a frame that will never come.
const resultTimeout = 2 * time.Second

// emitResult writes the turn's terminator.
//
// The terminator is delivered even when the turn was cancelled: the
// client waits for it, and a turn that ends without one leaves the
// client hanging. A cancelled turn is not a failed one — the reference
// reports a cancellation through the composer, not as an error — so the
// terminator carries no error either.
func emitResult(ch chan<- agentlink.Frame, f agentlink.Frame) {
	timer := time.NewTimer(resultTimeout)
	defer timer.Stop()
	select {
	case ch <- f:
	case <-timer.C:
	}
}

// Close releases the engine.
func (l *Local) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
	return nil
}

// EngineName identifies this engine as the native pigasus engine.
func (l *Local) EngineName() string {
	return "pigasus"
}

// Capabilities returns all capabilities supported by the local engine.
func (l *Local) Capabilities() Capability {
	return CapAll
}

// run is the turn itself: it drives the agent loop and reports what the
// loop produced as frames.
func (l *Local) run(ctx context.Context, env Env, model string, t Turn, emit func(agentlink.Frame) bool, ch chan<- agentlink.Frame) {
	var (
		full    strings.Builder
		thought strings.Builder
		steps   []agentlink.Step
		meta    *backend.ChunkMeta
		done    bool
	)

	// The sanitizer strips the reasoning block the upstream streams
	// inline, and hands the reasoning itself to the thought frames.
	sanitizer := agent.NewSanitizer()
	sanitizer.OnThought = func(s string) {
		if s == "" {
			return
		}
		thought.WriteString(s)
		emit(agentlink.Frame{Kind: agentlink.KindThought, Text: s})
	}

	runner := &frameRunner{inner: env.ToolRunner, emit: emit, steps: &steps}

	// The loop treats a missing tool runner as "surface the calls, run
	// none", so a session without one must not be handed a wrapper: the
	// wrapper would turn the missing runner into a failing call.
	var toolRunner backend.ToolRunner
	if env.ToolRunner != nil {
		toolRunner = runner
	}

	// The compaction's duration is the server's own, so it is measured
	// here and carried on the frame.
	var compactStart time.Time

	opts := agent.Options{
		Backend:      env.Backend,
		SystemPrompt: env.SystemPrompt,
		Handler: func(chunk backend.StreamChunk) error {
			if chunk.Error != nil {
				emitError(chunk.Error, emit)
				return nil
			}
			if chunk.Meta != nil {
				meta = chunk.Meta
			}
			if chunk.Text != "" {
				if s := sanitizer.Push(chunk.Text); s != "" {
					full.WriteString(s)
					emit(agentlink.Frame{Kind: agentlink.KindText, Text: s})
				}
			}
			if chunk.Done && !done {
				done = true
				// The tail the sanitizer held back belongs to the
				// answer, but the reference does not stream it — it
				// appears with the finished turn.
				full.WriteString(sanitizer.Finish())
			}
			return nil
		},
		DefaultModel:    model,
		MaxTurns:        1,
		Tools:           env.Tools,
		ToolRunner:      toolRunner,
		ExtraAgentTools: env.ExtraTools,
		Effort:          env.Effort,
		AutoCont:        true,
		OnCompactionStart: func(reason string, startedAt time.Time) {
			compactStart = startedAt
			emit(agentlink.Frame{Kind: agentlink.KindCompact, Compact: &agentlink.Compact{
				Phase:  agentlink.CompactStart,
				Reason: reason,
			}})
		},
		OnCompactionEnd: func(reason string, endedAt time.Time, summary string, tokensBefore, tokensAfter int, aborted bool) {
			frame := agentlink.Frame{Kind: agentlink.KindCompact, Compact: &agentlink.Compact{
				Phase:        agentlink.CompactEnd,
				Reason:       reason,
				Summary:      summary,
				TokensBefore: tokensBefore,
				TokensAfter:  tokensAfter,
				Aborted:      aborted,
			}}
			if !compactStart.IsZero() && !endedAt.Before(compactStart) {
				frame.Compact.ElapsedMS = endedAt.Sub(compactStart).Milliseconds()
			}
			emit(frame)
		},
		AutoContBanner: func(depth, maxDepth int) {
			emit(agentlink.Frame{Kind: agentlink.KindNotice, Notice: &agentlink.Notice{
				Kind:     agentlink.NoticeAutoCont,
				Depth:    depth,
				MaxDepth: maxDepth,
			}})
		},
	}

	loop, err := agent.New(opts)
	if err != nil {
		emit(agentlink.Frame{Kind: agentlink.KindResult, Error: err.Error()})
		return
	}
	defer loop.Close()

	sendErr := loop.Send(ctx, agent.TurnInput{Messages: ToBackendMessages(t.History)})
	if ctx.Err() != nil {
		// A cancelled turn reports the cancellation through the
		// composer, not as a failure of the turn.
		sendErr = nil
	}

	frame := agentlink.Frame{
		Kind: agentlink.KindResult,
		Result: &agentlink.Result{
			Reply:     full.String(),
			Reasoning: thought.String(),
			Steps:     steps,
		},
	}
	if meta != nil {
		frame.Usage = &agentlink.Usage{
			PromptTokens:     meta.PromptTokens,
			CompletionTokens: meta.CompletionTokens,
			TotalTokens:      meta.TotalTokens,
			ReasoningTokens:  meta.ReasoningTokens,
		}
	}
	if sendErr != nil {
		frame.Error = sendErr.Error()
	}
	emitResult(ch, frame)
}

// emitError reports a failure the upstream produced.
//
// An eligibility failure is its own notice: the account cannot run the
// model until it is verified, and the verification URL is what the user
// needs. Every other failure is the turn's error text.
func emitError(err error, emit func(agentlink.Frame) bool) {
	if ee := eligibility.AsError(err); ee != nil {
		emit(agentlink.Frame{Kind: agentlink.KindNotice, Notice: &agentlink.Notice{
			Kind: agentlink.NoticeEligibility,
			Text: ee.Reason,
			URL:  ee.VerificationURL,
		}})
		return
	}
	text := err.Error()
	var he *apierror.HTTPError
	if errors.As(err, &he) && he.IsScopeMismatch() {
		text = text + " " + apierror.OAuthScopeHintMessage
	}
	emit(agentlink.Frame{Kind: agentlink.KindNotice, Notice: &agentlink.Notice{
		Kind: agentlink.NoticeError,
		Text: text,
	}})
}

// frameRunner wraps the host's tool runner and reports the calls as
// frames.
//
// The call is reported twice: once when it starts, so the reader can
// paint the row while the tool runs, and once when it ends, with the
// tool's own output.
type frameRunner struct {
	inner backend.ToolRunner
	emit  func(agentlink.Frame) bool
	steps *[]agentlink.Step
}

func (r *frameRunner) Run(ctx context.Context, call backend.ToolCall) (string, error) {
	if r.inner == nil {
		return "", errors.New("agentturn: the turn has no tool runner")
	}
	r.emit(agentlink.Frame{Kind: agentlink.KindTool, Tool: &agentlink.Tool{
		Phase: agentlink.ToolStart,
		Name:  call.Name,
		Args:  string(call.Arguments),
	}})
	res, err := r.inner.Run(ctx, call)
	r.emit(agentlink.Frame{Kind: agentlink.KindTool, Tool: &agentlink.Tool{
		Phase:  agentlink.ToolEnd,
		Name:   call.Name,
		Args:   string(call.Arguments),
		Result: res,
		Err:    err != nil,
	}})
	if r.steps != nil {
		*r.steps = append(*r.steps, agentlink.Step{
			Name:   call.Name,
			Args:   string(call.Arguments),
			Result: res,
			Err:    err != nil,
		})
	}
	return res, err
}
