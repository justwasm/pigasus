// slash_panel_keymap_test.go — the panel key router's golden.
//
// Cycle 381: the panel registry owns the fan-out over the panels — the
// route that claims a key for the open panel, and the panel's row
// count. The host's key router asks the registry and keeps the rest: the
// /config and /model panels, the generic navigation and the host's own
// key handling.
//
// The golden pins the decisions the move touched: which command opens
// which panel, and what every panel kind does with every key — whether
// the key was handled, whether the panel is still open, and where the
// panel's cursor stands. The bytes were taken from the tree *before* the
// move, so the golden is the move's parity proof, not a description of
// the new code.
//
// The golden does not pin the viewer's scroll window: the three document
// viewers own their own offset, and the /feature-diff document is not
// the same document on every machine.

package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/slashcommands"
)

// goldenKind is one panel kind of the golden's fixture.
type goldenKind struct {
	kind panelregistry.Kind
	name string
}

// goldenKinds is every panel kind, in the registry's order.
func goldenKinds() []goldenKind {
	return []goldenKind{
		{panelregistry.None, "none"},
		{panelregistry.MCP, "mcp"},
		{panelregistry.Tasks, "tasks"},
		{panelregistry.Effort, "effort"},
		{panelregistry.Hooks, "hooks"},
		{panelregistry.Permissions, "permissions"},
		{panelregistry.Agents, "agents"},
		{panelregistry.Credits, "credits"},
		{panelregistry.CodeSearch, "codesearch"},
		{panelregistry.Artifact, "artifact"},
		{panelregistry.Feedback, "feedback"},
		{panelregistry.Rewind, "rewind"},
		{panelregistry.Context, "context"},
		{panelregistry.Model, "model"},
		{panelregistry.Config, "config"},
		{panelregistry.Plugin, "plugin"},
		{panelregistry.Skills, "skills"},
		{panelregistry.Usage, "usage"},
		{panelregistry.Changelog, "changelog"},
		{panelregistry.Diff, "diff"},
		{panelregistry.AddDir, "add-dir"},
		{panelregistry.Rename, "rename"},
		{panelregistry.FeatDiff, "feature-diff"},
		{panelregistry.Provider, "provider"},
		{panelregistry.Accounts, "accounts"},
	}
}

// goldenKey is one key of the golden's fixture.
type goldenKey struct {
	name string
	key  tea.Key
}

// goldenKeys is the key fixture: the navigation keys, the keys the panels
// answer, and the keys the host's generic handlers swallow.
func goldenKeys() []goldenKey {
	return []goldenKey{
		{"up", tea.Key{Code: tea.KeyUp}},
		{"down", tea.Key{Code: tea.KeyDown}},
		{"pgup", tea.Key{Code: tea.KeyPgUp}},
		{"pgdown", tea.Key{Code: tea.KeyPgDown}},
		{"home", tea.Key{Code: tea.KeyHome}},
		{"end", tea.Key{Code: tea.KeyEnd}},
		{"left", tea.Key{Code: tea.KeyLeft}},
		{"right", tea.Key{Code: tea.KeyRight}},
		{"esc", tea.Key{Code: tea.KeyEsc}},
		{"enter", tea.Key{Code: tea.KeyEnter}},
		{"tab", tea.Key{Code: tea.KeyTab}},
		{"backspace", tea.Key{Code: tea.KeyBackspace}},
		{"space", tea.Key{Code: tea.KeySpace, Text: " "}},
		{"ctrl+c", tea.Key{Code: 'c', Mod: tea.ModCtrl}},
		{"ctrl+l", tea.Key{Code: 'l', Mod: tea.ModCtrl}},
		{"ctrl+o", tea.Key{Code: 'o', Mod: tea.ModCtrl}},
		{"letter-x", tea.Key{Code: 'x', Text: "x"}},
		{"letter-j", tea.Key{Code: 'j', Text: "j"}},
		{"letter-k", tea.Key{Code: 'k', Text: "k"}},
		{"letter-q", tea.Key{Code: 'q', Text: "q"}},
		{"letter-g", tea.Key{Code: 'g', Text: "g"}},
		{"letter-G", tea.Key{Code: 'G', Text: "G"}},
	}
}

