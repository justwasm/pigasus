package actor

import (
	"context"
	"fmt"
	"strings"
)

// ContextRequest supplies everything a ContextBudgetManager needs to materialize
// the conversation messages for the upcoming model call.
type ContextRequest struct {
	Trajectory        *Trajectory
	TurnRecord        *TurnRecord
	Input             string
	SteerInstructions []string
	Model             string
	MaxInputTokens    int // 0 = unbounded
	ToolPruningTurns  int // 0 = no pruning of historical tool outputs
}

// ContextBudgetManager determines how trajectory history and the active turn are
// materialized into []ConversationMessage for the model, enforcing budgets and pruning.
type ContextBudgetManager interface {
	MaterializeContext(ctx context.Context, req ContextRequest) ([]ConversationMessage, error)
}

// StandardBudgetManager implements ContextBudgetManager supporting:
// 1. Checkpoint Compaction: starts at checkpoint boundary with ResumeText.
// 2. Tool Pruning: contracts voluminous tool results from older turns into concise summaries.
// 3. Token Sliding Window: truncates older historical steps when total tokens exceed MaxInputTokens.
type StandardBudgetManager struct {
	MaxInputTokens   int
	ToolPruningTurns int
	MaxPrunedBytes   int
}

// BudgetOption configures a StandardBudgetManager.
type BudgetOption func(*StandardBudgetManager)

// WithMaxInputTokens sets the token budget ceiling.
func WithMaxInputTokens(tokens int) BudgetOption {
	return func(s *StandardBudgetManager) {
		s.MaxInputTokens = tokens
	}
}

// WithToolPruning sets the number of recent turns that retain full tool outputs.
func WithToolPruning(turns int) BudgetOption {
	return func(s *StandardBudgetManager) {
		s.ToolPruningTurns = turns
	}
}

// NewStandardBudgetManager returns a configured StandardBudgetManager.
func NewStandardBudgetManager(opts ...BudgetOption) *StandardBudgetManager {
	bm := &StandardBudgetManager{
		MaxPrunedBytes: 500,
	}
	for _, opt := range opts {
		opt(bm)
	}
	return bm
}

// defaultBudgetManagerInstance is the singleton default budget manager.
var defaultBudgetManagerInstance = NewStandardBudgetManager()

// DefaultBudgetManager returns the default context budget manager.
func DefaultBudgetManager() ContextBudgetManager {
	return defaultBudgetManagerInstance
}

// MaterializeContext builds the conversation messages honoring compaction, tool pruning,
// and sliding window budgets.
func (m *StandardBudgetManager) MaterializeContext(ctx context.Context, req ContextRequest) ([]ConversationMessage, error) {
	if req.Trajectory == nil {
		return nil, nil
	}

	maxInputTokens := req.MaxInputTokens
	if maxInputTokens <= 0 {
		maxInputTokens = m.MaxInputTokens
	}
	toolPruningTurns := req.ToolPruningTurns
	if toolPruningTurns <= 0 {
		toolPruningTurns = m.ToolPruningTurns
	}
	maxPrunedBytes := m.MaxPrunedBytes
	if maxPrunedBytes <= 0 {
		maxPrunedBytes = 500
	}

	// 1. Gather historical messages respecting Checkpoint boundary (Strategy A)
	history := m.materializeHistory(req.Trajectory, req.TurnRecord, toolPruningTurns, maxPrunedBytes)

	// 2. Append current turn messages (Input, current steps, steer)
	currentMsgs := m.materializeCurrentTurn(req)

	// Combine history and current turn
	combined := append(history, currentMsgs...)

	// 3. Apply Sliding Window Truncation if MaxInputTokens is configured (Strategy B)
	if maxInputTokens > 0 {
		combined = m.applySlidingWindow(combined, currentMsgs, maxInputTokens)
	}

	return combined, nil
}

