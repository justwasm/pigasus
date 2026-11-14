package storybook

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	subagentquestions "github.com/justwasm/pigasus/internal/app/subagent/questions"
	taskregistry "github.com/justwasm/pigasus/internal/app/task"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/features/agents"
	"github.com/justwasm/pigasus/internal/ui/views/activeitemspanel"
	"github.com/justwasm/pigasus/internal/ui/views/agentspanel"
	"github.com/justwasm/pigasus/internal/ui/views/renamepanel"
	"github.com/justwasm/pigasus/internal/ui/views/slashquote"
	"github.com/justwasm/pigasus/internal/ui/views/subagentquestionpanel"
)

// -----------------------------------------------------------------------------
// 1. AgentsPanel Story
// -----------------------------------------------------------------------------

type agentsPanelStory struct {
	width, height int
	stateIdx      int
	panel         *agentspanel.Panel
}

func newAgentsPanelStory() Story {
	return Story{
		ID:          "panel-agents",
		Name:        "Agents Panel (/agents)",
		Category:    CategoryPanels,
		Description: "Markdown agents catalogue, running subagent supervisor, and termination / authorization actions.",
		UpstreamSym: "jetski/cli/panel.AgentsPanel",
		States: []State{
			{ID: "catalog", Name: "Agents Catalogue", Description: "List of workspace markdown agents and built-in defaults."},
			{ID: "running-subagents", Name: "Running Subagents & Keys", Description: "Active subagents with Kill (k) and Authorization (A/D) hotkeys."},
		},
		Factory: func() StoryInstance {
			s := &agentsPanelStory{width: 80, height: 24}
			s.rebuild()
			return s
		},
	}
}

func (s *agentsPanelStory) rebuild() {
	catalogue := []agents.Agent{
		{
			Name:        "code-reviewer",
			Description: "Rigorous code review against Go idioms, concurrency safety, and layering rules",
		},
		{
			Name:        "sql-optimizer",
			Description: "Database query planner, index analyzer, and migration advisor",
		},
		{
			Name:        "doc-writer",
			Description: "Generates high-fidelity markdown documentation and architectural cycle reports",
		},
	}
	s.panel = agentspanel.New(agentspanel.Seams{}, catalogue)
	if s.stateIdx == 1 {
		s.panel.HandleKey(tea.Key{Code: tea.KeyDown})
	}
}

func (s *agentsPanelStory) SetState(idx int) {
	s.stateIdx = idx
	s.rebuild()
}

func (s *agentsPanelStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *agentsPanelStory) View() string {
	if s.panel == nil {
		return ""
	}
	lines := s.panel.Lines()
	return strings.Join(lines, "\n")
}

func (s *agentsPanelStory) HandleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if s.panel == nil {
		return nil, false
	}
	handled, cmd := s.panel.HandleKey(msg.Key())
	return cmd, handled
}

// -----------------------------------------------------------------------------
// 2. SubagentQuestionPanel Story
// -----------------------------------------------------------------------------

type subagentQuestionStory struct {
	width, height int
	stateIdx      int
	panel         *subagentquestionpanel.Panel
	manager       *subagentquestions.Manager
}

func newSubagentQuestionStory() Story {
	return Story{
		ID:          "panel-subagent-question",
		Name:        "Subagent Question Panel",
		Category:    CategoryPanels,
		Description: "Interactive clarification question prompt from subagents with left/right option selector.",
		UpstreamSym: "jetski/cli/panel.SubAgentQuestionPanel",
		States: []State{
			{ID: "picker-overlay", Name: "Question Picker Overlay", Description: "Subagent question overlay with options and arrow navigation."},
			{ID: "option-select", Name: "Option Walk & Check", Description: "Cursor moved to target option with active selection indicator."},
			{ID: "status-alert", Name: "Status Line Alert Row", Description: "Compact alert notification in status line when agent has question."},
		},
		Factory: func() StoryInstance {
			s := &subagentQuestionStory{width: 80, height: 24}
			s.rebuild()
			return s
		},
	}
}

