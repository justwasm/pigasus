package storybook

import (
	"bytes"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/app/session/rewind"
	"github.com/justwasm/pigasus/internal/features/auth"
	tuiviewer "github.com/justwasm/pigasus/internal/ui/foundations/viewer"
	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
	"github.com/justwasm/pigasus/internal/ui/resumepicker"
	"github.com/justwasm/pigasus/internal/ui/views/accountspanel"
	"github.com/justwasm/pigasus/internal/ui/views/changelog"
	"github.com/justwasm/pigasus/internal/ui/views/contextpanel"
	featdiff "github.com/justwasm/pigasus/internal/ui/views/diff/feat"
	"github.com/justwasm/pigasus/internal/ui/views/mcppanel"
	"github.com/justwasm/pigasus/internal/ui/views/onboarding"
	"github.com/justwasm/pigasus/internal/ui/views/permissionspanel"
	"github.com/justwasm/pigasus/internal/ui/views/pluginsui"
	"github.com/justwasm/pigasus/internal/ui/views/rewindpanel"
	"github.com/justwasm/pigasus/internal/ui/views/skillsui"
	"github.com/justwasm/pigasus/internal/ui/views/usagepanel"
)

// RegisterPanelsExtendedStories registers additional interactive panels and dialogs.
func RegisterPanelsExtendedStories(c *Catalog) {
	// 1. ContextPanel
	c.Register(Story{
		ID:          "panel-context",
		Name:        "ContextPanel (Token & Usage Grid)",
		Category:    CategoryPanels,
		Description: "Context window breakdown: 44-cell usage grid, system prompt, rules, skills, checkpoint buffer, and related commands.",
		UpstreamSym: "jetski/cli/panel.ContextPanel",
		States: []State{
			{ID: "empty", Name: "Empty Session", Description: "Awaiting first prompt turn"},
			{ID: "streaming", Name: "In-Flight Streaming", Description: "Active turn token counts updating in real-time"},
			{ID: "live", Name: "Live / Complete Turn", Description: "Full category breakdown with customization & checkpoint buffer"},
		},
		Factory: func() StoryInstance {
			inst := &contextPanelStory{width: 80, height: 24}
			inst.SetState(2)
			return inst
		},
	})

	// 2. UsagePanel
	c.Register(Story{
		ID:          "panel-usage",
		Name:        "UsagePanel (Model Quotas)",
		Category:    CategoryPanels,
		Description: "Rate limit quotas: Gemini 5-hour rolling limit, weekly bucket, countdown timer, and loading state.",
		UpstreamSym: "jetski/cli/panel.UsagePanel",
		States: []State{
			{ID: "summary", Name: "Active Quotas", Description: "Gemini and Claude tier usage bars with reset timers"},
			{ID: "loading", Name: "Loading Quota", Description: "Braille spinner while retrieving quota summary"},
			{ID: "stub-groups", Name: "Default Groups", Description: "Fallback static groups with frozen refresh times"},
		},
		Factory: func() StoryInstance {
			inst := &usagePanelStory{width: 80, height: 24}
			inst.SetState(0)
			return inst
		},
	})

	// 3. PluginsUI
	c.Register(Story{
		ID:          "panel-plugins",
		Name:        "PluginsUI (Marketplace & Installed)",
		Category:    CategoryPanels,
		Description: "Plugin manager with Installed and Discover marketplace tabs, search filter, and shelf layout.",
		UpstreamSym: "jetski/cli/panel.PluginPicker",
		States: []State{
			{ID: "installed", Name: "Installed Tab", Description: "Installed plugins with status badges and actions"},
			{ID: "discover", Name: "Discover Tab", Description: "Marketplace shelves: Featured, Popular, Tools"},
			{ID: "search", Name: "Search Filter", Description: "Instant keyword search results across catalog"},
		},
		Factory: func() StoryInstance {
			inst := &pluginsUIStory{width: 80, height: 24}
			inst.SetState(0)
			return inst
		},
	})

	// 4. SkillsUI
	c.Register(Story{
		ID:          "panel-skills",
		Name:        "SkillsUI (Skills Catalog & Detail)",
		Category:    CategoryPanels,
		Description: "Available skills grouped by root directory with interactive SKILL.md markdown detail viewer.",
		UpstreamSym: "jetski/cli/panel.SkillsPicker",
		States: []State{
			{ID: "catalog-list", Name: "Grouped Catalog", Description: "Workspace, builtin, and plugin skill roots"},
			{ID: "skill-detail", Name: "SKILL.md Detail", Description: "Interactive markdown documentation preview"},
		},
		Factory: func() StoryInstance {
			inst := &skillsUIStory{width: 80, height: 24}
			inst.SetState(0)
			return inst
		},
	})

	// 5. McpPanel
	c.Register(Story{
		ID:          "panel-mcp",
		Name:        "McpPanel (MCP Servers Frame)",
		Category:    CategoryPanels,
		Description: "Model Context Protocol servers frame: title, empty state frame, and navigation legend.",
		UpstreamSym: "jetski/cli/panel.McpPanel",
		States: []State{
			{ID: "empty", Name: "Empty Frame", Description: "No MCP servers configured state"},
		},
		Factory: func() StoryInstance {
			inst := &mcpPanelStory{width: 80, height: 24}
			inst.SetState(0)
			return inst
		},
	})

	// 6. PermissionsPanel
	c.Register(Story{
		ID:          "panel-permissions",
		Name:        "PermissionsPanel (Rule Config)",
		Category:    CategoryPanels,
		Description: "Permission rule editor across scopes (Project, Shared, Global) and lists (Allow, Deny, Ask).",
		UpstreamSym: "jetski/cli/panel.PermissionsPanel",
		States: []State{
			{ID: "scopes", Name: "Scopes Picker", Description: "Project, Shared with Pigasus, Global scopes"},
			{ID: "lists", Name: "Permission Lists", Description: "Allowlist, Denylist, Asklist tabs with rule counts"},
			{ID: "form", Name: "Add Rule Form", Description: "Action selection and target pattern input"},
		},
		Factory: func() StoryInstance {
			inst := &permissionsPanelStory{width: 80, height: 24}
			inst.SetState(0)
			return inst
		},
	})

	// 7. ResumePicker
	c.Register(Story{
		ID:          "panel-resume",
		Name:        "ResumePicker (Conversation Tree)",
		Category:    CategoryPanels,
		Description: "Interactive conversation tree grouped by workspace, with [CURRENT] indicator, timestamps, and search.",
		UpstreamSym: "jetski/cli/panel.ResumePicker",
		States: []State{
			{ID: "tree", Name: "Workspace Tree", Description: "Grouped conversations with timestamps and preview snippets"},
			{ID: "search", Name: "Search Filter", Description: "Instant search matching conversation titles"},
		},
		Factory: func() StoryInstance {
			inst := &resumePickerStory{width: 80, height: 24}
			inst.SetState(0)
			return inst
		},
	})

	// 8. RewindPanel
	c.Register(Story{
		ID:          "panel-rewind",
		Name:        "RewindPanel (Turn History Rollback)",
		Category:    CategoryPanels,
		Description: "Conversation turn rollback picker showing prompt history and rollback checkpoints.",
		UpstreamSym: "jetski/cli/panel.RewindPanel",
		States: []State{
			{ID: "turns", Name: "Turn Checkpoints", Description: "List of conversation turns newest first"},
			{ID: "empty", Name: "No User Turns", Description: "Initial turn before conversation starts"},
		},
		Factory: func() StoryInstance {
			inst := &rewindPanelStory{width: 80, height: 24}
			inst.SetState(0)
			return inst
		},
	})

	// 9. AccountsPanel
	c.Register(Story{
		ID:          "panel-accounts",
		Name:        "AccountsPanel (Multi-Account & Pool)",
		Category:    CategoryPanels,
		Description: "Account profile switcher, active authentication badge, and multi-account pool routing policy editor.",
		UpstreamSym: "pigasus/accounts.AccountsPanel",
		States: []State{
			{ID: "accounts-list", Name: "Accounts List", Description: "Active credentials, plan tiers, and sign-in status"},
			{ID: "pool-policy", Name: "Routing Policy", Description: "Account pool routing mode (Round-Robin, Failover)"},
		},
		Factory: func() StoryInstance {
			inst := &accountsPanelStory{width: 80, height: 24}
			inst.SetState(0)
			return inst
		},
	})

	// 10. OnboardingWizard
	c.Register(Story{
		ID:          "panel-onboarding",
		Name:        "OnboardingWizard (First-Launch FSM)",
		Category:    CategoryPanels,
		Description: "3-step interactive setup wizard: color scheme selection with Go code preview, terms of service, and migration.",
		UpstreamSym: "jetski/cli/onboarding.Wizard",
		States: []State{
			{ID: "color-scheme", Name: "Color Scheme (Side-by-Side)", Description: "Side-by-side layout with rounded preview card, diff line numbers, and syntax highlighting"},
			{ID: "color-scheme-narrow", Name: "Color Scheme (Narrow Window)", Description: "Adaptive narrow layout showing '(widen the terminal to see a preview)' hint"},
			{ID: "terms", Name: "Terms & Telemetry", Description: "Terms of Service and telemetry opt-in checkbox"},
			{ID: "migration", Name: "Legacy Migration", Description: "Legacy Gemini CLI configuration import checklist"},
		},
		Factory: func() StoryInstance {
			inst := &onboardingWizardStory{width: 90, height: 26}
			inst.SetState(0)
			return inst
		},
	})

	// 11. ChangelogPanel
	c.Register(Story{
		ID:          "panel-changelog",
		Name:        "ChangelogPanel (Release Notes)",
		Category:    CategoryPanels,
		Description: "Scrollable release notes document with version headers, release highlights, and Markdown formatting.",
		UpstreamSym: "jetski/cli/panel.ChangelogPanel",
		States: []State{
			{ID: "formatted", Name: "Formatted Document", Description: "Wrapped release notes ladder for current terminal size"},
		},
		Factory: func() StoryInstance {
			inst := &changelogPanelStory{width: 80, height: 24}
			inst.SetState(0)
			return inst
		},
	})

	// 12. FeatDiffPanel
	c.Register(Story{
		ID:          "panel-featdiff",
		Name:        "FeatureDiffPanel (/feature-diff)",
		Category:    CategoryPanels,
		Description: "Clean-room parity audit table comparing OpenAGY against upstream jetski/cortex 1.5.5.",
		UpstreamSym: "jetski/cli/panel.FeatDiff",
		States: []State{
			{ID: "feature-matrix", Name: "Feature Comparison Matrix", Description: "Categorized feature audit status, parity classification, and cycles."},
		},
		Factory: func() StoryInstance {
			inst := &featDiffPanelStory{width: 80, height: 24}
			return inst
		},
	})
}

