package herdr

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestHerdr_PaneMockLifecycle(t *testing.T) {
	p, err := NewPane("pane-1", "test-term", "", 10, 40, WithMockSession("Welcome to Herdr Mock Terminal!\r\n$ "))
	if err != nil {
		t.Fatalf("unexpected error creating mock pane: %v", err)
	}
	defer p.Close()

	if p.Title != "test-term" {
		t.Errorf("title = %q, want test-term", p.Title)
	}

	lines := p.Lines()
	if len(lines) != 10 {
		t.Fatalf("expected 10 lines, got %d", len(lines))
	}
	if !strings.Contains(lines[0], "Welcome to Herdr") {
		t.Errorf("expected welcome message on line 0, got %q", lines[0])
	}

	// Write command
	n, err := p.Write([]byte("echo hello\r"))
	if err != nil || n == 0 {
		t.Fatalf("write failed: %v", err)
	}

	lines2 := p.Lines()
	joined := strings.Join(lines2, "\n")
	if !strings.Contains(joined, "echo hello") {
		t.Errorf("expected echo hello in buffer, got:\n%s", joined)
	}

	// Test Resize
	p.Resize(15, 60)
	if len(p.Lines()) != 15 {
		t.Errorf("expected 15 lines after resize, got %d", len(p.Lines()))
	}
}

func TestHerdr_ManagerOperations(t *testing.T) {
	mgr := NewManager(10, 40)
	if mgr.Count() != 0 {
		t.Fatalf("expected 0 panes, got %d", mgr.Count())
	}

	p1, err := mgr.AddPane("term-1", "", WithMockSession("session 1"))
	if err != nil {
		t.Fatalf("AddPane failed: %v", err)
	}
	if mgr.Count() != 1 || mgr.ActiveIndex() != 0 {
		t.Fatalf("expected 1 pane active at 0, got count %d, active %d", mgr.Count(), mgr.ActiveIndex())
	}

	p2, err := mgr.AddPane("term-2", "", WithMockSession("session 2"))
	if err != nil {
		t.Fatalf("AddPane 2 failed: %v", err)
	}
	if mgr.Count() != 2 || mgr.ActiveIndex() != 1 {
		t.Fatalf("expected 2 panes active at 1, got count %d, active %d", mgr.Count(), mgr.ActiveIndex())
	}

	// Next / Prev cycling
	mgr.NextPane()
	if mgr.ActiveIndex() != 0 {
		t.Errorf("NextPane expected active 0, got %d", mgr.ActiveIndex())
	}
	mgr.PrevPane()
	if mgr.ActiveIndex() != 1 {
		t.Errorf("PrevPane expected active 1, got %d", mgr.ActiveIndex())
	}

	// Select specific pane
	if !mgr.SelectPane(0) || mgr.Active() != p1 {
		t.Errorf("SelectPane(0) failed")
	}

	// Close pane
	mgr.CloseCurrent()
	if mgr.Count() != 1 || mgr.Active() != p2 {
		t.Errorf("expected 1 pane remaining (p2), got %d, active: %v", mgr.Count(), mgr.Active())
	}

	mgr.CloseAll()
	if mgr.Count() != 0 {
		t.Errorf("expected 0 panes after CloseAll, got %d", mgr.Count())
	}
}

func TestHerdr_KeymapEncoding(t *testing.T) {
	cases := []struct {
		name string
		key  tea.KeyMsg
		want []byte
	}{
		{
			name: "enter",
			key:  tea.KeyPressMsg{Code: tea.KeyEnter},
			want: []byte("\r"),
		},
		{
			name: "backspace",
			key:  tea.KeyPressMsg{Code: tea.KeyBackspace},
			want: []byte{0x7f},
		},
		{
			name: "tab",
			key:  tea.KeyPressMsg{Code: tea.KeyTab},
			want: []byte("\t"),
		},
		{
			name: "arrow up",
			key:  tea.KeyPressMsg{Code: tea.KeyUp},
			want: []byte("\x1b[A"),
		},
		{
			name: "ctrl+c",
			key:  tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl},
			want: []byte{0x03},
		},
		{
			name: "text string",
			key:  tea.KeyPressMsg{Code: 'a', Text: "a"},
			want: []byte("a"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := EncodeKeyToPTY(tc.key)
			if string(got) != string(tc.want) {
				t.Errorf("EncodeKeyToPTY(%q) = %q, want %q", tc.name, string(got), string(tc.want))
			}
		})
	}
}

