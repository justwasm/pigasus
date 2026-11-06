package actor

import (
	"sync"
	"time"
)

// EventType classifies observable occurrences emitted by the Actor.
type EventType string

const (
	EventTurnStarted      EventType = "turn_started"
	EventStateChanged     EventType = "state_changed"
	EventThought          EventType = "thought"
	EventDelta            EventType = "delta"
	EventToolCallStarted  EventType = "tool_call_started"
	EventToolCallFinished EventType = "tool_call_finished"
	EventApprovalRequired EventType = "approval_required"
	EventScheduled        EventType = "scheduled"
	EventScheduleFired    EventType = "schedule_fired"
	EventSteerReceived    EventType = "steer_received"
	EventSteerApplied     EventType = "steer_applied"
	EventCompacted        EventType = "compacted"
	EventTurnCompleted    EventType = "turn_completed"
	EventCancelled        EventType = "cancelled"
	EventError            EventType = "error"
	EventUsage            EventType = "usage"
)

// Event represents an individual event frame emitted by the Actor.
type Event struct {
	Type      EventType `json:"type"`
	ActorID   string    `json:"actor_id"`
	TurnID    string    `json:"turn_id,omitempty"`
	Timestamp time.Time `json:"timestamp"`

	// Specific event payload fields
	Delta            string          `json:"delta,omitempty"`
	Thought          string          `json:"thought,omitempty"`
	SteerInstruction string          `json:"steer_instruction,omitempty"`
	ToolCall         *ToolCall       `json:"tool_call,omitempty"`
	ToolResult       *ToolResult     `json:"tool_result,omitempty"`
	Usage            *TokenUsage     `json:"usage,omitempty"`
	OldState         LifecycleState  `json:"old_state,omitempty"`
	NewState         LifecycleState  `json:"new_state,omitempty"`
	ScheduleRecord   *ScheduleRecord `json:"schedule,omitempty"`
	Error            string          `json:"error,omitempty"`
	Err              error           `json:"-"`
	TurnSummary      *TurnSummary    `json:"turn_summary,omitempty"`
	Metadata         map[string]any  `json:"metadata,omitempty"`
}

// TurnSummary provides the final outcome of an executed turn.
type TurnSummary struct {
	TurnID       string        `json:"turn_id"`
	Status       string        `json:"status"` // "done", "error", "cancelled"
	Output       string        `json:"output"`
	StepsCount   int           `json:"steps_count"`
	Duration     time.Duration `json:"duration"`
	TotalTokens  int           `json:"total_tokens,omitempty"`
	PromptTokens int           `json:"prompt_tokens,omitempty"`
	OutputTokens int           `json:"output_tokens,omitempty"`
}

// ScheduleRecord describes a registered timer or cron job.
type ScheduleRecord struct {
	ID        string    `json:"id"`
	Prompt    string    `json:"prompt"`
	CronExpr  string    `json:"cron_expr,omitempty"`
	Duration  string    `json:"duration,omitempty"`
	IsDaemon  bool      `json:"is_daemon"`
	CreatedAt time.Time `json:"created_at"`
	NextRun   time.Time `json:"next_run"`
}

// eventBroker manages broadcast fan-out of events to multiple subscribers.
type eventBroker struct {
	mu          sync.RWMutex
	subscribers map[uint64]chan Event
	nextID      uint64
	bufferSize  int
	closed      bool
}

func newEventBroker(bufferSize int) *eventBroker {
	if bufferSize <= 0 {
		bufferSize = 256
	}
	return &eventBroker{
		subscribers: make(map[uint64]chan Event),
		bufferSize:  bufferSize,
	}
}

func (b *eventBroker) Subscribe() (<-chan Event, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		ch := make(chan Event)
		close(ch)
		return ch, func() {}
	}

	id := b.nextID
	b.nextID++
	ch := make(chan Event, b.bufferSize)
	b.subscribers[id] = ch

	unsubscribe := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if existing, ok := b.subscribers[id]; ok {
			delete(b.subscribers, id)
			close(existing)
		}
	}

	return ch, unsubscribe
}

func (b *eventBroker) Publish(ev Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return
	}

	for _, ch := range b.subscribers {
		func() {
			defer func() { _ = recover() }()
			select {
			case ch <- ev:
			default:
				// Non-blocking drop on slow consumer buffer overflow to preserve actor responsiveness.
			}
		}()
	}
}

func (b *eventBroker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}
	b.closed = true

	for id, ch := range b.subscribers {
		delete(b.subscribers, id)
		close(ch)
	}
}
