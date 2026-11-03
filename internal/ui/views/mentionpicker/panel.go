// Package mentionpicker is the "@" file-mention dropdown: the overlay
// the composer raises above the prompt block when the buffer ends in an
// "@<token>" mention.
//
// The panel owns the overlay's state — the candidate list, the
// highlight, the mention token's span — and the rows it writes. The
// host that owns the composer keeps the facts the panel cannot know
// (the composer's text, the workspace root, the terminal's width) and
// hands them to the panel as seams.
//
// 【一手】 agy 1.4.3, live: the dropdown renders five visible rows, the
// "↑ N more" / "↓ N more" tails, a blank separator and the
// "↑/↓ Navigate · enter Select · tab Complete" hint row. The empty
// listing renders the placeholder row "Type to search workspace
// files..." with the "File" type column.
//
// Lifecycle:
//
//	idle  the buffer does not end in an "@<token>" mention
//	shown the buffer ends in a mention and at least one candidate
//	      matches
//	empty the buffer ends in a mention and no candidate matches
//
// The behaviour matches the v1.3.6 binary's captures:
//
//	"@ma"      -> dropdown open, the matching files listed
//	"@"        -> dropdown open, the workspace listing
//	Up arrow   -> the previous candidate
//	Down arrow -> the next candidate
//	Tab        -> completes the mention into the composer and keeps the
//	               dropdown open
//	Enter      -> completes the mention into the composer and closes the
//	               dropdown
//	Esc        -> closes the dropdown
//
// The mention grammar and the workspace listing live in
// internal/app/prompt/mentions/files: they are the facts, and the
// dropdown is the view over them.
package mentionpicker

import (
	"strings"

	atfiles "github.com/justwasm/pigasus/internal/app/prompt/mentions/files"
)

// Seams are what the panel needs from the host that owns it.
//
// A nil seam reads as its zero value: the panel sees an empty
// composer, an empty workspace and a zero width. The panel never
// reaches into the host.
type Seams struct {
	// Buffer returns the composer's current text.
	Buffer func() string
	// SetBuffer replaces the composer's text.
	SetBuffer func(s string)
	// Cwd returns the workspace root the candidate list is built
	// from. The panel re-scans the workspace when it changes.
	Cwd func() string
	// Width returns the terminal's width in columns.
	Width func() int
}

// Panel is the "@" file-mention dropdown.
//
// Every method is safe on a nil panel: a host that never built the
// dropdown answers "not visible" and writes nothing.
type Panel struct {
	seams Seams

	// visible is true while the overlay should be rendered.
	visible bool
	// candidates is the filtered row set (recomputed on every
	// keystroke).
	candidates []atfiles.Entry
	// all is the scanned workspace listing, cached per cwd.
	all []atfiles.Entry
	// dirMode is true when the mention token ends in "/" — the
	// dropdown then lists the directory's direct children.
	dirMode bool
	// queryMode is true when the mention token is a query: the rows
	// show the relative path alone, without the type and path
	// columns.
	queryMode bool
	// prefix is the text the user typed after the "@".
	prefix string
	// mentionStart and mentionEnd are the byte offsets of the "@" and
	// of the end of the typed token — the span the completion
	// replaces.
	mentionStart int
	mentionEnd   int
	// idx is the highlighted row. -1 when the dropdown holds no row.
	idx int
	// cwd is the workspace root the cached listing belongs to.
	cwd string
}

// New builds the dropdown with the host's seams.
func New(seams Seams) *Panel {
	return &Panel{seams: seams, idx: -1}
}

// Reset hides the dropdown and clears the candidate list.
//
// The host calls it whenever the mention token is gone — the buffer no
// longer names a file — and on the Esc path.
func (p *Panel) Reset() {
	if p == nil {
		return
	}
	p.visible = false
	p.candidates = nil
	p.all = nil
	p.dirMode = false
	p.queryMode = false
	p.prefix = ""
	p.mentionStart = 0
	p.mentionEnd = 0
	p.idx = -1
}

// Visible reports whether the dropdown overlay should be drawn.
func (p *Panel) Visible() bool {
	return p != nil && p.visible
}

// Empty reports whether the dropdown is visible but holds no
// candidate.
func (p *Panel) Empty() bool {
	return p != nil && p.visible && len(p.candidates) == 0
}

// Len returns the candidate count.
func (p *Panel) Len() int {
	if p == nil {
		return 0
	}
	return len(p.candidates)
}

// Prefix returns the active prefix — the text the user typed after
// the "@".
func (p *Panel) Prefix() string {
	if p == nil {
		return ""
	}
	return p.prefix
}

