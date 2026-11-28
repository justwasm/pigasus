package tui

import (
	tea "charm.land/bubbletea/v2"
	atfiles "github.com/justwasm/pigasus/internal/app/prompt/mentions/files"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/app/subagent/questions"
	"github.com/justwasm/pigasus/internal/tui/panels"
	tuiviewer "github.com/justwasm/pigasus/internal/ui/foundations/viewer"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/views/changelog"
	"github.com/justwasm/pigasus/internal/ui/views/mentionpicker"
	"github.com/justwasm/pigasus/internal/ui/views/subagentquestionpanel"
	"github.com/justwasm/pigasus/internal/ui/views/viewerpanel"
)

// Viewer
func (m *RootModel) ViewerSpec() (tuiviewer.Spec, bool) {
	return m.getPanelRegistry().ViewerSpec(m.overlays.panel.kind)
}
func (m *RootModel) ViewerPageRows(spec tuiviewer.Spec) int {
	if m.overlays.panel.kind == panelregistry.Changelog {
		return changelog.PageRows(spec, m.viewport.height)
	}
	return spec.Visible
}
func (m *RootModel) ViewerWidth() int                   { return m.viewerVPWidth() }
func (m *RootModel) newViewerPanel() *viewerpanel.Panel { return panels.NewViewerPanel(m) }
func (m *RootModel) viewerPanel() *viewerpanel.Panel {
	if m.overlays.panel.vw == nil {
		p := m.newViewerPanel()
		p.Open()
		m.overlays.panel.vw = p
	}
	return m.overlays.panel.vw
}

func (m *RootModel) initViewerViewport() {
	if m.overlays.panel.vw == nil {
		m.overlays.panel.vw = m.newViewerPanel()
	}
	m.overlays.panel.vw.Open()
}

func (m *RootModel) viewerVPWidth() int {
	if m.viewport.width > 0 {
		return m.viewport.width
	}
	return 80
}

func (m *RootModel) handleViewerPanelKey(key tea.Key) (bool, tea.Cmd) {
	// /logs is a panel now, not a document: it owns the view bar, the
	// fold keys and the follow toggle, so its keys go to the panel rather
	// than to the document viewer's scroll handler.
	if m.overlays.panel.kind == panelregistry.Debug {
		handled, cmd := m.debugPanel().HandleKey(key)
		if handled {
			m.invalidateViewCache()
		}
		return handled, cmd
	}
	handled, cmd, offset := m.viewerPanel().HandleKey(key, m.overlays.panel.window)
	m.overlays.panel.window = offset
	return handled, cmd
}

func (m *RootModel) scrollViewerPanel(delta int) {
	if m.overlays.panel.kind == panelregistry.Debug {
		if m.debugPanel().Scroll(delta) {
			m.invalidateViewCache()
		}
		return
	}
	m.overlays.panel.window = m.viewerPanel().Scroll(delta, m.overlays.panel.window)
}

// listResumeConversations returns the store's conversations for the /resume picker.
// The active conversation is included and tagged [CURRENT] (matching agy 1.5.1).
func (m *RootModel) listResumeConversations() ([]convstore.ConversationMeta, error) {
	return convstore.ListConversations()
}

func (m *RootModel) renameResumeConversation(meta convstore.ConversationMeta) {
	m.session.conversationName = meta.Title
	m.openRenamePanel()
}

func (m *RootModel) openRenamePanel() {
	m.overlays.panel.kind = panelregistry.Rename
	m.overlays.panel.cmd = "/rename"
	m.overlays.panel.rn = m.newRenamePanel()
	m.armRenameFocus()
}

func (m *RootModel) armRenameFocus() {
	if m.overlays.panel.rn == nil {
		return
	}
	cmd := m.overlays.panel.rn.Focus()
	if cmd != nil && m.turn.prog != nil {
		m.turn.prog.Send(cmd)
	}
}

func (m *RootModel) AppendSubAgentResponse(line string) {
	m.conversation.response = append(m.conversation.response, line)
}

// SubAgentQuestion
func (m *RootModel) SubAgentQuestionsManager() *questions.Manager {
	return m.askQuestion.manager
}
func (m *RootModel) ResetInput() { m.prompt.input.Reset() }
func (m *RootModel) newSubAgentQuestionPanel() *subagentquestionpanel.Panel {
	return panels.NewSubAgentQuestionPanel(m)
}

// Mention
func (m *RootModel) InputValue() string { return m.prompt.input.Value() }
func (m *RootModel) SetInputValue(val string) {
	m.prompt.input.SetValue(val)
	m.prompt.SetInputCursorOffset(len(val))
	m.prompt.input.CursorEnd()
	if m.prompt.vimEng != nil {
		m.prompt.vimEng.SetTextKeepingPending(val)
		m.prompt.vimEng.Row = len(m.prompt.vimEng.Lines) - 1
		if m.prompt.vimEng.Row >= 0 && m.prompt.vimEng.Row < len(m.prompt.vimEng.Lines) {
			m.prompt.vimEng.Col = len([]rune(m.prompt.vimEng.Lines[m.prompt.vimEng.Row]))
		}
		m.syncVimInput()
	}
}
func (m *RootModel) TerminalWidth() int                     { return m.viewport.width }
func (m *RootModel) TerminalHeight() int                    { return m.viewport.height }
func (m *RootModel) newMentionPicker() *mentionpicker.Panel { return panels.NewMentionPicker(m) }

// MentionList makes the @ picker read the remote workspace in a
// --remote-agent session. The picker is built before the flag injection, so
// the decision is made per call rather than at construction.
func (m *RootModel) MentionList() func(string) ([]atfiles.Entry, error) {
	return func(root string) ([]atfiles.Entry, error) {
		if m != nil && m.session.remoteAgent && m.remote.files != nil {
			return m.remote.files.List(root)
		}
		return atfiles.List(root)
	}
}

func (m *RootModel) IsInputY(y int) bool { return m.prompt.IsInputY(y) }
