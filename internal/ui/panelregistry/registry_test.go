package panelregistry

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	tuiviewer "github.com/justwasm/pigasus/internal/ui/foundations/viewer"
)

func TestKindProperties(t *testing.T) {
	if MCP.ExitNotice() {
		t.Errorf("expected MCP.ExitNotice() false")
	}
	if !Skills.ExitNotice() || !Accounts.ExitNotice() {
		t.Errorf("expected Skills and Accounts ExitNotice() true")
	}
	if !CodeSearch.KeepStatusHint() || Tasks.KeepStatusHint() {
		t.Errorf("expected only CodeSearch to keep status hint")
	}
	if !Usage.IsViewer() || !Changelog.IsViewer() || !FeatDiff.IsViewer() || Diff.IsViewer() {
		t.Errorf("unexpected IsViewer() result")
	}
	if Hooks.TrailingBlank() || Context.TrailingBlank() || Usage.TrailingBlank() {
		t.Errorf("Hooks, Context, Usage must not have trailing blank")
	}
	if !Tasks.TrailingBlank() {
		t.Errorf("Tasks must have trailing blank")
	}
	if !Permissions.OwnsEarlyKeys() || Tasks.OwnsEarlyKeys() {
		t.Errorf("unexpected OwnsEarlyKeys result")
	}
	if !Tasks.IsList() || !Artifact.IsList() || !CodeSearch.IsList() || !Context.IsList() {
		t.Errorf("unexpected IsList result")
	}
	if Model.IsList() || Config.IsList() || Usage.IsList() {
		t.Errorf("Model, Config and Usage are not list panels")
	}
}

// TestKindRoutes pins the route table: which route claims a key for which
// kind.
//
// The table is the registry's own — it sits next to the fan-out it
// drives, so the dispatcher never re-implements a domain's cases inline.
func TestKindRoutes(t *testing.T) {
	want := map[Kind]Route{
		// No route: the keys reach the host's own handlers.
		None:   RouteNone,
		MCP:    RouteNone,
		Model:  RouteNone,
		Config: RouteNone,
		// The panel owns every key it receives.
		Tasks:       RoutePanel,
		Effort:      RoutePanel,
		Hooks:       RoutePanel,
		Permissions: RoutePanel,
		Agents:      RoutePanel,
		Credits:     RoutePanel,
		Feedback:    RoutePanel,
		Rewind:      RoutePanel,
		Diff:        RoutePanel,
		AddDir:      RoutePanel,
		Rename:      RoutePanel,
		Plugin:      RoutePanel,
		Skills:      RoutePanel,
		Provider:    RoutePanel,
		Accounts:    RoutePanel,
		// The search route: the panel owns the keys of its result list
		// and lets the rest through to the list rule.
		CodeSearch: RouteSearch,
		// The list rule: every key but Ctrl+C.
		Artifact: RouteList,
		Context:  RouteList,
		// The document viewer.
		Usage:     RouteViewer,
		Changelog: RouteViewer,
		FeatDiff:  RouteViewer,
	}
	for k, w := range want {
		if got := k.Route(); got != w {
			t.Errorf("kind %d: route = %d, want %d", k, got, w)
		}
	}
}

// TestCommandPanel pins the command → panel table.
func TestCommandPanel(t *testing.T) {
	want := map[string]Kind{
		"mcp":          MCP,
		"tasks":        Tasks,
		"effort":       Effort,
		"hooks":        Hooks,
		"permissions":  Permissions,
		"agents":       Agents,
		"credits":      Credits,
		"codesearch":   CodeSearch,
		"artifact":     Artifact,
		"feedback":     Feedback,
		"rewind":       Rewind,
		"context":      Context,
		"model":        Model,
		"config":       Config,
		"plugin":       Plugin,
		"skills":       Skills,
		"usage":        Usage,
		"changelog":    Changelog,
		"diff":         Diff,
		"add-dir":      AddDir,
		"rename":       Rename,
		"feature-diff": FeatDiff,
		"provider":     Provider,
		"accounts":     Accounts,
		// /pool is the /accounts panel's Pool tab on its own.
		"pool": Accounts,
	}
	for cmd, k := range want {
		got, ok := CommandPanel(cmd)
		if !ok || got != k {
			t.Errorf("CommandPanel(%q) = (%d, %v), want (%d, true)", cmd, got, ok, k)
		}
	}
	for _, cmd := range []string{"clear", "help", "fork", "btw", "voice", "record", "model-x"} {
		if _, ok := CommandPanel(cmd); ok {
			t.Errorf("CommandPanel(%q) unexpectedly opens a panel", cmd)
		}
	}
}

