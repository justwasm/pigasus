// model_host.go — the host's side of the /model panel.
//
// # Where the panel lives
//
// The panel itself is internal/ui/views/modelpicker: it owns the search
// box, the row list, the effort slider, the keys and the writes. The
// host supplies the three things the panel cannot answer itself — the
// label of the model the session runs, the way a chosen model reaches
// the session, and the way out of the panel — and nothing else.
//
// The panel is rebuilt every time it opens: the host's slashPanel struct
// is reset when a panel closes, so a stale copy of the search box, the
// cursor or the slider's position is never shown.

package cliapp

import (
	"context"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/internal/ui/views/modelpicker"
)

// newModelPanel builds the /model panel with the host's seams.
func (m *runMode) newModelPanel() *modelpicker.Panel {
	return modelpicker.New(modelpicker.Seams{
		// The running model's label is the session's: the panel marks
		// the row that is current and parks the slider on its tier.
		CurrentModelLabel: m.panelCurrentModelLabel,
		// The session's model, its status row and its transport are the
		// host's; the panel persists the catalogue slug and hands the
		// display label over.
		ApplyModel: func(label string) {
			m.model = label
			m.statusModel = tuilib.StripTier(label)
			m.syncBackendForModel()
		},
		// Esc leaves the panel.
		Close: m.closePanel,
		// CustomRows queries the active agentturn engine if it supports dynamic model discovery.
		CustomRows: m.engineModelRows,
	})
}

// engineModelRows returns the model rows from the active agentturn engine if it
// implements agentturn.ModelLister (e.g. Codex, Claude, Agy).
func (m *runMode) engineModelRows() []modelpicker.Row {
	if len(m.engineModelRowsCache) > 0 {
		return m.engineModelRowsCache
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
	m.engineModelRowsCache = rows
	return rows
}

// modelPanel returns the open /model panel.
//
// The panel is built when the /model panel opens — the host's slashPanel
// struct is reset whenever a panel closes — so the accessor is here so
// that a caller which finds the panel missing still gets one that
// answers.
func (m *runMode) modelPanel() *modelpicker.Panel {
	if m.panel.mdl == nil {
		m.panel.mdl = m.newModelPanel()
	}
	return m.panel.mdl
}

// syncBackendForModel rebuilds the transport after a model change. The
// selected family decides the endpoint: catalogue Gemini/Claude/GPT-OSS
// go to CloudCode, MiniMax to the gateway. A nil factory (unit tests
// constructing runMode directly) leaves the backend untouched.
func (m *runMode) syncBackendForModel() {
	if m.backendFor == nil || strings.TrimSpace(m.model) == "" {
		return
	}
	if b := m.backendFor(m.model); b != nil {
		m.backend = b
	}
	// Cycle 292: a new selection puts the reference's
	// <USER_SETTINGS_CHANGE> block on the next user turn.
	m.wireSettingsPending = true
}
