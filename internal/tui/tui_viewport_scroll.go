package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/core/settings"
	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/internal/ui/tuitools"
	"github.com/justwasm/pigasus/palette"
)

// ---- tui_viewport_scroll.go ----

const selHighlightOpen = tuiviewport.SelHighlightOpen

func (m *RootModel) selectionDocRange() (int, int, int, int, bool) {
	startRow := m.viewport.selStartDocRow
	endRow := m.viewport.selEndDocRow
	if startRow == 0 && endRow == 0 && (m.viewport.SelStartRow != 0 || m.viewport.SelEndRow != 0) {
		startRow = m.viewport.scroll + m.viewport.SelStartRow
		endRow = m.viewport.scroll + m.viewport.SelEndRow
	}
	return tuiviewport.SelectionRange(
		startRow, m.viewport.SelStartCol,
		endRow, m.viewport.SelEndCol,
		m.viewport.SelDragging,
	)
}

func (m *RootModel) selectionText() string {
	if !m.viewport.altScreen {
		return ""
	}
	rows, _, _ := m.syncRowsView()
	if len(rows) == 0 {
		return ""
	}
	sr, sc, er, ec, ok := m.selectionDocRange()
	if !ok {
		return ""
	}
	sr = tuilib.ClampInt(sr, 0, len(rows)-1)
	er = tuilib.ClampInt(er, 0, len(rows)-1)
	return tuiviewport.SelectionText(rows, sr, sc, er, ec)
}

func (m *RootModel) copySelectionCmd(text string) tea.Cmd {
	if text == "" {
		return nil
	}
	return tea.SetClipboard(text)
}

func (m *RootModel) selectionPass(rows []string) []string {
	if !m.hasSelection() || len(rows) == 0 {
		return rows
	}
	sr, sc, er, ec, ok := m.selectionDocRange()
	if !ok {
		return rows
	}
	stickyOffset := 0
	if m.viewport.pinnedPromptText != "" || m.viewport.stickyBarRow != "" {
		stickyOffset = 2
	}
	visSr := sr - m.viewport.scroll + stickyOffset
	visEr := er - m.viewport.scroll + stickyOffset
	if visEr < 0 || visSr >= len(rows) {
		return rows
	}
	if visSr < 0 {
		visSr = 0
		sc = 0
	}
	if visEr >= len(rows) {
		visEr = len(rows) - 1
		ec = m.viewport.width
	}
	return tuiviewport.SelectionPass(rows, visSr, sc, visEr, ec, true)
}

func (m *RootModel) hasSelection() bool {
	_, _, _, _, ok := m.selectionDocRange()
	return ok || m.viewport.SelDragging
}

func (m *RootModel) scrollBy(delta int) {
	if delta == 0 {
		return
	}
	if delta > 0 && m.viewport.autoScroll {
		// Already parked at the bottom; continuing to scroll downwards cannot
		// scroll past the end of the transcript. Keep autoScroll active so
		// sticky headers never flash during downward inertia scroll.
		return
	}
	vpRows := m.viewport.vpRows
	if vpRows <= 0 {
		vpRows = m.viewportHeight()
	}
	oldScroll := m.viewport.scroll
	oldAutoScroll := m.viewport.autoScroll
	m.viewport.scroll += delta
	if m.viewport.scroll < 0 {
		m.viewport.scroll = 0
		m.viewport.autoScroll = false
	} else if vpRows > 0 && len(m.viewport.rowsView.Rows) > 0 {
		maxScroll := len(m.viewport.rowsView.Rows) - vpRows
		if maxScroll < 0 {
			maxScroll = 0
		}
		if m.viewport.scroll >= maxScroll {
			m.viewport.scroll = maxScroll
			m.viewport.autoScroll = true
		} else {
			m.viewport.autoScroll = false
		}
	} else {
		m.viewport.autoScroll = false
	}
	if m.viewport.scroll != oldScroll || m.viewport.autoScroll != oldAutoScroll {
		m.viewport.viewCacheValid = false
	}
}

