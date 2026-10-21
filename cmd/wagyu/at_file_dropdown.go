// at_file_dropdown.go — feature/at-file-dropdown-newline
//
// The `@` file-mention dropdown surfaced by AGY 1.3.6 line 12
// (and the corresponding fix that lets it appear at the start of a
// new line, not just at end-of-line). The clone learns this by
// maintaining a small, runMode-scoped state machine:
//
//   - **visible** is true when the buffer's trailing run carries an
//     `@<chars>` mention token (the parser lives in
//     internal/atfiles and is exercised by atfiles_test.go).
//   - **candidates** are the workspace file paths matching the
//     current prefix, recomputed on every Update call. The list
//     is sorted so Up/Down navigation is stable.
//   - **idx** is the highlighted row. -1 means "no selection";
//     always reset to 0 when the candidates refresh.
//
// Wiring points (mirroring pickerView's slash-command analogue):
//
//   - cmd/wagyu/tui_run.go calls m.atFileDropdown.Update(buffer)
//     after every input change and emits a redraw via the
//     existing tea.Cmd plumbing.
//   - cmd/wagyu/tui_run.go routes Tab, Up, Down, and Esc to the
//     dropdown helpers when the dropdown is visible, falling
//     through to the textarea otherwise.
//   - The renderer below is invoked from viewBody — same slot as
//     pickerView — so the dropdown sits between the bottom
//     separator and the status row, matching the AGY layout.
//
// The previous behaviour — "the dropdown only fires when `@` is at
// the end of a line (or the line is empty)" — is the AGY 1.3.6 line
// bug this cycle fixes. We exercise the fix in
// AtFileDropdown_AfterNewlineFiresLikeEndOfLine.
package main

import (
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/internal/atfiles"
)

// atFileDropdown is the runMode-scoped state for the @ file-mention
// overlay. Kept separate from m.picker (which owns the slash-command
// picker) because the prefix character and the candidate source are
// different — sharing one state would force the renderer to branch
// at every accessor call site.
type atFileDropdown struct {
	// visible is true when the dropdown overlay should be rendered.
	// Driven by Update: if the buffer does not end with an active
	// @-mention token, visible is forced false.
	visible bool
	// empty is the "no matches" sub-state: visible=true but the
	// candidate list has zero entries. Distinct so the renderer
	// can emit "   No matches" (matching AGY's slash-picker's
	// cycle-93 row) instead of the highlighted-row layout.
	empty bool
	// candidates holds the ranked rows for the current prefix.
	candidates []atfiles.Entry
	// all is the cached recursive workspace scan. Listing a large
	// checkout on every keystroke is wasteful; it is refreshed
	// when the dropdown opens (Reset clears it).
	all []atfiles.Entry
	// dirMode is true when the prefix ends in "/" and the rows are
	// the direct children of that directory.
	dirMode bool
	// queryMode is true when the prefix is non-empty and not a
	// directory path; 1.4.3 renders those rows as names only.
	queryMode bool
	// prefix is the substring after `@` in the current active
	// mention. Stored alongside the candidate list so Tab
	// completion can compute the delta without re-running the
	// parser.
	prefix string
	// mentionStart / mentionEnd are the byte offsets in the input
	// buffer that the dropdown will splice the completion into.
	mentionStart int
	mentionEnd   int
	// idx is the highlighted row (-1 when the candidate list is
	// empty). Reset to 0 each time the candidates refresh so
	// typing a single letter keeps the highlight on the first
	// alpha-numerically-sorted match.
	idx int
	// cwd is the directory the candidates are rooted at.
	cwd string
}

// Reset hides the dropdown and clears the candidate list. Called
// when the user navigates away from the @-mention (e.g. presses
// Esc on the overlay) or submits the prompt.
func (d *atFileDropdown) Reset() {
	d.visible = false
	d.empty = false
	d.candidates = nil
	d.all = nil
	d.dirMode = false
	d.queryMode = false
	d.prefix = ""
	d.mentionStart = 0
	d.mentionEnd = 0
	d.idx = -1
}

