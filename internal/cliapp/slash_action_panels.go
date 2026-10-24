// slash_action_panels.go — cycle 205: the key actions of the four action panels
// (/permissions, /mcp, /agents, /hooks).
//
// What is implemented:
//   /permissions  stage 1 (scope list) → stage 2 (allow / deny / ask tabs) →
//                 stage 3 add/edit rule form with validation and persistence.
//   /hooks        stage 1 (type list) → stage 2 (matcher list) → stage 3 forms.
//   /agents       Enter echoes "Already using default agent." and keeps panel open.
//   /mcp          empty-state frame.
//
// Hooks and agents panel implementation moved to slash_action_panels_hooks.go.

package cliapp

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/permissions"
)

// ---- permission scopes and lists -------------------------------------------

const (
	permScopeProject = permissions.PermScopeProject
	permScopeShared  = permissions.PermScopeShared
	permScopeGlobal  = permissions.PermScopeGlobal
)

// permScopeNames are the three scope rows.
var permScopeNames = permissions.PermScopeNames

// permListNames / permListKeys are the three permission lists in tab order.
var permListNames = permissions.PermListNames
var permListKeys = permissions.PermListKeys

// actionPanelState is the extra state the cycle-205 panels need. It hangs
// off slashPanel as a single field so the wiring in slash_panels.go stays
// one line.
type actionPanelState struct {
	// /permissions
	permStage int // 0 = scope list, 1 = tab view, 2 = add/edit rule form
	permScope int // 0 Project, 1 Shared, 2 Global
	permTab   int // 0 allowlist, 1 denylist, 2 asklist
	permForm  int // 1 = add, 2 = edit
	permEdit  int // row index being edited (permForm == 2)
	permText  string
	permErr   string
	// shared holds the "Shared with Pigasus" scope's three lists for
	// the session. The reference persists them somewhere this probe did
	// not locate (存疑); keeping them in memory makes the add/edit/delete
	// actions render correctly without inventing an on-disk target.
	shared [3][]string

	// /hooks
	hookStage int // 0 = type list, 1 = matcher list, 2 = form
	hookIdx   int // selected hook type (index into hookRows)
	hookField int // focused field in the add-hook form (0 name, 1 cmd, 2 timeout)
	hookMatch string
	hookName  string
}

// hookIsTool reports whether a hook type uses the tool-matcher form.
func hookIsTool(idx int) bool { return idx == 0 || idx == 1 }

// ---- permission list storage ----------------------------------------------

// permReadList returns the rules of one (scope, tab) list.
func (m *runMode) permReadList(scope, tab int) []string {
	if tab < 0 || tab > 2 {
		return nil
	}
	switch scope {
	case permScopeGlobal:
		return readSettingsPermList(tab)
	case permScopeProject:
		return readProjectPermList(tab)
	default:
		return append([]string(nil), m.panel.act.shared[tab]...)
	}
}

// permWriteList persists one (scope, tab) list.
func (m *runMode) permWriteList(scope, tab int, list []string) {
	if tab < 0 || tab > 2 {
		return
	}
	if list == nil {
		list = []string{}
	}
	switch scope {
	case permScopeGlobal:
		_ = auth.SetSettingsField("permissions."+permListKeys[tab], list)
	case permScopeProject:
		writeProjectPermList(tab, list)
	default:
		m.panel.act.shared[tab] = append([]string(nil), list...)
	}
}

func readSettingsPermList(tab int) []string { return permissions.ReadSettingsPermList(tab) }
func readProjectPermList(tab int) []string  { return permissions.ReadProjectPermList(tab) }
func writeProjectPermList(tab int, list []string) {
	permissions.WriteProjectPermList(tab, list)
}

// validateGrant delegates to permissions.ValidateGrant.
func validateGrant(rule string) string { return permissions.ValidateGrant(rule) }
func validGrantAction(a string) bool   { return permissions.ValidGrantAction(a) }

// ---- /permissions rendering ------------------------------------------------

// permissionsPanelLines dispatches the /permissions panel by stage.
func (m *runMode) permissionsPanelLines() []string {
	switch m.panel.act.permStage {
	case 1:
		return m.permissionsTabLines()
	case 2:
		return m.permissionsFormLines()
	}
	return m.panelPermissionsLines()
}

