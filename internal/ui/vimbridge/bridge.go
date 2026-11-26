// Package vimbridge owns the host half of the composer's Vim editing
// mode: the settings gate, the engine's seeding from the composer, the
// mirroring of the engine back onto the textarea, the key loop, the
// paste fold and the "/model <query>" completion's Enter semantics.
//
// The engine itself — the public vim package — is a pure state machine
// that knows nothing about bubbles or the run loop. What used to sit on
// the TUI's RootModel was everything around it: which settings turn it
// on, when it is re-seeded after the host rewrote the buffer behind its
// back, how a submit keeps a half-typed operator alive, and what the
// host has to do with a resolved model.
//
// The module is host-free in the same sense as the composer's selection
// module (internal/ui/editor/selection) and the voice session module
// (internal/ui/voicebridge): it never imports the TUI host, every side
// effect arrives as a narrow port, and every port is optional — a nil
// port reads as its zero value, so the rules can be driven by fakes in
// tests.
//
// # Ports
//
//   - InputEditor is the composer: the engine owns the buffer, the
//     textarea is only a display surface in Vim mode.
//   - PickerPort / AtFilePickerPort are the two completion popups,
//     which live only in Insert mode.
//   - KeybindingsPort answers the user's vim.insert.submit /
//     vim.normal.submit bindings from keybindings.json.
//   - SessionPort, OverlayPort, HistoryPort and PasteStore carry the
//     host state the bridge reads, resets or folds into.
//   - Host is the set of effects only the host can perform: submitting
//     the prompt, echoing a slash answer, switching the session's
//     model, cancelling a turn, focusing the Active Items block.
//
// # Reading the engine
//
// Engine exposes the live *vim.State so a host that already seeds or
// inspects it (a submit reset, a slash rewrite, the status row's badge)
// keeps working through one object instead of a second copy of the
// state. Everything that used to be a *RootModel method is a method
// here with the same name and semantics.
package vimbridge

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/features/tools/clipboard"
	"github.com/justwasm/pigasus/internal/ui/editor/keybind"
	"github.com/justwasm/pigasus/internal/ui/editor/vimkeys"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/internal/ui/views/modelargpopup"
	"github.com/justwasm/pigasus/vim"
)

// InputEditor is the composer port.
//
// The engine owns the buffer in Vim mode, so the bridge writes the
// engine's text and cursor back out after every key; LineCount,
// CursorUp and SetCursorColumn are what walking the textarea's cursor
// to the engine's row takes (SetValue leaves it at the end of the
// buffer). Reset clears the composer; InsertString is the paste path.
type InputEditor interface {
	Value() string
	SetValue(string)
	InsertString(string)
	Reset()
	LineCount() int
	CursorUp()
	SetCursorColumn(col int)
	Line() int
	Column() int
}

// PickerPort is the slash-command completion popup.
type PickerPort interface {
	Update(buffer []rune) bool
	Reset()
	IsModelMode() bool
	Query() string
	Selected() *ModelRow
}

// ModelRow is the highlighted popup row the bridge completes into the
// composer. The host's own row type (*slashcommands.Command) is
// converted by its adapter.
type ModelRow struct {
	Name        string
	Description string
}

// AtFilePickerPort is the "@" file-mention dropdown.
type AtFilePickerPort interface {
	Update() bool
	Reset()
}

// KeybindingsPort answers the user's binding for a key inside a scope
// ("vim.insert.", "vim.normal."). The host's *keybind.Keybindings
// satisfies it.
type KeybindingsPort interface {
	ActionForExactKeyInScope(key, scopePrefix string) string
}

// SessionPort is the session state a "/model" switch writes.
type SessionPort interface {
	Model() string
	SetModel(label string)
	SetStatusModel(label string)
	ModelWarning() string
}

// OverlayPort is the overlay state the bridge reads.
type OverlayPort interface {
	PanelKind() panelregistry.Kind
	ResumePickerActive() bool
	// ResumePickerTypeText routes a paste to the /resume search field
	// when that picker owns the keyboard. Returns true when consumed.
	ResumePickerTypeText(s string) bool
}

// HistoryPort is the prompt history the model completion resets.
type HistoryPort interface {
	ResetHistory()
}

