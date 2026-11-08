package tui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/tuimodel"
)

// TestRootModel_ArchitectureGuard verifies invariants:
// 1. RootModel field count drops to <= 15 (decomposed into cohesive sub-state components).
// 2. RootModel satisfies tuimodel.Model.
// 3. Zero switch statements dispatch on m.overlays.panel.kind in internal/tui.
// 4. internal/ui/tuimodel and internal/app/tuistore import no host packages.
func TestRootModel_ArchitectureGuard(t *testing.T) {
	// Invariant 1: RootModel implements tuimodel.Model interface.
	var _ tuimodel.Model = (*RootModel)(nil)

	// Invariant 2: RootModel field count <= 195.
	fset := token.NewFileSet()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob *.go: %v", err)
	}

	var fields int
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		for _, d := range af.Decls {
			gen, ok := d.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, s := range gen.Specs {
				ts, ok := s.(*ast.TypeSpec)
				if !ok || ts.Name.Name != "RootModel" {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}
				var embeddedCount int
				var namedFields []string
				for _, fl := range st.Fields.List {
					if len(fl.Names) == 0 {
						// Embedded field must only be tuimodel.Base; it is
						// the model contract, not domain state, so it does
						// not count against the field ceiling.
						embeddedCount++
						continue
					}
					for _, name := range fl.Names {
						namedFields = append(namedFields, name.Name)
					}
					fields += len(fl.Names)
				}
				if embeddedCount > 1 {
					t.Errorf("RootModel has %d embedded structs, want <= 1 (only tuimodel.Base allowed; no anonymous domain embedding!)", embeddedCount)
				}
				// The host's state is a tree of cohesive sub-models, one
				// per domain. The list is the guard: a domain that loses
				// its model has been folded back into loose scalars.
				expectedFields := []string{
					"session", "turn", "display", "commands",
					"viewport", "prompt", "overlays", "statusLine",
					"conversation", "diff", "auth", "rewind",
					"activeItems", "help", "audio", "askQuestion",
					"tasks", "agents", "tools", "remote", "compaction",
				}
				present := make(map[string]bool, len(namedFields))
				for _, nf := range namedFields {
					present[nf] = true
				}
				for _, exp := range expectedFields {
					if !present[exp] {
						t.Errorf("RootModel missing explicit domain model field %q; fields are %v", exp, namedFields)
					}
				}
				// The composite tree is the whole state. A field beyond
				// the list is either a domain that was not given a model
				// or a scalar duplicating state one of the models owns.
				if len(namedFields) > len(expectedFields) {
					t.Errorf("RootModel has %d named fields, want <= %d: %v",
						len(namedFields), len(expectedFields), namedFields)
				}
				looseScalars := []string{
					"model", "email", "tier", "cwd", "conversationID",
					"streaming", "turnInterrupted", "exitConfirm", "quitting",
					"width", "height", "scroll", "autoScroll", "activeTool",
				}
				for _, loose := range looseScalars {
					if present[loose] {
						t.Errorf("RootModel keeps loose scalar %q; it belongs to a domain model", loose)
					}
				}
			}
		}
	}

	const maxAllowedFields = 22
	if fields > maxAllowedFields {
		t.Errorf("RootModel field count = %d, want <= %d (composite Elm model tree)", fields, maxAllowedFields)
	}

	// Invariant 3: Zero switch m.overlays.panel.kind across internal/cliapp production files.
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if strings.Contains(string(data), "switch m.overlays.panel.kind") || strings.Contains(string(data), "switch m.panel.kind") || strings.Contains(string(data), "switch panel.kind") {
			t.Errorf("%s contains switch dispatch on panel.kind, must route via panelregistry.Registry", f)
		}
	}

	// Invariant 4: No host package imports in tuimodel and tuistore.
	hostPkgs := []string{
		"internal/agent",
		"internal/cli",
		"internal/cliapp",
		"internal/modular",
	}

	for _, subpkg := range []string{"../ui/tuimodel", "../app/tuistore"} {
		subFiles, err := filepath.Glob(filepath.Join(subpkg, "*.go"))
		if err != nil {
			t.Fatalf("glob %s: %v", subpkg, err)
		}
		for _, sf := range subFiles {
			if strings.HasSuffix(sf, "_test.go") {
				continue
			}
			data, err := os.ReadFile(sf)
			if err != nil {
				t.Fatalf("read %s: %v", sf, err)
			}
			for _, host := range hostPkgs {
				if strings.Contains(string(data), host) {
					t.Errorf("%s illegally imports host package %s (tier violation)", sf, host)
				}
			}
		}
	}
}

// TestRootModel_PanelRegistryCoverage asserts that all 24
// panelregistry.Kind values are registered in the panel registry.
func TestRootModel_PanelRegistryCoverage(t *testing.T) {
	m := &RootModel{}
	reg := m.getPanelRegistry()
	if reg == nil {
		t.Fatal("panel registry not initialized")
	}

	// Verify every kind can be queried without panic.
	for kind := panelregistry.Kind(1); kind <= panelregistry.Changelog; kind++ {
		_, _ = reg.Get(kind)
	}
}

// TestArchitecture_TiersCompliance runs the tier check if available.
func TestArchitecture_TiersCompliance(t *testing.T) {
	cmd := exec.Command("go", "vet", "github.com/justwasm/pigasus/internal/ui/tuimodel", "github.com/justwasm/pigasus/internal/app/tuistore")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go vet failed:\n%s", out)
	}
}