// permissionsTabLines is stage 1: the scope's three lists.
func (m *runMode) permissionsTabLines() []string {
	a := m.panel.act
	scope := a.permScope
	if scope < 0 || scope > 2 {
		scope = 0
	}
	var counts [3]int
	for i := 0; i < 3; i++ {
		counts[i] = len(m.permReadList(scope, i))
	}
	lines := []string{
		panelTitleOpen + "Permissions — " + permScopeNames[scope] + panelTitleClose,
		permTabRow(a.permTab, counts),
		"",
	}
	list := m.permReadList(scope, a.permTab)
	if len(list) == 0 {
		lines = append(lines, panelGray238+"No "+permListNames[a.permTab]+" rules."+panelGray238C)
	} else {
		for i, rule := range list {
			if i == m.panel.cursor {
				lines = append(lines, cBlueOpen+"> "+rule+cBlueClose)
			} else {
				lines = append(lines, "  "+rule)
			}
		}
	}
	lines = append(lines, "",
		panelKeyboardHint("↑/↓", "Navigate", "←/→", "Switch View",
			"a", "Add rule", "e", "Edit rule", "d/\u232b", "Delete rule",
			"esc", "Close"),
	)
	return lines
}

// permTabRow renders the allowlist / denylist / asklist tab strip.
func permTabRow(active int, counts [3]int) string {
	return permissions.PermTabRow(active, counts)
}

// permissionsFormLines is stage 2: the add/edit rule form.
func (m *runMode) permissionsFormLines() []string {
	a := m.panel.act
	scope := a.permScope
	if scope < 0 || scope > 2 {
		scope = 0
	}
	verb := "Add"
	if a.permForm == 2 {
		verb = "Edit"
	}
	lines := []string{
		panelTitleOpen + verb + " Rule — " + permScopeNames[scope] + " — " +
			permListNames[a.permTab] + panelTitleClose,
		"",
		panelGray238 + "Enter a permission rule:" + panelGray238C,
		"",
		a.permText + "\x1b[7m\x1b[37m \x1b[0m",
		"",
	}
	if a.permErr != "" {
		lines = append(lines, panelEffHighOpen+"✗ "+a.permErr+panelGray238C, "")
	}
	lines = append(lines,
		panelGray238+"Format: action(target)"+panelGray238C,
		"",
		// The reference separates the first two legend pairs with a
		// literal TAB (probe_perm_add.py, 3/3 runs). The clone's cell
		// renderer drops raw control characters, so a literal "\t" never
		// reaches the pane; two spaces preserve the separator instead
		// (see 存疑 in the cycle report).
		"\x1b[1mKeyboard: \x1b[0m"+cBlueOpen+"↑/↓"+cBlueClose+" "+
			cDimGrayOpen+"Navigate"+cDimGrayClose+"  "+
			cBlueOpen+"enter"+cBlueClose+" "+cDimGrayOpen+"Select"+cDimGrayClose+"  "+
			cBlueOpen+"esc"+cBlueClose+" "+cDimGrayOpen+"Close"+cDimGrayClose,
	)
	return lines
}

// ---- /permissions keys -----------------------------------------------------

func (m *runMode) handlePermissionsKey(key tea.Key) (bool, tea.Cmd) {
	a := &m.panel.act
	switch a.permStage {
	case 1:
		return m.handlePermissionsTabKey(key)
	case 2:
		return m.handlePermissionsFormKey(key)
	}
	// stage 0: scope list. ↓/↑ wrap (probe_verify2.py: Up at Project lands
	// on Global).
	switch key.Code {
	case tea.KeyUp:
		m.panel.cursor--
		if m.panel.cursor < 0 {
			m.panel.cursor = len(permScopeNames) - 1
		}
		return true, nil
	case tea.KeyDown:
		m.panel.cursor = (m.panel.cursor + 1) % len(permScopeNames)
		return true, nil
	case tea.KeyEnter:
		if m.panel.cursor < 0 || m.panel.cursor >= len(permScopeNames) {
			m.panel.cursor = 0
		}
		a.permScope = m.panel.cursor
		a.permStage = 1
		a.permTab = 0
		m.panel.cursor = 0
		return true, nil
	case tea.KeyEsc:
		return true, m.closePanel()
	}
	if key.Mod == 0 && key.Text != "" {
		return true, nil
	}
	return false, nil
}

