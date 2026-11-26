package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"
	"io"
	"os"
	"path"
	"sort"
	"strings"
)

// Package metrics cover direct package files, not subpackages, and all build
// tags/platforms (like the existing analyzer). LOC means physical source lines,
// including blank lines and comments, with an unterminated final line counted.
// Only successfully parsed production files contribute; ratchet checks fail
// closed on production parse errors in watched packages. The existing analyzer
// supplies the shared test/.pb.go/generated-header exclusions.
type packageMetrics struct {
	LOC       int
	Files     int
	Receivers map[string]int
}

// Baselines are reviewed ceilings, never generated or changed by this command.
// A pointer distinguishes an explicit zero ceiling from a missing/null field.
type modularityBaseline struct {
	Version        int                       `json:"version"`
	SourceRevision string                    `json:"source_revision,omitempty"`
	Packages       map[string]packageCeiling `json:"packages"`
}

type packageCeiling struct {
	MaxLOC *int `json:"max_loc"`
	// MaxFiles caps the number of production files in the package, so a
	// package cannot regain cohesion by fanning out into more files.
	MaxFiles *int `json:"max_files,omitempty"`
	// ReportOnly keeps a ceiling advisory: it prints the delta but never
	// fails the run. Used for packages the roadmap has not yet decomposed,
	// so growth is visible without blocking unrelated work.
	ReportOnly bool            `json:"report_only,omitempty"`
	Receivers  map[string]*int `json:"receivers,omitempty"`
}

