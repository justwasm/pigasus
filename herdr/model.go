package herdr

import (
	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"
)

// TerminalOutputMsg signals that a pane has received new output from its PTY.
type TerminalOutputMsg struct {
	PaneID string
}

// Model is the Bubbletea application model for the Herdr terminal multiplexer.
type Model struct {
	Manager       *Manager
	Focus         FocusMode
	PrefixArmed   bool
	Width         int
	Height        int
	sidebarCursor int

	outputChan chan string
	exitChan   chan PaneExitMsg
}

// PaneExitMsg is emitted when a managed pane's child process terminates.
type PaneExitMsg struct {
	PaneID string
	Code   int
}

// NewModel creates an initialized Herdr multiplexer model.
func NewModel(rows, cols int) *Model {
	if rows < 10 {
		rows = 24
	}
	if cols < 40 {
		cols = 80
	}

	termCols := cols - SidePanelWidth - 1
	if termCols < 10 {
		termCols = 10
	}
	termRows := rows - 2
	if termRows < 1 {
		termRows = 1
	}

	mgr := NewManager(termRows, termCols)
	outCh := make(chan string, 128)
	exitCh := make(chan PaneExitMsg, 64)

	mgr.SetOnUpdate(func() {
		select {
		case outCh <- "update":
		default:
		}
	})

	mgr.SetOnPaneExit(func(paneID string, code int) {
		select {
		case exitCh <- PaneExitMsg{PaneID: paneID, Code: code}:
		default:
		}
	})

	m := &Model{
		Manager:    mgr,
		Focus:      FocusTerminal,
		Width:      cols,
		Height:     rows,
		outputChan: outCh,
		exitChan:   exitCh,
	}

	return m
}

// StartSocketServer launches the Herdr Unix domain socket server attached to this model.
func (m *Model) StartSocketServer(socketPath string) (*Server, error) {
	return NewServer(socketPath, m.Manager)
}

// Close terminates all running panes and closes the Herdr socket server.
func (m *Model) Close() {
	m.Manager.CloseAll()
}

// Init starts initial listeners for the multiplexer.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.waitForOutput(), m.waitForExit())
}

func (m *Model) waitForOutput() tea.Cmd {
	return func() tea.Msg {
		if m.outputChan == nil {
			return nil
		}
		id, ok := <-m.outputChan
		if !ok {
			return nil
		}
		return TerminalOutputMsg{PaneID: id}
	}
}

func (m *Model) waitForExit() tea.Cmd {
	return func() tea.Msg {
		if m.exitChan == nil {
			return nil
		}
		msg, ok := <-m.exitChan
		if !ok {
			return nil
		}
		return msg
	}
}

// SpawnDefaultPane initializes the first terminal pane if empty.
func (m *Model) SpawnDefaultPane(title, dir string) (*Pane, error) {
	termCols := m.Width - SidePanelWidth - 1
	if termCols < 10 {
		termCols = 10
	}
	termRows := m.Height - 2
	if termRows < 1 {
		termRows = 1
	}

	ch := m.outputChan
	p, err := m.Manager.AddPane(title, dir, WithOnOutput(func() {
		select {
		case ch <- title:
		default:
		}
	}))
	return p, err
}

// SpawnMockPane creates a mock terminal pane for offline/demo/storybook usage.
func (m *Model) SpawnMockPane(title, welcomeMsg string) (*Pane, error) {
	ch := m.outputChan
	p, err := m.Manager.AddPane(title, "", WithMockSession(welcomeMsg), WithOnOutput(func() {
		select {
		case ch <- title:
		default:
		}
	}))
	return p, err
}