// ---------------------------------------------------------------- ContextPanel Story

type contextPanelStory struct {
	state  int
	width  int
	height int
	offset int
}

func (s *contextPanelStory) SetState(idx int) {
	s.state = idx
	s.offset = 0
}

func (s *contextPanelStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *contextPanelStory) View() string {
	w := s.width
	if w <= 0 {
		w = 80
	}
	h := s.height
	if h <= 0 {
		h = 24
	}
	var st contextpanel.State
	switch s.state {
	case 0: // empty
		st = contextpanel.State{
			Model: "Gemini 3.8 Flash (High)",
			Width: w,
			Mode:  contextpanel.ModeEmpty,
		}
	case 1: // streaming
		st = contextpanel.State{
			Model: "Claude Sonnet 4.6",
			Width: w,
			Mode:  contextpanel.ModeStreaming,
			User:  1280,
			Agent: 2450,
			Tool:  890,
		}
	default: // live
		st = contextpanel.State{
			Model:            "Gemini 3.8 Flash (High)",
			Width:            w,
			Mode:             contextpanel.ModeLive,
			User:             3400,
			Agent:            6800,
			Tool:             2100,
			SystemPrompt:     71393,
			SystemTools:      4200,
			SystemOverhead:   1500,
			Rules:            2400,
			Skills:           1800,
			Subagents:        900,
			Workflows:        0,
			Plugins:          1200,
			MCPTools:         600,
			SystemMessages:   2,
			CheckpointBuffer: 15400,
		}
	}
	allLines := contextpanel.Lines(st)
	total := len(allLines)
	vis := h - 4
	if vis < 5 {
		vis = 5
	}
	win := tuiviewport.ScrollWindow(total, s.offset, vis)
	return strings.Join(win.SliceRows(allLines), "\n")
}

