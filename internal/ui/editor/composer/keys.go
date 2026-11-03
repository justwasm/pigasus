// keys.go — the composer's editing keys.
//
// Every function here answers a decision: which key does what to the
// composer's text. The arithmetic that follows a decision belongs to
// the inputselection package, and the widget's own writes belong to
// the Editor the host hands in.

package composer

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/tuilib"
)

// HandleKey applies the selection keys to the composer.
//
// It answers the command the key produced and whether the key was
// consumed. A key the composer does not own is left to the host.
//
// The keys are the reference's own:
//
//	Cmd+A / Ctrl+Shift+A   select the whole value and copy it
//	Cmd+C                  copy the selection
//	Backspace / Delete     delete the selection, or a pasted-text
//	                       placeholder as one unit
//	Left / Right           collapse the selection to its own edge
//	any printable key      replace the selection with the key's text
func HandleKey(s Seams, key tea.Key) (tea.Cmd, bool) {
	sel := s.Selection
	hasSelection := !sel.Empty()

	// Select all: Cmd+A, Ctrl+Shift+A or the Meta variant.
	if (key.Code == 'a' || key.Code == 'A') &&
		(key.Mod.Contains(tea.ModSuper) || key.Mod.Contains(tea.ModMeta) ||
			(key.Mod.Contains(tea.ModCtrl) && key.Mod.Contains(tea.ModShift))) {
		if cmd, ok := s.SelectAll(); ok {
			return cmd, true
		}
		return nil, true
	}

	// Copy the selection.
	if (key.Code == 'c' || key.Code == 'C') && key.Mod.Contains(tea.ModSuper) {
		if hasSelection {
			val := s.Editor.Value()
			if sel.Start < len(val) && sel.End <= len(val) {
				text := val[sel.Start:sel.End]
				return s.Copy(text), true
			}
		}
		return nil, true
	}

	// Delete the selection.
	if (key.Code == tea.KeyBackspace || key.Code == tea.KeyDelete) && hasSelection {
		s.DeleteSelection()
		s.afterEdit()
		return nil, true
	}

	// Delete a pasted-text placeholder as one unit: a pasted block is
	// undone in a single key.
	if (key.Code == tea.KeyBackspace || key.Code == tea.KeyDelete) && !hasSelection {
		val := s.Editor.Value()
		start, end, ok := placeholderAt(val, s.Editor.CursorOffset(), key.Code == tea.KeyBackspace)
		if ok {
			s.Editor.SetValue(val[:start] + val[end:])
			s.MoveCursor(start)
			s.afterEdit()
			return nil, true
		}
	}

	// An arrow key without Shift collapses the selection to the edge
	// the key names.
	if (key.Code == tea.KeyLeft || key.Code == tea.KeyRight) && hasSelection && !key.Mod.Contains(tea.ModShift) {
		s.ClearSelection()
		if key.Code == tea.KeyLeft {
			s.MoveCursor(sel.Start)
		} else {
			s.MoveCursor(sel.End)
		}
		if s.History != nil {
			s.History.Reset()
		}
		return nil, true
	}

	// A printable key replaces the selection with the key's own text.
	if hasSelection && key.Text != "" &&
		!key.Mod.Contains(tea.ModCtrl) && !key.Mod.Contains(tea.ModAlt) && !key.Mod.Contains(tea.ModSuper) {
		s.ReplaceSelection(key.Text)
		s.afterEdit()
		return nil, true
	}

	return nil, false
}

// BackslashNewline turns a trailing backslash into a newline.
//
// The composer's Enter key asks this first: a backslash before the
// cursor is the reference's way of asking for a newline, and the
// prompt must not be submitted.
//
// It answers whether the fallback applied.
func BackslashNewline(s Seams) bool {
	offset := s.Editor.CursorOffset()
	val := s.Editor.Value()
	if offset > 0 && offset <= len(val) && val[offset-1] == '\\' {
		s.Editor.Backspace()
		s.Editor.InsertRune('\n')
		s.DismissModeHint()
		s.Picker.Update([]rune(tuilib.FirstLine(s.Editor.Value())))
		s.Files.Update()
		return true
	}
	return false
}

// UpdateTextarea feeds one key to the composer's widget and keeps the
// two dropdowns in step with it.
//
// It answers the command the widget produced.
//
// The prompt history's navigation is reset when the text changed
// under a running navigation: the user has typed something else, and
// the prompt the cursor is on is no longer the one it recalled.
func UpdateTextarea(s Seams, msg tea.KeyMsg) tea.Cmd {
	oldVal := s.Editor.Value()
	oldLine := s.Editor.Line()
	oldCol := s.Editor.Column()
	cmd := s.Editor.Update(msg)
	s.DismissModeHint()
	s.Picker.Update([]rune(tuilib.FirstLine(s.Editor.Value())))
	s.ClearEligibilityNotice()
	s.Files.Update()
	if s.History != nil && s.History.Index() > 0 {
		newVal := s.Editor.Value()
		newLine := s.Editor.Line()
		newCol := s.Editor.Column()
		if newVal != oldVal || newLine != oldLine || newCol != oldCol {
			s.History.Reset()
		}
	}
	return cmd
}
