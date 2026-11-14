package herdr

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	SidePanelWidth = 26
	cReset         = "\x1b[m"
	cBold          = "\x1b[1m"
	cDim           = "\x1b[2m"
)

// Legacy alias variables mapped to ThemeMocha for backwards compatibility
var (
	cAccent      = ThemeMocha.AccentBg
	cAccentBg    = ThemeMocha.AccentBg
	cAccentFg    = ThemeMocha.AccentFg
	cPanelBg     = ThemeMocha.PanelBg
	cActiveRowBg = ThemeMocha.ActiveRowBg
	cSelectionBg = ThemeMocha.SelectionBg
	cSurface0Bg  = ThemeMocha.Surface0Bg
	cSurface1Bg  = ThemeMocha.Surface1Bg
	cSurfaceDim  = ThemeMocha.SurfaceDim
	cOverlay0    = ThemeMocha.Overlay0
	cOverlay1    = ThemeMocha.Overlay1
	cText        = ThemeMocha.Text
	cSubtext0    = ThemeMocha.Subtext0
	cMauve       = ThemeMocha.Mauve
	cMauveBg     = ThemeMocha.MauveBg
	cGreen       = ThemeMocha.Green
	cYellow      = ThemeMocha.Yellow
	cRed         = ThemeMocha.Red
)

// HitRect defines a 1D or 2D region for mouse click hits.
type HitRect struct {
	X1, X2 int
	Y      int
	Y2     int
}

// Contains checks if point (x, y) falls within this rectangular hit zone.
func (r HitRect) Contains(x, y int) bool {
	if x < r.X1 || x >= r.X2 {
		return false
	}
	top := r.Y
	bottom := r.Y
	if r.Y2 > r.Y {
		bottom = r.Y2
	}
	return y >= top && y <= bottom
}

// TabHit records the position of a top tab pill.
type TabHit struct {
	Index int
	Rect  HitRect
}

// WorkspaceHit records the position of a workspace row in the sidebar.
type WorkspaceHit struct {
	Index int
	Rect  HitRect
}

// AgentHit records the position of an agent row in the sidebar.
type AgentHit struct {
	PaneID string
	Index  int
	Rect   HitRect
}

// HitMap stores the clickable UI elements for mouse interaction.
type HitMap struct {
	Tabs         []TabHit
	NewTab       HitRect
	Workspaces   []WorkspaceHit
	NewWorkspace HitRect
	Agents       []AgentHit
}

// RenderView composes the complete multiplexer layout:
// - Left Sidebar (0..25) running top to bottom
// - Top Tab Strip (row 0, col 26..width)
// - Terminal Canvas (row 1..height-2, col 26..width)
// - Mode Bar / Footer (row height-1)
func RenderView(m *Manager, focus FocusMode, prefixArmed bool, width, height int) string {
	if width < 40 {
		width = 80
	}
	if height < 6 {
		height = 24
	}

	hits := HitMap{}

	// Main terminal dimensions
	termWidth := width - SidePanelWidth
	if termWidth < 10 {
		termWidth = 10
	}
	canvasHeight := height - 2 // row 0 is top tab bar, row height-1 is mode bar
	if canvasHeight < 1 {
		canvasHeight = 1
	}

	th := m.Theme()

	// 1. Render Left Sidebar column (lines 0..height-1)
	sideLines := renderSidebarLines(m, th, focus, height, &hits)

	// 2. Render Top Tab Strip (Row 0, columns 26..width)
	tabBarLine := renderTopTabBar(m, th, termWidth, &hits)

	// 3. Render Terminal Canvas Lines (Rows 1..height-2, columns 26..width)
	activePane := m.Active()
	var termLines []string
	if activePane != nil {
		termLines = activePane.Lines()
	}

	// 4. Render Bottom Mode Bar (Row height-1, columns 26..width)
	bottomBarLine := renderBottomBar(m, th, prefixArmed, termWidth)

	// Save hits back to manager for mouse click dispatch
	m.SetHits(hits)

	// 5. Compose full grid line-by-line
	var b strings.Builder
	for y := 0; y < height; y++ {
		// Left cell (sidebar)
		sCell := ""
		if y < len(sideLines) {
			sCell = sideLines[y]
		}
		sPadded := padToVisible(sCell, SidePanelWidth)

		// Right cell
		var rCell string
		if y == 0 {
			rCell = padToVisible(tabBarLine, termWidth)
		} else if y == height-1 {
			rCell = padToVisible(bottomBarLine, termWidth)
		} else {
			tIdx := y - 1
			tLine := ""
			if tIdx < len(termLines) {
				tLine = termLines[tIdx]
			}
			rCell = padToVisible(tLine, termWidth)
		}

		b.WriteString(sPadded)
		b.WriteString(rCell)
		if y < height-1 {
			b.WriteByte('\n')
		}
	}

	return b.String()
}