func (s *subagentQuestionStory) rebuild() {
	s.manager = subagentquestions.NewManager(false)
	s.manager.Enqueue(
		subagentquestions.ParentKey,
		"Which storage engine should the session cache migrate to?",
		[]string{"In-Memory SQLite", "Durable Objects KV", "PostgreSQL 16 (Local)"},
		false,
	)

	s.panel = subagentquestionpanel.New(subagentquestionpanel.Seams{
		Manager: s.manager,
	})
	s.panel.SetWidth(s.width)

	if s.stateIdx == 0 {
		s.panel.Toggle()
	} else if s.stateIdx == 1 {
		s.panel.Toggle()
		s.panel.HandleKey(tea.Key{Code: tea.KeyRight})
	}
}

func (s *subagentQuestionStory) SetState(idx int) {
	s.stateIdx = idx
	s.rebuild()
}

func (s *subagentQuestionStory) SetSize(w, h int) {
	s.width, s.height = w, h
	if s.panel != nil {
		s.panel.SetWidth(w)
	}
}

func (s *subagentQuestionStory) View() string {
	if s.panel == nil {
		return ""
	}
	if s.stateIdx == 2 {
		return strings.Join(s.panel.AlertRows(), "\n")
	}
	return strings.Join(s.panel.PickerRows(), "\n")
}

func (s *subagentQuestionStory) HandleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if s.panel == nil {
		return nil, false
	}
	handled, cmd := s.panel.HandleKeyMsg(msg.Key())
	return cmd, handled
}

// -----------------------------------------------------------------------------
// 3. ActiveItemsPanel Story
// -----------------------------------------------------------------------------

type activeItemsStory struct {
	width, height int
	stateIdx      int
	panel         *activeitemspanel.Panel
	registry      *taskregistry.Registry
}

func newActiveItemsStory() Story {
	return Story{
		ID:          "status-active-items",
		Name:        "Active Items Block (Prompt Tray)",
		Category:    CategoryStatus,
		Description: "Compact active tasks, background shell commands, and subagent status tray under input box.",
		UpstreamSym: "jetski/cli.ActiveItems",
		States: []State{
			{ID: "running-tray", Name: "Running Tasks Tray", Description: "Background processes and subagents with status dots and elapsed time."},
			{ID: "focused-selection", Name: "Focused Cursor Selection", Description: "Focused active item with navigation indicator (>)."},
		},
		Factory: func() StoryInstance {
			s := &activeItemsStory{width: 80, height: 24}
			s.rebuild()
			return s
		},
	}
}

func (s *activeItemsStory) rebuild() {
	s.registry = taskregistry.New()
	s.registry.Register(&taskregistry.Task{
		ID:        "task-1",
		Name:      "go test -v ./...",
		Kind:      taskregistry.KindShell,
		State:     taskregistry.StateRunning,
		StartedAt: time.Now().Add(-14 * time.Second),
		Command:   "go test -v ./...",
	})
	s.registry.Register(&taskregistry.Task{
		ID:        "task-2",
		Name:      "subagent: codebase-auditor",
		Kind:      taskregistry.KindSubagent,
		State:     taskregistry.StateRunning,
		StartedAt: time.Now().Add(-48 * time.Second),
		Model:     "claude-sonnet-4-6",
	})
	s.registry.Register(&taskregistry.Task{
		ID:        "task-3",
		Name:      "npm run build:storybook",
		Kind:      taskregistry.KindShell,
		State:     taskregistry.StateRunning,
		StartedAt: time.Now().Add(-4 * time.Second),
		Command:   "npm run build:storybook",
	})

	s.panel = activeitemspanel.New(activeitemspanel.Seams{
		Registry: func() *taskregistry.Registry { return s.registry },
		Width:    func() int { return s.width },
		Height:   func() int { return s.height },
	})

	if s.stateIdx == 1 {
		s.panel.SetFocus(true)
		s.panel.SelectFirst()
	}
}

func (s *activeItemsStory) SetState(idx int) {
	s.stateIdx = idx
	s.rebuild()
}

func (s *activeItemsStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *activeItemsStory) View() string {
	if s.panel == nil {
		return ""
	}
	return s.panel.Rows()
}

func (s *activeItemsStory) HandleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if s.panel == nil {
		return nil, false
	}
	handled, cmd := s.panel.HandleKey(msg.Key())
	return cmd, handled
}

// -----------------------------------------------------------------------------
// 4. RenamePanel Story
// -----------------------------------------------------------------------------

