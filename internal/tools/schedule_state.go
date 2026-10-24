package tools

import (
	"fmt"
	"sync"
)

// ScheduleEntry is one scheduled task of a conversation.
//
// The reference's schedule tool registers a deferred or recurring
// prompt with the session's scheduler and returns immediately with a
// background-task acknowledgement (【一手】 1.4.4, the reference's
// declaration: `CronExpression`, `DurationSeconds`, `ExactTiming`,
// `IsDaemon`, `MaxIterations`, `Prompt`, `TimerCondition`).
//
// The clone has no scheduler backend: the tool writes the log file the
// reference names and returns the reference's text. The entry still
// exists, though — the conversation owns a list of the schedules it
// created, and that list is what the rehydration pass rebuilds.
type ScheduleEntry struct {
	// ID is the task id the reference mints: "<conversation-id>/task-<n>".
	//
	// 【一手】 the reference's stored manage_task calls name their task
	// like "0d6f85b5-d661-4705-8a75-1387ab8a9446/task-34".
	ID string
	// Prompt is the prompt the schedule fires.
	Prompt string
	// DurationSeconds is the one-shot delay. Zero for a cron schedule.
	DurationSeconds int
	// CronExpression is the recurring expression. Empty for a one-shot.
	CronExpression string
	// TimerCondition is the condition that must hold before a timer
	// fires; a matching message cancels the timer early.
	TimerCondition string
	// IsDaemon marks a schedule that stays alive across turns.
	IsDaemon bool
	// MaxIterations stops the schedule after this many runs.
	MaxIterations int
	// LogPath is where the tool wrote the task log.
	LogPath string
}

// ScheduleTable holds the schedules of one conversation.
//
// The table owns the per-conversation task-id counter, so a resumed
// conversation continues the numbering instead of minting ids that
// collide with the ones the conversation already used.
type ScheduleTable struct {
	mu      sync.Mutex
	convID  string
	entries []ScheduleEntry
	seq     int
}

// NewScheduleTable returns an empty table for a conversation.
func NewScheduleTable(conversationID string) *ScheduleTable {
	return &ScheduleTable{convID: conversationID}
}

// ConversationID returns the conversation the table belongs to.
func (t *ScheduleTable) ConversationID() string {
	if t == nil {
		return ""
	}
	return t.convID
}

// Schedules returns the schedules of the conversation, in the order
// they were created.
func (t *ScheduleTable) Schedules() []ScheduleEntry {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]ScheduleEntry, len(t.entries))
	copy(out, t.entries)
	return out
}

// Len returns the number of schedules in the table.
func (t *ScheduleTable) Len() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.entries)
}

// Reset empties the table and restarts the task-id counter.
//
// The rehydration pass replaces the conversation's state, so a second
// pass over the same trajectory must not double the schedules it
// restores.
func (t *ScheduleTable) Reset() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.entries = nil
	t.seq = 0
}

// nextTaskID mints the next task id of the conversation. The counter
// starts at one, so the first schedule of a conversation is task-1.
func (t *ScheduleTable) nextTaskID() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.seq++
	return fmt.Sprintf("task-%d", t.seq)
}

// add records a schedule.
func (t *ScheduleTable) add(s ScheduleEntry) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.entries = append(t.entries, s)
}

// scheduleTables holds the schedule table of every conversation this
// process has seen, keyed by conversation id.
//
// The tables are process-local, like the reference's scheduler state
// before the rehydration pass: a resumed conversation's table is
// rebuilt from the trajectory (see internal/rehydration).
var scheduleTables = struct {
	mu   sync.Mutex
	byID map[string]*ScheduleTable
}{byID: map[string]*ScheduleTable{}}

// ScheduleTableFor returns the schedule table of a conversation,
// creating it on first use.
func ScheduleTableFor(conversationID string) *ScheduleTable {
	scheduleTables.mu.Lock()
	defer scheduleTables.mu.Unlock()
	if t, ok := scheduleTables.byID[conversationID]; ok {
		return t
	}
	t := NewScheduleTable(conversationID)
	scheduleTables.byID[conversationID] = t
	return t
}