// jumpToPinnedPrompt scrolls the viewport so the producing user-prompt row lands at top.
func (m *RootModel) jumpToPinnedPrompt() bool {
	if m.viewport.pinnedPromptRowIdx < 0 {
		return false
	}
	m.viewport.autoScroll = false
	rows, _, _ := m.syncRowsView()
	m.viewport.scroll = tuiviewport.PromptTurnStart(rows, m.viewport.pinnedPromptRowIdx)
	m.viewport.viewCacheValid = false
	return true
}

// gotoTop / gotoBottom are the recovered ctrl+home / ctrl+end bindings.
func (m *RootModel) gotoTop() {
	if m.viewport.scroll == 0 && !m.viewport.autoScroll {
		return
	}
	m.viewport.autoScroll = false
	m.viewport.scroll = 0
	m.viewport.viewCacheValid = false
}

func (m *RootModel) gotoBottom() {
	if m.viewport.autoScroll {
		return
	}
	m.viewport.autoScroll = true
	m.viewport.scroll = 1 << 30
	m.viewport.viewCacheValid = false
}

// pageStep computes the vertical distance one PageUp or PageDown moves,
// accounting for any pinned prompt header lines in altScreen mode (AGY 1.5.4).
func (m *RootModel) pageStep() int {
	step := m.viewport.vpRows - 1
	if m.viewport.pinnedPromptText != "" {
		step -= 2
	}
	return tuilib.ClampInt(step, 1, 1<<20)
}

// scrollPage scrolls by one page and aligns to user prompt message boundaries
// to avoid skipping prompt rows (AGY 1.5.4).
func (m *RootModel) scrollPage(down bool) {
	step := m.pageStep()
	if !down {
		step = -step
	}
	target := m.viewport.scroll + step
	for _, pRow := range m.viewport.userPromptRows {
		if down {
			if target > pRow && target <= pRow+2 {
				target = pRow
				break
			}
		} else {
			if target < pRow && target >= pRow-2 {
				target = pRow
				break
			}
		}
	}
	m.scrollBy(target - m.viewport.scroll)
}

// handleScrollKey consumes the recovered transcript scroll bindings.
//
// The page-scroll distances are the recovered ones: a page is the
// viewport's height less one row, and a half page is half of it.
func (m *RootModel) handleScrollKey(key tea.Key) bool {
	if !m.viewport.altScreen {
		return false
	}
	switch {
	case key.Code == tea.KeyPgUp:
		m.scrollPage(false)
		return true
	case key.Code == tea.KeyPgDown:
		m.scrollPage(true)
		return true
	case key.Code == tea.KeyUp && key.Mod.Contains(tea.ModShift):
		m.scrollBy(-tuilib.ClampInt(m.viewport.vpRows/2, 1, 1<<20))
		return true
	case key.Code == tea.KeyDown && key.Mod.Contains(tea.ModShift):
		m.scrollBy(tuilib.ClampInt(m.viewport.vpRows/2, 1, 1<<20))
		return true
	case key.Code == tea.KeyHome && key.Mod.Contains(tea.ModCtrl):
		m.gotoTop()
		return true
	case key.Code == tea.KeyEnd && key.Mod.Contains(tea.ModCtrl):
		m.gotoBottom()
		return true
	}
	return false
}

