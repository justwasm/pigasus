package tui

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	"github.com/justwasm/pigasus/internal/core/settings"
	"github.com/justwasm/pigasus/internal/features/models"
	"github.com/justwasm/pigasus/internal/infra/backend/backendselect"
	"github.com/justwasm/pigasus/internal/tui/panels"
	"github.com/justwasm/pigasus/internal/ui/editor/editorkit"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/internal/ui/views/codesearchpanel"
	"github.com/justwasm/pigasus/internal/ui/views/modelpicker"
)

// Codesearch
func (m *RootModel) SearchRoot() string    { return m.session.cwd }
func (m *RootModel) SearchStreaming() bool { return m.display.streamingSearch }
func (m *RootModel) CodesearchReadFile(path string) ([]string, string, bool) {
	return m.codesearchReadFile(path)
}
func (m *RootModel) CodesearchOpenEditor(path string, line int) tea.Cmd {
	return m.codesearchOpenEditor(path, line)
}
func (m *RootModel) CodesearchSendComments(text string) tea.Cmd {
	return m.codesearchSendComments(text)
}
func (m *RootModel) newCodesearchPanel() *codesearchpanel.Panel {
	return panels.NewCodesearchPanel(m)
}
func (m *RootModel) codesearchPanel() *codesearchpanel.Panel {
	if m.overlays.panel.csp == nil {
		m.overlays.panel.csp = m.newCodesearchPanel()
	}
	return m.overlays.panel.csp
}
func (m *RootModel) codesearchReadFile(path string) ([]string, string, bool) {
	full := m.codesearchPath(path)
	info, err := os.Stat(full)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Sprintf("File not found: %s", full), false
		}
		return nil, err.Error(), false
	}
	if !info.Mode().IsRegular() {
		return nil, codesearchPipeMessage, false
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return nil, err.Error(), false
	}
	return splitFileLines(data), "", true
}
func (m *RootModel) codesearchPath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(m.session.cwd, path)
}
func (m *RootModel) codesearchOpenEditor(path string, line int) tea.Cmd {
	name, args, notice, err := editorkit.ResolveEditor(settings.String("editor"))
	if err != nil {
		m.conversation.response = append(m.conversation.response, "Failed to resolve editor: "+err.Error())
		return nil
	}
	if _, err := exec.LookPath(name); err != nil {
		m.conversation.response = append(m.conversation.response, "Failed to resolve editor: "+err.Error())
		return nil
	}
	if notice != "" {
		m.conversation.response = append(m.conversation.response, notice)
	}
	full := m.codesearchPath(path)
	c := exec.Command(name, append(args, editorLineArgs(name, full, line)...)...)
	c.Dir = m.session.cwd
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return editorFinishedMsg{err: err}
	})
}
func (m *RootModel) codesearchSendComments(text string) tea.Cmd {
	closed := m.closePanel()
	if strings.TrimSpace(text) == "" {
		return closed
	}
	return tea.Batch(closed, m.startModelTurn(text))
}

// Model
// statusModeLabel is the agent mode the built-in status row displays:
// "auto", "plan", "accept-edits", or "" while the session has no configured mode.
func (m *RootModel) statusModeLabel() string {
	return strings.TrimSpace(m.session.mode)
}

// cycleModeLabel is the agent mode reported in the external statusline JSON
// script's `cycle_mode` field — omitted when the session runs in the mode it
// started in.
func (m *RootModel) cycleModeLabel() string {
	return modeLabelFrom(m.session.mode, m.session.startMode)
}

func modeLabelFrom(mode, start string) string {
	mode = strings.TrimSpace(mode)
	if mode == "" || mode == strings.TrimSpace(start) {
		return ""
	}
	return mode
}

// promptPlaceholder is the composer's placeholder for a session: the live
// mode's banner.
func (m *RootModel) promptPlaceholder(mode string) string {
	return permissions.ModeBanner(strings.TrimSpace(mode))
}

func (m *RootModel) ApplyModel(label string) {
	m.session.model = label
	m.session.statusModel = tuilib.StripTier(label)
	m.syncBackendForModel()
}
func (m *RootModel) CustomModelRows() []modelpicker.Row { return m.engineModelRows() }
func (m *RootModel) engineModelRows() []modelpicker.Row {
	if len(m.display.engineModelRowsCache) > 0 {
		return m.display.engineModelRowsCache
	}
	eng := m.turnEngine()
	if eng == nil {
		return nil
	}
	lister, ok := eng.(agentturn.ModelLister)
	if !ok {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	models, err := lister.ListModels(ctx)
	if err != nil || len(models) == 0 {
		return nil
	}
	rows := make([]modelpicker.Row, 0, len(models))
	for _, info := range models {
		rows = append(rows, modelpicker.Row{
			Display:     info.DisplayName,
			Slug:        info.ID,
			Note:        info.Description,
			Tiers:       info.Tiers,
			DefaultTier: info.DefaultTier,
		})
	}
	m.display.engineModelRowsCache = rows
	return rows
}

func (m *RootModel) syncBackendForModel() {
	if m.turn.backendFor == nil || strings.TrimSpace(m.session.model) == "" {
		return
	}
	if b := m.turn.backendFor(m.session.model); b != nil {
		m.turn.backend = b
		if backend.IsMock(b) && !backendselect.IsReferenceModel(m.session.model, modelpicker.ResolveCatalogueID) {
			m.session.modelWarning = fmt.Sprintf("Model %q is not configured in %s. Falling back to mock shim.", m.session.model, models.ProvidersPath())
		} else {
			m.session.modelWarning = ""
		}
	}
	m.session.wireSettingsPending = true
}
func (m *RootModel) IsRemoteAgent() bool               { return m != nil && m.session.remoteAgent }
func (m *RootModel) newModelPanel() *modelpicker.Panel { return panels.NewModelPanel(m) }
func (m *RootModel) modelPanel() *modelpicker.Panel {
	if m.overlays.panel.mdl == nil {
		m.overlays.panel.mdl = m.newModelPanel()
	}
	return m.overlays.panel.mdl
}