// Update processes Bubbletea events.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height

		termCols := m.Width - SidePanelWidth
		if termCols < 10 {
			termCols = 10
		}
		termRows := m.Height - 2
		if termRows < 1 {
			termRows = 1
		}
		m.Manager.Resize(termRows, termCols)
		return m, nil

	case TerminalOutputMsg:
		return m, m.waitForOutput()

	case PaneExitMsg:
		m.Manager.ClosePaneByID(msg.PaneID)
		if m.Manager.Count() == 0 {
			return m, tea.Quit
		}
		return m, m.waitForExit()

	case tea.PasteMsg:
		// Native Cmd+V bracketed paste
		active := m.Manager.Active()
		if active != nil {
			_, _ = active.Write([]byte(msg.String()))
		}
		return m, nil

	case tea.MouseMsg:
		mouse := msg.Mouse()
		if mouse.Button == tea.MouseLeft {
			m.handleMouseClick(mouse.X, mouse.Y)
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m *Model) handleMouseClick(x, y int) {
	hits := m.Manager.Hits()

	// 1. Top Tab Strip Click (Row 0, columns 26..width)
	if y == 0 && x >= SidePanelWidth {
		// Check Tab Pills
		for _, tabHit := range hits.Tabs {
			if x >= tabHit.Rect.X1 && x < tabHit.Rect.X2 {
				m.Manager.SelectPane(tabHit.Index)
				m.Focus = FocusTerminal
				return
			}
		}
		// Check [+] New Tab button
		if x >= hits.NewTab.X1 && x < hits.NewTab.X2 {
			_, _ = m.SpawnDefaultPane("", "")
			m.Focus = FocusTerminal
			return
		}
		return
	}

	// 2. Sidebar Click (columns 0..25)
	if x < SidePanelWidth {
		// Check Workspaces list
		for _, wsHit := range hits.Workspaces {
			if wsHit.Rect.Contains(x, y) {
				m.Manager.SelectWorkspace(wsHit.Index)
				m.Focus = FocusTerminal
				return
			}
		}
		// Check " new" button
		if hits.NewWorkspace.Contains(x, y) {
			m.Manager.AddWorkspace("", "")
			_, _ = m.SpawnDefaultPane("", "")
			m.Focus = FocusTerminal
			return
		}
		// Check Agents list
		for _, agentHit := range hits.Agents {
			if agentHit.Rect.Contains(x, y) {
				m.Manager.SelectAgentPane(agentHit.PaneID)
				m.Focus = FocusTerminal
				return
			}
		}
		return
	}

	// 3. Terminal Canvas Click (columns 26..width, rows 1..height-2)
	if x >= SidePanelWidth && y >= 1 {
		m.Focus = FocusTerminal
	}
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// 0. Check for Clipboard Paste (Cmd+V fallback / Ctrl+V)
	if msg.String() == "ctrl+v" || (msg.Key().Code == 'v' && (msg.Key().Mod.Contains(tea.ModCtrl) || msg.Key().Mod&tea.ModCtrl != 0)) {
		if text, err := clipboard.ReadAll(); err == nil && len(text) > 0 {
			active := m.Manager.Active()
			if active != nil {
				_, _ = active.Write([]byte(text))
			}
			return m, nil
		}
	}

	// 1. Check for Ctrl+B prefix toggle
	if IsCtrlB(msg) {
		if m.PrefixArmed {
			// Double Ctrl+B sends literal Ctrl+B to the active terminal
			m.PrefixArmed = false
			if active := m.Manager.Active(); active != nil && m.Focus == FocusTerminal {
				_, _ = active.Write([]byte{0x02})
			}
			return m, nil
		}
		m.PrefixArmed = true
		return m, nil
	}

	// 2. Handle Prefix Commands
	if m.PrefixArmed {
		m.PrefixArmed = false
		switch msg.String() {
		case "c":
			// Create new pane
			_, _ = m.SpawnDefaultPane("", "")
			return m, nil
		case "n":
			// Next pane
			m.Manager.NextPane()
			return m, nil
		case "p":
			// Previous pane
			m.Manager.PrevPane()
			return m, nil
		case "x":
			// Close current pane
			m.Manager.CloseCurrent()
			if m.Manager.Count() == 0 {
				return m, tea.Quit
			}
			return m, nil
		case "d", "D", "q", "Q", "ctrl+d", "ctrl+q", "ctrl+c":
			// Detach / Quit multiplexer session
			return m, tea.Quit
		case "t", "T":
			// Toggle color theme between Catppuccin Mocha (dark) and Latte (light)
			m.Manager.ToggleTheme()
			return m, nil
		case "tab":
			// Toggle focus between terminal and side panel
			if m.Focus == FocusTerminal {
				m.Focus = FocusSidePanel
				m.sidebarCursor = m.Manager.ActiveIndex()
			} else {
				m.Focus = FocusTerminal
			}
			return m, nil
		case "1", "2", "3", "4", "5", "6", "7", "8", "9":
			idx := int(msg.String()[0] - '1')
			m.Manager.SelectPane(idx)
			return m, nil
		}
		// Any unrecognized key disarms prefix and is dropped
		return m, nil
	}

	// 3. Handle Side Panel Navigation Focus
	if m.Focus == FocusSidePanel {
		switch msg.String() {
		case "j", "down":
			if m.sidebarCursor < len(m.Manager.Workspaces())-1 {
				m.sidebarCursor++
			}
			return m, nil
		case "k", "up":
			if m.sidebarCursor > 0 {
				m.sidebarCursor--
			}
			return m, nil
		case "enter":
			m.Manager.SelectWorkspace(m.sidebarCursor)
			m.Focus = FocusTerminal
			return m, nil
		case "c", "n":
			_, _ = m.SpawnDefaultPane("", "")
			m.sidebarCursor = m.Manager.ActiveIndex()
			return m, nil
		case "x":
			m.Manager.CloseCurrent()
			if m.Manager.Count() == 0 {
				return m, tea.Quit
			}
			m.sidebarCursor = m.Manager.ActiveIndex()
			return m, nil
		case "d", "D", "q", "Q":
			// Detach / Quit
			return m, tea.Quit
		case "tab", "esc":
			m.Focus = FocusTerminal
			return m, nil
		}
		return m, nil
	}

	// 4. Handle Terminal Focus: Forward raw keystrokes directly to the PTY
	active := m.Manager.Active()
	if active != nil {
		rawBytes := EncodeKeyToPTY(msg)
		if len(rawBytes) > 0 {
			_, _ = active.Write(rawBytes)
		}
	}

	return m, nil
}

// View draws the complete terminal multiplexer layout as a Bubbletea v2 View.
func (m *Model) View() tea.View {
	v := tea.NewView(m.ViewString())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.DisableBracketedPasteMode = false
	return v
}

// ViewString renders the complete multiplexer layout as a string.
func (m *Model) ViewString() string {
	return RenderView(m.Manager, m.Focus, m.PrefixArmed, m.Width, m.Height)
}