func TestHerdr_ModelInteractivePrefixAndNavigation(t *testing.T) {
	m := NewModel(20, 80)
	p1, err := m.SpawnMockPane("sh", "sh-3.2$ ")
	if err != nil {
		t.Fatalf("failed to spawn mock pane: %v", err)
	}
	defer p1.Close()

	view0 := m.ViewString()
	if !strings.Contains(view0, "spaces") || !strings.Contains(view0, "agents") {
		t.Errorf("view missing expected spaces or agents sidebar sections:\n%s", view0)
	}

	// 1. Arm prefix with Ctrl+B
	ctrlB := tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl}
	m.Update(ctrlB)
	if !m.PrefixArmed {
		t.Errorf("expected prefix to be armed after Ctrl+B")
	}

	// Check view shows prefix indicator
	viewPrefix := m.ViewString()
	if !strings.Contains(viewPrefix, "PREFIX") {
		t.Errorf("expected view to reflect armed prefix:\n%s", viewPrefix)
	}

	// 2. Trigger Tab via prefix to switch focus to side panel
	tabKey := tea.KeyPressMsg{Code: tea.KeyTab}
	m.Update(tabKey)
	if m.Focus != FocusSidePanel {
		t.Errorf("expected focus to switch to FocusSidePanel, got %v", m.Focus)
	}
	if m.PrefixArmed {
		t.Errorf("prefix should be disarmed after command")
	}

	// 3. Switch back to terminal focus via Tab
	m.Update(tabKey)
	if m.Focus != FocusTerminal {
		t.Errorf("expected focus to return to FocusTerminal, got %v", m.Focus)
	}

	// 4. Test typing in terminal focus
	textKey := tea.KeyPressMsg{Code: 'l', Text: "ls\r"}
	m.Update(textKey)

	// Wait briefly for mock echo
	time.Sleep(10 * time.Millisecond)
	viewAfterType := m.ViewString()
	if !strings.Contains(viewAfterType, "ls") {
		t.Errorf("expected typed text in view:\n%s", viewAfterType)
	}
}

func TestHerdr_AgentBadgeRendering(t *testing.T) {
	m := NewModel(20, 80)
	p1, err := m.SpawnMockPane("worker-1", "ready\r\n")
	if err != nil {
		t.Fatalf("failed to spawn mock pane: %v", err)
	}
	defer p1.Close()

	p1.SetAgentState("crush", "working", "Running build tests")
	view := m.ViewString()
	if !strings.Contains(view, "crush") || !strings.Contains(view, "Running build") {
		t.Errorf("expected agent badge and message in view:\n%s", view)
	}
}

func TestHerdr_CmdVPaste(t *testing.T) {
	m := NewModel(20, 80)
	p1, err := m.SpawnMockPane("sh", "$ ")
	if err != nil {
		t.Fatalf("failed to spawn mock pane: %v", err)
	}
	defer p1.Close()

	// Simulate bracketed paste event emitted by terminal on Cmd+V
	pasteMsg := tea.PasteMsg{Content: "echo pasted_code\r"}
	m.Update(pasteMsg)

	time.Sleep(10 * time.Millisecond)
	view := m.ViewString()
	if !strings.Contains(view, "echo pasted_code") {
		t.Errorf("expected pasted content in terminal view:\n%s", view)
	}
}

