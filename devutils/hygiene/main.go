// Command hygiene is a stdlib-only Go AST analyzer that reports high
// cyclomatic complexity functions and production task-marker counts.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	skipDirs = map[string]bool{
		".git":         true,
		"node_modules": true,
		"out":          true,
		"scratch":      true,
		"testdata":     true,
	}
	generatedRe = regexp.MustCompile(`(?m)^// Code generated .* DO NOT EDIT\.$`)
	todoFixmeRe = regexp.MustCompile(`\b(TODO|FIXME)\b`)
)

type options struct {
	root          string
	maxComplexity int
	includeTests  bool
	ratchet       string
}

type funcComplexity struct {
	File       string
	Line       int
	Name       string
	Complexity int
}

type fileTodoCount struct {
	File  string
	Todo  int
	Fixme int
}

func (f fileTodoCount) Total() int {
	return f.Todo + f.Fixme
}

type report struct {
	ProdFilesScanned       int
	ComplexityFilesScanned int
	ComplexFuncs           []funcComplexity
	TodoCount              int
	FixmeCount             int
	FileTodos              []fileTodoCount
	ParseErrors            []string
	Packages               map[string]*packageMetrics
	PackageParseErrors     map[string][]string
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fsFlags := flag.NewFlagSet("hygiene", flag.ContinueOnError)
	fsFlags.SetOutput(stderr)

	var opts options
	fsFlags.StringVar(&opts.root, "root", ".", "repository root directory to scan")
	fsFlags.IntVar(&opts.maxComplexity, "max-complexity", 30, "cyclomatic complexity warning threshold")
	fsFlags.BoolVar(&opts.includeTests, "include-tests", false, "include *_test.go files in cyclomatic complexity checks")
	fsFlags.StringVar(&opts.ratchet, "ratchet", "", "reviewed modularity baseline JSON file (fail on package LOC or receiver method growth)")

	if err := fsFlags.Parse(args); err != nil {
		return 2
	}
	var emptyRatchet bool
	fsFlags.Visit(func(f *flag.Flag) {
		if f.Name == "ratchet" && opts.ratchet == "" {
			emptyRatchet = true
		}
	})
	if emptyRatchet {
		fmt.Fprintln(stderr, "hygiene: -ratchet requires a baseline JSON path")
		return 1
	}

	var baseline *modularityBaseline
	if opts.ratchet != "" {
		var err error
		baseline, err = loadModularityBaseline(opts.ratchet)
		if err != nil {
			fmt.Fprintf(stderr, "hygiene: %v\n", err)
			return 1
		}
	}

	rep, err := analyze(opts)
	if err != nil {
		fmt.Fprintf(stderr, "hygiene: %v\n", err)
		return 1
	}

	printReport(stdout, opts, rep)
	if baseline == nil {
		return 0
	}
	return printAndCheckRatchet(stdout, stderr, rep, baseline)
}