func (s *contextPanelStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	k := key.Key()
	switch k.Code {
	case tea.KeyUp, 'k':
		if s.offset > 0 {
			s.offset--
		}
		return nil, true
	case tea.KeyDown, 'j':
		s.offset++
		return nil, true
	case tea.KeyPgUp, 'b':
		s.offset -= 5
		if s.offset < 0 {
			s.offset = 0
		}
		return nil, true
	case tea.KeyPgDown, 'f', tea.KeySpace:
		s.offset += 5
		return nil, true
	}
	return nil, false
}

func (s *contextPanelStory) HandleMouse(msg tea.MouseMsg) (tea.Cmd, bool) {
	ev := msg.Mouse()
	switch ev.Button {
	case tea.MouseWheelUp:
		s.offset -= 3
		if s.offset < 0 {
			s.offset = 0
		}
		return nil, true
	case tea.MouseWheelDown:
		s.offset += 3
		return nil, true
	}
	return nil, false
}

// ---------------------------------------------------------------- UsagePanel Story

type usagePanelStory struct {
	state  int
	width  int
	height int
	window int
}

func (s *usagePanelStory) SetState(idx int) {
	s.state = idx
	s.window = 0
}

func (s *usagePanelStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *usagePanelStory) View() string {
	w := s.width
	if w <= 0 {
		w = 80
	}
	h := s.height
	if h <= 0 {
		h = 24
	}

	st := usagepanel.State{
		Email:  "developer@corp.com",
		Width:  w,
		Height: h,
	}

	switch s.state {
	case 1: // loading
		st.Loading = true
		st.Frame = 3
	case 2: // stub-groups
		st.RefreshG = time.Now().Add(158 * time.Hour)
		st.RefreshO = time.Now().Add(64 * time.Hour)
	default: // summary
		st.Summary = &auth.QuotaSummary{
			Groups: []auth.QuotaGroup{
				{
					Name:        "Gemini 3.8 Models",
					Description: "Gemini 3.8 Flash, Gemini 3.8 Pro",
					Buckets: []auth.QuotaBucket{
						{
							DisplayName:       "5-Hour Rolling Window",
							RemainingFraction: 0.75,
							ResetTime:         time.Now().Add(4*time.Hour + 12*time.Minute),
						},
						{
							DisplayName:       "Weekly Allocation",
							RemainingFraction: 0.90,
							ResetTime:         time.Now().Add(142 * time.Hour),
						},
					},
				},
				{
					Name:        "Claude & GPT Models",
					Description: "Claude Opus 4.6, Claude Sonnet 4.6, GPT-OSS",
					Buckets: []auth.QuotaBucket{
						{
							DisplayName:       "5-Hour Rolling Window",
							RemainingFraction: 0.15,
							ResetTime:         time.Now().Add(1*time.Hour + 45*time.Minute),
						},
					},
				},
			},
		}
	}

	lines, _ := usagepanel.Lines(st, s.window)
	return strings.Join(lines, "\n")
}

