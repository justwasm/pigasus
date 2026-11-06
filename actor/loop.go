package actor

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

func (a *Actor) loop() {
	defer close(a.done)

	for {
		select {
		case <-a.ctx.Done():
			return

		case msg, ok := <-a.mailbox:
			if !ok {
				return
			}
			a.handleMessage(msg)
		}
	}
}

func (a *Actor) handleMessage(msg Message) {
	switch m := msg.(type) {
	case *CancelSignal:
		_ = a.CancelTurn(m.Reason)

	case *SystemPromptUpdate:
		a.SetSystemPrompt(m.SystemPrompt)

	case *SteerMessage:
		_ = a.Steer(m.Instruction)

	case *ApprovalDecision:
		select {
		case a.approvalCh <- m:
		default:
		}

	case *UserMessage:
		a.executeTurn(m)

	case *ScheduledWakeup:
		a.executeTurn(m)

	default:
		// Unsupported or custom message ignored or could be stored
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

func (a *Actor) executeTurn(input Message) {
	turnID := newID("turn")
	startTime := time.Now()

	turnCtx, turnCancel := context.WithCancel(a.ctx)
	a.mu.Lock()
	a.turnCancel = turnCancel
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		a.turnCancel = nil
		a.stepCancel = nil
		a.mu.Unlock()
		turnCancel()
	}()

	if err := a.fsm.Transition(StateRunning, turnID, ""); err != nil {
		a.emitError(turnID, err.Error())
		return
	}

	a.events.Publish(Event{
		Type:      EventTurnStarted,
		ActorID:   a.id,
		TurnID:    turnID,
		Timestamp: startTime,
		Metadata: map[string]any{
			"message_type": input.MessageType(),
			"message_id":   input.ID(),
		},
	})

	turnRecord := &TurnRecord{
		ID:        turnID,
		CreatedAt: startTime,
		Input:     input,
		Steps:     make([]Step, 0),
		Status:    "working",
	}

	var finalOutput strings.Builder
	var turnUsage TokenUsage
	stepIndex := 0
	turnStatus := "done"
	var turnErr error

	// Run turns iteratively until model completes without tool calls or limit reached
	for a.config.MaxStepsPerTurn <= 0 || stepIndex < a.config.MaxStepsPerTurn {
		select {
		case <-turnCtx.Done():
			turnStatus = "cancelled"
			turnErr = ErrTurnCancelled
			break
		case <-a.ctx.Done():
			turnStatus = "cancelled"
			turnErr = ErrActorClosed
			break
		default:
		}

		if turnErr != nil {
			break
		}

		// Drain pending steer instructions for this step
		a.mu.Lock()
		steerInstructions := a.pendingSteer
		a.pendingSteer = nil
		a.mu.Unlock()

		if len(steerInstructions) > 0 {
			turnRecord.Steers = append(turnRecord.Steers, steerInstructions...)
			for _, instr := range steerInstructions {
				a.events.Publish(Event{
					Type:             EventSteerApplied,
					ActorID:          a.id,
					TurnID:           turnID,
					Timestamp:        time.Now(),
					SteerInstruction: instr,
					Metadata: map[string]any{
						"step_index": stepIndex,
					},
				})
			}
		}

		// Build conversation messages for model
		history := a.trajectory.ToConversationMessages()

		// Append intermediate steps of the current turn
		currentTurnMsgs := a.buildCurrentTurnMessages(input, turnRecord.Steps, steerInstructions)
		history = append(history, currentTurnMsgs...)

		// Call Provider
		req := GenerateRequest{
			Model:        a.Model(),
			SystemPrompt: a.SystemPrompt(),
			Messages:     history,
			Tools:        a.tools.Definitions(),
			Temperature:  a.config.Temperature,
			MaxTokens:    a.config.MaxTokens,
		}

		stepCtx, stepCancel := context.WithCancel(turnCtx)
		a.mu.Lock()
		a.stepCancel = stepCancel
		a.mu.Unlock()

		provider := a.Provider()
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
				break
			case <-time.After(time.Duration(100*(1<<attempt)) * time.Millisecond):
			}
		}
		if err != nil {
			a.mu.Lock()
			a.stepCancel = nil
			a.mu.Unlock()
			stepCancel()
			if turnCtx.Err() != nil {
				turnStatus = "cancelled"
				turnErr = ErrTurnCancelled
			} else {
				turnStatus = "error"
				turnErr = err
				a.emitError(turnID, fmt.Sprintf("provider generate failed: %v", err))
			}
			break
		}

		step := Step{
			Index:       stepIndex,
			Steer:       steerInstructions,
			ToolCalls:   make([]ToolCall, 0),
			ToolResults: make([]ToolResult, 0),
		}

		var stepText strings.Builder
		var stepThought strings.Builder
		var finishReason string

		// Stream chunks
		for {
			chunk, rErr := stream.Recv()
			if rErr == io.EOF {
				break
			}
			if rErr != nil {
				if turnCtx.Err() != nil {
					turnStatus = "cancelled"
					turnErr = ErrTurnCancelled
					break
				}
				if stepCtx.Err() != nil {
					// Step generation was interrupted by dynamic steering
					break
				}
				turnStatus = "error"
				turnErr = rErr
				a.emitError(turnID, fmt.Sprintf("stream recv error: %v", rErr))
				break
			}

			if chunk.FinishReason != "" {
				finishReason = chunk.FinishReason
			}

			if chunk.Thought != "" {
				stepThought.WriteString(chunk.Thought)
				a.events.Publish(Event{
					Type:      EventThought,
					ActorID:   a.id,
					TurnID:    turnID,
					Timestamp: time.Now(),
					Thought:   chunk.Thought,
				})
			}

			if chunk.Delta != "" {
				stepText.WriteString(chunk.Delta)
				a.events.Publish(Event{
					Type:      EventDelta,
					ActorID:   a.id,
					TurnID:    turnID,
					Timestamp: time.Now(),
					Delta:     chunk.Delta,
				})
			}

			if len(chunk.ToolCalls) > 0 {
				step.ToolCalls = append(step.ToolCalls, chunk.ToolCalls...)
			}

			if chunk.Usage != nil {
				turnUsage.Add(chunk.Usage)
				a.events.Publish(Event{
					Type:      EventUsage,
					ActorID:   a.id,
					TurnID:    turnID,
					Timestamp: time.Now(),
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

		if turnErr != nil {
			break
		}

		step.Delta = stepText.String()
		step.Thought = stepThought.String()

		// If no tool calls, check if we should continue due to steer guidance or auto-continue
		if len(step.ToolCalls) == 0 {
			finalOutput.WriteString(step.Delta)
			step.Usage = &turnUsage
			turnRecord.Steps = append(turnRecord.Steps, step)

			// 1. In-flight steering guidance takes priority
			if hasPendingSteer && (a.config.MaxStepsPerTurn <= 0 || stepIndex+1 < a.config.MaxStepsPerTurn) {
				stepIndex++
				continue
			}

			// 2. Auto-continue if generation was truncated by length limit
			isTruncated := finishReason == "length" || finishReason == "MAX_TOKENS"
			if isTruncated && a.config.AutoContinueEnabled() && (a.config.MaxStepsPerTurn <= 0 || stepIndex+1 < a.config.MaxStepsPerTurn) {
				a.mu.Lock()
				a.pendingSteer = append(a.pendingSteer, StreamInterruptedMessage)
				a.mu.Unlock()
				stepIndex++
				continue
			}

			break
		}

		// Tool calls need execution
		for _, call := range step.ToolCalls {
			if turnCtx.Err() != nil {
				turnStatus = "cancelled"
				turnErr = ErrTurnCancelled
				break
			}
			a.events.Publish(Event{
				Type:      EventToolCallStarted,
				ActorID:   a.id,
				TurnID:    turnID,
				Timestamp: time.Now(),
				ToolCall:  &call,
			})

			tool, exists := a.tools.Get(call.Name)
			if !exists {
				res := ToolResult{
					ToolCallID: call.ID,
					Error:      fmt.Sprintf("tool %q not registered", call.Name),
				}
				step.ToolResults = append(step.ToolResults, res)
				a.events.Publish(Event{
					Type:       EventToolCallFinished,
					ActorID:    a.id,
					TurnID:     turnID,
					Timestamp:  time.Now(),
					ToolCall:   &call,
					ToolResult: &res,
				})
				continue
			}

			// Check approval requirement
			if tool.RequiresApproval(call.Input) {
				if err := a.fsm.Transition(StateWaitingApproval, turnID, call.ID); err != nil {
					turnStatus = "error"
					turnErr = err
					break
				}

				a.events.Publish(Event{
					Type:      EventApprovalRequired,
					ActorID:   a.id,
					TurnID:    turnID,
					Timestamp: time.Now(),
					ToolCall:  &call,
				})

				// Wait for approval decision, cancellation or actor shutdown
				var approved bool
				var feedback string
				select {
				case <-turnCtx.Done():
					turnStatus = "cancelled"
					turnErr = ErrTurnCancelled
					break
				case <-a.ctx.Done():
					turnStatus = "cancelled"
					turnErr = ErrActorClosed
					break
				case decision := <-a.approvalCh:
					if decision.ToolCallID == call.ID {
						approved = decision.Approved
						feedback = decision.Feedback
					} else {
						// Mismatched ID, default reject
						approved = false
						feedback = "approval id mismatch"
					}
				}

				if turnErr != nil {
					break
				}

				// Transition back to running
				_ = a.fsm.Transition(StateRunning, turnID, "")

				if !approved {
					res := ToolResult{
						ToolCallID: call.ID,
						Error:      fmt.Sprintf("execution denied by user: %s", feedback),
					}
					step.ToolResults = append(step.ToolResults, res)
					a.events.Publish(Event{
						Type:       EventToolCallFinished,
						ActorID:    a.id,
						TurnID:     turnID,
						Timestamp:  time.Now(),
						ToolCall:   &call,
						ToolResult: &res,
					})
					continue
				}
			}

			// Execute tool
			callCtx := ContextWithToolCallID(turnCtx, call.ID)
			res, execErr := tool.Execute(callCtx, call.Input)
			res.ToolCallID = call.ID
			if execErr != nil && res.Error == "" {
				res.Error = execErr.Error()
			}

			step.ToolResults = append(step.ToolResults, res)
			a.events.Publish(Event{
				Type:       EventToolCallFinished,
				ActorID:    a.id,
				TurnID:     turnID,
				Timestamp:  time.Now(),
				ToolCall:   &call,
				ToolResult: &res,
			})
		}

		turnRecord.Steps = append(turnRecord.Steps, step)
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

	if turnErr == nil && a.config.MaxStepsPerTurn > 0 && stepIndex >= a.config.MaxStepsPerTurn && len(turnRecord.Steps) > 0 && len(turnRecord.Steps[len(turnRecord.Steps)-1].ToolCalls) > 0 {
		turnStatus = "error"
		turnErr = ErrMaxStepsReached
	}

	// Finalize turn record
	completedAt := time.Now()
	turnRecord.CompletedAt = completedAt
	turnRecord.FinalOutput = finalOutput.String()
	turnRecord.Status = turnStatus
	if turnErr != nil {
		turnRecord.Error = turnErr.Error()
	}
	turnRecord.Usage = turnUsage

	a.trajectory.AddTurn(turnRecord)

	// Check auto-compaction
	if a.config.AutoCompactThreshold > 0 && a.trajectory.TotalSteps() > a.config.AutoCompactThreshold {
		compactedCount := a.trajectory.Compact(3)
		if compactedCount > 0 {
			a.events.Publish(Event{
				Type:      EventCompacted,
				ActorID:   a.id,
				TurnID:    turnID,
				Timestamp: time.Now(),
				Metadata: map[string]any{
					"compacted_turns": compactedCount,
				},
			})
		}
	}

	// Transition back to idle
	_ = a.fsm.Transition(StateIdle, "", "")

	// Publish turn completed event
	summary := &TurnSummary{
		TurnID:       turnID,
		Status:       turnStatus,
		Output:       turnRecord.FinalOutput,
		StepsCount:   len(turnRecord.Steps),
		Duration:     completedAt.Sub(startTime),
		TotalTokens:  turnUsage.TotalTokens,
		PromptTokens: turnUsage.PromptTokens,
		OutputTokens: turnUsage.OutputTokens,
	}

	evType := EventTurnCompleted
	if turnStatus == "cancelled" {
		evType = EventCancelled
	}

	a.events.Publish(Event{
		Type:        evType,
		ActorID:     a.id,
		TurnID:      turnID,
		Timestamp:   completedAt,
		TurnSummary: summary,
		Error:       turnRecord.Error,
	})
}

func (a *Actor) buildCurrentTurnMessages(input Message, steps []Step, steer []string) []ConversationMessage {
	var msgs []ConversationMessage

	var inputContent string
	if u, ok := input.(*UserMessage); ok {
		inputContent = u.Text
	} else if s, ok := input.(*ScheduledWakeup); ok {
		inputContent = fmt.Sprintf("[Scheduled wakeup: %s]\n%s", s.Source, s.Prompt)
	} else if st, ok := input.(*SteerMessage); ok {
		inputContent = fmt.Sprintf("[Steer guidance]: %s", st.Instruction)
	}

	if len(steer) > 0 && len(steps) == 0 {
		inputContent += "\n\n[Active Guidance]: " + strings.Join(steer, "; ")
	}

	msgs = append(msgs, ConversationMessage{
		Role:    RoleUser,
		Content: inputContent,
	})

	for _, step := range steps {
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

	if len(steer) > 0 && len(steps) > 0 {
		msgs = append(msgs, ConversationMessage{
			Role:    RoleUser,
			Content: "[Active Guidance]: " + strings.Join(steer, "; "),
		})
	}

	return msgs
}

func (a *Actor) emitError(turnID, errText string) {
	a.events.Publish(Event{
		Type:      EventError,
		ActorID:   a.id,
		TurnID:    turnID,
		Timestamp: time.Now(),
		Error:     errText,
	})
}