func TestHerdr_MouseClickInteraction(t *testing.T) {
	m := NewModel(20, 80)
	_, _ = m.SpawnMockPane("tab-1", "tab 1\r\n")
	_, _ = m.SpawnMockPane("tab-2", "tab 2\r\n")

	// Render view so hit map is populated
	_ = m.ViewString()
	hits := m.Manager.Hits()

	if len(hits.Tabs) != 2 {
		t.Fatalf("expected 2 tab hits, got %d", len(hits.Tabs))
	}

	// 1. Click on tab-2 pill
	tab2 := hits.Tabs[1]
	clickTab2 := tea.MouseClickMsg{
		X:      (tab2.Rect.X1 + tab2.Rect.X2) / 2,
		Y:      0,
		Button: tea.MouseLeft,
	}
	m.Update(clickTab2)
	if m.Manager.ActiveIndex() != 1 {
		t.Fatalf("expected active tab index 1 after click, got %d", m.Manager.ActiveIndex())
	}

	// 2. Click on [+] New Tab button
	clickNewTab := tea.MouseClickMsg{
		X:      (hits.NewTab.X1 + hits.NewTab.X2) / 2,
		Y:      0,
		Button: tea.MouseLeft,
	}
	m.Update(clickNewTab)
	if m.Manager.Count() != 3 {
		t.Fatalf("expected 3 panes after [+] click, got %d", m.Manager.Count())
	}

	// 3. Click on tab-1 pill to switch back to tab 0
	tab1 := hits.Tabs[0]
	clickTab1 := tea.MouseClickMsg{
		X:      (tab1.Rect.X1 + tab1.Rect.X2) / 2,
		Y:      0,
		Button: tea.MouseLeft,
	}
	m.Update(clickTab1)
	if m.Manager.ActiveIndex() != 0 {
		t.Fatalf("expected active tab index 0 after clicking tab1, got %d", m.Manager.ActiveIndex())
	}

	// 4. Click workspace card in sidebar to switch project workspace
	m.Manager.AddWorkspace("herdr", "/Users/gear/GitHub/herdr")
	_, _ = m.SpawnMockPane("cargo build", "compiling...")
	_ = m.ViewString()
	hits = m.Manager.Hits()

	if len(hits.Workspaces) < 2 {
		t.Fatalf("expected at least 2 workspaces, got %d", len(hits.Workspaces))
	}

	// Click on workspace 0 (openagy)
	clickWs0 := tea.MouseClickMsg{
		X:      5,
		Y:      hits.Workspaces[0].Rect.Y,
		Button: tea.MouseLeft,
	}
	m.Update(clickWs0)
	if m.Manager.ActiveWorkspaceIndex() != 0 {
		t.Fatalf("expected active workspace index 0 after click, got %d", m.Manager.ActiveWorkspaceIndex())
	}

	// Click on workspace 1 (herdr)
	clickWs1 := tea.MouseClickMsg{
		X:      5,
		Y:      hits.Workspaces[1].Rect.Y,
		Button: tea.MouseLeft,
	}
	m.Update(clickWs1)
	if m.Manager.ActiveWorkspaceIndex() != 1 {
		t.Fatalf("expected active workspace index 1 after click, got %d", m.Manager.ActiveWorkspaceIndex())
	}
}

func TestHerdr_ModelExitBindings(t *testing.T) {
	// Helper to check if a Bubbletea command produces a tea.QuitMsg
	isQuitCmd := func(cmd tea.Cmd) bool {
		if cmd == nil {
			return false
		}
		msg := cmd()
		_, ok := msg.(tea.QuitMsg)
		return ok
	}

	// 1. Prefix + d (tmux detach / quit)
	t.Run("PrefixDetachD", func(t *testing.T) {
		m := NewModel(20, 80)
		_, _ = m.SpawnMockPane("sh", "sh$ ")
		ctrlB := tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl}
		m.Update(ctrlB)
		if !m.PrefixArmed {
			t.Fatalf("expected prefix armed")
		}
		_, cmd := m.Update(tea.KeyPressMsg{Code: 'd'})
		if !isQuitCmd(cmd) {
			t.Errorf("expected tea.Quit command for prefix+d")
		}
	})

	// 2. Prefix + q (herdr quit)
	t.Run("PrefixQuitQ", func(t *testing.T) {
		m := NewModel(20, 80)
		_, _ = m.SpawnMockPane("sh", "sh$ ")
		ctrlB := tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl}
		m.Update(ctrlB)
		_, cmd := m.Update(tea.KeyPressMsg{Code: 'q'})
		if !isQuitCmd(cmd) {
			t.Errorf("expected tea.Quit command for prefix+q")
		}
	})

	// 3. Prefix + x closes pane; exits if last pane
	t.Run("PrefixCloseLastPaneExits", func(t *testing.T) {
		m := NewModel(20, 80)
		_, _ = m.SpawnMockPane("sh", "sh$ ")
		ctrlB := tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl}
		m.Update(ctrlB)
		_, cmd := m.Update(tea.KeyPressMsg{Code: 'x'})
		if !isQuitCmd(cmd) {
			t.Errorf("expected tea.Quit command when closing last pane")
		}
		if m.Manager.Count() != 0 {
			t.Errorf("expected 0 panes, got %d", m.Manager.Count())
		}
	})

	// 4. Prefix + x closes only active pane if multiple remain
	t.Run("PrefixCloseOneOfMultiplePanes", func(t *testing.T) {
		m := NewModel(20, 80)
		_, _ = m.SpawnMockPane("sh1", "sh1$ ")
		_, _ = m.SpawnMockPane("sh2", "sh2$ ")
		ctrlB := tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl}
		m.Update(ctrlB)
		_, cmd := m.Update(tea.KeyPressMsg{Code: 'x'})
		if isQuitCmd(cmd) {
			t.Errorf("expected not to quit when 1 pane still remains")
		}
		if m.Manager.Count() != 1 {
			t.Errorf("expected 1 pane remaining, got %d", m.Manager.Count())
		}
	})

	// 5. PaneExitMsg terminates session when last shell process exits
	t.Run("PaneExitMsgLastPaneExits", func(t *testing.T) {
		m := NewModel(20, 80)
		p, _ := m.SpawnMockPane("sh", "sh$ ")
		_, cmd := m.Update(PaneExitMsg{PaneID: p.ID, Code: 0})
		if !isQuitCmd(cmd) {
			t.Errorf("expected tea.Quit when shell process exits on last pane")
		}
		if m.Manager.Count() != 0 {
			t.Errorf("expected 0 panes remaining, got %d", m.Manager.Count())
		}
	})

	// 6. Sidebar focus 'd' and 'q' detach/quit
	t.Run("SidebarFocusDetachD", func(t *testing.T) {
		m := NewModel(20, 80)
		_, _ = m.SpawnMockPane("sh", "sh$ ")
		m.Focus = FocusSidePanel
		_, cmd := m.Update(tea.KeyPressMsg{Code: 'd'})
		if !isQuitCmd(cmd) {
			t.Errorf("expected tea.Quit command for 'd' in side panel focus")
		}
		_, cmdQ := m.Update(tea.KeyPressMsg{Code: 'q'})
		if !isQuitCmd(cmdQ) {
			t.Errorf("expected tea.Quit command for 'q' in side panel focus")
		}
	})
}

