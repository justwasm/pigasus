package vimbridge

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/vim"
)

// scenarioEditor is the composer port. It behaves like the host's
// textarea in the two ways the bridge depends on: SetValue leaves the
// cursor at the end of the buffer, and the cursor is a (row, col) pair
// the bridge walks back up.
type scenarioEditor struct {
	value string
	row   int
	col   int
}

func (e *scenarioEditor) Value() string { return e.value }

func (e *scenarioEditor) SetValue(v string) {
	e.value = v
	e.row, e.col = 0, 0
	for i := 0; i < len(v); i++ {
		if v[i] == '\n' {
			e.row++
			e.col = 0
			continue
		}
		e.col++
	}
}

func (e *scenarioEditor) InsertString(s string) {
	off := e.offset()
	e.value = e.value[:off] + s + e.value[off:]
	e.SetValue(e.value)
	// InsertString in the real textarea leaves the cursor after the
	// inserted text.
	e.col += len(s)
}

func (e *scenarioEditor) Reset() { e.SetValue("") }

func (e *scenarioEditor) LineCount() int { return strings.Count(e.value, "\n") + 1 }

func (e *scenarioEditor) CursorUp() {
	if e.row > 0 {
		e.row--
	}
}

func (e *scenarioEditor) SetCursorColumn(col int) { e.col = col }
func (e *scenarioEditor) Line() int               { return e.row }
func (e *scenarioEditor) Column() int             { return e.col }