// PasteStore owns the folded paste blocks: the placeholder that stands
// for a long paste in the composer, and the text the submit expands it
// back into. The host's composer sub-model satisfies it.
type PasteStore interface {
	// Fold records text under a fresh placeholder and returns it.
	Fold(text string, lines, chars int) string
}

// UndoRecorder records one composer snapshot before an edit.
type UndoRecorder interface {
	RecordBeforeChange(value string, cursor int, kind string)
}

// Host is what only the host can do. Every method is optional.
type Host interface {
	// SubmitPrompt sends the composer's content as the next turn.
	SubmitPrompt() tea.Cmd
	// FocusActiveItems moves the keyboard into the Active Items block
	// when it is showing running work. It reports whether it took the
	// key.
	FocusActiveItems() bool
	// ClearInputSelection drops a mouse selection in the composer.
	ClearInputSelection()
	// DeleteSelection removes the selected range from the composer. It
	// is what a paste replaces.
	DeleteSelection()
	// HasInputSelection reports whether a mouse selection is active.
	HasInputSelection() bool
	// HasActiveModal reports whether a modal overlay owns the keyboard.
	HasActiveModal() bool
	// ClearEligibilityNotice wipes the inline notice.
	ClearEligibilityNotice()
	// Respond echoes a slash command's answer into the transcript.
	Respond(cmd, msg string)
	// CancelTurn cancels the in-flight turn.
	CancelTurn()
	// SyncBackend re-resolves the turn backend for the new model.
	SyncBackend()
	// PersistModel writes the model id to the settings file.
	PersistModel(id string)
}

// RemoteModelLister is the attached backend's own catalogue, used when
// the "/model" argument names no reference model. It is separate from
// Host because only a session attached to a remote agent has one; a nil
// lister answers no models.
type RemoteModelLister interface {
	ListRemoteModels(ctx context.Context) []agentturn.ModelInfo
}

// CheckpointCounts is the trajectory bookkeeping a compact checkpoint
// is placed by: the calls recorded before the turn in flight, and the
// steps that turn has produced. It is separate from Host because it is
// conversation state, not an effect.
type CheckpointCounts interface {
	TrajectoryCalls() int
	LiveSteps() int
}

// Settings is the live settings reader. The host passes
// settings.Field; a nil reader leaves both settings off.
type Settings func(key string) (any, error)

// Ports are the host seams the bridge needs.
type Ports struct {
	Input       InputEditor
	Picker      PickerPort
	AtFile      AtFilePickerPort
	Keybindings KeybindingsPort
	Session     SessionPort
	Overlays    OverlayPort
	History     HistoryPort
	Pastes      PasteStore
	Undo        UndoRecorder
	Host        Host
	// Remote is the attached backend's catalogue. Optional.
	Remote RemoteModelLister
	// Counts is the trajectory bookkeeping. Optional.
	Counts   CheckpointCounts
	Settings Settings
}

// Bridge is the Vim editing mode's state: the engine plus the two
// settings that gate it. Its zero value is inert — every method answers
// the way a host with vim mode off does — so a host may hold one
// without constructing anything.
type Bridge struct {
	ports Ports

	eng      *vim.State
	vimOn    bool
	vimFirst bool
}

// New returns a bridge over ports.
func New(p Ports) *Bridge { return &Bridge{ports: p} }

// Ports returns the seams the bridge was built with.
func (b *Bridge) Ports() Ports { return b.ports }

// SetPorts installs the host seams. The host calls it when it wires the
// composer, so a bridge built inert (by the composer's mirror) starts
// reaching the host without losing its engine or settings.
func (b *Bridge) SetPorts(p Ports) {
	if b == nil {
		return
	}
	b.ports = p
}

// Enabled reports whether "Editor Mode" selects vim. The value is read
// from settings.json at TUI start-up (and again whenever a slash panel
// closes, so toggling Editor Mode in /config applies without a
// restart); it is kept in a field rather than re-read per keystroke so
// the tests stay hermetic and the render path does no file I/O.
func (b *Bridge) Enabled() bool { return b != nil && b.vimOn }