func loadModularityBaseline(filename string) (*modularityBaseline, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("read ratchet baseline %q: %w", filename, err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var baseline modularityBaseline
	if err := dec.Decode(&baseline); err != nil {
		return nil, fmt.Errorf("parse ratchet baseline %q: %w", filename, err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("parse ratchet baseline %q: expected exactly one JSON object", filename)
	}
	if baseline.Version != 1 {
		return nil, fmt.Errorf("invalid ratchet baseline %q: version must be 1", filename)
	}
	if len(baseline.Packages) == 0 {
		return nil, fmt.Errorf("invalid ratchet baseline %q: packages must not be empty", filename)
	}
	for _, pkg := range sortedKeys(baseline.Packages) {
		ceiling := baseline.Packages[pkg]
		if !validPackagePath(pkg) {
			return nil, fmt.Errorf("invalid ratchet baseline %q: invalid root-relative package path %q", filename, pkg)
		}
		if ceiling.MaxLOC == nil || *ceiling.MaxLOC < 0 {
			return nil, fmt.Errorf("invalid ratchet baseline %q: package %q requires nonnegative max_loc", filename, pkg)
		}
		if ceiling.MaxFiles != nil && *ceiling.MaxFiles < 0 {
			return nil, fmt.Errorf("invalid ratchet baseline %q: package %q requires nonnegative max_files", filename, pkg)
		}
		for _, receiver := range sortedKeys(ceiling.Receivers) {
			maxMethods := ceiling.Receivers[receiver]
			if !token.IsIdentifier(receiver) || maxMethods == nil || *maxMethods < 0 {
				return nil, fmt.Errorf("invalid ratchet baseline %q: package %q receiver %q requires a Go type name and nonnegative method ceiling", filename, pkg, receiver)
			}
		}
	}
	return &baseline, nil
}

func validPackagePath(pkg string) bool {
	return pkg != "" && !path.IsAbs(pkg) && path.Clean(pkg) == pkg &&
		pkg != ".." && !strings.HasPrefix(pkg, "../") &&
		!strings.ContainsAny(pkg, "\\:")
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (rep *report) recordPackageMetrics(pkg string, src []byte, file *ast.File) {
	if rep.Packages == nil {
		rep.Packages = make(map[string]*packageMetrics)
	}
	m := rep.Packages[pkg]
	if m == nil {
		m = &packageMetrics{Receivers: make(map[string]int)}
		rep.Packages[pkg] = m
	}
	m.Files++
	m.LOC += physicalLines(src)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 {
			continue
		}
		// Value/pointer receivers and generic instantiations share a type
		// ceiling. Count declarations, including assembly-backed methods.
		if receiver := receiverTypeName(fn.Recv.List[0].Type); receiver != "" {
			m.Receivers[receiver]++
		}
	}
}

func physicalLines(src []byte) int {
	lines := bytes.Count(src, []byte{'\n'})
	if len(src) > 0 && src[len(src)-1] != '\n' {
		lines++
	}
	return lines
}

func receiverTypeName(expr ast.Expr) string {
	switch node := expr.(type) {
	case *ast.Ident:
		return node.Name
	case *ast.StarExpr:
		return receiverTypeName(node.X)
	case *ast.IndexExpr:
		return receiverTypeName(node.X)
	case *ast.IndexListExpr:
		return receiverTypeName(node.X)
	case *ast.ParenExpr:
		return receiverTypeName(node.X)
	default:
		return ""
	}
}

func printAndCheckRatchet(stdout, stderr io.Writer, rep *report, baseline *modularityBaseline) int {
	var violations []string
	fmt.Fprintln(stdout, "==> Modularity ratchet (direct-package production physical LOC/files; all build tags/platforms):")
	for _, pkg := range sortedKeys(baseline.Packages) {
		ceiling := baseline.Packages[pkg]
		// advisory collects this package's over-ceiling notes; they are
		// printed either way and only become violations when the ceiling
		// is binding.
		var advisory []string
		note := func(format string, args ...any) {
			advisory = append(advisory, fmt.Sprintf(format, args...))
		}
		if errors := rep.PackageParseErrors[pkg]; len(errors) > 0 {
			for _, parseError := range errors {
				violations = append(violations, fmt.Sprintf("%s cannot be measured: %s", pkg, parseError))
			}
			continue
		}
		m := rep.Packages[pkg]
		if m == nil {
			violations = append(violations, fmt.Sprintf("%s has no production Go files; check the watched package path", pkg))
			continue
		}
		fmt.Fprintf(stdout, "    %s: LOC=%d ceiling=%d delta=%+d files=%d\n", pkg, m.LOC, *ceiling.MaxLOC, m.LOC-*ceiling.MaxLOC, m.Files)
		if m.LOC > *ceiling.MaxLOC {
			note("%s production LOC %d > ceiling %d (+%d)", pkg, m.LOC, *ceiling.MaxLOC, m.LOC-*ceiling.MaxLOC)
		}
		if ceiling.MaxFiles != nil {
			fmt.Fprintf(stdout, "    %s: files=%d ceiling=%d delta=%+d\n", pkg, m.Files, *ceiling.MaxFiles, m.Files-*ceiling.MaxFiles)
			if m.Files > *ceiling.MaxFiles {
				note("%s production files %d > ceiling %d (+%d)", pkg, m.Files, *ceiling.MaxFiles, m.Files-*ceiling.MaxFiles)
			}
		}
		for _, receiver := range sortedKeys(ceiling.Receivers) {
			got, max := m.Receivers[receiver], *ceiling.Receivers[receiver]
			fmt.Fprintf(stdout, "    %s.%s: methods=%d ceiling=%d delta=%+d\n", pkg, receiver, got, max, got-max)
			if got > max {
				note("%s.%s receiver methods %d > ceiling %d (+%d)", pkg, receiver, got, max, got-max)
			}
		}
		if len(advisory) == 0 {
			continue
		}
		if ceiling.ReportOnly {
			for _, a := range advisory {
				fmt.Fprintf(stdout, "    [advisory] %s\n", a)
			}
			continue
		}
		violations = append(violations, advisory...)
	}
	if len(violations) == 0 {
		fmt.Fprintln(stdout, "==> Modularity ratchet passed.")
		return 0
	}
	fmt.Fprintf(stderr, "hygiene: modularity ratchet failed (%d violation(s)):\n", len(violations))
	for _, violation := range violations {
		fmt.Fprintf(stderr, "  %s\n", violation)
	}
	fmt.Fprintln(stderr, "Reduce growth or explicitly review the baseline; it was not modified.")
	return 1
}
