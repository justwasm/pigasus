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
package cliapp

import (
	"encoding/json"
	"flag"
	"io"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/atconversation"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/cliflags"
	"github.com/justwasm/pigasus/internal/convstore"
	"github.com/justwasm/pigasus/internal/rehydration"
	"github.com/justwasm/pigasus/internal/subagenttools"
	"github.com/justwasm/pigasus/internal/tools"
)

const (
	conversationHistoryTurns = convstore.DefaultHistoryTurns
	conversationHistoryChars = convstore.DefaultHistoryChars
)

// conversationSelection is an alias for convstore.Selection.
type conversationSelection = convstore.Selection

// resolveConversationSelection decides what a run continues, printing the
// reference's warning on warn when an explicitly named conversation is missing.
func resolveConversationSelection(fs *flag.FlagSet, warn io.Writer) conversationSelection {
	convID := cliflags.StringValue(fs, "conversation")
	cont := cliflags.ResolveBoolAlias(fs, "continue", "c")
	return convstore.ResolveSelection(convID, cont, workspaceDir(), warn)
}

func loadConversationSelection(id, source, path string) conversationSelection {
	return convstore.LoadSelection(id, source, path)
}

func workspaceDir() string {
	if d, err := os.Getwd(); err == nil {
		return d
	}
	return ""
}

func (m *runMode) seedConversation(c *convstore.Conversation) {
	if c == nil {
		return
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
	m.trajLoaded = c.Steps
	m.trajCalls = nil
	m.trajSeededLen = len(m.session)
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
		Schedules:      tools.ScheduleTableFor(convID),
		Tasks:          m.taskReg,
	}
	rehydration.Restore(c.RehydrationSteps(), state)
}

func (m *runMode) seedToolGroup(steps []convstore.Step, startIdx int) int {
	j := startIdx
	var group []toolStep
	for j < len(steps) && steps[j].Role() == "tool" {
		group = append(group, loadedToolStep(steps[j]))
		j++
	}
	block := toolBlockLines(group, m.expanded, m.width)
	start := len(m.response)
	m.response = append(m.response, block...)
	m.blocks = append(m.blocks, turnBlock{start: start, lines: len(block), steps: group})
	return j
}

func (m *runMode) seedNonToolStep(s convstore.Step) {
	switch s.Role() {
	case "user":
		m.session = append(m.session, backend.Message{Role: "user", Content: s.Text})
		return
	case "model":
		if strings.TrimSpace(s.Text) == "" {
			return
		}
		styled := renderMarkdown(s.Text, m.width)
		lines := strings.Split(styled, "\n")
		start := len(m.response)
		m.response = append(m.response, lines...)
		m.session = append(m.session, backend.Message{Role: "assistant", Content: s.Text})
		m.assistants = append(m.assistants, assistantBlock{start: start, lines: len(lines), raw: s.Text})
	default:
		m.response = append(m.response, "  "+firstLine(s.Text))
	}
	m.response = append(m.response, "")
}

func loadedToolStep(s convstore.Step) toolStep {
	st := newToolStep(s.ToolName, json.RawMessage(s.ToolArgs), s.ToolResult, loadedStepErr(s.Status))
	return st
}

func loadedStepErr(status int64) bool {
	switch status {
	case 4, 6, 7:
		return true
	default:
		return false
	}
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
	m.blocks = nil
	m.liveSteps = nil
	m.activeTool = ""
	m.seedConversation(c)
	m.conversationID = meta.ID
	if c != nil && c.ID != "" {
		m.conversationID = c.ID
	}
	m.autoTitle = ""
	return nil
}

const (
	atConversationMentionPrefix = atconversation.MentionPrefix
	atConversationTimeout       = atconversation.Timeout
	atConversationMaxMatches    = atconversation.MaxMatches
)

type AtConversationSource = atconversation.Source

const (
	atSourceRemote        = atconversation.SourceRemote
	atSourceLocalFallback = atconversation.SourceLocalFallback
	atSourceEmpty         = atconversation.SourceEmpty
)

type atConversationMatch = atconversation.Match

type atConversationLocalFn = atconversation.LocalFn

type atConversationSearchFn = atconversation.SearchFn

var defaultAtConversationSearcher = atconversation.DefaultSearcher

func parseAtConversationMention(buf string) (string, bool) {
	return atconversation.ParseMention(buf)
}

func searchAtConversationMatches(searcher atConversationSearchFn, local atConversationLocalFn, query string) ([]atConversationMatch, AtConversationSource) {
	return atconversation.SearchMatches(searcher, local, query)
}

func listLocalAtConversationMatches(query string) []atConversationMatch {
	return atconversation.ListLocalMatches(query)
}

func handleAtConversationMention(buf string) (bool, tea.Cmd) {
	query, ok := parseAtConversationMention(buf)
	if !ok {
		return false, nil
	}
	matches, _ := searchAtConversationMatches(defaultAtConversationSearcher, nil, query)
	if len(matches) == 0 {
		return false, nil
	}
	return true, nil
}

func atConversationMentionInsert(id string) string {
	return atconversation.MentionInsert(id)
}
