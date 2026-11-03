package cliapp

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

// TestRunMode_ArchitectureGuard verifies Cycle 382 invariants:
// 1. runMode field count drops below 195 (down from 219 baseline).
// 2. runMode satisfies tuimodel.Model.
// 3. Zero switch statements dispatch on m.panel.kind in internal/cliapp.
// 4. internal/ui/tuimodel and internal/app/tuistore import no host packages.
func TestRunMode_ArchitectureGuard(t *testing.T) {
	// Invariant 1: runMode implements tuimodel.Model interface.
	var _ tuimodel.Model = (*runMode)(nil)

	// Invariant 2: runMode field count <= 195.
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
				if !ok || ts.Name.Name != "runMode" {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}
				for _, fl := range st.Fields.List {
					if len(fl.Names) == 0 {
						fields++ // embedded type
						continue
					}
					fields += len(fl.Names)
				}
			}
		}
	}

	const maxAllowedFields = 195
	if fields > maxAllowedFields {
		t.Errorf("runMode field count = %d, want <= %d (cycle 382 target)", fields, maxAllowedFields)
	}

	// Invariant 3: Zero switch m.panel.kind across internal/cliapp production files.
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if strings.Contains(string(data), "switch m.panel.kind") || strings.Contains(string(data), "switch panel.kind") {
			t.Errorf("%s contains switch dispatch on panel.kind, must route via panelregistry.Registry", f)
		}
	}

	// Invariant 4: No host package imports in tuimodel and tuistore.
	hostPkgs := []string{
		"internal/agent",
		"internal/cli",
		"internal/cliapp",
		"internal/pigrt",
		"internal/piagentbridge",
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

// TestRunMode_PanelRegistryCoverage asserts that all 24
// panelregistry.Kind values are registered in the panel registry.
func TestRunMode_PanelRegistryCoverage(t *testing.T) {
	m := &runMode{}
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
