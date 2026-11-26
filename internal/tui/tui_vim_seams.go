package tui

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/core/settings"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/views/mentionpicker"
	"github.com/justwasm/pigasus/internal/ui/views/slashpicker"
	"github.com/justwasm/pigasus/internal/ui/vimbridge"
)

// tui_vim_seams.go — the host half of the vim bridge.
//
// The rules live in internal/ui/vimbridge: the settings gate, the
// engine's seeding, the mirroring onto the textarea, the key loop, the
// paste fold and the "/model <query>" completion. What stays here is
// what only the host can do — reach the turn engine, paint the
// transcript, own the composer and the panels — plus the adapters that
// present the host's own types through the bridge's narrow ports.
//
// Every adapter reads the model at call time, so a bridge built before
// the composer or the panels exist still writes to the live ones.

// vimBridge returns the session's bridge, building it on first use.
//
// The bridge is cached on the model so the engine and the two settings
// live between keys; reloadVimSettings re-seeds it from the settings
// file (and from the engine the host stashed on the composer sub-model,
// which is where the tests and the older host paths keep it).
func (m *RootModel) vimBridge() *vimbridge.Bridge {
	if m.prompt.vim == nil {
		m.prompt.SetVimBridge(m.newVimBridge())
	} else {
		// A bridge the composer built from its mirror has no seams yet.
		m.prompt.vim.SetPorts(m.newVimPorts())
	}
	return m.prompt.vim
}

// newVimBridge assembles the host seams the vim bridge asks for.
func (m *RootModel) newVimBridge() *vimbridge.Bridge {
	return vimbridge.New(m.newVimPorts())
}

// newVimPorts is the host's side of the bridge.
func (m *RootModel) newVimPorts() vimbridge.Ports {
	return vimbridge.Ports{
		Input:       vimComposerPort{m: m},
		Picker:      vimPickerPort{m: m},
		AtFile:      vimAtFilePort{m: m},
		Keybindings: vimKeybindingsPort{m: m},
		Session:     vimSessionPort{state: &m.session},
		Overlays:    vimOverlayPort{m: m},
		History:     vimHistoryPort{m: m},
		Pastes:      vimPasteStore{m: m},
		Undo:        vimUndoPort{m: m},
		Host:        vimHostPort{m: m},
		Remote:      vimRemoteModelsPort{m: m},
		Counts:      vimCheckpointCounts{m: m},
		Settings:    settings.Field,
	}
}

// vimComposer adapts the composer widget to the bridge's input port.
//
// It reads the model at call time rather than holding a *textarea.Model,
// so the port never goes stale when the composer is replaced and a test
// that writes m.prompt.input directly is seen by the bridge.
type vimComposerPort struct{ m *RootModel }

func (t vimComposerPort) Value() string { return t.m.prompt.input.Value() }
func (t vimComposerPort) SetValue(v string) {
	t.m.prompt.input.SetValue(v)
}
func (t vimComposerPort) InsertString(s string) { t.m.prompt.input.InsertString(s) }
func (t vimComposerPort) Reset()                { t.m.prompt.input.Reset() }
func (t vimComposerPort) LineCount() int        { return t.m.prompt.input.LineCount() }
func (t vimComposerPort) CursorUp()             { t.m.prompt.input.CursorUp() }
func (t vimComposerPort) SetCursorColumn(c int) { t.m.prompt.input.SetCursorColumn(c) }
func (t vimComposerPort) Line() int             { return t.m.prompt.input.Line() }
func (t vimComposerPort) Column() int           { return t.m.prompt.input.Column() }

// vimPickerPort adapts the slash popup to the bridge's picker port. It
// reads the panel off the model at call time, so a host that replaces the
// popup is seen without rebuilding the bridge.
type vimPickerPort struct{ m *RootModel }

func (p vimPickerPort) panel() *slashpicker.Panel { return p.m.prompt.picker }

func (p vimPickerPort) Update(buffer []rune) bool {
	panel := p.panel()
	return panel != nil && panel.Update(buffer)
}
func (p vimPickerPort) Reset() {
	if panel := p.panel(); panel != nil {
		panel.Reset()
	}
}
func (p vimPickerPort) IsModelMode() bool {
	panel := p.panel()
	return panel != nil && panel.IsModelMode()
}
func (p vimPickerPort) Query() string {
	if panel := p.panel(); panel != nil {
		return panel.Query()
	}
	return ""
}
func (p vimPickerPort) Selected() *vimbridge.ModelRow {
	panel := p.panel()
	if panel == nil {
		return nil
	}
	sel := panel.Selected()
	if sel == nil {
		return nil
	}
	return &vimbridge.ModelRow{Name: sel.Name, Description: sel.Description}
}

// vimAtFilePort adapts the "@" mention popup to the bridge's port.
type vimAtFilePort struct{ m *RootModel }

func (p vimAtFilePort) panel() *mentionpicker.Panel { return p.m.prompt.atFilePicker }

func (p vimAtFilePort) Update() bool {
	panel := p.panel()
	return panel != nil && panel.Update()
}
func (p vimAtFilePort) Reset() {
	if panel := p.panel(); panel != nil {
		panel.Reset()
	}
}

// vimKeybindingsPort adapts the parsed keybindings.json.
type vimKeybindingsPort struct{ m *RootModel }

