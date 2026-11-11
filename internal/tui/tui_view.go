package tui

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/app/agentturn"
	_ "github.com/justwasm/pigasus/internal/app/agentturn/plugins/all"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	subagentrouter "github.com/justwasm/pigasus/internal/app/subagent/router"
	"github.com/justwasm/pigasus/internal/app/tools"
	"github.com/justwasm/pigasus/internal/app/tools/shell"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/cliflags"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	"github.com/justwasm/pigasus/internal/core/settings"
	modelcatalog "github.com/justwasm/pigasus/internal/features/models/catalog"
	"github.com/justwasm/pigasus/internal/features/tools/bashmode"
	"github.com/justwasm/pigasus/internal/features/tools/clipboard"
	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	uirows "github.com/justwasm/pigasus/internal/ui/rows"
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
	"github.com/justwasm/pigasus/internal/ui/transcript"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/internal/ui/tuitools"
	"github.com/justwasm/pigasus/internal/ui/views/announce"
	"github.com/justwasm/pigasus/internal/ui/views/programstatus"
	"github.com/justwasm/pigasus/internal/ui/views/statusline"
	"github.com/justwasm/pigasus/palette"
	"github.com/justwasm/pigasus/procgroup"
)

// ---- tui_view.go ----

func (m *RootModel) clipViewBody(body string, cursorY int) (string, int) {
	if m.viewport.height <= 0 {
		return body, cursorY
	}
	rows := tuilib.ViewRows(body)
	maxRows := m.viewport.height + viewOverflowRows
	drop := rows - m.viewport.height
	if drop < 0 {
		drop = 0
	}
	if rows > maxRows || drop > 0 {
		lines := strings.Split(body, "\n")
		if n := len(lines); n > 0 && lines[n-1] == "" {
			lines = lines[:n-1]
		}
		keep := len(lines)
		if keep > maxRows {
			keep = maxRows
		}
		if drop > keep {
			drop = keep
		}
		if drop < keep {
			body = strings.Join(lines[drop:keep], "\n")
		} else {
			body = ""
		}
		cursorY -= drop
		if cursorY < 0 {
			cursorY = -1
		}
	}
	m.prompt.picker.ClipBody(drop)
	if m.prompt.promptBodyStart >= drop {
		m.prompt.promptScreenY = m.prompt.promptBodyStart - drop
	}
	return body, cursorY
}

// isFullViewportPanel reports whether the active panel occupies the full terminal viewport,
// replacing the conversation transcript, live rows, and prompt box.
func (m *RootModel) isFullViewportPanel() bool {
	kind := m.overlays.panel.kind
	if kind == panelregistry.None {
		return false
	}
	if kind == panelregistry.Changelog || kind == panelregistry.Diff || kind == panelregistry.FeatDiff {
		return true
	}
	if kind == panelregistry.Tasks && m.overlays.panel.tp != nil && m.overlays.panel.tp.Detail() {
		return true
	}
	if kind == panelregistry.Agents && m.overlays.panel.ag != nil && m.overlays.panel.ag.DetailMode() {
		return true
	}
	if kind == panelregistry.Skills && m.overlays.panel.sk != nil && m.overlays.panel.sk.Detail() {
		return true
	}
	return false
}

// requiresAltScreen reports whether the current view/panel demands alternate screen mode.
func (m *RootModel) requiresAltScreen() bool {
	if m.isAltScreenDisabled() {
		return false
	}
	if m.isFullViewportPanel() {
		return true
	}
	kind := m.overlays.panel.kind
	if kind == panelregistry.CodeSearch || kind == panelregistry.Rewind {
		return true
	}
	return false
}

// isAltScreenDisabled reports whether altScreenMode is explicitly disabled in settings.
func (m *RootModel) isAltScreenDisabled() bool {
	stored, err := settings.Field("altScreenMode")
	if err == nil && stored != nil {
		if s, ok := stored.(string); ok {
			s = strings.ToLower(strings.TrimSpace(s))
			if s == "never" || s == "off" || s == "false" || s == "0" {
				return true
			}
		}
	}
	return false
}

// effectiveAltScreen returns the actual alternate screen state for the frame:
// true if session altScreen is enabled, or if the current active view demands altScreen.
func (m *RootModel) effectiveAltScreen() bool {
	if m.isAltScreenDisabled() {
		return false
	}
	return m.viewport.altScreen || m.requiresAltScreen()
}

// applyTerminalSettings applies the session's terminal settings to a
// view.
//
// Every frame the program renders has to carry them: bubbletea reads
// AltScreen off the view, so a view that forgets it makes the
// renderer leave the alternate screen in the middle of the session.
// The login screen renders its own view (it is a whole-screen modal),
// but it lives inside this program — it must not drop the terminal out
// of the alternate screen that the session started in.
func (m *RootModel) applyTerminalSettings(v tea.View) tea.View {
	altScreen := m.effectiveAltScreen()
	v.AltScreen = altScreen
	if altScreen {
		v.MouseMode = tea.MouseModeCellMotion
	}
	return v
}

func (m *RootModel) shouldShowInputCursor(cursorX, cursorY int) bool {
	if m.help.Panel.Active() {
		return false
	}
	if m.swarm.Board != nil && m.swarm.Board.Active() {
		return false
	}
	if m.overlays.panel.kind != panelregistry.None {
		return false
	}
	if m.isFullViewportPanel() {
		return false
	}
	if m.conversation.streaming || m.conversation.thinking {
		return false
	}
	if m.session.store.BashRunning() {
		return false
	}
	if !m.prompt.input.Focused() {
		return false
	}
	if m.hasSelection() || m.prompt.HasInputSelection() {
		return false
	}
	if len(m.overlays.btwCards) > 0 || m.overlays.activeBtwID != "" {
		return false
	}
	if m.overlays.HasActiveModal() {
		return false
	}
	if m.overlays.resumePicker != nil && m.overlays.resumePicker.Active() {
		return false
	}
	if m.audio.Surface != nil && m.audio.Surface.Active() {
		return false
	}
	if m.activeItems.Panel != nil && m.activeItems.Panel.Focused() {
		return false
	}
	if m.askQuestion.panel != nil && m.askQuestion.panel.Active() {
		return false
	}
	// The turn-level question view replaces the composer; the real
	// terminal cursor would otherwise keep blinking at the old composer
	// position while the form is up. The write-in textarea draws its own
	// virtual cursor.
	if m.askQuestion.Active() {
		return false
	}
	if m.hasActivePermissionPrompt() {
		return false
	}
	if cursorY < 0 || (m.viewport.height > 0 && cursorY >= m.viewport.height) {
		return false
	}
	if cursorX < 0 || (m.viewport.width > 0 && cursorX >= m.viewport.width) {
		return false
	}
	return true
}

func (m *RootModel) buildTeaView(body string, cursorX, cursorY int) tea.View {
	v := tea.NewView(body)
	v = m.applyTerminalSettings(v)
	if title := m.windowTitleText(); title != "" {
		v.WindowTitle = title
	}
	v.ProgramStatus = m.programStatus()
	if m.shouldShowInputCursor(cursorX, cursorY) {
		v.Cursor = &tea.Cursor{
			Position: tea.Position{X: cursorX, Y: cursorY},
			Shape:    tea.CursorBlock,
			Blink:    true,
		}
	}
	return v
}

func (m *RootModel) isViewCacheValid() bool {
	if !m.viewport.viewCacheValid || m.viewport.rowsCacheDirty || m.viewport.cachedView.Content == "" {
		return false
	}
	if m.viewport.cachedStreaming != m.conversation.streaming || m.conversation.streaming {
		return false
	}
	if m.conversation.thinking || m.session.store.BashRunning() {
		return false
	}
	if m.viewport.cachedPickerVis != m.prompt.picker.Visible() || m.viewport.cachedPanelKind != m.overlays.panel.kind {
		return false
	}
	if m.swarm.Board != nil && m.swarm.Board.Active() {
		return false
	}
	if m.prompt.picker.Visible() || m.hasActiveModal() || (m.audio.Surface != nil && m.audio.Surface.Active()) || (m.activeItems.Panel != nil && m.activeItems.Panel.Focused()) || m.askQuestion.Active() || (m.askQuestion.panel != nil && m.askQuestion.panel.Active()) || m.hasSelection() || m.prompt.HasInputSelection() {
		return false
	}
	if m.viewport.cachedScroll != m.viewport.scroll || m.viewport.cachedAutoScroll != m.viewport.autoScroll {
		return false
	}
	if m.viewport.cachedW != m.viewport.width || m.viewport.cachedH != m.viewport.height {
		return false
	}
	if m.viewport.cachedRespLen != len(m.conversation.response) || m.viewport.cachedRowsGen != m.viewport.rowsGen {
		return false
	}
	if m.viewport.cachedHeaderKey != m.currentHeaderKey(m.viewport.width) {
		return false
	}
	return true
}

