// keys.go — the composer's editing keys.
//
// Every function here answers a decision: which key does what to the
// composer's text. The arithmetic that follows a decision belongs to
// the inputselection package, and the widget's own writes belong to
// the Editor the host hands in.

package composer

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/features/tools/clipboard"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
)

// HandlePaste pastes text into the composer at the cursor position, or
// replaces the active selection.
//
// If the host provides a custom Paste seam (e.g. for media attachment
// or placeholder folding), it delegates to s.Paste. Otherwise, it
// writes through to the editor directly, records undo history, and runs
// afterEdit.
func HandlePaste(s Seams, text string) tea.Cmd {
	if s.Paste != nil {
		return s.Paste(text)
	}
	text = clipboard.NormalizeText(text)
	if text == "" {
		return nil
	}
	if s.UndoMgr != nil {
		s.UndoMgr.RecordBeforeChange(s.Editor.Value(), s.Editor.CursorOffset(), "paste")
	}
	if !s.Selection.Empty() {
		if s.ReplaceSelection != nil {
			s.ReplaceSelection(text)
		} else {
			val := s.Editor.Value()
			start, end := s.Selection.Start, s.Selection.End
			if start < 0 {
				start = 0
			}
			if end > len(val) {
				end = len(val)
			}
			if start <= end {
				s.Editor.SetValue(val[:start] + text + val[end:])
				s.MoveCursor(start + len(text))
			}
		}
		if s.ClearSelection != nil {
			s.ClearSelection()
		}
		s.afterEdit()
		return nil
	}
	s.Editor.InsertString(text)
	s.afterEdit()
	return nil
}

