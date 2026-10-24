// picker_test.go — validates the picker state machine wires up
// correctly against the observed v1.3.5 binary behavior in
// /tmp/agy_slash.txt and /tmp/agy_slash_add.txt.
package tuilib

import (
	"testing"
)

func TestPickerUpdate_EmptyBuffer(t *testing.T) {
	var p PickerState
	if p.Update(nil) {
		// Reset is a state change but visible should remain false.
	}
	if p.visible {
		t.Error("picker should be hidden on empty buffer")
	}
}

func TestPickerUpdate_LeadingSlashOpensPicker(t *testing.T) {
	var p PickerState
	p.Update([]rune{'/'})
	if !p.visible {
		t.Fatal("picker should open on bare /")
	}
	if len(p.all) == 0 {
		t.Fatal("picker should have at least one match on bare /")
	}
	if p.idx != 0 {
		t.Errorf("idx should start at 0, got %d", p.idx)
	}
	if got := p.Selected().Name; got != "/add-dir" {
		t.Errorf("first visible row should be /add-dir (matching /tmp/agy_slash.txt), got %q", got)
	}
}

func TestPickerUpdate_FilterAdd(t *testing.T) {
	var p PickerState
	p.Update([]rune{'/', 'a', 'd', 'd'})
	if !p.visible {
		t.Fatal("picker should be visible after /add")
	}
	if len(p.all) == 0 {
		t.Fatal("picker should have at least one match for /add")
	}
	if got := p.all[0].Name; got != "/add-dir" {
		t.Errorf("first filtered row should be /add-dir, got %q", got)
	}
}

func TestPickerUpdate_FilterNoMatch(t *testing.T) {
	var p PickerState
	p.Update([]rune{'/', 'z', 'z', 'z', 'z'})
	// Cycle 93: picker stays visible but empty so the overlay can
	// render the "   No matches" row + hint row (matches ref v1.3.5).
	if !p.visible {
		t.Error("picker should remain visible when buffer starts with '/' but has no matches")
	}
	if len(p.all) != 0 {
		t.Errorf("all should be empty for no-match filter, got %d", len(p.all))
	}
	if !p.Empty() {
		t.Error("Empty() should be true when visible but no rows match")
	}
	if p.idx != -1 {
		t.Errorf("idx should be -1 for no-match state, got %d", p.idx)
	}
}

func TestPickerMoveDownMoveUp(t *testing.T) {
	var p PickerState
	p.Update([]rune{'/'})
	if !p.visible {
		t.Fatal("picker should open on bare /")
	}
	start := p.Selected().Name
	p.MoveDown()
	if p.Selected().Name == start {
		t.Error("MoveDown should change selection")
	}
	p.MoveUp()
	if p.Selected().Name != start {
		t.Errorf("MoveUp should restore prior selection, got %q", p.Selected().Name)
	}
}

func TestPickerMoveUp_ClampsAt0(t *testing.T) {
	var p PickerState
	p.Update([]rune{'/'})
	if !p.visible {
		t.Fatal("picker should open on bare /")
	}
	p.MoveUp()
	p.MoveUp()
	p.MoveUp()
	if p.idx != 0 {
		t.Errorf("idx should clamp at 0, got %d", p.idx)
	}
}

func TestPickerMoveDown_ClampsAtLast(t *testing.T) {
	var p PickerState
	p.Update([]rune{'/'})
	for i := 0; i < 1000; i++ {
		p.MoveDown()
	}
	if p.idx != len(p.all)-1 {
		t.Errorf("idx should clamp at last row %d, got %d", len(p.all)-1, p.idx)
	}
}

func TestPickerUpdate_BufferLosesSlash(t *testing.T) {
	var p PickerState
	p.Update([]rune{'/', 'a', 'd', 'd'})
	if !p.visible {
		t.Fatal("picker should be visible after /add")
	}
	// Simulate backspace x4 to remove the slash.
	buf := []rune{'a', 'd', 'd'}
	p.Update(buf)
	if p.visible {
		t.Error("picker should be hidden once leading slash is gone")
	}
}

// Cycle 232: SetIdx moves the highlight to an absolute index. The
// mouse click + hover handlers in cmd/wagyu/picker_mouse.go call this
// to land the highlight on the clicked/hovered row.
func TestPickerSetIdx_MovesHighlight(t *testing.T) {
	var p PickerState
	p.Update([]rune{'/'})
	if !p.visible {
		t.Fatal("picker should be visible after /")
	}
	if got := p.Len(); got < 4 {
		t.Fatalf("setup: want >= 4 picker rows, got %d", got)
	}
	if !p.SetIdx(3) {
		t.Error("SetIdx(3) should report a change")
	}
	if got := p.Idx(); got != 3 {
		t.Errorf("Idx after SetIdx(3) = %d, want 3", got)
	}
	if p.SetIdx(3) {
		t.Error("SetIdx(3) on the same row should report no change")
	}
}

// SetIdx clamps out-of-range values to the valid range, and returns
// false when the picker is hidden or has no rows.
func TestPickerSetIdx_ClampsAndHides(t *testing.T) {
	var p PickerState
	// Hidden picker: SetIdx is a no-op.
	if p.SetIdx(2) {
		t.Error("SetIdx on hidden picker should return false")
	}
	// Visible picker with rows: clamp negatives to 0 and huge values to last.
	p.Update([]rune{'/'})
	last := p.Len() - 1
	// After Update('/') the picker highlights row 0; nudge to row 1
	// first so SetIdx(-5) actually changes the highlight.
	p.MoveDown()
	if p.Idx() != 1 {
		t.Fatalf("setup: idx should be 1 after MoveDown, got %d", p.Idx())
	}
	if !p.SetIdx(-5) {
		t.Error("SetIdx(-5) should still clamp and return true")
	}
	if p.Idx() != 0 {
		t.Errorf("Idx after SetIdx(-5) = %d, want 0", p.Idx())
	}
	if !p.SetIdx(9999) {
		t.Error("SetIdx(9999) should clamp and return true")
	}
	if p.Idx() != last {
		t.Errorf("Idx after SetIdx(9999) = %d, want %d", p.Idx(), last)
	}
}