func (m *RootModel) View() tea.View {
	// The frame is painted with the selected scheme's colours: the
	// palette resolves them from the scheme the session wears.
	restore := palette.PushScheme(m.viewport.colorScheme)
	defer restore()
	if !m.prompt.picker.Visible() {
		m.prompt.picker.ResetGeom()
	}
	if m.auth.login != nil {
		// The login screen owns its own layout, but it still runs in
		// this program's terminal: keep the session's alternate-screen
		// setting, or the renderer leaves it (the /accounts → a flow
		// used to drop the session out of the alt screen).
		// Note: the login screen does not use in-app transcript mouse
		// selection, so mouse tracking is left off (MouseModeNone) so
		// that the terminal can handle native mouse text selection,
		// copying, and Cmd+click / click on OSC 8 links without
		// application interception.
		v := m.auth.login.View()
		v.AltScreen = m.effectiveAltScreen()
		return v
	}
	if m.swarm.Board != nil && m.swarm.Board.Active() {
		m.swarm.Board.SetSize(m.viewport.width, m.viewport.height)
		return m.applyTerminalSettings(tea.NewView(m.swarm.Board.View()))
	}
	if m.isViewCacheValid() {
		return m.viewport.cachedView
	}
	var transcript []string
	if m.viewport.height > 0 {
		transcript = m.transcriptWindow()
	} else {
		transcript = m.transcriptRows(m.viewport.width)
	}
	transcript = m.selectionPass(transcript)
	body, cursorY := m.viewBody(transcript)
	if m.viewport.width > 0 && m.overlays.panel.kind != panelregistry.Changelog {
		body = tuilib.FitLines(body, m.viewport.width)
	}
	body, cursorY = m.clipViewBody(body, cursorY)
	cursorX := m.inputCursorX()
	v := m.buildTeaView(body, cursorX, cursorY)
	m.viewport.cachedView = v
	m.viewport.cachedScroll = m.viewport.scroll
	m.viewport.cachedAutoScroll = m.viewport.autoScroll
	m.viewport.cachedW = m.viewport.width
	m.viewport.cachedH = m.viewport.height
	m.viewport.cachedRespLen = len(m.conversation.response)
	m.viewport.cachedRowsGen = m.viewport.rowsGen
	m.viewport.cachedPickerVis = m.prompt.picker.Visible()
	m.viewport.cachedPanelKind = m.overlays.panel.kind
	m.viewport.cachedStreaming = m.conversation.streaming
	m.viewport.viewCacheValid = !m.conversation.streaming && !m.conversation.thinking && !m.session.store.BashRunning() && m.shouldShowInputCursor(cursorX, cursorY)
	return v
}

func (m *RootModel) renderBodyQueuesAndCard(b *strings.Builder) {
	if !m.prompt.picker.Visible() && !m.hasActiveModal() {
		if row := m.overlays.announceQueue.Row(); row != "" {
			b.WriteString(row)
		}
		if row := announce.RenderNoticeRow(m.overlays.eligibilityNotice.Reason, m.overlays.eligibilityNotice.VerificationURL, m.viewport.hyperlinkCaps); row != "" {
			b.WriteString(row)
		}
	}
	if len(m.session.store.SendingMessages) > 0 {
		for _, sending := range m.session.store.SendingMessages {
			b.WriteString(uirows.SendingMessageRow(sending) + "\n")
		}
		b.WriteString(m.separator() + "\n")
	}
	if len(m.session.store.StreamQueue) > 0 || len(m.session.store.BashQueue) > 0 {
		for i, queued := range m.session.store.StreamQueue {
			isLast := i == len(m.session.store.StreamQueue)-1 && len(m.session.store.BashQueue) == 0
			withHint := isLast && m.conversation.streaming
			b.WriteString(uirows.QueuedPromptRowWithHint(queued, withHint) + "\n")
		}
		for _, queued := range m.session.store.BashQueue {
			b.WriteString(uirows.QueuedCommandRow(queued) + "\n")
		}
		b.WriteString(m.separator() + "\n")
	}
	if row := m.aboveInputCard(); row != "" {
		b.WriteString(row + "\n")
	}
}

func (m *RootModel) syncInputPlaceholder() {
	switch {
	case m.prompt.picker.Visible() || m.hasActiveModal() || m.audio.Surface.Active():
		m.prompt.input.Placeholder = ""
	case m.viewport.inputNotice != "":
		m.prompt.input.Placeholder = m.viewport.inputNotice
	case m.prompt.modeHintDismissed:
		m.prompt.input.Placeholder = ""
	default:
		m.prompt.input.Placeholder = permissions.ModeBanner(m.session.mode)
	}
}

func (m *RootModel) formatInputView() string {
	m.syncInputPlaceholder()
	inputView := m.prompt.input.View()
	inputView = strings.Replace(inputView, cBlueOpen+"> \x1b[m", cBlueOpen+">\x1b[m ", 1)
	if !strings.Contains(inputView, cBlueOpen+">\x1b[m ") {
		inputView = strings.Replace(inputView, "\x1b[38;5;69m> \x1b[m", "\x1b[38;5;69m>\x1b[m ", 1)
	}
	// The reference colours the command token in the composer and shows
	// the argument hint of an empty "/model " argument
	// (【一手】 agy 1.4.6, live — internal/tuilib/composer_token.go).
	composerValue := tuilib.FirstLine(m.prompt.input.Value())
	inputView = tuilib.StyleComposerCommandToken(inputView, composerValue)
	if hint, ok := tuilib.ComposerArgumentHint(composerValue); ok {
		inputView = tuilib.AppendComposerHint(inputView, cDimGrayOpen+hint+cDimGrayClose, composerValue)
	}
	if m.session.store.BashMode {
		inputView = bashmode.PromptGlyph(inputView, cBlueOpen)
	}
	if !strings.HasSuffix(inputView, "\n") {
		inputView += "\n"
	}
	if m.prompt.HasInputSelection() {
		inputView = m.prompt.ApplyInputSelectionHighlight(inputView)
	}
	return inputView
}

func (m *RootModel) renderBodyPrompt(b *strings.Builder) int {
	b.WriteString(m.promptRule(true) + "\n")
	m.renderBodyQueuesAndCard(b)

	rowsAbovePrompt := tuilib.ViewRows(b.String())
	m.prompt.promptBodyStart = rowsAbovePrompt

	// The attachments of the next turn (agy 1.4.9's image pasting) sit
	// above the composer, so the user can see what will be sent.
	for _, row := range uirows.AttachmentRows(m.prompt.pendingMedia, m.prompt.mediaExpanded) {
		b.WriteString(row + "\n")
		rowsAbovePrompt++
	}

	if m.askQuestion.Active() {
		b.WriteString(m.askQuestion.View())
		b.WriteString(m.promptRule(false) + "\n")
		return rowsAbovePrompt
	}

	inputView := m.formatInputView()
	b.WriteString(inputView)

	// The reference answers a turn the user cancelled with Esc with a
	// hint in the composer, not with an error row in the transcript.
	if hint := m.interruptedHintRow(); hint != "" {
		b.WriteString(hint + "\n")
	}

	cursorY := rowsAbovePrompt
	if c := m.prompt.input.Cursor(); c != nil {
		cursorY += c.Y
	}
	b.WriteString(m.promptRule(false) + "\n")
	if m.overlays.panel.kind == panelregistry.None {
		if activeView := m.activeItems.Panel.Rows(); activeView != "" {
			b.WriteString(activeView + "\n")
			b.WriteString(m.separator() + "\n")
		}
	}
	return cursorY
}

func (m *RootModel) renderBodyOverlays(b *strings.Builder) {
	m.prompt.picker.SetBodyStart(tuilib.ViewRows(b.String()))
	if pickerRows := m.prompt.picker.Rows(); pickerRows != "" {
		b.WriteString(pickerRows)
	}
	if atFileRows := m.prompt.atFilePicker.Rows(); atFileRows != "" {
		b.WriteString(atFileRows)
	}
	if resumeRows := m.overlays.resumePicker.View(m.viewport.width, m.viewport.height); resumeRows != "" {
		b.WriteString(resumeRows)
	}
	if panelRows := m.panelView(); panelRows != "" {
		b.WriteString(panelRows)
	}
	if permissionRows := m.permissionPromptLines(m.viewport.width); len(permissionRows) > 0 {
		for _, row := range permissionRows {
			b.WriteString(row + "\n")
		}
	}
	if voiceRows := m.audio.Surface.View(); voiceRows != "" {
		b.WriteString(voiceRows)
	}
	if m.overlays.panel.kind != panelregistry.Help && m.help.Panel.Active() {
		b.WriteString(m.help.Panel.Rows())
	}
}

func (m *RootModel) renderBodyStatusLine(b *strings.Builder) {
	if m.statusLine.visible != "off" && !(m.overlays.panel.kind == panelregistry.Changelog && m.viewport.height > 0) {
		if rows := m.askQuestion.panel.AlertRows(); len(rows) > 0 {
			for _, row := range rows {
				b.WriteString(row + "\n")
			}
		}
		if !m.askQuestion.Active() && m.askQuestion.panel != nil {
			if rows := m.askQuestion.panel.PickerRows(); len(rows) > 0 {
				for _, row := range rows {
					b.WriteString(row + "\n")
				}
			}
		}
		m.writeStatusArea(b)
	}
}