// handleMouseWheel scrolls the transcript or active panel on a wheel event.
func (m *RootModel) handleMouseWheel(msg tea.MouseMsg) bool {
	if !m.effectiveAltScreen() && m.overlays.panel.kind != panelregistry.Terminal {
		return false
	}
	if m.prompt.picker.HandleWheel(msg.Mouse()) {
		return true
	}
	up := msg.Mouse().Button == tea.MouseWheelUp
	if m.isBtwCardVisible() && m.IsBtwCardY(msg.Mouse().Y) {
		delta := 1
		if up {
			delta = -1
		}
		if m.btwScrollBy(delta) {
			return true
		}
	}
	if m.overlays.resumePicker != nil && m.overlays.resumePicker.Active() {
		key := tea.Key{Code: tea.KeyDown}
		if up {
			key.Code = tea.KeyUp
		}
		m.overlays.resumePicker.HandleKey(key, m.viewport.height)
		return true
	}
	if m.help.Panel != nil && m.help.Panel.Active() {
		key := tea.Key{Code: tea.KeyDown}
		if up {
			key.Code = tea.KeyUp
		}
		m.help.Panel.Update(key)
		return true
	}
	if m.overlays.panel.kind != panelregistry.None {
		if m.overlays.panel.kind == panelregistry.Terminal {
			if tp := m.terminalPanel(); tp != nil {
				ev := msg.Mouse()
				ev.Y -= m.overlays.panelScreenY
				if tp.HandleWheel(ev) {
					return true
				}
			}
			return true
		}
		if m.overlays.panel.kind == panelregistry.Tasks && m.overlays.panel.tp != nil {
			if m.overlays.panel.tp.HandleWheel(up) {
				m.overlays.panel.cursor = m.overlays.panel.tp.Cursor()
				return true
			}
		}
		if m.overlays.panel.kind == panelregistry.Agents && m.overlays.panel.ag != nil {
			if m.overlays.panel.ag.HandleWheel(up) {
				m.overlays.panel.cursor = m.overlays.panel.ag.Cursor()
				return true
			}
		}
		if m.overlays.panel.kind.IsViewer() {
			step := m.wheelScrollRows()
			if up {
				m.scrollViewerPanel(-step)
			} else {
				m.scrollViewerPanel(step)
			}
			return true
		}
		// For all other interactive panels, dispatch Up/Down navigation keys
		// to the panel handler and ensure the overlay completely absorbs the wheel.
		key := tea.Key{Code: tea.KeyDown}
		if up {
			key.Code = tea.KeyUp
		}
		m.handlePanelKey(key)
		return true
	}
	if m.activeItems.Panel != nil && m.activeItems.Panel.Focused() {
		if m.activeItems.Panel.HandleWheel(up) {
			return true
		}
	}
	step := m.wheelScrollRows()
	switch msg.Mouse().Button {
	case tea.MouseWheelUp:
		m.scrollBy(-step)
		return true
	case tea.MouseWheelDown:
		m.scrollBy(step)
		return true
	}
	return false
}

// wheelScrollRows returns the scroll step for mouse wheel events.
// In upstream agy, the base wheel step is 1 line per notch:
//
//	speed = 1 (or accelerated via render.WheelSpeed)
//	maxSpeed = max(1, availableLines / 3)
//	step = min(speed, maxSpeed) // evaluates to 1 for discrete ticks
//
// In previous versions of pigasus, the formula mistakenly set step = availableLines / 3,
// which evaluated to 2 (or 3) rows on standard terminal sizes.
//
// Upstream:
//   - google3/third_party/jetski/cli/model/model.(*ConversationModel).scrollWheel (0x10303dbd0)
//   - google3/third_party/jetski/cli/render/render.WheelSpeed (0x102e1d340)
//   - google3/third_party/jetski/cli/model/model.(*scrollPanel).handleScroll (0x103137040)
func (m *RootModel) wheelScrollRows() int {
	return mouseWheelRows
}

type wrapSlot = tuiviewport.WrapSlot
type composedRows = tuiviewport.ComposedRows

const (
	mouseWheelRows   = 1
	viewOverflowRows = tuiviewport.ViewOverflowRows
)

func (m *RootModel) currentHeaderKey(width int) headerCacheKey {
	return headerCacheKey{
		width:        width,
		seasonal:     m.viewport.seasonal,
		email:        m.session.email,
		model:        m.session.model,
		tier:         m.session.tier,
		cwd:          m.session.cwd,
		remoteAgent:  m.session.remoteAgent,
		colorScheme:  m.viewport.colorScheme,
		agentWarning: m.session.agentWarning,
		modelWarning: m.session.modelWarning,
		fallbackFrom: m.session.fallbackFrom,
		fallbackTo:   m.session.fallbackTo,
	}
}

