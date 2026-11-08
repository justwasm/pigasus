package actor

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// TokenUsage tallies model token consumption.
type TokenUsage struct {
	PromptTokens   int `json:"prompt_tokens"`
	OutputTokens   int `json:"output_tokens"`
	TotalTokens    int `json:"total_tokens"`
	CachedTokens   int `json:"cached_tokens,omitempty"`
	ThoughtsTokens int `json:"thoughts_tokens,omitempty"`
}

// Add aggregates tokens from another TokenUsage.
func (u *TokenUsage) Add(other *TokenUsage) {
	if other == nil {
		return
	}
	u.PromptTokens += other.PromptTokens
	u.OutputTokens += other.OutputTokens
	u.TotalTokens += other.TotalTokens
	u.CachedTokens += other.CachedTokens
	u.ThoughtsTokens += other.ThoughtsTokens
}

// Step status values. A step is "running" while it is being generated, and
// "interrupted" when a transient transport error cut it short but the turn
// continues with the recovery instruction.
const (
	StepRunning     = "running"
	StepDone        = "done"
	StepError       = "error"
	StepCancelled   = "cancelled"
	StepInterrupted = "interrupted"
)

// Step records one interaction cycle between the actor and the provider
// (model generation + tool execution).
type Step struct {
	Index int `json:"index"`
	// GlobalIndex is the step's index in the whole trajectory. Checkpoints
	// refer to it; it never restarts at a turn boundary.
	GlobalIndex  int          `json:"global_index,omitempty"`
	Status       string       `json:"status,omitempty"`
	Thought      string       `json:"thought,omitempty"`
	Delta        string       `json:"delta,omitempty"`
	ToolCalls    []ToolCall   `json:"tool_calls,omitempty"`
	ToolResults  []ToolResult `json:"tool_results,omitempty"`
	Steer        []string     `json:"steer,omitempty"`
	Usage        *TokenUsage  `json:"usage,omitempty"`
	FinishReason string       `json:"finish_reason,omitempty"`
	StartedAt    time.Time    `json:"started_at,omitempty"`
	CompletedAt  time.Time    `json:"completed_at,omitempty"`
}

