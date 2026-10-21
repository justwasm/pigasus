// slash_action_panels.go — cycle 205: the *key actions* of the four
// "action" panels (/permissions, /mcp, /agents, /hooks).
//
// Cycle 195/201 drew these panels and cycle 201 implemented the key
// actions of the *A-class* panels (/usage /changelog /plugin /skills
// /diff /feedback). The four panels in this file announced keys that the
// clone ignored; this cycle implements the subset whose behaviour could
// be measured against the reference, and leaves the rest inert under
// 存疑 (see docs/cycles/cycle-205/deliverable-action-panels.md).
//
// Evidence: private tmux socket `tmux -L agy205`, REF v1.4.0 at 220×50,
// captures saved under /private/var/folders/.../T/opencode/agy205/ref/.
// Every byte below is transcribed from those captures or from the
// robust re-probes (probe_perm_sep2.py, probe_perm_add.py,
// probe_lists.py, probe_project.py, probe_agents.py).
//
// What is implemented:
//   /permissions  stage 1 (scope list, now wrapping) → stage 2 (allow /
//                 deny / ask tabs, rows, ←/→ switch, a/e/d edit) →
//                 stage 3 add/edit rule form with validation and
//                 persistence (Global → settings.json, Project → the
//                 default-cli-project config; Shared is session-local).
//   /hooks        stage 1 (type list) → stage 2 (matcher list) →
//                 stage 3 add-matcher / add-hook forms.
//   /agents       Enter echoes "Already using default agent." and keeps
//                 the panel open; Esc closes silently.
//   /mcp          unchanged (the empty-state frame already matches).
//
// What is deliberately left inert (存疑): the /hooks `e Toggle` /
// `backspace Delete` on the matcher list, the /hooks confirm-to-persist
// path, the /mcp non-empty Actions menu, and the full grant-action set.

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/auth"
)

// ---- permission scopes and lists -------------------------------------------

const (
	permScopeProject = iota
	permScopeShared
	permScopeGlobal
)

// permScopeNames are the three scope rows. The names are transcribed from
// the stage-1 capture (the clone's permissionScopes table in
// slash_panels.go carries the same three labels plus their blurbs).
var permScopeNames = [3]string{"Project", "Shared with Pigasus", "Global"}

// permListNames / permListKeys are the three permission lists, in tab
// order, and the settings.json key each maps to. allow/deny are the
// verified Global targets; ask is verified too (probe_lists.py).
var permListNames = [3]string{"allowlist", "denylist", "asklist"}
var permListKeys = [3]string{"allow", "deny", "ask"}

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

// readSettingsPermList reads permissions.<key> from settings.json.
func readSettingsPermList(tab int) []string {
	v, err := auth.SettingsField("permissions." + permListKeys[tab])
	if err != nil || v == nil {
		return nil
	}
	return anyStringSlice(v)
}

// anyStringSlice coerces a decoded JSON array into []string.
func anyStringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// projectConfigPath is the reference's default CLI project file. Probed:
// the Project scope's rules are written here under
// permissionGrants.permissionGrants.<key> (probe_project.py).
func projectConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".gemini", "config", "projects", "default-cli-project.json")
}

