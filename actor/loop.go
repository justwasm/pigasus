package actor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// turnEnvelope is the mailbox representation of a submitted turn: its prompt
// plus the configuration captured when it was submitted.
type turnEnvelope struct {
	handle *TurnHandle
	config TurnConfig
	prompt string
}

func (a *Actor) loop() {
	defer close(a.done)

	for {
		select {
		case <-a.ctx.Done():
			a.finishPending(ErrActorClosed)
			return
		case env := <-a.mailbox:
			a.executeTurn(env)
		}
	}
}

// finishPending closes every handle still waiting in the mailbox. It runs when
// the loop stops, so a queued turn never leaves a caller waiting on Done.
func (a *Actor) finishPending(err error) {
	a.mu.Lock()
	pending := make([]*TurnHandle, 0, len(a.pending))
	for h := range a.pending {
		pending = append(pending, h)
		delete(a.pending, h)
	}
	a.mu.Unlock()

	for _, h := range pending {
		h.complete(err)
	}
}

// StreamInterruptedMessage is the verbatim recovery instruction injected
// when a model stream is truncated or interrupted.
const StreamInterruptedMessage = "The stream was interrupted. Please continue the task you were working on."

// isTransientNetworkError identifies recoverable socket / connection errors.
func isTransientNetworkError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	transientPhrases := []string{
		"broken pipe",
		"software caused connection abort",
		"network is down",
		"network is unreachable",
		"http2: client connection lost",
		"http2: stream closed",
		"http2: client disconnected",
		"connection reset by peer",
		"connection refused",
		"connection timed out",
		"unexpected eof",
	}
	for _, phrase := range transientPhrases {
		if strings.Contains(msg, phrase) {
			return true
		}
	}
	return false
}

