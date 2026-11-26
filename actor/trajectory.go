package actor

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
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

// Reconcile aligns the token usage against actual provider reported values.
func (u *TokenUsage) Reconcile(reportedPrompt, reportedOutput, reportedCached int) {
	if reportedPrompt > 0 {
		u.PromptTokens = reportedPrompt
	}
	if reportedOutput > 0 {
		u.OutputTokens = reportedOutput
	}
	if reportedCached > 0 {
		u.CachedTokens = reportedCached
	}
	u.TotalTokens = u.PromptTokens + u.OutputTokens
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
	Images       []ImagePart  `json:"images,omitempty"`
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
	InputImages []ImagePart `json:"input_images,omitempty"`
	Steers      []string   `json:"steers,omitempty"`
	Steps       []Step     `json:"steps"`
	FinalOutput string     `json:"final_output"`
	Status      string     `json:"status"` // "running", "done", "error", "cancelled"
	Error       string     `json:"error,omitempty"`
	Usage       TokenUsage `json:"usage"`

	// Cold Storage tracking
	ColdStored  bool   `json:"cold_stored,omitempty"`
	ColdChunkID string `json:"cold_chunk_id,omitempty"`
	StepCount   int    `json:"step_count,omitempty"`
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
	mu            sync.RWMutex
	turns         []*TurnRecord
	checkpoints   []Checkpoint
	staged        int
	revision      uint64
	resetEpoch    uint64
	now           func() time.Time
	coldDir       string
	coldChunks    []ColdChunk
	slidingWindow int
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
	tr = cloneTurnRecord(tr)
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
	newTurn := func() *TurnRecord {
		return &TurnRecord{
			ID:          fmt.Sprintf("hist_%d", len(t.turns)+1),
			CreatedAt:   t.now(),
			CompletedAt: t.now(),
			Status:      "done",
		}
	}
	for _, m := range history {
		switch m.Role {
		case RoleUser:
			if current != nil {
				t.turns = append(t.turns, current)
			}
			current = newTurn()
			current.Input = m.Content
			current.InputImages = cloneImageParts(m.Images)
		case RoleAssistant:
			if current == nil {
				current = newTurn()
			}
			current.FinalOutput = m.Content
			current.Steps = append(current.Steps, Step{
				Index:     len(current.Steps),
				Delta:     m.Content,
				Thought:   m.Thought,
				Images:    cloneImageParts(m.Images),
				ToolCalls: append([]ToolCall(nil), m.ToolCalls...),
			})
			// Keep this turn open: tool results and subsequent assistant
			// steps belong to it until the next user message.
		case RoleTool:
			if current != nil && len(current.Steps) > 0 {
				lastIdx := len(current.Steps) - 1
				current.Steps[lastIdx].ToolResults = append(current.Steps[lastIdx].ToolResults, ToolResult{
					ToolCallID: m.ToolCallID,
					ToolName:   m.ToolName,
					Output:     m.Content,
					Images:     cloneImageParts(m.Images),
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
		if turn.ColdStored && len(turn.Steps) == 0 && t.coldDir != "" {
			if hydrated, err := t.loadColdTurnLocked(t.coldDir, ti); err == nil && hydrated != nil {
				turn = hydrated
			}
		}
		keepTurnInput := boundary < 0 || ti > cpTurn || (ti == cpTurn && cpKeepInput)
		if len(turn.Steps) == 0 {
			if boundary >= 0 && turn.StartStep <= boundary {
				continue
			}
			if staged && ti == t.staged {
				if keepTurnInput {
					view.Pending = append(view.Pending, ConversationMessage{Role: RoleUser, Content: turn.Input, Images: cloneImageParts(turn.InputImages)})
				}
				for _, st := range turn.Steers {
					view.Pending = append(view.Pending, ConversationMessage{Role: RoleUser, Content: "[Active Guidance]: " + st})
				}
				continue
			}
			msgs := make([]ConversationMessage, 0, 2)
			if keepTurnInput {
				msgs = append(msgs, ConversationMessage{Role: RoleUser, Content: turn.Input, Images: cloneImageParts(turn.InputImages)})
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
			var inputImages []ImagePart
			if si == 0 && keepTurnInput {
				input = turn.Input
				inputImages = turn.InputImages
			}
			msgs := stepConversationMessages(step, input, inputImages)
			if si == len(turn.Steps)-1 && turn.FinalOutput != "" && step.Delta == "" {
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

func stepConversationMessages(step Step, input string, inputImages []ImagePart) []ConversationMessage {
	var msgs []ConversationMessage
	if input != "" || len(inputImages) > 0 {
		msgs = append(msgs, ConversationMessage{Role: RoleUser, Content: input, Images: cloneImageParts(inputImages)})
	}
	if len(step.Steer) > 0 {
		msgs = append(msgs, ConversationMessage{
			Role:    RoleUser,
			Content: "[Active Guidance]: " + strings.Join(step.Steer, "; "),
		})
	}
	if step.Delta != "" || step.Thought != "" || len(step.Images) > 0 || len(step.ToolCalls) > 0 {
		msgs = append(msgs, ConversationMessage{
			Role:      RoleAssistant,
			Content:   step.Delta,
			Thought:   step.Thought,
			Images:    cloneImageParts(step.Images),
			ToolCalls: append([]ToolCall(nil), step.ToolCalls...),
		})
	}
	for _, res := range step.ToolResults {
		msgs = append(msgs, toolResultMessage(step, res))
	}
	return msgs
}

func toolResultMessage(step Step, res ToolResult) ConversationMessage {
	content := res.Output
	if res.Error != "" {
		content = fmt.Sprintf("Error: %s", res.Error)
	}
	name := res.ToolName
	if name == "" {
		for _, call := range step.ToolCalls {
			if call.ID == res.ToolCallID {
				name = call.Name
				break
			}
		}
	}
	return ConversationMessage{
		Role:       RoleTool,
		Content:    content,
		Images:     cloneImageParts(res.Images),
		ToolCallID: res.ToolCallID,
		ToolName:   name,
	}
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
	cp.InputImages = cloneImageParts(tr.InputImages)
	cp.Steers = append([]string(nil), tr.Steers...)
	cp.Steps = make([]Step, len(tr.Steps))
	for i, st := range tr.Steps {
		cp.Steps[i] = cloneStep(st)
	}
	return &cp
}

func cloneStep(st Step) Step {
	cp := st
	cp.Images = cloneImageParts(st.Images)
	cp.Steer = append([]string(nil), st.Steer...)
	cp.ToolCalls = append([]ToolCall(nil), st.ToolCalls...)
	cp.ToolResults = append([]ToolResult(nil), st.ToolResults...)
	for i := range cp.ToolResults {
		cp.ToolResults[i].Images = cloneImageParts(st.ToolResults[i].Images)
	}
	if st.Usage != nil {
		usage := *st.Usage
		cp.Usage = &usage
	}
	return cp
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
		if tr.ColdStored && len(tr.Steps) == 0 {
			count += tr.StepCount
		} else {
			count += len(tr.Steps)
		}
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
		if turn.ColdStored && len(turn.Steps) == 0 && (boundary < 0 || turn.StartStep > boundary) && t.coldDir != "" {
			if hydrated, err := t.loadColdTurnLocked(t.coldDir, ti); err == nil && hydrated != nil {
				turn = hydrated
			}
		}
		if len(turn.Steps) == 0 {
			if turn.StartStep > boundary {
				if keepTurnInput {
					msgs = append(msgs, ConversationMessage{Role: RoleUser, Content: turn.Input, Images: cloneImageParts(turn.InputImages)})
				}
				if turn.FinalOutput != "" {
					msgs = append(msgs, ConversationMessage{Role: RoleAssistant, Content: turn.FinalOutput})
				}
			}
			continue
		}
		hasStepAssistant := false
		for si, step := range turn.Steps {
			if boundary >= 0 && step.GlobalIndex <= boundary {
				continue
			}
			input := ""
			var inputImages []ImagePart
			if si == 0 && keepTurnInput && (boundary < 0 || turn.StartStep > boundary) {
				input = turn.Input
				inputImages = turn.InputImages
			}
			stepMsgs := stepConversationMessages(step, input, inputImages)
			for _, sm := range stepMsgs {
				if sm.Role == RoleAssistant && (sm.Content != "" || sm.Thought != "" || len(sm.Images) > 0 || len(sm.ToolCalls) > 0) {
					hasStepAssistant = true
				}
			}
			msgs = append(msgs, stepMsgs...)
		}
		if len(turn.Steps) > 0 && turn.FinalOutput != "" && !hasStepAssistant {
			last := turn.Steps[len(turn.Steps)-1]
			if boundary < 0 || last.GlobalIndex > boundary {
				msgs = append(msgs, ConversationMessage{Role: RoleAssistant, Content: turn.FinalOutput})
			}
		}
	}

	return msgs
}

// TierColdHistory writes bulky tool outputs of folded steps (<= IncludedThroughStep)
// to persistent JSON files in coldDir and clears their in-memory text and images,
// preventing unconstrained heap growth in ultra-long sessions (100+ turns).
func (t *Trajectory) TierColdHistory(coldDir string) (prunedSteps int, freedBytes int64, err error) {
	if coldDir == "" {
		return 0, 0, errors.New("coldDir is required")
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(t.checkpoints) == 0 {
		return 0, 0, nil
	}
	boundary := t.checkpoints[len(t.checkpoints)-1].IncludedThroughStep
	if boundary < 0 {
		return 0, 0, nil
	}

	if err := os.MkdirAll(coldDir, 0o755); err != nil {
		return 0, 0, err
	}

	for _, turn := range t.turns {
		for si := range turn.Steps {
			st := &turn.Steps[si]
			if st.GlobalIndex > boundary {
				continue
			}
			needsPruning := false
			var stepBytes int64
			for _, tr := range st.ToolResults {
				if !tr.ColdStored && (len(tr.Output) > 0 || len(tr.Error) > 0 || len(tr.Images) > 0) {
					needsPruning = true
					stepBytes += int64(len(tr.Output) + len(tr.Error))
					for _, image := range tr.Images {
						stepBytes += int64(len(image.Data))
					}
				}
			}
			if !needsPruning {
				continue
			}

			// Write to cold file
			coldPath := filepath.Join(coldDir, fmt.Sprintf("step_%d.json", st.GlobalIndex))
			data, err := json.Marshal(st.ToolResults)
			if err != nil {
				return prunedSteps, freedBytes, err
			}
			if err := os.WriteFile(coldPath, data, 0o600); err != nil {
				return prunedSteps, freedBytes, err
			}

			// Clear in-memory buffers
			for ri := range st.ToolResults {
				st.ToolResults[ri].ColdStored = true
				st.ToolResults[ri].Output = ""
				st.ToolResults[ri].Error = ""
				st.ToolResults[ri].Images = nil
			}
			prunedSteps++
			freedBytes += stepBytes
		}
	}

	if prunedSteps > 0 {
		t.revision++
	}
	return prunedSteps, freedBytes, nil
}

// LoadColdStepResults retrieves historical tool results from cold storage on demand.
func LoadColdStepResults(coldDir string, globalIndex int) ([]ToolResult, error) {
	coldPath := filepath.Join(coldDir, fmt.Sprintf("step_%d.json", globalIndex))
	data, err := os.ReadFile(coldPath)
	if err != nil {
		return nil, err
	}
	var results []ToolResult
	if err := json.Unmarshal(data, &results); err != nil {
		return nil, err
	}
	return results, nil
}

// CumulativeUsage aggregates token usage across all turns.
func (t *Trajectory) CumulativeUsage() TokenUsage {
	t.mu.RLock()
	defer t.mu.RUnlock()
	var total TokenUsage
	for _, tr := range t.turns {
		total.Add(&tr.Usage)
	}
	return total
}

// ReconcileLatestTurnUsage reconciles the latest turn's token usage with upstream provider metrics.
func (t *Trajectory) ReconcileLatestTurnUsage(reportedPrompt, reportedOutput, reportedCached int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.turns) == 0 {
		return
	}
	last := t.turns[len(t.turns)-1]
	last.Usage.Reconcile(reportedPrompt, reportedOutput, reportedCached)
	t.revision++
}

// ColdChunk describes an SST-like immutable disk chunk storing a sequence of historical turns.
type ColdChunk struct {
	ID        string    `json:"id"`
	StartTurn int       `json:"start_turn"`
	EndTurn   int       `json:"end_turn"`
	StartStep int       `json:"start_step"`
	EndStep   int       `json:"end_step"`
	TurnCount int       `json:"turn_count"`
	StepCount int       `json:"step_count"`
	SizeBytes int64     `json:"size_bytes"`
	Checksum  uint32    `json:"checksum"` // CRC32 IEEE
	CreatedAt time.Time `json:"created_at"`
	FilePath  string    `json:"file_path"`
}

// SSTChunkFile models the on-disk SST chunk container with header, index and payload.
type SSTChunkFile struct {
	Magic string        `json:"magic"` // "PIGASST1"
	Chunk ColdChunk     `json:"chunk"`
	Index []SSTIndexRow `json:"index"`
	Turns []*TurnRecord `json:"turns"`
}

// SSTIndexRow records point lookup metadata for a turn within an SST chunk.
type SSTIndexRow struct {
	TurnIndex int    `json:"turn_index"`
	TurnID    string `json:"turn_id"`
	StartStep int    `json:"start_step"`
	StepCount int    `json:"step_count"`
}

// SetColdDir configures the directory used for persistent cold storage.
func (t *Trajectory) SetColdDir(dir string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.coldDir = dir
}

// ColdDir returns the configured cold storage directory.
func (t *Trajectory) ColdDir() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.coldDir
}

// SetSlidingWindow configures the number of recent active turns to retain in RAM.
func (t *Trajectory) SetSlidingWindow(w int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.slidingWindow = w
}

// SlidingWindow returns the configured sliding window size.
func (t *Trajectory) SlidingWindow() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.slidingWindow
}

// ColdChunks returns copies of all active cold chunk descriptors.
func (t *Trajectory) ColdChunks() []ColdChunk {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]ColdChunk, len(t.coldChunks))
	copy(out, t.coldChunks)
	return out
}

// TierColdTurns writes completed historical turns before the compaction boundary
// (or outside the sliding window) into an SST-like immutable disk chunk, freeing
// memory while keeping context summaries and active sliding window turns hot.
func (t *Trajectory) TierColdTurns(coldDir string, slidingWindowTurns int) (*ColdChunk, error) {
	if coldDir == "" {
		coldDir = t.coldDir
	}
	if coldDir == "" {
		return nil, errors.New("coldDir is required")
	}
	if slidingWindowTurns <= 0 {
		slidingWindowTurns = t.slidingWindow
	}
	if slidingWindowTurns <= 0 {
		slidingWindowTurns = 20
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	boundary := -1
	if len(t.checkpoints) > 0 {
		boundary = t.checkpoints[len(t.checkpoints)-1].IncludedThroughStep
	}

	limit := len(t.turns)
	if t.staged >= 0 && t.staged < limit {
		limit = t.staged
	}

	slidingCutoff := limit - slidingWindowTurns
	if slidingCutoff < 0 {
		slidingCutoff = 0
	}

	var targetIndices []int
	for i := 0; i < limit; i++ {
		turn := t.turns[i]
		if turn.ColdStored {
			continue
		}
		isEligible := false
		if boundary >= 0 {
			// When compaction boundary is present, tier historical turns folded into checkpoint
			if turn.StartStep <= boundary {
				isEligible = true
				if slidingWindowTurns > 0 && i >= slidingCutoff {
					// Retain sliding window turns hot in RAM
					isEligible = false
				}
			}
		} else {
			// No checkpoint yet: tier completed turns outside sliding window
			if i < slidingCutoff {
				isEligible = true
			}
		}

		if isEligible {
			targetIndices = append(targetIndices, i)
		}
	}

	if len(targetIndices) == 0 {
		return nil, nil
	}

	if err := os.MkdirAll(coldDir, 0o755); err != nil {
		return nil, err
	}

	startIdx := targetIndices[0]
	endIdx := targetIndices[len(targetIndices)-1]

	chunkID := fmt.Sprintf("chunk_%06d_%06d_%d", startIdx, endIdx, time.Now().UnixNano())
	fileName := chunkID + ".sst"
	finalPath := filepath.Join(coldDir, fileName)
	tmpPath := filepath.Join(coldDir, chunkID+".tmp")

	chunk := ColdChunk{
		ID:        chunkID,
		StartTurn: startIdx,
		EndTurn:   endIdx,
		StartStep: t.turns[startIdx].StartStep,
		TurnCount: len(targetIndices),
		CreatedAt: t.now(),
		FilePath:  finalPath,
	}

	lastTurn := t.turns[endIdx]
	lastStep := lastTurn.StartStep
	if len(lastTurn.Steps) > 0 {
		lastStep = lastTurn.Steps[len(lastTurn.Steps)-1].GlobalIndex
	}
	chunk.EndStep = lastStep

	turnsToSerialize := make([]*TurnRecord, 0, len(targetIndices))
	indexRows := make([]SSTIndexRow, 0, len(targetIndices))
	totalSteps := 0

	for _, idx := range targetIndices {
		tr := t.turns[idx]
		stepCount := len(tr.Steps)
		totalSteps += stepCount
		turnsToSerialize = append(turnsToSerialize, cloneTurnRecord(tr))
		indexRows = append(indexRows, SSTIndexRow{
			TurnIndex: idx,
			TurnID:    tr.ID,
			StartStep: tr.StartStep,
			StepCount: stepCount,
		})
	}
	chunk.StepCount = totalSteps

	sstFile := SSTChunkFile{
		Magic: "PIGASST1",
		Chunk: chunk,
		Index: indexRows,
		Turns: turnsToSerialize,
	}

	rawBytes, err := json.Marshal(sstFile)
	if err != nil {
		return nil, fmt.Errorf("marshal sst chunk: %w", err)
	}

	checksum := crc32.ChecksumIEEE(rawBytes)
	chunk.Checksum = checksum
	chunk.SizeBytes = int64(len(rawBytes))
	sstFile.Chunk = chunk

	finalBytes, err := json.Marshal(sstFile)
	if err != nil {
		return nil, fmt.Errorf("marshal final sst chunk: %w", err)
	}

	// Atomic write via .tmp and rename to prevent mid-stream corruption
	if err := os.WriteFile(tmpPath, finalBytes, 0o600); err != nil {
		return nil, fmt.Errorf("write tmp sst chunk: %w", err)
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("commit sst chunk: %w", err)
	}

	// Evict heavy in-memory payload for cold-stored turns
	for _, idx := range targetIndices {
		tr := t.turns[idx]
		tr.StepCount = len(tr.Steps)
		tr.Steps = nil
		tr.Steers = nil
		tr.InputImages = nil
		tr.ColdStored = true
		tr.ColdChunkID = chunkID
	}

	t.coldChunks = append(t.coldChunks, chunk)
	t.coldDir = coldDir
	t.revision++

	return &chunk, nil
}

// TierColdTurnsAsync executes TierColdTurns in a background goroutine.
func (t *Trajectory) TierColdTurnsAsync(coldDir string, slidingWindowTurns int) <-chan error {
	ch := make(chan error, 1)
	go func() {
		_, err := t.TierColdTurns(coldDir, slidingWindowTurns)
		ch <- err
		close(ch)
	}()
	return ch
}

// LoadColdTurn reads and returns a specific historical turn from its SST chunk on demand.
func (t *Trajectory) LoadColdTurn(coldDir string, turnIndex int) (*TurnRecord, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.loadColdTurnLocked(coldDir, turnIndex)
}

func (t *Trajectory) loadColdTurnLocked(coldDir string, turnIndex int) (*TurnRecord, error) {
	if coldDir == "" {
		coldDir = t.coldDir
	}
	if coldDir == "" {
		return nil, errors.New("coldDir is required")
	}

	var targetChunk *ColdChunk
	for i := range t.coldChunks {
		c := &t.coldChunks[i]
		if turnIndex >= c.StartTurn && turnIndex <= c.EndTurn {
			targetChunk = c
			break
		}
	}

	var filePath string
	if targetChunk != nil {
		filePath = targetChunk.FilePath
	} else {
		// Multi-process / resumed session fallback: inspect on-disk .sst files
		pattern := filepath.Join(coldDir, "*.sst")
		matches, _ := filepath.Glob(pattern)
		for _, m := range matches {
			data, err := os.ReadFile(m)
			if err != nil {
				continue
			}
			var sst SSTChunkFile
			if err := json.Unmarshal(data, &sst); err == nil && sst.Magic == "PIGASST1" {
				if turnIndex >= sst.Chunk.StartTurn && turnIndex <= sst.Chunk.EndTurn {
					filePath = m
					break
				}
			}
		}
	}

	if filePath == "" {
		return nil, fmt.Errorf("cold chunk for turn %d not found in %s", turnIndex, coldDir)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read cold chunk: %w", err)
	}

	var sst SSTChunkFile
	if err := json.Unmarshal(data, &sst); err != nil {
		return nil, fmt.Errorf("unmarshal cold chunk: %w", err)
	}

	if sst.Magic != "PIGASST1" {
		return nil, fmt.Errorf("invalid sst magic: %q", sst.Magic)
	}

	for i, row := range sst.Index {
		if row.TurnIndex == turnIndex && i < len(sst.Turns) {
			return cloneTurnRecord(sst.Turns[i]), nil
		}
	}

	relIdx := turnIndex - sst.Chunk.StartTurn
	if relIdx >= 0 && relIdx < len(sst.Turns) {
		return cloneTurnRecord(sst.Turns[relIdx]), nil
	}

	return nil, fmt.Errorf("turn index %d not found in chunk %s", turnIndex, filePath)
}

// HydrateTurns restores all cold-stored turns back into full in-memory turns.
func (t *Trajectory) HydrateTurns(coldDir string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	for i, tr := range t.turns {
		if tr.ColdStored {
			hydrated, err := t.loadColdTurnLocked(coldDir, i)
			if err != nil {
				return err
			}
			t.turns[i] = hydrated
		}
	}
	t.revision++
	return nil
}

// TurnAt returns the turn record at idx, transparently hydrating it if cold-stored.
func (t *Trajectory) TurnAt(idx int) (*TurnRecord, error) {
	t.mu.RLock()
	if idx < 0 || idx >= len(t.turns) {
		t.mu.RUnlock()
		return nil, fmt.Errorf("turn index %d out of bounds (len=%d)", idx, len(t.turns))
	}
	tr := t.turns[idx]
	if !tr.ColdStored {
		defer t.mu.RUnlock()
		return cloneTurnRecord(tr), nil
	}
	t.mu.RUnlock()

	return t.LoadColdTurn(t.coldDir, idx)
}
