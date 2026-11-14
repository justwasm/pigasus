package storybook

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	taskregistry "github.com/justwasm/pigasus/internal/app/task"
	"github.com/justwasm/pigasus/internal/ui/views/askquestion"
	"github.com/justwasm/pigasus/internal/ui/views/diffpanel"
	"github.com/justwasm/pigasus/internal/ui/views/effortpanel"
	"github.com/justwasm/pigasus/internal/ui/views/helppanel"
	"github.com/justwasm/pigasus/internal/ui/views/modelpicker"
	"github.com/justwasm/pigasus/internal/ui/views/taskspanel"
)

// RegisterPanelsStories registers interactive full-screen and overlay panels.
func RegisterPanelsStories(c *Catalog) {
	// 1. ModelPicker
	c.Register(Story{
		ID:          "panel-model-picker",
		Name:        "ModelPicker (Switch Model)",
		Category:    CategoryPanels,
		Description: "Interactive model switcher with subsequence search filter and embedded effort slider.",
		UpstreamSym: "jetski/cli/panel.ModelPicker",
		States: []State{
			{ID: "default", Name: "Default Catalogue", Description: "Standard reference models (Gemini, Claude, GPT)"},
			{ID: "filtered-claude", Name: "Filtered ('claude')", Description: "Subsequence filtered to Claude models"},
			{ID: "custom-models", Name: "Custom Providers", Description: "Configured models from gateway / providers"},
			{ID: "no-matches", Name: "No Matches ('xyz')", Description: "Empty search query result"},
		},
		Factory: func() StoryInstance {
			inst := &modelPickerStory{width: 80, height: 24}
			inst.SetState(0)
			return inst
		},
	})

	// 2. EffortPicker
	c.Register(Story{
		ID:          "panel-effort-picker",
		Name:        "EffortPicker (Reasoning Effort)",
		Category:    CategoryPanels,
		Description: "Horizontal slider for model reasoning effort levels (low, medium, high).",
		UpstreamSym: "jetski/cli/panel.EffortPicker",
		States: []State{
			{ID: "low", Name: "Low Effort", Description: "Quick responses with minimal thinking"},
			{ID: "medium", Name: "Medium Effort", Description: "Balanced reasoning effort"},
			{ID: "high", Name: "High Effort", Description: "Deep reasoning with expanded thinking"},
			{ID: "unadjustable", Name: "Not Adjustable", Description: "Model that does not publish reasoning tiers"},
		},
		Factory: func() StoryInstance {
			inst := &effortPickerStory{width: 80}
			inst.SetState(2) // Default to high
			return inst
		},
	})

	// 3. TasksPanel
	c.Register(Story{
		ID:          "panel-tasks",
		Name:        "TasksPanel (Background Tasks)",
		Category:    CategoryPanels,
		Description: "Background work overview with shell, subagent, and timer tasks plus detail views.",
		UpstreamSym: "jetski/cli/panel.TasksPanel",
		States: []State{
			{ID: "active", Name: "Active Tasks", Description: "Multiple running and pending background tasks"},
			{ID: "completed", Name: "Mixed / Completed", Description: "Successful and failed completed tasks"},
			{ID: "empty", Name: "Empty Task List", Description: "No background tasks running"},
		},
		Factory: func() StoryInstance {
			inst := &tasksPanelStory{width: 80, height: 24}
			inst.SetState(0)
			return inst
		},
	})

	// 4. HelpPanel
	c.Register(Story{
		ID:          "panel-help",
		Name:        "HelpPanel (Help & Docs Overlay)",
		Category:    CategoryPanels,
		Description: "Multi-tab documentation overlay: General, Commands, Shortcuts, and Vim cheat-sheet.",
		UpstreamSym: "jetski/cli/panel.HelpPanel",
		States: []State{
			{ID: "general", Name: "General Tab", Description: "Overview, version, workspace info"},
			{ID: "commands", Name: "Commands Tab", Description: "All available slash commands with descriptions"},
			{ID: "shortcuts", Name: "Shortcuts Tab", Description: "Keyboard navigation and hotkey mappings"},
			{ID: "vim", Name: "Vim Mode Tab", Description: "Vim navigation and operator cheatsheet"},
		},
		Factory: func() StoryInstance {
			inst := &helpPanelStory{width: 80, height: 24}
			inst.SetState(0)
			return inst
		},
	})

	// 5. DiffPanel
	c.Register(Story{
		ID:          "panel-diff",
		Name:        "DiffPanel (Working Copy Inspector)",
		Category:    CategoryPanels,
		Description: "VCS diff explorer showing modified/untracked files and interactive inline diffs with hunk folding.",
		UpstreamSym: "jetski/cli/panel.DiffPanel",
		States: []State{
			{ID: "vcs-files", Name: "All Changes Tab", Description: "Tracked modified and untracked workspace files"},
			{ID: "inline-diff", Name: "Inline File Diff", Description: "Interactive hunk-by-hunk diff view"},
			{ID: "no-vcs", Name: "No VCS Directory", Description: "Workspace without git repository"},
		},
		Factory: func() StoryInstance {
			inst := &diffPanelStory{width: 80, height: 24}
			inst.SetState(0)
			return inst
		},
	})

	// 6. AskQuestionForm
	c.Register(Story{
		ID:          "form-ask-question",
		Name:        "AskQuestionForm (Interactive Questionnaire)",
		Category:    CategoryPanels,
		Description: "Interactive decision interview form supporting single-choice, multi-select, and write-in text.",
		UpstreamSym: "jetski/cli.AskQuestionForm",
		States: []State{
			{ID: "single-select", Name: "Single Choice", Description: "Radio options with a single selection"},
			{ID: "multi-select", Name: "Multi Select", Description: "Checkboxes allowing multiple options"},
			{ID: "write-in", Name: "Write-In Active", Description: "Custom input field activated"},
		},
		Factory: func() StoryInstance {
			inst := &askQuestionStory{width: 80}
			inst.SetState(0)
			return inst
		},
	})
}

