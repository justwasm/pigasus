// Package hookspanel holds the /hooks panel: the hook-type list, the
// per-type matcher list and the add-matcher / add-hook form.
//
// # What it is
//
// The reference (`agy`) offers a hook editor with three stages: the list
// of the hook types the CLI ships, the matcher list of one type, and the
// form that adds a matcher (for the two tool hooks) or a hook (for the
// lifecycle hooks).
//
// # Where it lives
//
// The panel itself is here: the three stages' rows, the cursor, the keys
// and the state. The host keeps only the seams — the way out of the
// panel.
//
// The panel is rebuilt every time it opens, so a stale copy of the
// cursor, the stage or the form's text is never shown.
//
// # What the panel does not do
//
// The form collects a name and a matcher but persists neither: the
// reference's own hook store was not located, so the clone shows the
// editor without claiming a rule was saved (存疑 — see the cycle
// report).
package hookspanel

import (
	tea "charm.land/bubbletea/v2"

	corehooks "github.com/justwasm/pigasus/internal/core/policy/hooks"
)

// The panel's three stages.
const (
	// StageTypes is the type list: the hook types the CLI ships.
	StageTypes = iota
	// StageMatchers is the matcher list of one hook type.
	StageMatchers
	// StageForm is the add-matcher or add-hook form.
	StageForm
)

// hookType is one row of the type list: the event the row stands for and
// the description the panel shows next to it.
type hookType struct {
	event       corehooks.Event
	description string
}

// types are the hook types the panel lists.
//
// The event names belong to the core tier — internal/core/policy/hooks
// owns the vocabulary the reference documents — and the panel reads them
// from there. The descriptions are static in REF.
//
// 【一手】 the reference's own /hooks panel (cycle 205,
// hooks-enter.raw.txt) named five of the six; SessionStart is the sixth,
// and the official documentation names it.
//
// The reference's own five keep their order: SessionStart is appended,
// so the rows the reference showed stay where they were.
var types = []hookType{
	{corehooks.EventPreToolUse, "Before tool execution"},
	{corehooks.EventPostToolUse, "After tool execution"},
	{corehooks.EventPreInvocation, "Before each LLM invocation"},
	{corehooks.EventPostInvocation, "After each LLM invocation"},
	{corehooks.EventStop, "When agent tries to exit"},
	{corehooks.EventSessionStart, "When a session starts"},
}

// IsTool reports whether the hook type at idx uses the tool-matcher
// form.
//
// The two tool hooks (PreToolUse, PostToolUse) match on a tool name;
// the lifecycle hooks match on a shell command.
func IsTool(idx int) bool {
	if idx < 0 || idx >= len(types) {
		return false
	}
	return types[idx].event.UsesMatcher()
}

// selectableRows is the number of selectable rows of a type's matcher
// list: a tool hook offers the matcher row and the "match all" row, a
// lifecycle hook only the matcher row.
func selectableRows(idx int) int {
	if IsTool(idx) {
		return 2
	}
	return 1
}

// Seams are the host's answers the panel cannot answer itself.
//
// Every one of them is optional: a panel built without a seam behaves as
// if the seam were answered with "nothing happened", which keeps the
// package usable from a test without a running TUI.
type Seams struct {
	// Close leaves the panel.
	Close func() tea.Cmd
}

// Panel is the /hooks panel.
//
// The panel owns the whole surface: the three stages, the cursor, the
// keys and the state. Nothing of it is left in the host but the seams.
type Panel struct {
	seams Seams

	// stage is the panel's stage: the type list, the matcher list or the
	// form.
	stage int

	// idx is the selected hook type (an index into types).
	idx int

	// field is the focused field of the add-hook form: 0 name, 1 command,
	// 2 timeout.
	field int

	// match is the add-matcher form's input buffer.
	match string

	// name is the add-hook form's name buffer.
	name string

	// cursor is the selected row of the current stage.
	cursor int
}

// New builds the /hooks panel.
func New(seams Seams) *Panel {
	return &Panel{seams: seams}
}

// ---- the panel's state, read from outside --------------------------------

// Stage is the panel's stage: StageTypes, StageMatchers or StageForm.
func (p *Panel) Stage() int {
	if p == nil {
		return StageTypes
	}
	return p.stage
}

// Idx is the selected hook type: an index into the types.
func (p *Panel) Idx() int {
	if p == nil {
		return 0
	}
	return p.idx
}

// Field is the focused field of the add-hook form.
func (p *Panel) Field() int {
	if p == nil {
		return 0
	}
	return p.field
}

// Match is the add-matcher form's input buffer.
func (p *Panel) Match() string {
	if p == nil {
		return ""
	}
	return p.match
}

// Name is the add-hook form's name buffer.
func (p *Panel) Name() string {
	if p == nil {
		return ""
	}
	return p.name
}

// Cursor is the selected row's index.
func (p *Panel) Cursor() int {
	if p == nil {
		return 0
	}
	return p.cursor
}

// SetCursor puts the cursor on the row at the given index.
func (p *Panel) SetCursor(idx int) {
	if p == nil {
		return
	}
	p.cursor = idx
}

// RowCount is the number of selectable rows of the current stage: the
// hook types, a type's matcher rows, or the form's single input.
func (p *Panel) RowCount() int {
	if p == nil {
		return len(types)
	}
	switch p.stage {
	case StageMatchers:
		return selectableRows(p.idx)
	case StageForm:
		return 1
	}
	return len(types)
}

// ---- the seams ------------------------------------------------------------

func (p *Panel) close() tea.Cmd {
	if p == nil || p.seams.Close == nil {
		return nil
	}
	return p.seams.Close()
}