// Visible reports whether the dropdown overlay should be drawn.
// Mirrors the slash-picker's contract so viewBody can compose the
// two overlays without picking one explicitly.
func (d *atFileDropdown) Visible() bool { return d.visible }

// Empty reports whether the dropdown is visible but has no
// candidates (renders the "No matches" row).
func (d *atFileDropdown) Empty() bool { return d.visible && len(d.candidates) == 0 }

// Len is the candidate count. Zero + !Visible means "nothing to
// draw at all"; Empty() == true means "draw the no-matches row".
func (d *atFileDropdown) Len() int { return len(d.candidates) }

// Idx returns the highlighted row, -1 when the candidate list is
// empty.
func (d *atFileDropdown) Idx() int {
	if len(d.candidates) == 0 {
		return -1
	}
	return d.idx
}

// Selected returns the highlighted candidate, "" when none. The
// return type is string (not *string) so callers can branch on
// emptiness with a single comparison.
func (d *atFileDropdown) Selected() string {
	e := d.SelectedEntry()
	if e.Path == "" {
		return ""
	}
	return e.Path
}

// SelectedEntry returns the highlighted row. The zero Entry means
// "no selection" (hidden dropdown, empty list, or the synthetic
// "Type to search" placeholder).
func (d *atFileDropdown) SelectedEntry() atfiles.Entry {
	if !d.visible || d.idx < 0 || d.idx >= len(d.candidates) {
		return atfiles.Entry{}
	}
	return d.candidates[d.idx]
}

// MoveUp / MoveDown move the highlight. Clamped at the ends to
// match the slash-picker's no-wrap behaviour.
func (d *atFileDropdown) MoveUp() bool {
	if !d.visible || len(d.candidates) == 0 {
		return false
	}
	if d.idx <= 0 {
		return false
	}
	d.idx--
	return true
}

func (d *atFileDropdown) MoveDown() bool {
	if !d.visible || len(d.candidates) == 0 {
		return false
	}
	if d.idx >= len(d.candidates)-1 {
		return false
	}
	d.idx++
	return true
}

// Update re-derives the dropdown state from buffer + the runMode's
// working directory. Returns true when the overlay should redraw
// (callers can use this to skip a tea.Cmd on no-change updates).
//
// The buffer is the full input value (multi-line incl. newlines) —
// the atfiles.Parse walk handles the AGY 1.3.6 line-12 fix
// internally by treating the trailing run's leading '@' anywhere
// in the buffer as a trigger.
//
// cwd is the workspace directory the dropdown browses; pass the
// runMode's m.cwd when calling.
func (d *atFileDropdown) Update(buffer, cwd string) bool {
	m, ok := atfiles.Parse(buffer)
	if !ok {
		// No active mention. Reset but record the cwd so a
		// future Update with the same cwd doesn't churn the
		// directory cache.
		prev := d.visible || len(d.candidates) > 0
		d.Reset()
		d.cwd = cwd
		return prev
	}
	if d.cwd != cwd || d.all == nil {
		all, err := atfiles.List(cwd)
		if err != nil {
			d.all = nil
		} else {
			d.all = all
		}
	}
	d.cwd = cwd
	d.prefix = m.Prefix
	d.mentionStart = m.Start
	d.mentionEnd = m.End
	d.dirMode = strings.HasSuffix(m.Prefix, "/")
	d.queryMode = m.Prefix != "" && !d.dirMode

	d.candidates = atfiles.FilterEntries(d.all, m.Prefix)
	d.visible = true
	d.empty = len(d.candidates) == 0
	if d.idx < 0 || d.idx >= len(d.candidates) {
		d.idx = 0
	}
	return true
}

// TabCompletion returns the text that should replace the
// @-mention substring in the input buffer when the user presses
// Tab or Enter with the dropdown visible. The replacement is the
// highlighted row's workspace-relative path; directories already
// carry their trailing slash. 1.4.3 does not append a space:
// after completing a directory with Tab the picker stays open and
// lists that directory's children.
//
// Returns "" when the dropdown is empty / hidden.
func (d *atFileDropdown) TabCompletion() (replacement string, start, end int) {
	if !d.visible || len(d.candidates) == 0 {
		return "", 0, 0
	}
	e := d.SelectedEntry()
	if e.Rel == "" {
		return "", 0, 0
	}
	return e.Rel, d.mentionStart, d.mentionEnd
}