func (s *usagePanelStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	k := key.Key()
	switch k.Code {
	case tea.KeyUp, 'k':
		if s.window > 0 {
			s.window--
		}
		return nil, true
	case tea.KeyDown, 'j':
		s.window++
		return nil, true
	case tea.KeyPgUp, 'b':
		s.window -= 5
		if s.window < 0 {
			s.window = 0
		}
		return nil, true
	case tea.KeyPgDown, 'f', tea.KeySpace:
		s.window += 5
		return nil, true
	}
	return nil, false
}

func (s *usagePanelStory) HandleMouse(msg tea.MouseMsg) (tea.Cmd, bool) {
	ev := msg.Mouse()
	switch ev.Button {
	case tea.MouseWheelUp:
		s.window -= 3
		if s.window < 0 {
			s.window = 0
		}
		return nil, true
	case tea.MouseWheelDown:
		s.window += 3
		return nil, true
	}
	return nil, false
}

// ---------------------------------------------------------------- PluginsUI Story

type pluginsUIStory struct {
	state  int
	width  int
	height int
	panel  *pluginsui.Panel
}

func (s *pluginsUIStory) SetState(idx int) {
	s.state = idx
	s.panel = pluginsui.New(pluginsui.Seams{})
	switch idx {
	case 1: // discover
		s.panel.HandleKey(tea.Key{Code: tea.KeyTab})
	case 2: // search
		s.panel.HandleKey(tea.Key{Code: tea.KeyTab})
		s.panel.HandleKey(tea.Key{Code: '/'})
		s.panel.HandleKey(tea.Key{Code: 'd', Text: "d"})
		s.panel.HandleKey(tea.Key{Code: 'e', Text: "e"})
		s.panel.HandleKey(tea.Key{Code: 'v', Text: "v"})
	default: // installed
	}
}

func (s *pluginsUIStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *pluginsUIStory) View() string {
	if s.panel == nil {
		return ""
	}
	return strings.Join(s.panel.Lines(s.width), "\n")
}

func (s *pluginsUIStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	if s.panel != nil {
		handled, cmd := s.panel.HandleKey(key.Key())
		return cmd, handled
	}
	return nil, false
}

func (s *pluginsUIStory) HandleMouse(msg tea.MouseMsg) (tea.Cmd, bool) {
	if s.panel != nil {
		ev := msg.Mouse()
		switch ev.Button {
		case tea.MouseWheelDown:
			handled, cmd := s.panel.HandleKey(tea.Key{Code: tea.KeyDown})
			return cmd, handled
		case tea.MouseWheelUp:
			handled, cmd := s.panel.HandleKey(tea.Key{Code: tea.KeyUp})
			return cmd, handled
		}
	}
	return nil, false
}