// InsertFirstEnabled reports the "Editor Mode › Insert First" child
// setting: when on, the prompt opens in Insert mode and a bare Enter
// submits instead of inserting a newline.
func (b *Bridge) InsertFirstEnabled() bool { return b != nil && b.vimFirst }

// SetEnabled turns vim mode on or off without touching the settings
// file. The host uses it where it already knows the answer; ReloadSettings
// is the settings-file path.
func (b *Bridge) SetEnabled(on bool) {
	if b == nil {
		return
	}
	b.vimOn = on
	if !on {
		// The host keeps its "no engine without vim" invariant: the
		// badge is empty and no engine is built until vim is on again.
		b.eng = nil
	}
}

// rawString renders a settings value the way the config panel does.
func rawString(raw any) string {
	if s, ok := raw.(string); ok {
		return s
	}
	return fmt.Sprint(raw)
}

// ReloadSettings reads the two vim settings from the live settings
// file. It is called once at TUI start-up and again when a slash panel
// closes (/config can change both).
func (b *Bridge) ReloadSettings() {
	if b == nil {
		return
	}
	b.vimOn = false
	if b.ports.Settings != nil {
		if raw, err := b.ports.Settings("editorMode"); err == nil {
			b.vimOn = rawString(raw) == "vim"
		}
	}
	b.vimFirst = false
	if b.ports.Settings != nil {
		if raw, err := b.ports.Settings("vimInsertFirst"); err == nil {
			switch v := raw.(type) {
			case bool:
				b.vimFirst = v
			case string:
				b.vimFirst = v == "on" || v == "true"
			}
		}
	}
}

// Engine returns the engine, building it from the current prompt on
// first use. Whenever the host has replaced the buffer behind the
// engine's back — a submit reset, a history recall, the picker's Tab
// substitution, a slash rewrite — the engine is re-seeded so it always
// tracks what is actually on screen.
//
// It is nil while vim mode is off: the host keeps its "no engine
// without vim" invariant, and a host that never enables vim never
// allocates one.
func (b *Bridge) Engine() *vim.State {
	if b == nil || !b.vimOn {
		return nil
	}
	text := ""
	if b.ports.Input != nil {
		text = b.ports.Input.Value()
	}
	if b.eng == nil {
		b.eng = vim.New(text)
	} else if b.eng.Text() != text {
		b.eng.SetText(text)
	}
	// SetInsertFirst is a no-op when the setting is unchanged, so this
	// does not yank the user back into Insert mode on every keystroke.
	b.eng.SetInsertFirst(b.vimFirst)
	return b.eng
}

// AdoptEngine installs an engine the host already holds, so a host that
// stashed one on its composer sub-model shares it instead of building a
// second. A nil engine is ignored.
func (b *Bridge) AdoptEngine(eng *vim.State) {
	if b == nil || eng == nil {
		return
	}
	b.eng = eng
}

// Mode reports the mode the status row's badge shows.
//
// The engine is seeded the way the host seeds it for a key press, so
// the badge agrees with the mode the next key will be handled in. It is
// not built when vim mode is off: the badge is empty then, and the host
// keeps its "no engine without vim" invariant.
func (b *Bridge) Mode() vim.Mode {
	if !b.Enabled() {
		return vim.Normal
	}
	return b.Engine().Mode
}

// SyncInput mirrors the engine onto the textarea. The textarea is only
// a display surface in vim mode; the engine owns the buffer and cursor.
//
// SetValue leaves the textarea cursor at the end of the buffer, so the
// row is walked back up before the column is set. A column past the end
// of the line is legal in Insert mode (the append point), and the
// textarea clamps it itself.
func (b *Bridge) SyncInput() {
	if b == nil || b.eng == nil || b.ports.Input == nil {
		return
	}
	eng := b.eng
	in := b.ports.Input
	in.SetValue(eng.Text())
	for r := in.LineCount() - 1; r > eng.Row; r-- {
		in.CursorUp()
	}
	in.SetCursorColumn(eng.Col)

	insert := eng.Mode == vim.Insert
	if b.ports.Picker != nil {
		if insert {
			b.ports.Picker.Update([]rune(tuilib.FirstLine(in.Value())))
		} else {
			b.ports.Picker.Reset()
		}
	}
	if b.ports.AtFile != nil {
		if insert {
			b.ports.AtFile.Update()
		} else {
			b.ports.AtFile.Reset()
		}
	}
}

