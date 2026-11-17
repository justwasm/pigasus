// conversation_seed.go — conversation seeding and @conversation mention logic.
//
// Which conversation a run continues, transcript seeding into RootModel,
// and @conversation mention parsing, search, and insertion.
//
// Recovered behaviour (1.3.6, verified against the live binary):
//
//	--conversation=<id>   adopt <id> iff conversations/<id>.db exists as a
//	                      regular file (a zero-byte file counts, a directory
//	                      does not); otherwise print
//	                        warning: conversation "<id>" not found
//	                      on stderr and start a fresh conversation. An empty
//	                      value is silent, the lookup is case-sensitive, and
//	                      the last occurrence wins.
//	-c / --continue       pick the most recent conversation for the current
//	                      directory. The pick is workspace-scoped, and a
//	                      directory that has no conversation of its own
//	                      inherits its parent's (verified: running in
//	                      /tmp/agyconvprobe continued /tmp's conversation).
package tui

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/term"

	subagent "github.com/justwasm/pigasus/internal/app/subagent/core"
	taskregistry "github.com/justwasm/pigasus/internal/app/task"
	"github.com/justwasm/pigasus/internal/app/tools"
	"github.com/justwasm/pigasus/internal/core/confhome"
	"github.com/justwasm/pigasus/internal/ui/mdrender"
	"github.com/justwasm/pigasus/internal/ui/tuilib"

	atconversation "github.com/justwasm/pigasus/internal/app/prompt/mentions/conversation"
	"github.com/justwasm/pigasus/internal/app/session"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/app/session/rehydration"
	"github.com/justwasm/pigasus/internal/app/subagenttools"
	"github.com/justwasm/pigasus/internal/app/tools/shell"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/cliflags"
	"github.com/justwasm/pigasus/internal/core/workspace"
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
	"github.com/justwasm/pigasus/internal/ui/transcript"
)

// resolveConversationSelection decides what a run continues, printing the
// reference's warning on warn when an explicitly named conversation is missing.
func resolveConversationSelection(fs *flag.FlagSet, warn io.Writer) convstore.Selection {
	convID := cliflags.StringValue(fs, "conversation")
	cont := cliflags.ResolveBoolAlias(fs, "continue", "c")
	return convstore.ResolveSelection(convID, cont, workspace.Current(), warn)
}

func (m *RootModel) seedConversation(c *convstore.Conversation) {
	if c == nil {
		return
	}
	m.session.bumpEpoch()
	if len(c.Steps) > 0 || c.ID != "" {
		m.session.conversationStarted = true
	}
	m.rehydrateConversation(c)
	steps := c.Transcript()
	cp := c.Checkpoint()
	for i := 0; i < len(steps); {
		if steps[i].Role() == "tool" {
			i = m.seedToolGroup(steps, i)
			continue
		}
		m.seedNonToolStep(steps[i])
		i++
	}
	// The model context is the checkpoint's resume block plus the steps
	// after its boundary. The transcript still keeps the adopted steps for
	// persistence and for the blocks above.
	if cp != nil {
		m.setSessionMessages(checkpointChatMessages(c.ChatTurns()))
	}
	// The trajectory starts as the conversation's own steps: the tool
	// calls the reference stored are part of it, and the turns that run
	// from here are appended to them. The messages already live in the
	// domain's supervisor, so only the trajectory seed is recorded here.
	if sup := m.session.sup(); sup != nil {
		sup.SetSeededConversation(c.Steps, m.session.messageCount())
	}
	m.viewport.rowsCacheDirty = true
	m.viewport.rowsGen++

	// Direction 2: Restore persisted queued prompts if any.
	convID := c.ID
	if convID == "" {
		convID = m.session.conversationID
	}
	if convID != "" {
		sStore := session.NewStore("")
		queued := sStore.LoadQueued(convID)
		if len(queued) > 0 {
			m.session.store.QueuedPrompt = queued[0]
			if len(queued) > 1 {
				m.session.store.StreamQueue = append(m.session.store.StreamQueue, queued[1:]...)
			}
		}

		// Restore durable active timer/cron schedules.
		m.restoreActiveSchedules(sStore, convID)
	}
}