func TestHerdr_AgentRecognitionAndDetection(t *testing.T) {
	// 1. Verify canonical agent lookup
	agents := []struct {
		input     string
		wantAgent string
	}{
		{"agy", AgentAgy},
		{"antigravity", AgentAgy},
		{"antigravity-cli", AgentAgy},
		{"openagy", AgentAgy},
		{"pigasus", AgentAgy},
		{"claude", AgentClaude},
		{"claude-code", AgentClaude},
		{"codex", AgentCodex},
		{"crush", AgentCrush},
		{"gemini", AgentGemini},
		{"cursor", AgentCursor},
		{"devin", AgentDevin},
		{"copilot", AgentCopilot},
		{"github-copilot", AgentCopilot},
		{"kimi", AgentKimi},
		{"qwen", AgentQwen},
		{"pi", AgentPi},
		{"collie", AgentCollie},
	}

	for _, tc := range agents {
		got, ok := LookupAgent(tc.input)
		if !ok || got != tc.wantAgent {
			t.Errorf("LookupAgent(%q) = (%q, %v), want (%q, true)", tc.input, got, ok, tc.wantAgent)
		}
	}

	// 2. Test auto-detection from pane terminal screen output
	m := NewModel(20, 80)

	// agy pane with blocked permission prompt
	pAgy, _ := m.SpawnMockPane("agy-term", "Analyzing task...\r\nnavigate · tab amend\r\n")
	agent, state, msg, detected := DetectAgentFromPane(pAgy)
	if !detected || agent != AgentAgy || state != "blocked" {
		t.Errorf("expected agy blocked detection, got: (%s, %s, %s, %v)", agent, state, msg, detected)
	}

	// claude pane with interrupt working prompt
	pClaude, _ := m.SpawnMockPane("claude-term", "Claude Code v2.1\r\nesc to interrupt · 1 MCP task\r\n")
	agent, state, _, detected = DetectAgentFromPane(pClaude)
	if !detected || agent != AgentClaude || state != "working" {
		t.Errorf("expected claude working detection, got: (%s, %s, %v)", agent, state, detected)
	}

	// codex pane with action required
	pCodex, _ := m.SpawnMockPane("codex-term", "Codex session\r\nAction Required\r\n")
	agent, state, _, detected = DetectAgentFromPane(pCodex)
	if !detected || agent != AgentCodex || state != "blocked" {
		t.Errorf("expected codex blocked detection, got: (%s, %s, %v)", agent, state, detected)
	}

	// crush pane with working progress
	pCrush, _ := m.SpawnMockPane("crush-term", "[crush] Reading codebase and analyzing dependencies...\r\n")
	agent, state, _, detected = DetectAgentFromPane(pCrush)
	if !detected || agent != AgentCrush || state != "working" {
		t.Errorf("expected crush working detection, got: (%s, %s, %v)", agent, state, detected)
	}

	// 3. CRITICAL: Plain shell running in openagy repository MUST NOT be falsely detected as an agent!
	pShell, _ := m.SpawnMockPane("zsh", "gear@MacBook openagy % ls\r\ncmd go.mod internal openagy pigasus\r\ngear@MacBook openagy % ")
	agent, state, _, detected = DetectAgentFromPane(pShell)
	if detected {
		t.Errorf("plain shell was falsely detected as an agent: agent=%s state=%s", agent, state)
	}
}

