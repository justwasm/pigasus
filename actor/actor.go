package actor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Actor represents one agent's serialized turn runtime.
type Actor struct {
	id         string
	mailbox    chan *turnEnvelope
	fsm        *actorStateMachine
	trajectory *Trajectory
	config     Config
	now        func() time.Time

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	mu              sync.RWMutex
	current         *TurnHandle
	turnCancel      context.CancelFunc
	stepCancel      context.CancelFunc
	pendingApproval *approvalDecision
	approvalWake    chan struct{}
	pendingSteer    []string
	pending         map[*TurnHandle]struct{}
}

// approvalDecision is the single pending answer to an approval wait. It is
// unexported because approvals are a method call, not a message.
type approvalDecision struct {
	toolCallID string
	approved   bool
	feedback   string
}

// Spawn initializes and launches a new Actor runtime.
func Spawn(cfg Config) (*Actor, error) {
	if cfg.Default.Provider == nil {
		return nil, fmt.Errorf("actor: provider must not be nil")
	}
	cfg.EnsureDefaults()

	id := cfg.ID
	if id == "" {
		id = newID("act")
	}

	ctx, cancel := context.WithCancel(context.Background())

	trajectory := newTrajectoryWithClock(cfg.TimeNow)
	for _, tr := range cfg.InitialTurns {
		trajectory.AddTurn(tr)
	}
	if len(cfg.InitialHistory) > 0 {
		trajectory.SeedHistory(cfg.InitialHistory)
	}

	a := &Actor{
		id:           id,
		mailbox:      make(chan *turnEnvelope, mailboxSize),
		fsm:          newActorStateMachine(),
		trajectory:   trajectory,
		config:       cfg,
		now:          cfg.TimeNow,
		ctx:          ctx,
		cancel:       cancel,
		done:         make(chan struct{}),
		approvalWake: make(chan struct{}, 1),
		pendingSteer: make([]string, 0),
		pending:      make(map[*TurnHandle]struct{}),
	}

	go a.loop()

	return a, nil
}

// StartTurn submits a turn and returns a handle for it.
//
// The turn is queued behind any turn already running. Its configuration is
// resolved now, so a later submission cannot rewrite it. The handle's event
// channel is lossless and is closed when the turn ends; Done is closed just
// after it and Err reports the terminal error.
//
// A caller that stops reading Events before Done must cancel the turn (or the
// actor), because delivery applies backpressure to the turn loop.
func (a *Actor) StartTurn(ctx context.Context, req TurnRequest) (*TurnHandle, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	cfg, err := a.resolveTurnConfig(req)
	if err != nil {
		return nil, err
	}

	h := &TurnHandle{
		actor:  a,
		raw:    make(chan Event, a.config.EventBufferSize),
		events: make(chan Event, a.config.EventBufferSize),
		done:   make(chan struct{}),
		stopCh: make(chan struct{}),
	}
	go h.relay()

	a.mu.Lock()
	if a.fsm.State() == StateClosed {
		a.mu.Unlock()
		h.complete(ErrActorClosed)
		return nil, ErrActorClosed
	}
	a.pending[h] = struct{}{}
	a.mu.Unlock()
	go h.watchContext(ctx)

	env := &turnEnvelope{
		handle: h,
		config: cfg,
		prompt: req.Prompt,
	}

	select {
	case <-ctx.Done():
		a.mu.Lock()
		delete(a.pending, h)
		a.mu.Unlock()
		h.complete(ctx.Err())
		return nil, ctx.Err()
	case <-a.ctx.Done():
		a.mu.Lock()
		delete(a.pending, h)
		a.mu.Unlock()
		h.complete(ErrActorClosed)
		return nil, ErrActorClosed
	case a.mailbox <- env:
		return h, nil
	}
}

// resolveTurnConfig overlays a request on the actor's defaults at submission
// time.
func (a *Actor) resolveTurnConfig(req TurnRequest) (TurnConfig, error) {
	cfg := a.config.Default
	if req.Model != "" {
		cfg.Model = req.Model
	}
	if req.SystemPrompt != "" {
		cfg.SystemPrompt = req.SystemPrompt
	}
	if req.Provider != nil {
		cfg.Provider = req.Provider
	}
	if req.Temperature != nil {
		cfg.Temperature = req.Temperature
	}
	if req.MaxTokens != 0 {
		cfg.MaxTokens = req.MaxTokens
	}
	if req.Checkpoint != nil {
		cfg.Checkpoint = req.Checkpoint
	}
	if cfg.Provider == nil {
		return cfg, fmt.Errorf("actor: turn has no provider")
	}

	tools := cfg.Tools
	if req.Tools != nil {
		tools = req.Tools
	}
	cfg.tools = NewToolRegistry()
	cfg.tools.Register(tools...)
	return cfg, nil
}

