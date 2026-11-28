package permissions

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRootFromTest walks up from the test's working directory until it finds
// go.mod. The test runs with its own package directory as the working
// directory, so a fixed number of ".." would break the day the package moves.
func repoRootFromTest(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above %s", dir)
		}
		dir = parent
	}
}

// TestReviewerModelIsNotWired pins the claim the TODO in policy.go makes: the
// Auto-mode reviewer model is recovered reference data, not a live code path.
//
// The guard is worth having because the constants read as a capability. A
// reader who finds AutoModeReviewerModel and ReviewerModelForBase reasonably
// concludes that Auto mode consults a model before letting a command out of the
// sandbox. It does not — Auto mode is decided by Policy.Decide plus
// assessor.EvaluateForAutoMode, both of which are pure local rules. The day
// someone does wire it up, this test fails and says what to update, so the
// comment cannot silently go stale in the other direction.
func TestReviewerModelIsNotWired(t *testing.T) {
	root := repoRootFromTest(t)
	self := filepath.Join("internal", "core", "policy", "permissions", "policy.go")

	// The definition file is the one place these identifiers are allowed to
	// appear. Any other production file naming one of them is a caller.
	needles := []string{
		"ReviewerModelForBase",
		"AutoModeReviewerModel",
		"AutoModeHydrogenReviewerModel",
	}

	var callers []string
	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", "out", "bin":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == self {
			return nil
		}
		// Parse rather than substring-match: the TODO comments themselves
		// name these identifiers, and a comment is not a caller. Only a real
		// identifier reference counts.
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			// A file that does not parse is not this test's business; the
			// build catches it first.
			return nil
		}
		found := ""
		ast.Inspect(f, func(n ast.Node) bool {
			if found != "" {
				return false
			}
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			for _, needle := range needles {
				if id.Name == needle {
					found = needle
					return false
				}
			}
			return true
		})
		if found != "" {
			callers = append(callers, rel+" references "+found)
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk %s: %v", root, walkErr)
	}

	if len(callers) > 0 {
		t.Fatalf("the Auto-mode reviewer model is now wired up (%s); delete the "+
			"TODO(auto-mode-model-review) comments in permissions/policy.go and "+
			"toolrunner/pipeline.go, and replace this test with one that covers the "+
			"review path", strings.Join(callers, ", "))
	}
}

// TestReviewerModelTODOIsPresent asserts the marker itself survives.
//
// Without it, "unwired" is a fact only this test knows, and the next reader of
// the constants is back to assuming a capability that is not there.
func TestReviewerModelTODOIsPresent(t *testing.T) {
	root := repoRootFromTest(t)

	for _, rel := range []string{
		filepath.Join("internal", "core", "policy", "permissions", "policy.go"),
		filepath.Join("internal", "app", "toolrunner", "pipeline.go"),
	} {
		src, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if !strings.Contains(string(src), "TODO(auto-mode-model-review)") {
			t.Errorf("%s lost its TODO(auto-mode-model-review) marker; the "+
				"reviewer model is still unwired, so the note is still owed", rel)
		}
	}
}