// Submit runs the host's submit and re-seeds the engine from the
// composer the submit cleared.
//
// 【一手】 agy 1.4.9, the vendor's own release notes: "a half-typed
// operator such as `d` carrying over after you submit a prompt". The
// submit clears the composer, so the engine is re-seeded from it — with
// the half-typed operator intact (SetTextKeepingPending).
func (b *Bridge) Submit() tea.Cmd {
	if b == nil || b.ports.Host == nil {
		return nil
	}
	b.SyncInput()
	cmd := b.ports.Host.SubmitPrompt()
	if b.eng != nil && b.ports.Input != nil {
		b.eng.SetTextKeepingPending(b.ports.Input.Value())
	}
	b.SyncInput()
	return cmd
}

// HandleKey feeds one key to the engine. handled is false when the
// engine declined the key, which is the signal for the caller to fall
// through to the host's own binding (ctrl+c, ctrl+d, ctrl+o, Tab, …).
//
// The order matters: the special codes are checked first because a
// driver may attach text to them too (Enter arrives with Text "\n" in
// some terminals, and in the clone's own synthetic key events). Only
// then is the literal text consumed — a paste can deliver several runes
// in one event, so each is fed through the engine in order.
func (b *Bridge) HandleKey(key tea.Key) (tea.Cmd, bool) {
	if b == nil {
		return nil, false
	}
	eng := b.Engine()
	if eng == nil {
		return nil, false
	}

	// When on the last line of the buffer, Down arrow or 'j' (in
	// Normal mode) switches focus into Active Items if any are running,
	// matching PromptModel.Update (0x102c91d60).
	if (eng.Mode == vim.Normal && eng.Pending == "") || eng.Mode == vim.Insert {
		isDown := key.Code == tea.KeyDown
		if eng.Mode == vim.Normal {
			isDown = isDown || ((key.Code == 'j' || key.Text == "j") && key.Mod == 0)
		}
		if isDown && eng.Row >= len(eng.Lines)-1 {
			if b.ports.Host != nil && b.ports.Host.FocusActiveItems() {
				return nil, true
			}
		}
	}

	// Custom Vim submit bindings from keybindings.json.
	if b.ports.Keybindings != nil {
		ks := keybind.KeyString(key)
		if ks != "" {
			scope := ""
			switch eng.Mode {
			case vim.Insert:
				scope = "vim.insert."
			case vim.Normal:
				scope = "vim.normal."
			}
			if scope != "" && b.ports.Keybindings.ActionForExactKeyInScope(ks, scope) == scope+"submit" {
				return b.Submit(), true
			}
		}
	}

	if key.Code == tea.KeyEnter && eng.Mode == vim.Insert {
		line := strings.TrimSpace(eng.Text())
		if (strings.HasPrefix(line, "/") || strings.HasPrefix(line, "!")) && !strings.Contains(line, "\n") {
			return b.Submit(), true
		}
	}

	var res vim.Result
	handled := false

	if nk := vimkeys.Named(key); nk != (vim.Key{}) {
		res = eng.Handle(nk)
		handled = res.Consumed
	} else if key.Text != "" && key.Mod&(tea.ModCtrl|tea.ModAlt) == 0 {
		for _, r := range key.Text {
			res = eng.Handle(vim.Key{Rune: r})
			if res.Consumed {
				handled = true
			}
			if res.Submit {
				break
			}
		}
	} else {
		vk, ok := vimkeys.Rune(key)
		if !ok {
			return nil, false
		}
		res = eng.Handle(vk)
		handled = res.Consumed
	}

	if res.Submit {
		return b.Submit(), true
	}
	if !handled {
		return nil, false
	}
	b.SyncInput()
	return nil, true
}

// FoldLines and FoldChars are the paste sizes that fold into a
// placeholder rather than landing in the composer verbatim.
const (
	FoldLines = 16
	FoldChars = 1000
)

