// Package feedbackpanel holds the /feedback panel: the category picker
// and the free-text form the picker leads to.
//
// # What it is
//
// The reference (`agy`) asks what went wrong before it takes a report.
// The first stage lists six categories; the second stage shows the
// chosen category back and offers an optional free-text field.
//
// # Where it lives
//
// The panel itself is here: the two stages, the six categories, the
// cursor, the buffer and the keys. The host keeps only the seams —
// what to do with a submitted report and how to leave the panel.
//
// The panel is rebuilt every time it opens, so a stale draft or a stale
// stage is never shown.
//
// # The categories
//
// The six categories are the reference's own, transcribed from
// /tmp/agycmp/panels/ppref.feedback.raw.txt. The panel lists them with
// their picker index ("1. ", "2. ", …) and echoes the chosen one in
// the second stage without it — the reference's own two shapes.
package feedbackpanel

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Categories are the reference's six feedback categories, with the
// picker index the reference prints in front of each one.
var Categories = [6]string{
	"1. Code doesn't work (e.g., syntax errors, bad logic)",
	"2. Repeated itself (e.g., stuck running the same tool over again)",
	"3. Took too many steps (e.g., progress was much too slow)",
	"4. Ignored instructions (e.g., missed specific constraints)",
	"5. Got stuck (e.g., unable to recover from an error)",
	"6. Other",
}

// Seams are the host's answers the panel cannot answer itself.
//
// Every one of them is optional: a panel built without a seam behaves
// as if the seam were answered with "nothing happened", which keeps the
// package usable from a test without a running TUI.
type Seams struct {
	// Submit records the report and leaves the panel.
	//
	// The reference's own report goes nowhere in the clone — there is
	// no upload endpoint — so the host records it locally and writes
	// the message. The panel does not own the transcript block.
	Submit func()
	// Close leaves the panel without submitting.
	Close func() tea.Cmd
}

// Panel is the /feedback panel.
//
// The panel owns the whole surface: the two stages, the categories, the
// cursor, the free-text buffer and the keys. Nothing of it is left in
// the host but the seams.
type Panel struct {
	seams Seams

	// cursor is the selected category.
	cursor int

	// stage is 0 for the category picker and 1 for the free-text
	// form.
	stage int

	// text is the free-text form's buffer.
	text string
}

// New builds the /feedback panel.
func New(seams Seams) *Panel {
	return &Panel{seams: seams}
}

// ---- the panel's state, read from outside --------------------------------

// Cursor is the selected category's index.
func (p *Panel) Cursor() int {
	if p == nil {
		return 0
	}
	return p.cursor
}

// SetCursor puts the cursor on the category at the given index.
//
// The index is clamped into the category list, so a stale index cannot
// strand the cursor on a category that is not on screen.
func (p *Panel) SetCursor(idx int) {
	if p == nil {
		return
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= len(Categories) {
		idx = len(Categories) - 1
	}
	p.cursor = idx
}

// Stage is the panel's stage: 0 for the category picker, 1 for the
// free-text form.
func (p *Panel) Stage() int {
	if p == nil {
		return 0
	}
	return p.stage
}

// Text is the free-text form's buffer.
func (p *Panel) Text() string {
	if p == nil {
		return ""
	}
	return p.text
}

// RowCount is the number of selectable rows: the six categories.
func (p *Panel) RowCount() int {
	return len(Categories)
}

// ---- the panel's seams ----------------------------------------------------

func (p *Panel) submit() {
	if p == nil || p.seams.Submit == nil {
		return
	}
	p.seams.Submit()
}

func (p *Panel) close() tea.Cmd {
	if p == nil || p.seams.Close == nil {
		return nil
	}
	return p.seams.Close()
}

// CategoryLabel drops the `N. ` picker index from a category row.
//
// The reference's second stage echoes the label alone (its capture has
// "Feedback: Code doesn't work (e.g., syntax errors, bad logic)") while
// the picker lists it as "1. Code doesn't work…".
func CategoryLabel(cat string) string {
	i := strings.Index(cat, ". ")
	if i <= 0 || i > 3 {
		return cat
	}
	for _, r := range cat[:i] {
		if r < '0' || r > '9' {
			return cat
		}
	}
	return cat[i+2:]
}