func TestRegistry(t *testing.T) {
	r := New()
	r.Register(MCP, FuncHandler{
		OnLines: func() []string { return []string{"mcp row"} },
	})
	r.Register(Tasks, FuncHandler{
		OnLines:     func() []string { return []string{"task row"} },
		OnHandleKey: func(key tea.Key) (bool, tea.Cmd) { return true, nil },
	})
	r.Register(Usage, FuncHandler{
		OnViewerSpec: func() (tuiviewer.Spec, bool) {
			return tuiviewer.Spec{Total: 10}, true
		},
	})

	if lines := r.Lines(MCP); len(lines) != 1 || lines[0] != "mcp row" {
		t.Errorf("unexpected Lines: %v", lines)
	}
	if lines := r.Lines(Tasks); len(lines) != 1 || lines[0] != "task row" {
		t.Errorf("unexpected Lines: %v", lines)
	}
	if lines := r.Lines(Plugin); lines != nil {
		t.Errorf("expected nil for unregistered Plugin lines")
	}

	// The panel's own route: the handler answers, and the key is
	// claimed — the key belongs to the panel either way.
	handled, _, claimed := r.HandleKey(Tasks, tea.Key{})
	if !handled || !claimed {
		t.Errorf("expected the Tasks key to be handled and claimed, got (%v, %v)", handled, claimed)
	}
	// A kind with no route: the key reaches the host's own handlers.
	handled, _, claimed = r.HandleKey(MCP, tea.Key{})
	if handled || claimed {
		t.Errorf("expected the MCP key to be neither handled nor claimed, got (%v, %v)", handled, claimed)
	}

	spec, ok := r.ViewerSpec(Usage)
	if !ok || spec.Total != 10 {
		t.Errorf("expected Usage viewer spec ok, got %v, %+v", ok, spec)
	}
	_, ok = r.ViewerSpec(MCP)
	if ok {
		t.Errorf("expected MCP viewer spec not ok")
	}
}

// TestRegistryListRule pins the list rule: every key is swallowed, except
// Ctrl+C — the global "leave" key — and Esc leaves the panel.
func TestRegistryListRule(t *testing.T) {
	closed := 0
	r := New()
	r.Wire(Wiring{
		Seams: Seams{Close: func() tea.Cmd { closed++; return nil }},
	})

	handled, _, claimed := r.HandleKey(Artifact, tea.Key{Code: tea.KeyEsc})
	if !handled || !claimed {
		t.Errorf("Esc: expected the key to be handled and claimed, got (%v, %v)", handled, claimed)
	}
	if closed != 1 {
		t.Errorf("Esc: the panel was closed %d times, want 1", closed)
	}

	handled, _, claimed = r.HandleKey(Artifact, tea.Key{Code: 'c', Mod: tea.ModCtrl})
	if handled || !claimed {
		t.Errorf("Ctrl+C: expected the key to be claimed but not handled, got (%v, %v)", handled, claimed)
	}

	handled, _, claimed = r.HandleKey(Artifact, tea.Key{Code: 'x', Text: "x"})
	if !handled || !claimed {
		t.Errorf("a letter: expected the key to be handled and claimed, got (%v, %v)", handled, claimed)
	}
}

// TestRegistrySearchRoute pins the search route: the panel answers
// whether it claimed the key, and a key it did not claim belongs to the
// list rule.
func TestRegistrySearchRoute(t *testing.T) {
	closes := 0
	claimed := false
	r := New()
	r.Wire(Wiring{
		Claims: map[Kind]func(tea.Key) (bool, tea.Cmd, bool){
			CodeSearch: func(key tea.Key) (bool, tea.Cmd, bool) { return true, nil, claimed },
		},
		Seams: Seams{Close: func() tea.Cmd { closes++; return nil }},
	})

	// The panel claimed the key: the panel's answer stands.
	claimed = true
	handled, _, ok := r.HandleKey(CodeSearch, tea.Key{Code: 'j', Text: "j"})
	if !handled || !ok {
		t.Errorf("expected the claimed key to be handled and claimed, got (%v, %v)", handled, ok)
	}

	// The panel did not claim the key: the list rule takes it.
	claimed = false
	handled, _, ok = r.HandleKey(CodeSearch, tea.Key{Code: 'j', Text: "j"})
	if !handled || !ok {
		t.Errorf("expected the unclaimed key to fall through to the list rule, got (%v, %v)", handled, ok)
	}
	handled, _, ok = r.HandleKey(CodeSearch, tea.Key{Code: tea.KeyEsc})
	if !handled || !ok || closes != 1 {
		t.Errorf("Esc: expected the list rule to close the panel, got (%v, %v, %d)", handled, ok, closes)
	}
}

// TestRegistryViewerRoute pins the document viewer's route.
func TestRegistryViewerRoute(t *testing.T) {
	r := New()
	r.Wire(Wiring{
		Seams: Seams{ViewerKey: func(key tea.Key) (bool, tea.Cmd) { return true, nil }},
	})
	handled, _, claimed := r.HandleKey(Usage, tea.Key{Code: 'j', Text: "j"})
	if !handled || !claimed {
		t.Errorf("expected the viewer key to be handled and claimed, got (%v, %v)", handled, claimed)
	}
}

// TestRegistryRowCount pins the row count: the registry asks the panel
// that owns the rows.
func TestRegistryRowCount(t *testing.T) {
	r := New()
	if got := r.RowCount(Tasks); got != 0 {
		t.Errorf("an unwired registry: row count = %d, want 0", got)
	}
	r.Wire(Wiring{
		Rows: map[Kind]func() int{
			Tasks: func() int { return 7 },
		},
	})
	if got := r.RowCount(Tasks); got != 7 {
		t.Errorf("row count = %d, want 7", got)
	}
	if got := r.RowCount(Artifact); got != 0 {
		t.Errorf("a kind with no rows: row count = %d, want 0", got)
	}
	if !r.Wired() {
		t.Errorf("expected the registry to report itself wired")
	}
}