// renderSidebarLines composes the full-height left sidebar:
// - Header: " spaces"
// - Workspaces/Panes list
// - " new" and "menu"
// - Horizontal divider "─"
// - Header: " agents"
// - Agent status rows
// - Sidebar toggle "◀" at bottom
// - Vertical divider "│" at column 25
func renderSidebarLines(m *Manager, th Palette, focus FocusMode, height int, hits *HitMap) []string {
	lines := make([]string, height)

	// Vertical separator glyph
	sep := th.SurfaceDim + "│" + cReset
	if focus == FocusSidePanel {
		sep = th.AccentBg + "┃" + cReset
	}

	// Section split: upper half spaces, lower half agents
	spacesSectionHeight := height / 2
	if spacesSectionHeight < 4 {
		spacesSectionHeight = 4
	}

	// --- 1. SPACES SECTION ---
	// Row 0: " spaces" header
	lines[0] = fmt.Sprintf(" %s%sspaces%s", cBold, th.Overlay0, cReset)

	// Workspace project cards (each workspace card occupies 2 rows: Name + Git Branch)
	workspaces := m.Workspaces()
	activeWS := m.ActiveWorkspaceIndex()
	maxWsRows := spacesSectionHeight - 2
	wsRowIdx := 1

	for i, ws := range workspaces {
		if wsRowIdx+1 > maxWsRows {
			break
		}
		isActive := i == activeWS

		// Determine workspace status glyph from its child panes and agents
		statusGlyph := th.Green + "●" + cReset
		for _, p := range ws.Panes {
			st, _ := p.Status()
			if st == StatusFailed {
				statusGlyph = th.Red + "✖" + cReset
				break
			}
			_, astate, _ := p.AgentState()
			if astate == "blocked" {
				statusGlyph = th.Red + "⚠" + cReset
				break
			} else if astate == "working" {
				statusGlyph = th.Yellow + "●" + cReset
			}
		}

		numStyle := th.Overlay0
		nameStyle := th.Text
		bgStyle := ""
		branchStyle := th.Overlay0
		if isActive {
			bgStyle = th.ActiveRowBg
			numStyle = th.Text
			nameStyle = cBold + th.Text
			branchStyle = th.Mauve
		}

		title := ws.Name
		if len(title) > 13 {
			title = title[:12] + "…"
		}

		// Card Row 0: Status glyph + 1-based index + Workspace project directory name
		line0 := fmt.Sprintf("  %s%d%s  %s%-12s%s %s", numStyle, i+1, cReset, nameStyle, title, cReset, statusGlyph)
		if bgStyle != "" {
			line0 = bgStyle + line0 + cReset
		}
		lines[wsRowIdx] = line0

		// Card Row 1: Indented branch and Git ahead/behind status
		gitStatus := ws.GitStatusString()
		if len(gitStatus) > 18 {
			gitStatus = gitStatus[:17] + "…"
		}
		line1 := fmt.Sprintf("     %s%s%s", branchStyle, gitStatus, cReset)
		if bgStyle != "" {
			line1 = bgStyle + line1 + cReset
		}
		lines[wsRowIdx+1] = line1

		cardTop := wsRowIdx
		hits.Workspaces = append(hits.Workspaces, WorkspaceHit{
			Index: i,
			Rect:  HitRect{X1: 0, X2: SidePanelWidth - 1, Y: cardTop, Y2: cardTop + 1},
		})
		wsRowIdx += 2
	}

	// Spaces footer row: " new" on left, "menu" on right
	footerRow := spacesSectionHeight - 1
	if footerRow < height {
		newBtn := fmt.Sprintf(" %snew%s", th.Overlay0, cReset)
		menuBtn := fmt.Sprintf("%smenu%s ", th.Overlay0, cReset)
		spacePad := (SidePanelWidth - 1) - visibleLen(newBtn) - visibleLen(menuBtn)
		if spacePad < 1 {
			spacePad = 1
		}
		lines[footerRow] = newBtn + strings.Repeat(" ", spacePad) + menuBtn

		hits.NewWorkspace = HitRect{X1: 1, X2: 6, Y: footerRow, Y2: footerRow}
	}

	// --- 2. DIVIDER LINE ---
	dividerRow := spacesSectionHeight
	if dividerRow < height {
		lines[dividerRow] = th.SurfaceDim + strings.Repeat("─", SidePanelWidth-1) + cReset
	}

	// --- 3. AGENTS SECTION ---
	agentHeaderRow := spacesSectionHeight + 1
	if agentHeaderRow < height {
		lines[agentHeaderRow] = fmt.Sprintf(" %s%sagents%s", cBold, th.Overlay0, cReset)
	}

	agentRowIdx := agentHeaderRow + 1
	allAgents := m.AllAgents()
	activePane := m.Active()

	if len(allAgents) == 0 && agentRowIdx < height-1 {
		lines[agentRowIdx] = fmt.Sprintf("  %sno active agents%s", th.Overlay0, cReset)
	}

	for i, entry := range allAgents {
		if agentRowIdx+1 >= height-1 {
			break
		}

		isAgentActive := activePane != nil && entry.Pane.ID == activePane.ID
		bgStyle := ""
		if isAgentActive {
			bgStyle = th.ActiveRowBg
		}

		statusDot := th.Yellow + "●" + cReset
		switch entry.State {
		case "blocked":
			statusDot = th.Red + cBold + "⚠" + cReset
		case "idle":
			statusDot = th.Green + "✓" + cReset
		}

		// Card Row 0: Status glyph + Agent name + Workspace name
		agentLabel := entry.AgentName
		wsLabel := entry.Workspace.Name
		if len(wsLabel) > 8 {
			wsLabel = wsLabel[:7] + "…"
		}
		line0 := fmt.Sprintf("  %s %s%s%s %s· %s%s", statusDot, cBold+th.Text, agentLabel, cReset, th.Overlay0, wsLabel, cReset)
		if bgStyle != "" {
			line0 = bgStyle + line0 + cReset
		}
		lines[agentRowIdx] = line0

		// Card Row 1: Indented detail progress message / state subtitle
		msg := entry.Message
		if msg == "" {
			msg = entry.State
		}
		sub := truncateVisible(msg, SidePanelWidth-6)
		line1 := fmt.Sprintf("     %s%s%s", th.Subtext0, sub, cReset)
		if bgStyle != "" {
			line1 = bgStyle + line1 + cReset
		}
		lines[agentRowIdx+1] = line1

		cardTop := agentRowIdx
		hits.Agents = append(hits.Agents, AgentHit{
			PaneID: entry.Pane.ID,
			Index:  i,
			Rect:   HitRect{X1: 0, X2: SidePanelWidth - 1, Y: cardTop, Y2: cardTop + 1},
		})
		agentRowIdx += 2
	}

	// Sidebar toggle indicator at bottom right
	toggleRow := height - 1
	toggleGlyph := fmt.Sprintf("%s◀%s", th.Overlay0, cReset)
	lines[toggleRow] = padToVisible("", SidePanelWidth-2) + toggleGlyph

	// Append vertical separator "│" to every line in the sidebar, wrapping with panel background
	for y := 0; y < height; y++ {
		paddedContent := padToVisible(lines[y], SidePanelWidth-1)
		lines[y] = th.PanelBg + paddedContent + sep + cReset
	}

	return lines
}

