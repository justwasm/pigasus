package tui

// prompt_model.go — the composer sub-model.
//
// PromptModel owns everything the user types into and attaches to the
// prompt: the textarea, undo history, Vim engine, the Esc-Esc clear state,
// prompt history, the slash/mention completion popups, pasted-text blocks,
// pending media and the input-area mouse selection.
//
// RootModel routes keys here and performs the two operations that are not
// the model's: writing the clipboard and submitting the turn.
//
// Upstream: google3/third_party/jetski/cli/model/model.PromptModel /
// InputModel (the same split; the reference keeps the composer's editor and
// its suggestions beside each other in one package).

import (
	"strings"
	"time"

	tas "charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/features/tools/clipboard"
	"github.com/justwasm/pigasus/internal/ui/editor/composer"
	"github.com/justwasm/pigasus/internal/ui/editor/keybind"
	"github.com/justwasm/pigasus/internal/ui/foundations/history"
	inputselection "github.com/justwasm/pigasus/internal/ui/foundations/inputselection"
	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
	"github.com/justwasm/pigasus/internal/ui/views/mentionpicker"
	"github.com/justwasm/pigasus/internal/ui/views/slashpicker"
	"github.com/justwasm/pigasus/vim"
)

// PromptModel owns the composer's state and its input-area selection.
type PromptModel struct {
	tuiviewport.InputSelection

	// Cycle 71: input component. charm.land/bubbles/v2 textarea
	input tas.Model
	// Undo / redo stack manager for composer input
	undoMgr *composer.UndoManager
	// Cycle 216: the Vim editing engine, created on first use when
	vimEng *vim.State
	// vimOn / vimFirstOn mirror the "Editor Mode" and "Editor Mode ›
	vimOn      bool
	vimFirstOn bool
	// Cycle alt-keybinding-option-meta: the user's
	keybindings *keybind.Keybindings
	// altEscBuf holds one buffered bare Esc key while the TUI waits
	altEscBuf keybind.EscBuffer
	// altKeyFires records every alt+letter dispatch in chronological
	altKeyFires []altKeyFire
	// Cycle 383 (B-21): the two-step Esc that clears the composer.
	// escArmedFor holds the composer content the pair was armed on; the
	// empty string means "not armed". The reference clears the prompt box
	// on `esc esc` (official-docs/cli/using.md:22), so the first Esc only
	// arms the clear and the second one performs it.
	escArmedFor string
	// Reference agy: absorb rapid unbracketed Enter key bursts (pasting in terminals
	// lacking bracketed paste support). Spaced < 10ms, consecutive Enters become '\n'.
	lastEnterTime   time.Time
	enterBurstCount int
	// Pasted text blocks folded into placeholders ([Pasted text #N +M lines] or [Pasted text #N M chars]).
	pastedBlocks map[string]string
	nextPasteID  int
	// Cycle 381: the prompt history is a list
	// (internal/ui/foundations/history). It owns the remembered prompts,
	// the navigation's index and the draft the navigation stashed; the
	// host owns the composer and the persistence to history.jsonl.
	promptHistory history.List
	// Cycle 381: the slash-command completion popup is a panel
	// (internal/ui/views/slashpicker). It owns the filtered rows, the
	// highlight, the scroll window, the on-screen rectangle, the rows
	// it writes and the keys it handles; the host routes the rest.
	picker *slashpicker.Panel
	// Cycle 381: the "@" file-mention dropdown is a panel
	// (internal/ui/views/mentionpicker). It owns the candidate list,
	// the highlight, the mention token's span, the rows it writes and
	// the keys it handles; the host routes the rest.
	atFilePicker *mentionpicker.Panel
	// promptBodyStart is the line index in the pre-trim body
	promptBodyStart int
	promptScreenY   int
	// Cycle 362 (agy 1.4.9): the images the user attached to the next
	// turn. A paste of an image — from the clipboard or from a dropped
	// image file — lands here and rides along with the next user
	// message as an image part.
	pendingMedia  []clipboard.Media
	mediaExpanded bool
	// modeHintDismissed records that the user has edited the input since
	modeHintDismissed bool
}