// Idx returns the highlighted row's index, or -1 when the dropdown
// holds no row.
func (p *Panel) Idx() int {
	if p == nil || len(p.candidates) == 0 {
		return -1
	}
	return p.idx
}

// Selected returns the highlighted candidate's path, or "" when there
// is none.
func (p *Panel) Selected() string {
	if p == nil {
		return ""
	}
	return p.selectedEntry().Path
}

// selectedEntry returns the highlighted entry.
func (p *Panel) selectedEntry() atfiles.Entry {
	if p == nil || !p.visible || p.idx < 0 || p.idx >= len(p.candidates) {
		return atfiles.Entry{}
	}
	return p.candidates[p.idx]
}

// MoveUp moves the highlight up. It reports whether the highlight
// moved.
func (p *Panel) MoveUp() bool {
	if p == nil || !p.visible || len(p.candidates) == 0 || p.idx <= 0 {
		return false
	}
	p.idx--
	return true
}

// MoveDown moves the highlight down. It reports whether the highlight
// moved.
func (p *Panel) MoveDown() bool {
	if p == nil || !p.visible || len(p.candidates) == 0 || p.idx >= len(p.candidates)-1 {
		return false
	}
	p.idx++
	return true
}

// Update re-derives the dropdown's state from the composer's text and
// the workspace root.
//
// It reports whether the dropdown was visible before the call — the
// host redraws when it was.
//
// The rules:
//
//   - the composer does not end in an "@<token>" mention -> hidden
//   - the composer ends in a mention -> visible, and the candidates
//     are the workspace entries the mention matches
//
// The workspace is scanned once per cwd and re-filtered on every
// keystroke.
func (p *Panel) Update() bool {
	if p == nil {
		return false
	}
	cwd := p.cwdValue()
	m, ok := atfiles.Parse(p.buffer())
	if !ok {
		prev := p.visible || len(p.candidates) > 0
		p.Reset()
		p.cwd = cwd
		return prev
	}
	if p.cwd != cwd || p.all == nil {
		all, err := atfiles.List(cwd)
		if err != nil {
			p.all = nil
		} else {
			p.all = all
		}
	}
	p.cwd = cwd
	p.prefix = m.Prefix
	p.mentionStart = m.Start
	p.mentionEnd = m.End
	p.dirMode = strings.HasSuffix(m.Prefix, "/")
	p.queryMode = m.Prefix != "" && !p.dirMode

	p.candidates = atfiles.FilterEntries(p.all, m.Prefix)
	p.visible = true
	if p.idx < 0 || p.idx >= len(p.candidates) {
		p.idx = 0
	}
	return true
}

// Complete replaces the composer's mention token with the highlighted
// candidate.
//
// keepOpen keeps the dropdown up — the Tab path; the Enter path closes
// it. It reports whether the composer was changed.
//
// A completion that cannot be spliced (no candidate, a candidate
// without a relative path, a span that does not fit the composer)
// leaves the composer alone.
func (p *Panel) Complete(keepOpen bool) bool {
	if p == nil || !p.visible {
		return false
	}
	repl, start, end := p.tabCompletion()
	if repl == "" {
		return false
	}
	cur := p.buffer()
	if start < 0 || end > len(cur) || start > end {
		return false
	}
	next := cur[:start] + "@" + repl + cur[end:]
	p.setBuffer(next)
	if keepOpen {
		p.Update()
	} else {
		p.Reset()
	}
	return true
}

// tabCompletion returns the replacement text and the buffer span it
// replaces.
func (p *Panel) tabCompletion() (replacement string, start, end int) {
	if p == nil || !p.visible || len(p.candidates) == 0 {
		return "", 0, 0
	}
	e := p.selectedEntry()
	if e.Rel == "" {
		return "", 0, 0
	}
	return e.Rel, p.mentionStart, p.mentionEnd
}

// buffer returns the composer's text.
func (p *Panel) buffer() string {
	if p == nil || p.seams.Buffer == nil {
		return ""
	}
	return p.seams.Buffer()
}

// setBuffer replaces the composer's text.
func (p *Panel) setBuffer(s string) {
	if p == nil || p.seams.SetBuffer == nil {
		return
	}
	p.seams.SetBuffer(s)
}

// cwdValue returns the workspace root.
func (p *Panel) cwdValue() string {
	if p == nil || p.seams.Cwd == nil {
		return ""
	}
	return p.seams.Cwd()
}

// width returns the terminal's width.
func (p *Panel) width() int {
	if p == nil || p.seams.Width == nil {
		return 0
	}
	return p.seams.Width()
}
