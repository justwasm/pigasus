// picker_model_test.go — the "/model <query>" completion popup.
//
// The reference's composer completes the /model *argument* with a popup
// of models. 【一手】 agy 1.4.6, live tmux captures — the transcript of
// the captures is in
// docs/cycles/model-argument-completion/captures.md.
//
//   - the rows are the display label in a 30-column field and the
//     catalogue id two columns further right;
//   - the filter is a case-insensitive substring of the label or the id;
//   - Tab completes the highlighted label into the composer and closes
//     the popup;
//   - Enter runs the command when the argument names a model, completes
//     the highlighted row otherwise, and reports an unknown model when
//     nothing matches.
package cliapp

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/configtest"
	"github.com/justwasm/pigasus/internal/tuilib"
)

// typeModelLine types a line into the composer the way the terminal
// does, so the picker refreshes on every keystroke.
func typeModelLine(m *runMode, line string) {
	for _, r := range line {
		m.Update(runeKey(r))
	}
}

// modelPopupRows returns the popup's plain-text rows.
func modelPopupRows(m *runMode) []string {
	body := stripAnsi(m.pickerView())
	var rows []string
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		rows = append(rows, line)
	}
	return rows
}

// The popup renders the display label in a 30-column field and the
// catalogue id two columns further right — the id of every row starts at
// the same column.
func TestModelPopup_RendersLabelAndCatalogueID(t *testing.T) {
	configtest.Isolate(t)
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	typeModelLine(m, "/model claude")
	if m.picker.Kind() != tuilib.PickerKindModel {
		t.Fatalf("picker kind = %v, want PickerKindModel", m.picker.Kind())
	}
	rows := modelPopupRows(m)
	if len(rows) < 2 {
		t.Fatalf("popup rows = %v, want the two Claude models", rows)
	}
	want := []string{
		"> Claude Sonnet 4.6 (Thinking)  claude-sonnet-4-6",
		"  Claude Opus 4.6 (Thinking)    claude-opus-4-6-thinking",
	}
	for i, w := range want {
		if rows[i] != w {
			t.Errorf("row %d = %q, want %q", i, rows[i], w)
		}
	}
	for i, row := range rows[:2] {
		if col := strings.Index(row, "claude-"); col != 32 {
			t.Errorf("row %d: catalogue id starts at column %d, want 32 (%q)", i, col, row)
		}
	}
}

// An empty query lists the whole catalogue, and the popup reports the
// rows below the window the way the reference does.
func TestModelPopup_EmptyQueryListsEveryModel(t *testing.T) {
	configtest.Isolate(t)
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	typeModelLine(m, "/model ")
	rows := modelPopupRows(m)
	if len(rows) < 6 {
		t.Fatalf("popup rows = %v, want the six reference models", rows)
	}
	if !strings.Contains(rows[0], "Gemini 3.8 Flash (High)") {
		t.Errorf("first row = %q, want the first catalogue model", rows[0])
	}
	// The reference's window shows five rows and reports the rest.
	tail := ""
	for _, row := range rows {
		if strings.Contains(row, "more") {
			tail = row
		}
	}
	if !strings.Contains(tail, "↓ 1 more") {
		t.Errorf("tail row = %q, want the ↓ 1 more tail", tail)
	}
}

// Tab completes the highlighted label into the composer and closes the
// popup.
func TestModelPopup_TabCompletesTheLabel(t *testing.T) {
	configtest.Isolate(t)
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	typeModelLine(m, "/model claude")
	m.Update(key(tea.KeyTab, 0))
	if got := m.input.Value(); got != "/model Claude Sonnet 4.6 (Thinking)" {
		t.Errorf("composer = %q, want the completed label", got)
	}
	if m.picker.Visible() {
		t.Error("the popup should close after Tab")
	}
}

// Enter with an argument that names a model runs the command: the model
// changes and the transcript says so.
func TestModelPopup_EnterRunsTheNamedModel(t *testing.T) {
	cycle196TestHome(t, nil)
	configtest.Isolate(t)
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	typeModelLine(m, "/model Claude Sonnet 4.6 (Thinking)")
	m.Update(enterKey())
	if m.model != "Claude Sonnet 4.6 (Thinking)" {
		t.Errorf("model = %q, want the named model", m.model)
	}
	if !strings.Contains(strings.Join(m.response, "\n"), "Model set to Claude Sonnet 4.6 (Thinking)") {
		t.Errorf("transcript = %v, want the Model set to line", m.response)
	}
	if got := m.input.Value(); got != "" {
		t.Errorf("composer = %q, want it cleared", got)
	}
}