// ---------------------------------------------------------------- SkillsUI Story

type skillsUIStory struct {
	state  int
	width  int
	height int
	panel  *skillsui.Panel
}

func (s *skillsUIStory) SetState(idx int) {
	s.state = idx
	cat := []skillsui.SkillInfo{
		{
			Name:        "bubbletea",
			Description: "Build terminal user interfaces with Go and Bubbletea framework",
			Path:        "/Users/gear/.gemini/skills/bubbletea/SKILL.md",
		},
		{
			Name:        "alphafold-database",
			Description: "Retrieve and analyze AlphaFold predicted structures for a protein",
			Path:        "/Users/gear/.gemini/config/plugins/science/skills/alphafold/SKILL.md",
		},
		{
			Name:        "cloudflare-workers",
			Description: "Comprehensive Cloudflare platform development and worker deployment",
			Path:        "/Users/gear/.gemini/skills/cloudflare/SKILL.md",
		},
	}

	w := s.width
	if w <= 0 {
		w = 80
	}
	h := s.height
	if h <= 0 {
		h = 24
	}

	s.panel = skillsui.New(skillsui.Seams{
		Catalog: func() []skillsui.SkillInfo { return cat },
		OSC8:    func() bool { return false },
		Size:    func() (int, int) { return w, h },
	})

	if idx == 1 {
		// Open detail view
		s.panel.HandleKey(tea.Key{Code: tea.KeyDown})
		s.panel.HandleKey(tea.Key{Code: tea.KeyEnter})
	}
}

func (s *skillsUIStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *skillsUIStory) View() string {
	if s.panel == nil {
		return ""
	}
	return strings.Join(s.panel.Lines(), "\n")
}

func (s *skillsUIStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	if s.panel != nil {
		handled, cmd := s.panel.HandleKey(key.Key())
		return cmd, handled
	}
	return nil, false
}

func (s *skillsUIStory) HandleMouse(msg tea.MouseMsg) (tea.Cmd, bool) {
	if s.panel != nil {
		ev := msg.Mouse()
		switch ev.Button {
		case tea.MouseWheelDown:
			handled, cmd := s.panel.HandleKey(tea.Key{Code: tea.KeyDown})
			return cmd, handled
		case tea.MouseWheelUp:
			handled, cmd := s.panel.HandleKey(tea.Key{Code: tea.KeyUp})
			return cmd, handled
		}
	}
	return nil, false
}

// ---------------------------------------------------------------- McpPanel Story

type mcpPanelStory struct {
	state  int
	width  int
	height int
}

func (s *mcpPanelStory) SetState(idx int) {
	s.state = idx
}

func (s *mcpPanelStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *mcpPanelStory) View() string {
	return strings.Join(mcppanel.Lines(), "\n")
}

func (s *mcpPanelStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	return nil, false
}

// ---------------------------------------------------------------- PermissionsPanel Story

type permissionsPanelStory struct {
	state  int
	width  int
	height int
	panel  *permissionspanel.Panel
}

func (s *permissionsPanelStory) SetState(idx int) {
	s.state = idx
	s.panel = permissionspanel.New(permissionspanel.Seams{})
	switch idx {
	case 1:
		// Advance to lists tab
		s.panel.HandleKey(tea.Key{Code: tea.KeyEnter})
	case 2:
		// Advance to lists tab then open form
		s.panel.HandleKey(tea.Key{Code: tea.KeyEnter})
		s.panel.HandleKey(tea.Key{Code: 'a', Text: "a"})
	default:
		// Scopes
	}
}

func (s *permissionsPanelStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *permissionsPanelStory) View() string {
	if s.panel == nil {
		return ""
	}
	return strings.Join(s.panel.Lines(), "\n")
}

func (s *permissionsPanelStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	if s.panel != nil {
		handled, cmd := s.panel.HandleKey(key.Key())
		return cmd, handled
	}
	return nil, false
}

func (s *permissionsPanelStory) HandleMouse(msg tea.MouseMsg) (tea.Cmd, bool) {
	if s.panel != nil {
		ev := msg.Mouse()
		switch ev.Button {
		case tea.MouseWheelDown:
			handled, cmd := s.panel.HandleKey(tea.Key{Code: tea.KeyDown})
			return cmd, handled
		case tea.MouseWheelUp:
			handled, cmd := s.panel.HandleKey(tea.Key{Code: tea.KeyUp})
			return cmd, handled
		}
	}
	return nil, false
}

// ---------------------------------------------------------------- ResumePicker Story

type resumePickerStory struct {
	state  int
	width  int
	height int
	picker *resumepicker.Picker
}