type renamePanelStory struct {
	width, height int
	stateIdx      int
	panel         *renamepanel.Panel
}

func newRenamePanelStory() Story {
	return Story{
		ID:          "panel-rename",
		Name:        "Rename Conversation (/rename)",
		Category:    CategoryPanels,
		Description: "Single-line interactive textinput dialog to rename the current session title.",
		UpstreamSym: "jetski/cli/panel.RenamePanel",
		States: []State{
			{ID: "editing-title", Name: "Editing Title", Description: "Textinput prompt pre-filled with candidate conversation title."},
			{ID: "empty-title", Name: "Empty Title Input", Description: "Initial blank rename dialog with placeholder."},
		},
		Factory: func() StoryInstance {
			s := &renamePanelStory{width: 80, height: 24}
			s.rebuild()
			return s
		},
	}
}

func (s *renamePanelStory) rebuild() {
	s.panel = renamepanel.New(renamepanel.Seams{})
	if s.stateIdx == 0 {
		s.panel.SetValue("feat: complete storybook TUI coverage")
	} else {
		s.panel.SetValue("")
	}
}

func (s *renamePanelStory) SetState(idx int) {
	s.stateIdx = idx
	s.rebuild()
}

func (s *renamePanelStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *renamePanelStory) View() string {
	if s.panel == nil {
		return ""
	}
	lines := s.panel.Lines()
	return strings.Join(lines, "\n")
}

func (s *renamePanelStory) HandleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if s.panel == nil {
		return nil, false
	}
	handled, cmd := s.panel.HandleKey(msg.Key())
	return cmd, handled
}

// -----------------------------------------------------------------------------
// 5. SlashQuote Story
// -----------------------------------------------------------------------------

type slashQuoteStory struct {
	width, height int
	stateIdx      int
}

func newSlashQuoteStory() Story {
	return Story{
		ID:          "status-slash-quote",
		Name:        "Quote Turn (/quote)",
		Category:    CategoryStatus,
		Description: "Turn quotation resolution and clean citation insertion into the prompt composer.",
		UpstreamSym: "jetski/cli.QuoteTurn",
		States: []State{
			{ID: "quoted-user-turn", Name: "Quoted User Turn", Description: "Resolved previous turn citation ready for inline quoting."},
			{ID: "multiline-quote", Name: "Multiline Clean Insertion", Description: "Multi-line code snippet quoting without phantom trailing newlines."},
		},
		Factory: func() StoryInstance {
			return &slashQuoteStory{width: 80, height: 24}
		},
	}
}

func (s *slashQuoteStory) SetState(idx int) {
	s.stateIdx = idx
}

func (s *slashQuoteStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *slashQuoteStory) View() string {
	var rawContent string
	if s.stateIdx == 0 {
		rawContent = "Can you optimize the regex pattern in internal/features/tools/codesearch?"
	} else {
		rawContent = "```go\nfunc ResolveQuote(arg string, sess []backend.Message) (string, string) {\n\t// strips trailing newlines\n}\n```\n"
	}

	cleaned := slashquote.InsertText(rawContent)
	msgs := []backend.Message{
		{Role: "user", Content: cleaned},
	}
	res, errText := slashquote.ResolveQuote("1", msgs)

	var b strings.Builder
	b.WriteString(bold("Turn Quotation Preview (/quote 1)") + "\n")
	b.WriteString(dim("────────────────────────────────────────────────────────────") + "\n")
	if errText != "" {
		b.WriteString(red("Error: " + errText) + "\n")
	} else {
		b.WriteString(blue("Quoted Content:") + "\n")
		lines := strings.Split(res, "\n")
		for _, line := range lines {
			b.WriteString("  " + dim("│ ") + line + "\n")
		}
		b.WriteString("\n" + green("✓ Inserted cleanly into composer without trailing phantom newlines.") + "\n")
	}
	return b.String()
}

func (s *slashQuoteStory) HandleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	return nil, false
}

// RegisterCollaborationStories mounts all agent collaboration and session management stories into the catalogue.
func RegisterCollaborationStories(c *Catalog) {
	c.Register(newAgentsPanelStory())
	c.Register(newSubagentQuestionStory())
	c.Register(newActiveItemsStory())
	c.Register(newRenamePanelStory())
	c.Register(newSlashQuoteStory())
}