func (m *RootModel) scrollHeaderRows(width int) []string {
	key := m.currentHeaderKey(width)
	if m.viewport.cachedHeaderKey == key && len(m.viewport.cachedHeaderRows) > 0 {
		return m.viewport.cachedHeaderRows
	}
	var b strings.Builder
	b.WriteString("\n")
	bannerCwd := m.session.cwd
	if m.session.remoteAgent {
		bannerCwd = "remote workspace"
	}
	renderBannerWithTheme(&b, m.viewport.seasonal, m.session.email, m.session.model, m.session.tier, bannerCwd)
	if m.session.agentWarning != "" {
		b.WriteString("\n")
		b.WriteString(tuilib.RenderAgentWarning(m.session.agentWarning))
	}
	if m.session.modelWarning != "" {
		b.WriteString("\n")
		b.WriteString(tuilib.RenderModelWarning(m.session.modelWarning))
	}
	if m.session.fallbackFrom != "" && m.session.fallbackTo != "" {
		b.WriteString("\n")
		b.WriteString(tuilib.RenderWarning(m.session.fallbackFrom, m.session.fallbackTo))
	}
	var rows []string
	for _, line := range strings.Split(b.String(), "\n") {
		if width > 0 {
			rows = append(rows, tuilib.WrapRows(line, width)...)
		} else {
			rows = append(rows, line)
		}
	}
	m.viewport.cachedHeaderRows = rows
	m.viewport.cachedHeaderKey = key
	return rows
}

func (m *RootModel) wrappedResponse(width int) []string {
	if width == m.viewport.width {
		if m.viewport.rowsCacheDirty || !m.viewport.rowsCache.Built || m.viewport.rowsCache.Width != width {
			if m.viewport.rowsCache.Rebuild(m.conversation.response, width) {
				m.viewport.rowsGen++
			}
			m.viewport.rowsCacheDirty = false
		}
		return m.viewport.rowsCache.Rows
	}
	if !m.viewport.rowsCacheN1.Built || m.viewport.rowsCacheN1.Width != width {
		if m.viewport.rowsCacheN1.Rebuild(m.conversation.response, width) {
			m.viewport.rowsGen++
		}
	}
	return m.viewport.rowsCacheN1.Rows
}

func (m *RootModel) wrappedResponseCounts(width int) []int {
	_ = m.wrappedResponse(width)
	if width == m.viewport.width {
		return m.viewport.rowsCache.Counts
	}
	return m.viewport.rowsCacheN1.Counts
}

func attachRightIndicator(left string, ind string, width int) string {
	if ind == "" {
		if width > 0 {
			return tuilib.Truncate(left, width, "...")
		}
		return left
	}
	indStyled := palette.DimGrayOpen + ind + palette.DimGrayClose
	indW := ansi.StringWidth(ind)
	if width <= 0 {
		return left + "  " + indStyled
	}
	if width <= indW {
		return tuilib.Truncate(indStyled, width, "")
	}
	maxLeft := width - indW - 1
	leftW := ansi.StringWidth(left)
	if leftW > maxLeft {
		left = tuilib.Truncate(left, maxLeft, "...")
		leftW = ansi.StringWidth(left)
	}
	pad := width - leftW - indW
	if pad < 1 {
		pad = 1
	}
	return left + strings.Repeat(" ", pad) + indStyled
}

func (m *RootModel) makeActivityPhaseKey() string {
	if _, ok := m.conversation.ModelRetryStatus(); ok {
		return ""
	}
	if (m.conversation.thinking || m.audio.thinking) && m.conversation.activeTool == "" {
		return "thinking"
	}
	if m.conversation.activeTool != "" {
		if m.conversation.phaseTimer.active && strings.HasPrefix(m.conversation.phaseTimer.key, "tool:") && strings.HasSuffix(m.conversation.phaseTimer.key, ":"+m.conversation.activeTool) {
			return m.conversation.phaseTimer.key
		}
		return "tool:" + m.conversation.activeTool
	}
	if m.conversation.anyToolRun {
		return "working"
	}
	return "generating"
}