// InsertPastedText is the single insertion point for both the
// terminal's bracketed paste and ctrl+v. It never submits.
//
// A paste at or above the folding threshold (>= 16 lines or > 1000
// characters) becomes "[Pasted text #N +M lines]" or
// "[Pasted text #N M chars]", with the text itself kept in the paste
// store for the submit to expand.
func (b *Bridge) InsertPastedText(s string) tea.Cmd {
	if b == nil {
		return nil
	}
	s = clipboard.NormalizeText(s)
	if s == "" {
		return nil
	}
	if b.ports.Host != nil && b.ports.Host.HasActiveModal() {
		// A modal — a slash panel, the /help overlay — owns the keyboard,
		// so the composer's paste does not reach it. The resume picker is
		// the exception: it wants the paste as search text.
		if b.ports.Overlays != nil && b.ports.Overlays.ResumePickerActive() {
			if b.ports.Overlays.ResumePickerTypeText(s) {
				return nil
			}
		}
		return nil
	}
	if b.ports.Input == nil {
		return nil
	}
	if b.ports.Host != nil && b.ports.Host.HasInputSelection() {
		b.ports.Host.DeleteSelection()
	}
	if b.ports.Undo != nil {
		b.ports.Undo.RecordBeforeChange(b.ports.Input.Value(), cursorOffset(b.ports.Input), "paste")
	}

	toInsert := s
	lines := strings.Count(s, "\n") + 1
	chars := len([]rune(s))
	if lines >= FoldLines || chars > FoldChars {
		if b.ports.Pastes == nil {
			// Without a store the paste lands verbatim: a host that
			// folds nothing must not lose the text.
			toInsert = s
		} else {
			toInsert = b.ports.Pastes.Fold(s, lines, chars)
		}
	}
	b.ports.Input.InsertString(toInsert)
	if b.ports.Picker != nil {
		b.ports.Picker.Update([]rune(tuilib.FirstLine(b.ports.Input.Value())))
	}
	if b.ports.AtFile != nil {
		b.ports.AtFile.Update()
	}
	if b.ports.Host != nil {
		b.ports.Host.ClearEligibilityNotice()
	}
	return nil
}

// ApplyModelArgument makes the model the argument names the session's
// model and echoes the reference's confirmation.
//
// 【一手】 agy 1.4.6, live: "/model Claude Sonnet 4.6 (Thinking)"
// answers "Model set to Claude Sonnet 4.6 (Thinking)" and the status row
// follows; naming the model that is already selected answers "Model
// already set to <label>".
func (b *Bridge) ApplyModelArgument(arg string) tea.Cmd {
	if b == nil {
		return nil
	}
	entry, ok := modelargpopup.Resolve(arg)
	if !ok {
		if mdl, ok := b.resolveRemoteModel(arg); ok {
			return b.setModel(mdl.ID, mdl.DisplayName, "")
		}
		b.respond("/model", modelargpopup.InvalidMessage(arg))
		return nil
	}
	return b.setModel(entry.ID, entry.Label, "warning: unconfigured, falling back to mock shim")
}