// renderTopTabBar composes the horizontal tab strip at row 0 (columns 26..width)
func renderTopTabBar(m *Manager, th Palette, width int, hits *HitMap) string {
	var b strings.Builder
	panes := m.Panes()
	activeIdx := m.ActiveIndex()

	currX := SidePanelWidth

	for i, p := range panes {
		isActive := i == activeIdx
		title := p.Title
		if len(title) > 16 {
			title = title[:15] + "…"
		}

		tabLabel := fmt.Sprintf("  %s  ", title)
		tabLen := visibleLen(tabLabel)

		var pill string
		if isActive {
			pill = fmt.Sprintf("%s%s%s%s%s", th.AccentBg, th.AccentFg, cBold, tabLabel, cReset)
		} else {
			pill = fmt.Sprintf("%s%s%s%s", th.Surface0Bg, th.Text, tabLabel, cReset)
		}

		b.WriteString(pill)
		b.WriteString(" ") // 1 space between pills

		hits.Tabs = append(hits.Tabs, TabHit{
			Index: i,
			Rect:  HitRect{X1: currX, X2: currX + tabLen, Y: 0},
		})
		currX += tabLen + 1
	}

	// New Tab [+] button
	newTabBtn := fmt.Sprintf("%s%s [+] %s", th.Surface0Bg, th.Overlay0, cReset)
	btnLen := visibleLen(newTabBtn)
	b.WriteString(newTabBtn)

	hits.NewTab = HitRect{X1: currX, X2: currX + btnLen, Y: 0}
	currX += btnLen

	// Right-aligned status (e.g. socket or version)
	sockPath := m.SocketPath()
	statusText := ""
	if sockPath != "" {
		statusText = fmt.Sprintf("%sherdr 0.9.5 | %s%s", th.Overlay0, filepath.Base(sockPath), cReset)
	} else {
		statusText = fmt.Sprintf("%sherdr 0.9.5%s", th.Overlay0, cReset)
	}

	statusLen := visibleLen(statusText)
	remaining := width - (currX - SidePanelWidth) - statusLen
	if remaining > 0 {
		b.WriteString(strings.Repeat(" ", remaining))
		b.WriteString(statusText)
	}

	return th.PanelBg + b.String() + cReset
}