func (m *RootModel) liveRows(width int) []string {
	if (!m.conversation.streaming && !m.audio.thinking) || m.hasActivePermissionPrompt() {
		return nil
	}
	var rows []string
	speed := settings.CachedSnapshot().String("runningLightSpeed")
	line := m.conversation.spinner.View()
	if speed == "off" && len(m.conversation.spinner.Spinner.Frames) > 0 {
		line = m.conversation.spinner.Style.Render(m.conversation.spinner.Spinner.Frames[0])
	}
	phrase := "Generating"
	if m.conversation.anyToolRun {
		phrase = "Working"
	}
	now := time.Now()
	nowMs := now.UnixMilli()
	if key := m.makeActivityPhaseKey(); key != "" {
		m.conversation.phaseTimer.track(key, now)
	} else {
		m.conversation.phaseTimer.freeze(now)
	}
	elapsedSuffix := ""
	if d := m.conversation.phaseTimer.elapsed(now); d >= time.Second {
		elapsedSuffix = " " + palette.DimGrayOpen + d.Truncate(time.Second).String() + palette.DimGrayClose
	}
	if retryText, ok := m.conversation.ModelRetryStatus(); ok {
		line += " " + retryText
	} else if (m.conversation.thinking || m.audio.thinking) && m.conversation.activeTool == "" {
		if m.conversation.liveThinkingSnippet != "" {
			line += " " + tuitools.RunningLight(m.conversation.liveThinkingSnippet, speed, nowMs) + elapsedSuffix
		} else {
			line += " " + tuitools.RunningLight("Thinking...", speed, nowMs) + elapsedSuffix
		}
	} else if m.conversation.activeTool != "" {
		label := m.conversation.activeTool
		if !strings.HasSuffix(label, "...") {
			label += "..."
		}
		line += " " + tuitools.RunningLight(label, speed, nowMs) + elapsedSuffix
	} else {
		line += " " + tuitools.RunningLight(phrase+"...", speed, nowMs) + elapsedSuffix
	}
	if summary := m.conversation.stepTray.statusSummary(); summary != "" {
		line += " " + palette.DimGrayOpen + "· " + summary + palette.DimGrayClose
	}

	ind := ""
	if m.viewport.altScreen && !m.viewport.autoScroll {
		ind = m.scrollPositionIndicator()
	}

	hasTip := false
	var tipLine string
	if m.viewport.activeTip != "" {
		tipLine = tuitools.FormatTip(m.viewport.activeTip)
		if tipLine != "" {
			hasTip = true
		}
	}

	if hasTip {
		if width > 0 {
			line = tuilib.Truncate(line, width, "...")
		}
		rows = append(rows, line)
		if ind != "" {
			tipLine = attachRightIndicator(tipLine, ind, width)
		} else if width > 0 {
			tipLine = tuilib.Truncate(tipLine, width, "...")
		}
		rows = append(rows, tipLine)
	} else {
		if ind != "" {
			line = attachRightIndicator(line, ind, width)
		} else if width > 0 {
			line = tuilib.Truncate(line, width, "...")
		}
		rows = append(rows, line)
	}
	if trayRows := m.conversation.stepTray.renderTreeRows(width); len(trayRows) > 0 {
		rows = append(rows, trayRows...)
	}
	return rows
}

func (m *RootModel) transcriptRows(width int) []string {
	if m.prompt.picker.Visible() {
		return nil
	}
	header := m.scrollHeaderRows(width)
	rows := make([]string, 0, len(header)+len(m.conversation.response))
	rows = append(rows, header...)
	for _, line := range m.conversation.response {
		rows = append(rows, tuilib.WrapRows(line, width)...)
	}
	return rows
}

func (m *RootModel) syncRowsView() ([]string, []string, []string) {
	header := m.scrollHeaderRows(m.viewport.width)
	resp := m.wrappedResponse(m.viewport.width)
	respCounts := m.wrappedResponseCounts(m.viewport.width)
	if !m.viewport.rowsView.Fresh(m.viewport.rowsGen, m.viewport.width, header, nil) {
		m.viewport.rowsView.Rebuild(m.viewport.rowsGen, m.viewport.width, header, resp, nil, respCounts)
	}
	m.viewport.userPromptRows = m.viewport.rowsView.Prompt
	return m.viewport.rowsView.Rows, header, resp
}

