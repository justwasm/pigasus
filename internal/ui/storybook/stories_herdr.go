package storybook

import (
	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/herdr"
)

// RegisterHerdrStories registers the Herdr terminal multiplexer story.
func RegisterHerdrStories(c *Catalog) {
	c.Register(Story{
		ID:          "organism-herdr-multiplexer",
		Name:        "Herdr (Terminal Multiplexer)",
		Level:       LevelOrganism,
		Category:    CategoryWorkspace,
		Description: "Go implementation of Herdr: a tmux-like terminal multiplexer with side tab panel, interactive PTY shell, and multi-session switching.",
		UpstreamSym: "vito/midterm.Terminal",
		States: []State{
			{ID: "live-pty", Name: "Interactive Live PTY", Description: "Live interactive shell: type real commands directly into the terminal"},
			{ID: "multi-tab", Name: "Multi-Tab Workspaces", Description: "Multiple concurrent panes with side tab status badges and unread activity"},
			{ID: "sidebar-focus", Name: "Sidebar Navigation", Description: "Side panel focused tab management mode with j/k selection"},
			{ID: "socket-compat", Name: "Herdr Socket & Agents", Description: "Herdr Unix domain socket IPC with live Crush and Claude agent state reporting"},
		},
		Factory: func() StoryInstance {
			return newHerdrStory()
		},
	})
}

type herdrStory struct {
	model *herdr.Model
	state int
	w     int
	h     int
}

func newHerdrStory() *herdrStory {
	s := &herdrStory{w: 90, h: 26}
	s.initModel()
	return s
}

func (s *herdrStory) initModel() {
	if s.model != nil {
		s.model.Manager.CloseAll()
	}
	s.model = herdr.NewModel(s.h, s.w)

	switch s.state {
	case 0:
		// Live Interactive PTY shell in current workspace
		_, _ = s.model.SpawnDefaultPane("shell", "")

	case 1:
		// Pre-seeded multi-workspace setup categorized by project directories with Git branches
		ws1 := s.model.Manager.ActiveWorkspace()
		ws1.Name = "openagy"
		ws1.Branch = "main"
		p1, _ := s.model.SpawnMockPane("sh", "sh-3.2$ git status\r\nOn branch main\r\nnothing to commit, working tree clean\r\nsh-3.2$ ")
		_, _ = s.model.SpawnMockPane("pigasus-srv", "[INFO] Listening on ws://127.0.0.1:8080/agentlink\r\n[INFO] 1 client attached (pigasus-tui)\r\n[TURN] Executing turn for session 00ac8418\r\n")
		_, _ = s.model.SpawnMockPane("test-runner", "=== RUN   TestHerdr_PaneMockLifecycle\r\n--- PASS: TestHerdr_PaneMockLifecycle (0.00s)\r\n=== RUN   TestHerdr_ManagerOperations\r\n--- PASS: TestHerdr_ManagerOperations (0.00s)\r\nPASS\r\n")

		ws2 := s.model.Manager.AddWorkspace("herdr", "/Users/gear/GitHub/herdr")
		ws2.Branch = "feat/sidebar"
		ws2.Ahead = 1
		_, _ = s.model.SpawnMockPane("cargo build", "Compiling herdr v0.9.5\r\nFinished release [optimized] target(s) in 3.42s\r\n$ ")

		ws3 := s.model.Manager.AddWorkspace("tuios", "/Users/gear/GitHub/tuios")
		ws3.Branch = "master"
		_, _ = s.model.SpawnMockPane("daemon", "2026-11-12 [INFO] Socket server listening on /tmp/tuios.sock\r\n")

		s.model.Manager.SelectWorkspace(0)
		if p1 != nil {
			s.model.Manager.SelectPane(0)
		}

	case 2:
		// Sidebar focused navigation mode across multiple project workspaces
		ws1 := s.model.Manager.ActiveWorkspace()
		ws1.Name = "openagy"
		ws1.Branch = "main"
		_, _ = s.model.SpawnMockPane("build", "$ make quick && make arch\r\nall rules hold: 324 packages pass\r\n$ ")

		ws2 := s.model.Manager.AddWorkspace("herdr", "/Users/gear/GitHub/herdr")
		ws2.Branch = "main"
		_, _ = s.model.SpawnMockPane("cargo test", "test result: ok. 142 passed; 0 failed\r\n")

		s.model.Manager.SelectWorkspace(0)
		s.model.Focus = herdr.FocusSidePanel

	case 3:
		// Herdr Agent Compatibility: agy, claude, codex, and crush recognized and rendered as multi-line cards
		s.model.Manager.SetSocketPath("/tmp/herdr-live.sock")

		ws1 := s.model.Manager.ActiveWorkspace()
		ws1.Name = "openagy"
		ws1.Branch = "main"

		// 1. Antigravity (agy) agent
		p1, _ := s.model.SpawnMockPane("agy-worker", "$ openagy --agent engineer\r\n[TURN] Analyzing AST-driven symbol extraction\r\n⠋ Refactoring herdr workspaces\r\n─── esc to cancel\r\n")
		if p1 != nil {
			p1.SetAgentState("agy", "working", "Refactoring cmd/herdr")
		}

		// 2. Claude agent
		p2, _ := s.model.SpawnMockPane("claude-worker", "$ claude-code --dangerously-skip-permissions\r\nClaude Code v2.1.228\r\nRequesting permission for run_command: rm -rf /tmp/scratch\r\nDo you want to proceed? [y/n]\r\n")
		if p2 != nil {
			p2.SetAgentState("claude", "blocked", "Awaiting confirmation")
		}

		// 3. Codex agent
		p3, _ := s.model.SpawnMockPane("codex-worker", "$ codex --model o3-mini\r\nCodex interactive session\r\nThinking... ⠋ Generating test suite\r\n")
		if p3 != nil {
			p3.SetAgentState("codex", "working", "Writing unit tests...")
		}

		// 4. Crush agent
		p4, _ := s.model.SpawnMockPane("crush-worker", "$ crush run --model claude-3-5-sonnet\r\n[crush] Step completed successfully.\r\n$ \r\n")
		if p4 != nil {
			p4.SetAgentState("crush", "idle", "Ready for prompt")
		}

		s.model.Manager.SelectWorkspace(0)
		s.model.Manager.SelectPane(0)
	}
}

func (s *herdrStory) SetState(stateIndex int) {
	s.state = stateIndex
	s.initModel()
}

func (s *herdrStory) SetSize(width, height int) {
	s.w = width
	s.h = height
	if s.model != nil {
		s.model.Update(tea.WindowSizeMsg{Width: width, Height: height})
	}
}

func (s *herdrStory) View() string {
	if s.model == nil {
		s.initModel()
	}
	return s.model.ViewString()
}

func (s *herdrStory) HandleKey(key tea.KeyMsg) (tea.Cmd, bool) {
	if s.model == nil {
		s.initModel()
	}
	_, cmd := s.model.Update(key)
	return cmd, true
}

func (s *herdrStory) HandlePaste(msg tea.PasteMsg) (tea.Cmd, bool) {
	if s.model == nil {
		s.initModel()
	}
	_, cmd := s.model.Update(msg)
	return cmd, true
}

func (s *herdrStory) HandleMouse(msg tea.MouseMsg) (tea.Cmd, bool) {
	if s.model == nil {
		s.initModel()
	}
	_, cmd := s.model.Update(msg)
	return cmd, true
}