func (k vimKeybindingsPort) ActionForExactKeyInScope(key, scope string) string {
	kb := k.m.prompt.keybindings
	if kb == nil {
		return ""
	}
	return kb.ActionForExactKeyInScope(key, scope)
}

// vimSessionPort adapts the session state to the bridge's model port.
type vimSessionPort struct{ state *sessionState }

func (p vimSessionPort) Model() string           { return p.state.model }
func (p vimSessionPort) SetModel(label string)   { p.state.model = label }
func (p vimSessionPort) SetStatusModel(l string) { p.state.statusModel = l }
func (p vimSessionPort) ModelWarning() string {
	return p.state.modelWarning
}

// vimOverlayPort adapts the overlay state.
type vimOverlayPort struct{ m *RootModel }

func (p vimOverlayPort) PanelKind() panelregistry.Kind { return p.m.overlays.panel.kind }
func (p vimOverlayPort) ResumePickerActive() bool      { return p.m.overlays.resumePicker.Active() }
func (p vimOverlayPort) ResumePickerTypeText(s string) bool {
	if !p.m.overlays.resumePicker.Active() {
		return false
	}
	p.m.overlays.resumePicker.TypeText(s)
	return true
}

// vimHistoryPort adapts the prompt history.
type vimHistoryPort struct{ m *RootModel }

func (p vimHistoryPort) ResetHistory() { p.m.prompt.promptHistory.Reset() }

// vimPasteStore adapts the composer's folded paste blocks.
type vimPasteStore struct{ m *RootModel }

// Fold records text under the next "[Pasted text #N …]" placeholder and
// returns the placeholder, matching the reference's own wording.
func (p vimPasteStore) Fold(text string, lines, chars int) string {
	if p.m.prompt.pastedBlocks == nil {
		p.m.prompt.pastedBlocks = make(map[string]string)
	}
	p.m.prompt.nextPasteID++
	var placeholder string
	if lines >= vimbridge.FoldLines {
		placeholder = fmt.Sprintf("[Pasted text #%d +%d lines]", p.m.prompt.nextPasteID, lines)
	} else {
		placeholder = fmt.Sprintf("[Pasted text #%d %d chars]", p.m.prompt.nextPasteID, chars)
	}
	p.m.prompt.pastedBlocks[placeholder] = text
	return placeholder
}

// vimUndoPort adapts the composer's undo manager.
type vimUndoPort struct{ m *RootModel }

func (p vimUndoPort) RecordBeforeChange(value string, cursor int, kind string) {
	if p.m.prompt.undoMgr == nil {
		return
	}
	p.m.prompt.undoMgr.RecordBeforeChange(value, cursor, kind)
}

// vimHostPort is the set of effects only the host can perform.
type vimHostPort struct{ m *RootModel }

func (p vimHostPort) SubmitPrompt() tea.Cmd { return p.m.submitPrompt() }

// FocusActiveItems moves the keyboard into the Active Items block when
// it is showing running work, and reports whether it took the key. The
// "Show Active Tasks" setting hides the block, and a hidden block has
// nothing to focus.
func (p vimHostPort) FocusActiveItems() bool {
	panel := p.m.activeItems.Panel
	if panel == nil || panel.Hidden() {
		return false
	}
	// Count() reads the panel's own registry seam, which a host that
	// builds the block without seams leaves nil; the model's registry is
	// the session's, so fall back to it. The swarm row is not a task.
	if panel.Count() == 0 && !p.m.hasActiveSwarm() {
		if reg := p.m.TaskRegistry(); reg == nil || len(reg.Active()) == 0 {
			return false
		}
	}
	panel.SetFocus(true)
	panel.SelectFirst()
	return true
}

func (p vimHostPort) ClearInputSelection() { p.m.ResetInputSelection() }
func (p vimHostPort) HasActiveModal() bool { return p.m.hasActiveModal() }
func (p vimHostPort) HasInputSelection() bool {
	return p.m.prompt.HasInputSelection()
}

// DeleteSelection replaces the composer's selected range: the paste path
// deletes the range through the same module the mouse selection uses, so
// the textarea and the selection model stay in step.
func (p vimHostPort) DeleteSelection() {
	p.m.prompt.DeleteInputSelection()
}
func (p vimHostPort) ClearEligibilityNotice() { p.m.clearEligibilityNotice() }
func (p vimHostPort) Respond(cmd, msg string) { p.m.slashRespond(cmd, msg) }
func (p vimHostPort) CancelTurn()             { p.m.cancelActiveTurn() }
func (p vimHostPort) SyncBackend()            { p.m.syncBackendForModel() }
func (p vimHostPort) PersistModel(id string)  { _ = settings.SetField("model", id) }

// vimRemoteModelsPort is the attached backend's own catalogue.
type vimRemoteModelsPort struct{ m *RootModel }

func (p vimRemoteModelsPort) ListRemoteModels(ctx context.Context) []agentturn.ModelInfo {
	lister, ok := p.m.turnEngine().(agentturn.ModelLister)
	if !ok {
		return nil
	}
	models, _ := lister.ListModels(ctx)
	return models
}

// vimCheckpointCounts is the trajectory bookkeeping.
type vimCheckpointCounts struct{ m *RootModel }

func (p vimCheckpointCounts) TrajectoryCalls() int {
	_, calls, _ := p.m.trajectoryCounts()
	return calls
}

func (p vimCheckpointCounts) LiveSteps() int { return len(p.m.conversation.liveSteps) }
