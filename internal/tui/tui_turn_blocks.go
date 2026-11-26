package tui

import (
	"context"
	"encoding/json"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/app/subagenttools"
	taskregistry "github.com/justwasm/pigasus/internal/app/task"
	"github.com/justwasm/pigasus/internal/app/toolrunner"
	"github.com/justwasm/pigasus/internal/app/tools/shell"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/compaction"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	"github.com/justwasm/pigasus/internal/core/policy/sandbox"
	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
	"github.com/justwasm/pigasus/internal/ui/mdrender"
	uirows "github.com/justwasm/pigasus/internal/ui/rows"
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
	"github.com/justwasm/pigasus/internal/ui/transcript"
	"github.com/justwasm/pigasus/internal/ui/views/statusrow"
	"github.com/justwasm/pigasus/palette"
)

// ---- tui_turn_blocks.go ----

// transcriptDocument hands the host's transcript state to the ui tier.
//
// The host owns the rows: the document is the shape the transcript's
// mechanics work on, and the host takes the result back through
// applyTranscriptDocument.
func (m *RootModel) transcriptDocument() transcript.Document {
	return transcript.Document{
		Lines:             m.conversation.response,
		Blocks:            m.conversation.blocks,
		Assistants:        m.conversation.assistants,
		UserBlocks:        m.conversation.userBlocks,
		Runs:              m.conversation.liveStepRuns,
		LiveStart:         m.conversation.liveStart,
		ReplyStart:        m.conversation.replyStart,
		LiveThoughtRunIdx: m.conversation.liveThoughtRunIdx,
		LiveTextLines:     m.conversation.liveTextLines,
		LiveReplyBuf:      m.conversation.liveReplyBuf,
		StreamedText:      m.conversation.streamedText,
	}
}

// applyTranscriptDocument takes the transcript's state back.
func (m *RootModel) applyTranscriptDocument(doc transcript.Document) {
	m.conversation.response = doc.Lines
	m.conversation.blocks = doc.Blocks
	m.conversation.assistants = doc.Assistants
	m.conversation.userBlocks = doc.UserBlocks
	m.conversation.liveStepRuns = doc.Runs
	m.conversation.liveStart = doc.LiveStart
	m.conversation.replyStart = doc.ReplyStart
	m.conversation.liveThoughtRunIdx = doc.LiveThoughtRunIdx
	m.conversation.liveTextLines = doc.LiveTextLines
	m.conversation.liveReplyBuf = doc.LiveReplyBuf
	m.conversation.streamedText = doc.StreamedText
}

// transcriptOptions are the host's own render facts, as the transcript's
// mechanics see them.
func (m *RootModel) transcriptOptions(msg toolsteps.TurnDoneMsg) transcript.Options {
	opts := m.repaintOptions()
	opts.EmptyTurn = agentturn.IsEmptyAssistantTurn(msg.Reply, nil, msg.Reasoning)
	return opts
}

// repaintOptions are the host's own render facts, as the transcript's
// repaint engine sees them.
//
// The viewport's height is handed over as a lookup rather than as a
// number: the engine asks for it only when it has to decide how many of
// the blocks it repaints, and the host's answer is a cached
// measurement.
func (m *RootModel) repaintOptions() transcript.Options {
	verb := m.currentVerbosity()
	return transcript.Options{
		Width:          m.viewport.width,
		Height:         m.viewport.height,
		Verbosity:      verb,
		Expanded:       m.effectiveExpanded(verb),
		ViewportHeight: m.viewportHeight,
		Streaming:      m.conversation.streaming,
		Thought: transcript.LiveThought{
			Active: m.conversation.thinking,
			Text:   m.conversation.liveThinking,
			Since:  m.conversation.thinkingSince,
		},
	}
}

// toggleTrajectoryExpanded flips the trajectory-wide expanded flag,
// clears the scrollback by snapping the viewport to the active turn,
// and repaints the tool-step blocks under the active verbosity.
func (m *RootModel) toggleTrajectoryExpanded() {
	m.viewport.expandedUserToggled = true
	m.viewport.expanded = !m.isExpanded()
	m.viewport.scroll = 0
	m.viewport.autoScroll = true
	m.viewport.rowsCacheDirty = true
	m.viewport.viewCacheValid = false
	m.rerenderBlocks()
	m.rerenderLiveSteps()
}

// rerenderBlocks repaints the transcript's tool-step blocks under the
// active verbosity and expansion.
func (m *RootModel) rerenderBlocks() {
	m.viewport.rowsCacheDirty = true
	doc := m.transcriptDocument()
	doc.RerenderBlocks(m.repaintOptions())
	m.applyTranscriptDocument(doc)
}