// view renders the dropdown overlay rows that go between the
// bottom separator and the status row. Returns "" when the
// overlay is hidden, matching pickerView's contract.
//
// Visual layout (matches the AGY slash-picker's cycle-126
// styling: 5-row window, "↑ N more" / "↓ N more" tails, hint row
// at the bottom in the recovered key/label palette).
func (d *atFileDropdown) view(width int) string {
	if !d.visible {
		return ""
	}
	const visible = 5
	rowWidth := width
	if rowWidth <= 0 {
		rowWidth = 120
	}
	var b strings.Builder

	// Column width. 1.4.3 uses a fixed 33-cell name field for the
	// empty-query workspace listing (including the "> " marker), so
	// absolute paths start at column 45. Directory browsing and the
	// synthetic placeholder instead let the name column grow and
	// truncate the absolute path to "/...".
	nameWidth := 33
	if d.queryMode {
		nameWidth = 0
	} else if d.prefix != "" {
		nameWidth = rowWidth - 18
		if nameWidth < 20 {
			nameWidth = 20
		}
	}

	if len(d.candidates) == 0 {
		// 1.4.3's no-match state is a synthetic selected row, not
		// the slash picker's "No matches" line. It always uses the
		// column layout even in query mode.
		renderAtFileRow(&b, 33, rowWidth, true,
			"Type to search workspace files...",
			"File", "Keep typing to filter files or directories", true)
		for i := 1; i < visible; i++ {
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
		b.WriteByte('\n')
		b.WriteString(atFileHintRow())
		return b.String()
	}

	total := len(d.candidates)
	hi := d.idx
	if hi < 0 {
		hi = 0
	}
	wantOffset := 0
	if hi >= visible-1 {
		wantOffset = hi - visible/2
	}
	if wantOffset > hi {
		wantOffset = hi
	}
	if wantOffset > total-visible {
		wantOffset = total - visible
	}
	if wantOffset < 0 {
		wantOffset = 0
	}
	if wantOffset > 0 {
		more := wantOffset
		b.WriteString(pickerDescDim)
		if more == 1 {
			b.WriteString("   \u2191 1 more")
		} else {
			fmt.Fprintf(&b, "   \u2191 %d more", more)
		}
		b.WriteString(pickerReset)
		b.WriteByte('\n')
	}
	for i := 0; i < visible; i++ {
		row := i + wantOffset
		selected := row == hi
		if row >= total {
			// The live picker always reserves five list rows,
			// even when a query returns fewer matches.
			b.WriteByte('\n')
			continue
		}
		e := d.candidates[row]
		if d.queryMode {
			prefix := "  "
			if selected {
				prefix = "> "
			}
			if selected {
				b.WriteString(pickerNameHi)
			}
			b.WriteString(prefix)
			b.WriteString(e.Rel)
			if selected {
				b.WriteString(pickerReset)
			}
			b.WriteByte('\n')
			continue
		}
		kind := "File"
		if e.IsDir {
			kind = "Directory"
		}
		renderAtFileRow(&b, nameWidth, rowWidth, selected, e.Rel, kind, e.Path, false)
	}
	rowsBelow := total - (wantOffset + visible)
	if rowsBelow > 0 {
		b.WriteString(pickerDescDim)
		if rowsBelow == 1 {
			b.WriteString("   \u2193 1 more")
		} else {
			fmt.Fprintf(&b, "   \u2193 %d more", rowsBelow)
		}
		b.WriteString(pickerReset)
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	b.WriteByte('\n')
	b.WriteString(atFileHintRow())
	return b.String()
}

// renderAtFileRow emits one column-mode row: name, type, path.
// The name field includes the "> " / "  " marker in 1.4.3's empty
// listing; longer names (the synthetic placeholder) overflow it
// instead of being truncated. The absolute path collapses to
// "/..." when there is not enough horizontal room.
func renderAtFileRow(b *strings.Builder, nameWidth, rowWidth int, selected bool, name, kind, path string, placeholder bool) {
	prefix := "  "
	if selected {
		prefix = "> "
	}
	label := prefix + name
	// The live picker drops the type/path columns when a row's
	// name no longer fits the name field, and hard-truncates the
	// name at the terminal edge. This is visible when scrolling
	// into the deep `out/` / `re-decompiled-*` entries.
	if !placeholder && nameWidth > 0 && len(label) > nameWidth {
		if len(label) > rowWidth {
			label = label[:rowWidth]
		}
		if selected {
			b.WriteString(pickerNameHi)
		}
		b.WriteString(label)
		if selected {
			b.WriteString(pickerReset)
		}
		b.WriteByte('\n')
		return
	}
	pad := ""
	if nameWidth > 0 {
		if len(label) < nameWidth {
			pad = strings.Repeat(" ", nameWidth-len(label))
		} else if placeholder {
			pad = "  "
		}
	}
	if selected {
		b.WriteString(pickerNameHi)
	}
	b.WriteString(label)
	if selected {
		b.WriteString(pickerReset)
	}
	b.WriteString(pad)
	b.WriteString(pickerDescDim)
	fmt.Fprintf(b, "%-12s", kind)
	avail := rowWidth - len(label) - len(pad) - 12
	if avail < 4 {
		avail = 4
	}
	if len(path) > avail {
		path = "/..."
	} else if !placeholder {
		// Keep the path cell exactly as wide as the terminal when
		// the path is short: the reference emits no padding, so
		// leave it alone.
	}
	b.WriteString(path)
	b.WriteString(pickerReset)
	b.WriteByte('\n')
}

// atFileHintRow is the keyboard hint strip rendered under the
// candidate list. It reuses the slash-picker's key/label palette
// (cycles 91/126) so the visual treatment is consistent across
// the two overlays: brand-blue keys, dim-gray labels.
func atFileHintRow() string {
	var b strings.Builder
	key := func(s string) string { return pickerKeyOpen + s + pickerKeyClose }
	label := func(s string) string { return pickerDescDim + s }
	b.WriteString("  ")
	b.WriteString(key("\u2191/\u2193") + label(" Navigate \u00b7 "))
	b.WriteString(key("enter") + label(" Select \u00b7 "))
	b.WriteString(key("tab") + label(" Complete"))
	b.WriteString(pickerReset)
	b.WriteByte('\n')
	return b.String()
}

// tabSpliceAtFile applies TabCompletion to the textarea value:
// replaces the @-mention substring with the highlighted candidate
// path and re-parses the buffer through the dropdown so the
// overlay stays in sync. Returns true when the textarea was
// actually mutated; false when the dropdown was hidden / empty.
//
// Splice semantics: drop the leading '@' and the typed prefix,
// keep everything before 'mentionStart' and everything after
// 'mentionEnd', join with the candidate path + trailing space.
// Callers should re-derive the dropdown state after the splice.
func (m *runMode) tabSpliceAtFile(keepOpen bool) bool {
	if !m.atFileDropdown.Visible() {
		return false
	}
	repl, start, end := m.atFileDropdown.TabCompletion()
	if repl == "" {
		return false
	}
	cur := m.input.Value()
	if start < 0 || end > len(cur) || start > end {
		return false
	}
	next := cur[:start] + "@" + repl + cur[end:]
	m.input.SetValue(next)
	if keepOpen {
		// 1.4.3: Tab completes the highlighted row and keeps the
		// picker open. Completing a directory re-roots the list at
		// that directory; completing a file narrows it to that
		// exact path.
		m.atFileDropdown.Update(next, m.cwd)
	} else {
		// Enter inserts the same text but closes the overlay.
		m.atFileDropdown.Reset()
	}
	return true
}

// atFileView exposes the dropdown overlay rows for viewBody. It
// mirrors pickerView's view-slot signature — return "" when
// hidden so viewBody can ignore us. The terminal width is plumbed
// through so the "no matches" row can pad out to the row width.
func (m *runMode) atFileView() string {
	if !m.atFileDropdown.Visible() {
		return ""
	}
	return m.atFileDropdown.view(m.width)
}
