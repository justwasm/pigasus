package tui

import (
	"testing"

	tas "charm.land/bubbles/v2/textarea"
)

// TestPromptModel_OwnsInputSelection pins the ownership move: the input
// area's selection belongs to PromptModel, not to the viewport's
// TextSelection. The old location is gone at compile time; this test
// exercises the behaviour through the model alone.
func TestPromptModel_OwnsInputSelection(t *testing.T) {
	var p PromptModel
	p.input = tas.New()
	p.input.SetValue("hello world")

	val, ok := p.SelectAll()
	if !ok || val != "hello world" {
		t.Fatalf("SelectAll = (%q, %v)", val, ok)
	}
	if !p.HasInputSelection() {
		t.Fatal("SelectAll did not set a selection")
	}
	start, end := p.InputSelectionRange()
	if start != 0 || end != len("hello world") {
		t.Fatalf("range = (%d, %d)", start, end)
	}

	p.ReplaceInputSelection("bye")
	if got := p.ComposerValue(); got != "bye" {
		t.Fatalf("value = %q after replace, want bye", got)
	}
	if p.HasInputSelection() {
		t.Fatal("replace must clear the selection")
	}
}

// TestPromptModel_DeleteSelection keeps the range arithmetic and the
// textarea write in one place.
func TestPromptModel_DeleteSelection(t *testing.T) {
	var p PromptModel
	p.input = tas.New()
	p.input.SetValue("abcdef")
	p.InputSelStart = 1
	p.InputSelEnd = 4
	p.DeleteInputSelection()
	if got := p.ComposerValue(); got != "aef" {
		t.Fatalf("value = %q, want aef", got)
	}
	if p.HasInputSelection() {
		t.Fatal("delete must clear the selection")
	}
}

// TestPromptModel_ClearResetsSelection checks that a cleared composer does
// not leave a stale selection behind for the next prompt.
func TestPromptModel_ClearResetsSelection(t *testing.T) {
	var p PromptModel
	p.input = tas.New()
	p.input.SetValue("draft")
	p.InputSelStart = 0
	p.InputSelEnd = 5
	p.ClearComposer()
	if p.HasInputSelection() || p.ComposerValue() != "" {
		t.Fatalf("clear left value=%q selection=%v", p.ComposerValue(), p.HasInputSelection())
	}
}

// TestRootModel_RoutesComposerToPromptModel keeps the root an adapter: the
// exported composer methods are thin forwards.
func TestRootModel_RoutesComposerToPromptModel(t *testing.T) {
	m := &RootModel{}
	m.prompt.input = tas.New()
	m.SetComposerValue("typed")
	if m.ComposerValue() != "typed" {
		t.Fatalf("ComposerValue = %q", m.ComposerValue())
	}
	if m.IsComposerEmpty() {
		t.Fatal("typed composer reported empty")
	}
	m.ClearComposer()
	if !m.IsComposerEmpty() {
		t.Fatal("ClearComposer did not clear")
	}
}