// resolveRemoteModel looks the argument up in the attached backend's own
// catalogue, which is the only catalogue a remote agent has.
func (b *Bridge) resolveRemoteModel(arg string) (agentturn.ModelInfo, bool) {
	if b.ports.Remote == nil {
		return agentturn.ModelInfo{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	models := b.ports.Remote.ListRemoteModels(ctx)
	cancel()
	argLower := strings.ToLower(strings.TrimSpace(arg))
	for _, mdl := range models {
		if strings.ToLower(mdl.ID) == argLower || strings.ToLower(mdl.DisplayName) == argLower {
			return mdl, true
		}
	}
	return agentturn.ModelInfo{}, false
}

// setModel is the "/model" switch every path ends in: cancel the turn,
// persist the id, move the session and its status row, re-resolve the
// backend, then echo the reference's answer. warning is appended to the
// answer only when the session reports no warning of its own.
func (b *Bridge) setModel(id, label, warning string) tea.Cmd {
	if b.ports.Session == nil {
		return nil
	}
	if b.ports.Session.Model() == label {
		b.respond("/model", "Model already set to "+label)
		return nil
	}
	if b.ports.Host != nil {
		b.ports.Host.CancelTurn()
		b.ports.Host.PersistModel(id)
	}
	b.ports.Session.SetModel(label)
	b.ports.Session.SetStatusModel(tuilib.StripTier(label))
	if b.ports.Host != nil {
		b.ports.Host.SyncBackend()
	}
	if w := b.ports.Session.ModelWarning(); w != "" {
		warning = w
	}
	if warning != "" {
		b.respond("/model", fmt.Sprintf("Model set to %s (warning: %s)", label, warning))
		return nil
	}
	b.respond("/model", "Model set to "+label)
	return nil
}

// SubmitModelCompletion runs the Enter semantics of the "/model <query>"
// popup. It reports whether it consumed the key.
func (b *Bridge) SubmitModelCompletion() (bool, tea.Cmd) {
	if b == nil || b.ports.Picker == nil || !b.ports.Picker.IsModelMode() {
		return false, nil
	}
	arg := strings.TrimSpace(b.ports.Picker.Query())
	if entry, ok := modelargpopup.Resolve(arg); ok {
		b.resetComposer()
		if b.ports.Session != nil && b.ports.Session.Model() == entry.Label {
			b.respond("/model", "Model already set to "+entry.Label)
			return true, nil
		}
		if b.ports.Host != nil {
			b.ports.Host.CancelTurn()
			b.ports.Host.PersistModel(entry.ID)
		}
		if b.ports.Session != nil {
			b.ports.Session.SetModel(entry.Label)
			b.ports.Session.SetStatusModel(tuilib.StripTier(entry.Label))
		}
		if b.ports.Host != nil {
			b.ports.Host.SyncBackend()
		}
		b.respond("/model", "Model set to "+entry.Label)
		return true, nil
	}
	// The argument names no model: the highlighted row is completed into
	// the composer instead of running the command.
	if sel := b.ports.Picker.Selected(); sel != nil {
		target := sel.Description
		if target == "" {
			target = sel.Name
		}
		b.completeInto("/model " + target)
		return true, nil
	}
	// Nothing matches at all: the command runs and reports the argument.
	b.resetComposer()
	b.respond("/model", modelargpopup.InvalidMessage(arg))
	return true, nil
}

// CheckpointInsertionPoint is the call count a compact checkpoint is
// inserted at: the calls before the turn plus the turn's live steps,
// keeping the recent tail the compaction retained.
func (b *Bridge) CheckpointInsertionPoint(keepRecent int) int {
	if b == nil || b.ports.Counts == nil {
		return 0
	}
	after := b.ports.Counts.TrajectoryCalls() + b.ports.Counts.LiveSteps() - keepRecent
	if after < 0 {
		return 0
	}
	return after
}

// completeInto writes text into the composer and closes the popup.
func (b *Bridge) completeInto(text string) {
	if b.ports.Input != nil {
		b.ports.Input.SetValue(text)
	}
	if b.ports.Picker != nil {
		b.ports.Picker.Reset()
	}
	b.resetHistory()
}

// resetComposer empties the composer, the popup and the history.
func (b *Bridge) resetComposer() {
	if b.ports.Input != nil {
		b.ports.Input.Reset()
	}
	if b.ports.Host != nil {
		b.ports.Host.ClearInputSelection()
	}
	if b.ports.Picker != nil {
		b.ports.Picker.Reset()
	}
	b.resetHistory()
}

func (b *Bridge) resetHistory() {
	if b.ports.History != nil {
		b.ports.History.ResetHistory()
	}
}

func (b *Bridge) respond(cmd, msg string) {
	if b.ports.Host != nil {
		b.ports.Host.Respond(cmd, msg)
	}
}

// cursorOffset is the textarea cursor's byte offset in its own value.
// The host's editor and tuilib.InputCursorByteOffset compute the same
// number; it is repeated here so the module stays host-free.
func cursorOffset(in InputEditor) int {
	value := in.Value()
	line, col := in.Line(), in.Column()
	rowStart, rowsWalked := 0, 0
	for i := 0; i < len(value); i++ {
		if rowsWalked == line {
			rowStart = i
			break
		}
		if value[i] == '\n' {
			rowsWalked++
		}
	}
	offset := rowStart + col
	if offset > len(value) {
		offset = len(value)
	}
	return offset
}
