// Package permissionspanel holds the /permissions panel: the scope
// picker, the per-scope permission lists and the add/edit rule form.
//
// # What it is
//
// The reference (`agy`) offers a permission config editor with three
// config scopes — Project, Shared with Pigasus and Global — and, per
// scope, three permission lists — allowlist, denylist and asklist. The
// panel walks the user through three stages: the scope picker, the tab
// view of one scope's lists, and the add/edit rule form.
//
// # Where it lives
//
// The panel itself is here: the three stages' rows, the cursor, the keys
// and the writes. The host keeps only the seams — the way out of the
// panel.
//
// The panel is rebuilt every time it opens, so a stale copy of the
// cursor, the scope, the tab or the form's text is never shown.
//
// # Where the rules live
//
// The rule storage and the validation are the permission policy's
// (`internal/core/policy/permissions`): the panel reads and writes
// through it, and the policy's own file layout and messages are the
// reference's.
package permissionspanel

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	settings "github.com/justwasm/pigasus/internal/core/settings"
)

// The panel's three stages.
const (
	// StageScopes is the scope picker: the list of config scopes.
	StageScopes = iota
	// StageLists is the tab view: one scope's three permission lists.
	StageLists
	// StageForm is the add/edit rule form.
	StageForm
)

// The form's two modes.
const (
	// FormAdd adds a new rule.
	FormAdd = 1
	// FormEdit edits the rule under the cursor.
	FormEdit = 2
)

// scopes are the three config scopes the editor offers. All three blurbs
// were recovered by driving REF's /permissions panel with Down
// (/tmp/agycmp/nav-permissions.log).
var scopes = [3][2]string{
	{"Project", "Rules that apply only to this project (highest priority)"},
	{"Shared with Pigasus", "Rules that apply to all Pigasus products"},
	{"Global", "Global rules"},
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

// Panel is the /permissions panel.
//
// The panel owns the whole surface: the scope picker, the per-scope
// lists, the add/edit form and the writes that back them. Nothing of it
// is left in the host but the seams.
type Panel struct {
	seams Seams

	// stage is the panel's stage: the scope picker, the tab view or the
	// add/edit form.
	stage int

	// scope is the selected scope: 0 Project, 1 Shared, 2 Global.
	scope int

	// tab is the selected list: 0 allowlist, 1 denylist, 2 asklist.
	tab int

	// form is the form's mode: FormAdd or FormEdit.
	form int

	// edit is the row index being edited (form == FormEdit).
	edit int

	// text is the form's input buffer.
	text string

	// errMsg is the form's last rejection message.
	errMsg string

	// shared holds the "Shared with Pigasus" scope's three lists for
	// the session. The reference persists them somewhere this probe did
	// not locate (存疑); keeping them in memory makes the add/edit/delete
	// actions render correctly without inventing an on-disk target.
	shared [3][]string

	// cursor is the selected row of the current stage.
	cursor int
}

// New builds the /permissions panel.
func New(seams Seams) *Panel {
	return &Panel{seams: seams}
}

// ---- the panel's state, read from outside --------------------------------

// Stage is the panel's stage: StageScopes, StageLists or StageForm.
func (p *Panel) Stage() int {
	if p == nil {
		return StageScopes
	}
	return p.stage
}

// Scope is the selected scope: 0 Project, 1 Shared, 2 Global.
func (p *Panel) Scope() int {
	if p == nil {
		return permissions.PermScopeProject
	}
	return p.scope
}

// Tab is the selected list: 0 allowlist, 1 denylist, 2 asklist.
func (p *Panel) Tab() int {
	if p == nil {
		return 0
	}
	return p.tab
}

// Form is the form's mode: FormAdd or FormEdit.
func (p *Panel) Form() int {
	if p == nil {
		return FormAdd
	}
	return p.form
}

// Text is the form's input buffer.
func (p *Panel) Text() string {
	if p == nil {
		return ""
	}
	return p.text
}

// Err is the form's last rejection message.
func (p *Panel) Err() string {
	if p == nil {
		return ""
	}
	return p.errMsg
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

// RowCount is the number of selectable rows of the current stage.
//
// The scope picker has three rows, the tab view has as many rows as the
// selected list holds and the form has one (its input).
func (p *Panel) RowCount() int {
	if p == nil {
		return len(scopes)
	}
	switch p.stage {
	case StageLists:
		return len(p.readList(p.scope, p.tab))
	case StageForm:
		return 1
	}
	return len(scopes)
}

// Shared is the session-local list of the "Shared with Pigasus" scope.
func (p *Panel) Shared(tab int) []string {
	if p == nil || tab < 0 || tab > 2 {
		return nil
	}
	return append([]string(nil), p.shared[tab]...)
}

// ---- the seams ------------------------------------------------------------

func (p *Panel) close() tea.Cmd {
	if p == nil {
		return nil
	}
	if p.seams.Close != nil {
		return p.seams.Close()
	}
	return DismissCmd()
}

// ---- the rule storage -----------------------------------------------------

// readList returns the rules of one (scope, tab) list.
//
// The Global scope lives in settings.json, the Project scope in the
// reference's default CLI project file and the Shared scope in the
// session.
func (p *Panel) readList(scope, tab int) []string {
	if tab < 0 || tab > 2 {
		return nil
	}
	switch scope {
	case permissions.PermScopeGlobal:
		return permissions.ReadSettingsPermList(tab)
	case permissions.PermScopeProject:
		return permissions.ReadProjectPermList(tab)
	default:
		return append([]string(nil), p.shared[tab]...)
	}
}

// writeList persists one (scope, tab) list.
func (p *Panel) writeList(scope, tab int, list []string) {
	if tab < 0 || tab > 2 {
		return
	}
	if list == nil {
		list = []string{}
	}
	switch scope {
	case permissions.PermScopeGlobal:
		_ = settings.SetField("permissions."+permissions.PermListKeys[tab], list)
	case permissions.PermScopeProject:
		permissions.WriteProjectPermList(tab, list)
	default:
		p.shared[tab] = append([]string(nil), list...)
	}
}