func (a *Actor) executeTurn(env *turnEnvelope) {
	h := env.handle
	cfg := env.config
	input := env.prompt
	startTime := a.now()

	// A turn cancelled while it was queued never starts.
	a.mu.Lock()
	wasCancelled := h != nil && h.cancelled
	reason := ""
	if wasCancelled {
		reason = h.cancelReason
	}
	a.mu.Unlock()
	if wasCancelled {
		h.emit(Event{
			Type:      EventCancelled,
			ActorID:   a.id,
			Timestamp: a.now(),
			Error:     reason,
			Err:       ErrTurnCancelled,
		})
		a.finishTurn(h, ErrTurnCancelled)
		return
	}

	turnCtx, turnCancel := context.WithCancel(a.ctx)
	a.mu.Lock()
	a.current = h
	a.turnCancel = turnCancel
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		a.current = nil
		a.turnCancel = nil
		a.stepCancel = nil
		a.mu.Unlock()
		turnCancel()
	}()

	if err := a.fsm.Transition(StateRunning, ""); err != nil {
		a.emitError(h, err)
		a.finishTurn(h, err)
		return
	}

	h.emit(Event{
		Type:      EventTurnStarted,
		ActorID:   a.id,
		Timestamp: startTime,
	})

	turnRecord := &TurnRecord{
		ID:        fmt.Sprintf("turn_%d", a.trajectory.TotalTurns()+1),
		StartStep: a.trajectory.TotalSteps(),
		Model:     cfg.Model,
		CreatedAt: startTime,
		Input:     input,
		Steps:     make([]Step, 0),
		Status:    "running",
	}
	// Stage the turn before the first model call: a host that snapshots the
	// trajectory while it runs sees the in-flight record, not nothing.
	a.trajectory.beginTurn(turnRecord)

	var finalOutput strings.Builder
	var turnUsage TokenUsage
	stepIndex := 0
	autoContinueDepth := 0
	turnStatus := "done"
	var turnErr error

	// Run steps until the model completes without tool calls.
	for {
		select {
		case <-turnCtx.Done():
			turnStatus = "cancelled"
			turnErr = ErrTurnCancelled
		case <-a.ctx.Done():
			turnStatus = "cancelled"
			turnErr = ErrActorClosed
		default:
		}

		if turnErr != nil {
			break
		}

		// Drain pending steer instructions for this step.
		a.mu.Lock()
		steerInstructions := a.pendingSteer
		a.pendingSteer = nil
		a.mu.Unlock()

		if len(steerInstructions) > 0 {
			turnRecord.Steers = append(turnRecord.Steers, steerInstructions...)
			for _, instr := range steerInstructions {
				h.emit(Event{
					Type:             EventSteerApplied,
					ActorID:          a.id,
					Timestamp:        a.now(),
					SteerInstruction: instr,
					Metadata: map[string]any{
						"step_index": stepIndex,
					},
				})
			}
		}

		// Ask the host whether the next call needs a fresh context. The
		// callback only inspects; the actor applies its decision below.
		if cfg.Checkpoint != nil {
			probe := CheckpointProbe{
				TurnID:          turnRecord.ID,
				StepIndex:       stepIndex,
				GlobalStepIndex: turnRecord.StartStep + len(turnRecord.Steps),
				Model:           cfg.Model,
				LastStep:        lastStepCopy(turnRecord.Steps),
				TurnUsage:       turnUsage,
			}
			decision, cpErr := cfg.Checkpoint(turnCtx, probe)
			switch {
			case cpErr != nil && (turnCtx.Err() != nil || errors.Is(cpErr, context.Canceled)):
				turnStatus = "cancelled"
				turnErr = ErrTurnCancelled
			case cpErr != nil && errors.Is(cpErr, ErrCheckpointRequired):
				turnStatus = "error"
				turnErr = cpErr
				a.emitError(h, cpErr)
			case cpErr != nil:
				// A soft checkpoint failure leaves the full context in place;
				// the next call may still fit, and a hard failure will surface
				// as ErrCheckpointRequired.
			case decision.Apply && decision.ResumeText != "":
				cp := a.trajectory.recordCheckpoint(Checkpoint{
					TurnID:              turnRecord.ID,
					IncludedThroughStep: decision.IncludedThroughStep,
					KeepRecent:          decision.KeepRecent,
					KeepInput:           decision.KeepInput,
					ResumeText:          decision.ResumeText,
					Summary:             decision.Summary,
					Model:               decision.Model,
					Strategy:            decision.Strategy,
					TokensBefore:        decision.TokensBefore,
					TokensAfter:         decision.TokensAfter,
					StepsSummarized:     decision.StepsSummarized,
					Sync:                decision.Sync,
				})
				h.emit(Event{
					Type:       EventCheckpoint,
					ActorID:    a.id,
					Timestamp:  a.now(),
					Checkpoint: &cp,
				})
			}
			if turnErr != nil {
				break
			}
		}

		// Build conversation messages for the model. The staged turn is
		// excluded from the trajectory history; it is added below.
		history := a.trajectory.ToConversationMessages()
		from, hasCheckpoint, keepInput := a.trajectory.ContextFor(turnRecord.ID)
		history = append(history, a.buildCurrentTurnMessages(input, turnRecord.Steps, steerInstructions, from, hasCheckpoint, keepInput, turnRecord.StartStep)...)

		req := GenerateRequest{
			Model:        cfg.Model,
			SystemPrompt: cfg.SystemPrompt,
			Messages:     history,
			Tools:        cfg.tools.Definitions(),
			Temperature:  cfg.Temperature,
			MaxTokens:    cfg.MaxTokens,
		}

		stepCtx, stepCancel := context.WithCancel(turnCtx)
		a.mu.Lock()
		a.stepCancel = stepCancel
		a.mu.Unlock()

		provider := cfg.Provider
		var stream ResponseStream
		var err error
		for attempt := 0; attempt < 3; attempt++ {
			stream, err = provider.Generate(stepCtx, req)
			if err == nil {
				break
			}
			if !isTransientNetworkError(err) || turnCtx.Err() != nil {
				break
			}
			select {
			case <-turnCtx.Done():
			case <-time.After(time.Duration(100*(1<<attempt)) * time.Millisecond):
			}
		}
		if err != nil {
			a.mu.Lock()
			a.stepCancel = nil
			a.mu.Unlock()
			stepCancel()

			switch {
			case turnCtx.Err() != nil:
				turnStatus = "cancelled"
				turnErr = ErrTurnCancelled
			case isTransientNetworkError(err) && autoContinueDepth < AutoContMaxDepth:
				// The reference recovers a transport interruption with the
				// same recovery copy, on the same turn.
				autoContinueDepth++
				a.injectRecovery()
				stepIndex++
				continue
			case isTransientNetworkError(err):
				turnStatus = "error"
				turnErr = fmt.Errorf("%w after %d attempts: %v", ErrAutoContinueLimit, autoContinueDepth, err)
				a.emitError(h, turnErr)
			default:
				turnStatus = "error"
				turnErr = err
				a.emitError(h, err)
			}
			break
		}

		step := Step{
			Index:       stepIndex,
			GlobalIndex: turnRecord.StartStep + len(turnRecord.Steps),
			Status:      StepRunning,
			Steer:       steerInstructions,
			ToolCalls:   make([]ToolCall, 0),
			ToolResults: make([]ToolResult, 0),
			StartedAt:   a.now(),
		}
		turnRecord.Steps = append(turnRecord.Steps, step)
		stepIdx := len(turnRecord.Steps) - 1
		a.trajectory.updateTurn(turnRecord)

		var stepText strings.Builder
		var stepThought strings.Builder
		var stepUsage TokenUsage
		var finishReason string
		interrupted := false

		// Stream chunks.
		for {
			chunk, rErr := stream.Recv()
			if rErr == io.EOF {
				break
			}
			if rErr != nil {
				switch {
				case turnCtx.Err() != nil:
					turnStatus = "cancelled"
					turnErr = ErrTurnCancelled
				case stepCtx.Err() != nil:
					// Step generation was interrupted by dynamic steering.
				case isTransientNetworkError(rErr):
					interrupted = true
				default:
					turnStatus = "error"
					turnErr = rErr
					a.emitError(h, rErr)
				}
				break
			}

			if chunk.FinishReason != "" {
				finishReason = chunk.FinishReason
			}

			if chunk.Thought != "" {
				stepThought.WriteString(chunk.Thought)
				h.emit(Event{
					Type:      EventThought,
					ActorID:   a.id,
					Timestamp: a.now(),
					Thought:   chunk.Thought,
				})
			}

			if chunk.Delta != "" {
				stepText.WriteString(chunk.Delta)
				h.emit(Event{
					Type:      EventDelta,
					ActorID:   a.id,
					Timestamp: a.now(),
					Delta:     chunk.Delta,
				})
			}

			if len(chunk.ToolCalls) > 0 {
				step.ToolCalls = append(step.ToolCalls, chunk.ToolCalls...)
			}

			if chunk.Usage != nil {
				stepUsage.Add(chunk.Usage)
				turnUsage.Add(chunk.Usage)
				h.emit(Event{
					Type:      EventUsage,
					ActorID:   a.id,
					Timestamp: a.now(),
					Usage:     chunk.Usage,
				})
			}
		}
		_ = stream.Close()
		a.mu.Lock()
		a.stepCancel = nil
		hasPendingSteer := len(a.pendingSteer) > 0
		a.mu.Unlock()
		stepCancel()

		step.Delta = stepText.String()
		step.Thought = stepThought.String()

		// A transport interruption that the reference would recover: keep
		// the partial step (so the model and the user see the same text),
		// then continue the same turn with the recovery instruction.
		if interrupted && turnErr == nil {
			step.Status = StepInterrupted
			step.FinishReason = firstNonEmpty(finishReason, StepInterrupted)
			step.CompletedAt = a.now()
			step.Usage = tokenUsagePtr(stepUsage)
			step = pairMissingCalls(step)
			turnRecord.Steps[stepIdx] = step
			a.trajectory.updateTurn(turnRecord)
			finalOutput.WriteString(step.Delta)

			autoContinueDepth++
			a.injectRecovery()
			stepIndex++
			continue
		}

		if turnErr != nil {
			// Keep what the user already saw. A step that produced nothing
			// (for example a non-transient Generate failure) is dropped.
			step.Status = stepStatusFor(turnStatus)
			step.FinishReason = firstNonEmpty(finishReason, step.Status)
			step.CompletedAt = a.now()
			step.Usage = tokenUsagePtr(stepUsage)
			if hasStepContent(step) {
				step = pairMissingCalls(step)
				turnRecord.Steps[stepIdx] = step
				finalOutput.WriteString(step.Delta)
			} else {
				turnRecord.Steps = turnRecord.Steps[:stepIdx]
			}
			a.trajectory.updateTurn(turnRecord)
			break
		}

		// If no tool calls, check whether to continue due to steer guidance or auto-continue.
		if len(step.ToolCalls) == 0 {
			finalOutput.WriteString(step.Delta)
			step.Status = StepDone
			step.FinishReason = firstNonEmpty(finishReason, "stop")
			step.CompletedAt = a.now()
			step.Usage = tokenUsagePtr(stepUsage)
			turnRecord.Steps[stepIdx] = step
			a.trajectory.updateTurn(turnRecord)

			// 1. In-flight steering guidance takes priority.
			if hasPendingSteer {
				stepIndex++
				continue
			}

			// 2. Auto-continue if generation was truncated by length limit.
			isTruncated := finishReason == "length" || finishReason == "MAX_TOKENS"
			if isTruncated && a.config.AutoContinueEnabled() {
				if autoContinueDepth >= AutoContMaxDepth {
					turnStatus = "error"
					turnErr = fmt.Errorf("%w: model output kept truncating", ErrAutoContinueLimit)
					break
				}
				autoContinueDepth++
				a.injectRecovery()
				stepIndex++
				continue
			}

			break
		}

		// Tool calls need execution.
		for _, call := range step.ToolCalls {
			if turnCtx.Err() != nil {
				turnStatus = "cancelled"
				turnErr = ErrTurnCancelled
				break
			}
			h.emit(Event{
				Type:      EventToolCallStarted,
				ActorID:   a.id,
				Timestamp: a.now(),
				ToolCall:  &call,
			})

			tool, exists := cfg.tools.Get(call.Name)
			if !exists {
				res := ToolResult{
					ToolCallID: call.ID,
					Error:      fmt.Sprintf("tool %q not registered", call.Name),
				}
				step.ToolResults = append(step.ToolResults, res)
				h.emit(Event{
					Type:       EventToolCallFinished,
					ActorID:    a.id,
					Timestamp:  a.now(),
					ToolCall:   &call,
					ToolResult: &res,
				})
				continue
			}

			// Check approval requirement.
			if tool.RequiresApproval(call.Input) {
				if err := a.fsm.Transition(StateWaitingApproval, call.ID); err != nil {
					turnStatus = "error"
					turnErr = err
					break
				}

				// Drop anything a previous wait left behind. A duplicate
				// Approve can only target the call that is waiting now (the
				// state check rejects everything else), so this entry drain
				// makes a stale decision impossible to consume.
				select {
				case <-a.approvalWake:
				default:
				}
				a.mu.Lock()
				a.pendingApproval = nil
				a.mu.Unlock()

				h.emit(Event{
					Type:      EventApprovalRequired,
					ActorID:   a.id,
					Timestamp: a.now(),
					ToolCall:  &call,
				})

				// Wait for approval decision, cancellation or actor shutdown.
				var approved bool
				var feedback string
				select {
				case <-turnCtx.Done():
					turnStatus = "cancelled"
					turnErr = ErrTurnCancelled
				case <-a.ctx.Done():
					turnStatus = "cancelled"
					turnErr = ErrActorClosed
				case <-a.approvalWake:
					a.mu.Lock()
					decision := a.pendingApproval
					a.pendingApproval = nil
					a.mu.Unlock()
					if decision != nil && decision.toolCallID == call.ID {
						approved = decision.approved
						feedback = decision.feedback
					} else {
						// A decision for another call is never applied.
						approved = false
						feedback = "approval id mismatch"
					}
				}

				if turnErr != nil {
					break
				}

				// Transition back to running.
				_ = a.fsm.Transition(StateRunning, "")

				if !approved {
					res := ToolResult{
						ToolCallID: call.ID,
						Error:      fmt.Sprintf("execution denied by user: %s", feedback),
					}
					step.ToolResults = append(step.ToolResults, res)
					h.emit(Event{
						Type:       EventToolCallFinished,
						ActorID:    a.id,
						Timestamp:  a.now(),
						ToolCall:   &call,
						ToolResult: &res,
					})
					continue
				}
			}

			// Execute tool.
			callCtx := ContextWithToolCallID(turnCtx, call.ID)
			callCtx = ContextWithToolStepIndex(callCtx, step.GlobalIndex)
			res, execErr := tool.Execute(callCtx, call.Input)
			res.ToolCallID = call.ID
			if execErr != nil && res.Error == "" {
				res.Error = execErr.Error()
			}

			step.ToolResults = append(step.ToolResults, res)
			h.emit(Event{
				Type:       EventToolCallFinished,
				ActorID:    a.id,
				Timestamp:  a.now(),
				ToolCall:   &call,
				ToolResult: &res,
			})
		}

		step.Status = StepDone
		if turnErr != nil {
			step.Status = stepStatusFor(turnStatus)
		}
		step.FinishReason = firstNonEmpty(finishReason, step.Status)
		step.CompletedAt = a.now()
		step.Usage = tokenUsagePtr(stepUsage)
		step = pairMissingCalls(step)
		turnRecord.Steps[stepIdx] = step
		a.trajectory.updateTurn(turnRecord)

		if turnErr != nil {
			break
		}

		hasTerminate := false
		for _, res := range step.ToolResults {
			if res.Terminate {
				hasTerminate = true
				break
			}
		}
		if hasTerminate {
			break
		}

		stepIndex++
	}

	// Finalize turn record.
	completedAt := a.now()
	turnRecord.CompletedAt = completedAt
	turnRecord.FinalOutput = finalOutput.String()
	turnRecord.Status = turnStatus
	if turnErr != nil {
		turnRecord.Error = turnErr.Error()
	}
	turnRecord.Usage = turnUsage

	a.trajectory.completeTurn(turnRecord)

	// Transition back to idle.
	_ = a.fsm.Transition(StateIdle, "")

	summary := &TurnSummary{
		Status:         turnStatus,
		Output:         turnRecord.FinalOutput,
		StepsCount:     len(turnRecord.Steps),
		Duration:       completedAt.Sub(startTime),
		TotalTokens:    turnUsage.TotalTokens,
		PromptTokens:   turnUsage.PromptTokens,
		OutputTokens:   turnUsage.OutputTokens,
		ThoughtsTokens: turnUsage.ThoughtsTokens,
	}

	evType := EventTurnCompleted
	if turnStatus == "cancelled" {
		evType = EventCancelled
	}

	h.emit(Event{
		Type:        evType,
		ActorID:     a.id,
		Timestamp:   completedAt,
		TurnSummary: summary,
		Error:       turnRecord.Error,
		Err:         turnErr,
	})

	a.finishTurn(h, turnErr)
}