// ensureVisibleBlocksExpanded repaints the blocks inside the visible
// window, so a block that scrolls into view is drawn under the current
// verbosity and expansion.
func (m *RootModel) ensureVisibleBlocksExpanded(avail int) {
	doc := m.transcriptDocument()
	if doc.EnsureVisibleBlocksExpanded(m.repaintOptions(), avail, m.viewport.scroll, m.viewport.autoScroll) {
		m.viewport.rowsCacheDirty = true
		m.viewport.viewCacheValid = false
	}
	m.applyTranscriptDocument(doc)
}

// rerenderTranscriptForTheme repaints all user prompts, assistant replies,
// and tool step blocks across m.conversation.response under the active colour scheme.
//
// 【反编译】 agy 1.4.8 model.(*ConversationModel).handleSettingsChanges
// calls store.(*Manager).RerenderAll when the colour scheme changes,
// repainting the entire transcript with the new scheme's colours.
func (m *RootModel) rerenderTranscriptForTheme() {
	if m == nil {
		return
	}
	m.viewport.rowsCacheDirty = true

	doc := m.transcriptDocument()
	doc.RerenderForTheme(m.repaintOptions())
	m.applyTranscriptDocument(doc)

	// Invalidate view caches so viewport redraws completely
	m.viewport.rowsCacheDirty = true
	m.viewport.rowsCache = wrapSlot{}
	m.viewport.rowsView = composedRows{}
}

// reflowTranscript re-renders and re-wraps the entire transcript (markdown replies,
// tool step blocks, turn separators, in-flight streaming replies) when the terminal width changes.
//
// 【反编译】 agy 1.4.12 ConversationModel.handleResizeRedraw (0x102bcbb70)
// calls buildRenderedItems (0x102bbc2d0) on terminal resize, recalculating and
// re-rendering all message items to reflow properly for the new column width.
func (m *RootModel) reflowTranscript(width int) {
	if m == nil || width <= 0 {
		return
	}
	m.viewport.width = width
	mdrender.SetCurrentWidth(width)
	m.viewport.rowsCacheDirty = true
	m.viewport.viewCacheValid = false
	m.viewport.extraRowsValid = false

	doc := m.transcriptDocument()
	doc.Reflow(m.repaintOptions())
	m.applyTranscriptDocument(doc)

	// Invalidate viewport caches and rebuild wraps for the new width.
	m.viewport.rowsCacheDirty = true
	m.viewport.rowsCache = wrapSlot{}
	m.viewport.rowsCacheN1 = wrapSlot{}
	m.viewport.rowsView = composedRows{}
	m.viewport.viewCacheValid = false
	m.viewport.cachedHeaderKey = headerCacheKey{}
}

// syncLiveThoughtStep paints the turn's live thought row.
func (m *RootModel) syncLiveThoughtStep() {
	doc := m.transcriptDocument()
	if doc.SyncLiveThoughtStep(m.repaintOptions()) {
		m.viewport.rowsCacheDirty = true
	}
	m.applyTranscriptDocument(doc)
}

// finishLiveThinking closes the turn's live thought: the running row
// becomes the finished thought.
func (m *RootModel) finishLiveThinking() {
	if !m.conversation.thinking {
		return
	}
	// The options are taken while the thought is still live: the
	// finished thought is painted from the thought that was running.
	opts := m.repaintOptions()
	m.conversation.ClearThinking()

	doc := m.transcriptDocument()
	if doc.FinishLiveThinking(opts) {
		m.viewport.rowsCacheDirty = true
	}
	m.applyTranscriptDocument(doc)
}

// rerenderLiveSteps repaints the in-flight step rows: the live thought
// and the tool batches the model is still running.
func (m *RootModel) rerenderLiveSteps() {
	doc := m.transcriptDocument()
	if doc.RerenderLiveSteps(m.repaintOptions()) {
		m.viewport.rowsCacheDirty = true
	}
	m.applyTranscriptDocument(doc)
}