// restoreActiveSchedules restores durable timer and cron schedules on conversation resume.
func (m *RootModel) restoreActiveSchedules(sStore *session.Store, convID string) {
	if convID == "" {
		return
	}
	if sStore == nil {
		sStore = session.NewStore("")
	}
	scheds := sStore.LoadSchedules(convID)
	if len(scheds) == 0 {
		return
	}
	now := time.Now()
	for _, s := range scheds {
		remaining := s.TargetTime.Sub(now)
		taskID := s.TaskID
		logPath := filepath.Join(tools.ScheduleHome(""), "brain", convID,
			".system_generated", "tasks", taskID+".log")

		if remaining > 0 {
			// Schedule is still in the future: re-arm with remaining duration
			_, _ = shell.StartScheduledTask(shell.ScheduledTaskOptions{
				Home:           "",
				ConvID:         convID,
				TaskID:         taskID,
				LogPath:        logPath,
				Prompt:         s.Prompt,
				Duration:       remaining,
				CronExpr:       s.CronExpr,
				TimerCondition: s.TimerCondition,
				IsDaemon:       s.IsDaemon,
				ExactTiming:    s.ExactTiming,
				CreatedAt:      s.CreatedAt,
				TargetTime:     s.TargetTime,
			})
		} else {
			// Schedule expired while offline: remove from active and fire immediate catch-up wakeup
			_ = sStore.RemoveSchedule(convID, taskID)
			shell.NotifyTask(shell.TaskNotification{
				TaskID:   convID + "/" + taskID,
				Priority: shell.TaskPriorityHigh,
				Body:     fmt.Sprintf("[Scheduled wakeup: timer catch-up (scheduled for %s)]\n%s", s.TargetTime.Format("15:04:05"), s.Prompt),
				LogPath:  logPath,
				At:       now,
			})
		}
	}
}

// rehydrateConversation rebuilds the conversation's live state from its
// trajectory.
//
// The reference keeps one durable record of a conversation — the
// trajectory — and derives the agent's world from it when the
// conversation is loaded: the subagents it defined, the schedules it
// created, the tasks it managed (【反编译】 the reference's
// `cortex/rehydration` package and the cascade manager's
// `processLoadedTrajectory`, CLI 0x101f26440; see
// out/ide-language-server/README.md §10.9).
//
// The clone's transcript comes back from the store, but the state the
// tools hold does not: the definition store, the schedule table and the
// task registry are process-local. This is where they are filled from
// the stored steps, so a resumed conversation continues where it left
// off instead of starting blind.
//
// The pass is a no-op when the trajectory holds no state-carrying tool
// call, and it is skipped entirely when OPENAGY_DISABLE_REHYDRATION is
// set (the clone's name for the reference's `cascade-disable-rehydration`
// flag).
func (m *RootModel) rehydrateConversation(c *convstore.Conversation) {
	if c == nil {
		return
	}
	convID := c.ID
	if convID == "" {
		convID = m.session.conversationID
	}
	// A fresh store: adopting a conversation replaces the previous
	// one's definitions, it does not add to them.
	m.agents.defs = subagenttools.NewStore()
	state := &rehydration.State{
		ConversationID: convID,
		Definitions:    m.agents.defs,
		Schedules:      shell.ScheduleTableFor(convID),
		Tasks:          m.tasks.reg,
	}
	rehydration.Restore(c.RehydrationSteps(), state)
	m.rehydrateSubagents(convID)
}