func TestHerdr_ThemesAndLightMode(t *testing.T) {
	m := NewModel(24, 80)

	// Default theme should be Mocha or detected
	th := m.Manager.Theme()
	if th.Name != "catppuccin" && th.Name != "catppuccin-latte" {
		t.Errorf("expected valid theme name, got %q", th.Name)
	}

	// 1. Force Latte (Light Theme)
	m.Manager.SetTheme(ThemeLatte)
	if m.Manager.Theme().IsDark {
		t.Errorf("expected ThemeLatte to be light mode (IsDark == false)")
	}
	if m.Manager.Theme().Text != "\x1b[38;2;76;79;105m" {
		t.Errorf("ThemeLatte text must be deep slate (#4c4f69) for light terminals, got %q", m.Manager.Theme().Text)
	}

	// Check Latte view rendering
	viewLatte := m.ViewString()
	if !strings.Contains(viewLatte, "\x1b[38;2;76;79;105m") {
		t.Errorf("rendered view in Latte theme must contain high-contrast dark text (#4c4f69)")
	}
	if !strings.Contains(viewLatte, "t:latte") {
		t.Errorf("footer should display active theme latte, got:\n%s", viewLatte)
	}

	// 2. Toggle Theme via Manager
	newTh := m.Manager.ToggleTheme()
	if !newTh.IsDark || newTh.Name != "catppuccin" {
		t.Errorf("expected toggle to switch to Catppuccin Mocha (dark), got %s", newTh.Name)
	}
	viewMocha := m.ViewString()
	if !strings.Contains(viewMocha, "t:mocha") {
		t.Errorf("footer should display active theme mocha, got:\n%s", viewMocha)
	}

	// 3. Toggle Theme via Keybinding: Prefix + t
	// Arm prefix (Ctrl+B)
	m.Update(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	if !m.PrefixArmed {
		t.Fatalf("expected prefix armed after ctrl+b")
	}
	// Press 't'
	m.Update(tea.KeyPressMsg{Code: 't', Text: "t"})
	if m.PrefixArmed {
		t.Errorf("expected prefix disarmed after 't'")
	}
	if m.Manager.Theme().Name != "catppuccin-latte" {
		t.Errorf("expected prefix+t to toggle back to catppuccin-latte, got %s", m.Manager.Theme().Name)
	}
}

func TestHerdr_WorkspaceGitAndMultiLineCards(t *testing.T) {
	m := NewModel(24, 80)

	// Setup Workspace 1: openagy (main)
	ws1 := m.Manager.ActiveWorkspace()
	ws1.Name = "openagy"
	ws1.Branch = "main"
	p1, _ := m.SpawnMockPane("term-1", "echo hello\r\n")
	p1.SetAgentState("agy", "working", "Refactoring cmd/herdr")

	// Setup Workspace 2: herdr (feat/sidebar)
	ws2 := m.Manager.AddWorkspace("herdr", "/Users/gear/GitHub/herdr")
	ws2.Branch = "feat/sidebar"
	ws2.Ahead = 1
	p2, _ := m.SpawnMockPane("cargo build", "compiling...\r\n")
	p2.SetAgentState("claude", "blocked", "Awaiting confirmation")

	// Setup Workspace 3: codex agent in separate pane
	p3, _ := m.SpawnMockPane("codex-worker", "thinking...\r\n")
	p3.SetAgentState("codex", "working", "Writing unit tests")

	m.Manager.SelectWorkspace(0)
	view := m.ViewString()

	// 1. Verify Spaces are categorized by project directory names, NOT individual terms
	if !strings.Contains(view, "openagy") || !strings.Contains(view, "herdr") {
		t.Errorf("view missing project workspace categories:\n%s", view)
	}

	// 2. Verify multi-line workspace cards show branch names
	if !strings.Contains(view, "main") || !strings.Contains(view, "feat/sidebar") {
		t.Errorf("view missing Git branch names in spaces section:\n%s", view)
	}

	// 3. Verify all agents appear in the agents panel with multi-line card details
	if !strings.Contains(view, "agy") || !strings.Contains(view, "Refactoring") {
		t.Errorf("view missing agy multi-line card in agents panel:\n%s", view)
	}
	if !strings.Contains(view, "claude") || !strings.Contains(view, "Awaiting") {
		t.Errorf("view missing claude multi-line card in agents panel:\n%s", view)
	}
	if !strings.Contains(view, "codex") || !strings.Contains(view, "Writing unit tests") {
		t.Errorf("view missing codex multi-line card in agents panel:\n%s", view)
	}
}