func (s *resumePickerStory) SetState(idx int) {
	s.state = idx
	convs := []convstore.ConversationMeta{
		{
			ID:        "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
			Path:      "/Users/gear/.gemini/antigravity-cli/conversations/a1b2c3d4.db",
			Title:     "Align Language Server Protos with CL 1017595969",
			Project:   "openagy",
			StepCount: 14,
			ModTime:   time.Now().Add(-15 * time.Minute),
		},
		{
			ID:        "b2c3d4e5-f6a7-8901-bcde-f12345678901",
			Path:      "/Users/gear/.gemini/antigravity-cli/conversations/b2c3d4e5.db",
			Title:     "Implement Antigravity TUI Component Storybook",
			Project:   "openagy",
			StepCount: 28,
			ModTime:   time.Now().Add(-2 * time.Hour),
		},
		{
			ID:        "c3d4e5f6-a7b8-9012-cdef-123456789012",
			Path:      "/Users/gear/.gemini/antigravity-cli/conversations/c3d4e5f6.db",
			Title:     "Cloudflare Workers AI Agent Integration",
			Project:   "cf-agents",
			StepCount: 8,
			ModTime:   time.Now().Add(-24 * time.Hour),
		},
	}

	s.picker = resumepicker.New(resumepicker.Seams{
		CurrentConversationID: func() string { return "a1b2c3d4-e5f6-7890-abcd-ef1234567890" },
		CurrentWorkspace:      func() string { return "/Users/gear/GitHub/openagy" },
		ListConversations:     func() ([]convstore.ConversationMeta, error) { return convs, nil },
	})
	_ = s.picker.Open()

	if idx == 1 {
		// Type search query
		h := s.height
		if h <= 0 {
			h = 24
		}
		s.picker.HandleKey(tea.Key{Code: 's'}, h)
		s.picker.HandleKey(tea.Key{Code: 't'}, h)
		s.picker.HandleKey(tea.Key{Code: 'o'}, h)
		s.picker.HandleKey(tea.Key{Code: 'r'}, h)
		s.picker.HandleKey(tea.Key{Code: 'y'}, h)
	}
}

func (s *resumePickerStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *resumePickerStory) View() string {
	if s.picker == nil {
		return ""
	}
	w := s.width
	if w <= 0 {
		w = 80
	}
	h := s.height
	if h <= 0 {
		h = 24
	}
	return s.picker.View(w, h)
}

func (s *resumePickerStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	if s.picker != nil {
		h := s.height
		if h <= 0 {
			h = 24
		}
		return s.picker.HandleKey(key.Key(), h), true
	}
	return nil, false
}

func (s *resumePickerStory) HandleMouse(msg tea.MouseMsg) (tea.Cmd, bool) {
	if s.picker != nil {
		h := s.height
		if h <= 0 {
			h = 24
		}
		ev := msg.Mouse()
		switch ev.Button {
		case tea.MouseWheelDown:
			return s.picker.HandleKey(tea.Key{Code: tea.KeyDown}, h), true
		case tea.MouseWheelUp:
			return s.picker.HandleKey(tea.Key{Code: tea.KeyUp}, h), true
		}
	}
	return nil, false
}

// ---------------------------------------------------------------- RewindPanel Story

type rewindPanelStory struct {
	state  int
	width  int
	height int
	panel  *rewindpanel.Panel
}

func (s *rewindPanelStory) SetState(idx int) {
	s.state = idx
	var steps []rewind.Step
	if idx == 0 {
		steps = []rewind.Step{
			{UserIdx: 4, Content: "Expand Storybook with newly scanned TUI components"},
			{UserIdx: 2, Content: "Align language server RPC methods with CL 1017595969"},
			{UserIdx: 0, Content: "Initial prompt: analyze protocol differences in 1.5.5"},
		}
	}
	s.panel = rewindpanel.New(rewindpanel.Seams{
		Steps: func() []rewind.Step { return steps },
		Height: func() int {
			if s.height <= 0 {
				return 24
			}
			return s.height
		},
	})
}

func (s *rewindPanelStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *rewindPanelStory) View() string {
	if s.panel == nil {
		return ""
	}
	return strings.Join(s.panel.Lines(), "\n")
}

func (s *rewindPanelStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	if s.panel != nil {
		handled, cmd := s.panel.HandleKey(key.Key())
		return cmd, handled
	}
	return nil, false
}

