package subagenttools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/justwasm/pigasus/actor"
	taskregistry "github.com/justwasm/pigasus/internal/app/task"
	"github.com/justwasm/pigasus/internal/app/tools"
	"github.com/justwasm/pigasus/internal/app/tools/tasks"
	agentmessage "github.com/justwasm/pigasus/internal/core/msg"
	"github.com/justwasm/pigasus/internal/features/checkpoint"
)

// taskInstruction is the reference's own instruction to a delegated agent.
const taskInstruction = "Your task arrives as a system message from the agent that invoked you. " +
	"Treat that message exactly as if the user had sent it to you directly: it is the user's complete request, " +
	"and you respond to it as you would to the user."

// idleNotice is what the parent is handed when a subagent ends without reporting anything back.
const idleNotice = "Subagent %s went idle without reporting anything back. " +
	"Check its work, or send it a message at that id asking it to report back."

// Launched is the result of a background launch.
type Launched struct {
	Acknowledgement Acknowledgement
	Child           *Child
}

// Launch starts a delegation in the background and returns the reference's launch acknowledgement.
func Launch(ctx context.Context, inv Invocation, deps Deps) (Launched, error) {
	if strings.TrimSpace(inv.Prompt) == "" {
		return Launched{}, errors.New("subagenttools: Invocation.Prompt is required")
	}
	model := invModel(inv, deps)
	provider := deps.Provider
	if provider == nil {
		return Launched{}, errors.New("subagenttools: Deps.Provider is required")
	}

	agentDir := deps.AgentDir
	if agentDir == "" {
		agentDir = filepath.Join(os.TempDir(), "subagents")
	}

	systemPrompt := inv.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = deps.parentPrompt()
	}

	conversationID := newConversationID()
	subDir := filepath.Join(agentDir, "subagents")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		subDir = ""
	}

	cwd, worktreePath := resolveSubagentCWD(agentDir, conversationID, inv.Worktree, deps)
	startedAt := time.Now()

	childCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	child := NewChild(conversationID, inv.TypeName, inv.Role, worktreePath, cancel)
	child.ParentID = deps.ConversationID
	if deps.Registry != nil && deps.Registry.ParentID() != "" {
		child.ParentID = deps.Registry.ParentID()
	}
	child.Model = model
	child.Effort = strings.TrimSpace(deps.Effort)
	child.SetSubDir(subDir)
	child.StartedAt = startedAt
	if idx, ok := actor.ToolStepIndexFromContext(ctx); ok {
		child.SpawnStepIndex = int32(idx)
	}

	ack := Acknowledgement{
		ConversationID: conversationID,
		LogAbsoluteURI: fileURI(filepath.Join(subDir, conversationID+".jsonl")),
		WorkspaceURIs:  []string{fileURI(worktreePath)},
		At:             startedAt,
	}
	child.LogAbsoluteURI = ack.LogAbsoluteURI

	if deps.Registry != nil {
		deps.Registry.Add(child)
	}
	if deps.Tasks != nil {
		taskName := subagentDisplayName(inv)
		deps.Tasks.Register(&taskregistry.Task{
			ID:             conversationID,
			Name:           taskName,
			Kind:           taskregistry.KindSubagent,
			State:          taskregistry.StateRunning,
			StartedAt:      startedAt,
			LastActivityAt: startedAt,
			Prompt:         inv.Prompt,
			Model:          model,
			Effort:         strings.TrimSpace(deps.Effort),
		})
		deps.Tasks.RegisterHandle(conversationID, &taskregistry.TaskHandle{
			ID:     conversationID,
			Cancel: cancel,
			Abort:  func() { child.Stop() },
			Resume: func(ctx context.Context) error {
				if !child.Revive(ctx, nil) {
					return taskregistry.ErrTaskNotResumable
				}
				return nil
			},
		})
	}

	tools := rebindMessaging(inv.Tools, deps, conversationID)
	if len(tools) == 0 {
		tools = grantedToolSet(deps, deps.Definitions, inv.Grant)
		tools = rebindMessaging(tools, deps, conversationID)
	}
	var gate BeforeToolCallGate
	if deps.PermissionGateFor != nil {
		gate = deps.PermissionGateFor(cwd)
	}
	gatedTools := make([]actor.Tool, 0, len(tools))
	for _, t := range tools {
		if gate != nil {
			gatedTools = append(gatedTools, &gatedTool{inner: t, gate: gate})
		} else {
			gatedTools = append(gatedTools, t)
		}
	}

	launchFn := func(runCtx context.Context, initialMsg *Message) {
		childRunCtx, runCancel := context.WithCancel(context.WithoutCancel(runCtx))
		child.SetCancel(runCancel)
		child.setState(StateRunning)
		go runChild(childRunCtx, child, deps, model, provider, systemPrompt, cwd, worktreePath, subDir, inv, gatedTools, startedAt, initialMsg)
	}
	child.SetLaunch(launchFn)
	launchFn(childCtx, nil)

	return Launched{Acknowledgement: ack, Child: child}, nil
}