// Prompt submits a text prompt with the actor's default configuration.
func (a *Actor) Prompt(ctx context.Context, text string) (*TurnHandle, error) {
	return a.StartTurn(ctx, TurnRequest{Prompt: text})
}

// Approve delivers an approval or denial for a tool execution waiting for authorization.
//
// The decision is a single, replaceable slot rather than a queue: a repeated
// approval for the same call overwrites the previous one, and a decision for a
// call that is no longer waiting is rejected. A queued approval could otherwise
// outlive its wait and be consumed by the next one, rejecting a call the user
// approved.
func (a *Actor) Approve(toolCallID string, approved bool, feedback string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.fsm.State() != StateWaitingApproval || a.fsm.WaitingToolCallID() != toolCallID {
		return fmt.Errorf("actor is not currently waiting for approval for tool_call_id %q", toolCallID)
	}

	a.pendingApproval = &approvalDecision{
		toolCallID: toolCallID,
		approved:   approved,
		feedback:   feedback,
	}
	select {
	case a.approvalWake <- struct{}{}:
	default:
	}
	return nil
}

// CancelTurn cancels the running turn. Its handle (when it has one) is closed
// immediately, so a caller waiting on Events is not held hostage by a tool that
// ignores its context. Cancelling a specific queued turn is TurnHandle.Cancel;
// an idle actor is ErrTurnNotFound, so a late cancel cannot be mistaken for a
// success.
func (a *Actor) CancelTurn(reason string) error {
	a.mu.Lock()
	if a.turnCancel == nil {
		a.mu.Unlock()
		return ErrTurnNotFound
	}
	cancel := a.turnCancel
	stepCancel := a.stepCancel
	handle := a.current
	a.turnCancel = nil
	a.stepCancel = nil
	a.mu.Unlock()

	if handle != nil {
		handle.stop(cancelError(reason))
	}
	if stepCancel != nil {
		stepCancel()
	}
	if cancel != nil {
		cancel()
	}
	return nil
}

// cancelError keeps the caller's reason on the handle's terminal error.
func cancelError(reason string) error {
	if trimmed := strings.TrimSpace(reason); trimmed != "" {
		return fmt.Errorf("%w: %s", ErrTurnCancelled, trimmed)
	}
	return ErrTurnCancelled
}

// Steer injects dynamic guidance into an in-flight turn (or queues guidance for
// the next turn). It operates concurrently, out-of-band, without cancelling the
// active step; the loop consumes the queue at the next step boundary.
func (a *Actor) Steer(instruction string) error {
	instruction = strings.TrimSpace(instruction)
	if instruction == "" {
		return errors.New("actor: steer instruction cannot be empty")
	}

	a.mu.Lock()
	if a.fsm.State() == StateClosed {
		a.mu.Unlock()
		return ErrActorClosed
	}
	a.pendingSteer = append(a.pendingSteer, instruction)
	a.mu.Unlock()
	return nil
}

// PendingSteer returns a copy of the pending steer instructions waiting to be processed.
func (a *Actor) PendingSteer() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if len(a.pendingSteer) == 0 {
		return nil
	}
	res := make([]string, len(a.pendingSteer))
	copy(res, a.pendingSteer)
	return res
}

// PopSteer removes and returns the most recent pending steer instruction.
func (a *Actor) PopSteer() (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := len(a.pendingSteer)
	if n == 0 {
		return "", false
	}
	last := a.pendingSteer[n-1]
	a.pendingSteer = a.pendingSteer[:n-1]
	return last, true
}

// ClearSteer clears and returns all pending steer instructions.
func (a *Actor) ClearSteer() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.pendingSteer) == 0 {
		return nil
	}
	cleared := a.pendingSteer
	a.pendingSteer = make([]string, 0)
	return cleared
}

// State returns a snapshot of the actor's current status and trajectory counts.
func (a *Actor) State() StateSnapshot {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return StateSnapshot{
		ActorID:              a.id,
		State:                a.fsm.State(),
		WaitingToolCallID:    a.fsm.WaitingToolCallID(),
		PendingMessagesCount: len(a.mailbox),
		TotalTurns:           a.trajectory.TotalTurns(),
		TotalSteps:           a.trajectory.TotalSteps(),
		UpdatedAt:            a.now(),
	}
}

// Trajectory returns the actor's execution history.
func (a *Actor) Trajectory() *Trajectory {
	return a.trajectory
}

// ResetHistory resets the actor's trajectory with a fresh set of conversation messages.
func (a *Actor) ResetHistory(history []ConversationMessage) {
	a.trajectory.resetWithHistory(history)
}