// appendCompactResult renders the completion of a PiG-driven
// compaction. The reference (agy 1.4.5) draws a centred
// "Conversation compacted" rule plus a centred stats line instead of a
// transcript bullet; the same two rows are used here.
//
// PiG's end event only carries the token counts, so the stats line
// omits the step count and the summary size the reference reports
// (both are passed as "absent"). See
// docs/cycles/auto-compact-parity/captures.md.
func (m *RootModel) appendCompactResult(msg compactDoneMsg) {
	if msg.Err != nil {
		m.conversation.response = append(m.conversation.response,
			uirows.NoticeCompact("Compact failed: "+msg.Err.Error()),
			"",
		)
		return
	}
	stats := compaction.StatsRow(m.viewport.width, 0, msg.TokensBefore, msg.TokensAfter, -1, -1)
	if msg.Aborted {
		stats += " (aborted)"
	}
	m.conversation.response = append(m.conversation.response,
		compaction.DividerRow(m.viewport.width),
		stats,
		"",
	)
}

var separator = palette.Rule(separatorWidth)

// separator returns the horizontal rule drawn between the banner and the
// prompt. Cycle 119: when the terminal width is known the rule is drawn at
// that width rather than the fixed 220 columns, so it never wraps.
func (m *RootModel) separator() string {
	if m.viewport.width > 0 {
		return palette.Rule(m.viewport.width)
	}
	return separator
}

// scrollPositionIndicator returns the 1.5.12 indicator when scrolled up in altscreen mode,
// such as "↓ 25 lines · 2 turns".
func (m *RootModel) scrollPositionIndicator() string {
	if !m.viewport.altScreen || m.viewport.autoScroll {
		return ""
	}
	rows := m.viewport.rowsView.Rows
	if len(rows) == 0 {
		rows, _, _ = m.syncRowsView()
	}
	if len(rows) == 0 {
		return ""
	}
	avail := m.viewport.vpRows
	if avail <= 0 {
		if m.viewport.height > 6 {
			avail = m.viewport.height - 6
		} else {
			avail = 10
		}
	}
	if len(rows) <= avail {
		return ""
	}
	lastVisible := m.viewport.scroll + avail
	if m.viewport.pinnedPromptText != "" && avail > 2 {
		lastVisible -= 2
	}
	if lastVisible >= len(rows) {
		return ""
	}
	linesBelow := len(rows) - lastVisible
	turnsBelow := 0
	for _, pRow := range m.viewport.userPromptRows {
		if pRow >= lastVisible {
			turnsBelow++
		}
	}
	return tuiviewport.FormatScrollPositionIndicator(linesBelow, turnsBelow)
}

// promptRule returns the rule that frames the prompt box.
//
// 【一手】 agy 1.4.7: with a seasonal event switched on the prompt box's
// two rules become the event's own — a gradient rule with face motifs in
// it (out/changelog-147/ref/seasonal-prompt.txt) — while the rule that
// separates the queued messages from the transcript stays plain.
//
// agy 1.5.12 item 3: while scrolled up in full screen (altscreen) mode,
// the line above the prompt shows the scroll position indicator (e.g. "↓ 25 lines · 2 turns").
func (m *RootModel) promptRule(top bool) string {
	if m.viewport.seasonal == nil {
		return m.separator()
	}
	return m.viewport.seasonal.Border.Rule(m.viewport.width, top)
}

const inputPrompt = "> "

// inputWidth is the textarea render width.
const inputWidth = 219

// statusRow is the status area's single row: the custom script's output
// when one is active, the built-in template otherwise.
func (m *RootModel) statusRow() string {
	if out, _ := m.externalRenderedForRow(); out != "" {
		return out
	}
	return statusrow.Render(m.statusRowState())
}

// ---- tui_task_notify.go ----

// tui_task_notify.go — cycle 309: the delivery of a background task's
// notification into the turn loop.
//
// The reference tells the model that a background task is over: the
// notification is an agent message, and the agent picks it up and carries
// on with its work. The clone's seam is the same, in three pieces:
//
//   - the tools layer fires the notification when the command is gone
//     (internal/tools/support.go);
//   - the inbox holds it until the next turn (internal/agent/task_inbox.go);
//   - the TUI drains the inbox into the session and starts a turn of its
//     own when the agent is idle.
//
// The inbox lives on the run mode, not on the agent.Loop: the TUI builds a
// fresh Loop for every turn, so a per-loop queue would drop every
// notification that arrives between turns — which is when a background task
// finishes.
//
// The transcript's own rows for a notification are the ui tier's
// (internal/ui/rows.NotificationRows); this file owns the delivery, not
// the shape of the rows that show it.

// taskNotificationMsg carries the fact that a task notification is waiting.
//
// The payload is not in the message: the notification is in the inbox, and
// the handler drains it. One message per notification would race the
// inbox's own ordering.
type taskNotificationMsg struct{}