// TurnRecord documents an entire processed turn from input to conclusion.
//
// A turn is staged when it starts (Status "running") and finalised when it
// ends; the staged record is what a host can snapshot while the turn runs.
type TurnRecord struct {
	ID string `json:"id"`
	// StartStep is the global index of this turn's first step.
	StartStep   int        `json:"start_step,omitempty"`
	Model       string     `json:"model,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt time.Time  `json:"completed_at"`
	Input       string     `json:"input"`
	Steers      []string   `json:"steers,omitempty"`
	Steps       []Step     `json:"steps"`
	FinalOutput string     `json:"final_output"`
	Status      string     `json:"status"` // "running", "done", "error", "cancelled"
	Error       string     `json:"error,omitempty"`
	Usage       TokenUsage `json:"usage"`
}

// Trajectory manages the ordered history of turns and steps executed by an Actor.
// Checkpoint is one compaction boundary in the trajectory.
//
// It is not a turn step: TurnRecord.Steps still records only model/tool
// cycles, so turn semantics and getTurnDiff are unchanged. The checkpoint
// records which global steps were folded into ResumeText.
type Checkpoint struct {
	Index               int       `json:"index"`
	CreatedAt           time.Time `json:"created_at"`
	TurnID              string    `json:"turn_id,omitempty"`
	IncludedThroughStep int       `json:"included_through_step"`
	KeepRecent          int       `json:"keep_recent,omitempty"`
	KeepInput           bool      `json:"keep_input,omitempty"`
	TurnIndex           int       `json:"turn_index,omitempty"`
	ResumeText          string    `json:"resume_text"`
	Summary             string    `json:"summary,omitempty"`
	UserRequests        []string  `json:"user_requests,omitempty"`
	Model               string    `json:"model,omitempty"`
	Strategy            string    `json:"strategy,omitempty"`
	TokensBefore        int       `json:"tokens_before,omitempty"`
	TokensAfter         int       `json:"tokens_after,omitempty"`
	StepsSummarized     int       `json:"steps_summarized,omitempty"`
	Sync                bool      `json:"sync,omitempty"`
}

// CheckpointStep groups the messages one global step contributed.
type CheckpointStep struct {
	GlobalIndex int
	Messages    []ConversationMessage
}

// CheckpointView is the transcript grouped by global step. The host decides
// where to cut it; the actor only applies the resulting boundary.
type CheckpointView struct {
	Steps   []CheckpointStep
	Pending []ConversationMessage
	// Resume is the latest checkpoint's resume block. It is the context the
	// transcript below replaced; a policy that checkpoints again must send it
	// to the summariser instead of replaying the folded steps.
	Resume string
}

type Trajectory struct {
	mu          sync.RWMutex
	turns       []*TurnRecord
	checkpoints []Checkpoint
	staged      int
	revision    uint64
	resetEpoch  uint64
	now         func() time.Time
}

// NewTrajectory allocates a new empty trajectory.
func NewTrajectory() *Trajectory {
	return newTrajectoryWithClock(time.Now)
}

// newTrajectoryWithClock allocates a trajectory that stamps seeded turns with
// the given clock.
func newTrajectoryWithClock(now func() time.Time) *Trajectory {
	if now == nil {
		now = time.Now
	}
	return &Trajectory{
		turns:  make([]*TurnRecord, 0),
		staged: -1,
		now:    now,
	}
}

// Revision returns a counter that changes whenever the trajectory is mutated.
//
// A host that keeps an actor alive across turns uses it to notice that the
// history was reset behind its back (a /clear, a manual compaction), which a
// message-count comparison cannot see.
func (t *Trajectory) Revision() uint64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.revision
}

// ResetWithHistory clears the existing trajectory turns and re-seeds it with history.
func (t *Trajectory) resetWithHistory(history []ConversationMessage) {
	t.mu.Lock()
	t.turns = nil
	t.checkpoints = nil
	t.staged = -1
	t.revision++
	t.resetEpoch++
	t.mu.Unlock()
	t.SeedHistory(history)
}

// ResetEpoch changes only when the trajectory is reset or re-seeded. A host
// uses it to tell "the actor compacted itself" (Revision changed, ResetEpoch
// did not) from "the host replaced the history" (both changed).
func (t *Trajectory) ResetEpoch() uint64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.resetEpoch
}

// AddTurn appends a completed turn record.
func (t *Trajectory) AddTurn(tr *TurnRecord) {
	if tr == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.staged >= 0 && t.staged < len(t.turns) {
		// Keep the staged turn last: ToConversationMessages identifies it by
		// its position.
		t.turns = append(t.turns, nil)
		copy(t.turns[t.staged+1:], t.turns[t.staged:])
		t.turns[t.staged] = tr
		t.staged++
	} else {
		t.turns = append(t.turns, tr)
	}
	t.reindexLocked()
	t.revision++
}

// SeedHistory populates the trajectory from a list of ConversationMessages.
func (t *Trajectory) SeedHistory(history []ConversationMessage) {
	if len(history) == 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.now == nil {
		t.now = time.Now
	}

	var current *TurnRecord
	for _, m := range history {
		switch m.Role {
		case RoleUser:
			if current != nil {
				t.turns = append(t.turns, current)
			}
			current = &TurnRecord{
				ID:          fmt.Sprintf("hist_%d", len(t.turns)+1),
				CreatedAt:   t.now(),
				CompletedAt: t.now(),
				Input:       m.Content,
				Status:      "done",
			}
		case RoleAssistant:
			if current != nil && current.FinalOutput == "" {
				current.FinalOutput = m.Content
				if len(m.ToolCalls) > 0 {
					step := Step{
						Index:     len(current.Steps),
						Delta:     m.Content,
						Thought:   m.Thought,
						ToolCalls: m.ToolCalls,
					}
					current.Steps = append(current.Steps, step)
				}
				t.turns = append(t.turns, current)
				current = nil
			} else {
				tr := &TurnRecord{
					ID:          fmt.Sprintf("hist_%d", len(t.turns)+1),
					CreatedAt:   t.now(),
					CompletedAt: t.now(),
					Input:       "",
					FinalOutput: m.Content,
					Status:      "done",
				}
				if len(m.ToolCalls) > 0 {
					tr.Steps = append(tr.Steps, Step{
						Index:     0,
						Delta:     m.Content,
						Thought:   m.Thought,
						ToolCalls: m.ToolCalls,
					})
				}
				t.turns = append(t.turns, tr)
			}
		case RoleTool:
			if current != nil && len(current.Steps) > 0 {
				lastIdx := len(current.Steps) - 1
				current.Steps[lastIdx].ToolResults = append(current.Steps[lastIdx].ToolResults, ToolResult{
					ToolCallID: m.ToolCallID,
					Output:     m.Content,
				})
			}
		}
	}
	if current != nil {
		t.turns = append(t.turns, current)
	}
	t.reindexLocked()
	t.revision++
	t.resetEpoch++
}

// reindexLocked assigns every turn a StartStep and every step a GlobalIndex.
// It is called when turns are seeded or imported, never while a turn is live.
func (t *Trajectory) reindexLocked() {
	next := 0
	for _, turn := range t.turns {
		turn.StartStep = next
		for i := range turn.Steps {
			turn.Steps[i].GlobalIndex = next
			next++
		}
	}
}

// beginTurn stages a running turn record. It is visible through Turns and
// LastTurn while it runs; ToConversationMessages excludes it so the live turn
// is not repeated in the model context.
func (t *Trajectory) beginTurn(tr *TurnRecord) {
	if tr == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.turns = append(t.turns, cloneTurnRecord(tr))
	t.staged = len(t.turns) - 1
	t.revision++
}

// updateTurn replaces the staged record with the current state of tr.
func (t *Trajectory) updateTurn(tr *TurnRecord) {
	if tr == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.staged < 0 || t.staged >= len(t.turns) {
		return
	}
	t.turns[t.staged] = cloneTurnRecord(tr)
	t.revision++
}

// completeTurn finalises the staged record and clears the staging slot.
func (t *Trajectory) completeTurn(tr *TurnRecord) {
	if tr == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.staged >= 0 && t.staged < len(t.turns) {
		t.turns[t.staged] = cloneTurnRecord(tr)
	}
	t.staged = -1
	t.revision++
}

// Checkpoints returns copies of the trajectory's checkpoint boundaries.
func (t *Trajectory) Checkpoints() []Checkpoint {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]Checkpoint, len(t.checkpoints))
	for i, cp := range t.checkpoints {
		out[i] = cloneCheckpoint(cp)
	}
	return out
}

// ContextStart returns the global step index of the latest checkpoint.
// ok is false when the trajectory has no checkpoint.
func (t *Trajectory) ContextStart() (boundary int, ok bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if len(t.checkpoints) == 0 {
		return 0, false
	}
	return t.checkpoints[len(t.checkpoints)-1].IncludedThroughStep, true
}

// ContextFor reports the boundary for a turn and whether that turn's input is
// still part of the model context.
func (t *Trajectory) ContextFor(turnID string) (boundary int, ok, keepInput bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if len(t.checkpoints) == 0 {
		return 0, false, true
	}
	cp := t.checkpoints[len(t.checkpoints)-1]
	keep := cp.TurnID != turnID || cp.KeepInput
	return cp.IncludedThroughStep, true, keep
}

// CheckpointView groups the transcript by global step. The host reads it only
// when it is about to checkpoint; the actor never copies the view itself.
func (t *Trajectory) CheckpointView() CheckpointView {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var view CheckpointView
	boundary := -1
	cpTurn := -1
	cpKeepInput := true
	if len(t.checkpoints) > 0 {
		cp := t.checkpoints[len(t.checkpoints)-1]
		boundary = cp.IncludedThroughStep
		cpTurn = cp.TurnIndex
		cpKeepInput = cp.KeepInput
		view.Resume = cp.ResumeText
	}
	limit := len(t.turns)
	staged := t.staged >= 0 && t.staged < limit
	for ti, turn := range t.turns {
		keepTurnInput := boundary < 0 || ti > cpTurn || (ti == cpTurn && cpKeepInput)
		if len(turn.Steps) == 0 {
			if boundary >= 0 && turn.StartStep <= boundary {
				continue
			}
			if staged && ti == t.staged {
				if keepTurnInput {
					view.Pending = append(view.Pending, ConversationMessage{Role: RoleUser, Content: turn.Input})
				}
				for _, st := range turn.Steers {
					view.Pending = append(view.Pending, ConversationMessage{Role: RoleUser, Content: "[Active Guidance]: " + st})
				}
				continue
			}
			msgs := make([]ConversationMessage, 0, 2)
			if keepTurnInput {
				msgs = append(msgs, ConversationMessage{Role: RoleUser, Content: turn.Input})
			}
			if turn.FinalOutput != "" {
				msgs = append(msgs, ConversationMessage{Role: RoleAssistant, Content: turn.FinalOutput})
			}
			if len(msgs) > 0 {
				view.Steps = append(view.Steps, CheckpointStep{GlobalIndex: turn.StartStep, Messages: msgs})
			}
			continue
		}
		for si, step := range turn.Steps {
			if boundary >= 0 && step.GlobalIndex <= boundary {
				continue
			}
			input := ""
			if si == 0 && keepTurnInput {
				input = turn.Input
			}
			msgs := stepConversationMessages(step, input)
			if si == len(turn.Steps)-1 && turn.FinalOutput != "" {
				msgs = append(msgs, ConversationMessage{Role: RoleAssistant, Content: turn.FinalOutput})
			}
			view.Steps = append(view.Steps, CheckpointStep{GlobalIndex: step.GlobalIndex, Messages: msgs})
		}
	}
	return view
}

// recordCheckpoint appends a boundary. It never resets the trajectory: the
// completed turns and steps stay available for the UI and for persistence.
func (t *Trajectory) recordCheckpoint(cp Checkpoint) Checkpoint {
	t.mu.Lock()
	defer t.mu.Unlock()
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = t.now()
	}
	if cp.Index == 0 {
		cp.Index = len(t.checkpoints)
	}
	if t.staged >= 0 && t.staged < len(t.turns) {
		cp.TurnIndex = t.staged
	}
	t.checkpoints = append(t.checkpoints, cloneCheckpoint(cp))
	t.revision++
	return cp
}

func cloneCheckpoint(cp Checkpoint) Checkpoint {
	cp.UserRequests = append([]string(nil), cp.UserRequests...)
	return cp
}

func stepConversationMessages(step Step, input string) []ConversationMessage {
	var msgs []ConversationMessage
	if input != "" {
		msgs = append(msgs, ConversationMessage{Role: RoleUser, Content: input})
	}
	if len(step.Steer) > 0 {
		msgs = append(msgs, ConversationMessage{
			Role:    RoleUser,
			Content: "[Active Guidance]: " + strings.Join(step.Steer, "; "),
		})
	}
	if step.Delta != "" || step.Thought != "" || len(step.ToolCalls) > 0 {
		msgs = append(msgs, ConversationMessage{
			Role:      RoleAssistant,
			Content:   step.Delta,
			Thought:   step.Thought,
			ToolCalls: step.ToolCalls,
		})
		for _, res := range step.ToolResults {
			content := res.Output
			if res.Error != "" {
				content = fmt.Sprintf("Error: %s", res.Error)
			}
			msgs = append(msgs, ConversationMessage{
				Role:       RoleTool,
				Content:    content,
				ToolCallID: res.ToolCallID,
			})
		}
	}
	return msgs
}

// LastTurn returns a copy of the most recent turn record, or nil if none.
func (t *Trajectory) LastTurn() *TurnRecord {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if len(t.turns) == 0 {
		return nil
	}
	return cloneTurnRecord(t.turns[len(t.turns)-1])
}

// Turns returns copies of all turn records.
func (t *Trajectory) Turns() []*TurnRecord {
	t.mu.RLock()
	defer t.mu.RUnlock()
	copied := make([]*TurnRecord, len(t.turns))
	for i, tr := range t.turns {
		copied[i] = cloneTurnRecord(tr)
	}
	return copied
}

// cloneTurnRecord deep-copies a record so a reader never shares slices with
// the live turn.
func cloneTurnRecord(tr *TurnRecord) *TurnRecord {
	if tr == nil {
		return nil
	}
	cp := *tr
	cp.Steers = append([]string(nil), tr.Steers...)
	cp.Steps = make([]Step, len(tr.Steps))
	for i, st := range tr.Steps {
		cp.Steps[i] = st
		cp.Steps[i].Steer = append([]string(nil), st.Steer...)
		cp.Steps[i].ToolCalls = append([]ToolCall(nil), st.ToolCalls...)
		cp.Steps[i].ToolResults = append([]ToolResult(nil), st.ToolResults...)
		if st.Usage != nil {
			usage := *st.Usage
			cp.Steps[i].Usage = &usage
		}
	}
	return &cp
}

// TotalTurns returns the total number of turns recorded.
func (t *Trajectory) TotalTurns() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.turns)
}

// TotalSteps returns the count of steps across all turns.
func (t *Trajectory) TotalSteps() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	count := 0
	for _, tr := range t.turns {
		count += len(tr.Steps)
	}
	return count
}

// ToConversationMessages converts the stored turns into provider messages.
//
// When the trajectory carries a checkpoint, the context starts with its
// resume block and then includes only the steps after the boundary. The
// staged turn is excluded: the turn loop appends its live messages itself.
func (t *Trajectory) ToConversationMessages() []ConversationMessage {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var msgs []ConversationMessage
	boundary := -1
	cpTurn := -1
	cpKeepInput := true
	if len(t.checkpoints) > 0 {
		cp := t.checkpoints[len(t.checkpoints)-1]
		boundary = cp.IncludedThroughStep
		cpTurn = cp.TurnIndex
		cpKeepInput = cp.KeepInput
		if cp.ResumeText != "" {
			msgs = append(msgs, ConversationMessage{Role: RoleUser, Content: cp.ResumeText})
		}
	}

	limit := len(t.turns)
	if t.staged == limit-1 {
		limit--
	}
	for ti, turn := range t.turns[:limit] {
		keepTurnInput := boundary < 0 || ti > cpTurn || (ti == cpTurn && cpKeepInput)
		if len(turn.Steps) == 0 {
			if turn.StartStep > boundary {
				if keepTurnInput {
					msgs = append(msgs, ConversationMessage{Role: RoleUser, Content: turn.Input})
				}
				if turn.FinalOutput != "" {
					msgs = append(msgs, ConversationMessage{Role: RoleAssistant, Content: turn.FinalOutput})
				}
			}
			continue
		}
		for si, step := range turn.Steps {
			if boundary >= 0 && step.GlobalIndex <= boundary {
				continue
			}
			input := ""
			if si == 0 && keepTurnInput && (boundary < 0 || turn.StartStep > boundary) {
				input = turn.Input
			}
			msgs = append(msgs, stepConversationMessages(step, input)...)
		}
		if len(turn.Steps) > 0 && turn.FinalOutput != "" {
			last := turn.Steps[len(turn.Steps)-1]
			if boundary < 0 || last.GlobalIndex > boundary {
				msgs = append(msgs, ConversationMessage{Role: RoleAssistant, Content: turn.FinalOutput})
			}
		}
	}

	return msgs
}