// rebindMessaging points the conversation-scoped tools at the child that owns them.
func rebindMessaging(list []actor.Tool, deps Deps, owner string) []actor.Tool {
	out := make([]actor.Tool, 0, len(list)+1)
	hasAskQuestion := false
	for _, t := range list {
		if t == nil {
			continue
		}
		switch t.Name() {
		case "send_message":
			out = append(out, &sendMessageTool{registry: deps.Registry, owner: owner})
		case "manage_subagents":
			out = append(out, &manageSubagentsTool{registry: deps.Registry, owner: owner})
		case "ask_question":
			hasAskQuestion = true
			if deps.Questions != nil {
				qTool := &tasks.AskQuestion{
					Host:              nil,
					SubAgentQuestions: deps.Questions,
					SubAgentID:        owner,
					OnAsk:             deps.OnAsk,
				}
				out = append(out, adaptSubagentTool(qTool))
			} else {
				out = append(out, t)
			}
		default:
			out = append(out, t)
		}
	}
	if !hasAskQuestion && deps.Questions != nil {
		qTool := &tasks.AskQuestion{
			Host:              nil,
			SubAgentQuestions: deps.Questions,
			SubAgentID:        owner,
			OnAsk:             deps.OnAsk,
		}
		out = append(out, adaptSubagentTool(qTool))
	}
	return out
}

func adaptSubagentTool(t tools.Tool) actor.Tool {
	if t == nil {
		return nil
	}
	var params map[string]any
	desc := t.Name()
	if raw := t.Schema(); len(raw) > 0 {
		_ = json.Unmarshal(raw, &params)
		if d, ok := params["description"].(string); ok && d != "" {
			desc = d
		}
	}
	return actor.NewFuncTool(t.Name(), desc, func(ctx context.Context, input string) (string, error) {
		res, err := t.Run(ctx, json.RawMessage(input))
		if err != nil {
			return "", err
		}
		if s, ok := res.(string); ok {
			return s, nil
		}
		b, _ := json.Marshal(res)
		return string(b), nil
	}, actor.WithParameters(params))
}

