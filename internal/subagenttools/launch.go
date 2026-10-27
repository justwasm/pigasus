package subagenttools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/internal/agentmessage"
	"github.com/justwasm/pigasus/internal/taskregistry"
)

// taskInstruction is the reference's own instruction to a delegated agent.
//
// 【反编译】 agy 1.4.4's `handlers.(*InvokeSubagentHandler).launchSubagent`
// carries this 224-byte literal as the subagent's system instruction:
//
//	Your task arrives as a system message from the agent that invoked you. Treat that message exactly as if the user had sent it to you directly: it is the user's complete request, and you respond to it as you would to the user.
//
// The reference delivers the task as a message (the experiment flag
// `send-subagent-initial-prompt-as-message: true`), so the instruction is
// what tells the child to read that message as its request.
const taskInstruction = "Your task arrives as a system message from the agent that invoked you. " +
	"Treat that message exactly as if the user had sent it to you directly: it is the user's complete request, " +
	"and you respond to it as you would to the user."

// idleNotice is what the parent is handed when a subagent ends without
// reporting anything back.
//
// 【strings】 agy 1.4.4's binary holds this sentence next to the delivery
// code (docs/cycles/async-subagent-framing/deliverable.md).
const idleNotice = "Subagent %s went idle without reporting anything back. " +
	"Check its work, or send it a message at that id asking it to report back."

// Launched is the result of a background launch.
type Launched struct {
	// Acknowledgement is the reference's launch acknowledgement — the text
	// the parent's model reads in place of the subagent's answer.
	Acknowledgement Acknowledgement
	// Child is the live subagent the launch created.
	Child *Child
}