// -----------------------------------------------------------------------------
// 1. ModelPicker Story
// -----------------------------------------------------------------------------

type modelPickerStory struct {
	state  int
	width  int
	height int
	panel  *modelpicker.Panel
	custom []modelpicker.Row
}

func (s *modelPickerStory) SetState(idx int) {
	s.state = idx
	s.custom = nil

	currentModel := "Gemini 3.8 Flash"
	if idx == 2 {
		s.custom = []modelpicker.Row{
			{
				Display:     "MiniMax M3",
				Slug:        "minimax-m3",
				Tiers:       []string{"low", "medium", "high"},
				DefaultTier: "high",
			},
			{
				Display: "GPT-6.1 Sol",
				Slug:    "gpt-6.1-sol",
				Tiers:   []string{"low", "medium", "high", "xhigh"},
			},
			{
				Display: "Claude Opus 4.6 (Thinking)",
				Slug:    "claude-opus-4.6-thinking",
			},
		}
		currentModel = "MiniMax M3"
	}

	s.panel = modelpicker.New(modelpicker.Seams{
		CurrentModelLabel: func() string { return currentModel },
		CustomRows: func() []modelpicker.Row {
			return s.custom
		},
	})

	switch idx {
	case 1:
		s.panel.HandleKey(tea.Key{Code: 'c'})
		s.panel.HandleKey(tea.Key{Code: 'l'})
		s.panel.HandleKey(tea.Key{Code: 'a'})
		s.panel.HandleKey(tea.Key{Code: 'u'})
	case 3:
		s.panel.HandleKey(tea.Key{Code: 'x'})
		s.panel.HandleKey(tea.Key{Code: 'y'})
		s.panel.HandleKey(tea.Key{Code: 'z'})
	}
}

func (s *modelPickerStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *modelPickerStory) View() string {
	if s.panel == nil {
		return ""
	}
	lines := s.panel.Lines()
	return strings.Join(lines, "\n")
}

func (s *modelPickerStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	if s.panel != nil {
		handled, cmd := s.panel.HandleKey(key.Key())
		return cmd, handled
	}
	return nil, false
}

// -----------------------------------------------------------------------------
// 2. EffortPicker Story
// -----------------------------------------------------------------------------

type effortPickerStory struct {
	state int
	width int
	panel *effortpanel.Panel
}

func (s *effortPickerStory) SetState(idx int) {
	s.state = idx
	tier := "high"
	var tiers []string = []string{"low", "medium", "high"}

	switch idx {
	case 0:
		tier = "low"
	case 1:
		tier = "medium"
	case 2:
		tier = "high"
	case 3:
		tiers = nil
		tier = ""
	}

	s.panel = effortpanel.New(effortpanel.Seams{
		Tiers:   func() []string { return tiers },
		Current: func() string { return tier },
	})
}