func (m *RootModel) updateStickyPrompt(rows, header, resp []string, avail int) int {
	m.viewport.stickyBarRow = ""
	m.viewport.pinnedPromptRowIdx = -1
	m.viewport.pinnedPromptText = ""
	if !m.viewport.altScreen || m.viewport.autoScroll || avail <= 2 {
		return avail
	}
	if idx := tuiviewport.StickyUserPromptRow(m.viewport.userPromptRows, m.viewport.autoScroll, m.viewport.scroll); idx >= 0 {
		if !tuiviewport.PromptTurnVisible(rows, idx, m.viewport.scroll, m.viewport.userPromptRows) {
			respIdx := idx - len(header)
			if respIdx >= 0 && respIdx < len(resp) {
				m.viewport.pinnedPromptRowIdx = idx
				m.viewport.pinnedPromptText = tuiviewport.RenderPinnedPrompt(resp[respIdx], m.viewport.width)
			}
		}
	}
	availScroll := avail
	if m.viewport.pinnedPromptText != "" {
		availScroll -= 2
	}
	if availScroll < 1 {
		availScroll = 1
	}
	return availScroll
}

func (m *RootModel) computeVisibleWindow(rows []string, availScroll int) []string {
	visible, scroll, auto := tuiviewport.ComputeVisibleWindow(rows, m.viewport.rowsView.Prefix, m.viewport.scroll, availScroll, m.viewport.autoScroll)
	m.viewport.scroll = scroll
	m.viewport.autoScroll = auto
	return visible
}

type extraRowsCacheKey struct {
	width            int
	height           int
	altScreen        bool
	autoScroll       bool
	hasScrollInd     bool
	streaming        bool
	thinking         bool
	audioThinking    bool
	pickerVis        bool
	atFileVis        bool
	resumeVis        bool
	panelKind        panelregistry.Kind
	helpActive       bool
	askActive        bool
	hasModal         bool
	fallbackFrom     string
	fallbackTo       string
	sendingLen       int
	streamQueueLen   int
	bashQueueLen     int
	pendingMediaLen  int
	mediaExpanded    bool
	hasBtw           bool
	inputVal         string
	statusVisible    string
	bashMode         bool
	activeItemsRows  int
	stepTrayRows     int
	permissionPrompt bool
}

func (m *RootModel) currentExtraRowsKey() extraRowsCacheKey {
	hasScrollInd := false
	if m.viewport.altScreen && !m.viewport.autoScroll {
		hasScrollInd = m.scrollPositionIndicator() != ""
	}
	activeItemsRows := 0
	if m.overlays.panel.kind == panelregistry.None && m.activeItems.Panel != nil {
		activeItemsRows = m.activeItems.Panel.VisibleRowCount()
	}
	stepTrayRows := 0
	if m.conversation.streaming || m.audio.thinking {
		stepTrayRows = m.conversation.stepTray.treeRowCount()
	}
	return extraRowsCacheKey{
		width:            m.viewport.width,
		height:           m.viewport.height,
		altScreen:        m.viewport.altScreen,
		autoScroll:       m.viewport.autoScroll,
		hasScrollInd:     hasScrollInd,
		streaming:        m.conversation.streaming,
		thinking:         m.conversation.thinking,
		audioThinking:    m.audio.thinking,
		pickerVis:        m.prompt.picker.Visible(),
		atFileVis:        m.prompt.atFilePicker.Visible(),
		resumeVis:        m.overlays.resumePicker.Active(),
		panelKind:        m.overlays.panel.kind,
		helpActive:       m.help.Panel.Active(),
		askActive:        m.askQuestion.Active(),
		hasModal:         m.hasActiveModal(),
		fallbackFrom:     m.session.fallbackFrom,
		fallbackTo:       m.session.fallbackTo,
		sendingLen:       len(m.session.store.SendingMessages),
		streamQueueLen:   len(m.session.store.StreamQueue),
		bashQueueLen:     len(m.session.store.BashQueue),
		pendingMediaLen:  len(m.prompt.pendingMedia),
		mediaExpanded:    m.prompt.mediaExpanded,
		hasBtw:           m.isBtwCardVisible(),
		inputVal:         m.prompt.input.Value(),
		statusVisible:    m.statusLine.visible,
		bashMode:         m.session.store.BashMode,
		activeItemsRows:  activeItemsRows,
		stepTrayRows:     stepTrayRows,
		permissionPrompt: m.hasActivePermissionPrompt(),
	}
}