// Launch starts a delegation in the background and returns the reference's
// launch acknowledgement.
//
// 【一手】 agy 1.4.4's `invoke_subagent` does not wait: it creates the
// subagent's conversation, sends it the task, and returns an acknowledgement
// naming the new conversation. The parent is told, in the acknowledgement
// itself, that the subagent will report back:
//
//	Created the following subagent:
//	{
//	  "conversationId":  "73ab3617-5a99-4e26-b1f1-c88e712efe39",
//	  ...
//	}
//	The subagent will send you a message when it has completed its task or requires guidance. There is no need to poll for its response.
//
// (docs/cycles/subagent-async-framing-capture/deliverable.md)
//
// The child's own turn runs on its own goroutine. It is not tied to the
// parent's turn: the parent's turn ends as soon as the acknowledgement is
// returned, and the child keeps running until it is killed.
func Launch(ctx context.Context, inv Invocation, deps Deps) (Launched, error) {
	if deps.Services == nil {
		return Launched{}, errors.New("subagenttools: Deps.Services is required")
	}
	if strings.TrimSpace(inv.Prompt) == "" {
		return Launched{}, errors.New("subagenttools: Invocation.Prompt is required")
	}
	model := invModel(inv, deps)
	if model == nil {
		return Launched{}, errors.New("subagenttools: no model resolved (set Deps.Model or Invocation.Model)")
	}

	agentDir := deps.AgentDir
	if agentDir == "" {
		agentDir = deps.Services.AgentDir()
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

	// The child's context outlives the parent's turn: the parent's turn
	// ends the moment the acknowledgement is returned, and the child is
	// still running then. The registry owns the child's lifetime — a
	// killed subagent, or a closed conversation, ends it.
	childCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	child := NewChild(conversationID, inv.TypeName, inv.Role, worktreePath, cancel)

	ack := Acknowledgement{
		ConversationID: conversationID,
		LogAbsoluteURI: fileURI(filepath.Join(subDir, conversationID+".jsonl")),
		WorkspaceURIs:  []string{fileURI(worktreePath)},
		At:             startedAt,
	}

	if deps.Registry != nil {
		deps.Registry.Add(child)
	}

	tools := rebindMessaging(inv.Tools, deps.Registry, conversationID)
	childTools := tools

	go runChild(childCtx, child, deps, model, systemPrompt, cwd, worktreePath, subDir, inv, childTools, startedAt)

	return Launched{Acknowledgement: ack, Child: child}, nil
}

// rebindMessaging points the conversation-scoped tools at the child that
// owns them.
//
// The tool set is built before the launch (the grant decides it), so the
// messaging tool it carries knows the parent. A child needs its own: its
// messages go to the parent, and the parent's go to it. The same is true of
// `manage_subagents`: its `list_peers` action answers differently for a
// subagent than for the conversation that owns the registry.
func rebindMessaging(list []piagent.AgentTool, registry *Registry, owner string) []piagent.AgentTool {
	out := make([]piagent.AgentTool, 0, len(list))
	for _, t := range list {
		switch t.(type) {
		case *sendMessageTool:
			out = append(out, &sendMessageTool{registry: registry, owner: owner})
		case *manageSubagentsTool:
			out = append(out, &manageSubagentsTool{registry: registry, owner: owner})
		default:
			out = append(out, t)
		}
	}
	return out
}

// runChild is the child's own loop: it runs a turn, reports what it has to
// say, and then waits for the parent to message it.
func runChild(
	ctx context.Context,
	child *Child,
	deps Deps,
	model *pi.Model,
	systemPrompt, cwd, worktreePath, subDir string,
	inv Invocation,
	childTools []piagent.AgentTool,
	startedAt time.Time,
) {
	taskReg := deps.Tasks
	if taskReg != nil {
		taskName := inv.Prompt
		if len(taskName) > 60 {
			taskName = taskName[:57] + "..."
		}
		taskReg.Register(&taskregistry.Task{
			ID:        child.ConversationID,
			Name:      taskName,
			Kind:      taskregistry.KindSubagent,
			State:     taskregistry.StateRunning,
			StartedAt: startedAt,
			Prompt:    inv.Prompt,
		})
	}

	rt, session, err := createSubagentSession(ctx, deps, model, childSystemPrompt(systemPrompt, inv), cwd, childTools)
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
	defer func() { _ = rt.Close() }()
	defer func() { _ = session.Close() }()
	defer purgeWorktreeArtifacts(worktreePath, child.ConversationID)

	parentID := ""
	if deps.Registry != nil {
		parentID = deps.Registry.ParentID()
	}

	// The first message is the task, delivered the way the reference
	// delivers it: as a message from the agent that invoked the child,
	// wrapped in the reference's envelope.
	//
	// 【一手】 the 1.4.4 capture: "The child sees the mirror image: its
	// first user message is the envelope carrying the parent's Prompt,
	// with sender = the parent's conversation id."
	first := agentmessage.Render(parentID, inv.Prompt, time.Now(), agentmessage.PriorityHigh)
	msgs, runErr := session.Send(ctx, first)

	recordChildTurn(taskReg, child, msgs, runErr)
	if subDir != "" {
		persistTranscript(subDir, child.ConversationID, msgs, runErr)
	}

	for {
		reportChildTurn(deps, child, msgs, runErr)
		if runErr != nil {
			return
		}
		// The child's turn is over: it is alive, but quiet, and the
		// parent can message it again.
		child.setState(StateIdle)

		// The child stays alive: the parent can message it ("send it a
		// message at that id asking it to report back"), and a message
		// wakes it up for another turn.
		msg, ok := child.Wait(ctx)
		if !ok {
			return
		}
		child.setState(StateRunning)
		envelope := agentmessage.Render(parentID, msg.Content, msg.At, agentmessage.PriorityHigh)
		msgs, runErr = session.Send(ctx, envelope)
		recordChildTurn(taskReg, child, msgs, runErr)
		if subDir != "" {
			persistTranscript(subDir, child.ConversationID, msgs, runErr)
		}
	}
}

// childSystemPrompt adds the reference's task instruction to the child's
// prompt.
//
// The `self` type inherits the parent's prompt as it is — the reference's
// own words are "inherits the parent agent's full configuration including
// tools, system prompt, and model" — so the instruction is not added there.
func childSystemPrompt(base string, inv Invocation) string {
	if inv.InheritPrompt {
		return base
	}
	if base == "" {
		return taskInstruction
	}
	return taskInstruction + "\n\n" + base
}

// reportChildTurn hands the child's result to the parent.
//
// The reference delivers a subagent's report to the parent as an agent
// message (the `<SYSTEM_MESSAGE>` envelope), not as a tool result. A
// subagent that reported nothing gets the reference's own notice instead.
func reportChildTurn(deps Deps, child *Child, msgs []piagent.AgentMessage, runErr error) {
	if deps.Registry == nil {
		return
	}
	if child.Reported() {
		// The child already sent something to its parent; a second
		// delivery would say it twice.
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

// reportFailure tells the parent that the child's turn failed.
//
// 【反编译】 agy 1.4.8 — the reference's own sentence (the literal is at
// 0x10368c746):
//
//	The subagent %s with id %q encountered an error and has either stopped or failed to start execution: %s
//
// The first argument is the subagent's own name — its type, or its role when
// the call named one.
func reportFailure(deps Deps, child *Child, err error) {
	if deps.Registry == nil || err == nil {
		return
	}
	name := child.TypeName
	if name == "" {
		name = child.Role
	}
	deps.Registry.NotifyParent(child.ConversationID, fmt.Sprintf(
		"The subagent %s with id %q encountered an error and has either stopped or failed to start execution: %s",
		name, child.ConversationID, err))
}

// recordChildTurn writes the child's turn into the task registry, so the
// /tasks panel shows what the subagent did.
func recordChildTurn(taskReg *taskregistry.Registry, child *Child, msgs []piagent.AgentMessage, runErr error) {
	if taskReg == nil {
		return
	}
	if runErr != nil {
		taskReg.Update(child.ConversationID, func(t *taskregistry.Task) {
			t.State = taskregistry.StateFailed
			t.Err = runErr
			t.EndedAt = time.Now()
		})
		return
	}
	taskReg.Update(child.ConversationID, func(t *taskregistry.Task) {
		t.Output = assistantText(msgs)
		t.Tools = usedTools(msgs)
		t.Steps = usedSteps(msgs)
		if t.State != taskregistry.StateFailed {
			t.State = taskregistry.StateDone
		}
		t.EndedAt = time.Now()
	})
}