func runChild(
	ctx context.Context,
	child *Child,
	deps Deps,
	model string,
	provider actor.Provider,
	systemPrompt, cwd, worktreePath, subDir string,
	inv Invocation,
	childTools []actor.Tool,
	startedAt time.Time,
	initialMsg *Message,
) {
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			fmt.Fprintf(os.Stderr, "\n[subagent %s panic]: %v\n%s\n", child.ConversationID, r, string(stack))
			child.setState(StateErrored)
			if deps.Tasks != nil {
				deps.Tasks.Update(child.ConversationID, func(t *taskregistry.Task) {
					t.State = taskregistry.StateFailed
					t.Err = fmt.Errorf("subagent panic: %v", r)
					t.EndedAt = time.Now()
				})
			}
			reportFailure(deps, child, fmt.Errorf("subagent panicked: %v", r))
		}
	}()

	taskReg := deps.Tasks
	if taskReg != nil {
		if taskReg.Get(child.ConversationID) == nil {
			taskName := subagentDisplayName(inv)
			taskReg.Register(&taskregistry.Task{
				ID:        child.ConversationID,
				Name:      taskName,
				Kind:      taskregistry.KindSubagent,
				State:     taskregistry.StateRunning,
				StartedAt: startedAt,
				Prompt:    inv.Prompt,
			})
		} else {
			taskReg.Update(child.ConversationID, func(t *taskregistry.Task) {
				t.State = taskregistry.StateRunning
			})
		}
	}

	parentID := ""
	if deps.Registry != nil {
		parentID = deps.Registry.ParentID()
	}
	if parentID == "" {
		parentID = deps.ConversationID
	}
	pctx := parentContextOf(deps, inv)

	loadedMsgs, _ := loadTranscript(subDir, child.ConversationID)

	childPrompt := childSystemPrompt(systemPrompt, inv)
	act, err := actor.Spawn(actor.Config{
		ID: child.ConversationID,
		Default: actor.TurnConfig{
			SystemPrompt: childPrompt,
			Model:        model,
			Provider:     provider,
			Tools:        childTools,
		},
	})
	if err != nil {
		child.setState(StateErrored)
		if taskReg != nil {
			taskReg.Update(child.ConversationID, func(t *taskregistry.Task) {
				t.State = taskregistry.StateFailed
				t.Err = err
				t.EndedAt = time.Now()
			})
		}
		reportFailure(deps, child, err)
		return
	}
	defer act.Close()

	var policy *checkpoint.Policy
	if deps.Backend != nil {
		var defs []actor.ToolDefinition
		for _, t := range childTools {
			defs = append(defs, t.Definition())
		}
		decls, _ := json.Marshal(defs)
		policy = checkpoint.New(checkpoint.Config{
			Model:            model,
			SystemPrompt:     childPrompt,
			ToolDeclarations: decls,
			Backend:          deps.Backend,
		})
		defer policy.Close()
	}

	watchdog := newActivityWatchdog(DefaultWatchdogTimeout, func() {
		child.abortTurn()
	})
	defer watchdog.Stop()

	defer purgeWorktreeArtifacts(worktreePath, child.ConversationID)

	child.SetAbort(func() {
		_ = act.CancelTurn("aborted")
	})

	executeTurn := func(prompt string) ([]SubagentMessage, error) {
		req := actor.TurnRequest{Prompt: prompt}
		if policy != nil {
			// The parent's half is refreshed before every turn: a message
			// delivered after an earlier checkpoint must appear in the next
			// one.
			lines, mostRecent := pctx.Lines()
			policy.SetSnapshot(checkpoint.Snapshot{
				ParentMessages:           lines,
				ParentMessagesMostRecent: mostRecent,
			})
			req.Checkpoint = func(stepCtx context.Context, probe actor.CheckpointProbe) (actor.CheckpointDecision, error) {
				return policy.BeforeStep(stepCtx, act, probe)
			}
		}
		turn, err := act.StartTurn(ctx, req)
		if err != nil {
			return nil, err
		}
		events := turn.Events()
		var textBuilder strings.Builder
		for ev := range events {
			switch ev.Type {
			case actor.EventDelta:
				if watchdog != nil {
					watchdog.Ping()
				}
				textBuilder.WriteString(ev.Delta)
				if taskReg != nil {
					taskReg.RecordActivity(child.ConversationID)
					taskReg.Update(child.ConversationID, func(t *taskregistry.Task) {
						t.Output = textBuilder.String()
					})
				}
			case actor.EventToolCallStarted:
				if watchdog != nil {
					watchdog.Ping()
				}
				if taskReg != nil {
					taskReg.RecordActivity(child.ConversationID)
					taskReg.Update(child.ConversationID, func(t *taskregistry.Task) {
						if ev.ToolCall != nil {
							t.CurrentTool = ev.ToolCall.Name
							// The trajectory fills while the child
							// works: a detail view opened mid-run
							// shows the steps taken so far instead
							// of "No steps yet". The turn's end
							// replaces the list with usedSteps(msgs),
							// so nothing is counted twice.
							t.Steps = append(t.Steps, ev.ToolCall.Name)
						}
					})
				}
			case actor.EventToolCallFinished:
				if watchdog != nil {
					watchdog.Ping()
				}
				if taskReg != nil {
					taskReg.RecordActivity(child.ConversationID)
					taskReg.Update(child.ConversationID, func(t *taskregistry.Task) {
						t.CurrentTool = ""
					})
				}
			}
		}

		var runErr error
		if watchdog.IsStalled() {
			runErr = ErrConnectionStalled
		}
		lastTurn := act.Trajectory().LastTurn()
		if lastTurn != nil && lastTurn.Error != "" {
			runErr = errors.New(lastTurn.Error)
		}
		msgs := trajectoryToSubagentMessages(act.Trajectory())
		return msgs, runErr
	}

	var msgs []SubagentMessage
	var runErr error

	if initialMsg != nil {
		pctx.Record(initialMsg.Content, initialMsg.At)
		envelope := agentmessage.Render(parentID, initialMsg.Content, initialMsg.At, agentmessage.PriorityHigh)
		msgs, runErr = executeTurn(envelope)
	} else if len(loadedMsgs) == 0 {
		first := agentmessage.Render(parentID, inv.Prompt, time.Now(), agentmessage.PriorityHigh)
		msgs, runErr = executeTurn(first)
	} else {
		msgs = loadedMsgs
	}

	if runErr != nil {
		if watchdog.IsStalled() {
			runErr = ErrConnectionStalled
			child.setState(StateErrored)
		} else if isRecoverableLimitError(runErr) {
			child.setState(StateStopped)
		}
	}

	if child.State() == StateStopped {
		recordChildTurnWithMeta(taskReg, child, msgs, nil, subDir, inv, startedAt)
		if subDir != "" {
			persistTranscript(subDir, child.ConversationID, msgs, nil)
		}
		return
	}

	recordChildTurnWithMeta(taskReg, child, msgs, runErr, subDir, inv, startedAt)
	if subDir != "" {
		persistTranscript(subDir, child.ConversationID, msgs, runErr)
	}

	for {
		reportChildTurn(deps, child, msgs, runErr)
		if runErr != nil {
			return
		}
		child.setState(StateIdle)
		watchdog.Stop()

		msg, ok := child.Wait(ctx)
		if !ok {
			if child.State() == StateStopped {
				recordChildTurnWithMeta(taskReg, child, msgs, nil, subDir, inv, startedAt)
				if subDir != "" {
					persistTranscript(subDir, child.ConversationID, msgs, nil)
				}
			}
			return
		}
		child.setState(StateRunning)
		watchdog = newActivityWatchdog(DefaultWatchdogTimeout, func() {
			child.abortTurn()
		})
		defer watchdog.Stop()

		pctx.Record(msg.Content, msg.At)
		envelope := agentmessage.Render(parentID, msg.Content, msg.At, agentmessage.PriorityHigh)
		msgs, runErr = executeTurn(envelope)
		if runErr != nil {
			if watchdog.IsStalled() {
				runErr = ErrConnectionStalled
				child.setState(StateErrored)
			} else if isRecoverableLimitError(runErr) {
				child.setState(StateStopped)
			}
		}

		if child.State() == StateStopped {
			recordChildTurnWithMeta(taskReg, child, msgs, nil, subDir, inv, startedAt)
			if subDir != "" {
				persistTranscript(subDir, child.ConversationID, msgs, nil)
			}
			return
		}

		recordChildTurnWithMeta(taskReg, child, msgs, runErr, subDir, inv, startedAt)
		if subDir != "" {
			persistTranscript(subDir, child.ConversationID, msgs, runErr)
		}
	}
}

