// picker_model.go — the "/model <query>" completion popup.
//
// The reference's composer completes the /model *argument* with a popup
// of models: the display label in a 30-column field, the catalogue id two
// columns further right. 【一手】 agy 1.4.6, live tmux capture:
//
//	> /model claude
//	────────────────────────────────────────────────────────
//	> Claude Sonnet 4.6 (Thinking)  claude-sonnet-4-6
//	  Claude Opus 4.6 (Thinking)    claude-opus-4-6-thinking
//
//	  ↑/↓ Navigate · enter Select · tab Complete
//
// The filter is a case-insensitive *substring* of the label or of the id
// (internal/ui/views/slashpicker), the popup replaces the command rows
// while it is open, and Enter has three outcomes:
//
//   - the argument already names a model → the command runs and the model
//     changes ("Model set to <label>");
//   - the argument does not, but a row is highlighted → the row's label is
//     completed into the composer and the popup closes;
//   - the argument does not and nothing matches → the command runs and
//     reports `Invalid model "zzz" (available: ...)`.
//
// Tab always completes the highlighted row into the composer.
//
// The popup itself is a panel (internal/ui/views/slashpicker), and the
// argument's resolution against the catalogue belongs to the ui tier
// (internal/ui/views/modelargpopup). This file owns what the host has
// to do with a *resolved* model — making it the session's model and
// echoing the reference's answer.
package cliapp

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/core/settings"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/internal/ui/views/modelargpopup"
)

// applyModelArgument makes the model the argument names the session's
// model and echoes the reference's confirmation.
//
// 【一手】 agy 1.4.6, live: "/model Claude Sonnet 4.6 (Thinking)" answers
// "Model set to Claude Sonnet 4.6 (Thinking)" and the status row follows;
// naming the model that is already selected answers "Model already set to
// <label>".
func (m *runMode) applyModelArgument(arg string) tea.Cmd {
	entry, ok := modelargpopup.Resolve(arg)
	if !ok {
		// If attached to a remote backend with dynamic models, check its catalogue:
		if lister, ok := m.turnEngine().(agentturn.ModelLister); ok {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			models, _ := lister.ListModels(ctx)
			cancel()
			argLower := strings.ToLower(strings.TrimSpace(arg))
			for _, mdl := range models {
				if strings.ToLower(mdl.ID) == argLower || strings.ToLower(mdl.DisplayName) == argLower {
					if m.model == mdl.ID || m.model == mdl.DisplayName {
						m.slashRespond("/model", "Model already set to "+mdl.DisplayName)
						return nil
					}
					if m.streaming && m.turnCancel != nil {
						m.turnCancel()
						m.turnCancel = nil
						m.turnInterrupted = true
					}
					_ = settings.SetField("model", mdl.ID)
					m.model = mdl.ID
					m.statusModel = tuilib.StripTier(mdl.DisplayName)
					m.slashRespond("/model", "Model set to "+mdl.DisplayName)
					return nil
				}
			}
		}
		m.slashRespond("/model", modelargpopup.InvalidMessage(arg))
		return nil
	}
	if m.model == entry.Label {
		m.slashRespond("/model", "Model already set to "+entry.Label)
		return nil
	}
	if m.streaming && m.turnCancel != nil {
		m.turnCancel()
		m.turnCancel = nil
		m.turnInterrupted = true
	}
	_ = settings.SetField("model", entry.ID)
	m.model = entry.Label
	m.statusModel = tuilib.StripTier(entry.Label)
	m.syncBackendForModel()
	m.slashRespond("/model", "Model set to "+entry.Label)
	return nil
}

// submitModelCompletion runs the Enter semantics of the "/model <query>"
// popup. It reports whether it consumed the key.
func (m *runMode) submitModelCompletion() (bool, tea.Cmd) {
	if !m.picker.IsModelMode() {
		return false, nil
	}
	arg := strings.TrimSpace(m.picker.Query())
	if entry, ok := modelargpopup.Resolve(arg); ok {
		m.picker.Reset()
		m.input.Reset()
		m.store.ResetInputSelection()
		m.promptHistory.Reset()
		if m.model == entry.Label {
			m.slashRespond("/model", "Model already set to "+entry.Label)
			return true, nil
		}
		_ = settings.SetField("model", entry.ID)
		m.model = entry.Label
		m.statusModel = tuilib.StripTier(entry.Label)
		m.syncBackendForModel()
		m.slashRespond("/model", "Model set to "+entry.Label)
		return true, nil
	}
	// The argument names no model: the highlighted row is completed into
	// the composer instead of running the command.
	if sel := m.picker.Selected(); sel != nil {
		m.input.SetValue("/model " + sel.Name)
		m.picker.Reset()
		m.promptHistory.Reset()
		return true, nil
	}
	// Nothing matches at all: the command runs and reports the argument.
	m.picker.Reset()
	m.input.Reset()
	m.store.ResetInputSelection()
	m.promptHistory.Reset()
	m.slashRespond("/model", modelargpopup.InvalidMessage(arg))
	return true, nil
}
