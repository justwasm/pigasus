package agentturn

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/actor"
	"github.com/justwasm/pigasus/agentlink"
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

	mu           sync.Mutex
	closed       bool
	activeActor  *actor.Actor
	permHandler  PermissionHandler
	pendingSteer []string
}

var _ Engine = (*Local)(nil)
var _ CapabilityProvider = (*Local)(nil)
var _ ActionHandler = (*Local)(nil)
var _ Steerer = (*Local)(nil)
var _ SteerController = (*Local)(nil)
var _ PermissionAware = (*Local)(nil)

// NewLocal returns an engine that runs the turns in this process using the actor.Actor runtime.
func NewLocal(env EnvFunc) *Local {
	return &Local{env: env}
}

// NewActor returns an engine that runs turns using the actor.Actor runtime.
func NewActor(env EnvFunc) *Local {
	return NewLocal(env)
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
	var (
		chMu     sync.Mutex
		chClosed bool
	)
	safeClose := func() {
		chMu.Lock()
		defer chMu.Unlock()
		if !chClosed {
			chClosed = true
			close(ch)
		}
	}
	emit := func(f agentlink.Frame) (sent bool) {
		defer func() {
			if r := recover(); r != nil {
				sent = false
			}
		}()
		if ctx.Err() != nil {
			return false
		}
		chMu.Lock()
		defer chMu.Unlock()
		if chClosed {
			return false
		}
		select {
		case ch <- f:
			return true
		case <-ctx.Done():
			return false
		}
	}
	go func() {
		defer safeClose()
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
	defer func() {
		_ = recover()
	}()
	timer := time.NewTimer(resultTimeout)
	defer timer.Stop()
	select {
	case ch <- f:
	case <-timer.C:
	}
}

// Close releases the engine and terminates its persistent actor runtime.
func (l *Local) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
	if l.activeActor != nil {
		_ = l.activeActor.Close()
		l.activeActor = nil
	}
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

// ExecuteAction executes a local engine action.
func (l *Local) ExecuteAction(ctx context.Context, action string, args string) (ActionResult, error) {
	return ActionResult{
		Success: true,
		Message: "Action processed by local engine",
	}, nil
}

// SetPermissionHandler wires an interactive permission handler to the local turn engine.
func (l *Local) SetPermissionHandler(h PermissionHandler) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.permHandler = h
}

// Steer forwards dynamic guidance into the active Actor running in this engine.
func (l *Local) Steer(instruction string) error {
	instruction = strings.TrimSpace(instruction)
	if instruction == "" {
		return errors.New("agentturn: steer instruction cannot be empty")
	}
	l.mu.Lock()
	act := l.activeActor
	if act == nil {
		l.pendingSteer = append(l.pendingSteer, instruction)
		l.mu.Unlock()
		return nil
	}
	l.mu.Unlock()
	return act.Steer(instruction)
}

// PendingSteer returns a snapshot of pending steering instructions for the active turn.
func (l *Local) PendingSteer() []string {
	l.mu.Lock()
	act := l.activeActor
	pending := append([]string(nil), l.pendingSteer...)
	l.mu.Unlock()
	if act != nil {
		pending = append(pending, act.PendingSteer()...)
	}
	return pending
}

// PopSteer removes and returns the most recent pending steering instruction.
func (l *Local) PopSteer() (string, bool) {
	l.mu.Lock()
	act := l.activeActor
	if act != nil {
		if msg, ok := act.PopSteer(); ok {
			l.mu.Unlock()
			return msg, true
		}
	}
	if len(l.pendingSteer) > 0 {
		last := l.pendingSteer[len(l.pendingSteer)-1]
		l.pendingSteer = l.pendingSteer[:len(l.pendingSteer)-1]
		l.mu.Unlock()
		return last, true
	}
	l.mu.Unlock()
	return "", false
}

// ClearSteer clears all pending steering instructions for the active turn.
func (l *Local) ClearSteer() []string {
	l.mu.Lock()
	act := l.activeActor
	cleared := l.pendingSteer
	l.pendingSteer = nil
	l.mu.Unlock()
	if act != nil {
		cleared = append(cleared, act.ClearSteer()...)
	}
	return cleared
}

func (l *Local) setActiveActor(act *actor.Actor) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.activeActor = act
}

// run is the turn itself: it drives the turn using the actor.Actor runtime
// and reports what the actor produced as frames.
func (l *Local) run(ctx context.Context, env Env, model string, t Turn, emit func(agentlink.Frame) bool, ch chan<- agentlink.Frame) {
	if model == "" {
		model = env.Model
	}
	if env.Backend == nil {
		emit(agentlink.Frame{Kind: agentlink.KindResult, Error: ErrNoTransport.Error()})
		return
	}

	l.mu.Lock()
	act := l.activeActor
	// If active actor exists but is closed, or if history was reset/cleared, discard it
	if act != nil {
		storedTurns := act.Trajectory().TotalTurns()
		if act.State().State == actor.StateClosed || (storedTurns > 0 && len(t.History) <= 1) || (storedTurns > 0 && len(t.History) <= storedTurns) {
			_ = act.Close()
			act = nil
			l.activeActor = nil
		}
	}

	if act == nil {
		var initialHistory []actor.ConversationMessage
		if len(t.History) > 1 {
			for i := 0; i < len(t.History)-1; i++ {
				msg := t.History[i]
				initialHistory = append(initialHistory, actor.ConversationMessage{
					Role:    actor.Role(msg.Role),
					Content: msg.Content,
				})
			}
		}

		provider := newBackendProviderAdapter(env.Backend, model)
		var err error
		act, err = actor.Spawn(actor.Config{
			Model:          model,
			SystemPrompt:   env.SystemPrompt,
			Provider:       provider,
			InitialHistory: initialHistory,
		})
		if err != nil {
			l.mu.Unlock()
			emit(agentlink.Frame{Kind: agentlink.KindResult, Error: err.Error()})
			return
		}
		l.activeActor = act
	}
	if len(l.pendingSteer) > 0 {
		for _, s := range l.pendingSteer {
			_ = act.Steer(s)
		}
		l.pendingSteer = nil
	}
	permHandler := l.permHandler
	l.mu.Unlock()

	RunActorTurnOnActorWithPermHandler(ctx, act, env, model, t, emit, ch, permHandler)
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