func readProjectPermList(tab int) []string {
	p := projectConfigPath()
	if p == "" {
		return nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(data, &m) != nil {
		return nil
	}
	pg, _ := m["permissionGrants"].(map[string]any)
	if pg == nil {
		return nil
	}
	inner, _ := pg["permissionGrants"].(map[string]any)
	if inner == nil {
		return nil
	}
	return anyStringSlice(inner[permListKeys[tab]])
}

func writeProjectPermList(tab int, list []string) {
	p := projectConfigPath()
	if p == "" {
		return
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return
	}
	var m map[string]any
	if json.Unmarshal(data, &m) != nil {
		return
	}
	pg, _ := m["permissionGrants"].(map[string]any)
	if pg == nil {
		pg = map[string]any{}
		m["permissionGrants"] = pg
	}
	inner, _ := pg["permissionGrants"].(map[string]any)
	if inner == nil {
		inner = map[string]any{}
		pg["permissionGrants"] = inner
	}
	vals := make([]any, len(list))
	for i, s := range list {
		vals[i] = s
	}
	inner[permListKeys[tab]] = vals
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(p, out, 0o644)
}

// ---- permission validation -------------------------------------------------

// validateGrant reproduces the reference's two rejection messages. Only
// `command` is a verified accepted action; `read` and `foo` were both
// probed and rejected (probe_read.py). The reference's full action set
// could not be enumerated — see 存疑.
func validateGrant(rule string) string {
	open := strings.IndexByte(rule, '(')
	if open <= 0 || !strings.HasSuffix(rule, ")") {
		return fmt.Sprintf("invalid grant string: %q", rule)
	}
	action := rule[:open]
	for _, r := range action {
		if !(r >= 'a' && r <= 'z') && r != '_' {
			return fmt.Sprintf("invalid grant string: %q", rule)
		}
	}
	if !validGrantAction(action) {
		return fmt.Sprintf("unknown action %q in grant string: %q", action, rule)
	}
	return ""
}

func validGrantAction(a string) bool { return a == "command" }

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

// permTabRow renders the allowlist / denylist / asklist tab strip. The
// active tab is bold+underline blue with a bold count; the others are
// 256-colour 253. Verified byte-for-byte against perm-after-enter.raw.txt.
func permTabRow(active int, counts [3]int) string {
	var b strings.Builder
	b.WriteString(" ")
	for i := 0; i < 3; i++ {
		if i > 0 {
			b.WriteString("    ")
		}
		name := permListNames[i]
		cnt := fmt.Sprintf("(%d)", counts[i])
		if i == active {
			b.WriteString("\x1b[1;4m\x1b[38;5;69m" + name +
				"\x1b[0;4m\x1b[38;5;69m " + "\x1b[1m" + cnt + "\x1b[0m")
		} else {
			b.WriteString("\x1b[38;2;238;232;213m" + name + " " + cnt + "\x1b[39m")
		}
	}
	b.WriteString(" \x1b[38;2;238;232;213m  (←/→ to switch)\x1b[39m")
	return b.String()
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
	if key.Code == tea.KeyBackspace || (key.Mod == 0 && key.Text == "d") {
		if n > 0 && m.panel.cursor >= 0 && m.panel.cursor < n {
			list := m.permReadList(a.permScope, a.permTab)
			list = append(list[:m.panel.cursor], list[m.panel.cursor+1:]...)
			m.permWriteList(a.permScope, a.permTab, list)
			if m.panel.cursor >= len(list) && m.panel.cursor > 0 {
				m.panel.cursor--
			}
		}
		return true, nil
	}
	if key.Mod == 0 && key.Text == "a" {
		a.permForm = 1
		a.permEdit = -1
		a.permText = ""
		a.permErr = ""
		a.permStage = 2
		return true, nil
	}
	if key.Mod == 0 && key.Text == "e" {
		list := m.permReadList(a.permScope, a.permTab)
		a.permErr = ""
		if n > 0 && m.panel.cursor >= 0 && m.panel.cursor < len(list) {
			a.permForm = 2
			a.permEdit = m.panel.cursor
			a.permText = list[m.panel.cursor]
		} else {
			// No row selected → the reference opens the Add form.
			a.permForm = 1
			a.permEdit = -1
			a.permText = ""
		}
		a.permStage = 2
		return true, nil
	}
	if key.Mod == 0 {
		return true, nil
	}
	return false, nil
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

// ---- /hooks rendering ------------------------------------------------------

// hooksPanelLines dispatches the /hooks panel by stage.
func (m *runMode) hooksPanelLines() []string {
	switch m.panel.act.hookStage {
	case 1:
		return m.hooksMatchersLines()
	case 2:
		return m.hooksFormLines()
	}
	return m.panelHooksLines()
}

// hooksMatchersLines is stage 1: the matcher list for one hook type.
// Transcribed from hooks-enter.raw.txt (PreToolUse).
func (m *runMode) hooksMatchersLines() []string {
	a := m.panel.act
	idx := a.hookIdx
	if idx < 0 || idx >= len(hookRows) {
		idx = 0
	}
	name, desc := hookRows[idx][0], hookRows[idx][1]
	tool := hookIsTool(idx)
	addLabel := "+ Add new matcher..."
	if !tool {
		addLabel = "+ Add new hook..."
	}
	lines := []string{
		"\x1b[39m \x1b[1m" + name + " — Matchers\x1b[0m",
		" " + panelGray238 + "  " + desc + panelGray238C,
		"",
	}
	if m.panel.cursor == 0 {
		lines = append(lines, " "+cBlueOpen+"> "+addLabel+cBlueClose)
	} else {
		lines = append(lines, "   "+addLabel)
	}
	if tool {
		if m.panel.cursor == 1 {
			lines = append(lines, " "+cBlueOpen+"> + Match all (no filter)"+cBlueClose)
		} else {
			lines = append(lines, "   + Match all (no filter)")
		}
	}
	lines = append(lines, "   "+panelGray238+"No hooks configured"+panelGray238C,
		"",
		panelDotHint("   ", "↑/↓", "Navigate", "enter", "Select",
			"e", "Toggle", "backspace", "Delete", "esc", "Back"),
	)
	return lines
}

// hooksFormLines is stage 2: the add-matcher (tool hooks) or add-hook
// (lifecycle hooks) form. Transcribed from hooks-add-enter.raw.txt and
// the PreInvocation probe (probe_hooks.py).
func (m *runMode) hooksFormLines() []string {
	a := m.panel.act
	idx := a.hookIdx
	if idx < 0 || idx >= len(hookRows) {
		idx = 0
	}
	name, desc := hookRows[idx][0], hookRows[idx][1]
	if hookIsTool(idx) {
		field := " " + cBlueOpen + ">  " + cBlueClose
		if a.hookMatch != "" {
			field = " " + cBlueOpen + "> " + cBlueClose + a.hookMatch
		}
		return []string{
			"\x1b[39m \x1b[1mAdd new matcher for " + name + "\x1b[0m",
			"",
			" " + panelGray238 + "  " + desc + panelGray238C,
			"",
			" \x1b[1m  Matcher:\x1b[0m",
			field,
			"",
			" " + panelGray238 + "  Examples:" + panelGray238C,
			" " + panelGray238 + "  • Write (single tool)" + panelGray238C,
			" " + panelGray238 + "  • Write|Edit (multiple tools)" + panelGray238C,
			" " + panelGray238 + "  • Web.* (regex pattern)" + panelGray238C,
			"",
			panelDotHint("   ", "enter", "Confirm", "esc", "Cancel"),
		}
	}
	nameField := " " + cBlueOpen + "> " + "\x1b[7m\x1b[37m \x1b[0m"
	if a.hookName != "" {
		nameField = " " + cBlueOpen + "> " + cBlueClose + a.hookName
	}
	cmdField := "     " + panelGray238 + " " + panelGray238C
	if a.hookField == 1 {
		cmdField = "     \x1b[7m\x1b[37m \x1b[0m"
	}
	return []string{
		"\x1b[39m \x1b[1mAdd new hook\x1b[0m",
		"",
		" " + panelGray238 + "  Event: " + name + " — " + desc + panelGray238C,
		"",
		" \x1b[1m  Name:\x1b[0m",
		nameField,
		"",
		" " + panelGray238 + "  Command:" + panelGray238C,
		cmdField,
		"",
		" " + panelGray238 + "  Timeout (seconds):" + panelGray238C,
		"     " + panelGray238 + "default: 30" + panelGray238C,
		"",
		"   " + panelEffMedOpen + "⚠ Hooks run shell commands with your full permissions." + panelGray238C,
		"",
		panelDotHint("   ", "tab", "Switch field", "enter", "Confirm", "esc", "Cancel"),
	}
}

// ---- /hooks keys -----------------------------------------------------------

func (m *runMode) handleHooksKey(key tea.Key) (bool, tea.Cmd) {
	a := &m.panel.act
	switch a.hookStage {
	case 1:
		return m.handleHooksMatchersKey(key)
	case 2:
		return m.handleHooksFormKey(key)
	}
	// stage 0: hook-type list (clamps, matching the pre-cycle behaviour).
	switch key.Code {
	case tea.KeyUp:
		if m.panel.cursor > 0 {
			m.panel.cursor--
		}
		return true, nil
	case tea.KeyDown:
		if m.panel.cursor < len(hookRows)-1 {
			m.panel.cursor++
		}
		return true, nil
	case tea.KeyEnter:
		a.hookIdx = m.panel.cursor
		a.hookStage = 1
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

func hookSelectableRows(idx int) int {
	if hookIsTool(idx) {
		return 2
	}
	return 1
}

func (m *runMode) handleHooksMatchersKey(key tea.Key) (bool, tea.Cmd) {
	a := &m.panel.act
	n := hookSelectableRows(a.hookIdx)
	switch key.Code {
	case tea.KeyUp:
		if m.panel.cursor > 0 {
			m.panel.cursor--
		}
		return true, nil
	case tea.KeyDown:
		if m.panel.cursor < n-1 {
			m.panel.cursor++
		}
		return true, nil
	case tea.KeyEnter:
		if m.panel.cursor == 0 {
			a.hookStage = 2
			a.hookField = 0
			a.hookMatch = ""
			a.hookName = ""
		}
		return true, nil
	case tea.KeyEsc:
		a.hookStage = 0
		m.panel.cursor = a.hookIdx
		return true, nil
	}
	// `e Toggle` / `backspace Delete` are advertised but their effect is
	// unobservable with no hooks configured — left inert (存疑).
	if key.Mod == 0 {
		return true, nil
	}
	return false, nil
}

func (m *runMode) handleHooksFormKey(key tea.Key) (bool, tea.Cmd) {
	a := &m.panel.act
	switch key.Code {
	case tea.KeyEsc:
		a.hookStage = 1
		m.panel.cursor = 0
		return true, nil
	case tea.KeyTab:
		if !hookIsTool(a.hookIdx) {
			a.hookField = (a.hookField + 1) % 3
		}
		return true, nil
	case tea.KeyBackspace:
		switch {
		case hookIsTool(a.hookIdx):
			if r := []rune(a.hookMatch); len(r) > 0 {
				a.hookMatch = string(r[:len(r)-1])
			}
		case a.hookField == 0:
			if r := []rune(a.hookName); len(r) > 0 {
				a.hookName = string(r[:len(r)-1])
			}
		}
		return true, nil
	}
	if key.Mod == 0 && key.Text != "" {
		switch {
		case hookIsTool(a.hookIdx):
			a.hookMatch += key.Text
		case a.hookField == 0:
			a.hookName += key.Text
		}
		return true, nil
	}
	// Enter (Confirm) is intentionally inert: the reference's confirm
	// path persists a hook, and neither the store nor the resulting
	// frame could be captured. 存疑.
	if key.Mod == 0 {
		return true, nil
	}
	return false, nil
}

// ---- /agents keys ----------------------------------------------------------

// handleAgentsKey implements the one measured /agents action: Enter on
// the default agent echoes "Already using default agent." into the
// transcript and keeps the panel open; Esc closes it silently
// (probe_agents.py + esc-ref-agents-Esc.txt). `k` (Kill Active Subagent)
// is inert with no active subagent.
func (m *runMode) handleAgentsKey(key tea.Key) (bool, tea.Cmd) {
	rows := 1 + len(m.panel.agents)
	switch key.Code {
	case tea.KeyEsc:
		if m.panel.agentPreparedSet {
			// The reference applies the prepared switch on Esc and does
			// not write an `Exited /agents command` notice; the fork's
			// empty transcript is the visible result.
			m.applyAgentSwitch(m.panel.agentPreparedName, m.panel.agentPreparedBody)
			m.panel = slashPanel{}
			return true, nil
		}
		return true, m.closePanel()
	case tea.KeyUp:
		if m.panel.cursor > 0 {
			m.panel.cursor--
		}
		return true, nil
	case tea.KeyDown:
		if m.panel.cursor < rows-1 {
			m.panel.cursor++
		}
		return true, nil
	case tea.KeyEnter:
		cursor := m.panel.cursor
		if cursor < 0 || cursor >= rows {
			cursor = 0
		}
		name, body := "", ""
		if cursor > 0 {
			a := m.panel.agents[cursor-1]
			name, body = a.Name, a.Body
		}
		active := m.agentName
		if m.panel.agentPreparedSet {
			active = m.panel.agentPreparedName
		}
		if name == active {
			if name == "" {
				m.slashRespond("/agents", "Already using default agent.")
			} else {
				m.slashRespond("/agents", "Already selected agent: "+name)
			}
			return true, nil
		}
		m.panel.agentPreparedSet = true
		m.panel.agentPreparedName = name
		m.panel.agentPreparedBody = body
		// The panel title always says "fork"; the transcript message is
		// "create a new conversation" when the current one is still empty,
		// and "fork the current conversation" once it has messages.
		// Verified live against agy 1.4.3 in both states.
		verb := "create a new conversation"
		if len(m.session) > 0 {
			verb = "fork the current conversation"
		}
		if name == "" {
			m.slashRespond("/agents", "Prepared switch to default agent (will "+verb+" on exit).")
		} else {
			m.slashRespond("/agents", "Prepared selection: "+name+" (will "+verb+" on exit).")
		}
		return true, nil
	}
	// k (Kill Active Subagent) is inert with no active subagent, and every
	// other key is swallowed while the panel is open.
	if key.Mod == 0 {
		return true, nil
	}
	return false, nil
}
