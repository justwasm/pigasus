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

// Step records one interaction cycle between the actor and the provider
// (model generation + tool execution).
type Step struct {
	Index       int          `json:"index"`
	Thought     string       `json:"thought,omitempty"`
	Delta       string       `json:"delta,omitempty"`
	ToolCalls   []ToolCall   `json:"tool_calls,omitempty"`
	ToolResults []ToolResult `json:"tool_results,omitempty"`
	Steer       []string     `json:"steer,omitempty"`
	Usage       *TokenUsage  `json:"usage,omitempty"`
}

// TurnRecord documents an entire processed turn from input to conclusion.
type TurnRecord struct {
	ID          string     `json:"id"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt time.Time  `json:"completed_at"`
	Input       Message    `json:"input"`
	Steers      []string   `json:"steers,omitempty"`
	Steps       []Step     `json:"steps"`
	FinalOutput string     `json:"final_output"`
	Status      string     `json:"status"` // "done", "error", "cancelled"
	Error       string     `json:"error,omitempty"`
	Usage       TokenUsage `json:"usage"`
}

// Trajectory manages the ordered history of turns and steps executed by an Actor.
type Trajectory struct {
	mu    sync.RWMutex
	turns []*TurnRecord
}

// NewTrajectory allocates a new empty trajectory.
func NewTrajectory() *Trajectory {
	return &Trajectory{
		turns: make([]*TurnRecord, 0),
	}
}

// AddTurn appends a completed turn record.
func (t *Trajectory) AddTurn(tr *TurnRecord) {
	if tr == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.turns = append(t.turns, tr)
}

// SeedHistory populates the trajectory from a list of ConversationMessages.
func (t *Trajectory) SeedHistory(history []ConversationMessage) {
	if len(history) == 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	var current *TurnRecord
	for _, m := range history {
		switch m.Role {
		case RoleUser:
			if current != nil {
				t.turns = append(t.turns, current)
			}
			current = &TurnRecord{
				ID:          fmt.Sprintf("hist_%d", len(t.turns)+1),
				CreatedAt:   time.Now(),
				CompletedAt: time.Now(),
				Input:       NewUserMessage(m.Content),
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
					CreatedAt:   time.Now(),
					CompletedAt: time.Now(),
					Input:       NewUserMessage(""),
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
}

// LastTurn returns the most recent turn record, or nil if none.
func (t *Trajectory) LastTurn() *TurnRecord {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if len(t.turns) == 0 {
		return nil
	}
	return t.turns[len(t.turns)-1]
}

// Turns returns a slice copy of all turn records.
func (t *Trajectory) Turns() []*TurnRecord {
	t.mu.RLock()
	defer t.mu.RUnlock()
	copied := make([]*TurnRecord, len(t.turns))
	copy(copied, t.turns)
	return copied
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

// Compact summarizes or truncates older turns, retaining the most recent keepRecent turns.
func (t *Trajectory) Compact(keepRecent int) (compacted int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if keepRecent <= 0 {
		keepRecent = 1
	}
	if len(t.turns) <= keepRecent {
		return 0
	}

	compactCount := len(t.turns) - keepRecent

	// Build a compact summary turn for the pruned turns
	var summaryBuilder strings.Builder
	summaryBuilder.WriteString(fmt.Sprintf("[Compacted summary of previous %d turns]:\n", compactCount))
	for i := 0; i < compactCount; i++ {
		turn := t.turns[i]
		inputText := ""
		if usr, ok := turn.Input.(*UserMessage); ok {
			inputText = usr.Text
		} else if sch, ok := turn.Input.(*ScheduledWakeup); ok {
			inputText = sch.Prompt
		}
		summaryBuilder.WriteString(fmt.Sprintf("- Turn %d (%s): User asked %q -> Outcome: %s\n",
			i+1, turn.ID, truncateString(inputText, 60), truncateString(turn.FinalOutput, 100)))
	}

	summaryTurn := &TurnRecord{
		ID:          newID("turn_compact"),
		CreatedAt:   t.turns[0].CreatedAt,
		CompletedAt: time.Now(),
		Input:       NewUserMessage(summaryBuilder.String()),
		FinalOutput: "Summary of prior turns preserved.",
		Status:      "done",
	}

	newTurns := make([]*TurnRecord, 0, keepRecent+1)
	newTurns = append(newTurns, summaryTurn)
	newTurns = append(newTurns, t.turns[compactCount:]...)
	t.turns = newTurns

	return compactCount
}

// ToConversationMessages converts the stored turns into provider messages.
func (t *Trajectory) ToConversationMessages() []ConversationMessage {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var msgs []ConversationMessage

	for _, turn := range t.turns {
		// Input message
		var inputContent string
		if u, ok := turn.Input.(*UserMessage); ok {
			inputContent = u.Text
		} else if s, ok := turn.Input.(*ScheduledWakeup); ok {
			inputContent = fmt.Sprintf("[Scheduled wakeup: %s]\n%s", s.Source, s.Prompt)
		} else if st, ok := turn.Input.(*SteerMessage); ok {
			inputContent = fmt.Sprintf("[Steer guidance]: %s", st.Instruction)
		} else {
			inputContent = turn.ID
		}

		msgs = append(msgs, ConversationMessage{
			Role:    RoleUser,
			Content: inputContent,
		})

		// Intermediate steps with tool calls and steers
		var stepSteersTotal int
		for _, step := range turn.Steps {
			if len(step.Steer) > 0 {
				stepSteersTotal += len(step.Steer)
				msgs = append(msgs, ConversationMessage{
					Role:    RoleUser,
					Content: "[Active Guidance]: " + strings.Join(step.Steer, "; "),
				})
			}
			if len(step.ToolCalls) > 0 {
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
		}

		if len(turn.Steers) > stepSteersTotal {
			for _, st := range turn.Steers[stepSteersTotal:] {
				msgs = append(msgs, ConversationMessage{
					Role:    RoleUser,
					Content: "[Active Guidance]: " + st,
				})
			}
		}

		// Final assistant output if not empty
		if turn.FinalOutput != "" {
			msgs = append(msgs, ConversationMessage{
				Role:    RoleAssistant,
				Content: turn.FinalOutput,
			})
		}
	}

	return msgs
}

func truncateString(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