// offset is the cursor's byte offset in the value.
func (e *scenarioEditor) offset() int {
	if e.row <= 0 {
		return min(e.col, len(e.value))
	}
	line := 0
	for i := 0; i < len(e.value); i++ {
		if e.value[i] == '\n' {
			line++
			if line == e.row {
				return min(i+1+e.col, len(e.value))
			}
		}
	}
	return len(e.value)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// scenarioPicker is the slash popup: it records the buffer it was last
// handed and how often it was reset, and answers the model-mode
// questions the scenario set.
type scenarioPicker struct {
	buffer    string
	updates   int
	resets    int
	modelMode bool
	query     string
	sel       *ModelRow
}

func (p *scenarioPicker) Update(buffer []rune) bool {
	p.updates++
	p.buffer = string(buffer)
	return true
}
func (p *scenarioPicker) Reset()            { p.resets++ }
func (p *scenarioPicker) IsModelMode() bool { return p.modelMode }
func (p *scenarioPicker) Query() string     { return p.query }
func (p *scenarioPicker) Selected() *ModelRow {
	return p.sel
}

// scenarioAtFile is the "@" mention popup.
type scenarioAtFile struct {
	updates int
	resets  int
}

func (p *scenarioAtFile) Update() bool { p.updates++; return true }
func (p *scenarioAtFile) Reset()       { p.resets++ }

// scenarioKeys is keybindings.json: one action per scope.
type scenarioKeys struct{ submit map[string]string }

func (k scenarioKeys) ActionForExactKeyInScope(key, scope string) string {
	return k.submit[scope+key]
}

// scenarioSession is the session state a "/model" switch writes.
type scenarioSession struct {
	model, status, warning string
}

func (s *scenarioSession) Model() string           { return s.model }
func (s *scenarioSession) SetModel(label string)   { s.model = label }
func (s *scenarioSession) SetStatusModel(l string) { s.status = l }
func (s *scenarioSession) ModelWarning() string    { return s.warning }

// scenarioOverlays is the overlay state.
type scenarioOverlays struct {
	kind   panelregistry.Kind
	resume bool
}

func (o scenarioOverlays) PanelKind() panelregistry.Kind { return o.kind }
func (o scenarioOverlays) ResumePickerActive() bool      { return o.resume }

// scenarioHistory is the prompt history.
type scenarioHistory struct{ resets int }

func (h *scenarioHistory) ResetHistory() { h.resets++ }

// scenarioPastes is the folded paste store: it numbers the blocks the
// way the reference does.
type scenarioPastes struct {
	blocks map[string]string
	next   int
}

func (p *scenarioPastes) Fold(text string, lines, chars int) string {
	if p.blocks == nil {
		p.blocks = make(map[string]string)
	}
	p.next++
	var placeholder string
	if lines >= FoldLines {
		placeholder = "[Pasted text #" + itoa(p.next) + " +" + itoa(lines) + " lines]"
	} else {
		placeholder = "[Pasted text #" + itoa(p.next) + " " + itoa(chars) + " chars]"
	}
	p.blocks[placeholder] = text
	return placeholder
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// scenarioUndo records the snapshots the bridge asked for.
type scenarioUndo struct {
	values  []string
	kinds   []string
	cursors []int
}

func (u *scenarioUndo) RecordBeforeChange(value string, cursor int, kind string) {
	u.values = append(u.values, value)
	u.cursors = append(u.cursors, cursor)
	u.kinds = append(u.kinds, kind)
}

// scenarioHost is the host's own effects.
type scenarioHost struct {
	submits      int
	focus        bool
	focusCalls   int
	selection    bool
	modal        bool
	clears       int
	deletes      int
	notices      int
	responses    []string
	cancels      int
	syncs        int
	persisted    []string
	remoteModels []agentturn.ModelInfo
	calls, steps int
}

func (h *scenarioHost) SubmitPrompt() tea.Cmd { h.submits++; return nil }
func (h *scenarioHost) FocusActiveItems() bool {
	h.focusCalls++
	return h.focus
}
func (h *scenarioHost) ClearInputSelection()    { h.clears++ }
func (h *scenarioHost) HasActiveModal() bool    { return h.modal }
func (h *scenarioHost) DeleteSelection()        { h.deletes++ }
func (h *scenarioHost) HasInputSelection() bool { return h.selection }
func (h *scenarioHost) ClearEligibilityNotice() { h.notices++ }
func (h *scenarioHost) Respond(cmd, msg string) {
	h.responses = append(h.responses, cmd+" "+msg)
}
func (h *scenarioHost) CancelTurn()            { h.cancels++ }
func (h *scenarioHost) SyncBackend()           { h.syncs++ }
func (h *scenarioHost) PersistModel(id string) { h.persisted = append(h.persisted, id) }

type scenarioRemote struct{ models []agentturn.ModelInfo }

func (r scenarioRemote) ListRemoteModels(ctx context.Context) []agentturn.ModelInfo {
	return r.models
}

type scenarioCounts struct{ calls, steps int }

func (c scenarioCounts) TrajectoryCalls() int { return c.calls }
func (c scenarioCounts) LiveSteps() int       { return c.steps }

// harness is one scenario's parts.
type harness struct {
	bridge  *Bridge
	editor  *scenarioEditor
	picker  *scenarioPicker
	atFile  *scenarioAtFile
	session *scenarioSession
	history *scenarioHistory
	pastes  *scenarioPastes
	undo    *scenarioUndo
	host    *scenarioHost
	keys    *scenarioKeys
}

// newHarness builds a bridge with vim mode on and Insert First off.
func newHarness() *harness {
	h := &harness{
		editor:  &scenarioEditor{},
		picker:  &scenarioPicker{},
		atFile:  &scenarioAtFile{},
		session: &scenarioSession{model: "Gemini 3.8 Flash (High)"},
		history: &scenarioHistory{},
		pastes:  &scenarioPastes{},
		undo:    &scenarioUndo{},
		host:    &scenarioHost{},
	}
	h.bridge = New(Ports{
		Input:    h.editor,
		Picker:   h.picker,
		AtFile:   h.atFile,
		Session:  h.session,
		History:  h.history,
		Pastes:   h.pastes,
		Undo:     h.undo,
		Host:     h.host,
		Settings: staticSettings{"editorMode": "vim"}.Field,
	})
	h.bridge.ReloadSettings()
	return h
}

// staticSettings is a settings file with fixed values.
type staticSettings map[string]any

func (s staticSettings) Field(key string) (any, error) { return s[key], nil }

func runeKey(r rune) tea.Key { return tea.Key{Code: r, Text: string(r)} }
func escKey() tea.Key        { return tea.Key{Code: tea.KeyEsc} }
func enterKey() tea.Key      { return tea.Key{Code: tea.KeyEnter} }
func ctrlKey(r rune) tea.Key { return tea.Key{Code: r, Mod: tea.ModCtrl} }
func downKey() tea.Key       { return tea.Key{Code: tea.KeyDown} }

// --- settings gate ---------------------------------------------------

func TestSettingsGate(t *testing.T) {
	h := newHarness()
	if !h.bridge.Enabled() {
		t.Fatal("editorMode=vim must enable the bridge")
	}
	if h.bridge.InsertFirstEnabled() {
		t.Fatal("vimInsertFirst unset must be off")
	}
	if got := h.bridge.Mode(); got != vim.Normal {
		t.Fatalf("mode = %v, want Normal", got)
	}

	off := New(Ports{Settings: staticSettings{}.Field})
	off.ReloadSettings()
	if off.Enabled() {
		t.Fatal("an empty settings file must leave vim off")
	}
	if off.Engine() != nil {
		t.Fatal("the engine must not be built when vim is off")
	}
	if got := off.Mode(); got != vim.Normal {
		t.Fatalf("badge mode with vim off = %v, want Normal", got)
	}
	// A host that turns vim off drops the engine: the badge is empty and
	// no engine is built until vim is on again.
	off.SetEnabled(false)
	if off.Enabled() {
		t.Fatal("SetEnabled(false) must turn vim off")
	}

	first := New(Ports{Settings: staticSettings{"editorMode": "vim", "vimInsertFirst": "on"}.Field})
	first.ReloadSettings()
	if !first.InsertFirstEnabled() {
		t.Fatal(`vimInsertFirst="on" must enable Insert First`)
	}
	if got := first.Mode(); got != vim.Insert {
		t.Fatalf("Insert First must open in Insert, got %v", got)
	}
}

// --- modes and the key loop ------------------------------------------

func TestNormalInsertVisualTransitions(t *testing.T) {
	h := newHarness()
	// "a" opens Insert; the rest is text.
	feed(t, h, "abc")
	if got := h.editor.Value(); got != "bc" {
		t.Fatalf("composer = %q, want bc", got)
	}
	if got := h.bridge.Mode(); got != vim.Insert {
		t.Fatalf("mode = %v, want Insert", got)
	}
	// Esc returns to Normal without touching the buffer.
	if _, handled := h.bridge.HandleKey(escKey()); !handled {
		t.Fatal("Esc must be consumed in Insert mode")
	}
	if got := h.bridge.Mode(); got != vim.Normal {
		t.Fatalf("mode after Esc = %v, want Normal", got)
	}
	if got := h.editor.Value(); got != "bc" {
		t.Fatalf("composer after Esc = %q, want bc", got)
	}
	// "v" enters Visual, Esc leaves it.
	feed(t, h, "v")
	if got := h.bridge.Mode(); got != vim.Visual {
		t.Fatalf("mode after v = %v, want Visual", got)
	}
	h.bridge.HandleKey(escKey())
	if got := h.bridge.Mode(); got != vim.Normal {
		t.Fatalf("mode after Esc = %v, want Normal", got)
	}
	// "V" enters Visual Line.
	feed(t, h, "V")
	if got := h.bridge.Mode(); got != vim.VisualLine {
		t.Fatalf("mode after V = %v, want VisualLine", got)
	}
}

func TestDeclinedKeyFallsThroughToHost(t *testing.T) {
	h := newHarness()
	// ctrl+c is not the engine's: the host keeps it.
	if _, handled := h.bridge.HandleKey(ctrlKey('c')); handled {
		t.Fatal("ctrl+c must be declined so the host can bind it")
	}
}

func TestCursorMirrorsEngine(t *testing.T) {
	h := newHarness()
	feed(t, h, "ihello world")
	h.bridge.HandleKey(escKey())
	feed(t, h, "0") // start of line
	feed(t, h, "w") // onto "world"
	if got := h.bridge.Engine().Col; got != 6 {
		t.Fatalf("engine col = %d, want 6", got)
	}
	if h.editor.row != 0 || h.editor.col != 6 {
		t.Fatalf("composer cursor = (%d,%d), want (0,6)", h.editor.row, h.editor.col)
	}
}

func TestEngineReseedsAfterHostRewrite(t *testing.T) {
	h := newHarness()
	feed(t, h, "iabc")
	// The host replaces the buffer behind the engine's back.
	h.editor.SetValue("zzz")
	if got := h.bridge.Engine().Text(); got != "zzz" {
		t.Fatalf("engine text = %q, want zzz", got)
	}
}

func TestMultiLineCursorWalksUp(t *testing.T) {
	h := newHarness()
	h.editor.SetValue("first\nsecond")
	h.bridge.Engine().Row = 0
	h.bridge.Engine().Col = 2
	h.bridge.SyncInput()
	if h.editor.row != 0 || h.editor.col != 2 {
		t.Fatalf("composer cursor = (%d,%d), want (0,2)", h.editor.row, h.editor.col)
	}
}

// --- submit ----------------------------------------------------------

func TestPendingOperatorCarriesOverSubmit(t *testing.T) {
	h := newHarness()
	feed(t, h, "iabc")
	h.bridge.HandleKey(escKey())
	feed(t, h, "d") // half-typed operator
	if got := h.bridge.Engine().Pending; got != "d" {
		t.Fatalf("pending = %q, want d", got)
	}
	h.bridge.HandleKey(ctrlKey('s'))
	if h.host.submits != 1 {
		t.Fatalf("submits = %d, want 1", h.host.submits)
	}
	if got := h.bridge.Engine().Pending; got != "d" {
		t.Fatalf("pending after submit = %q, want d", got)
	}
}

func TestKeybindingsSubmitBinding(t *testing.T) {
	h := newHarness()
	h.keys = &scenarioKeys{submit: map[string]string{"vim.insert.ctrl+s": "vim.insert.submit"}}
	h.bridge = New(Ports{
		Input: h.editor, Picker: h.picker, AtFile: h.atFile, Session: h.session,
		History: h.history, Pastes: h.pastes, Undo: h.undo, Host: h.host,
		Keybindings: h.keys, Settings: staticSettings{"editorMode": "vim"}.Field,
	})
	h.bridge.ReloadSettings()
	feed(t, h, "ihi")
	if _, handled := h.bridge.HandleKey(ctrlKey('s')); !handled {
		t.Fatal("ctrl+s must be handled by the binding")
	}
	if h.host.submits != 1 {
		t.Fatalf("submits = %d, want 1", h.host.submits)
	}
}

func TestSlashLineSubmitsFromInsert(t *testing.T) {
	h := newHarness()
	feed(t, h, "i/doctor")
	if _, handled := h.bridge.HandleKey(enterKey()); !handled {
		t.Fatal("Enter on a slash line must be handled")
	}
	if h.host.submits != 1 {
		t.Fatalf("submits = %d, want 1", h.host.submits)
	}
}

// --- active items ----------------------------------------------------

func TestDownOnLastLineFocusesActiveItems(t *testing.T) {
	h := newHarness()
	h.host.focus = true
	if _, handled := h.bridge.HandleKey(downKey()); !handled {
		t.Fatal("Down on the last line must be handled")
	}
	if h.host.focusCalls != 1 {
		t.Fatalf("focus calls = %d, want 1", h.host.focusCalls)
	}
	// With nothing running the host is asked and declines; the engine
	// then handles Down as its own motion.
	lonely := newHarness()
	if _, handled := lonely.bridge.HandleKey(downKey()); !handled {
		t.Fatal("Down with no active items must fall through to the engine")
	}
	if lonely.host.focusCalls != 1 {
		t.Fatalf("focus calls = %d, want 1 (asked, declined)", lonely.host.focusCalls)
	}
}

// --- paste -----------------------------------------------------------

func TestPasteInsertsAndNotifiesPickers(t *testing.T) {
	h := newHarness()
	h.bridge.InsertPastedText("hello")
	if got := h.editor.Value(); got != "hello" {
		t.Fatalf("composer = %q, want hello", got)
	}
	if h.picker.updates == 0 || h.atFile.updates == 0 {
		t.Fatalf("pickers not notified: picker=%d atFile=%d", h.picker.updates, h.atFile.updates)
	}
	if len(h.undo.values) != 1 || h.undo.kinds[0] != "paste" {
		t.Fatalf("undo = %v %v, want one paste snapshot", h.undo.values, h.undo.kinds)
	}
	if h.host.notices != 1 {
		t.Fatalf("notices cleared = %d, want 1", h.host.notices)
	}
}

func TestLongPasteFoldsIntoPlaceholder(t *testing.T) {
	h := newHarness()
	long := strings.Repeat("line\n", 15) + "line"
	h.bridge.InsertPastedText(long)
	if got := h.editor.Value(); got != "[Pasted text #1 +16 lines]" {
		t.Fatalf("composer = %q, want the placeholder", got)
	}
	if h.pastes.blocks["[Pasted text #1 +16 lines]"] != long {
		t.Fatal("the store must keep the full text under the placeholder")
	}

	wide := newHarness()
	wide.bridge.InsertPastedText(strings.Repeat("x", 1001))
	if got := wide.editor.Value(); got != "[Pasted text #1 1001 chars]" {
		t.Fatalf("composer = %q, want the chars placeholder", got)
	}
}

func TestEmptyPasteIsIgnored(t *testing.T) {
	h := newHarness()
	h.bridge.InsertPastedText("")
	if h.editor.Value() != "" || len(h.undo.values) != 0 {
		t.Fatal("an empty paste must be a no-op")
	}
}

func TestPasteIntoModalIsDeclined(t *testing.T) {
	h := newHarness()
	h.host.modal = true
	h.bridge.InsertPastedText("text")
	if h.editor.Value() != "" {
		t.Fatalf("composer = %q, want empty: the modal owns the paste", h.editor.Value())
	}
}

// --- /model completion ----------------------------------------------

func TestApplyModelArgumentResolves(t *testing.T) {
	h := newHarness()
	h.bridge.ApplyModelArgument("Claude Sonnet 4.6 (Thinking)")
	if h.session.model != "Claude Sonnet 4.6 (Thinking)" {
		t.Fatalf("model = %q", h.session.model)
	}
	// "(Thinking)" is part of the model's name, not an effort tier, so
	// the status row keeps it — the reference does exactly this.
	if h.session.status != "Claude Sonnet 4.6 (Thinking)" {
		t.Fatalf("status = %q, want the label verbatim", h.session.status)
	}
	if len(h.host.persisted) != 1 || h.host.persisted[0] != "claude-sonnet-4-6" {
		t.Fatalf("persisted = %v", h.host.persisted)
	}
	if h.host.cancels != 1 || h.host.syncs != 1 {
		t.Fatalf("cancels=%d syncs=%d, want 1/1", h.host.cancels, h.host.syncs)
	}
	if len(h.host.responses) != 1 ||
		!strings.Contains(h.host.responses[0], "Model set to Claude Sonnet 4.6 (Thinking)") {
		t.Fatalf("responses = %v", h.host.responses)
	}
}

func TestApplyModelArgumentAlreadySelected(t *testing.T) {
	h := newHarness()
	h.session.model = "Gemini 3.8 Flash (High)"
	h.bridge.ApplyModelArgument("Gemini 3.8 Flash (High)")
	if len(h.host.responses) != 1 ||
		!strings.Contains(h.host.responses[0], "Model already set to Gemini 3.8 Flash (High)") {
		t.Fatalf("responses = %v", h.host.responses)
	}
	if h.host.cancels != 0 || len(h.host.persisted) != 0 {
		t.Fatal("an already-selected model must not cancel or persist")
	}
}

func TestApplyModelArgumentInvalid(t *testing.T) {
	h := newHarness()
	h.bridge.ApplyModelArgument("zzz")
	if len(h.host.responses) != 1 || !strings.Contains(h.host.responses[0], `Invalid model "zzz"`) {
		t.Fatalf("responses = %v", h.host.responses)
	}
}

func TestApplyModelArgumentRemoteCatalogue(t *testing.T) {
	h := newHarness()
	h.bridge = New(Ports{
		Input: h.editor, Session: h.session, Host: h.host,
		Remote:   scenarioRemote{models: []agentturn.ModelInfo{{ID: "acme-1", DisplayName: "Acme One"}}},
		Settings: staticSettings{"editorMode": "vim"}.Field,
	})
	h.bridge.ApplyModelArgument("acme one")
	if h.session.model != "Acme One" {
		t.Fatalf("model = %q, want Acme One", h.session.model)
	}
}

func TestApplyModelArgumentReportsModelWarning(t *testing.T) {
	h := newHarness()
	h.bridge = New(Ports{
		Input: h.editor,
		Session: &scenarioSession{
			model:   "Gemini 3.8 Flash (High)",
			warning: `Model "X" is not configured. Falling back to mock shim.`,
		},
		Host:     h.host,
		Settings: staticSettings{"editorMode": "vim"}.Field,
	})
	h.bridge.ApplyModelArgument("Claude Sonnet 4.6 (Thinking)")
	if len(h.host.responses) != 1 || !strings.Contains(h.host.responses[0], "warning:") {
		t.Fatalf("responses = %v, want the warning echoed", h.host.responses)
	}
}

func TestSubmitModelCompletionResolves(t *testing.T) {
	h := newHarness()
	h.picker.modelMode = true
	h.picker.query = "Claude Sonnet 4.6 (Thinking)"
	handled, _ := h.bridge.SubmitModelCompletion()
	if !handled {
		t.Fatal("the key must be consumed")
	}
	if h.session.model != "Claude Sonnet 4.6 (Thinking)" {
		t.Fatalf("model = %q", h.session.model)
	}
	if h.editor.Value() != "" {
		t.Fatalf("composer = %q, want empty", h.editor.Value())
	}
	if h.history.resets == 0 {
		t.Fatal("the prompt history must be reset")
	}
}

func TestSubmitModelCompletionCompletesHighlightedRow(t *testing.T) {
	h := newHarness()
	h.picker.modelMode = true
	h.picker.query = "claude"
	h.picker.sel = &ModelRow{Name: "Claude Opus 4.6 (Thinking)", Description: "claude-opus-4-6-thinking"}
	handled, _ := h.bridge.SubmitModelCompletion()
	if !handled {
		t.Fatal("the key must be consumed")
	}
	if got := h.editor.Value(); got != "/model claude-opus-4-6-thinking" {
		t.Fatalf("composer = %q", got)
	}
	if h.host.cancels != 0 {
		t.Fatal("completing a row must not switch the model")
	}
}

func TestSubmitModelCompletionInvalid(t *testing.T) {
	h := newHarness()
	h.picker.modelMode = true
	h.picker.query = "zzz"
	handled, _ := h.bridge.SubmitModelCompletion()
	if !handled {
		t.Fatal("the key must be consumed")
	}
	if len(h.host.responses) != 1 || !strings.Contains(h.host.responses[0], `Invalid model "zzz"`) {
		t.Fatalf("responses = %v", h.host.responses)
	}
}

func TestSubmitModelCompletionIgnoresOtherModes(t *testing.T) {
	h := newHarness()
	h.picker.modelMode = false
	if handled, _ := h.bridge.SubmitModelCompletion(); handled {
		t.Fatal("the command-mode popup must not consume the key")
	}
}

// --- checkpoint ------------------------------------------------------

func TestCheckpointInsertionPoint(t *testing.T) {
	h := newHarness()
	h.bridge = New(Ports{
		Input: h.editor, Host: h.host,
		Counts:   scenarioCounts{calls: 5, steps: 3},
		Settings: staticSettings{"editorMode": "vim"}.Field,
	})
	if got := h.bridge.CheckpointInsertionPoint(1); got != 7 {
		t.Fatalf("point = %d, want 7", got)
	}
	if got := h.bridge.CheckpointInsertionPoint(20); got != 0 {
		t.Fatalf("point = %d, want 0 (clamped)", got)
	}
	// Without the port the answer is the zero point.
	bare := New(Ports{})
	if got := bare.CheckpointInsertionPoint(1); got != 0 {
		t.Fatalf("point = %d, want 0", got)
	}
}

// --- zero value ------------------------------------------------------

func TestZeroBridgeIsInert(t *testing.T) {
	var b Bridge
	if b.Enabled() || b.InsertFirstEnabled() {
		t.Fatal("the zero bridge must be off")
	}
	if b.Mode() != vim.Normal {
		t.Fatal("the zero bridge reports Normal")
	}
	if _, handled := b.HandleKey(runeKey('i')); handled {
		t.Fatal("the zero bridge handles no key")
	}
	if b.InsertPastedText("x") != nil {
		t.Fatal("the zero bridge has no composer to paste into")
	}
	if b.ApplyModelArgument("claude") != nil {
		t.Fatal("the zero bridge switches no model")
	}
	if handled, _ := b.SubmitModelCompletion(); handled {
		t.Fatal("the zero bridge completes nothing")
	}
}

// feed types every rune of s through the key loop.
func feed(t *testing.T, h *harness, s string) {
	t.Helper()
	for _, r := range s {
		h.bridge.HandleKey(runeKey(r))
	}
}
