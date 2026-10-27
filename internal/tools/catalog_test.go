package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestCatalogMatchesRecoveredAdvertisedList pins the advertised surface against
// the live 1.4.3 capture (identical to 1.3.6, and re-verified byte-identical
// at 1.4.7 and 1.4.8 — see docs/cycles/reference-drift-147/ and
// docs/cycles/cycle-359-agy-1.4.8-reanchor/deliverable.md). The fixture is the
// `init.tools` array from
//
//	agy -p='x' --output-format stream-json
//
// whose `init.tools` array the reference emits already sorted. The capture
// was repeated from three working directories (a bare /tmp, a temp workspace
// holding .git and an .ipynb, and this Go repository) and was byte-identical
// every time, so the list is fixed rather than workspace-filtered.
func TestCatalogMatchesRecoveredAdvertisedList(t *testing.T) {
	want := readAdvertisedList(t, "advertised_tools_1.4.3.txt")
	if len(want) != 60 {
		t.Fatalf("fixture holds %d names, want 60", len(want))
	}
	reg := Catalog(t.TempDir())
	got := reg.List()
	sort.Strings(got)
	if len(got) != len(want) {
		t.Fatalf("catalog advertises %d tools, want %d\ngot: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("advertised[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestAdvertisedListIsTheLiveSubset pins the difference between the two
// name lists the reference carries at 1.4.8.
//
// The binary knows 63 tool names (a `[]string` global in its rodata, slice
// header at vmaddr 0x10b5ba450, backing array at 0x10b623bb0, length 63).
// A live session's `init.tools` holds 60 of them: the three searches are
// withheld, because they need a service the CLI does not have.
//
// The clone advertises what the reference *prints*, so it advertises 60.
func TestAdvertisedListIsTheLiveSubset(t *testing.T) {
	live := readAdvertisedList(t, "advertised_tools_1.4.3.txt")
	known := readAdvertisedList(t, "tool_names_1.4.7.txt")
	if len(known) != 63 {
		t.Fatalf("the binary's list holds %d names, want 63", len(known))
	}
	have := map[string]bool{}
	for _, name := range known {
		have[name] = true
	}
	for _, name := range live {
		if !have[name] {
			t.Errorf("the reference advertises %q, which its own name list does not hold", name)
		}
	}
	// The names the reference knows but does not advertise.
	withheld := map[string]bool{}
	for _, name := range known {
		if !contains(live, name) {
			withheld[name] = true
		}
	}
	for _, name := range []string{"code_search", "moma_search", "skill_search"} {
		if !withheld[name] {
			t.Errorf("%q is not in the withheld set", name)
		}
		delete(withheld, name)
	}
	if len(withheld) != 0 {
		t.Errorf("unexpected withheld names: %v", withheld)
	}
}

// contains reports whether the list holds the name.
func contains(list []string, name string) bool {
	for _, item := range list {
		if item == name {
			return true
		}
	}
	return false
}

// readAdvertisedList reads one advertised-name fixture.
func readAdvertisedList(t *testing.T, name string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			names = append(names, line)
		}
	}
	return names
}

// TestCatalogDropsUnadvertisedNames pins the names the earlier pclntab-derived
// reconstruction advertised but the reference never emits, plus the MCP rename.
func TestCatalogDropsUnadvertisedNames(t *testing.T) {
	reg := Catalog(t.TempDir())
	for _, gone := range []string{"codebase_search", "notify_user", "view_content_chunk", "call_mcp"} {
		if _, ok := reg.Get(gone); ok {
			t.Errorf("catalog still advertises %q", gone)
		}
	}
	if _, ok := reg.Get("call_mcp_tool"); !ok {
		t.Error("catalog is missing call_mcp_tool")
	}
}

// TestCatalogAdvertisesReferenceNames pins the advertised v1.3.5 surface: the
// names must be the recovered reference names, not the earlier invented set.
func TestCatalogAdvertisesReferenceNames(t *testing.T) {
	reg := Catalog(t.TempDir())
	for _, want := range []string{
		"run_command", "view_file", "write_to_file", "list_dir",
		"replace_file_content", "grep_search", "search_web", "finish",
	} {
		if _, ok := reg.Get(want); !ok {
			t.Errorf("catalog is missing advertised tool %q", want)
		}
	}
	// The invented names must be gone from the advertised surface.
	for _, gone := range []string{"read_file", "create_file", "edit_file"} {
		if _, ok := reg.Get(gone); ok {
			t.Errorf("catalog still advertises invented name %q", gone)
		}
	}
}

// TestCatalogRunCommandIsReal proves the one tool we can execute end to end
// actually runs, and that its result is rendered as text for the model.
func TestCatalogRunCommandIsReal(t *testing.T) {
	reg := Catalog(t.TempDir())
	out, err := reg.Dispatch(context.Background(), "run_command",
		json.RawMessage(`{"command":"echo hello-tools"}`))
	if err != nil {
		t.Fatalf("Dispatch(run_command): %v", err)
	}
	if !strings.Contains(out, "hello-tools") {
		t.Errorf("run_command output = %q, want it to contain hello-tools", out)
	}
}

// TestCatalogAliasToolsShareBackingImplementation checks that the reference
// alias names delegate to the real filesystem executors.
func TestCatalogAliasToolsShareBackingImplementation(t *testing.T) {
	dir := t.TempDir()
	reg := Catalog(dir)

	if _, err := reg.Dispatch(context.Background(), "write_to_file",
		json.RawMessage(`{"path":"note.txt","content":"hi"}`)); err != nil {
		t.Fatalf("write_to_file: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "note.txt"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(data) != "hi" {
		t.Errorf("file = %q, want hi", data)
	}
	out, err := reg.Dispatch(context.Background(), "view_file",
		json.RawMessage(`{"path":"note.txt"}`))
	if err != nil {
		t.Fatalf("view_file: %v", err)
	}
	if !strings.Contains(out, "1: hi") {
		t.Errorf("view_file = %q, want a 1: hi line", out)
	}
}

// TestCatalogPendingToolsReportNotImplemented pins that advertised-but-unbacked
// tools fail loudly rather than silently succeeding. search_web used to be the
// example; it now returns the reference's no-candidates text, so the pin moves
// to a tool that is still unbacked (delete_knowledge). read_resource held the
// pin until cycle 310, which gave the reference's three MCP tools real
// executors.
func TestCatalogPendingToolsReportNotImplemented(t *testing.T) {
	reg := Catalog(t.TempDir())
	_, err := reg.Dispatch(context.Background(), "delete_knowledge",
		json.RawMessage(`{}`))
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("delete_knowledge error = %v, want ErrNotImplemented", err)
	}
}

// TestDispatchUnknownTool checks the unknown-name path.
func TestDispatchUnknownTool(t *testing.T) {
	reg := Catalog(t.TempDir())
	_, err := reg.Dispatch(context.Background(), "no_such_tool", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("Dispatch(no_such_tool) = nil, want error")
	}
}

// TestCatalogToolsCarryDescriptions pins that every catalog entry can hand its
// model-facing description to a caller that re-advertises the registry (the
// pig loop bridge). Before cycle 182 the text existed only in refSpec and was
// unreachable through the Tool interface.
func TestCatalogToolsCarryDescriptions(t *testing.T) {
	reg := Catalog(t.TempDir())
	for _, name := range reg.List() {
		tool, ok := reg.Get(name)
		if !ok {
			t.Fatalf("%s in List but not in Get", name)
		}
		described, ok := tool.(Describer)
		if !ok {
			t.Errorf("%s does not implement Describer", name)
			continue
		}
		if got := described.Description(); got == "" {
			t.Errorf("%s has an empty description", name)
		}
		if got := Describe(tool); got != described.Description() {
			t.Errorf("Describe(%s) = %q, Description() = %q", name, got, described.Description())
		}
	}
}

// TestDescribeWithoutDescription covers the interface-miss path.
func TestDescribeWithoutDescription(t *testing.T) {
	if got := Describe(&pendingTool{name: "x"}); got != "" {
		t.Errorf("Describe = %q, want empty", got)
	}
}

type stubLargeBudgetTool struct{}

func (t *stubLargeBudgetTool) Name() string            { return "large_budget_tool" }
func (t *stubLargeBudgetTool) Schema() json.RawMessage { return json.RawMessage(`{}`) }
func (t *stubLargeBudgetTool) Run(context.Context, json.RawMessage) (any, error) {
	items := make([]string, 5000)
	for i := range items {
		items[i] = fmt.Sprintf(`{"id":%d,"data":"some-padded-content-for-token-overflow"}`, i)
	}
	return "[" + strings.Join(items, ",") + "]", nil
}

func TestRegistryDispatch_AppliesOutputBudget(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&stubLargeBudgetTool{})
	out, err := reg.Dispatch(context.Background(), "large_budget_tool", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Dispatch failed: %v", err)
	}
	if !strings.HasPrefix(out, `{"_truncated":true`) {
		preview := out
		if len(preview) > 100 {
			preview = preview[:100]
		}
		t.Fatalf("expected truncated envelope, got: %s", preview)
	}
}
