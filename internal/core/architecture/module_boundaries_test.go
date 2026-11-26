package architecture

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// A tier check permits a codec in Tier 3 to import an HTTP client in Tier 3.
// That is legal layering but breaks the narrower scenario-first contract: the
// codec must run without a transport or ambient process state. These reviewed
// rules complement (rather than replace) the module-wide tier checks.
type moduleBoundary struct {
	path    string
	blocked []string
}

var scenarioBoundaries = []moduleBoundary{
	{
		path: "internal/ui/editor/selection",
		blocked: []string{
			"charm.land/bubbletea", "os", "os/exec", "net/http", "time",
			modulePath + "/internal/tui",
			modulePath + "/internal/cli",
			modulePath + "/internal/infra",
			modulePath + "/internal/app",
		},
	},
}

// boundaryViolations parses every production source file, including platform
// alternatives. Do not allow a dependency regression to hide behind GOOS or a
// fixture's fake import spelling; decisions are made on AST import paths.
func boundaryViolations(root string, rule moduleBoundary) ([]string, error) {
	dir := filepath.Join(root, filepath.FromSlash(rule.path))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var problems []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, parser.ImportsOnly)
		if err != nil {
			return nil, err
		}
		for _, imp := range file.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return nil, err
			}
			for _, blocked := range rule.blocked {
				if path != blocked && !strings.HasPrefix(path, blocked+"/") {
					continue
				}
				problems = append(problems, fmt.Sprintf("%s/%s imports %q (forbidden boundary %q)", rule.path, name, path, blocked))
				break
			}
		}
	}
	sort.Strings(problems)
	return problems, nil
}

func TestScenarioModuleBoundaries(t *testing.T) {
	root := findModuleRoot(".")
	if root == "" {
		t.Fatal("cannot find module root")
	}
	for _, rule := range scenarioBoundaries {
		t.Run(rule.path, func(t *testing.T) {
			problems, err := boundaryViolations(root, rule)
			if err != nil {
				t.Fatal(err)
			}
			for _, problem := range problems {
				t.Error(problem)
			}
		})
	}
}

func TestBoundaryViolationsScenarios(t *testing.T) {
	for _, tc := range []struct {
		name    string
		source  string
		file    string
		blocked []string
		want    int
	}{
		{name: "pure bytes mapping", source: "package codec\nimport \"encoding/json\"", file: "codec.go", blocked: []string{"net/http", "os"}},
		{name: "HTTP transport rejected", source: "package codec\nimport \"net/http\"", file: "codec.go", blocked: []string{"net/http"}, want: 1},
		{name: "subprocess rejected", source: "package codec\nimport _ \"os/exec\"", file: "codec.go", blocked: []string{"os"}, want: 1},
		{name: "platform alternative still guarded", source: "//go:build impossible_platform\n\npackage codec\nimport \"net/http\"", file: "codec_plan9.go", blocked: []string{"net/http"}, want: 1},
		{name: "tests may own transport harness", source: "package codec_test\nimport \"net/http/httptest\"", file: "codec_test.go", blocked: []string{"net/http"}},
		{name: "prefix uses package boundary", source: "package codec\nimport \"net/httpmock\"", file: "codec.go", blocked: []string{"net/http"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "codec")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, tc.file), []byte(tc.source), 0o644); err != nil {
				t.Fatal(err)
			}
			problems, err := boundaryViolations(root, moduleBoundary{path: "codec", blocked: tc.blocked})
			if err != nil {
				t.Fatal(err)
			}
			if len(problems) != tc.want {
				t.Fatalf("got %v, want %d violations", problems, tc.want)
			}
		})
	}
}