func (m *runMode) handlePermissionsTabKey(key tea.Key) (bool, tea.Cmd) {
	a := &m.panel.act
	n := len(m.permReadList(a.permScope, a.permTab))
	switch key.Code {
	case tea.KeyUp:
		if m.panel.cursor > 0 {
			m.panel.cursor--
		}
		return true, nil
	case tea.KeyDown:
		if n > 0 && m.panel.cursor < n-1 {
			m.panel.cursor++
		}
		return true, nil
	case tea.KeyLeft:
		a.permTab = (a.permTab + 2) % 3
		m.panel.cursor = 0
		return true, nil
	case tea.KeyRight:
		a.permTab = (a.permTab + 1) % 3
		m.panel.cursor = 0
		return true, nil
	case tea.KeyEsc:
		a.permStage = 0
		m.panel.cursor = a.permScope
		return true, nil
	}
	if m.handlePermRowActionKey(key, a, n) {
		return true, nil
	}
	if key.Mod == 0 {
		return true, nil
	}
	return false, nil
}

func (m *runMode) handlePermRowActionKey(key tea.Key, a *actionPanelState, n int) bool {
	if key.Code == tea.KeyBackspace || (key.Mod == 0 && key.Text == "d") {
		if n > 0 && m.panel.cursor >= 0 && m.panel.cursor < n {
			list := m.permReadList(a.permScope, a.permTab)
			list = append(list[:m.panel.cursor], list[m.panel.cursor+1:]...)
			m.permWriteList(a.permScope, a.permTab, list)
			if m.panel.cursor >= len(list) && m.panel.cursor > 0 {
				m.panel.cursor--
			}
		}
		return true
	}
	if key.Mod == 0 && key.Text == "a" {
		a.permForm = 1
		a.permEdit = -1
		a.permText = ""
		a.permErr = ""
		a.permStage = 2
		return true
	}
	if key.Mod == 0 && key.Text == "e" {
		list := m.permReadList(a.permScope, a.permTab)
		a.permErr = ""
		if n > 0 && m.panel.cursor >= 0 && m.panel.cursor < len(list) {
			a.permForm = 2
			a.permEdit = m.panel.cursor
			a.permText = list[m.panel.cursor]
		} else {
			a.permForm = 1
			a.permEdit = -1
			a.permText = ""
		}
		a.permStage = 2
		return true
	}
	return false
}

func (m *runMode) handlePermissionsFormKey(key tea.Key) (bool, tea.Cmd) {
	a := &m.panel.act
	switch key.Code {
	case tea.KeyEsc:
		a.permStage = 1
		a.permText = ""
		a.permErr = ""
		return true, nil
	case tea.KeyEnter:
		if msg := validateGrant(a.permText); msg != "" {
			a.permErr = msg
			return true, nil
		}
		list := m.permReadList(a.permScope, a.permTab)
		if a.permForm == 2 && a.permEdit >= 0 && a.permEdit < len(list) {
			// Edit = delete-then-append, i.e. the rule moves to the end
			// (probe_lists.py / the checkpoint's measured behaviour).
			list = append(list[:a.permEdit], list[a.permEdit+1:]...)
		}
		list = append(list, a.permText)
		m.permWriteList(a.permScope, a.permTab, list)
		a.permText = ""
		a.permErr = ""
		a.permStage = 1
		return true, nil
	case tea.KeyBackspace:
		if r := []rune(a.permText); len(r) > 0 {
			a.permText = string(r[:len(r)-1])
		}
		return true, nil
	}
	if key.Mod == 0 && key.Text != "" {
		a.permText += key.Text
		return true, nil
	}
	if key.Mod == 0 {
		return true, nil
	}
	return false, nil
}

// hooks and agents rendering and key handling are in slash_action_panels_hooks.go