// The argument also resolves through the catalogue id, case-insensitively.
func TestModelPopup_EnterResolvesTheCatalogueID(t *testing.T) {
	cycle196TestHome(t, nil)
	configtest.Isolate(t)
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	typeModelLine(m, "/model claude-sonnet-4-6")
	m.Update(enterKey())
	if m.model != "Claude Sonnet 4.6 (Thinking)" {
		t.Errorf("model = %q, want Claude Sonnet 4.6 (Thinking)", m.model)
	}
}

// Enter with an argument that names no model completes the highlighted
// row into the composer instead of running the command.
func TestModelPopup_EnterCompletesTheHighlightedRow(t *testing.T) {
	cycle196TestHome(t, nil)
	configtest.Isolate(t)
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	typeModelLine(m, "/model claude")
	m.Update(enterKey())
	if got := m.input.Value(); got != "/model Claude Sonnet 4.6 (Thinking)" {
		t.Errorf("composer = %q, want the completed label", got)
	}
	if m.picker.Visible() {
		t.Error("the popup should close after Enter")
	}
	if len(m.response) != 0 {
		t.Errorf("transcript = %v, want no command output", m.response)
	}
}

// Enter with an argument nothing matches runs the command and reports
// the reference's error.
func TestModelPopup_EnterReportsAnUnknownModel(t *testing.T) {
	cycle196TestHome(t, nil)
	configtest.Isolate(t)
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	typeModelLine(m, "/model zzz")
	m.Update(enterKey())
	joined := strings.Join(m.response, "\n")
	if !strings.Contains(joined, `Invalid model "zzz" (available: gemini-3.8-flash-high,`) {
		t.Errorf("transcript = %q, want the Invalid model line", joined)
	}
	if got := m.input.Value(); got != "" {
		t.Errorf("composer = %q, want it cleared", got)
	}
}

// The composer shows the argument hint of an empty "/model " argument
// and colours the command token.
func TestModelPopup_ComposerShowsTheArgumentHint(t *testing.T) {
	configtest.Isolate(t)
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.input.SetValue("/model ")
	view := stripAnsi(m.formatInputView())
	if !strings.Contains(view, "> /model [model]") {
		t.Errorf("composer = %q, want the [model] hint", view)
	}
	m.input.SetValue("/model claude")
	view = stripAnsi(m.formatInputView())
	if strings.Contains(view, "[model]") {
		t.Errorf("composer = %q, want no hint once the argument is typed", view)
	}
}

// A command token that names a command exactly is coloured; a prefix is
// not.
func TestComposer_CommandTokenIsColoured(t *testing.T) {
	configtest.Isolate(t)
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.input.SetValue("/model claude")
	view := m.formatInputView()
	if !strings.Contains(view, cBlueOpen+"/model"+cBlueClose) {
		t.Errorf("composer = %q, want the blue command token", view)
	}
	m.input.SetValue("/codesear")
	view = m.formatInputView()
	if strings.Contains(view, cBlueOpen+"/codesear"+cBlueClose) {
		t.Errorf("composer = %q, want a plain prefix", view)
	}
}

// The model popup drops the "esc to cancel" hint the command popup
// carries.
func TestModelPopup_StatusRowDropsTheCancelHint(t *testing.T) {
	configtest.Isolate(t)
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	typeModelLine(m, "/model claude")
	left, _ := m.statusLeftSegment()
	if strings.Contains(left, "esc to cancel") {
		t.Errorf("status left = %q, want no cancel hint", left)
	}
	typeModelLine(m, "") // reset the composer
	m.input.Reset()
	m.picker.Update([]rune("/"))
	left, _ = m.statusLeftSegment()
	if !strings.Contains(left, "esc to cancel") {
		t.Errorf("status left = %q, want the command popup's cancel hint", left)
	}
}
