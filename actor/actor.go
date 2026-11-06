package actor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Actor represents an autonomous agent entity operating under the Actor model.
type Actor struct {
	id           string
	name         string
	systemPrompt string
	model        string
	provider     Provider
	tools        *ToolRegistry
	mailbox      chan Message
	events       *eventBroker
	fsm          *actorStateMachine
	trajectory   *Trajectory
	scheduler    *scheduler
	config       Config

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	mu           sync.RWMutex
	turnCancel   context.CancelFunc
	stepCancel   context.CancelFunc
	approvalCh   chan *ApprovalDecision
	pendingSteer []string
}

// Compile-time interface satisfaction guarantees.
var (
	_ TurnRunner     = (*Actor)(nil)
	_ Steerer        = (*Actor)(nil)
	_ TurnController = (*Actor)(nil)
	_ SteerTracker   = (*Actor)(nil)
	_ EventStreamer  = (*Actor)(nil)
	_ ActorHandle    = (*Actor)(nil)
)

// Spawn initializes and launches a new Actor runtime.
func Spawn(cfg Config) (*Actor, error) {
	if cfg.Provider == nil {
		return nil, fmt.Errorf("actor: provider must not be nil")
	}
	cfg.EnsureDefaults()

	id := cfg.ID
	if id == "" {
		id = newID("act")
	}

	ctx, cancel := context.WithCancel(context.Background())

	events := newEventBroker(cfg.EventBufferSize)
	tools := NewToolRegistry()
	tools.Register(cfg.Tools...)

	trajectory := NewTrajectory()
	if len(cfg.InitialTurns) > 0 {
		for _, tr := range cfg.InitialTurns {
			trajectory.AddTurn(tr)
		}
	}
	if len(cfg.InitialHistory) > 0 {
		trajectory.SeedHistory(cfg.InitialHistory)
	}

	a := &Actor{
		id:           id,
		name:         cfg.Name,
		systemPrompt: cfg.SystemPrompt,
		model:        cfg.Model,
		provider:     cfg.Provider,
		tools:        tools,
		mailbox:      make(chan Message, cfg.MailboxSize),
		events:       events,
		trajectory:   trajectory,
		config:       cfg,
		ctx:          ctx,
		cancel:       cancel,
		done:         make(chan struct{}),
		approvalCh:   make(chan *ApprovalDecision, 16),
		pendingSteer: make([]string, 0),
	}

	a.fsm = newActorStateMachine(func(from, to LifecycleState, turnID string) {
		a.events.Publish(Event{
			Type:      EventStateChanged,
			ActorID:   a.id,
			TurnID:    turnID,
			Timestamp: time.Now(),
			OldState:  from,
			NewState:  to,
		})
	})

	a.scheduler = newScheduler(
		func(wakeup *ScheduledWakeup) {
			_ = a.Send(wakeup)
		},
		func(rec ScheduleRecord) {
			a.events.Publish(Event{
				Type:           EventScheduleFired,
				ActorID:        a.id,
				Timestamp:      time.Now(),
				ScheduleRecord: &rec,
			})
		},
		cfg.TimeNow,
	)

	go a.loop()

	return a, nil
}

// ID returns the unique actor identifier.
func (a *Actor) ID() string {
	return a.id
}

// Name returns the optional human-readable name of the actor.
func (a *Actor) Name() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.name
}

// SystemPrompt returns the current base instructions of the agent.
func (a *Actor) SystemPrompt() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.systemPrompt
}

// SetSystemPrompt updates the agent's base instructions.
func (a *Actor) SetSystemPrompt(prompt string) {
	a.mu.Lock()
	a.systemPrompt = prompt
	a.mu.Unlock()
}

// Model returns the currently configured model identifier.
func (a *Actor) Model() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.model
}

// SetModel updates the model used for subsequent turns.
func (a *Actor) SetModel(model string) {
	a.mu.Lock()
	a.model = model
	a.mu.Unlock()
}

// Provider returns the current LLM provider.
func (a *Actor) Provider() Provider {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.provider
}

// SetProvider updates the LLM provider used for subsequent turns.
func (a *Actor) SetProvider(p Provider) {
	if p == nil {
		return
	}
	a.mu.Lock()
	a.provider = p
	a.mu.Unlock()
}

// Tools returns the actor's ToolRegistry.
func (a *Actor) Tools() *ToolRegistry {
	return a.tools
}