func analyze(opts options) (*report, error) {
	info, err := os.Stat(opts.root)
	if err != nil {
		return nil, fmt.Errorf("stat root %q: %w", opts.root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("root %q is not a directory", opts.root)
	}

	rep := &report{}
	fset := token.NewFileSet()

	err = filepath.WalkDir(opts.root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if path != opts.root && skipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}

		name := d.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, ".pb.go") {
			return nil
		}

		isTest := strings.HasSuffix(name, "_test.go")
		if isTest && !opts.includeTests {
			return nil
		}

		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if isGenerated(src) {
			return nil
		}

		rel, err := filepath.Rel(opts.root, path)
		if err != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)

		pkg := filepath.ToSlash(filepath.Dir(rel))
		file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
		if err != nil {
			rep.ParseErrors = append(rep.ParseErrors, fmt.Sprintf("%s: %v", rel, err))
			if !isTest {
				if rep.PackageParseErrors == nil {
					rep.PackageParseErrors = make(map[string][]string)
				}
				rep.PackageParseErrors[pkg] = append(rep.PackageParseErrors[pkg], strings.ReplaceAll(err.Error(), path, rel))
			}
			return nil
		}

		rep.ComplexityFilesScanned++
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			c := cyclomaticComplexity(fn)
			if c > opts.maxComplexity {
				pos := fset.Position(fn.Pos())
				rep.ComplexFuncs = append(rep.ComplexFuncs, funcComplexity{
					File:       rel,
					Line:       pos.Line,
					Name:       formatFuncName(fn),
					Complexity: c,
				})
			}
		}

		if !isTest {
			rep.ProdFilesScanned++
			if opts.ratchet != "" {
				rep.recordPackageMetrics(pkg, src, file)
			}
			todo, fixme := countTodoFixme(file)
			if todo > 0 || fixme > 0 {
				rep.TodoCount += todo
				rep.FixmeCount += fixme
				rep.FileTodos = append(rep.FileTodos, fileTodoCount{
					File:  rel,
					Todo:  todo,
					Fixme: fixme,
				})
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(rep.ComplexFuncs, func(i, j int) bool {
		if rep.ComplexFuncs[i].Complexity != rep.ComplexFuncs[j].Complexity {
			return rep.ComplexFuncs[i].Complexity > rep.ComplexFuncs[j].Complexity
		}
		if rep.ComplexFuncs[i].File != rep.ComplexFuncs[j].File {
			return rep.ComplexFuncs[i].File < rep.ComplexFuncs[j].File
		}
		return rep.ComplexFuncs[i].Line < rep.ComplexFuncs[j].Line
	})

	sort.Slice(rep.FileTodos, func(i, j int) bool {
		if rep.FileTodos[i].Total() != rep.FileTodos[j].Total() {
			return rep.FileTodos[i].Total() > rep.FileTodos[j].Total()
		}
		return rep.FileTodos[i].File < rep.FileTodos[j].File
	})

	return rep, nil
}

func isGenerated(src []byte) bool {
	// Check only the header portion before the package declaration or first 1KB.
	limit := len(src)
	if limit > 1024 {
		limit = 1024
	}
	return generatedRe.Match(bytes.TrimSpace(src[:limit]))
}

func cyclomaticComplexity(fn *ast.FuncDecl) int {
	complexity := 1
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt:
			complexity++
		case *ast.CaseClause:
			if len(node.List) > 0 {
				complexity++
			}
		case *ast.CommClause:
			if node.Comm != nil {
				complexity++
			}
		case *ast.BinaryExpr:
			if node.Op == token.LAND || node.Op == token.LOR {
				complexity++
			}
		}
		return true
	})
	return complexity
}

func formatFuncName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	recv := exprString(fn.Recv.List[0].Type)
	if strings.HasPrefix(recv, "*") {
		return fmt.Sprintf("(%s).%s", recv, fn.Name.Name)
	}
	return fmt.Sprintf("%s.%s", recv, fn.Name.Name)
}

func exprString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + exprString(t.X)
	case *ast.IndexExpr:
		return exprString(t.X) + "[" + exprString(t.Index) + "]"
	case *ast.IndexListExpr:
		return exprString(t.X) + "[...]"
	case *ast.SelectorExpr:
		return exprString(t.X) + "." + t.Sel.Name
	default:
		return "recv"
	}
}

func countTodoFixme(file *ast.File) (todo, fixme int) {
	for _, cg := range file.Comments {
		for _, c := range cg.List {
			matches := todoFixmeRe.FindAllString(c.Text, -1)
			for _, m := range matches {
				switch m {
				case "TODO":
					todo++
				case "FIXME":
					fixme++
				}
			}
		}
	}
	return todo, fixme
}

func printReport(w io.Writer, opts options, rep *report) {
	if len(rep.ParseErrors) > 0 {
		fmt.Fprintf(w, "WARNING: skipped %d file(s) with parse errors:\n", len(rep.ParseErrors))
		for _, pe := range rep.ParseErrors {
			fmt.Fprintf(w, "  %s\n", pe)
		}
	}

	if len(rep.ComplexFuncs) > 0 {
		fmt.Fprintf(w, "WARNING: functions with cyclomatic complexity > %d (%d found across %d files):\n",
			opts.maxComplexity, len(rep.ComplexFuncs), rep.ComplexityFilesScanned)
		for _, fn := range rep.ComplexFuncs {
			fmt.Fprintf(w, "  %3d  %s:%d  %s\n", fn.Complexity, fn.File, fn.Line, fn.Name)
		}
	} else {
		fmt.Fprintf(w, "==> No functions exceeding cyclomatic complexity %d (%d files scanned).\n",
			opts.maxComplexity, rep.ComplexityFilesScanned)
	}

	fmt.Fprintf(w, "==> Production TODO/FIXME summary (%d production .go files): TODO=%d, FIXME=%d (across %d files)\n",
		rep.ProdFilesScanned, rep.TodoCount, rep.FixmeCount, len(rep.FileTodos))
	if len(rep.FileTodos) > 0 {
		limit := len(rep.FileTodos)
		if limit > 10 {
			limit = 10
		}
		fmt.Fprintf(w, "    Top files by TODO/FIXME count:\n")
		for _, ft := range rep.FileTodos[:limit] {
			fmt.Fprintf(w, "      %3d (TODO=%d, FIXME=%d)  %s\n", ft.Total(), ft.Todo, ft.Fixme, ft.File)
		}
	}
}
