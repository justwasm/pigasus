package mockremoteagent

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/justwasm/pigasus/internal/core/interaction"
)

// TrajectoryStep represents one step in the trajectory.
type TrajectoryStep struct {
	StepIndex int       `json:"stepIndex"`
	Type      string    `json:"type"`   // e.g. "USER_INPUT", "PLANNER_RESPONSE", "TOOL_CALL"
	Status    string    `json:"status"` // "DONE", "RUNNING", "WAITING"
	Content   string    `json:"content"`
	Thinking  string    `json:"thinking,omitempty"`
	ToolCalls []any     `json:"toolCalls,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	// Interaction fields carry a step that waits for the user: an
	// ask_question (type 138) or a permission request (type 132).
	AskQuestions          []any  `json:"askQuestions,omitempty"`
	RequestedInteraction  any    `json:"requestedInteraction,omitempty"`
	CompletedInteractions []any  `json:"completedInteractions,omitempty"`
	ToolName              string `json:"toolName,omitempty"`
	ToolArgs              string `json:"toolArgs,omitempty"`

	// FilePath and DiffLines carry a FILE_CHANGE step (86): the edited
	// file's URI and the DiffBlock lines getCodeActionDiff serves.
	FilePath  string     `json:"filePath,omitempty"`
	DiffLines []DiffLine `json:"diffLines,omitempty"`
}

// DiffLine is one line of a DiffBlock's unified diff. Type follows
// exa.diff_action_pb.UnifiedDiffLineType (0 unspecified, 1 insert,
// 2 delete, 3 unchanged).
type DiffLine struct {
	Type int32  `json:"type"`
	Text string `json:"text"`
}

// pendingInteraction is one step parked on the user's answer.
type pendingInteraction struct {
	request interaction.Request
	ch      chan interaction.Answer
}

// CascadeSession holds the state of a single remote agent cascade.
type CascadeSession struct {
	mu            sync.RWMutex
	ID            string
	ProjectID     string
	Model         string
	AuthToken     string
	WorkspaceURIs []string
	Environment   *Environment
	Status        string // "IDLE", "RUNNING", "COMPLETED", "FAILED"
	Steps         []TrajectoryStep
	// resumeSeq is the monotonic frame sequence the resumeToken reports.
	// It advances on every broadcast, independent of the step count: an
	// in-place step update (a streaming reply, a final IDLE) must still be
	// a new frame or the client drops it as a duplicate.
	resumeSeq   int
	Subscribers map[chan StreamAgentStateUpdatesResponse]struct{}
	turnCancel  func()
	CreatedAt   time.Time
	UpdatedAt   time.Time

	// pending holds the interactions a turn is parked on, keyed by the
	// step index the client's answer names.
	pending map[int]*pendingInteraction
}

// NewCascadeSession creates a new CascadeSession.
func NewCascadeSession(id, projectID, model string, ws []string, env *Environment) *CascadeSession {
	return &CascadeSession{
		ID:            id,
		ProjectID:     projectID,
		Model:         model,
		WorkspaceURIs: ws,
		Environment:   env,
		Status:        "IDLE",
		Steps:         make([]TrajectoryStep, 0),
		Subscribers:   make(map[chan StreamAgentStateUpdatesResponse]struct{}),
		pending:       make(map[int]*pendingInteraction),
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}
}

// AddStep appends a step to the session and broadcasts an update frame to subscribers.
func (cs *CascadeSession) AddStep(stepType, content, thinking string, toolCalls []any) TrajectoryStep {
	return cs.AddStepWithStatus(stepType, "DONE", content, thinking, toolCalls)
}

// AddStepWithStatus appends a step with the specified status and broadcasts an update frame.
func (cs *CascadeSession) AddStepWithStatus(stepType, status, content, thinking string, toolCalls []any) TrajectoryStep {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	now := time.Now().UTC()
	step := TrajectoryStep{
		StepIndex: len(cs.Steps),
		Type:      stepType,
		Status:    status,
		Content:   content,
		Thinking:  thinking,
		ToolCalls: toolCalls,
		CreatedAt: now,
		UpdatedAt: now,
	}
	cs.Steps = append(cs.Steps, step)
	cs.UpdatedAt = now

	cs.broadcastLocked()
	return step
}

// UpdateStep updates an existing step by index in place and broadcasts an update frame.
func (cs *CascadeSession) UpdateStep(idx int, status, content, thinking string, toolCalls []any) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if idx < 0 || idx >= len(cs.Steps) {
		return
	}
	now := time.Now().UTC()
	if status != "" {
		cs.Steps[idx].Status = status
	}
	if content != "" {
		cs.Steps[idx].Content = content
	}
	if thinking != "" {
		cs.Steps[idx].Thinking = thinking
	}
	if toolCalls != nil {
		cs.Steps[idx].ToolCalls = toolCalls
	}
	cs.Steps[idx].UpdatedAt = now
	cs.UpdatedAt = now

	cs.broadcastLocked()
}

// AskUser publishes a WAITING step and blocks until the client answers it
// with HandleCascadeUserInteraction (or the turn is cancelled).
//
// The step's shape follows the reference's own wire form: an ask_question
// is CORTEX_STEP_TYPE_ASK_QUESTION (138) with the questions in both
// `askQuestion` and `requestedInteraction.askQuestion`; a permission is a
// generic step carrying `requestedInteraction.permission`.
func (cs *CascadeSession) AskUser(ctx context.Context, req interaction.Request) (interaction.Answer, error) {
	cs.mu.Lock()
	now := time.Now().UTC()
	step := TrajectoryStep{
		StepIndex: len(cs.Steps),
		Status:    "WAITING",
		ToolName:  req.ToolName,
		ToolArgs:  req.ArgsJSON,
		CreatedAt: now,
		UpdatedAt: now,
	}
	switch req.Kind {
	case interaction.KindQuestion:
		step.Type = "ASK_QUESTION"
		questions := make([]any, 0, len(req.Questions))
		for _, q := range req.Questions {
			options := make([]any, 0, len(q.Options))
			for i, text := range q.Options {
				options = append(options, map[string]any{
					"id":   strconv.Itoa(i + 1),
					"text": text,
				})
			}
			questions = append(questions, map[string]any{
				"question":      q.Prompt,
				"options":       options,
				"isMultiSelect": q.IsMultiSelect,
			})
		}
		step.AskQuestions = questions
		step.RequestedInteraction = map[string]any{
			"askQuestion": map[string]any{"questions": questions},
		}
	default:
		step.Type = "GENERIC"
		resource := map[string]any{"action": req.Action, "target": req.Target}
		permission := map[string]any{"resource": resource}
		if req.Reason != "" {
			permission["reason"] = req.Reason
		}
		if req.Action != "" {
			permission["actionDescription"] = req.Action
		}
		step.RequestedInteraction = map[string]any{"permission": permission}
	}

	ch := make(chan interaction.Answer, 1)
	index := step.StepIndex
	cs.pending[index] = &pendingInteraction{request: req, ch: ch}
	cs.Steps = append(cs.Steps, step)
	cs.UpdatedAt = now
	cs.broadcastLocked()
	cs.mu.Unlock()

	select {
	case answer := <-ch:
		return answer, nil
	case <-ctx.Done():
		cs.mu.Lock()
		if _, stillPending := cs.pending[index]; stillPending {
			delete(cs.pending, index)
			cs.completeInteractionLocked(index, nil)
			cs.broadcastLocked()
		}
		cs.mu.Unlock()
		return interaction.Answer{Cancelled: true}, nil
	}
}

// ResolveInteraction delivers the client's answer to the parked step.
// It reports whether a pending interaction matched the step index.
func (cs *CascadeSession) ResolveInteraction(stepIndex int, answer interaction.Answer, response any) bool {
	cs.mu.Lock()
	p := cs.pending[stepIndex]
	if p == nil {
		cs.mu.Unlock()
		return false
	}
	delete(cs.pending, stepIndex)
	cs.completeInteractionLocked(stepIndex, response)
	cs.broadcastLocked()
	cs.mu.Unlock()

	select {
	case p.ch <- answer:
	default:
	}
	return true
}

// completeInteractionLocked closes a waiting step: the step is done and
// carries the request/response exchange the reference keeps in
// completedInteractions.
func (cs *CascadeSession) completeInteractionLocked(index int, response any) {
	if index < 0 || index >= len(cs.Steps) {
		return
	}
	cs.Steps[index].Status = "DONE"
	cs.Steps[index].UpdatedAt = time.Now().UTC()
	if response == nil {
		return
	}
	cs.Steps[index].CompletedInteractions = []any{map[string]any{
		"request":  cs.Steps[index].RequestedInteraction,
		"response": response,
	}}
}

// StepsSnapshot returns a copy of all steps in the session under lock.
func (cs *CascadeSession) StepsSnapshot() []TrajectoryStep {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	res := make([]TrajectoryStep, len(cs.Steps))
	copy(res, cs.Steps)
	return res
}

// BeginTurn marks the session as RUNNING and creates a cancellable context for the turn.
func (cs *CascadeSession) BeginTurn() (context.Context, func()) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if cs.turnCancel != nil {
		cs.turnCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	cs.turnCancel = cancel
	cs.Status = "RUNNING"
	cs.UpdatedAt = time.Now().UTC()
	cs.broadcastLocked()
	return ctx, cancel
}

// CancelTurn cancels any active turn in flight and resets status to IDLE.
func (cs *CascadeSession) CancelTurn() {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if cs.turnCancel != nil {
		cs.turnCancel()
		cs.turnCancel = nil
	}
	cs.Status = "IDLE"
	cs.UpdatedAt = time.Now().UTC()
	cs.broadcastLocked()
}

// SetStatus updates status and broadcasts update.
func (cs *CascadeSession) SetStatus(status string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.Status = status
	cs.UpdatedAt = time.Now().UTC()
	cs.broadcastLocked()
}

// GetStatus returns the current status thread-safely.
func (cs *CascadeSession) GetStatus() string {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.Status
}

// Subscribe adds a subscriber channel for state updates.
func (cs *CascadeSession) Subscribe() chan StreamAgentStateUpdatesResponse {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	ch := make(chan StreamAgentStateUpdatesResponse, 32)
	cs.Subscribers[ch] = struct{}{}

	// Send initial snapshot immediately
	cs.resumeSeq++
	frame := cs.buildStateFrameLocked()
	ch <- frame
	return ch
}

// Unsubscribe removes a subscriber channel.
func (cs *CascadeSession) Unsubscribe(ch chan StreamAgentStateUpdatesResponse) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if _, ok := cs.Subscribers[ch]; ok {
		delete(cs.Subscribers, ch)
		close(ch)
	}
}

func (cs *CascadeSession) broadcastLocked() {
	cs.resumeSeq++
	frame := cs.buildStateFrameLocked()
	for ch := range cs.Subscribers {
		select {
		case ch <- frame:
		default:
			// Non-blocking drop if channel is full
		}
	}
}

const (
	stepTypeCodeAction      = 5
	stepTypeUserInput       = 14
	stepTypePlannerResponse = 15
	stepTypeRunCommand      = 21
	stepTypeFileChange      = 86
	stepTypeGeneric         = 132
	stepTypeAskQuestion     = 138

	stepStatusDone       = 3
	stepStatusGenerating = 8
	stepStatusWaiting    = 9

	runStatusIdle    = 1
	runStatusRunning = 2

	stepSourceModel        = 2
	stepSourceUserExplicit = 4
)

func formatProtoStep(cs *CascadeSession, s TrajectoryStep, index int) map[string]any {
	nowStr := s.UpdatedAt.Format(time.RFC3339Nano)
	createdStr := s.CreatedAt.Format(time.RFC3339Nano)

	meta := map[string]any{
		"createdAt": createdStr,
		"sourceTrajectoryStepInfo": map[string]any{
			"cascadeId":    cs.ID,
			"trajectoryId": fmt.Sprintf("traj-%s", cs.ID),
			"stepIndex":    index,
		},
	}

	step := map[string]any{
		"metadata": meta,
	}

	switch s.Type {
	case "USER_INPUT", "CORTEX_STEP_TYPE_USER_INPUT":
		step["type"] = stepTypeUserInput
		step["status"] = stepStatusDone
		meta["source"] = stepSourceUserExplicit
		step["userInput"] = map[string]any{
			"query":        s.Content,
			"userResponse": s.Content,
			"items": []any{
				map[string]any{"text": s.Content},
			},
		}
	case "PLANNER_RESPONSE", "CORTEX_STEP_TYPE_PLANNER_RESPONSE":
		step["type"] = stepTypePlannerResponse
		meta["source"] = stepSourceModel
		if s.Status == "RUNNING" || s.Status == "CORTEX_STEP_STATUS_GENERATING" {
			step["status"] = stepStatusGenerating
		} else {
			step["status"] = stepStatusDone
			meta["viewableAt"] = nowStr
			meta["finishedGeneratingAt"] = nowStr
			meta["completedAt"] = nowStr
		}
		planner := map[string]any{}
		if s.Content != "" {
			planner["response"] = s.Content
		}
		if s.Thinking != "" {
			planner["thinking"] = s.Thinking
		}
		step["plannerResponse"] = planner
	case "RUN_COMMAND", "CORTEX_STEP_TYPE_RUN_COMMAND":
		step["type"] = stepTypeRunCommand
		step["status"] = stepStatusDone
		meta["source"] = stepSourceModel
		meta["viewableAt"] = nowStr
		meta["finishedGeneratingAt"] = nowStr
		meta["completedAt"] = nowStr

		cmdLine := s.Content
		exitCode := 0
		var stdout, stderr, combined string
		if len(s.ToolCalls) > 0 {
			if tc, ok := s.ToolCalls[0].(map[string]any); ok {
				if c, ok := tc["commandLine"].(string); ok && c != "" {
					cmdLine = c
				} else if c, ok := tc["command"].(string); ok && c != "" {
					cmdLine = c
				}
				if code, ok := tc["exitCode"].(int); ok {
					exitCode = code
				}
				if out, ok := tc["stdout"].(string); ok {
					stdout = out
				}
				if errOut, ok := tc["stderr"].(string); ok {
					stderr = errOut
				}
				if comb, ok := tc["combined"].(string); ok && comb != "" {
					combined = comb
				}
			}
		}
		if combined == "" {
			if stdout != "" && stderr != "" {
				combined = stdout + "\n" + stderr
			} else if stdout != "" {
				combined = stdout
			} else if stderr != "" {
				combined = stderr
			} else {
				combined = s.Content
			}
		}

		step["runCommand"] = map[string]any{
			"commandLine": cmdLine,
			"exitCode":    exitCode,
			"stdout":      stdout,
			"stderr":      stderr,
			"combinedOutput": map[string]any{
				"full": combined,
			},
		}
	case "ASK_QUESTION", "CORTEX_STEP_TYPE_ASK_QUESTION":
		step["type"] = stepTypeAskQuestion
		if s.Status == "WAITING" {
			step["status"] = stepStatusWaiting
		} else {
			step["status"] = stepStatusDone
		}
		meta["source"] = stepSourceModel
		if s.ToolName != "" {
			meta["toolCall"] = map[string]any{
				"id":            s.ToolName,
				"name":          s.ToolName,
				"argumentsJson": s.ToolArgs,
			}
		}
		questions := s.AskQuestions
		if questions == nil {
			questions = []any{}
		}
		step["askQuestion"] = map[string]any{"questions": questions}
		step["requestedInteraction"] = map[string]any{
			"askQuestion": map[string]any{"questions": questions},
		}
		if len(s.CompletedInteractions) > 0 {
			step["completedInteractions"] = s.CompletedInteractions
		}
	case "FILE_CHANGE", "CORTEX_STEP_TYPE_FILE_CHANGE":
		step["type"] = stepTypeFileChange
		step["status"] = stepStatusDone
		meta["source"] = stepSourceModel
		if s.ToolName != "" {
			meta["toolCall"] = map[string]any{
				"id":            s.ToolName,
				"name":          s.ToolName,
				"argumentsJson": s.ToolArgs,
			}
		}
		fileChange := map[string]any{"absolutePathUri": s.FilePath}
		if len(s.DiffLines) > 0 {
			lines := make([]any, 0, len(s.DiffLines))
			for _, line := range s.DiffLines {
				lines = append(lines, map[string]any{"type": line.Type, "text": line.Text})
			}
			fileChange["diff"] = map[string]any{"unifiedDiff": map[string]any{"lines": lines}}
		}
		step["fileChange"] = fileChange
	case "CODE_ACTION", "CORTEX_STEP_TYPE_CODE_ACTION":
		step["type"] = stepTypeCodeAction
		step["status"] = stepStatusDone
		meta["source"] = stepSourceModel
		meta["viewableAt"] = nowStr
		meta["finishedGeneratingAt"] = nowStr
		meta["completedAt"] = nowStr
		step["codeAction"] = map[string]any{
			"codeInstruction": s.Content,
		}
	default:
		step["type"] = stepTypeGeneric
		if s.Status == "WAITING" {
			step["status"] = stepStatusWaiting
		} else {
			step["status"] = stepStatusDone
		}
		meta["source"] = stepSourceModel
		if s.ToolName != "" {
			meta["toolCall"] = map[string]any{
				"id":            s.ToolName,
				"name":          s.ToolName,
				"argumentsJson": s.ToolArgs,
			}
		}
		if s.RequestedInteraction != nil {
			step["requestedInteraction"] = s.RequestedInteraction
		}
		if len(s.CompletedInteractions) > 0 {
			step["completedInteractions"] = s.CompletedInteractions
		}

		// A generic step is how the reference carries every tool that is
		// not run_command (view_file, write_file, search_web, ...). The
		// tool's name and arguments live on metadata.toolCall; the result
		// lives under generic.result. Older callers put the result in
		// Content, so keep that as the fallback.
		toolName, argsJSON, result := "", "", s.Content
		if len(s.ToolCalls) > 0 {
			if tc, ok := s.ToolCalls[0].(map[string]any); ok {
				if v, _ := tc["toolName"].(string); v != "" {
					toolName = v
				}
				if v, _ := tc["args"].(string); v != "" {
					argsJSON = v
				}
				if v, _ := tc["result"].(string); v != "" {
					result = v
				}
			}
		}
		if toolName != "" {
			meta["toolCall"] = map[string]any{
				"id":            toolName,
				"name":          toolName,
				"argumentsJson": argsJSON,
			}
		}
		generic := map[string]any{
			"result": map[string]any{"result": result},
		}
		if argsJSON != "" {
			generic["args"] = map[string]any{"raw": argsJSON}
		}
		step["generic"] = generic
	}

	return step
}

func (cs *CascadeSession) buildStateFrameLocked() StreamAgentStateUpdatesResponse {
	stepMaps := make([]map[string]any, len(cs.Steps))
	indices := make([]int, len(cs.Steps))
	lastStepType := stepTypeUserInput
	for i, s := range cs.Steps {
		indices[i] = i
		stepMaps[i] = formatProtoStep(cs, s, i)
		switch s.Type {
		case "PLANNER_RESPONSE", "CORTEX_STEP_TYPE_PLANNER_RESPONSE":
			lastStepType = stepTypePlannerResponse
		case "USER_INPUT", "CORTEX_STEP_TYPE_USER_INPUT":
			lastStepType = stepTypeUserInput
		case "RUN_COMMAND", "CORTEX_STEP_TYPE_RUN_COMMAND":
			lastStepType = stepTypeRunCommand
		case "CODE_ACTION", "CORTEX_STEP_TYPE_CODE_ACTION":
			lastStepType = stepTypeCodeAction
		case "FILE_CHANGE", "CORTEX_STEP_TYPE_FILE_CHANGE":
			lastStepType = stepTypeFileChange
		case "ASK_QUESTION", "CORTEX_STEP_TYPE_ASK_QUESTION":
			lastStepType = stepTypeAskQuestion
		default:
			lastStepType = stepTypeGeneric
		}
	}

	isRunning := cs.Status == "RUNNING"
	statusInt := runStatusIdle
	if isRunning {
		statusInt = runStatusRunning
	}

	return StreamAgentStateUpdatesResponse{
		Update: map[string]any{
			"conversationId":     cs.ID,
			"trajectoryId":       fmt.Sprintf("traj-%s", cs.ID),
			"status":             statusInt,
			"executableStatus":   statusInt,
			"executorLoopStatus": statusInt,
			"fullyIdle":          !isRunning,
			"mainTrajectoryUpdate": map[string]any{
				"stepsUpdate": map[string]any{
					"indices":     indices,
					"steps":       stepMaps,
					"totalLength": len(cs.Steps),
					"pageBounds": map[string]any{
						"startIndex":        0,
						"endIndexExclusive": len(cs.Steps),
					},
				},
				"metadata":     map[string]any{},
				"status":       statusInt,
				"lastStepType": lastStepType,
				"fullyIdle":    !isRunning,
			},
		},
		ResumeToken: fmt.Sprintf("tok-%d", cs.resumeSeq),
	}
}

// CascadeManager manages all active cascade sessions in memory.
type CascadeManager struct {
	mu       sync.RWMutex
	cascades map[string]*CascadeSession
}

// NewCascadeManager creates a new CascadeManager.
func NewCascadeManager() *CascadeManager {
	return &CascadeManager{
		cascades: make(map[string]*CascadeSession),
	}
}

// CreateSession allocates a new session.
func (cm *CascadeManager) CreateSession(id, projectID, model string, ws []string, env *Environment) *CascadeSession {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	session := NewCascadeSession(id, projectID, model, ws, env)
	cm.cascades[id] = session
	return session
}

// GetSession returns a session by ID.
func (cm *CascadeManager) GetSession(id string) (*CascadeSession, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	s, ok := cm.cascades[id]
	return s, ok
}

// DeleteSession removes a session.
func (cm *CascadeManager) DeleteSession(id string) bool {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	s, ok := cm.cascades[id]
	if !ok {
		return false
	}
	s.mu.Lock()
	for ch := range s.Subscribers {
		close(ch)
	}
	s.Subscribers = nil
	s.mu.Unlock()

	delete(cm.cascades, id)
	return true
}

// ListSummaries returns summaries of all active sessions.
func (cm *CascadeManager) ListSummaries() []ConversationSummary {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	summaries := make([]ConversationSummary, 0, len(cm.cascades))
	for _, s := range cm.cascades {
		s.mu.RLock()
		title := "New Session"
		if len(s.Steps) > 0 && s.Steps[0].Content != "" {
			title = s.Steps[0].Content
			if len(title) > 30 {
				title = title[:30] + "..."
			}
		}
		summaries = append(summaries, ConversationSummary{
			Summary:          title,
			TrajectoryID:     s.ID,
			StepCount:        len(s.Steps),
			LastModifiedTime: s.UpdatedAt.Format(time.RFC3339Nano),
			CreatedTime:      s.CreatedAt.Format(time.RFC3339Nano),
			Status:           "CASCADE_RUN_STATUS_IDLE",
		})
		s.mu.RUnlock()
	}
	return summaries
}

// AddFileChangeStep appends a FILE_CHANGE step (86) carrying the edited
// file's URI and its DiffBlock lines.
func (cs *CascadeSession) AddFileChangeStep(filePath, toolName, toolArgs string, lines []DiffLine) TrajectoryStep {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	now := time.Now().UTC()
	step := TrajectoryStep{
		StepIndex: len(cs.Steps),
		Type:      "FILE_CHANGE",
		Status:    "DONE",
		FilePath:  filePath,
		DiffLines: lines,
		ToolName:  toolName,
		ToolArgs:  toolArgs,
		CreatedAt: now,
		UpdatedAt: now,
	}
	cs.Steps = append(cs.Steps, step)
	cs.UpdatedAt = now
	cs.broadcastLocked()
	return step
}