func (s *rewindPanelStory) HandleMouse(msg tea.MouseMsg) (tea.Cmd, bool) {
	if s.panel != nil {
		ev := msg.Mouse()
		switch ev.Button {
		case tea.MouseWheelDown:
			handled, cmd := s.panel.HandleKey(tea.Key{Code: tea.KeyDown})
			return cmd, handled
		case tea.MouseWheelUp:
			handled, cmd := s.panel.HandleKey(tea.Key{Code: tea.KeyUp})
			return cmd, handled
		}
	}
	return nil, false
}

// ---------------------------------------------------------------- AccountsPanel Story

type accountsPanelStory struct {
	state  int
	width  int
	height int
	panel  *accountspanel.Panel
}

func (s *accountsPanelStory) SetState(idx int) {
	s.state = idx
	s.panel = accountspanel.New(accountspanel.Seams{})
	if idx == 1 {
		s.panel.SetTab(accountspanel.TabPool)
	} else {
		s.panel.SetTab(accountspanel.TabAccounts)
	}
}

func (s *accountsPanelStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *accountsPanelStory) View() string {
	if s.panel == nil {
		return ""
	}
	return strings.Join(s.panel.Lines(), "\n")
}

func (s *accountsPanelStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	if s.panel != nil {
		handled, cmd := s.panel.HandleKey(key.Key())
		return cmd, handled
	}
	return nil, false
}

// ---------------------------------------------------------------- OnboardingWizard Story

type onboardingWizardStory struct {
	state  int
	width  int
	height int
	wizard *onboarding.Wizard
}

func (s *onboardingWizardStory) SetState(idx int) {
	s.state = idx
	s.wizard = onboarding.NewWizard()
	s.wizard.Width = s.width
	s.wizard.Height = s.height
	switch idx {
	case 1:
		s.wizard.Step = onboarding.StepColorScheme
		s.wizard.Width = 60 // Narrow window mode
	case 2:
		s.wizard.Step = onboarding.StepTerms
	case 3:
		s.wizard.Step = onboarding.StepMigration
	default:
		s.wizard.Step = onboarding.StepColorScheme
	}
}

func (s *onboardingWizardStory) SetSize(w, h int) {
	s.width, s.height = w, h
	if s.wizard != nil {
		if s.state == 1 {
			s.wizard.Width = 60
		} else {
			s.wizard.Width = w
		}
		s.wizard.Height = h
	}
}

func (s *onboardingWizardStory) View() string {
	if s.wizard == nil {
		return ""
	}
	var buf bytes.Buffer
	_ = s.wizard.Render(&buf)
	return buf.String()
}

func (s *onboardingWizardStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	if s.wizard == nil {
		return nil, false
	}
	k := key.Key()
	switch k.Code {
	case '+', '=':
		s.wizard.Width += 5
		return nil, true
	case '-', '_':
		if s.wizard.Width > 40 {
			s.wizard.Width -= 5
		}
		return nil, true
	case tea.KeyUp, 'k':
		s.wizard.MoveSelection(-1)
		return nil, true
	case tea.KeyDown, 'j':
		s.wizard.MoveSelection(+1)
		return nil, true
	case tea.KeySpace:
		s.wizard.HandleSpace()
		return nil, true
	case tea.KeyEnter:
		s.wizard.HandleEnter()
		return nil, true
	case '1', '2', '3', '4', '5', '6', '7', '8':
		idx := int(k.Code - '1')
		if idx >= 0 && idx < onboarding.ColorSchemeCount {
			s.wizard.Selected = onboarding.ColorScheme(idx)
			return nil, true
		}
	default:
		if s.wizard.HandleKey(key.String()) {
			return nil, true
		}
	}
	return nil, false
}

func (s *onboardingWizardStory) HandleMouse(msg tea.MouseMsg) (tea.Cmd, bool) {
	if s.wizard == nil {
		return nil, false
	}
	ev := msg.Mouse()
	switch ev.Button {
	case tea.MouseWheelDown:
		s.wizard.MoveSelection(+1)
		return nil, true
	case tea.MouseWheelUp:
		s.wizard.MoveSelection(-1)
		return nil, true
	}
	return nil, false
}

// ---------------------------------------------------------------- ChangelogPanel Story

type changelogPanelStory struct {
	state  int
	width  int
	height int
	offset int
}

func (s *changelogPanelStory) SetState(idx int) {
	s.state = idx
	s.offset = 0
}

func (s *changelogPanelStory) SetSize(w, h int) {
	s.width, s.height = w, h
}

func (s *changelogPanelStory) View() string {
	w := s.width
	if w <= 0 {
		w = 80
	}
	h := s.height
	if h <= 0 {
		h = 24
	}
	doc := changelog.Document(w, h)
	total := len(doc)
	vis := changelog.VisibleRows(h)
	win := tuiviewport.ScrollWindow(total, s.offset, vis)
	rows := win.SliceRows(doc)
	lines := changelog.PanelLines(rows, win.Start(), win.End(), total)
	return strings.Join(lines, "\n")
}

