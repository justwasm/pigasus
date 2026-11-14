package storybook

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/palette"
)

// SupportedThemes lists the previewable color schemes.
var SupportedThemes = []string{
	"dark",
	"solarized dark",
	"tokyo night",
	"terminal",
	"light",
	"solarized light",
}

// Model is the top-level Bubbletea model for the Antigravity TUI Storybook.
type Model struct {
	catalog         *Catalog
	selectedNavIdx  int
	sidebarScroll   int
	activeStateIdx  int
	currentInstance StoryInstance
	activeStoryID   string
	focus           FocusArea
	filtering       bool
	filterText      string
	width           int
	height          int
	themeIdx        int
	quitting        bool
}

// NewModel constructs a new Storybook model seeded with the story catalog.
func NewModel(c *Catalog) Model {
	m := Model{
		catalog:  c,
		themeIdx: 0,
		width:    100,
		height:   30,
		focus:    FocusSidebar,
	}

	// Select first visible story if present
	items := c.VisibleNavItems("")
	for i, it := range items {
		if !it.IsCategory {
			m.selectedNavIdx = i
			m.selectStory(it.Story)
			break
		}
	}
	return m
}

func (m *Model) selectStory(s *Story) {
	if s == nil {
		m.currentInstance = nil
		m.activeStoryID = ""
		m.activeStateIdx = 0
		return
	}
	if m.activeStoryID == s.ID && m.currentInstance != nil {
		return
	}
	m.activeStoryID = s.ID
	m.activeStateIdx = 0
	if s.Factory != nil {
		m.currentInstance = s.Factory()
		m.currentInstance.SetState(0)
		layout := ComputeLayout(m.width, m.height)
		m.currentInstance.SetSize(layout.CanvasWidth, layout.BodyHeight)
	} else {
		m.currentInstance = nil
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		layout := ComputeLayout(m.width, m.height)
		if m.currentInstance != nil {
			m.currentInstance.SetSize(layout.CanvasWidth, layout.BodyHeight)
		}
		return m, nil

	case tea.PasteMsg:
		if m.focus == FocusPlayground && m.currentInstance != nil {
			if h, ok := m.currentInstance.(interface {
				HandlePaste(tea.PasteMsg) (tea.Cmd, bool)
			}); ok {
				cmd, _ := h.HandlePaste(msg)
				return m, cmd
			}
		}
		return m, nil

	case tea.MouseMsg:
		ev := msg.Mouse()
		layout := ComputeLayout(m.width, m.height)
		items := m.catalog.VisibleNavItems(m.filterText)
		isOverSidebar := ev.X <= layout.SidebarWidth

		// 1. Mouse event strictly over sidebar:
		if isOverSidebar {
			switch ev.Button {
			case tea.MouseWheelDown:
				if len(items) > 0 && m.selectedNavIdx < len(items)-1 {
					m.selectedNavIdx++
					m.syncSelection(items)
				}
				return m, nil

			case tea.MouseWheelUp:
				if len(items) > 0 && m.selectedNavIdx > 0 {
					m.selectedNavIdx--
					m.syncSelection(items)
				}
				return m, nil

			case tea.MouseLeft:
				m.focus = FocusSidebar
				// Row 0 is top title bar, row 1 is header (" STORIES"), row 2 is divider line ("───").
				// Therefore item 0 is rendered at screen row 3.
				rowOffset := ev.Y - 3
				if rowOffset >= 0 {
					availRows := layout.BodyHeight - 2
					if availRows < 1 {
						availRows = 1
					}
					scrollOffset := m.sidebarScroll
					if m.selectedNavIdx < scrollOffset {
						scrollOffset = m.selectedNavIdx
					}
					if m.selectedNavIdx >= scrollOffset+availRows {
						scrollOffset = m.selectedNavIdx - availRows + 1
					}
					if scrollOffset < 0 {
						scrollOffset = 0
					}

					clickedIdx := scrollOffset + rowOffset
					if clickedIdx >= 0 && clickedIdx < len(items) {
						m.selectedNavIdx = clickedIdx
						it := items[clickedIdx]
						if it.IsCategory {
							m.catalog.ToggleCategory(it.Category)
							m.clampSelection()
						} else {
							m.syncSelection(items)
						}
					}
				}
				return m, nil
			}
			return m, nil
		}

		// 2. Mouse event over canvas / playground:
		if ev.Button == tea.MouseLeft {
			m.focus = FocusPlayground
		}

		// 3. Playground mouse delegation
		if m.currentInstance != nil {
			if h, ok := m.currentInstance.(interface {
				HandleMouse(tea.MouseMsg) (tea.Cmd, bool)
			}); ok {
				cmd, handled := h.HandleMouse(msg)
				if handled {
					return m, cmd
				}
			}
		}

		// 4. Fallback for wheel scroll over playground when component doesn't consume it:
		switch ev.Button {
		case tea.MouseWheelDown:
			if len(items) > 0 && m.selectedNavIdx < len(items)-1 {
				m.selectedNavIdx++
				m.syncSelection(items)
			}
			return m, nil
		case tea.MouseWheelUp:
			if len(items) > 0 && m.selectedNavIdx > 0 {
				m.selectedNavIdx--
				m.syncSelection(items)
			}
			return m, nil
		}
		return m, nil

	case tea.KeyMsg:
		key := msg.Key()

		// 1. Interactive playground mode routes keys to the component directly
		if m.focus == FocusPlayground {
			if key.Code == tea.KeyEsc {
				m.focus = FocusSidebar
				return m, nil
			}
			if m.currentInstance != nil {
				cmd, _ := m.currentInstance.HandleKey(msg)
				return m, cmd
			}
			m.focus = FocusSidebar
			return m, nil
		}

		// 2. Search / Filter input mode
		if m.filtering {
			switch key.Code {
			case tea.KeyEsc:
				m.filtering = false
				m.filterText = ""
				m.clampSelection()
				return m, nil
			case tea.KeyEnter:
				m.filtering = false
				m.clampSelection()
				return m, nil
			case tea.KeyBackspace:
				if len(m.filterText) > 0 {
					m.filterText = m.filterText[:len(m.filterText)-1]
					m.clampSelection()
				}
				return m, nil
			default:
				if key.Text != "" {
					m.filterText += key.Text
					m.clampSelection()
					return m, nil
				}
			}
		}

		// 3. Sidebar navigation & controls mode
		items := m.catalog.VisibleNavItems(m.filterText)

		switch key.Code {
		case 'q', 'Q':
			m.quitting = true
			return m, tea.Quit

		case tea.KeyEsc:
			if m.filterText != "" {
				m.filterText = ""
				m.clampSelection()
				return m, nil
			}
			m.quitting = true
			return m, tea.Quit

		case '/':
			m.filtering = true
			return m, nil

		case 't', 'T':
			// Switch Theme
			m.themeIdx = (m.themeIdx + 1) % len(SupportedThemes)
			palette.SetScheme(SupportedThemes[m.themeIdx])
			return m, nil

		case tea.KeyUp, 'k', 'K':
			if len(items) > 0 {
				m.selectedNavIdx = (m.selectedNavIdx - 1 + len(items)) % len(items)
				m.syncSelection(items)
			}
			return m, nil

		case tea.KeyDown, 'j', 'J':
			if len(items) > 0 {
				m.selectedNavIdx = (m.selectedNavIdx + 1) % len(items)
				m.syncSelection(items)
			}
			return m, nil

		case tea.KeyEnter:
			if m.selectedNavIdx >= 0 && m.selectedNavIdx < len(items) {
				it := items[m.selectedNavIdx]
				if it.IsCategory {
					m.catalog.ToggleCategory(it.Category)
					m.clampSelection()
				} else {
					// Enter interactive playground
					m.focus = FocusPlayground
				}
			}
			return m, nil

		case tea.KeyTab:
			// Cycle through states of the current story
			if curStory, ok := m.catalog.Find(m.activeStoryID); ok && len(curStory.States) > 0 {
				m.activeStateIdx = (m.activeStateIdx + 1) % len(curStory.States)
				if m.currentInstance != nil {
					m.currentInstance.SetState(m.activeStateIdx)
				}
			}
			return m, nil

		case '1', '2', '3', '4', '5', '6', '7', '8', '9':
			idx := int(key.Code - '1')
			if curStory, ok := m.catalog.Find(m.activeStoryID); ok && idx >= 0 && idx < len(curStory.States) {
				m.activeStateIdx = idx
				if m.currentInstance != nil {
					m.currentInstance.SetState(m.activeStateIdx)
				}
			}
			return m, nil
		}
	}

	return m, nil
}