// installTaskNotifier wires the tools layer's notification hook to the
// session's inbox.
//
// The hook is process-wide (internal/shell.SetTaskNotifier), so the last
// run mode to install it owns the notifications. That is the TUI's case:
// there is one run mode per process.
func (m *RootModel) installTaskNotifier() {
	if m.tasks.ctrl != nil {
		shell.SetTaskNotifier(func(n shell.TaskNotification) {
			m.tasks.ctrl.Inbox().Add(taskregistry.TaskNotification{
				TaskID:   n.TaskID,
				Title:    n.Title,
				Body:     n.Body,
				LogPath:  n.LogPath,
				Priority: n.Priority,
				At:       n.At,
			})
			if m.turn.prog != nil {
				m.turn.prog.Send(taskNotificationMsg{})
			}
		})
		return
	}
	if m.tasks.inbox == nil {
		m.tasks.inbox = agentturn.NewTaskInbox()
	}
	shell.SetTaskNotifier(func(n shell.TaskNotification) {
		m.tasks.inbox.Add(n)
		if m.turn.prog != nil {
			m.turn.prog.Send(taskNotificationMsg{})
		}
	})
}

// hasPendingTaskNotifications reports whether any background task or subagent
// notification is waiting to be processed.
func (m *RootModel) hasPendingTaskNotifications() bool {
	if m.tasks.ctrl != nil && m.tasks.ctrl.Inbox().Len() > 0 {
		return true
	}
	if m.tasks.inbox != nil && m.tasks.inbox.Len() > 0 {
		return true
	}
	return false
}

// updateTaskNotificationMsg handles a task notification that arrived.
//
// The notification is drained into the session by the next turn's message
// builder. When the agent is idle, the TUI starts a turn right away: the
// reference's agent picks the message up and carries on with its work
// without waiting for the user to type something.
func (m *RootModel) updateTaskNotificationMsg() tea.Cmd {
	if !m.hasPendingTaskNotifications() {
		return nil
	}
	if m.conversation.streaming {
		// A turn is in flight: the notification waits for the next one.
		// The turn's end starts a turn of its own for it.
		return nil
	}
	if next := m.dequeueNextPrompt(); next != "" {
		return m.flushNextQueued(next)
	}
	return m.startNotificationTurn()
}

// startNotificationTurn starts a turn that hands the pending task
// notifications to the model.
func (m *RootModel) startNotificationTurn() tea.Cmd {
	m.conversation.liveStart = len(m.conversation.response)
	m.conversation.replyStart = -1
	m.conversation.liveReplyIdx = -1
	m.conversation.liveSteps = nil
	m.conversation.liveStepRuns = nil
	m.conversation.activeTool = ""
	m.conversation.thinking = false
	m.conversation.liveTextLines = 0
	m.conversation.liveReplyBuf = ""
	m.conversation.streamedText = false
	m.conversation.liveBatch = 0
	m.conversation.batchBreak = false
	m.conversation.anyToolRun = false
	m.conversation.spinFrame = 0
	m.conversation.streaming = true
	m.clearLastTurnOutcome()
	tickCmd := func() tea.Msg { return m.conversation.spinner.Tick() }
	return tea.Batch(m.streamPrompt(""), tickCmd)
}

// takeTaskNotifications removes the pending notifications from the inbox and
// returns them, oldest first.
func (m *RootModel) takeTaskNotifications() []shell.TaskNotification {
	var out []shell.TaskNotification
	if m.tasks.ctrl != nil {
		for _, n := range m.tasks.ctrl.Inbox().Drain() {
			out = append(out, shell.TaskNotification{
				TaskID:   n.TaskID,
				Title:    n.Title,
				Body:     n.Body,
				LogPath:  n.LogPath,
				Priority: n.Priority,
				At:       n.At,
			})
		}
	}
	if m.tasks.inbox != nil {
		out = append(out, m.tasks.inbox.Drain()...)
	}
	return out
}