// writeStatusArea writes the status area's rows. The last row carries no
// trailing newline: it is the last thing on the screen.
func (m *RootModel) writeStatusArea(b *strings.Builder) {
	rows := statusline.AreaRows(m.statusAreaState())
	for i, row := range rows {
		if i < len(rows)-1 {
			b.WriteString(row + "\n")
			continue
		}
		b.WriteString(row)
	}
}

func (m *RootModel) viewBody(transcript []string) (string, int) {
	var b strings.Builder
	m.prompt.picker.ResetGeom()
	m.prompt.promptScreenY = -1

	if m.isFullViewportPanel() && m.viewport.height > 0 {
		b.WriteString(m.panelView())
		if m.statusLine.visible != "off" {
			m.writeStatusArea(&b)
		}
		return b.String(), -1
	}

	for _, line := range transcript {
		b.WriteString(line + "\n")
	}

	for _, line := range m.liveRows(m.viewport.width) {
		b.WriteString(line + "\n")
	}

	cursorY := m.renderBodyPrompt(&b)
	m.renderBodyOverlays(&b)
	m.renderBodyStatusLine(&b)

	return b.String(), cursorY
}

func (m *RootModel) inputCursorX() int {
	if c := m.prompt.input.Cursor(); c != nil {
		return c.X
	}
	return 0
}

// programStatus reports Wagyu's current state to terminals that support the
// Program Status Protocol.
func (m *RootModel) programStatus() *tea.ProgramStatus {
	return programstatus.Resolve(programstatus.Inputs{
		PendingPermission:  m.session.pendingPermission,
		HasPendingQuestion: m.askQuestion.panel.HasPendingQuestion(),
		PendingAuth:        m.session.pendingAuth,
		IsAgentBusy:        m.isAgentBusy(),
		LastTurnOutcome:    m.display.lastTurnOutcome,
	})
}

// isAgentBusy is true while a model turn is in flight.
func (m *RootModel) isAgentBusy() bool {
	return m.conversation.streaming || (m.turn.activeBridge != nil && m.turn.activeBridge.IsRunning()) || m.conversation.thinking
}

// recordTurnOutcome records how the open turn ended.
func (m *RootModel) recordTurnOutcome(state tea.ProgramState) {
	m.display.lastTurnOutcome = state
}

// clearLastTurnOutcome clears the recorded outcome.
func (m *RootModel) clearLastTurnOutcome() {
	m.display.lastTurnOutcome = ""
}

// ---- tui_viewport_scroll.go ----

const selHighlightOpen = tuiviewport.SelHighlightOpen

func (m *RootModel) selectionText() string {
	if !m.viewport.altScreen {
		return ""
	}
	sr, sc, er, ec, ok := tuiviewport.SelectionRange(m.viewport.SelStartRow, m.viewport.SelStartCol, m.viewport.SelEndRow, m.viewport.SelEndCol, m.viewport.SelDragging)
	if !ok {
		return ""
	}
	return tuiviewport.SelectionText(m.transcriptWindow(), sr, sc, er, ec)
}

func (m *RootModel) copySelectionCmd(text string) tea.Cmd {
	if text == "" {
		return nil
	}
	return tea.SetClipboard(text)
}

func (m *RootModel) selectionPass(rows []string) []string {
	sr, sc, er, ec, ok := tuiviewport.SelectionRange(m.viewport.SelStartRow, m.viewport.SelStartCol, m.viewport.SelEndRow, m.viewport.SelEndCol, m.viewport.SelDragging)
	return tuiviewport.SelectionPass(rows, sr, sc, er, ec, ok)
}

