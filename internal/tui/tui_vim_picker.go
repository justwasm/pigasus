package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/features/tools/clipboard"
	"github.com/justwasm/pigasus/vim"
)

// tui_vim_picker.go — the host's thin half of the Vim editing mode.
//
// Cycle 216 put the TUI half of the bridge here: the engine lives in
// vim and knows nothing about bubbles or the run loop, so this file
// kept the composer in step with the engine, fed it one key at a time
// and handed a submit result to the shared submitPrompt path. Cycle
// 381, tranche 58 moved the key translation to
// internal/ui/editor/vimkeys.
//
// The third vertical slice moves the rest of the rules to
// internal/ui/vimbridge: the settings gate, the engine's seeding, the
// mirroring, the key loop, the paste fold and the "/model <query>"
// completion. What is left here is the delegation the host's callers
// already use — every method keeps its signature, so no call site
// changed — plus the two seams that reach the live engine (the submit
// reset and the slash rewrite seed it directly).
//
// Everything is inert unless the "Editor Mode" setting is vim, so the
// default (non-vim) prompt behaves exactly as it did before.

// vimEnabled reports whether "Editor Mode" selects vim. The value is
// read from settings.json at TUI start-up (and again whenever a slash
// panel closes, so toggling Editor Mode in /config applies without a
// restart); the bridge keeps it in a field rather than re-reading it per
// keystroke so the tests stay hermetic and the render path does no file
// I/O.
func (m *RootModel) vimEnabled() bool { return m.vimBridge().Enabled() }

func (m *RootModel) VimEnabled() bool { return m.vimEnabled() }

// reloadVimSettings reads the two vim settings from the live
// settings.json. It is called once at TUI start-up and again when a
// slash panel closes (/config can change both).
func (m *RootModel) reloadVimSettings() {
	b := m.vimBridge()
	b.ReloadSettings()
	// Keep the composer sub-model's mirror in step: the host's own paths
	// (and the tests) read and write the engine there.
	m.prompt.vimOn = b.Enabled()
	m.prompt.vimFirstOn = b.InsertFirstEnabled()
	m.prompt.vimEng = b.Engine()
}

// vimMode reports the mode the status row's badge shows.
func (m *RootModel) vimMode() vim.Mode { return m.vimBridge().Mode() }

// syncVimInput mirrors the engine onto the textarea.
func (m *RootModel) syncVimInput() { m.vimBridge().SyncInput() }

// handleVimKey feeds one key to the engine. handled is false when the
// engine declined the key, which is the signal for the caller to fall
// through to the host's own binding (ctrl+c, ctrl+d, ctrl+o, Tab, …).
func (m *RootModel) handleVimKey(key tea.Key) (tea.Cmd, bool) {
	return m.vimBridge().HandleKey(key)
}

// clipboardReadText is the platform clipboard read (tests stub it).
var clipboardReadText = clipboard.Read

// insertPastedText is the single insertion point for both the
// terminal's bracketed paste and ctrl+v. It never submits.
func (m *RootModel) insertPastedText(s string) tea.Cmd {
	return m.vimBridge().InsertPastedText(s)
}

// applyModelArgument makes the model the argument names the session's
// model and echoes the reference's confirmation.
func (m *RootModel) applyModelArgument(arg string) tea.Cmd {
	return m.vimBridge().ApplyModelArgument(arg)
}

// submitModelCompletion runs the Enter semantics of the "/model <query>"
// popup. It reports whether it consumed the key.
func (m *RootModel) submitModelCompletion() (bool, tea.Cmd) {
	return m.vimBridge().SubmitModelCompletion()
}

// checkpointInsertionPoint is the call count a compact checkpoint is
// inserted at: the calls before the turn plus the turn's live steps,
// keeping the recent tail the compaction retained.
func (m *RootModel) checkpointInsertionPoint(keepRecent int) int {
	return m.vimBridge().CheckpointInsertionPoint(keepRecent)
}