// SetTools replaces the actor's registered tools.
func (a *Actor) SetTools(tools ...Tool) {
	a.tools.Set(tools...)
}

// Send non-blockingly places a message into the actor's mailbox.
func (a *Actor) Send(msg Message) error {
	if msg == nil {
		return fmt.Errorf("message cannot be nil")
	}

	a.mu.RLock()
	if a.fsm.State() == StateClosed {
		a.mu.RUnlock()
		return ErrActorClosed
	}
	a.mu.RUnlock()

	select {
	case <-a.ctx.Done():
		return ErrActorClosed
	case a.mailbox <- msg:
		return nil
	default:
		return ErrMailboxFull
	}
}

// SendSync delivers a message into the mailbox, waiting if the mailbox is full.
func (a *Actor) SendSync(ctx context.Context, msg Message) error {
	if msg == nil {
		return fmt.Errorf("message cannot be nil")
	}

	a.mu.RLock()
	if a.fsm.State() == StateClosed {
		a.mu.RUnlock()
		return ErrActorClosed
	}
	a.mu.RUnlock()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-a.ctx.Done():
		return ErrActorClosed
	case a.mailbox <- msg:
		return nil
	}
}

// Ask sends a message and returns an event stream dedicated to that turn.
func (a *Actor) Ask(ctx context.Context, msg Message) (<-chan Event, error) {
	if msg == nil {
		return nil, fmt.Errorf("message cannot be nil")
	}

	msgID := msg.ID()
	ch, unsub := a.Subscribe()

	out := make(chan Event, a.config.EventBufferSize)
	go func() {
		defer unsub()
		defer close(out)

		var activeTurnID string

		for {
			select {
			case <-ctx.Done():
				_ = a.CancelTurn("context cancelled")
				timeout := time.NewTimer(500 * time.Millisecond)
				for {
					select {
					case ev, ok := <-ch:
						if !ok || ev.Type == EventTurnCompleted || ev.Type == EventCancelled {
							timeout.Stop()
							return
						}
					case <-timeout.C:
						timeout.Stop()
						return
					}
				}
			case ev, ok := <-ch:
				if !ok {
					return
				}

				// If we have a message ID and haven't latched onto our TurnID yet,
				// check if this is our turn starting.
				if msgID != "" && activeTurnID == "" {
					if ev.Type == EventTurnStarted && ev.Metadata != nil && ev.Metadata["message_id"] == msgID {
						activeTurnID = ev.TurnID
					} else {
						// Not for our message yet, skip
						continue
					}
				} else if activeTurnID != "" {
					// Once latched, ignore events belonging to other turns
					if ev.TurnID != "" && ev.TurnID != activeTurnID {
						continue
					}
				}

				select {
				case out <- ev:
				case <-ctx.Done():
					_ = a.CancelTurn("context cancelled")
					timeout := time.NewTimer(500 * time.Millisecond)
					for {
						select {
						case ev, ok := <-ch:
							if !ok || ev.Type == EventTurnCompleted || ev.Type == EventCancelled {
								timeout.Stop()
								return
							}
						case <-timeout.C:
							timeout.Stop()
							return
						}
					}
				}

				if ev.Type == EventTurnCompleted || ev.Type == EventCancelled {
					return
				}
			}
		}
	}()

	if err := a.SendSync(ctx, msg); err != nil {
		unsub()
		return nil, err
	}

	return out, nil
}

// Prompt sends a text prompt as a user message and returns an event stream dedicated to that turn.
func (a *Actor) Prompt(ctx context.Context, text string) (<-chan Event, error) {
	return a.Ask(ctx, NewUserMessage(text))
}

// Run executes a text prompt synchronously to completion and returns the final assistant text output.
func (a *Actor) Run(ctx context.Context, text string) (string, error) {
	events, err := a.Prompt(ctx, text)
	if err != nil {
		return "", err
	}

	var output strings.Builder
	var lastErr error

	for ev := range events {
		switch ev.Type {
		case EventDelta:
			output.WriteString(ev.Delta)
		case EventTurnCompleted:
			if ev.TurnSummary != nil && ev.TurnSummary.Output != "" {
				return ev.TurnSummary.Output, nil
			}
		case EventError:
			if ev.Error != "" {
				lastErr = errors.New(ev.Error)
			}
		case EventCancelled:
			return output.String(), ErrTurnCancelled
		}
	}

	if lastErr != nil {
		return output.String(), lastErr
	}
	return output.String(), nil
}