// goldenCommands is the command fixture: every command of the reference's
// own panel table, plus a few that open no panel at all.
func goldenCommands() []string {
	return []string{
		"mcp", "tasks", "effort", "hooks", "permissions", "agents",
		"credits", "codesearch", "artifact", "feedback", "rewind",
		"context", "model", "config", "plugin", "skills", "usage",
		"changelog", "diff", "add-dir", "rename", "feature-diff",
		"provider", "accounts", "pool",
		"clear", "help", "fork", "btw", "voice", "record", "model-x",
	}
}

// goldenPanel builds a runMode with the given panel open.
//
// The fixture is the host's own: the panel object is built with the
// host's seams, exactly as the open path builds it.
func goldenPanel(kind panelregistry.Kind) (m *runMode) {
	defer func() {
		if r := recover(); r != nil {
			m = nil
		}
	}()
	m = &runMode{}
	m.panel = slashPanel{kind: kind, cmd: "/" + goldenKindName(kind)}
	switch kind {
	case panelregistry.Tasks:
		m.panel.tp = m.newTasksPanel()
	case panelregistry.Effort:
		m.panel.ef = m.newEffortPanel()
	case panelregistry.Hooks:
		m.panel.hk = m.newHooksPanel()
	case panelregistry.Permissions:
		m.panel.perm = m.newPermissionsPanel()
	case panelregistry.Agents:
		m.panel.ag = m.newAgentsPanel()
	case panelregistry.Credits:
		m.panel.cr = m.newCreditsPanel()
	case panelregistry.CodeSearch:
		m.panel.csp = m.newCodesearchPanel()
	case panelregistry.Feedback:
		m.panel.fb = m.newFeedbackPanel()
	case panelregistry.Rewind:
		m.panel.rw = m.newRewindPanel()
	case panelregistry.Model:
		m.panel.mdl = m.newModelPanel()
	case panelregistry.Config:
		m.panel.cfg = m.newConfigPanel()
	case panelregistry.Plugin:
		m.panel.plg = m.newPluginPanel()
	case panelregistry.Skills:
		m.panel.sk = m.newSkillsPanel()
	case panelregistry.Diff:
		m.panel.df = m.newDiffPanel()
	case panelregistry.Rename:
		m.panel.rn = m.newRenamePanel()
	case panelregistry.AddDir:
		m.panel.ad = m.newAddDirPanel()
	case panelregistry.Provider:
		m.panel.prov = m.newProviderPanel()
	case panelregistry.Accounts:
		m.panel.acct = m.newAccountsPanel()
	}
	if kind.IsViewer() {
		m.initViewerViewport()
	}
	return m
}

// goldenKindName is the fixture's name of a panel kind.
func goldenKindName(kind panelregistry.Kind) string {
	for _, kd := range goldenKinds() {
		if kd.kind == kind {
			return kd.name
		}
	}
	return "none"
}

// TestPanelKeymapGolden pins the panel key router's decisions.
//
// The golden was taken from the tree before the registry took the fan-out
// over the panels, so a difference here is a difference in the reference's
// behaviour.
func TestPanelKeymapGolden(t *testing.T) {
	t.Setenv("OPENAGY_CONFIG_HOME", t.TempDir())
	var b strings.Builder

	// The command → panel table.
	for _, cmd := range goldenCommands() {
		kind, ok := slashcommands.PanelFor(cmd)
		fmt.Fprintf(&b, "command %q -> kind=%s ok=%v\n", cmd, goldenKindName(kind), ok)
	}

	// Every panel kind's answer to every key.
	for _, kd := range goldenKinds() {
		for _, kf := range goldenKeys() {
			m := goldenPanel(kd.kind)
			if m == nil {
				continue
			}
			handled, _ := m.handlePanelKey(kf.key)
			fmt.Fprintf(&b, "key %s %s -> handled=%v panel=%s cursor=%d\n",
				kd.name, kf.name, handled, goldenKindName(m.panel.kind), m.panel.cursor)
		}
	}

	golden := filepath.Join("testdata", "panel_keymap_golden.txt")
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if got := b.String(); got != string(want) {
		t.Errorf("the panel key router's decisions changed:\n%s", diffGolden(got, string(want)))
	}
}