func (m *Model) clampSelection() {
	items := m.catalog.VisibleNavItems(m.filterText)
	if len(items) == 0 {
		m.selectedNavIdx = 0
		m.selectStory(nil)
		return
	}
	if m.selectedNavIdx >= len(items) {
		m.selectedNavIdx = len(items) - 1
	}
	if m.selectedNavIdx < 0 {
		m.selectedNavIdx = 0
	}
	m.syncSelection(items)
}

func (m *Model) syncSelection(items []NavItem) {
	layout := ComputeLayout(m.width, m.height)
	availRows := layout.BodyHeight - 2
	if availRows < 1 {
		availRows = 1
	}
	if m.selectedNavIdx < m.sidebarScroll {
		m.sidebarScroll = m.selectedNavIdx
	}
	if m.selectedNavIdx >= m.sidebarScroll+availRows {
		m.sidebarScroll = m.selectedNavIdx - availRows + 1
	}
	if m.sidebarScroll < 0 {
		m.sidebarScroll = 0
	}

	if m.selectedNavIdx >= 0 && m.selectedNavIdx < len(items) {
		it := items[m.selectedNavIdx]
		if !it.IsCategory {
			m.selectStory(it.Story)
		}
	}
}

// SelectedNavIdx returns the current navigation index in the sidebar.
func (m Model) SelectedNavIdx() int {
	return m.selectedNavIdx
}