// AutoContMaxDepth bounds how many times a turn recovers from a truncated or
// interrupted generation. The reference uses the same ceiling (3): after that
// the turn fails and the user decides what to do.
const AutoContMaxDepth = 3

// injectRecovery queues the reference's verbatim recovery instruction as
// steering guidance for the next step.
func (a *Actor) injectRecovery() {
	a.mu.Lock()
	a.pendingSteer = append(a.pendingSteer, StreamInterruptedMessage)
	a.mu.Unlock()
}

// hasStepContent reports whether a partial step carries anything the user saw.
func hasStepContent(step Step) bool {
	return step.Delta != "" || step.Thought != "" || len(step.ToolCalls) > 0
}

// stepStatusFor maps a turn outcome to the step status of a partial step.
func stepStatusFor(turnStatus string) string {
	if turnStatus == "cancelled" {
		return StepCancelled
	}
	return StepError
}

// firstNonEmpty returns a when set, else b.
func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// tokenUsagePtr copies a step's own usage; a zero count is nil.
func tokenUsagePtr(u TokenUsage) *TokenUsage {
	if u == (TokenUsage{}) {
		return nil
	}
	cp := u
	return &cp
}

// pairMissingCalls adds a synthetic result for every tool call that was
// streamed but never executed, so the conversation never carries an assistant
// tool call without its result.
func pairMissingCalls(step Step) Step {
	if len(step.ToolCalls) == 0 {
		return step
	}
	done := make(map[string]bool, len(step.ToolResults))
	for _, res := range step.ToolResults {
		done[res.ToolCallID] = true
	}
	for _, call := range step.ToolCalls {
		if done[call.ID] {
			continue
		}
		step.ToolResults = append(step.ToolResults, ToolResult{
			ToolCallID: call.ID,
			Error:      "not executed: step " + step.Status,
		})
	}
	return step
}