// TestPanelRowCounts pins the row count of the panels whose rows are a
// constant of the panel, and checks the wiring of the panels whose rows
// depend on the machine.
//
// The count is the cursor bound of a vertical list panel. The registry
// asks the panel that owns the rows, so a panel whose rows follow the
// user's own configuration — /model, /config, /plugin, /skills,
// /provider, /accounts, /permissions and /agents — is checked as
// wiring: the registry's count is the panel's count, not a copy of it.
func TestPanelRowCounts(t *testing.T) {
	t.Setenv("OPENAGY_CONFIG_HOME", t.TempDir())

	constants := []struct {
		kind panelregistry.Kind
		name string
		want int
	}{
		// The effort slider's three levels.
		{panelregistry.Effort, "effort", 3},
		// The five hook types.
		{panelregistry.Hooks, "hooks", 6},
		// The credits panel's two actions.
		{panelregistry.Credits, "credits", 2},
		// The feedback picker's six categories.
		{panelregistry.Feedback, "feedback", 6},
	}
	for _, c := range constants {
		m := goldenPanel(c.kind)
		if m == nil {
			t.Fatalf("%s: no panel", c.name)
		}
		if got := m.panelRegistry().RowCount(c.kind); got != c.want {
			t.Errorf("%s: the registry's row count = %d, want %d", c.name, got, c.want)
		}
	}

	wiring := []struct {
		kind  panelregistry.Kind
		name  string
		panel func(m *runMode) int
	}{
		{panelregistry.Tasks, "tasks", func(m *runMode) int { return m.tasksPanel().RowCount() }},
		{panelregistry.Permissions, "permissions", func(m *runMode) int { return m.permissionsPanel().RowCount() }},
		{panelregistry.Agents, "agents", func(m *runMode) int { return m.agentsPanel().RowCount() }},
		{panelregistry.Rewind, "rewind", func(m *runMode) int { return m.rewindPanel().RowCount() }},
		{panelregistry.Model, "model", func(m *runMode) int { return m.modelPanel().RowCount() }},
		{panelregistry.Config, "config", func(m *runMode) int { return m.configPanel().RowCount() }},
		{panelregistry.Plugin, "plugin", func(m *runMode) int { return m.pluginPanel().RowCount() }},
		{panelregistry.Skills, "skills", func(m *runMode) int { return m.skillsPanel().RowCount() }},
		{panelregistry.Provider, "provider", func(m *runMode) int { return m.providerPanel().RowCount() }},
		{panelregistry.Accounts, "accounts", func(m *runMode) int { return m.accountsPanel().RowCount() }},
	}
	for _, c := range wiring {
		m := goldenPanel(c.kind)
		if m == nil {
			t.Fatalf("%s: no panel", c.name)
		}
		want := c.panel(m)
		if got := m.panelRegistry().RowCount(c.kind); got != want {
			t.Errorf("%s: the registry's row count = %d, want the panel's %d", c.name, got, want)
		}
	}

	// A kind with no rows counts zero.
	m := goldenPanel(panelregistry.Artifact)
	if got := m.panelRegistry().RowCount(panelregistry.Artifact); got != 0 {
		t.Errorf("artifact: the registry's row count = %d, want 0", got)
	}
}

// diffGolden renders the first differing lines of two goldens.
func diffGolden(got, want string) string {
	gl := strings.Split(got, "\n")
	wl := strings.Split(want, "\n")
	var b strings.Builder
	for i := 0; i < len(gl) || i < len(wl); i++ {
		g, w := "", ""
		if i < len(gl) {
			g = gl[i]
		}
		if i < len(wl) {
			w = wl[i]
		}
		if g == w {
			continue
		}
		fmt.Fprintf(&b, "line %d:\n got: %q\nwant: %q\n", i+1, g, w)
		if b.Len() > 4096 {
			break
		}
	}
	return b.String()
}