// ActiveStoryID returns the identifier of the active story.
func (m Model) ActiveStoryID() string {
	return m.activeStoryID
}

// Focus returns the current focus area (FocusSidebar or FocusPlayground).
func (m Model) Focus() FocusArea {
	return m.focus
}

// ActiveStateIdx returns the active state index of the current story.
func (m Model) ActiveStateIdx() int {
	return m.activeStateIdx
}

// CurrentInstance returns the active StoryInstance.
func (m Model) CurrentInstance() StoryInstance {
	return m.currentInstance
}

// SetFocus sets the focus area directly.
func (m *Model) SetFocus(f FocusArea) {
	m.focus = f
}

// Quitting reports whether the storybook was terminated by the user.
func (m Model) Quitting() bool {
	return m.quitting
}

func (m Model) View() tea.View {
	if m.quitting {
		v := tea.NewView("")
		v.AltScreen = false
		return v
	}

	layout := ComputeLayout(m.width, m.height)
	items := m.catalog.VisibleNavItems(m.filterText)

	// Top Title Bar with hollow (○) / solid (●) circle mode indicator
	var modeIndicator string
	if m.focus == FocusPlayground {
		modeIndicator = green(bold("● INTERACTIVE "))
	} else {
		modeIndicator = dim("○ NAV ")
	}
	titleText := " ❖ ANTIGRAVITY TUI STORYBOOK " + modeIndicator + dim("— Component Catalog & Playground")
	themeText := blue("Theme: " + SupportedThemes[m.themeIdx] + " [T] ")
	topBar := padOrTruncate(titleText, layout.TotalWidth-len(SupportedThemes[m.themeIdx])-10) + themeText

	// Left: Sidebar
	sidebarContent := RenderSidebar(items, m.selectedNavIdx, m.sidebarScroll, layout.SidebarWidth, layout.BodyHeight, m.filtering, m.filterText, m.focus)

	// Right: Canvas
	var canvasContent string
	var currentStory *Story
	if s, ok := m.catalog.Find(m.activeStoryID); ok {
		currentStory = &s
	}
	var viewBody string
	if m.currentInstance != nil {
		viewBody = m.currentInstance.View()
	}
	canvasContent = RenderCanvas(currentStory, m.activeStateIdx, SupportedThemes[m.themeIdx], m.focus, viewBody, layout.CanvasWidth, layout.BodyHeight)

	// Combine Columns
	body := JoinColumns(sidebarContent, layout.SidebarWidth, canvasContent, layout.CanvasWidth, layout.BodyHeight, dim("│"))

	fullView := topBar + "\n" + body
	lines := strings.Split(fullView, "\n")
	if len(lines) > m.height {
		lines = lines[:m.height]
		fullView = strings.Join(lines, "\n")
	}

	v := tea.NewView(fullView)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.DisableBracketedPasteMode = false
	return v
}