func (s *effortPickerStory) SetSize(w, _ int) {
	s.width = w
}

func (s *effortPickerStory) View() string {
	if s.panel == nil {
		return ""
	}
	lines := s.panel.Lines()
	return strings.Join(lines, "\n")
}

func (s *effortPickerStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	if s.panel != nil {
		handled, cmd := s.panel.HandleKey(key.Key())
		return cmd, handled
	}
	return nil, false
}

// -----------------------------------------------------------------------------
// 3. TasksPanel Story
// -----------------------------------------------------------------------------

type tasksPanelStory struct {
	state  int
	width  int
	height int
	panel  *taskspanel.Panel
	reg    *taskregistry.Registry
}

func (s *tasksPanelStory) SetState(idx int) {
	s.state = idx
	s.reg = taskregistry.New()

	now := time.Now()
	switch idx {
	case 0: // Active tasks
		s.reg.Register(&taskregistry.Task{
			ID:        "t-shell-1",
			Name:      "npm run build:prod",
			Kind:      taskregistry.KindShell,
			State:     taskregistry.StateRunning,
			StartedAt: now.Add(-45 * time.Second),
		})
		s.reg.Register(&taskregistry.Task{
			ID:        "t-subagent-1",
			Name:      "Architecture Researcher",
			Kind:      taskregistry.KindSubagent,
			State:     taskregistry.StateRunning,
			StartedAt: now.Add(-120 * time.Second),
		})
		s.reg.Register(&taskregistry.Task{
			ID:        "t-timer-1",
			Name:      "Heartbeat & Healthcheck",
			Kind:      taskregistry.KindTimer,
			State:     taskregistry.StatePending,
			StartedAt: now.Add(-10 * time.Second),
		})
	case 1: // Completed / mixed
		s.reg.Register(&taskregistry.Task{
			ID:        "t-done-1",
			Name:      "Lint and typecheck",
			Kind:      taskregistry.KindShell,
			State:     taskregistry.StateDone,
			ExitCode:  0,
			StartedAt: now.Add(-300 * time.Second),
		})
		s.reg.Register(&taskregistry.Task{
			ID:        "t-failed-1",
			Name:      "Docker image pull",
			Kind:      taskregistry.KindShell,
			State:     taskregistry.StateFailed,
			ExitCode:  1,
			StartedAt: now.Add(-180 * time.Second),
		})
	case 2: // Empty
		// No tasks
	}

	s.panel = taskspanel.New(taskspanel.Seams{
		Registry: func() *taskregistry.Registry { return s.reg },
		Size:     func() (int, int) { return s.width, s.height },
	})
}

func (s *tasksPanelStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *tasksPanelStory) View() string {
	if s.panel == nil {
		return ""
	}
	lines := s.panel.Lines()
	return strings.Join(lines, "\n")
}

func (s *tasksPanelStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	if s.panel != nil {
		handled, cmd := s.panel.HandleKey(key.Key())
		return cmd, handled
	}
	return nil, false
}

// -----------------------------------------------------------------------------
// 4. HelpPanel Story
// -----------------------------------------------------------------------------

type helpPanelStory struct {
	state  int
	width  int
	height int
	panel  *helppanel.Panel
}

func (s *helpPanelStory) SetState(idx int) {
	s.state = idx
	s.panel = helppanel.New(helppanel.Seams{
		Model:          func() string { return "Gemini 3.8 Flash" },
		Tier:           func() string { return "high" },
		Cwd:            func() string { return "/Users/openagy/workspace" },
		Email:          func() string { return "dev@openagy.org" },
		ConversationID: func() string { return "storybook-preview-001" },
		Width:          func() int { return s.width },
		Height:         func() int { return s.height },
		VimEnabled:     func() bool { return true },
	})
	s.panel.Open(idx)
}

func (s *helpPanelStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *helpPanelStory) View() string {
	if s.panel == nil {
		return ""
	}
	return s.panel.Rows()
}

func (s *helpPanelStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	if s.panel != nil {
		handled, cmd := s.panel.Update(key.Key())
		return cmd, handled
	}
	return nil, false
}

// -----------------------------------------------------------------------------
// 5. DiffPanel Story
// -----------------------------------------------------------------------------