func childSystemPrompt(base string, inv Invocation) string {
	if inv.InheritPrompt {
		return base
	}
	if base == "" {
		return taskInstruction
	}
	return taskInstruction + "\n\n" + base
}

func reportChildTurn(deps Deps, child *Child, msgs []SubagentMessage, runErr error) {
	if deps.Registry == nil {
		return
	}
	if child.Reported() {
		return
	}
	if runErr != nil {
		reportFailure(deps, child, runErr)
		return
	}
	text := assistantText(msgs)
	if strings.TrimSpace(text) == "" {
		text = fmt.Sprintf(idleNotice, child.ConversationID)
	}
	deps.Registry.NotifyParent(child.ConversationID, text)
}

func assistantText(msgs []SubagentMessage) string {
	var b strings.Builder
	for _, m := range msgs {
		if m.Role == "assistant" && m.ToolName == "" {
			b.WriteString(m.Content)
		}
	}
	return b.String()
}

func reportFailure(deps Deps, child *Child, err error) {
	if deps.Registry == nil || err == nil {
		return
	}
	name := child.TypeName
	if name == "" {
		name = child.Role
	}
	if isRecoverableLimitError(err) {
		deps.Registry.NotifyParent(child.ConversationID, fmt.Sprintf(
			"Subagent %s with id %q paused due to rate/token limit (%v). You can resume it once quota is available or after changing models using manage_subagents action=resume.",
			name, child.ConversationID, err))
		return
	}
	if errors.Is(err, ErrConnectionStalled) {
		deps.Registry.NotifyParent(child.ConversationID, fmt.Sprintf(
			"Subagent %s with id %q stalled due to network inactivity. You can inspect its partial work or resume it.",
			name, child.ConversationID))
		return
	}
	deps.Registry.NotifyParent(child.ConversationID, fmt.Sprintf(
		"The subagent %s with id %q encountered an error and has either stopped or failed to start execution: %s",
		name, child.ConversationID, err))
}