func (m *RootModel) viewportHeight() int {
	if m.viewport.height <= 0 {
		return 0
	}
	key := m.currentExtraRowsKey()
	if m.viewport.extraRowsValid && m.viewport.cachedExtraKey == key {
		return m.viewport.cachedExtraRows
	}
	savedIndY := m.viewport.scrollIndScreenY
	savedIndBody := m.viewport.scrollIndBodyStart
	savedIndStartX := m.viewport.scrollIndStartX
	savedIndEndX := m.viewport.scrollIndEndX
	savedPromptY := m.prompt.promptScreenY
	savedBtwY := m.overlays.btwScreenY

	body, _ := m.viewBody(nil)
	extra := tuilib.ViewRows(body)

	m.viewport.scrollIndScreenY = savedIndY
	m.viewport.scrollIndBodyStart = savedIndBody
	m.viewport.scrollIndStartX = savedIndStartX
	m.viewport.scrollIndEndX = savedIndEndX
	m.prompt.promptScreenY = savedPromptY
	m.overlays.btwScreenY = savedBtwY
	if m.help.Panel.Active() {
		extra -= tuilib.ViewRows(m.help.Panel.Rows())
	}
	budget := m.viewport.height
	if m.help.Panel.Active() {
		budget += viewOverflowRows
	}
	avail := budget - extra
	if avail < 1 {
		avail = 1
	}
	m.viewport.cachedExtraRows = avail
	m.viewport.cachedExtraW = m.viewport.width
	m.viewport.cachedExtraH = m.viewport.height
	m.viewport.cachedExtraKey = key
	m.viewport.extraRowsValid = true
	return avail
}

func (m *RootModel) transcriptWindow() []string {
	avail := m.viewportHeight()
	m.ensureVisibleBlocksExpanded(avail)
	if avail <= 0 {
		m.viewport.vpRows = 0
		return m.transcriptRows(m.viewport.width)
	}
	rows, header, resp := m.syncRowsView()
	if len(rows) <= avail {
		m.viewport.scroll = 0
		m.viewport.autoScroll = true
		m.viewport.pinnedPromptText = ""
		m.viewport.pinnedPromptRowIdx = -1
		m.viewport.stickyBarRow = ""
		m.viewport.vpRows = avail
		if m.viewport.altScreen && !m.help.Panel.Active() && !m.hasActivePermissionPrompt() && len(rows) < avail {
			padded := make([]string, avail)
			copy(padded, rows)
			return padded
		}
		return rows
	}
	maxScroll := len(rows) - avail
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.viewport.autoScroll || m.viewport.scroll >= maxScroll {
		m.viewport.scroll = maxScroll
		m.viewport.autoScroll = true
		m.viewport.pinnedPromptText = ""
		m.viewport.pinnedPromptRowIdx = -1
		m.viewport.stickyBarRow = ""
		m.viewport.vpRows = avail
		if maxScroll > len(rows) {
			maxScroll = len(rows)
		}
		visible := rows[maxScroll:]
		return tuiviewport.FormatVisibleRows(visible, m.viewport.width, m.viewport.pinnedPromptText, m.viewport.stickyBarRow, m.viewport.altScreen)
	}
	availScroll := m.updateStickyPrompt(rows, header, resp, avail)
	if availScroll <= 0 {
		m.viewport.vpRows = avail
		return nil
	}
	visible := m.computeVisibleWindow(rows, availScroll)
	out := tuiviewport.FormatVisibleRows(visible, m.viewport.width, m.viewport.pinnedPromptText, m.viewport.stickyBarRow, m.viewport.altScreen)
	m.viewport.vpRows = avail
	return out
}