// HandleKey applies the selection keys to the composer.
//
// It answers the command the key produced and whether the key was
// consumed. A key the composer does not own is left to the host.
//
// The keys are the reference's own:
//
//	Cmd+A / Ctrl+Shift+A   select the whole value and copy it
//	Cmd+C / Ctrl+C         copy the selection
//	Cmd+X / Ctrl+X         cut the selection
//	Cmd+V / Ctrl+V         paste from clipboard
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
		if s.SelectAll != nil {
			if cmd, ok := s.SelectAll(); ok {
				return cmd, true
			}
			return nil, true
		}
		val := s.Editor.Value()
		if len(val) > 0 {
			_ = clipboard.Write(val)
			s.MoveCursor(len(val))
			return nil, true
		}
		return nil, true
	}

	// Undo / Redo: Cmd+Z, Ctrl+Z, Cmd+Shift+Z, Ctrl+Shift+Z, Cmd+Y, Ctrl+Y
	if (key.Code == 'z' || key.Code == 'Z') && (key.Mod.Contains(tea.ModCtrl) || key.Mod.Contains(tea.ModSuper)) {
		if key.Mod.Contains(tea.ModShift) {
			// Redo
			if s.UndoMgr != nil {
				if snap, ok := s.UndoMgr.Redo(s.Editor.Value(), s.Editor.CursorOffset()); ok {
					if s.ClearSelection != nil {
						s.ClearSelection()
					}
					s.Editor.SetValue(snap.Value)
					s.MoveCursor(snap.CursorOffset)
					s.afterEdit()
					return nil, true
				}
				return nil, true
			}
		} else {
			// Undo
			if s.UndoMgr != nil {
				if snap, ok := s.UndoMgr.Undo(s.Editor.Value(), s.Editor.CursorOffset()); ok {
					if s.ClearSelection != nil {
						s.ClearSelection()
					}
					s.Editor.SetValue(snap.Value)
					s.MoveCursor(snap.CursorOffset)
					s.afterEdit()
					return nil, true
				}
				if s.Editor.Value() == "" {
					return nil, false
				}
				return nil, true
			}
		}
	}
	if (key.Code == 'y' || key.Code == 'Y') && (key.Mod.Contains(tea.ModCtrl) || key.Mod.Contains(tea.ModSuper)) {
		// Redo
		if s.UndoMgr != nil {
			if snap, ok := s.UndoMgr.Redo(s.Editor.Value(), s.Editor.CursorOffset()); ok {
				if s.ClearSelection != nil {
					s.ClearSelection()
				}
				s.Editor.SetValue(snap.Value)
				s.MoveCursor(snap.CursorOffset)
				s.afterEdit()
				return nil, true
			}
			return nil, true
		}
	}

	// Cut the selection: Cmd+X (macOS) or Ctrl+X (Linux/Windows when text selected)
	if (key.Code == 'x' || key.Code == 'X') &&
		(key.Mod.Contains(tea.ModSuper) || (hasSelection && key.Mod.Contains(tea.ModCtrl))) &&
		!key.Mod.Contains(tea.ModAlt) {
		if hasSelection {
			val := s.Editor.Value()
			if sel.Start >= 0 && sel.End <= len(val) && sel.Start < sel.End {
				text := val[sel.Start:sel.End]
				var cmd tea.Cmd
				if s.Copy != nil {
					cmd = s.Copy(text)
				} else {
					_ = clipboard.Write(text)
				}
				if s.UndoMgr != nil {
					s.UndoMgr.RecordBeforeChange(val, s.Editor.CursorOffset(), "cut-selection")
				}
				if s.DeleteSelection != nil {
					s.DeleteSelection()
				} else {
					s.Editor.SetValue(val[:sel.Start] + val[sel.End:])
					s.MoveCursor(sel.Start)
				}
				if s.ClearSelection != nil {
					s.ClearSelection()
				}
				s.afterEdit()
				return cmd, true
			}
		}
		return nil, true
	}

	// Copy the selection: Cmd+C (macOS) or Ctrl+C (Linux/Windows when text selected)
	if (key.Code == 'c' || key.Code == 'C') &&
		(key.Mod.Contains(tea.ModSuper) || (hasSelection && key.Mod.Contains(tea.ModCtrl))) &&
		!key.Mod.Contains(tea.ModAlt) {
		if hasSelection {
			val := s.Editor.Value()
			if sel.Start >= 0 && sel.End <= len(val) && sel.Start < sel.End {
				text := val[sel.Start:sel.End]
				if s.Copy != nil {
					return s.Copy(text), true
				}
				_ = clipboard.Write(text)
				return nil, true
			}
		}
		return nil, true
	}

	// Paste: Cmd+V (macOS) or Ctrl+V (Linux/Windows)
	if (key.Code == 'v' || key.Code == 'V') &&
		(key.Mod.Contains(tea.ModSuper) || key.Mod.Contains(tea.ModCtrl)) &&
		!key.Mod.Contains(tea.ModAlt) {
		text := clipboard.Read()
		return HandlePaste(s, text), true
	}

	// Delete the selection.
	if (key.Code == tea.KeyBackspace || key.Code == tea.KeyDelete) && hasSelection {
		if s.UndoMgr != nil {
			s.UndoMgr.RecordBeforeChange(s.Editor.Value(), s.Editor.CursorOffset(), "delete-selection")
		}
		if s.DeleteSelection != nil {
			s.DeleteSelection()
		} else {
			val := s.Editor.Value()
			if sel.Start >= 0 && sel.End <= len(val) && sel.Start < sel.End {
				s.Editor.SetValue(val[:sel.Start] + val[sel.End:])
				s.MoveCursor(sel.Start)
			}
		}
		if s.ClearSelection != nil {
			s.ClearSelection()
		}
		s.afterEdit()
		return nil, true
	}

	// Delete a pasted-text placeholder as one unit: a pasted block is
	// undone in a single key.
	if (key.Code == tea.KeyBackspace || key.Code == tea.KeyDelete) && !hasSelection {
		val := s.Editor.Value()
		start, end, ok := placeholderAt(val, s.Editor.CursorOffset(), key.Code == tea.KeyBackspace)
		if ok {
			if s.UndoMgr != nil {
				s.UndoMgr.RecordBeforeChange(val, s.Editor.CursorOffset(), "delete-placeholder")
			}
			s.Editor.SetValue(val[:start] + val[end:])
			s.MoveCursor(start)
			s.afterEdit()
			return nil, true
		}
	}

	// Multiline newline insertion: Ctrl+J (ASCII LF / 0x0A newline chord), LF (10 / '\n'), or modified Enter (Shift+Enter / Alt+Enter)
	if ((key.Code == 'j' || key.Code == 'J') && key.Mod.Contains(tea.ModCtrl) && !key.Mod.Contains(tea.ModAlt)) ||
		(key.Code == tea.KeyEnter && (key.Mod.Contains(tea.ModShift) || key.Mod.Contains(tea.ModAlt)) && !key.Mod.Contains(tea.ModCtrl)) ||
		(key.Code == 10 || key.Code == '\n') {
		if s.UndoMgr != nil {
			s.UndoMgr.RecordBeforeChange(s.Editor.Value(), s.Editor.CursorOffset(), "newline")
		}
		if hasSelection {
			if s.ReplaceSelection != nil {
				s.ReplaceSelection("\n")
			} else {
				val := s.Editor.Value()
				if sel.Start >= 0 && sel.End <= len(val) && sel.Start < sel.End {
					s.Editor.SetValue(val[:sel.Start] + "\n" + val[sel.End:])
					s.MoveCursor(sel.Start + 1)
				}
			}
			if s.ClearSelection != nil {
				s.ClearSelection()
			}
		} else {
			s.Editor.InsertRune('\n')
		}
		s.afterEdit()
		return nil, true
	}

	// Backslash newline on unmodified Enter: a trailing backslash before the
	// cursor converts into a newline rather than submitting the prompt.
	if key.Code == tea.KeyEnter && key.Mod == 0 {
		if BackslashNewline(s) {
			return nil, true
		}
	}

	// Word-boundary navigation: Alt+Left / Ctrl+Left / Alt+B, Alt+Right / Ctrl+Right / Alt+F.
	if (key.Code == tea.KeyLeft && (key.Mod.Contains(tea.ModAlt) || key.Mod.Contains(tea.ModCtrl))) ||
		((key.Code == 'b' || key.Code == 'B') && key.Mod.Contains(tea.ModAlt)) {
		if hasSelection {
			s.ClearSelection()
			s.MoveCursor(sel.Start)
		}
		if HandleWordLeft(s) {
			return nil, true
		}
		return nil, true
	}
	if (key.Code == tea.KeyRight && (key.Mod.Contains(tea.ModAlt) || key.Mod.Contains(tea.ModCtrl))) ||
		((key.Code == 'f' || key.Code == 'F') && key.Mod.Contains(tea.ModAlt)) {
		if hasSelection {
			s.ClearSelection()
			s.MoveCursor(sel.End)
		}
		if HandleWordRight(s) {
			return nil, true
		}
		return nil, true
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
		if s.UndoMgr != nil {
			s.UndoMgr.RecordBeforeChange(s.Editor.Value(), s.Editor.CursorOffset(), "replace-selection")
		}
		if s.ReplaceSelection != nil {
			s.ReplaceSelection(key.Text)
		} else {
			val := s.Editor.Value()
			if sel.Start >= 0 && sel.End <= len(val) && sel.Start < sel.End {
				s.Editor.SetValue(val[:sel.Start] + key.Text + val[sel.End:])
				s.MoveCursor(sel.Start + len(key.Text))
			}
		}
		if s.ClearSelection != nil {
			s.ClearSelection()
		}
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
		if s.UndoMgr != nil {
			s.UndoMgr.RecordBeforeChange(val, offset, "backslash-newline")
		}
		s.Editor.Backspace()
		s.Editor.InsertRune('\n')
		if s.DismissModeHint != nil {
			s.DismissModeHint()
		}
		if s.Picker != nil {
			s.Picker.Update([]rune(tuilib.FirstLine(s.Editor.Value())))
		}
		if s.Files != nil {
			s.Files.Update()
		}
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
	key := msg.Key()
	if s.UndoMgr != nil {
		switch key.Code {
		case tea.KeyBackspace, tea.KeyDelete:
			s.UndoMgr.RecordBeforeChange(s.Editor.Value(), s.Editor.CursorOffset(), "delete")
		default:
			if key.Text != "" && !key.Mod.Contains(tea.ModCtrl) && !key.Mod.Contains(tea.ModAlt) && !key.Mod.Contains(tea.ModSuper) {
				s.UndoMgr.RecordType(s.Editor.Value(), s.Editor.CursorOffset(), key.Text)
			}
		}
	}
	oldVal := s.Editor.Value()
	oldLine := s.Editor.Line()
	oldCol := s.Editor.Column()
	cmd := s.Editor.Update(msg)
	if s.DismissModeHint != nil {
		s.DismissModeHint()
	}
	if s.Picker != nil {
		s.Picker.Update([]rune(tuilib.FirstLine(s.Editor.Value())))
	}
	if s.ClearEligibilityNotice != nil {
		s.ClearEligibilityNotice()
	}
	if s.Files != nil {
		s.Files.Update()
	}
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