func (m *StandardBudgetManager) materializeHistory(
	t *Trajectory,
	currentTurn *TurnRecord,
	toolPruningTurns int,
	maxPrunedBytes int,
) []ConversationMessage {
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

	currentTurnIndex := limit
	if currentTurn != nil {
		currentTurnIndex = len(t.turns)
	}

	for ti, turn := range t.turns[:limit] {
		keepTurnInput := boundary < 0 || ti > cpTurn || (ti == cpTurn && cpKeepInput)
		shouldPruneTools := toolPruningTurns > 0 && (currentTurnIndex-ti) > toolPruningTurns
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
			stepMsgs := m.materializeStepMessages(step, input, inputImages, shouldPruneTools, maxPrunedBytes)
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

func (m *StandardBudgetManager) materializeStepMessages(
	step Step,
	input string,
	inputImages []ImagePart,
	shouldPruneTools bool,
	maxPrunedBytes int,
) []ConversationMessage {
	msgs := stepConversationMessages(step, input, inputImages)
	if shouldPruneTools {
		for i := range msgs {
			if msgs[i].Role != RoleTool {
				continue
			}
			// Image payloads are pruned independently of text length or errors.
			msgs[i].Images = nil
			if !strings.HasPrefix(msgs[i].Content, "Error: ") && len(msgs[i].Content) > maxPrunedBytes {
				msgs[i].Content = fmt.Sprintf("[Tool output pruned: %d characters omitted]", len(msgs[i].Content))
			}
		}
	}
	return msgs
}

func (m *StandardBudgetManager) materializeCurrentTurn(req ContextRequest) []ConversationMessage {
	if req.TurnRecord == nil {
		return nil
	}

	from, hasCheckpoint, keepInput := req.Trajectory.ContextFor(req.TurnRecord.ID)
	includeInput := !hasCheckpoint || keepInput

	input := req.Input
	steps := req.TurnRecord.Steps
	steer := req.SteerInstructions

	var msgs []ConversationMessage
	if len(steer) > 0 && len(steps) == 0 && includeInput {
		input += "\n\n[Active Guidance]: " + strings.Join(steer, "; ")
	}

	if includeInput {
		msgs = append(msgs, ConversationMessage{
			Role:    RoleUser,
			Content: input,
			Images:  cloneImageParts(req.TurnRecord.InputImages),
		})
	}

	for _, step := range steps {
		if hasCheckpoint && step.GlobalIndex <= from {
			continue
		}
		msgs = append(msgs, stepConversationMessages(step, "", nil)...)
	}

	if len(steer) > 0 && (includeInput || len(msgs) > 0) {
		msgs = append(msgs, ConversationMessage{
			Role:    RoleUser,
			Content: "[Active Guidance]: " + strings.Join(steer, "; "),
		})
	}

	return msgs
}

// applySlidingWindow trims older messages backwards when estimated tokens exceed maxTokens.
// The current turn and the initial resume block (if present) are preserved.
func (m *StandardBudgetManager) applySlidingWindow(
	allMsgs []ConversationMessage,
	currentMsgs []ConversationMessage,
	maxTokens int,
) []ConversationMessage {
	totalTokens := estimateMessagesTokens(allMsgs)
	if totalTokens <= maxTokens {
		return allMsgs
	}

	hasResume := len(allMsgs) > 0 && strings.HasPrefix(allMsgs[0].Content, "# Resuming from a compaction")
	currentCount := len(currentMsgs)
	histCount := len(allMsgs) - currentCount
	if histCount <= 0 {
		return allMsgs
	}

	// Always preserve the current turn
	currentTokens := estimateMessagesTokens(currentMsgs)
	budgetRemaining := maxTokens - currentTokens
	if budgetRemaining <= 0 {
		// Even current turn exceeds budget; return current turn as minimum viable context
		if hasResume {
			return append([]ConversationMessage{allMsgs[0]}, currentMsgs...)
		}
		return currentMsgs
	}

	firstHistIdx := 0
	var prefix []ConversationMessage
	if hasResume {
		prefix = []ConversationMessage{allMsgs[0]}
		firstHistIdx = 1
		budgetRemaining -= estimateMessageTokens(allMsgs[0])
		if budgetRemaining <= 0 {
			return append(prefix, currentMsgs...)
		}
	}

	// Scan backwards through historical messages
	historySlice := allMsgs[firstHistIdx:histCount]
	keptStart := len(historySlice)
	accumulated := 0

	for i := len(historySlice) - 1; i >= 0; i-- {
		t := estimateMessageTokens(historySlice[i])
		if accumulated+t > budgetRemaining {
			break
		}
		accumulated += t
		keptStart = i
	}

	out := append(prefix, historySlice[keptStart:]...)
	out = append(out, currentMsgs...)
	return out
}

func estimateMessageTokens(m ConversationMessage) int {
	tokens := (len(m.Content) + len(m.Thought) + 3) / 4
	if tokens < 1 {
		tokens = 1
	}
	for _, tc := range m.ToolCalls {
		tokens += (len(tc.Name) + len(tc.Input) + 3) / 4
	}
	return tokens
}

func estimateMessagesTokens(msgs []ConversationMessage) int {
	total := 0
	for _, m := range msgs {
		total += estimateMessageTokens(m)
	}
	return total
}