// Close shuts down the actor, cancels any active turn, and closes every
// outstanding handle with ErrActorClosed.
//
// Close waits for the turn loop to stop, so it must not be called from inside a
// Tool.Execute: the tool runs on that loop's goroutine and the wait would
// deadlock. A tool that needs to stop its actor calls CancelTurn instead.
func (a *Actor) Close() error {
	a.mu.Lock()
	if a.fsm.State() == StateClosed {
		a.mu.Unlock()
		return nil
	}

	if a.turnCancel != nil {
		a.turnCancel()
		a.turnCancel = nil
	}
	a.cancel()
	a.mu.Unlock()

	_ = a.fsm.Transition(StateClosed, "")

	a.mu.Lock()
	outstanding := make([]*TurnHandle, 0, len(a.pending))
	for h := range a.pending {
		outstanding = append(outstanding, h)
	}
	a.mu.Unlock()
	for _, h := range outstanding {
		h.stop(ErrActorClosed)
	}

	<-a.done
	return nil
}

// TurnHandle is one submitted turn's observable surface.
//
// The turn loop writes to raw; a relay owns the public events channel so a
// cancellation can close it even while the loop is blocked inside a tool that
// ignores its context.
type TurnHandle struct {
	actor  *Actor
	raw    chan Event
	events chan Event
	done   chan struct{}
	stopCh chan struct{}

	// cancelled and cancelReason are guarded by actor.mu; they describe a turn
	// that was cancelled while it was still queued.
	cancelled    bool
	cancelReason string

	mu       sync.Mutex
	err      error
	finished bool
}

// Events returns the turn's event stream. It is lossless and closes when the
// turn ends; Err is valid once Done is closed.
func (h *TurnHandle) Events() <-chan Event { return h.events }

// Done is closed when the turn has finished, whether it completed, failed or
// was cancelled.
func (h *TurnHandle) Done() <-chan struct{} { return h.done }

// Err reports the turn's terminal error. Call it after Done; it is nil for a
// completed turn.
func (h *TurnHandle) Err() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.err
}

// Cancel cancels this turn, whether it is running or still queued. Cancelling
// a finished turn is a no-op. The handle is the identity: a caller can only
// cancel a turn it holds.
func (h *TurnHandle) Cancel(reason string) error {
	select {
	case <-h.done:
		return nil
	default:
	}

	a := h.actor
	a.mu.Lock()
	if a.current == h {
		cancel := a.turnCancel
		stepCancel := a.stepCancel
		a.turnCancel = nil
		a.stepCancel = nil
		a.mu.Unlock()

		h.stop(cancelError(reason))
		if stepCancel != nil {
			stepCancel()
		}
		if cancel != nil {
			cancel()
		}
		return nil
	}
	if _, queued := a.pending[h]; queued {
		h.cancelled = true
		h.cancelReason = strings.TrimSpace(reason)
		a.mu.Unlock()
		return nil
	}
	a.mu.Unlock()
	return ErrTurnNotFound
}

// emit delivers one event without dropping it. A consumer that stops reading
// blocks the turn; it must cancel or drain.
func (h *TurnHandle) emit(ev Event) {
	select {
	case <-h.stopCh:
		return
	default:
	}
	select {
	case h.raw <- ev:
	case <-h.stopCh:
	case <-h.actor.ctx.Done():
	}
}

// relay forwards the turn's events to the public channel and closes it when
// the turn ends. It is the only closer of both events and done.
func (h *TurnHandle) relay() {
	// Done is closed after the event stream, so a consumer that waits on
	// Done knows it has already seen the last event.
	defer close(h.done)
	defer close(h.events)
	for {
		select {
		case ev, ok := <-h.raw:
			if !ok {
				return
			}
			select {
			case h.events <- ev:
			case <-h.stopCh:
				return
			}
		case <-h.stopCh:
			return
		}
	}
}

// watchContext cancels the turn when the caller's context does. The context is
// turn-scoped: one caller timing out never cancels another caller's turn.
func (h *TurnHandle) watchContext(ctx context.Context) {
	if ctx == nil {
		return
	}
	select {
	case <-ctx.Done():
		_ = h.Cancel("context cancelled")
	case <-h.done:
	}
}

// complete ends the turn normally: the loop has no more events to send, so the
// relay can drain raw and close the public channel.
func (h *TurnHandle) complete(err error) {
	h.mu.Lock()
	if h.finished {
		h.mu.Unlock()
		return
	}
	h.finished = true
	h.err = err
	h.mu.Unlock()
	close(h.raw)
}

// stop ends the turn from outside the loop (a cancellation or a close). It
// closes the public channel immediately; events still in raw are dropped,
// which is the point of cancelling.
func (h *TurnHandle) stop(err error) {
	h.mu.Lock()
	if h.finished {
		h.mu.Unlock()
		return
	}
	h.finished = true
	h.err = err
	h.mu.Unlock()
	close(h.stopCh)
}

// newID returns a random identifier with the given prefix.
func newID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}
