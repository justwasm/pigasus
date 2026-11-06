// conversation_seed.go — conversation seeding and @conversation mention logic.
//
// Which conversation a run continues, transcript seeding into runMode,
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

	"github.com/justwasm/pigasus/internal/app/tools"
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

func (m *runMode) seedConversation(c *convstore.Conversation) {
	if c == nil {
		return
	}
	if len(c.Steps) > 0 || c.ID != "" {
		m.conversationStarted = true
	}
	m.rehydrateConversation(c)
	steps := c.Transcript()
	for i := 0; i < len(steps); {
		if steps[i].Role() == "tool" {
			i = m.seedToolGroup(steps, i)
			continue
		}
		m.seedNonToolStep(steps[i])
		i++
	}
	// The trajectory starts as the conversation's own steps: the tool
	// calls the reference stored are part of it, and the turns that run
	// from here are appended to them.
	if m.supervisor != nil {
		m.supervisor.SetSeededConversation(c.Steps, len(m.session))
		m.supervisor.Replace(m.session)
	}
	m.trajLoaded = c.Steps
	m.trajCalls = nil
	m.trajSeededLen = len(m.session)
	m.rowsCacheDirty = true
	m.rowsGen++

	// Direction 2: Restore persisted queued prompts if any.
	convID := c.ID
	if convID == "" {
		convID = m.conversationID
	}
	if convID != "" {
		sStore := session.NewStore("")
		queued := sStore.LoadQueued(convID)
		if len(queued) > 0 {
			m.store.QueuedPrompt = queued[0]
			if len(queued) > 1 {
				m.store.StreamQueue = append(m.store.StreamQueue, queued[1:]...)
			}
		}

		// Restore durable active timer/cron schedules.
		m.restoreActiveSchedules(sStore, convID)
	}
}

// restoreActiveSchedules restores durable timer and cron schedules on conversation resume.
func (m *runMode) restoreActiveSchedules(sStore *session.Store, convID string) {
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
func (m *runMode) rehydrateConversation(c *convstore.Conversation) {
	if c == nil {
		return
	}
	convID := c.ID
	if convID == "" {
		convID = m.conversationID
	}
	// A fresh store: adopting a conversation replaces the previous
	// one's definitions, it does not add to them.
	m.subagentDefs = subagenttools.NewStore()
	state := &rehydration.State{
		ConversationID: convID,
		Definitions:    m.subagentDefs,
		Schedules:      shell.ScheduleTableFor(convID),
		Tasks:          m.taskReg,
	}
	rehydration.Restore(c.RehydrationSteps(), state)
}

func (m *runMode) seedToolGroup(steps []convstore.Step, startIdx int) int {
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
		Width:     m.width,
		Verbosity: verb,
	})
	start := len(m.response)
	m.response = append(m.response, block...)
	m.blocks = append(m.blocks, transcript.NewBlock(transcript.Block{
		Start:    start,
		Lines:    len(block),
		Steps:    group,
		Expanded: exp,
	}, block, m.width, verb))
	return j
}

func (m *runMode) seedNonToolStep(s convstore.Step) {
	switch s.Role() {
	case "user":
		if strings.TrimSpace(s.Text) == "" {
			return
		}
		m.session = append(m.session, backend.Message{Role: "user", Content: s.Text})
		for len(m.response) > 0 && strings.TrimSpace(m.response[len(m.response)-1]) == "" {
			m.response = m.response[:len(m.response)-1]
		}
		sep := transcript.TurnSeparator(m.width)
		if len(m.response) == 0 {
			m.response = append(m.response, sep)
		} else {
			m.response = append(m.response, "", sep)
		}
		start := len(m.response)
		promptLines := transcript.RenderUserPrompt(s.Text, m.width)
		m.response = append(m.response, promptLines...)
		m.userBlocks = append(m.userBlocks, transcript.UserBlock{
			Start: start,
			Lines: len(promptLines),
			Raw:   s.Text,
		})
		return
	case "model":
		if strings.TrimSpace(s.Text) == "" {
			return
		}
		styled := mdrender.RenderMarkdown(s.Text, m.width)
		lines := strings.Split(styled, "\n")
		start := len(m.response)
		m.response = append(m.response, lines...)
		m.session = append(m.session, backend.Message{Role: "assistant", Content: s.Text})
		m.assistants = append(m.assistants, transcript.AssistantBlock{Start: start, Lines: len(lines), Raw: s.Text})
	default:
		m.response = append(m.response, "  "+tuilib.FirstLine(s.Text))
	}
	m.response = append(m.response, "")
}

func loadedToolStep(s convstore.Step) toolsteps.Step {
	// A step that failed or was cancelled changed nothing: the
	// rehydration pass reads the same set of statuses the reference's
	// `StepIndicesExcludingStatuses` excludes.
	failed := !rehydration.Succeeded(s.Status)
	st := toolsteps.NewToolStep(s.ToolName, json.RawMessage(s.ToolArgs), s.ToolResult, failed)
	return st
}

func (m *runMode) resumeConversation(meta convstore.ConversationMeta) tea.Cmd {
	c, err := convstore.LoadConversation(meta.Path)
	if err != nil {
		m.response = append(m.response,
			"  Failed to load "+meta.ID+": "+err.Error())
		return nil
	}
	m.response = nil
	m.session = nil
	if m.supervisor != nil {
		m.supervisor.Clear()
	}
	m.blocks = nil
	m.assistants = nil
	m.userBlocks = nil
	m.liveSteps = nil
	m.liveStepRuns = nil
	m.activeTool = ""
	m.replyStart = -1
	m.liveTextLines = 0
	m.liveReplyBuf = ""
	m.rowsCacheDirty = true
	m.rowsCache = wrapSlot{}
	m.rowsCacheN1 = wrapSlot{}
	m.rowsView = composedRows{}
	m.scroll = 0
	m.autoScroll = true
	if m.width <= 0 {
		if cols, rows, err := term.GetSize(int(os.Stdout.Fd())); err == nil && cols > 0 {
			m.width, m.height = cols, rows
		}
	}
	if m.width > 0 {
		mdrender.SetCurrentWidth(m.width)
	}
	m.seedConversation(c)
	if m.width > 0 {
		m.reflowTranscript(m.width)
	}
	m.conversationID = meta.ID
	if c != nil && c.ID != "" {
		m.conversationID = c.ID
	}
	m.autoTitle = ""
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