// ComposerValue returns the current text inside the composer input.
func (p *PromptModel) ComposerValue() string {
	return p.input.Value()
}

// SetComposerValue updates the text inside the composer input.
func (p *PromptModel) SetComposerValue(val string) {
	p.input.SetValue(val)
}

// IsComposerEmpty reports whether the composer input is empty or only whitespace.
func (p *PromptModel) IsComposerEmpty() bool {
	return strings.TrimSpace(p.input.Value()) == ""
}

// ClearComposer clears the composer input and disarms any pending Esc clear state.
func (p *PromptModel) ClearComposer() {
	p.input.SetValue("")
	p.escArmedFor = ""
	p.pastedBlocks = nil
	p.nextPasteID = 0
	p.ResetInputSelection()
}

// HasPendingMedia reports whether there are media attachments pending for the next turn.
func (p *PromptModel) HasPendingMedia() bool {
	return len(p.pendingMedia) > 0
}

// AddPendingMedia appends a media attachment to the pending list.
func (p *PromptModel) AddPendingMedia(m clipboard.Media) {
	p.pendingMedia = append(p.pendingMedia, m)
}

// ClearPendingMedia empties the pending media list.
func (p *PromptModel) ClearPendingMedia() {
	p.pendingMedia = nil
}

// IsMediaExpanded reports whether the attached media preview is expanded.
func (p *PromptModel) IsMediaExpanded() bool {
	return p.mediaExpanded
}

// ToggleMediaExpanded toggles whether the media preview is expanded.
func (p *PromptModel) ToggleMediaExpanded() {
	p.mediaExpanded = !p.mediaExpanded
}

// ArmEsc arms the composer for two-step Esc clear on the given text.
func (p *PromptModel) ArmEsc(text string) {
	p.escArmedFor = text
}

// DisarmEsc clears the two-step Esc armed state.
func (p *PromptModel) DisarmEsc() {
	p.escArmedFor = ""
}

// IsEscArmed reports whether two-step Esc clear is currently armed.
func (p *PromptModel) IsEscArmed() bool {
	return p.escArmedFor != ""
}

// VimActive reports whether Vim editing mode is enabled in the composer.
func (p *PromptModel) VimActive() bool {
	return p.vimOn
}

// SetVimMode sets whether Vim editing mode is active in the composer.
func (p *PromptModel) SetVimMode(on bool) {
	p.vimOn = on
}

// InputLineCount returns the visual line count of the input field (at least 1).
func (p *PromptModel) InputLineCount() int {
	if p.input.LineCount() > 0 {
		return p.input.LineCount()
	}
	return 1
}

// IsInputY reports whether the screen row y falls within the input field.
func (p *PromptModel) IsInputY(y int) bool {
	if p.promptScreenY < 0 {
		return false
	}
	return y >= p.promptScreenY && y < p.promptScreenY+p.InputLineCount()
}

// HasInputSelection reports whether a non-empty selection is active.
func (p *PromptModel) HasInputSelection() bool {
	return p.InputSelStart != p.InputSelEnd
}

// InputOffsetAt maps screen coordinate (x, y) to a byte offset within the
// input value.
func (p *PromptModel) InputOffsetAt(x, y int) int {
	return inputselection.OffsetAt(p.input.Value(), p.InputLineCount(), p.promptScreenY, x, y)
}

// SetInputCursorOffset moves the textarea cursor to the given byte offset.
func (p *PromptModel) SetInputCursorOffset(offset int) {
	line, col := inputselection.CursorPosition(p.input.Value(), offset)
	p.input.MoveToBegin()
	for l := 0; l < line; l++ {
		p.input.CursorDown()
	}
	p.input.SetCursorColumn(col)
}