type diffPanelStory struct {
	state  int
	width  int
	height int
	panel  *diffpanel.Panel
}

func (s *diffPanelStory) SetState(idx int) {
	s.state = idx

	tracked := []diffpanel.Change{
		{Path: "internal/ui/storybook/model.go", Added: 42, Deleted: 8},
		{Path: "internal/ui/storybook/sidebar.go", Added: 15, Deleted: 2},
		{Path: "internal/ui/storybook/canvas.go", Added: 88, Deleted: 0},
	}
	untracked := []diffpanel.Change{
		{Path: "cmd/storybook/main.go", Added: 35, Deleted: 0},
	}

	body := []string{
		"diff --git a/internal/ui/storybook/model.go b/internal/ui/storybook/model.go",
		"index 89abc12..def4567 100644",
		"--- a/internal/ui/storybook/model.go",
		"+++ b/internal/ui/storybook/model.go",
		"@@ -45,6 +45,8 @@ type Model struct {",
		" 	sidebar   *Sidebar",
		" 	canvas    *Canvas",
		"+	filter    string",
		"+	themeIdx  int",
		" 	focusArea FocusArea",
		" }",
		"@@ -80,7 +82,7 @@ func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {",
		"-	return m, nil",
		"+	return m.handleInput(msg)",
		" }",
	}

	vcs := "git"
	if idx == 2 {
		vcs = ""
	}

	s.panel = diffpanel.New(diffpanel.Seams{
		Cwd:   func() string { return "/Users/openagy/workspace" },
		Size:  func() (int, int) { return s.width, s.height },
		VCS:   func() string { return vcs },
		Turns: func() int { return 3 },
		Changes: func() ([]diffpanel.Change, []diffpanel.Change, bool) {
			if idx == 2 {
				return nil, nil, false
			}
			return tracked, untracked, true
		},
		Body: func(target, kind string) ([]string, bool) {
			return body, true
		},
	})

	if idx == 1 {
		// Open inline diff for first file
		s.panel.HandleKey(tea.Key{Code: tea.KeyEnter})
	}
}

func (s *diffPanelStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *diffPanelStory) View() string {
	if s.panel == nil {
		return ""
	}
	lines := s.panel.Lines()
	return strings.Join(lines, "\n")
}

func (s *diffPanelStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	if s.panel != nil {
		handled, cmd := s.panel.HandleKey(key.Key())
		return cmd, handled
	}
	return nil, false
}

// -----------------------------------------------------------------------------
// 6. AskQuestionForm Story
// -----------------------------------------------------------------------------

type askQuestionStory struct {
	state int
	width int
	model *askquestion.Model
}

func (s *askQuestionStory) SetState(idx int) {
	s.state = idx

	questions := []askquestion.Question{
		{
			Question: "How should the Storybook components be organized?",
			Options: []string{
				"(Recommended) Group by architecture tier and UI functional domain",
				"Flat alphabetized catalog listing",
				"Mirror the internal/ui file tree directory structure",
			},
			IsMultiSelect: false,
		},
		{
			Question: "Select verification targets to run before release:",
			Options: []string{
				"Architecture boundary enforcement (make arch)",
				"Full test suite across internal/ui and cmd (make test)",
				"Binary compilation check (make build)",
				"IME double-width CJK cursor regression test",
			},
			IsMultiSelect: true,
		},
	}

	w := s.width
	if w <= 0 {
		w = 70
	}
	s.model = askquestion.New(questions, w)

	switch idx {
	case 1:
		// Switch to question 2 (multi-select)
		s.model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	case 2:
		// Activate write-in
		s.model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		s.model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		s.model.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // to write-in row
		s.model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	}
}

func (s *askQuestionStory) SetSize(w, _ int) {
	s.width = w
	if s.model != nil {
		s.model.SetWidth(w)
	}
}

func (s *askQuestionStory) View() string {
	if s.model == nil {
		return ""
	}
	return s.model.Render()
}

func (s *askQuestionStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	if s.model != nil {
		k := key.Key()
		press := tea.KeyPressMsg{
			Code: k.Code,
			Text: k.Text,
		}
		newModel, cmd := s.model.Update(press)
		if m, ok := newModel.(*askquestion.Model); ok {
			s.model = m
		}
		return cmd, true
	}
	return nil, false
}