// finishTurn removes the turn from the actor's bookkeeping and closes its
// handle. The loop is the only caller (or StartTurn when the turn never
// started), so the handle is finished exactly once.
func (a *Actor) finishTurn(h *TurnHandle, err error) {
	if h == nil {
		return
	}
	a.mu.Lock()
	delete(a.pending, h)
	a.mu.Unlock()
	h.complete(err)
}

func (a *Actor) buildCurrentTurnMessages(input string, steps []Step, steer []string, from int, hasCheckpoint, keepInput bool, turnStart int) []ConversationMessage {
	var msgs []ConversationMessage

	includeInput := !hasCheckpoint || keepInput
	if len(steer) > 0 && len(steps) == 0 && includeInput {
		input += "\n\n[Active Guidance]: " + strings.Join(steer, "; ")
	}

	if includeInput {
		msgs = append(msgs, ConversationMessage{
			Role:    RoleUser,
			Content: input,
		})
	}

	for _, step := range steps {
		if hasCheckpoint && step.GlobalIndex <= from {
			continue
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
	}

	if len(steer) > 0 && (includeInput || len(msgs) > 0) {
		msgs = append(msgs, ConversationMessage{
			Role:    RoleUser,
			Content: "[Active Guidance]: " + strings.Join(steer, "; "),
		})
	}

	return msgs
}

// lastStepCopy returns a copy of the most recent step, or nil.
func lastStepCopy(steps []Step) *Step {
	if len(steps) == 0 {
		return nil
	}
	cp := steps[len(steps)-1]
	return &cp
}

func (a *Actor) emitError(h *TurnHandle, err error) {
	if err == nil {
		return
	}
	h.emit(Event{
		Type:      EventError,
		ActorID:   a.id,
		Timestamp: a.now(),
		Error:     err.Error(),
		Err:       err,
	})
}
