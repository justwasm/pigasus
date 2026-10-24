package atfiles

import (
	"strings"
)

// Dropdown is the state machine for the @ file-mention overlay.
type Dropdown struct {
	visible      bool
	empty        bool
	candidates   []Entry
	all          []Entry
	dirMode      bool
	queryMode    bool
	prefix       string
	mentionStart int
	mentionEnd   int
	idx          int
	cwd          string
}

// Reset hides the dropdown and clears the candidate list.
func (d *Dropdown) Reset() {
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
func (d *Dropdown) Visible() bool { return d.visible }

// Empty reports whether the dropdown is visible but has no candidates.
func (d *Dropdown) Empty() bool { return d.visible && len(d.candidates) == 0 }

// Len returns candidate count.
func (d *Dropdown) Len() int { return len(d.candidates) }

// Prefix returns the active prefix.
func (d *Dropdown) Prefix() string { return d.prefix }

// Candidates returns the current candidate list.
func (d *Dropdown) Candidates() []Entry { return d.candidates }

// Idx returns the highlighted row index, or -1 when empty.
func (d *Dropdown) Idx() int {
	if len(d.candidates) == 0 {
		return -1
	}
	return d.idx
}

// Selected returns the highlighted candidate path, or "" when none.
func (d *Dropdown) Selected() string {
	e := d.SelectedEntry()
	return e.Path
}

// SelectedEntry returns the highlighted entry.
func (d *Dropdown) SelectedEntry() Entry {
	if !d.visible || d.idx < 0 || d.idx >= len(d.candidates) {
		return Entry{}
	}
	return d.candidates[d.idx]
}

// MoveUp moves highlight up. Returns true if moved.
func (d *Dropdown) MoveUp() bool {
	if !d.visible || len(d.candidates) == 0 || d.idx <= 0 {
		return false
	}
	d.idx--
	return true
}

// MoveDown moves highlight down. Returns true if moved.
func (d *Dropdown) MoveDown() bool {
	if !d.visible || len(d.candidates) == 0 || d.idx >= len(d.candidates)-1 {
		return false
	}
	d.idx++
	return true
}

// Update re-derives dropdown state from buffer and cwd.
func (d *Dropdown) Update(buffer, cwd string) bool {
	m, ok := Parse(buffer)
	if !ok {
		prev := d.visible || len(d.candidates) > 0
		d.Reset()
		d.cwd = cwd
		return prev
	}
	if d.cwd != cwd || d.all == nil {
		all, err := List(cwd)
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

	d.candidates = FilterEntries(d.all, m.Prefix)
	d.visible = true
	d.empty = len(d.candidates) == 0
	if d.idx < 0 || d.idx >= len(d.candidates) {
		d.idx = 0
	}
	return true
}

// TabCompletion returns the replacement text and buffer span for Tab/Enter.
func (d *Dropdown) TabCompletion() (replacement string, start, end int) {
	if !d.visible || len(d.candidates) == 0 {
		return "", 0, 0
	}
	e := d.SelectedEntry()
	if e.Rel == "" {
		return "", 0, 0
	}
	return e.Rel, d.mentionStart, d.mentionEnd
}