func (s *changelogPanelStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	w := s.width
	if w <= 0 {
		w = 80
	}
	h := s.height
	if h <= 0 {
		h = 24
	}
	doc := changelog.Document(w, h)
	total := len(doc)
	vis := changelog.VisibleRows(h)

	k := key.Key()
	switch k.Code {
	case tea.KeyUp, 'k':
		if s.offset > 0 {
			s.offset--
		}
		return nil, true
	case tea.KeyDown, 'j':
		if s.offset < total-vis {
			s.offset++
		}
		return nil, true
	case tea.KeyPgUp, 'b', 'u':
		s.offset -= vis
		if s.offset < 0 {
			s.offset = 0
		}
		return nil, true
	case tea.KeyPgDown, 'f', 'd', tea.KeySpace:
		s.offset += vis
		if s.offset > total-vis {
			s.offset = max(0, total-vis)
		}
		return nil, true
	case tea.KeyHome, 'g':
		s.offset = 0
		return nil, true
	case tea.KeyEnd, 'G':
		s.offset = max(0, total-vis)
		return nil, true
	}
	return nil, false
}

func (s *changelogPanelStory) HandleMouse(msg tea.MouseMsg) (tea.Cmd, bool) {
	w := s.width
	if w <= 0 {
		w = 80
	}
	h := s.height
	if h <= 0 {
		h = 24
	}
	doc := changelog.Document(w, h)
	total := len(doc)
	vis := changelog.VisibleRows(h)

	ev := msg.Mouse()
	switch ev.Button {
	case tea.MouseWheelDown:
		if s.offset < total-vis {
			s.offset = min(total-vis, s.offset+3)
		}
		return nil, true
	case tea.MouseWheelUp:
		if s.offset > 0 {
			s.offset = max(0, s.offset-3)
		}
		return nil, true
	}
	return nil, false
}

// ---------------------------------------------------------------- FeatDiffPanel Story

type featDiffPanelStory struct {
	width  int
	height int
	offset int
}

func (s *featDiffPanelStory) SetState(idx int) {
	s.offset = 0
}
func (s *featDiffPanelStory) SetSize(w, h int) { s.width, s.height = w, h }
func (s *featDiffPanelStory) View() string {
	lines := featdiff.PanelLines()
	h := s.height
	if h <= 0 {
		h = 24
	}
	vis := h - 4
	if vis < 5 {
		vis = 5
	}
	total := len(lines)
	win := tuiviewport.ScrollWindow(total, s.offset, vis)
	rows := win.SliceRows(lines)
	rows = append(rows, "", tuiviewer.Counter(win.Start(), win.End(), total))
	return strings.Join(rows, "\n")
}
func (s *featDiffPanelStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	lines := featdiff.PanelLines()
	h := s.height
	if h <= 0 {
		h = 24
	}
	vis := h - 4
	if vis < 5 {
		vis = 5
	}
	total := len(lines)

	k := key.Key()
	switch k.Code {
	case tea.KeyUp, 'k':
		if s.offset > 0 {
			s.offset--
		}
		return nil, true
	case tea.KeyDown, 'j':
		if s.offset < total-vis {
			s.offset++
		}
		return nil, true
	case tea.KeyPgUp, 'b':
		s.offset -= vis
		if s.offset < 0 {
			s.offset = 0
		}
		return nil, true
	case tea.KeyPgDown, 'f', tea.KeySpace:
		s.offset += vis
		if s.offset > total-vis {
			s.offset = max(0, total-vis)
		}
		return nil, true
	case tea.KeyHome, 'g':
		s.offset = 0
		return nil, true
	case tea.KeyEnd, 'G':
		s.offset = max(0, total-vis)
		return nil, true
	}
	return nil, false
}

func (s *featDiffPanelStory) HandleMouse(msg tea.MouseMsg) (tea.Cmd, bool) {
	lines := featdiff.PanelLines()
	h := s.height
	if h <= 0 {
		h = 24
	}
	vis := h - 4
	if vis < 5 {
		vis = 5
	}
	total := len(lines)

	ev := msg.Mouse()
	switch ev.Button {
	case tea.MouseWheelDown:
		if s.offset < total-vis {
			s.offset = min(total-vis, s.offset+3)
		}
		return nil, true
	case tea.MouseWheelUp:
		if s.offset > 0 {
			s.offset = max(0, s.offset-3)
		}
		return nil, true
	}
	return nil, false
}