// SelectAll selects the entire input and moves the cursor to the end. It
// returns the selected text; writing the clipboard is the host's job.
func (p *PromptModel) SelectAll() (string, bool) {
	val := p.input.Value()
	if len(val) == 0 {
		return "", false
	}
	p.InputSelStart = 0
	p.InputSelEnd = len(val)
	p.SetInputCursorOffset(len(val))
	return val, true
}

// DeleteInputSelection deletes the selected substring and clears selection.
func (p *PromptModel) DeleteInputSelection() {
	if !p.HasInputSelection() {
		return
	}
	start, end := p.InputSelectionRange()
	if newVal, ok := inputselection.DeleteRange(p.input.Value(), start, end); ok {
		p.input.SetValue(newVal)
		p.SetInputCursorOffset(start)
	}
	p.ResetInputSelection()
}

// ReplaceInputSelection replaces the selected substring and clears selection.
func (p *PromptModel) ReplaceInputSelection(replacement string) {
	if !p.HasInputSelection() {
		return
	}
	start, end := p.InputSelectionRange()
	if newVal, ok := inputselection.ReplaceRange(p.input.Value(), start, end, replacement); ok {
		p.input.SetValue(newVal)
		p.SetInputCursorOffset(start + len(replacement))
	}
	p.ResetInputSelection()
}

// ApplyInputSelectionHighlight applies reverse-video styling to the selected
// range within the rendered input view.
func (p *PromptModel) ApplyInputSelectionHighlight(inputView string) string {
	start, end := p.InputSelectionRange()
	return inputselection.ApplyHighlight(inputView, p.input.Value(), start, end)
}

// selectAllInput selects all text and copies it to the clipboard. The
// clipboard write is OS I/O and stays on the host side of the seam.
func (m *RootModel) selectAllInput() (tea.Cmd, bool) {
	val, ok := m.prompt.SelectAll()
	if !ok {
		return nil, false
	}
	_ = clipboard.Write(val)
	return m.copySelectionCmd(val), true
}

// Root delegators. A host command or a view reaches the composer through
// the root; the method forwards to the model that owns the state.
func (m *RootModel) ComposerValue() string               { return m.prompt.ComposerValue() }
func (m *RootModel) SetComposerValue(val string)         { m.prompt.SetComposerValue(val) }
func (m *RootModel) IsComposerEmpty() bool               { return m.prompt.IsComposerEmpty() }
func (m *RootModel) ClearComposer()                      { m.prompt.ClearComposer() }
func (m *RootModel) HasPendingMedia() bool               { return m.prompt.HasPendingMedia() }
func (m *RootModel) AddPendingMedia(med clipboard.Media) { m.prompt.AddPendingMedia(med) }
func (m *RootModel) ClearPendingMedia()                  { m.prompt.ClearPendingMedia() }
func (m *RootModel) IsMediaExpanded() bool               { return m.prompt.IsMediaExpanded() }
func (m *RootModel) ToggleMediaExpanded()                { m.prompt.ToggleMediaExpanded() }
func (m *RootModel) ArmEsc(text string)                  { m.prompt.ArmEsc(text) }
func (m *RootModel) DisarmEsc()                          { m.prompt.DisarmEsc() }
func (m *RootModel) IsEscArmed() bool                    { return m.prompt.IsEscArmed() }
func (m *RootModel) VimActive() bool                     { return m.prompt.VimActive() }
func (m *RootModel) SetVimMode(on bool)                  { m.prompt.SetVimMode(on) }

// DismissPopups closes the slash-command and file-mention popups when they
// are visible. It reports whether one claimed the key.
func (p *PromptModel) DismissPopups() bool {
	if p.picker != nil && p.picker.Cancel() {
		return true
	}
	if p.atFilePicker != nil && p.atFilePicker.Visible() {
		p.atFilePicker.Reset()
		return true
	}
	return false
}

// CollapseMedia closes the expanded media preview. It reports whether the
// preview was open.
func (p *PromptModel) CollapseMedia() bool {
	if !p.mediaExpanded {
		return false
	}
	p.mediaExpanded = false
	return true
}