// drainTaskNotifications moves the pending notifications into the session
// and renders them in the transcript.
//
// The rows go in before the turn's live block: the drain happens while the
// turn is being set up, so the notification belongs to the conversation
// before the model's answer to it. If a user turn was already appended to
// the session, the notifications are placed BEFORE the user turn so the user
// prompt remains the active request.
func (m *RootModel) drainTaskNotifications() {
	items := m.takeTaskNotifications()
	if len(items) == 0 {
		return
	}
	rows := 0
	trailingUser := false
	var userMsg backend.Message
	if last, ok := m.session.lastMessage(); ok && last.Role == "user" {
		trailingUser = true
		userMsg = last
		m.session.truncateMessages(m.session.messageCount() - 1)
	}

	for _, item := range items {
		msg := agentturn.TaskNotificationMessage(item)
		m.session.appendMessage(msg)
		if m.isSubagentNotification(item) || item.Title != "Notification timeout reached" {
			// Subagent responses and task completion notifications are delivered
			// as internal system messages to the model context only, and are NOT
			// echoed directly into the visible user-facing transcript.
			// Only in-flight warnings (like "Notification timeout reached") show up in the transcript.
			continue
		}
		lines := uirows.NotificationRows(item)
		if trailingUser && len(m.conversation.userBlocks) > 0 {
			ub := m.conversation.userBlocks[len(m.conversation.userBlocks)-1]
			idx := ub.Start
			if idx >= 0 && idx <= len(m.conversation.response) {
				m.conversation.response = append(m.conversation.response[:idx], append(lines, m.conversation.response[idx:]...)...)
				m.conversation.userBlocks[len(m.conversation.userBlocks)-1].Start += len(lines)
			} else {
				m.conversation.response = append(m.conversation.response, lines...)
			}
		} else {
			m.conversation.response = append(m.conversation.response, lines...)
		}
		rows += len(lines)
	}

	if trailingUser {
		m.session.appendMessage(userMsg)
	}

	// The turn's live block starts after the notification's rows.
	if m.conversation.liveStart >= 0 && m.conversation.liveStart+rows <= len(m.conversation.response) {
		m.conversation.liveStart += rows
	}
}

// ---- tui_subagent_async.go ----

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
func (m *RootModel) ensureSubagentRegistry() *subagenttools.Registry {
	if m.agents.registry != nil && m.agents.registry.ParentID() == m.session.conversationID {
		m.agents.registry.SetNotify(m.notifySubagentMessage)
		return m.agents.registry
	}
	if m.agents.registry != nil {
		// The conversation changed: the old children belong to it.
		m.agents.registry.Close()
	}
	m.agents.registry = subagenttools.NewRegistry(m.session.conversationID)
	m.agents.registry.SetNotify(m.notifySubagentMessage)
	if dom := m.DomainSession(); dom != nil {
		dom.SetSubagentRegistry(m.agents.registry)
	}
	return m.agents.registry
}

// isSubagentNotification reports whether the notification was emitted by a
// child subagent rather than an external background shell task.
func (m *RootModel) isSubagentNotification(item shell.TaskNotification) bool {
	if m.agents.subagentSenders != nil && m.agents.subagentSenders[item.TaskID] {
		return true
	}
	if m.agents.registry != nil {
		if _, ok := m.agents.registry.Get(item.TaskID); ok {
			return true
		}
	}
	if m.tasks.ctrl != nil && m.tasks.ctrl.Registry() != nil {
		if t := m.tasks.ctrl.Registry().Get(item.TaskID); t != nil && t.Kind == taskregistry.KindSubagent {
			return true
		}
	}
	return false
}

// notifySubagentMessage hands a subagent's message to the session.
//
// It travels the way a background task's notification travels: into the
// session's inbox, which the next turn drains into the conversation. The
// text the model reads is the reference's own envelope, with the
// subagent's conversation id as the sender.
func (m *RootModel) notifySubagentMessage(sender, content string) {
	if m.agents.subagentSenders == nil {
		m.agents.subagentSenders = make(map[string]bool)
	}
	m.agents.subagentSenders[sender] = true
	if m.tasks.ctrl != nil {
		m.tasks.ctrl.Inbox().Add(taskregistry.TaskNotification{
			TaskID:   sender,
			Body:     content,
			Priority: shell.TaskPriorityHigh,
			At:       time.Now(),
		})
		if m.turn.prog != nil {
			m.turn.prog.Send(taskNotificationMsg{})
		}
		return
	}
	if m.tasks.inbox == nil {
		m.tasks.inbox = agentturn.NewTaskInbox()
	}
	m.tasks.inbox.Add(shell.TaskNotification{
		TaskID:   sender,
		Body:     content,
		Priority: shell.TaskPriorityHigh,
		At:       time.Now(),
	})
	if m.turn.prog != nil {
		m.turn.prog.Send(taskNotificationMsg{})
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
func (m *RootModel) subagentGateFactory(worktree string) subagenttools.BeforeToolCallGate {
	return &subagentPermissionGate{
		policy:   m.session.policy,
		sandbox:  m.tools.sandbox,
		worktree: worktree,
	}
}