func (m *RootModel) hasSelection() bool {
	_, _, _, _, ok := tuiviewport.SelectionRange(m.viewport.SelStartRow, m.viewport.SelStartCol, m.viewport.SelEndRow, m.viewport.SelEndCol, m.viewport.SelDragging)
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
	oldScroll := m.viewport.scroll
	oldAutoScroll := m.viewport.autoScroll
	m.viewport.scroll += delta
	if m.viewport.scroll < 0 {
		m.viewport.scroll = 0
		m.viewport.autoScroll = false
	} else if m.viewport.vpRows > 0 && len(m.viewport.rowsView.Rows) > 0 {
		maxScroll := len(m.viewport.rowsView.Rows) - m.viewport.vpRows
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
	if !m.effectiveAltScreen() {
		return false
	}
	if m.prompt.picker.HandleWheel(msg.Mouse()) {
		return true
	}
	up := msg.Mouse().Button == tea.MouseWheelUp
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
			for i := 0; i < step; i++ {
				key := tea.Key{Code: tea.KeyDown}
				if up {
					key.Code = tea.KeyUp
				}
				m.handleViewerPanelKey(key)
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

// wheelScrollRows returns the dynamic scroll step for mouse wheel events,
// deriving the step from the available viewport height:
//
//	availableLines = totalHeight - chromeLines
//	step = max(1, availableLines / 3)
//	step = min(step, mouseWheelRows)
//
// Upstream: google3/third_party/jetski/cli/model/model.(*ConversationModel).scrollWheel (0x102bb5430)
func (m *RootModel) wheelScrollRows() int {
	avail := m.viewportHeight()
	step := avail / 3
	if step < 1 {
		step = 1
	}
	if step > mouseWheelRows {
		step = mouseWheelRows
	}
	return step
}

type wrapSlot = tuiviewport.WrapSlot
type composedRows = tuiviewport.ComposedRows

const (
	mouseWheelRows   = 3
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
		agentWarning: m.session.agentWarning,
		fallbackFrom: m.session.fallbackFrom,
		fallbackTo:   m.session.fallbackTo,
	}
}

func (m *RootModel) scrollHeaderRows(width int) []string {
	key := m.currentHeaderKey(width)
	if m.viewport.cachedHeaderKey == key && len(m.viewport.cachedHeaderRows) > 0 && !m.viewport.rowsCacheDirty {
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

func (m *RootModel) liveRows(width int) []string {
	if !m.conversation.streaming {
		return nil
	}
	var rows []string
	line := m.conversation.spinner.View()
	phrase := "Generating"
	if m.conversation.anyToolRun {
		phrase = "Working"
	}
	if m.conversation.thinking && m.conversation.activeTool == "" {
		if m.conversation.liveThinkingSnippet != "" {
			line += " " + palette.SpinnerBaseOpen + m.conversation.liveThinkingSnippet + "\x1b[m"
		} else {
			line += " " + tuitools.SpinnerText("Thinking", m.conversation.spinFrame)
		}
	} else if m.conversation.activeTool != "" {
		line += " " + m.conversation.activeTool
	} else {
		line += " " + tuitools.SpinnerText(phrase, m.conversation.spinFrame)
	}
	if width > 0 {
		line = tuilib.Truncate(line, width, "...")
	}
	rows = append(rows, line)
	if m.viewport.activeTip != "" {
		tipLine := tuitools.FormatTip(m.viewport.activeTip)
		if tipLine != "" {
			if width > 0 {
				tipLine = tuilib.Truncate(tipLine, width, "...")
			}
			rows = append(rows, tipLine)
		}
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

func (m *RootModel) viewportHeight() int {
	if m.viewport.height <= 0 {
		return 0
	}
	var extra int
	liveLen := len(m.liveRows(m.viewport.width))
	if m.viewport.extraRowsValid && m.viewport.cachedExtraW == m.viewport.width && m.viewport.cachedExtraH == m.viewport.height && m.viewport.cachedExtraLiveRows == liveLen {
		extra = m.viewport.cachedExtraRows
	} else {
		body, _ := m.viewBody(nil)
		extra = tuilib.ViewRows(body)
		if m.help.Panel.Active() {
			extra -= tuilib.ViewRows(m.help.Panel.Rows())
		}
		m.viewport.cachedExtraRows = extra
		m.viewport.cachedExtraLiveRows = liveLen
		m.viewport.cachedExtraW = m.viewport.width
		m.viewport.cachedExtraH = m.viewport.height
		m.viewport.extraRowsValid = true
	}
	budget := m.viewport.height
	if m.help.Panel.Active() {
		budget += viewOverflowRows
	}
	avail := budget - extra
	if avail < 1 {
		avail = 1
	}
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

// ---- renderer.go ----

// renderer.go - cycle 256: the renderer's optimization/bypass
// behaviour, exposed as a public, testable seam.
//
// AGY 1.1.19's render path emits only dirty rows on each frame
// and falls back to the full viewport only when the consumer
// explicitly disables the optimization. chasm.land/bubbletea/v2
// owns the actual diff implementation inside cursed_renderer.go;
// we don't fork it. What cycle 256 ships is:
//
//  1. A boolean escapeSequenceOptimizationsEnabled() (in
//     escape_optimizations.go) that reads
//     AGY_CLI_DISABLE_ESCAPE_SEQUENCE_OPTIMIZATIONS.
//
//  2. applyRenderer(prev, current, optimizationsEnabled) -
//     public test surface for "given two frames and a gate,
//     what does the renderer emit?". The contract:
//
//     optimizationsEnabled AND prev == current : ""    (no-op)
//     otherwise                                : current (full redraw)
//
//     When the env var bypasses optimizations,
//     optimizationsEnabled is false and applyRenderer always
//     returns `current`. The cycle-256 deliverable's
//     "What the fix does" section lists every call site
//     currently consulting this seam; the View() in tui_run.go
//     does not yet route through applyRenderer because that
//     would require either wrapping or replacing the
//     bubbletea/v2 renderer (out of scope for cycle 256's
//     env-var gap; documented in 硬约束).
//
//  3. shouldConsultRenderer(mode) - the print-mode bypass pin.
//     Print mode (-p / --print) writes plain text to stdout and
//     never reaches bubbletea's View() pipeline, so the
//     AGY_CLI_DISABLE_ESCAPE_SEQUENCE_OPTIMIZATIONS env var is
//     irrelevant there. The cycle-256 spec says
//     "print mode (`-p`) doesn't trigger the renderer code path
//     (it's a TUI concern); pin in test." shouldConsultRenderer
//     is the public pin.

// applyRenderer returns the bytes the renderer should emit given
// the previous frame, the current frame, and whether the
// dirty-rectangle / diff optimizations are enabled.
//
// When optimizations are on AND the previous and current frame
// are identical (byte-for-byte), applyRenderer returns the empty
// string to signal "no draw needed". The match is exact and
// untrimmed: the renderer downstream always sees the literal
// viewport bytes, and a single trailing newline difference is
// still a redraw. The cycle 256 deliverable documents this
// decision against any future cycle that wants a trimmed
// comparison (trimming would let optimisations elide frame
// transitions bubbletea treats as different for layout reasons
// but humans view as identical - kept simple here).
//
// When optimizations are off, applyRenderer returns the full
// current view regardless of whether it differs from prev. This
// is the AGY 1.1.19 "bypass the renderer's ... optimizations"
// behaviour: full redraw every frame.
//
// When optimizations are on AND the views differ, applyRenderer
// returns the full current view (the diff itself would be
// returned in a future cycle - cycle 256 exercises the gate
// only, not the diff algorithm).
func applyRenderer(prev, current string, optimizationsEnabled bool) string {
	if optimizationsEnabled && prev == current {
		return ""
	}
	return current
}

// mode constants for shouldConsultRenderer. Held as named
// strings rather than an enum so the print-mode dispatcher in
// cmd/wagyu/main.go can pass through values verbatim without a
// conversion step.
const (
	modePrint = "print"
	modeDashP = "-p"
)

// shouldConsultRenderer reports whether the renderer / dirty-rect
// optimization seam applies for a given run mode. Returns false
// for print mode (and its `-p` shorthand) and true otherwise.
//
// match is case-insensitive and whitespace-trimmed so callers do
// not have to normalize mode strings before calling; the test
// fixture in renderer_test.go pins this contract for every
// spelling the print dispatcher accepts (cmd/wagyu/main.go
// normalizes "-p" and "--print" to a single print path long
// before this gate fires in production, but the gate still has
// to be robust to the literals a future cycle might emit).
//
// The default (unknown mode) returns true - "be conservative
// and apply the gate" matches the AGY 1.1.19 reading of the
// spec.
func shouldConsultRenderer(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case modePrint, modeDashP:
		return false
	}
	return true
}

const envDisableEscapeSequenceOptimizations = tuilib.EnvDisableEscapeSequenceOptimizations

// The palette's colours follow the selected colour scheme, so these
// aliases are refreshed whenever the scheme changes.
var (
	cBlueOpen            = palette.BlueOpen
	cBlueBoldOpen        = palette.BlueBoldOpen
	cUserFgOpen          = palette.UserFgOpen
	cDimGrayOpen         = palette.DimGrayOpen
	cWarningOpen         = palette.WarningOpen
	cWarningBlueOpen     = palette.WarningBlueOpen
	cWarningBodyOpen     = palette.WarningBodyOpen
	cSeparatorOpen       = palette.SeparatorOpen
	cSubtleOpen          = palette.SubtleOpen
	cToolBulletErrOpen   = palette.ToolBulletErrOpen
	cDiffAddTintDark     = palette.DiffAddTintDark
	cDiffRemoveTintDark  = palette.DiffRemoveTintDark
	cDiffAddTintLight    = palette.DiffAddTintLight
	cDiffRemoveTintLight = palette.DiffRemoveTintLight
	cDiffAddTintOpen     = palette.DiffAddTintOpen
	cDiffRemoveTintOpen  = palette.DiffRemoveTintOpen
	cInlineCodeOpen      = palette.InlineCodeOpen
	cInlineCodeClose     = palette.InlineCodeClose
	cBoldOpen            = palette.BoldOpen
	cBoldClose           = palette.BoldClose
	cBlueClose           = palette.BlueClose
	cDimGrayClose        = palette.DimGrayClose
	cWarningClose        = palette.WarningClose
	cWarningBlueClose    = palette.WarningBlueClose
	cWarningBodyClose    = palette.WarningBodyClose
	cSeparatorClose      = palette.SeparatorClose
)

func init() {
	palette.OnSchemeChange(refreshPaletteAliases)
}

// refreshPaletteAliases re-reads the palette's colours after the
// selected colour scheme changed.
func refreshPaletteAliases() {
	cBlueOpen = palette.BlueOpen
	cBlueBoldOpen = palette.BlueBoldOpen
	cUserFgOpen = palette.UserFgOpen
	cDimGrayOpen = palette.DimGrayOpen
	cWarningOpen = palette.WarningOpen
	cWarningBlueOpen = palette.WarningBlueOpen
	cWarningBodyOpen = palette.WarningBodyOpen
	cSeparatorOpen = palette.SeparatorOpen
	cSubtleOpen = palette.SubtleOpen
	panelGray238 = palette.SubtleOpen
	cToolBulletErrOpen = palette.ToolBulletErrOpen
	cDiffAddTintDark = palette.DiffAddTintDark
	cDiffRemoveTintDark = palette.DiffRemoveTintDark
	cDiffAddTintLight = palette.DiffAddTintLight
	cDiffRemoveTintLight = palette.DiffRemoveTintLight
	cDiffAddTintOpen = palette.DiffAddTintOpen
	cDiffRemoveTintOpen = palette.DiffRemoveTintOpen
	cInlineCodeOpen = palette.InlineCodeOpen
	cInlineCodeClose = palette.InlineCodeClose
	cBoldOpen = palette.BoldOpen
	cBoldClose = palette.BoldClose
	cBlueClose = palette.BlueClose
	cDimGrayClose = palette.DimGrayClose
	cWarningClose = palette.WarningClose
	cWarningBlueClose = palette.WarningBlueClose
	cWarningBodyClose = palette.WarningBodyClose
	cSeparatorClose = palette.SeparatorClose

	// Cycle 381: the /help overlay's own aliases left the host with
	// the panel; the panel reads tuilib's colours directly, and tuilib
	// refreshes them when the scheme changes.

	// Cycle 381: the slash-command popup's own colours left the host
	// with the panel; the panel reads the palette's colours directly
	// and refreshes them when the scheme changes.

	// Cycle 381: the bash block's body colours left the host with the
	// block; the block reads the palette's colours directly, and the
	// palette refreshes them when the scheme changes.
}

// ---- tui_attach.go ----

// Package cliapp — tui_attach.go: the `pigasus tui` subcommand.
//
// The interactive session is the client half of the client/server split.
// Without --server it is the ordinary local session; with it, the turn
// runs on the server — on that machine's workspace, tools and
// credentials — and the session renders the server's frames here.
//
// The client owns the conversation: the transcript travels with every
// prompt, so the server replays what the client sends and never has to
// remember a conversation of its own.

// attachTimeout is how long the client waits for the handshake.
const attachTimeout = 20 * time.Second

// normalizeServerAddr maps the address the user typed onto a websocket
// URL.
//
// A bare "host:port" is the common way to name a server, and the
// websocket scheme is the only one the protocol speaks.
func normalizeServerAddr(addr string) string {
	a := strings.TrimSpace(addr)
	if a == "" {
		return ""
	}
	if !strings.Contains(a, "://") {
		return "ws://" + a
	}
	return a
}

// runTUIHandler is the `pigasus tui` handler.
//
// The session's own flags are the root command's flags, so they are
// parsed here — the subcommand owns only --server and --token.
func runTUIHandler(j *cli.JetskiCLI, args cli.TUIArgs) error {
	fs := cli.NewFlagSet("tui", append(cli.StandardFlags(), cli.ExtraFlags()...))
	if j != nil && j.ErrOut != nil {
		fs.SetOutput(j.ErrOut)
	} else {
		fs.SetOutput(os.Stderr)
	}
	if err := fs.Parse(args.Args); err != nil {
		return err
	}
	out := io.Writer(os.Stdout)
	if j != nil && j.Out != nil {
		out = j.Out
	}
	if strings.TrimSpace(args.ACPCmd) != "" {
		return runAttachedTUI_ACPCmd(out, fs, args)
	}
	if strings.TrimSpace(args.ACPManifest) != "" {
		return runAttachedTUI_ACPManifest(out, fs, args)
	}
	backend := strings.ToLower(strings.TrimSpace(args.Backend))
	if driver, ok := agentturn.Lookup(backend); ok {
		return runAttachedTUI_Driver(out, fs, driver, args)
	}
	if backend == "pigasus-acp" || backend == "pig-acp" {
		return runAttachedTUI_ACPAgent(out, fs, "pigasus-acp", args)
	}
	if backend == "codex-acp" {
		return runAttachedTUI_ACPAgent(out, fs, "codex-acp", args)
	}
	if backend == "claude-acp" {
		return runAttachedTUI_ACPAgent(out, fs, "claude-acp", args)
	}
	if backend == "agy-acp" || backend == "agy-acp-server" || backend == "antigravity-acp" {
		return runAttachedTUI_ACPAgent(out, fs, "agy-acp", args)
	}
	if backend != "" && backend != "local" {
		return runAttachedTUI_ACPAgent(out, fs, backend, args)
	}
	if strings.TrimSpace(args.Server) == "" {
		return runLiveTUI(out, fs)
	}
	return runAttachedTUI(out, fs, args)
}

// resolveAttachProtocol determines whether the client should speak AHP, ACP, or AgentLink.
func resolveAttachProtocol(addr, proto string) string {
	p := strings.ToLower(strings.TrimSpace(proto))
	if p == "ahp" {
		return "ahp"
	}

	cleanAddr := strings.TrimRight(strings.TrimSpace(addr), "/")
	if u, err := url.Parse(cleanAddr); err == nil && u.Path != "" {
		path := strings.TrimRight(u.Path, "/")
		if strings.HasSuffix(path, "/ahp") {
			return "ahp"
		}
		if strings.HasSuffix(path, "/acp") {
			return "acp"
		}
	}
	if strings.HasSuffix(cleanAddr, "/ahp") {
		return "ahp"
	}
	if strings.HasSuffix(cleanAddr, "/acp") {
		return "acp"
	}

	if p == "agentlink" {
		return "agentlink"
	}
	if p == "" || p == "acp" {
		return "acp"
	}
	return p
}

// runAttachedTUI runs the interactive session against a remote server.
func runAttachedTUI(w io.Writer, fs *flag.FlagSet, args cli.TUIArgs) error {
	addr := normalizeServerAddr(args.Server)
	if addr == "" {
		return fmt.Errorf("tui: no server address")
	}

	switch resolveAttachProtocol(addr, args.Protocol) {
	case "ahp":
		return runAttachedTUI_AHP(w, fs, addr, args)
	case "acp":
		return runAttachedTUI_ACP(w, fs, addr, args)
	}

	ctx, cancel := context.WithTimeout(context.Background(), attachTimeout)
	defer cancel()

	client, err := agentlink.Dial(ctx, addr, agentlink.DialOptions{Token: strings.TrimSpace(args.Token)})
	if err != nil {
		return fmt.Errorf("tui: cannot reach the server at %s: %w", addr, err)
	}
	defer client.Close()

	// The handshake names the server's workspace and the model it
	// resolved, so the session shows the server's own.
	if err := client.Send(ctx, agentlink.Frame{Kind: agentlink.KindHello}); err != nil {
		return fmt.Errorf("tui: cannot greet the server: %w", err)
	}
	f, err := client.Next(ctx)
	if err != nil {
		return fmt.Errorf("tui: the server did not answer the greeting: %w", err)
	}
	if f.Kind != agentlink.KindHello {
		return fmt.Errorf("tui: the server answered the greeting with %q", f.Kind)
	}

	modelName := strings.TrimSpace(f.Model)
	if modelName == "" {
		modelName = defaultModel
	}
	altScreen := resolveAltScreen(fs)
	m, watchCancel := initTUIRootModel(fs, nil, modelName, "", altScreen)
	defer watchCancel()
	if m.viewport.pluginWatcher != nil {
		defer pluginWatcherPtr.Store(nil)
		defer m.viewport.pluginWatcher.Close()
	}
	// The turn runs on the server: the local engine would run it here.
	m.turn.engine = agentturn.NewRemote(client)
	m.remote.SetAttached(true)
	setupTUIToolsAndPolicy(m, fs)
	canonicalCwd := m.session.cwd
	setupTUIConversation(m, fs, canonicalCwd)

	return runTUIProgram(m, fs, altScreen, canonicalCwd)
}

// runAttachedTUI_AHP runs the interactive session against a remote AHP server.
func runAttachedTUI_AHP(w io.Writer, fs *flag.FlagSet, addr string, args cli.TUIArgs) error {
	ctx, cancel := context.WithTimeout(context.Background(), attachTimeout)
	defer cancel()

	cwd, _ := os.Getwd()
	cliModel := cliflags.StringValue(fs, "model")
	remote, err := agentturn.DialAHPRemote(ctx, addr, strings.TrimSpace(args.Token), cwd, agentturn.AHPRemoteOptions{
		SessionURI: strings.TrimSpace(args.Session),
		Model:      cliModel,
	})
	if err != nil {
		return fmt.Errorf("tui (ahp): cannot reach the server at %s: %w", addr, err)
	}
	defer remote.Close()
	modelName := cliModel
	if modelName == "" {
		if dm, ok := any(remote).(agentturn.DefaultModeler); ok {
			modelName = dm.DefaultModel()
		}
	}
	if modelName == "" {
		modelName = defaultModel
	}
	if canonical := modelcatalog.CanonicalModelLabel(modelName); canonical != "" {
		modelName = canonical
	}

	altScreen := resolveAltScreen(fs)
	m, watchCancel := initTUIRootModel(fs, nil, modelName, "", altScreen)
	defer watchCancel()
	if m.viewport.pluginWatcher != nil {
		defer pluginWatcherPtr.Store(nil)
		defer m.viewport.pluginWatcher.Close()
	}

	// The turn runs on the AHP server.
	m.turn.engine = remote
	m.remote.SetAttached(true)
	setupTUIToolsAndPolicy(m, fs)
	canonicalCwd := m.session.cwd
	setupTUIConversation(m, fs, canonicalCwd)

	return runTUIProgram(m, fs, altScreen, canonicalCwd)
}

// runAttachedTUI_ACP runs the interactive session against a remote ACP server.
func runAttachedTUI_ACP(w io.Writer, fs *flag.FlagSet, addr string, args cli.TUIArgs) error {
	ctx, cancel := context.WithTimeout(context.Background(), attachTimeout)
	defer cancel()

	cwd, _ := os.Getwd()
	remote, err := agentturn.DialACPRemote(ctx, addr, strings.TrimSpace(args.Token), cwd, args.ACPVersion)
	if err != nil {
		return fmt.Errorf("tui (acp): cannot reach the server at %s: %w", addr, err)
	}
	defer remote.Close()

	modelName := defaultModel
	altScreen := resolveAltScreen(fs)
	m, watchCancel := initTUIRootModel(fs, nil, modelName, "", altScreen)
	defer watchCancel()
	if m.viewport.pluginWatcher != nil {
		defer pluginWatcherPtr.Store(nil)
		defer m.viewport.pluginWatcher.Close()
	}

	// The turn runs on the ACP server.
	m.turn.engine = remote
	m.remote.SetAttached(true)
	setupTUIToolsAndPolicy(m, fs)
	canonicalCwd := m.session.cwd
	setupTUIConversation(m, fs, canonicalCwd)

	return runTUIProgram(m, fs, altScreen, canonicalCwd)
}

// runAttachedTUI_Driver runs the interactive session against any registered backend driver.
func runAttachedTUI_Driver(w io.Writer, fs *flag.FlagSet, driver agentturn.Driver, args cli.TUIArgs) error {
	ctx, cancel := context.WithTimeout(context.Background(), attachTimeout)
	defer cancel()

	cwd, _ := os.Getwd()
	cliModel := cliflags.StringValue(fs, "model")
	effortFlag := cliflags.StringValue(fs, "effort")

	opts := agentturn.SpawnOptions{
		CWD:    cwd,
		Model:  cliModel,
		Effort: effortFlag,
		Args:   args.Args,
	}

	remote, err := driver.Spawn(ctx, opts)
	if err != nil {
		return fmt.Errorf("tui (%s): failed to initialize engine: %w", driver.Name, err)
	}
	defer remote.Close()

	modelName := cliModel
	if modelName == "" {
		if dm, ok := remote.(agentturn.DefaultModeler); ok {
			modelName = dm.DefaultModel()
		}
	}
	if modelName == "" {
		modelName = driver.DefaultModel
	}
	if modelName == "" {
		modelName = "gemini-3.8-flash-high"
	}
	if canonical := modelcatalog.CanonicalModelLabel(modelName); canonical != "" {
		modelName = canonical
	}

	altScreen := resolveAltScreen(fs)
	m, watchCancel := initTUIRootModel(fs, nil, modelName, "", altScreen)
	defer watchCancel()
	if m.viewport.pluginWatcher != nil {
		defer pluginWatcherPtr.Store(nil)
		defer m.viewport.pluginWatcher.Close()
	}

	m.turn.engine = remote
	m.remote.SetAttached(true)
	setupTUIToolsAndPolicy(m, fs)
	canonicalCwd := m.session.cwd
	setupTUIConversation(m, fs, canonicalCwd)

	return runTUIProgram(m, fs, altScreen, canonicalCwd)
}

// runAttachedTUI_ACPAgent runs the interactive session against an ACP agent (built-in or manifest).
func runAttachedTUI_ACPAgent(w io.Writer, fs *flag.FlagSet, agentID string, args cli.TUIArgs) error {
	ctx, cancel := context.WithTimeout(context.Background(), attachTimeout)
	defer cancel()

	cwd, _ := os.Getwd()
	remote, err := agentturn.SpawnACPAgent(ctx, agentID, nil, cwd, args.ACPVersion)
	if err != nil {
		return fmt.Errorf("tui (%s): failed to spawn ACP agent: %w", agentID, err)
	}
	defer remote.Close()

	modelName := agentID
	altScreen := resolveAltScreen(fs)
	m, watchCancel := initTUIRootModel(fs, nil, modelName, "", altScreen)
	defer watchCancel()
	if m.viewport.pluginWatcher != nil {
		defer pluginWatcherPtr.Store(nil)
		defer m.viewport.pluginWatcher.Close()
	}

	m.turn.engine = remote
	m.remote.SetAttached(true)
	setupTUIToolsAndPolicy(m, fs)
	canonicalCwd := m.session.cwd
	setupTUIConversation(m, fs, canonicalCwd)

	return runTUIProgram(m, fs, altScreen, canonicalCwd)
}

// runAttachedTUI_ACPCmd runs the interactive session against a custom ACP command over stdio.
func runAttachedTUI_ACPCmd(w io.Writer, fs *flag.FlagSet, args cli.TUIArgs) error {
	ctx, cancel := context.WithTimeout(context.Background(), attachTimeout)
	defer cancel()

	cmdParts := strings.Fields(strings.TrimSpace(args.ACPCmd))
	if len(cmdParts) == 0 {
		return fmt.Errorf("tui: empty --acp-cmd")
	}

	cwd, _ := os.Getwd()
	remote, err := agentturn.SpawnACPRemote(ctx, cmdParts[0], cmdParts[1:], cwd, args.ACPVersion)
	if err != nil {
		return fmt.Errorf("tui (acp): failed to spawn ACP command %q: %w", args.ACPCmd, err)
	}
	defer remote.Close()

	modelName := filepath.Base(cmdParts[0])
	altScreen := resolveAltScreen(fs)
	m, watchCancel := initTUIRootModel(fs, nil, modelName, "", altScreen)
	defer watchCancel()
	if m.viewport.pluginWatcher != nil {
		defer pluginWatcherPtr.Store(nil)
		defer m.viewport.pluginWatcher.Close()
	}

	m.turn.engine = remote
	m.remote.SetAttached(true)
	setupTUIToolsAndPolicy(m, fs)
	canonicalCwd := m.session.cwd
	setupTUIConversation(m, fs, canonicalCwd)

	return runTUIProgram(m, fs, altScreen, canonicalCwd)
}

// runAttachedTUI_ACPManifest runs the interactive session against an ACP agent described by an agent.json manifest.
func runAttachedTUI_ACPManifest(w io.Writer, fs *flag.FlagSet, args cli.TUIArgs) error {
	ctx, cancel := context.WithTimeout(context.Background(), attachTimeout)
	defer cancel()

	cwd, _ := os.Getwd()
	remote, err := agentturn.SpawnACPAgent(ctx, strings.TrimSpace(args.ACPManifest), nil, cwd, args.ACPVersion)
	if err != nil {
		return fmt.Errorf("tui: failed to spawn ACP manifest %q: %w", args.ACPManifest, err)
	}
	defer remote.Close()

	modelName := "acp-agent"
	altScreen := resolveAltScreen(fs)
	m, watchCancel := initTUIRootModel(fs, nil, modelName, "", altScreen)
	defer watchCancel()
	if m.viewport.pluginWatcher != nil {
		defer pluginWatcherPtr.Store(nil)
		defer m.viewport.pluginWatcher.Close()
	}

	m.turn.engine = remote
	m.remote.SetAttached(true)
	setupTUIToolsAndPolicy(m, fs)
	canonicalCwd := m.session.cwd
	setupTUIConversation(m, fs, canonicalCwd)

	return runTUIProgram(m, fs, altScreen, canonicalCwd)
}

// ---- tui_bash_mode.go ----

// tui_bash_mode.go — the reference's bash mode ("! mode"): the process.
//
// 【一手】 agy 1.4.4, byte-level pty captures 2026-10-22
// (docs/cycles/bash-mode/). What the reference does:
//
//	> !                     ← typing `!` on an empty composer arms the mode
//	 activated bash mode · esc to cancel
//
// The command never reaches the model. The block's own rows — the head
// row, the body, the failed-command shape, the hollow `○` of an
// interrupted head — belong to internal/features/tools/bashmode, which
// renders them.
//
// Measured branches:
//
//   - `!` arms the mode only when the buffer is already empty. ` !echo hi`
//     (leading space) is an ordinary prompt and goes to the model.
//   - The `!` is consumed by the mode; the buffer holds the command alone.
//     The composer draws a bold blue `!` + space as the prompt glyph.
//   - Enter with an empty command just leaves the mode (nothing is printed).
//   - Esc leaves the mode and clears the buffer.
//   - The command runs through the platform's default shell in the
//     workspace (zsh -c on darwin — the same DefaultShellArgv the
//     run_command tool uses).
//   - stdout and stderr are merged; trailing newlines are dropped.
//   - A failed command with no output prints the Go error text
//     ("exit status 3"); a failed command that *did* print something
//     prints only that output — there is no status line.
//   - The command behaves like a local turn: the spinner reads
//     "Generating...", the status row reads "esc to cancel", and Esc
//     cancels it — leaving the head row and no body.
//   - A command submitted while a *model* turn streams is queued and shown
//     as "▸ <cmd>" above the input (no "(alt+m …)" suffix, unlike queued
//     prompts); it runs when the turn ends.
//   - A command submitted while another *shell* command runs preempts it:
//     the old process tree is stopped, its partial output is kept as the
//     block's body, and the new command starts at once (【一手】 agy 1.4.5,
//     `!top` then `!pwd`).
//   - Esc and Ctrl+C cancel a running command. The block keeps only the head
//     row (no partial body); Ctrl+C additionally arms the two-step exit
//     confirmation on the same press.

// bashDoneMsg is the host's message: it carries the host's generation
// counter (the stale-completion guard) and the exit status the implicit
// trajectory records, so it stays with the host's pump. The block the
// message fills belongs to internal/features/tools/bashmode.
type bashDoneMsg struct {
	gen       int
	cmd       string
	output    string
	failed    bool
	cancelled bool
	// code is the command's exit status. It is only meaningful when
	// hasCode is set: a canceled command has no exit status, and the
	// reference's record leaves the field unset for one.
	code    int
	hasCode bool
}

// The bash block is the transcript's bash block: the transcript asks it
// for its rows through the interface, and the block itself — its state
// and its rows — belongs to internal/features/tools/bashmode.
var _ transcript.BashBlock = (*bashmode.Block)(nil)

// startBashCommand runs the command as a local turn: it appends the
// in-flight head row, flips the TUI into the "streaming" state (spinner +
// "esc to cancel"), and returns the tea.Cmd that performs the exec and the
// spinner tick. The caller must have already cleared the composer and the
// bash-mode flag.
func (m *RootModel) startBashCommand(cmd string) tea.Cmd {
	m.session.store.BashGen++
	gen := m.session.store.BashGen
	m.session.store.BashCmd = cmd
	m.conversation.response = transcript.BlankBefore(m.conversation.response)
	m.session.store.BashRowIdx = len(m.conversation.response)
	m.conversation.response = append(m.conversation.response, bashmode.HeadRow(cmd, false, true))
	m.conversation.streaming = true
	m.conversation.thinking = false
	m.conversation.activeTool = ""
	m.conversation.anyToolRun = false
	m.conversation.spinFrame = 0
	m.clearLastTurnOutcome()
	out := &tools.SafeBuffer{}
	m.session.store.BashRun = out
	ctx, cancel := context.WithCancel(context.Background())
	m.session.store.SetBashCancel(cancel)
	dir := m.session.cwd
	run := func() tea.Msg {
		return execBashCommand(ctx, dir, cmd, out, gen)
	}
	tick := func() tea.Msg { return m.conversation.spinner.Tick() }
	return tea.Batch(run, tick)
}

// execBashCommand runs one command through the platform shell with merged
// stdout/stderr. The reference prints "exit status N" only when a failed
// command produced no output, which is exactly Go's *exec.ExitError text.
//
// Cancel kills the whole process group, not just the shell: `sh -c 'sleep 30'`
// leaves an orphan holding the stdout pipe, and killing only the shell makes
// exec.Cmd.Wait block until the grandchild exits on its own — the turn (and the
// spinner) would outlive the cancel by however long the orphan runs.
func execBashCommand(ctx context.Context, dir, command string, out *tools.SafeBuffer, gen int) bashDoneMsg {
	argv := shell.DefaultShellArgv(command)
	c := exec.CommandContext(ctx, argv[0], argv[1:]...)
	c.Dir = dir
	c.Stdout = out
	c.Stderr = out
	procgroup.Set(c)
	c.Cancel = func() error { return procgroup.Kill(c) }
	c.WaitDelay = 2 * time.Second
	err := c.Run()
	if ctx.Err() != nil {
		// Esc / Ctrl+C: the reference finalises the block with no body and
		// never prints an "Interrupted" row. The preemption path snapshots the
		// buffer before cancelling, so dropping it here is correct.
		return bashDoneMsg{gen: gen, cmd: command, cancelled: true}
	}
	text := bashmode.TrimOutput(out.String())
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		code, hasCode := bashExitCode(err)
		return bashDoneMsg{gen: gen, cmd: command, output: text, failed: true, code: code, hasCode: hasCode}
	}
	return bashDoneMsg{gen: gen, cmd: command, output: text, hasCode: true}
}

// bashExitCode returns the command's exit status. The second result is false
// when the command has no exit status at all — a command that never ran, or
// one that was canceled.
func bashExitCode(err error) (int, bool) {
	if err == nil {
		return 0, true
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), true
	}
	return 0, false
}

// finishBashCommand applies a bashDoneMsg: repaint the head row in place,
// append the body (if any) plus the block's trailing blank row. A cancelled
// command keeps only the head row — the reference never shows partial output
// for Esc / Ctrl+C.
func (m *RootModel) finishBashCommand(msg bashDoneMsg) {
	m.session.store.ClearInFlightBash()
	m.conversation.streaming = false
	b := &bashmode.Block{Cmd: msg.cmd, Failed: msg.failed}
	if !msg.cancelled {
		b.Body = msg.output
	}
	m.placeBashBlock(b)
	m.session.store.BashRowIdx = -1
	// The reference records the command in its implicit trajectory: this is
	// the only place a `!` command is written down.
	m.recordImplicitStep(msg.cmd, msg.output, msg.code, msg.hasCode, msg.cancelled)
}

// placeBashBlock paints a finished block. When an in-flight head row exists it
// is rebuilt in place and the rest appended after it; otherwise the block is
// appended to the transcript. Every block is recorded in m.conversation.blocks so ctrl+o
// can re-render it from the full body.
func (m *RootModel) placeBashBlock(b *bashmode.Block) {
	rows := b.Lines(m.isExpanded(), m.viewport.height)
	if m.session.store.BashRowIdx >= 0 && m.session.store.BashRowIdx < len(m.conversation.response) {
		m.conversation.response[m.session.store.BashRowIdx] = rows[0]
		m.conversation.response = append(m.conversation.response, rows[1:]...)
		m.conversation.blocks = append(m.conversation.blocks, transcript.Block{Start: m.session.store.BashRowIdx, Lines: len(rows), Bash: b})
		return
	}
	m.conversation.response = transcript.BlankBefore(m.conversation.response)
	start := len(m.conversation.response)
	m.conversation.response = append(m.conversation.response, rows...)
	m.conversation.blocks = append(m.conversation.blocks, transcript.Block{Start: start, Lines: len(rows), Bash: b})
}

// preemptBashCommand interrupts the running command the way the reference does
// when a *new* shell command is submitted while one is still running: the old
// process tree is stopped, whatever it had already written becomes its body,
// and the new command starts immediately instead of queueing.
//
// 【一手】 agy 1.4.5, `!sh -c 'echo first; sleep 30'` then `!echo queued2`: the
// first block keeps its hollow amber `○` head (never repainted) and gains
// `  ⎿  first`, then the second block runs to completion — all before any Esc.
func (m *RootModel) preemptBashCommand(next string) tea.Cmd {
	partial := ""
	if m.session.store.BashRun != nil {
		partial = bashmode.TrimOutput(m.session.store.BashRun.String())
	}
	cmd := m.session.store.BashCmd
	m.session.store.CancelBash()
	m.session.store.BashRun = nil
	m.placeBashBlock(&bashmode.Block{Cmd: cmd, Body: partial, Running: true})
	m.session.store.BashRowIdx = -1
	// The preempted command is recorded as canceled: the reference's
	// tracker finishes it when the process tree is stopped, and the step it
	// writes carries CORTEX_STEP_STATUS_CANCELED.
	m.recordImplicitStep(cmd, partial, 0, false, true)
	return m.startBashCommand(next)
}

// cancelBashCommand runs the Esc-cancel path for an in-flight command.
func (m *RootModel) cancelBashCommand() {
	m.session.store.CancelBash()
}

// applyHistoryEntry loads a recalled entry into the composer. Entries are
// stored with the leading "!" for shell commands (submitPrompt pushes
// "!"+cmd), so a recall re-arms the mode and strips the marker — the
// reference recalls "!echo hist" as "! echo hist" in bash mode.
// If the recalled entry meets the folding threshold (>= 16 lines or > 1000 runes),
// it is dynamically folded into [Pasted text #1 ...] matching reference agy.
func (m *RootModel) applyHistoryEntry(entry string) {
	m.session.store.BashMode = false
	if strings.HasPrefix(entry, "!") {
		m.session.store.BashMode = true
		entry = strings.TrimPrefix(entry, "!")
		entry = strings.TrimPrefix(entry, " ")
	}
	m.prompt.pastedBlocks = nil
	m.prompt.nextPasteID = 0

	lines := strings.Count(entry, "\n") + 1
	charCount := len([]rune(entry))
	if lines >= 16 || charCount > 1000 {
		m.prompt.pastedBlocks = make(map[string]string)
		m.prompt.nextPasteID = 1
		var placeholder string
		if lines >= 16 {
			placeholder = fmt.Sprintf("[Pasted text #1 +%d lines]", lines)
		} else {
			placeholder = fmt.Sprintf("[Pasted text #1 %d chars]", charCount)
		}
		m.prompt.pastedBlocks[placeholder] = entry
		entry = placeholder
	}

	m.prompt.input.SetValue(entry)
}

// ---- tui_paste_media.go ----

// Package cliapp — tui_paste_media.go: the paste key's image half.
//
// agy 1.4.9 (upstream release notes):
//
//	"Added image pasting through your terminal's own paste shortcut, such
//	 as Cmd+V or Ctrl+Shift+V with only an image copied, and by dragging
//	 image files into the prompt, which attaches them instead of
//	 inserting their paths; image paste now also works over SSH with X11
//	 or Wayland forwarding, and the CLI's paste key now says
//	 `No image or video found in the clipboard` when there is nothing to
//	 attach and names the clipboard tool to install, such as
//	 `wl-clipboard` on Wayland, when one is missing."
//
// What this file does:
//
//   - the paste key (ctrl+v) and the terminal's own bracketed paste both
//     land in handlePasteContent, which decides what the paste means:
//     an image on the clipboard becomes an attachment, a path to an image
//     file becomes an attachment, everything else stays text;
//   - the attachment lives in pendingMedia until the next turn is sent,
//     and then rides along with the user message as an image part (the
//     transports already carry images: internal/backend/client.go's
//     Message.Images);
//   - when the clipboard holds nothing to attach, the composer says the
//     reference's own sentence — "No image or video found in the
//     clipboard" — or the missing-tool sentence the platform reader
//     produced.
//
// The read half (the platform clipboard) lives in internal/features/tools/clipboard.
// The indicator above the composer is the ui tier's own attachment rows
// (internal/ui/rows.AttachmentRows); this file owns the attachment, not
// the shape of the row that shows it.

// clipboardReadMedia is the seam the tests replace: it reads the
// platform clipboard's image payload.
var clipboardReadMedia = clipboard.ReadMedia

// pasteMediaMsg carries the outcome of a clipboard media probe.
type pasteMediaMsg struct {
	media clipboard.Media
	err   error
}

// handlePasteContent is the single paste entry point.
//
// text is what the paste delivered: the clipboard's text for the ctrl+v
// key, the bracketed paste's payload for the terminal's own shortcut.
//
// A path to an image file becomes an attachment — that is what "dragging
// image files into the prompt" produces, because a terminal cannot hand
// a file to a program and inserts its path instead. Ordinary text is
// pasted as text. Only a paste that delivered nothing asks the platform
// for an image, which is the "Cmd+V with only an image copied" case.
func (m *RootModel) handlePasteContent(text string) tea.Cmd {
	if m.hasActiveModal() {
		// A panel owns the keyboard (the /rename field pastes its own
		// text); the composer's paste does not reach it.
		return m.insertPastedText(text)
	}
	if paths := clipboard.PastedMediaPaths(text, runtime.GOOS); len(paths) > 0 {
		for _, p := range paths {
			if media, ok := clipboard.MediaFromFile(p); ok {
				m.attachMedia(media)
			}
		}
		return nil
	}
	if media, ok := clipboard.MediaFromPath(text); ok {
		m.attachMedia(media)
		return nil
	}
	if strings.TrimSpace(text) != "" {
		return m.insertPastedText(text)
	}
	return m.probeClipboardMedia()
}

// probeClipboardMedia asks the platform clipboard for an image. The read
// shells out (osascript, wl-paste, xclip), so it runs off the update
// goroutine.
func (m *RootModel) probeClipboardMedia() tea.Cmd {
	return func() tea.Msg {
		media, err := clipboardReadMedia(clipboard.DefaultOptions())
		return pasteMediaMsg{media: media, err: err}
	}
}

// updatePasteMedia handles the probe's answer: an image is attached, and
// a clipboard with nothing to attach says so.
func (m *RootModel) updatePasteMedia(msg pasteMediaMsg) (tea.Model, tea.Cmd) {
	if msg.err == nil && len(msg.media.Data) > 0 {
		m.attachMedia(msg.media)
		return m, nil
	}
	m.viewport.inputNotice = clipboardMediaNotice(msg.err)
	return m, nil
}

// clipboardMediaNotice renders the sentence the composer shows when the
// clipboard holds no image.
//
// The reference's own wording is "No image or video found in the
// clipboard"; a missing helper names the tool to install ("install
// wl-clipboard on Wayland"), and an SSH session without X11 forwarding
// gets the reference's ssh advice.
func clipboardMediaNotice(err error) string {
	if err == nil {
		return clipboard.ErrNoMedia.Error()
	}
	return err.Error()
}

// attachMedia adds an image to the attachments of the next turn.
func (m *RootModel) attachMedia(media clipboard.Media) {
	if len(media.Data) == 0 {
		return
	}
	m.prompt.pendingMedia = append(m.prompt.pendingMedia, media)
	m.viewport.inputNotice = ""
}

// mediaImageParts turns the attachments into the transport's image
// parts. The mime type comes from the detected format, so the model
// receives the same inlineData block the reference sends.
func (m *RootModel) mediaImageParts() []backend.ImagePart {
	if len(m.prompt.pendingMedia) == 0 {
		return nil
	}
	parts := make([]backend.ImagePart, 0, len(m.prompt.pendingMedia))
	for _, media := range m.prompt.pendingMedia {
		mime := media.Format.MIMEType()
		if mime == "" {
			continue
		}
		parts = append(parts, backend.ImagePart{MIMEType: mime, Data: media.Data})
	}
	return parts
}

// ---- prompt_at.go ----

// prompt_at.go — the "@" prefix: the subagent dispatch.
//
// The composer's "@" opens two different things:
//
//   - "@<name> <message>" addresses a named subagent — the message is
//     delivered to it and the turn continues without the model;
//   - "@<path>" opens the file-mention dropdown, whose rows the
//     composer completes with Tab.
//
// The second one is a panel — internal/ui/views/mentionpicker — and
// the host only routes its keys. This file owns the first one, which
// is the host's own business: it delivers a message to a subagent and
// echoes the reference's answer.

// defaultRouter is the process-wide subagent router the "@<name>"
// dispatch resolves against.
var defaultRouter = subagentrouter.NewManager()

// atDispatchTimeout bounds the delivery of a message to a named
// subagent.
const atDispatchTimeout = 30 * 1000 * 1000 * 1000 // 30 seconds

// handlePromptAtPrefix dispatches a prompt that starts with "@" to a
// named subagent.
func (m *RootModel) handlePromptAtPrefix(buf string) (bool, tea.Cmd) {
	if len(buf) == 0 || buf[0] != '@' {
		return false, nil
	}
	name, message, ok := subagentrouter.ParsePrompt(buf)
	if !ok {
		return false, nil
	}
	m.resetPromptForAt(buf)
	if message == "" {
		m.conversation.response = append(m.conversation.response, "", "> "+buf, "", subagentrouter.NamedSubagentEmptyMessage(name))
		m.saveSession()
		return true, nil
	}
	m.deliverAtSubagent(name, message, buf)
	return true, nil
}

// resetPromptForAt clears the composer after an "@" dispatch.
func (m *RootModel) resetPromptForAt(buf string) {
	m.pushHistory(buf)
	m.prompt.input.Reset()
	m.conversation.streaming = false
	m.prompt.promptHistory.Reset()
	m.prompt.picker.Reset()
}

// deliverAtSubagent sends the message to the named subagent.
func (m *RootModel) deliverAtSubagent(name, message, buf string) {
	if m.agents.router == nil {
		m.agents.router = defaultRouter
	}
	entry, ok := m.agents.router.Lookup(name)
	if !ok {
		m.conversation.response = append(m.conversation.response, "", "> "+buf, "", subagentrouter.NamedSubagentUnknownMessage(name))
		m.saveSession()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), atDispatchTimeout)
	defer cancel()
	if err := entry.Sender.Send(ctx, message); err != nil {
		m.conversation.response = append(m.conversation.response,
			"",
			"> "+buf,
			"",
			strings.TrimSpace("at-subagent: failed to deliver: "+err.Error()),
		)
		m.saveSession()
		return
	}
	m.conversation.response = append(m.conversation.response,
		"",
		cWarningBlueOpen+"> @"+entry.Name+" "+message+cWarningBlueClose,
		"",
		panelGray238+"delivered to \""+entry.Name+"\""+panelGray238C,
		"",
	)
	m.saveSession()
}

// ---- trajectory.go ----

// trajectory.go — the conversation's durable record.
//
// The reference stores every step of a conversation in one trajectory
// table — the user inputs, the model responses, the tool calls — and
// derives the agent's live state from it when the conversation is
// loaded: the subagents it defined, the schedules it created, the
// subagents it killed (【反编译】 the reference's `cortex/rehydration`
// package and the cascade manager's `processLoadedTrajectory`, CLI
// 0x101f26440; see out/ide-language-server/README.md §10.9).
//
// The clone used to write only the model messages, so a resumed
// conversation lost every tool call it had made — and, because the
// clone writes to the same store the reference uses, it also replaced
// the reference's stored trajectory with that thinner record.
//
// The store owns the trajectory — `convstore.BuildSteps` assembles it,
// because the store is what the steps are written to. This file keeps
// the host's half: the tool calls a turn made, and the reset when a
// conversation ends.

// trajectoryFacts collects the four facts the store needs to assemble
// the conversation's trajectory.
//
// The host owns the facts — they are the live state of the running
// session — and the store owns the assembly (convstore.BuildSteps).
func (m *RootModel) trajectoryFacts() convstore.Trajectory {
	if sup := m.session.sup(); sup != nil {
		return sup.TrajectoryFacts()
	}
	return convstore.Trajectory{Session: m.SessionMessages()}
}

// trajectoryCounts reports the adopted step count, the calls made since and
// the number of transcripts the adopted conversation contributed.
func (m *RootModel) trajectoryCounts() (loaded, calls, seededLen int) {
	if sup := m.session.sup(); sup != nil {
		return sup.TrajectoryCounts()
	}
	return 0, 0, 0
}

// flushPendingCheckpoints inserts the checkpoints a turn recorded into the
// trajectory's call list. The insertion point is the number of calls that
// preceded the checkpoint, so the persisted order matches the turn.
func (m *RootModel) flushPendingCheckpoints() {
	m.session.flushCheckpoints()
}

// recordTrajectoryCalls adds a turn's tool calls to the trajectory.
//
// The steps come from the turn's own tool steps, so the raw arguments
// the model produced are stored verbatim — the rehydration pass reads
// them back to rebuild the state the calls produced (see
// internal/app/session/rehydration).
func (m *RootModel) recordTrajectoryCalls(steps []toolsteps.Step) {
	var convSteps []convstore.Step
	for _, s := range steps {
		if s.Kind != toolsteps.KindTool || s.Name == "" {
			continue
		}
		status := int64(3)
		if s.Err {
			status = 4
		}
		convSteps = append(convSteps, convstore.Step{
			Type:       convstore.StepToolCall,
			Status:     status,
			ToolName:   s.Name,
			ToolArgs:   s.Args,
			ToolResult: s.Result,
		})
	}
	if sup := m.session.sup(); sup != nil {
		sup.RecordTrajectoryCalls(convSteps)
	}
}

// resetTrajectory drops the trajectory of the conversation that ended.
func (m *RootModel) resetTrajectory() {
	if sup := m.session.sup(); sup != nil {
		sup.Clear()
	}
}