func recordChildTurn(taskReg *taskregistry.Registry, child *Child, msgs []SubagentMessage, runErr error) {
	recordChildTurnWithMeta(taskReg, child, msgs, runErr, "", Invocation{}, time.Now())
}

func recordChildTurnWithMeta(
	taskReg *taskregistry.Registry,
	child *Child,
	msgs []SubagentMessage,
	runErr error,
	subDir string,
	inv Invocation,
	startedAt time.Time,
) {
	if child != nil {
		child.SetCurrentTool("")
	}
	endedAt := time.Time{}
	state := StateRunning
	if child != nil {
		state = child.State()
	}

	if taskReg != nil && child != nil {
		if child.State() == StateStopped {
			taskReg.Update(child.ConversationID, func(t *taskregistry.Task) {
				t.State = taskregistry.StateStopped
				t.Output = assistantText(msgs)
				t.Tools = usedTools(msgs)
				t.Steps = usedSteps(msgs)
				t.CurrentTool = ""
				t.EndedAt = time.Now()
				t.LastActivityAt = time.Now()
			})
			endedAt = time.Now()
		} else if runErr != nil {
			taskReg.Update(child.ConversationID, func(t *taskregistry.Task) {
				t.State = taskregistry.StateFailed
				t.Err = runErr
				t.CurrentTool = ""
				t.EndedAt = time.Now()
				t.LastActivityAt = time.Now()
			})
			endedAt = time.Now()
			state = StateErrored
		} else {
			taskReg.Update(child.ConversationID, func(t *taskregistry.Task) {
				t.Output = assistantText(msgs)
				t.Tools = usedTools(msgs)
				t.Steps = usedSteps(msgs)
				t.CurrentTool = ""
				if t.State != taskregistry.StateFailed && t.State != taskregistry.StateStopped {
					t.State = taskregistry.StateDone
				}
				t.EndedAt = time.Now()
				t.LastActivityAt = time.Now()
			})
			endedAt = time.Now()
			if state != StateStopped && state != StateErrored {
				state = StateIdle
			}
		}
	}

	if subDir != "" && child != nil {
		steps := usedSteps(msgs)
		meta := CheckpointMeta{
			ConversationID: child.ConversationID,
			ParentID:       child.ParentID,
			TypeName:       inv.TypeName,
			Role:           inv.Role,
			Worktree:       child.Worktree,
			Model:          child.Model,
			Effort:         child.Effort,
			Prompt:         inv.Prompt,
			SystemPrompt:   inv.SystemPrompt,
			InheritPrompt:  inv.InheritPrompt,
			State:          state,
			StartedAt:      startedAt,
			SpawnStepIndex: child.SpawnStepIndex,
			EndedAt:        endedAt,
			LastActivityAt: time.Now(),
			StepCount:      len(steps),
			ToolsUsed:      usedTools(msgs),
			Steps:          steps,
			Output:         assistantText(msgs),
		}
		if runErr != nil {
			meta.Error = runErr.Error()
		}
		_ = saveCheckpoint(subDir, meta)
	}
}