// Subscribe returns a channel of all emitted events and an unsubscribe callback.
func (a *Actor) Subscribe() (<-chan Event, func()) {
	return a.events.Subscribe()
}

// Events returns a simple event subscription channel that stays open until actor shutdown.
func (a *Actor) Events() <-chan Event {
	ch, _ := a.Subscribe()
	return ch
}

// Schedule schedules a one-shot prompt after delay.
func (a *Actor) Schedule(delay time.Duration, prompt string) (string, error) {
	rec, err := a.scheduler.ScheduleDelay(delay, prompt)
	if err != nil {
		return "", err
	}

	a.events.Publish(Event{
		Type:           EventScheduled,
		ActorID:        a.id,
		Timestamp:      time.Now(),
		ScheduleRecord: &rec,
	})

	return rec.ID, nil
}

// Cron schedules a recurring or single cron prompt.
func (a *Actor) Cron(cronExpr, prompt string, isDaemon bool) (string, error) {
	rec, err := a.scheduler.ScheduleCron(cronExpr, prompt, isDaemon)
	if err != nil {
		return "", err
	}

	a.events.Publish(Event{
		Type:           EventScheduled,
		ActorID:        a.id,
		Timestamp:      time.Now(),
		ScheduleRecord: &rec,
	})

	return rec.ID, nil
}

// CancelSchedule stops an active timer or cron job.
func (a *Actor) CancelSchedule(scheduleID string) error {
	return a.scheduler.Cancel(scheduleID)
}

// ActiveSchedules returns currently active schedules.
func (a *Actor) ActiveSchedules() []ScheduleRecord {
	return a.scheduler.ActiveSchedules()
}

// Approve delivers an approval or denial for a tool execution waiting for authorization.
func (a *Actor) Approve(toolCallID string, approved bool, feedback string) error {
	a.mu.RLock()
	state := a.fsm.State()
	waitingID := a.fsm.WaitingToolCallID()
	a.mu.RUnlock()

	if state != StateWaitingApproval || waitingID != toolCallID {
		return fmt.Errorf("actor is not currently waiting for approval for tool_call_id %q", toolCallID)
	}

	decision := NewApprovalDecision(toolCallID, approved, feedback)
	select {
	case a.approvalCh <- decision:
		return nil
	default:
		return fmt.Errorf("approval buffer is busy")
	}
}

// CancelTurn cancels the active turn if running.
func (a *Actor) CancelTurn(reason string) error {
	a.mu.Lock()
	cancel := a.turnCancel
	a.turnCancel = nil
	stepCancel := a.stepCancel
	a.stepCancel = nil
	a.mu.Unlock()

	if stepCancel != nil {
		stepCancel()
	}
	if cancel != nil {
		cancel()
	}
	return nil
}

// Steer injects dynamic guidance into an in-flight turn (or queues guidance for the next turn).
// It operates concurrently and out-of-band without being blocked by mailbox processing.
// It appends the guidance to pendingSteer without cancelling the active step or breaking the loop.
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
	currentTurnID := a.fsm.CurrentTurnID()
	a.mu.Unlock()

	a.events.Publish(Event{
		Type:             EventSteerReceived,
		ActorID:          a.id,
		TurnID:           currentTurnID,
		Timestamp:        time.Now(),
		SteerInstruction: instruction,
	})

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
		CurrentTurnID:        a.fsm.CurrentTurnID(),
		WaitingToolCallID:    a.fsm.WaitingToolCallID(),
		PendingMessagesCount: len(a.mailbox),
		ActiveSchedulesCount: len(a.scheduler.ActiveSchedules()),
		TotalTurns:           a.trajectory.TotalTurns(),
		TotalSteps:           a.trajectory.TotalSteps(),
		UpdatedAt:            time.Now(),
	}
}

// Trajectory returns the actor's execution history.
func (a *Actor) Trajectory() *Trajectory {
	return a.trajectory
}

// ResetHistory resets the actor's trajectory with a fresh set of conversation messages.
func (a *Actor) ResetHistory(history []ConversationMessage) {
	a.trajectory.ResetWithHistory(history)
}

// Close shuts down the actor, its timers, and cancels any active turn.
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

	_ = a.fsm.Transition(StateClosed, "", "")
	a.scheduler.Close()
	a.events.Close()

	<-a.done
	return nil
}