// renderBottomBar composes the bottom mode bar (Row height-1)
func renderBottomBar(m *Manager, th Palette, prefixArmed bool, width int) string {
	if prefixArmed {
		// Catppuccin Mauve Mode Bar for Prefix
		prefixTag := fmt.Sprintf("%s%s%s PREFIX %s ", th.MauveBg, th.AccentFg, cBold, cReset)
		actions := fmt.Sprintf("%sesc%s%s cancel  %s^B%s%s send prefix  %sc%s%s new tab  %st%s%s theme  %sn/p%s%s cycle  %sx%s%s close  %sd/q%s%s detach/quit  %sTab%s%s focus%s",
			th.AccentBg, cReset, th.Overlay0,
			th.AccentBg, cReset, th.Overlay0,
			th.AccentBg, cReset, th.Overlay0,
			th.AccentBg, cReset, th.Overlay0,
			th.AccentBg, cReset, th.Overlay0,
			th.AccentBg, cReset, th.Overlay0,
			th.AccentBg, cReset, th.Overlay0,
			th.AccentBg, cReset, th.Overlay0,
			cReset,
		)
		full := prefixTag + actions
		return th.PanelBg + full + cReset
	}

	// Normal Mode: Clean subtle footer matching Herdr
	activePane := m.Active()
	paneTitle := "none"
	dir := ""
	if activePane != nil {
		paneTitle = activePane.Title
		dir = activePane.Dir
		if len(dir) > 28 {
			dir = "…" + dir[len(dir)-27:]
		}
	}

	themeLabel := "latte"
	if th.IsDark {
		themeLabel = "mocha"
	}

	left := fmt.Sprintf(" Active: %s%s%s (%s%s%s)", cBold+th.Text, paneTitle, cReset, th.Overlay0, dir, cReset)
	right := fmt.Sprintf("%s^B prefix | d quit | t:%s%s ", th.Overlay0, themeLabel, cReset)

	leftVis := visibleLen(left)
	rightVis := visibleLen(right)
	pad := width - leftVis - rightVis
	if pad < 1 {
		pad = 1
	}

	return th.PanelBg + left + strings.Repeat(" ", pad) + right + cReset
}

func padToVisible(s string, target int) string {
	vis := visibleLen(s)
	if vis >= target {
		return truncateVisible(s, target)
	}
	return s + strings.Repeat(" ", target-vis)
}

func visibleLen(s string) int {
	clean := stripANSI(s)
	count := 0
	for _, r := range clean {
		if r < 32 {
			continue
		}
		count++
	}
	return count
}

func truncateVisible(s string, maxVis int) string {
	if maxVis <= 0 {
		return ""
	}
	var b strings.Builder
	vis := 0
	inEsc := false

	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size

		if r == 0x1b {
			inEsc = true
			b.WriteRune(r)
			continue
		}
		if inEsc {
			b.WriteRune(r)
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '~' {
				inEsc = false
			}
			continue
		}

		if vis >= maxVis {
			break
		}
		b.WriteRune(r)
		if r >= 32 {
			vis++
		}
	}
	b.WriteString(cReset)
	return b.String()
}

func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		if r == 0x1b {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '~' {
				inEsc = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
