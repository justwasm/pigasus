package toolsteps

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStepTitle_NoErroredStringInPackageGoSources walks every .go file of
// the tool-step surface and asserts that the legacy wording no longer
// appears.
//
// The surface is the three packages that own it: this package (the
// transcript's step model and its rendering), internal/ui/tuitools (the
// rules that decide what a step says) and internal/cliapp (the host that
// paints the transcript). testdata/ is excluded because the
// print_changelog.md it holds is the byte-exact copy of `agy -p /changelog`
// and references the legacy title on purpose.
//
// The scan covers test sources too: the AGY 1.2.10 changelog gap is
// specifically about the tool-step surface, so no Go file of that surface
// may carry the old label — not even a test's failure message.
func TestStepTitle_NoErroredStringInPackageGoSources(t *testing.T) {
	pkgDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}

	// selfPath is the file containing THIS test. The legacy wording
	// appears in it on purpose — every comment above and every
	// failure-message format string names the label that is being
	// checked for. Excluding the test from itself is the only way to run
	// the scan without the scan trapping its own source.
	selfPath, err := filepath.Abs("legacy_test.go")
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}

	roots := []struct {
		dir  string
		name string
	}{
		{pkgDir, "toolsteps"},
		{filepath.Join(pkgDir, "..", "tuitools"), "tuitools"},
		{filepath.Join(pkgDir, "..", "..", "tui"), "tui"},
	}

	var hits []string
	for _, root := range roots {
		walkErr := filepath.WalkDir(root.dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			// Skip testdata — those files are reference fixtures or
			// byte-exact embedded AGY outputs and intentionally carry
			// the legacy wording in the changelog log.
			if d.IsDir() {
				if d.Name() == "testdata" || d.Name() == ".git" {
					return filepath.SkipDir
				}
				return nil
			}
			// Only check .go files (production + tests). The legacy
			// wording must not reappear in any Go source.
			if !strings.HasSuffix(d.Name(), ".go") {
				return nil
			}
			// Skip this file (see selfPath note above).
			if abs, aerr := filepath.Abs(path); aerr == nil && abs == selfPath {
				return nil
			}

			fset := token.NewFileSet()
			f, perr := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if perr != nil {
				return perr
			}

			rel, _ := filepath.Rel(root.dir, path)
			label := root.name + "/" + filepath.ToSlash(rel)

			// Walk comments — both // ... and /* ... */ — and flag any
			// that contain the legacy wording.
			for _, cg := range f.Comments {
				for _, c := range cg.List {
					if strings.Contains(c.Text, "Errored") {
						hits = append(hits, label+":"+c.Text)
					}
				}
			}

			// Walk expression statements / basic literals and flag any
			// string literal that contains the legacy wording.
			// Identifiers are excluded — the label is a word, not a
			// token, and a future Go identifier like `erroredFlag` is
			// unrelated.
			ast.Inspect(f, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				if strings.Contains(lit.Value, "Errored") {
					hits = append(hits, label+":"+lit.Value)
				}
				return true
			})
			return nil
		})
		if walkErr != nil {
			t.Fatalf("walk %s: %v", root.name, walkErr)
		}
	}

	if len(hits) > 0 {
		t.Fatalf("found legacy wording \"Errored\" in the tool-step surface's Go sources:\n  %s\nAGY 1.2.10 renamed that label to \"Failed\". testdata fixtures are excluded because they hold byte-exact AGY /changelog output we do not own.",
			strings.Join(hits, "\n  "))
	}
}
