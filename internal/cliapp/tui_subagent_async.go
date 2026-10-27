package cliapp

import (
	"context"
	"encoding/json"

	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/permissions"
	"github.com/justwasm/pigasus/internal/sandbox"
	"github.com/justwasm/pigasus/internal/subagenttools"
	"github.com/justwasm/pigasus/internal/toolrunner"
	"github.com/justwasm/pigasus/internal/tools"
)

// tui_subagent_async.go — the TUI's side of the asynchronous subagent.
//
// The reference's `invoke_subagent` does not wait: it creates the
// subagent's conversation, sends it the task, and returns an
// acknowledgement. The subagent runs in the background and reports back as
// a message to the parent — the same `<SYSTEM_MESSAGE>` envelope a
// background task's completion notification uses.
//
// 【一手】 the 1.4.4 live capture (docs/cycles/subagent-async-framing-capture/deliverable.md):
// the acknowledgement names the new conversation, and the delivery arrives
// as a user-role content part in the parent's *next* invocation.
//
// Three pieces make that work in the TUI:
//
//   - the registry, which holds the live children (the conversation's own
//     scope: "Manage the direct subagents of this conversation");
//   - the notifier, which puts a child's message into the session's inbox;
//   - the permission gate, which judges the child's tool calls under the
//     conversation's one policy.
func (m *runMode) ensureSubagentRegistry() *subagenttools.Registry {
	if m.subagentRegistry != nil && m.subagentRegistry.ParentID() == m.conversationID {
		return m.subagentRegistry
	}
	if m.subagentRegistry != nil {
		// The conversation changed: the old children belong to it.
		m.subagentRegistry.Close()
	}
	m.subagentRegistry = subagenttools.NewRegistry(m.conversationID)
	m.subagentRegistry.SetNotify(m.notifySubagentMessage)
	return m.subagentRegistry
}

// notifySubagentMessage hands a subagent's message to the session.
//
// It travels the way a background task's notification travels: into the
// session's inbox, which the next turn drains into the conversation. The
// text the model reads is the reference's own envelope, with the
// subagent's conversation id as the sender.
func (m *runMode) notifySubagentMessage(sender, content string) {
	if m.taskInbox == nil {
		m.taskInbox = agent.NewTaskInbox()
	}
	m.taskInbox.Add(tools.TaskNotification{
		TaskID:   sender,
		Body:     content,
		Priority: tools.TaskPriorityHigh,
	})
	if m.prog != nil {
		m.prog.Send(taskNotificationMsg{})
	}
}

// subagentPermissionGate judges a subagent's tool calls.
//
// The policy is the conversation's one policy — the same *permissions.Policy
// the parent's own calls go through — so a rule the user wrote for the
// parent governs the subagent too. The call is judged under both of the
// tool's names (the catalogue's and the declaration's), because the
// subagent calls the catalogue's spelling while the parent calls the
// declaration's.
//
// The sandbox is the parent's, with the subagent's own worktree added: a
// subagent that runs in its own directory must be allowed to work in it.
type subagentPermissionGate struct {
	policy   *permissions.Policy
	sandbox  sandbox.Sandbox
	worktree string
}

// Decide reports whether the call may run.
func (g *subagentPermissionGate) Decide(_ context.Context, toolName string, args json.RawMessage) subagenttools.GateDecision {
	sb := g.sandbox
	if sb.Enabled && g.worktree != "" {
		extra := make([]string, 0, len(sb.ExtraPaths)+1)
		extra = append(extra, sb.ExtraPaths...)
		extra = append(extra, g.worktree)
		sb.ExtraPaths = extra
	}
	decision := toolrunner.Decide(g.policy, sb, backend.ToolCall{Name: toolName, Arguments: args})
	return subagenttools.GateDecision{Allow: decision.Allow, Reason: decision.Reason}
}

// subagentGateFactory builds the gate for a subagent that runs in the given
// directory.
func (m *runMode) subagentGateFactory(worktree string) subagenttools.BeforeToolCallGate {
	return &subagentPermissionGate{
		policy:   m.policy,
		sandbox:  m.toolSandbox,
		worktree: worktree,
	}
}