func (m *RootModel) rehydrateSubagents(convID string) {
	if convID == "" {
		return
	}
	candidates := []string{
		filepath.Join(confhome.Antigravity(), "brain", convID, ".system_generated", "subagents"),
	}
	if home := os.Getenv("HOME"); home != "" {
		candidates = append(candidates,
			filepath.Join(home, ".gemini", "antigravity-cli", "brain", convID, ".system_generated", "subagents"),
		)
	}

	for _, subagentsDir := range candidates {
		entries, err := os.ReadDir(subagentsDir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			path := filepath.Join(subagentsDir, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			var record struct {
				ConversationID     string `json:"conversationId"`
				SubagentDescriptor struct {
					TypeName string `json:"typeName"`
					Role     string `json:"role"`
				} `json:"subagentDescriptor"`
				State string `json:"state"`
			}
			if err := json.Unmarshal(data, &record); err != nil || record.ConversationID == "" {
				continue
			}
			subID := record.ConversationID
			name := record.SubagentDescriptor.TypeName
			if record.SubagentDescriptor.Role != "" {
				if name != "" {
					name += ": " + record.SubagentDescriptor.Role
				} else {
					name = record.SubagentDescriptor.Role
				}
			}

			// Read subagent transcript for metadata if available
			transcriptPath := filepath.Join(confhome.Antigravity(), "brain", subID, ".system_generated", "logs", "transcript.jsonl")
			task := parseSubagentTaskRecord(subID, name, transcriptPath)
			if m.tasks.reg != nil {
				m.tasks.reg.Register(task)
			}
			if m.tasks.subagentManager != nil {
				m.tasks.subagentManager.Add(&subagent.Subagent{
					ID:     subID,
					Name:   name,
					Title:  name,
					State:  string(task.State),
					Model:  task.Model,
					Effort: task.Effort,
					Result: task.Output,
					Err:    task.Err,
				})
			}
		}
		break
	}
}

func parseSubagentTaskRecord(subID, name, transcriptPath string) *taskregistry.Task {
	task := &taskregistry.Task{
		ID:        subID,
		Name:      name,
		Kind:      taskregistry.KindSubagent,
		State:     taskregistry.StateDone,
		Model:     "Gemini 3.8 Flash (High)",
		Prompt:    name,
		StepCount: 2,
	}

	f, err := os.Open(transcriptPath)
	if err != nil {
		return task
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var firstTime, lastTime time.Time
	seenTools := make(map[string]bool)
	var stepCount int

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var r struct {
			StepIndex int    `json:"step_index"`
			Source    string `json:"source"`
			Type      string `json:"type"`
			Model     string `json:"model"`
			Content   string `json:"content"`
			CreatedAt string `json:"created_at"`
			ToolCalls []struct {
				Name string `json:"name"`
			} `json:"tool_calls"`
		}
		if err := json.Unmarshal(line, &r); err != nil {
			continue
		}
		stepCount++
		t, _ := time.Parse(time.RFC3339Nano, r.CreatedAt)
		if t.IsZero() {
			t, _ = time.Parse(time.RFC3339, r.CreatedAt)
		}
		if !t.IsZero() {
			if firstTime.IsZero() {
				firstTime = t
			}
			lastTime = t
		}
		if r.Model != "" {
			task.Model = r.Model
		}
		if r.StepIndex == 0 && r.Content != "" {
			if idx := strings.Index(r.Content, "content="); idx != -1 {
				promptText := r.Content[idx+8:]
				if endIdx := strings.Index(promptText, "\n</SYSTEM_MESSAGE>"); endIdx != -1 {
					promptText = promptText[:endIdx]
				}
				task.Prompt = strings.TrimSpace(promptText)
			} else {
				task.Prompt = strings.TrimSpace(r.Content)
			}
		}
		for _, tc := range r.ToolCalls {
			if tc.Name != "" && !seenTools[tc.Name] {
				seenTools[tc.Name] = true
				task.Tools = append(task.Tools, tc.Name)
			}
			task.Steps = append(task.Steps, tc.Name)
		}
		if r.Type == "PLANNER_RESPONSE" && r.Content != "" {
			task.Output = r.Content
		}
	}

	if stepCount > 0 {
		task.StepCount = stepCount
	}
	if !firstTime.IsZero() {
		task.StartedAt = firstTime
	}
	if !lastTime.IsZero() {
		task.EndedAt = lastTime
	}
	return task
}

func (m *RootModel) seedToolGroup(steps []convstore.Step, startIdx int) int {
	j := startIdx
	var group []toolsteps.Step
	for j < len(steps) && steps[j].Role() == "tool" {
		group = append(group, loadedToolStep(steps[j]))
		j++
	}
	verb := m.currentVerbosity()
	exp := m.effectiveExpanded(verb)
	block := toolsteps.RenderBlock(group, toolsteps.RenderOptions{
		Expanded:  exp,
		Width:     m.viewport.width,
		Verbosity: verb,
	})
	start := len(m.conversation.response)
	m.conversation.response = append(m.conversation.response, block...)
	m.conversation.blocks = append(m.conversation.blocks, transcript.NewBlock(transcript.Block{
		Start:    start,
		Lines:    len(block),
		Steps:    group,
		Expanded: exp,
	}, block, m.viewport.width, verb))
	return j
}

func (m *RootModel) seedNonToolStep(s convstore.Step) {
	switch s.Role() {
	case "user":
		if strings.TrimSpace(s.Text) == "" {
			return
		}
		m.session.appendMessage(backend.Message{Role: "user", Content: s.Text})
		for len(m.conversation.response) > 0 && strings.TrimSpace(m.conversation.response[len(m.conversation.response)-1]) == "" {
			m.conversation.response = m.conversation.response[:len(m.conversation.response)-1]
		}
		sep := transcript.TurnSeparator(m.viewport.width)
		if len(m.conversation.response) == 0 {
			m.conversation.response = append(m.conversation.response, sep)
		} else {
			m.conversation.response = append(m.conversation.response, "", sep)
		}
		start := len(m.conversation.response)
		promptLines := transcript.RenderUserPrompt(s.Text, m.viewport.width)
		m.conversation.response = append(m.conversation.response, promptLines...)
		m.conversation.userBlocks = append(m.conversation.userBlocks, transcript.UserBlock{
			Start: start,
			Lines: len(promptLines),
			Raw:   s.Text,
		})
		return
	case "model":
		if strings.TrimSpace(s.Text) == "" {
			return
		}
		styled := mdrender.RenderMarkdown(s.Text, m.viewport.width)
		lines := strings.Split(styled, "\n")
		start := len(m.conversation.response)
		m.conversation.response = append(m.conversation.response, lines...)
		m.session.appendMessage(backend.Message{Role: "assistant", Content: s.Text})
		m.conversation.assistants = append(m.conversation.assistants, transcript.AssistantBlock{Start: start, Lines: len(lines), Raw: s.Text})
	case "checkpoint":
		m.conversation.response = append(m.conversation.response, "  ⎿  Conversation compacted", "")
		return
	case "system":
		if strings.TrimSpace(s.Text) == "" {
			return
		}
		m.session.appendMessage(backend.Message{Role: "system", Content: s.Text})
		if isInternalSystemMessage(s.Text) {
			// Internal system messages (background tasks, subagent notifications)
			// belong to the model context only and must never leak into the visible transcript.
			return
		}
		m.conversation.response = append(m.conversation.response, "  "+tuilib.FirstLine(s.Text), "")
		return
	default:
		if isInternalSystemMessage(s.Text) {
			return
		}
		m.conversation.response = append(m.conversation.response, "  "+tuilib.FirstLine(s.Text))
	}
	m.conversation.response = append(m.conversation.response, "")
}

func isInternalSystemMessage(text string) bool {
	trimmed := strings.TrimSpace(text)
	return strings.HasPrefix(trimmed, "<SYSTEM_MESSAGE>") ||
		strings.HasPrefix(trimmed, "The following is a <SYSTEM_MESSAGE>") ||
		strings.Contains(trimmed, "<SYSTEM_MESSAGE>")
}

func loadedToolStep(s convstore.Step) toolsteps.Step {
	// A step that failed or was cancelled changed nothing: the
	// rehydration pass reads the same set of statuses the reference's
	// `StepIndicesExcludingStatuses` excludes.
	failed := !rehydration.Succeeded(s.Status)
	st := toolsteps.NewToolStep(s.ToolName, json.RawMessage(s.ToolArgs), s.ToolResult, failed)
	if s.ToolSummary != "" && (st.Summary == "" || st.Summary == s.ToolName) {
		st.Summary = s.ToolSummary
	}
	return st
}

func (m *RootModel) resumeConversation(meta convstore.ConversationMeta) tea.Cmd {
	if meta.ID != "" && meta.ID == m.session.conversationID {
		return nil
	}
	c, err := convstore.LoadConversation(meta.Path)
	if err != nil {
		m.conversation.response = append(m.conversation.response,
			"  Failed to load "+meta.ID+": "+err.Error())
		return nil
	}
	m.conversation.response = nil
	m.session.bumpEpoch()
	m.session.setMessages(nil)
	if sup := m.session.sup(); sup != nil {
		sup.Clear()
	}
	m.conversation.blocks = nil
	m.conversation.assistants = nil
	m.conversation.userBlocks = nil
	m.conversation.liveSteps = nil
	m.conversation.liveStepRuns = nil
	m.conversation.activeTool = ""
	m.conversation.replyStart = -1
	m.conversation.liveTextLines = 0
	m.conversation.liveReplyBuf = ""
	m.viewport.rowsCacheDirty = true
	m.viewport.rowsCache = wrapSlot{}
	m.viewport.rowsCacheN1 = wrapSlot{}
	m.viewport.rowsView = composedRows{}
	m.viewport.scroll = 0
	m.viewport.autoScroll = true
	if m.viewport.width <= 0 {
		if cols, rows, err := term.GetSize(int(os.Stdout.Fd())); err == nil && cols > 0 {
			m.viewport.width, m.viewport.height = cols, rows
		}
	}
	if m.viewport.width > 0 {
		mdrender.SetCurrentWidth(m.viewport.width)
	}
	m.seedConversation(c)
	if m.viewport.width > 0 {
		m.reflowTranscript(m.viewport.width)
	}
	m.session.conversationID = meta.ID
	if c != nil && c.ID != "" {
		m.session.conversationID = c.ID
	}
	m.session.autoTitle = ""
	return nil
}

func handleAtConversationMention(buf string) (bool, tea.Cmd) {
	query, ok := atconversation.ParseMention(buf)
	if !ok {
		return false, nil
	}
	matches, _ := atconversation.SearchMatches(atconversation.DefaultSearcher, nil, query)
	if len(matches) == 0 {
		return false, nil
	}
	return true, nil
}

// checkpointChatMessages turns the conversation's model turns into backend
// messages. The first turn is the checkpoint's resume block when one exists.
func checkpointChatMessages(turns []convstore.ChatTurn) []backend.Message {
	out := make([]backend.Message, 0, len(turns))
	for _, t := range turns {
		out = append(out, backend.Message{Role: t.Role, Content: t.Text})
	}
	return out
}