// modelCatalogue is the row set the "/model <query>" popup filters.
func modelCatalogue() []ModelRow {
	return []ModelRow{
		{Label: "Gemini 3.8 Flash (High)", ID: "gemini-3.8-flash-high"},
		{Label: "Gemini 3.8 Flash (Medium)", ID: "gemini-3.8-flash-medium"},
		{Label: "Claude Sonnet 4.6 (Thinking)", ID: "claude-sonnet-4-6"},
		{Label: "Claude Opus 4.6 (Thinking)", ID: "claude-opus-4-6-thinking"},
		{Label: "GPT-OSS 120B (Medium)", ID: "gpt-oss-120b-medium"},
	}
}

// The "/model " line is not a command prefix but an *argument* being
// typed: the reference swaps the command rows for the model rows and
// lists the whole catalogue for an empty query
// (【一手】 agy 1.4.6, live).
func TestPickerUpdate_ModelModeListsCatalogue(t *testing.T) {
	var p PickerState
	p.SetModelCatalogueSource(modelCatalogue)
	p.Update([]rune("/model "))
	if p.Kind() != PickerKindModel {
		t.Fatalf("Kind = %v, want PickerKindModel", p.Kind())
	}
	if !p.Visible() {
		t.Fatal("picker should be visible for /model ")
	}
	if p.Len() != len(modelCatalogue()) {
		t.Errorf("Len = %d, want %d", p.Len(), len(modelCatalogue()))
	}
	if got := p.Query(); got != "" {
		t.Errorf("Query = %q, want empty", got)
	}
}

// A bare "/model" is still a command prefix: the reference shows the
// command rows, not the models (【一手】 agy 1.4.6, live).
func TestPickerUpdate_BareModelStaysACommand(t *testing.T) {
	var p PickerState
	p.SetModelCatalogueSource(modelCatalogue)
	p.Update([]rune("/model"))
	if p.Kind() != PickerKindCommand {
		t.Fatalf("Kind = %v, want PickerKindCommand", p.Kind())
	}
	sel := p.Selected()
	if sel == nil || sel.Name != "/model" {
		t.Fatalf("selected row = %v, want /model", sel)
	}
}

// The filter is a case-insensitive *substring* of the label or of the
// catalogue id — not a subsequence. "son" matches Sonnet and not Opus
// (【一手】 agy 1.4.6, live: "onnet", "4.6", "ink", "flash", "oss" and
// "120" all match, "son" matches only Sonnet).
func TestPickerUpdate_ModelModeFiltersBySubstring(t *testing.T) {
	cases := []struct {
		query string
		want  []string
	}{
		{"claude", []string{"Claude Sonnet 4.6 (Thinking)", "Claude Opus 4.6 (Thinking)"}},
		{"son", []string{"Claude Sonnet 4.6 (Thinking)"}},
		{"sonnet-4-6", []string{"Claude Sonnet 4.6 (Thinking)"}},
		{"4.6", []string{"Claude Sonnet 4.6 (Thinking)", "Claude Opus 4.6 (Thinking)"}},
		{"oss", []string{"GPT-OSS 120B (Medium)"}},
		{"120", []string{"GPT-OSS 120B (Medium)"}},
		{"ink", []string{"Claude Sonnet 4.6 (Thinking)", "Claude Opus 4.6 (Thinking)"}},
	}
	for _, tc := range cases {
		var p PickerState
		p.SetModelCatalogueSource(modelCatalogue)
		p.Update([]rune("/model " + tc.query))
		if p.Kind() != PickerKindModel {
			t.Fatalf("%q: Kind = %v, want PickerKindModel", tc.query, p.Kind())
		}
		if p.Len() != len(tc.want) {
			t.Errorf("%q: Len = %d, want %d", tc.query, p.Len(), len(tc.want))
			continue
		}
		for i, want := range tc.want {
			if got := p.all[i].Name; got != want {
				t.Errorf("%q: row %d = %q, want %q", tc.query, i, got, want)
			}
		}
	}
}

// A query that matches nothing keeps the popup visible with no rows, so
// the overlay can render the "   No matches" row
// (【一手】 agy 1.4.6, live).
func TestPickerUpdate_ModelModeNoMatch(t *testing.T) {
	var p PickerState
	p.SetModelCatalogueSource(modelCatalogue)
	p.Update([]rune("/model zzz"))
	if !p.Visible() {
		t.Fatal("picker should stay visible when no model matches")
	}
	if p.Len() != 0 {
		t.Errorf("Len = %d, want 0", p.Len())
	}
	if !p.Empty() {
		t.Error("Empty() should be true when no model matches")
	}
}

// The catalogue is re-read on every update, so a configuration change
// shows up on the next keystroke.
func TestPickerUpdate_ModelModeRereadsCatalogue(t *testing.T) {
	rows := modelCatalogue()
	var p PickerState
	p.SetModelCatalogueSource(func() []ModelRow { return rows })
	p.Update([]rune("/model "))
	if p.Len() != len(rows) {
		t.Fatalf("Len = %d, want %d", p.Len(), len(rows))
	}
	rows = append(rows, ModelRow{Label: "MiniMax M3", ID: "minimax-m3"})
	p.Update([]rune("/model "))
	if p.Len() != len(rows) {
		t.Errorf("Len after catalogue grew = %d, want %d", p.Len(), len(rows))
	}
}
