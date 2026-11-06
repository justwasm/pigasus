package architecture

import (
	"bytes"
	"fmt"
	"go/build"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The guard in this file is the same measurement devutils/tiergraph.py
// performs, moved into the tree so that a violation fails `go test`
// instead of waiting for somebody to remember the script.
//
// The scope is the whole module, not only internal/: the public-package
// rule (R6) is about the module's root packages, so the guard has to see
// them. The internal/ subtree is measured separately and reported next to
// the module-wide numbers, so the two can be compared with the script's.
//
// Everything here reads the tree with go/parser and go/build and imports
// nothing outside the standard library, which is what lets the guard sit
// in the core tier: it measures the tree rather than being part of it.

const modulePath = "github.com/justwasm/pigasus"

// tiers lists the five tiers in dependency order. A package under one of
// these prefixes inherits that tier; anything else is untiered.
var tiers = []struct {
	prefix string
	number int
	name   string
}{
	{"internal/core", 1, "core"},
	{"internal/features", 2, "features"},
	{"internal/infra", 3, "infra"},
	{"internal/app", 4, "app"},
	{"internal/ui", 5, "ui"},
}

// hosts lists the entry hosts: the packages that sit above every
// tier and own the turn engines or run a session.
var hosts = []string{
	"internal/modular",
	"internal/cli",
	"internal/clirunner",
	"internal/tui",
	"internal/printmode",
	"internal/rchost",
	"internal/lsserver",
}

// engineHosts are the hosts the app tier may reach: AGENTS.md lets the
// app tier use the turn engines, and only the app tier.
var engineHosts = []string{
	"internal/modular",
	"internal/cli",
}

// entryHosts are the hosts that sit above every tier: an entry surface is
// not a tier, so no tier may import one — the app tier included.
var entryHosts = []string{
	"internal/clirunner",
	"internal/tui",
	"internal/printmode",
	"internal/rchost",
	"internal/lsserver",
}

// skipDirs are the directories the walk never enters. out/ holds the
// evidence, scratch/ has never compiled, and testdata/ holds fixtures
// rather than packages.
var skipDirs = map[string]bool{
	".git":         true,
	"out":          true,
	"scratch":      true,
	"testdata":     true,
	"node_modules": true,
}

// knownExceptions lists what the rules below forgive. Each entry names the
// packages involved and why the rule does not apply to them.
//
// The list is empty unless a rule genuinely does not hold on the tree. An
// entry is a decision, not a hiding place: the guard prints the list on
// every run, and a new violation next to an excused one still fails.
var knownExceptions = []exception{}

// unclassifiedSubsystems records the legacy internal packages awaiting tier placement or host migration.
// This is a strict ratchet to prevent architecture escape: any package under internal/ must either be
// in tiers 1..5, be a registered host, or be documented here.
var unclassifiedSubsystems = map[string]string{
	"internal/agyupdate":                "auto-updater utility",
	"internal/codeium_common_pb":        "generated protobuf contracts",
	"internal/cortex":                   "cortex turn engine compatibility adapter",
	"internal/cortex_pb":                "generated protobuf contracts",
	"internal/extension_server_pb":      "generated protobuf contracts",
	"internal/language_server":          "language server domain handler",
	"internal/language_server_pb":       "generated protobuf contracts",
	"internal/planrender":               "markdown plan rendering helper",
	"internal/rcaudio":                  "remote control audio streamer",
	"internal/rcfrontend":               "remote control web UI assets and routes",
	"internal/rctunnel":                 "remote control tunnel manager",
	"internal/rctunnel/patches":         "tunnel patch set",
	"internal/remotecontrol":            "remote control RPC dispatcher",
	"internal/remotecontrol/cascades":   "remote control cascades controller",
	"internal/remotecontrol/convconfig": "remote control conversation config",
	"internal/remotecontrol/effort":     "remote control reasoning effort vocabulary",
	"internal/remotecontrol/fastpush":   "remote control fastpush controller",
	"internal/remotecontrol/files":      "remote control file inspector",
	"internal/remotecontrol/mendel":     "remote control mendel flag cache",
	"internal/remotecontrol/origins":    "remote control origin policy",
	"internal/remotecontrol/permissions": "remote control permission CRUD store",
	"internal/remotecontrol/plugins":    "remote control plugin metadata manager",
	"internal/remotecontrol/projects":   "remote control project controller",
	"internal/remotecontrol/resources":  "remote control resource provider",
	"internal/remotecontrol/rpc":        "remote control JSON-RPC wire codec",
	"internal/remotecontrol/terminals":  "remote control terminal multiplexer",
	"internal/remotecontrol/triggers":   "remote control trigger orchestrator",
	"internal/voice":                    "voice dictation driver",
	"internal/voice/voicepb":            "generated voice protobuf contracts",
}

// exception is one documented exemption from one rule.
type exception struct {
	rule   string
	pkgs   []string
	reason string
	// testOnly limits the exemption to the edges a _test.go file makes.
	// An exemption that reaches into the shipped graph would excuse a
	// violation nobody has committed yet.
	testOnly bool
}

// covers reports whether the exception applies to an edge between two
// packages. The pair is unordered: an edge is an edge.
func (e exception) covers(from, to string) bool {
	if len(e.pkgs) != 2 {
		return false
	}
	return (e.pkgs[0] == from && e.pkgs[1] == to) || (e.pkgs[0] == to && e.pkgs[1] == from)
}

// coversCycle reports whether the exception applies to a cycle with the
// given members.
func (e exception) coversCycle(members []string) bool {
	if len(e.pkgs) != len(members) {
		return false
	}
	want := map[string]bool{}
	for _, p := range e.pkgs {
		want[p] = true
	}
	for _, p := range members {
		if !want[p] {
			return false
		}
	}
	return true
}

// violation is one broken rule: the edge, the file that makes it, and the
// rule it breaks.
type violation struct {
	rule    string
	from    string
	to      string
	file    string
	members []string // the packages of a cycle, for the rules that report one
}

// String renders a violation the way the report prints it.
func (v violation) String() string {
	if len(v.members) > 0 {
		return strings.Join(v.members, " -> ")
	}
	return fmt.Sprintf("%s -> %s (%s)", v.from, v.to, v.file)
}

// tree is the module's package graph, split into the production graph and
// the test-only graph.
type tree struct {
	root string
	pkgs map[string]*pkgInfo
	// nested holds the module paths of the nested modules the walk
	// skipped: their packages are not this module's packages.
	nested []string
}

// pkgInfo is one package: the imports its production files make, the
// imports its test files make, and where each of them was found.
type pkgInfo struct {
	path    string
	hasProd bool
	hasTest bool
	prod    map[string]string // imported package -> the file that imports it
	test    map[string]string
}

// pkg returns the package at a path, creating it on first sight.
func (t *tree) pkg(path string) *pkgInfo {
	if p, ok := t.pkgs[path]; ok {
		return p
	}
	p := &pkgInfo{path: path, prod: map[string]string{}, test: map[string]string{}}
	t.pkgs[path] = p
	return p
}

// prodPkgs returns the packages with at least one production file, sorted.
func (t *tree) prodPkgs() []*pkgInfo {
	var out []*pkgInfo
	for _, p := range t.pkgs {
		if p.hasProd {
			out = append(out, p)
		}
	}
	sortPkgs(out)
	return out
}

// testPkgs returns the packages with at least one test file, sorted. A
// directory that holds nothing but _test.go files is a package of the
// test-only graph.
func (t *tree) testPkgs() []*pkgInfo {
	var out []*pkgInfo
	for _, p := range t.pkgs {
		if p.hasTest {
			out = append(out, p)
		}
	}
	sortPkgs(out)
	return out
}

// allPkgs returns every package the walk found, sorted.
func (t *tree) allPkgs() []*pkgInfo {
	out := make([]*pkgInfo, 0, len(t.pkgs))
	for _, p := range t.pkgs {
		out = append(out, p)
	}
	sortPkgs(out)
	return out
}

// internalPkgs returns the packages under internal/, split the same way.
func internalPkgs(pkgs []*pkgInfo) (prod, test []*pkgInfo) {
	for _, p := range pkgs {
		if !strings.HasPrefix(p.path, "internal/") {
			continue
		}
		if p.hasProd {
			prod = append(prod, p)
		}
		if p.hasTest {
			test = append(test, p)
		}
	}
	return prod, test
}

func sortPkgs(pkgs []*pkgInfo) {
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].path < pkgs[j].path })
}

// edges returns the graph's edges: from package -> to package, with the
// file the import was found in.
func edges(pkgs []*pkgInfo, test bool) map[string][]edge {
	g := map[string][]edge{}
	for _, p := range pkgs {
		var es []edge
		src := p.prod
		if test {
			src = p.test
		}
		for to, file := range src {
			es = append(es, edge{to: to, file: file})
		}
		sort.Slice(es, func(i, j int) bool { return es[i].to < es[j].to })
		g[p.path] = es
	}
	return g
}

// edge is one import, with the file that carries it.
type edge struct {
	to   string
	file string
}

// findModuleRoot walks up from dir looking for the module's go.mod.
func findModuleRoot(dir string) string {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// collect walks the module and reads every package's imports.
//
// The walk skips the evidence, the fixtures and the nested modules: a
// nested module is a different module, and its packages are not this
// module's packages even when its module path is a prefix of this one's.
func collect(root string) (*tree, error) {
	tr := &tree{root: root, pkgs: map[string]*pkgInfo{}}
	dirs := map[string][]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == root {
				return nil
			}
			name := d.Name()
			if skipDirs[name] || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
				return fs.SkipDir
			}
			if mod, ok := nestedModulePath(filepath.Join(path, "go.mod")); ok {
				tr.nested = append(tr.nested, mod)
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		dir := filepath.Dir(path)
		dirs[dir] = append(dirs[dir], d.Name())
		return nil
	})
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	for dir, files := range dirs {
		rel, err := filepath.Rel(root, dir)
		if err != nil {
			return nil, err
		}
		pkgPath := filepath.ToSlash(rel)
		if pkgPath == "." || pkgPath == "internal" {
			continue
		}
		info := tr.pkg(pkgPath)
		for _, name := range files {
			isTest := strings.HasSuffix(name, "_test.go")
			ok, err := build.Default.MatchFile(dir, name)
			if err != nil || !ok {
				continue // a file the build does not select
			}
			src, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				return nil, err
			}
			if isTest {
				info.hasTest = true
			} else {
				info.hasProd = true
			}
			if !bytes.Contains(src, []byte(modulePath)) {
				continue // no import of this module can be in there
			}
			file, err := parser.ParseFile(fset, filepath.Join(dir, name), src, parser.ImportsOnly)
			if err != nil {
				continue // a file the compiler would reject anyway
			}
			relFile := filepath.ToSlash(filepath.Join(pkgPath, name))
			for _, spec := range file.Imports {
				imp, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					continue
				}
				target, ok := tr.moduleRelative(imp)
				if !ok {
					continue
				}
				bucket := info.prod
				if isTest {
					bucket = info.test
				}
				if cur, seen := bucket[target]; !seen || relFile < cur {
					bucket[target] = relFile
				}
			}
		}
	}
	return tr, nil
}

// nestedModulePath reads the module path out of a nested module's go.mod.
func nestedModulePath(goMod string) (string, bool) {
	src, err := os.ReadFile(goMod)
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(src), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "module") {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(line, "module"))
		if rest == "" {
			continue
		}
		if i := strings.Index(rest, "//"); i >= 0 {
			rest = strings.TrimSpace(rest[:i])
		}
		return strings.Trim(rest, `"`), true
	}
	return "", false
}

// moduleRelative turns an import path into this module's package path.
// The second value is false for an import that is not one of ours.
func (t *tree) moduleRelative(imp string) (string, bool) {
	if imp == modulePath {
		return "", true
	}
	if !strings.HasPrefix(imp, modulePath+"/") {
		return "", false
	}
	rel := imp[len(modulePath)+1:]
	for _, nested := range t.nested {
		if imp == nested || strings.HasPrefix(imp, nested+"/") {
			return "", false
		}
	}
	return rel, true
}

// tierOf returns the tier a package belongs to, or 0 when it belongs to
// none. A package under a tier prefix inherits that tier.
func tierOf(pkg string) int {
	for _, t := range tiers {
		if pkg == t.prefix || strings.HasPrefix(pkg, t.prefix+"/") {
			return t.number
		}
	}
	return 0
}

// tierName returns the tier's name, for a message.
func tierName(pkg string) string {
	n := tierOf(pkg)
	if n == 0 {
		return "untiered"
	}
	return tiers[n-1].name
}

// isHost reports whether a package is one of the hosts, or lives under
// one.
func isHost(pkg string) bool {
	return underAny(pkg, hosts)
}

// isEngineHost reports whether a package is one of the turn engines, or
// lives under one.
func isEngineHost(pkg string) bool {
	return underAny(pkg, engineHosts)
}

// isEntryHost reports whether a package is one of the entry surfaces, or
// lives under one.
func isEntryHost(pkg string) bool {
	return underAny(pkg, entryHosts)
}

func underAny(pkg string, prefixes []string) bool {
	for _, p := range prefixes {
		if pkg == p || strings.HasPrefix(pkg, p+"/") {
			return true
		}
	}
	return false
}

// isPublic reports whether a package is one of the module's public
// packages: a package at the module's root, outside internal/ and cmd/.
// Those are the compatibility promise the tree makes to third parties.
func isPublic(pkg string) bool {
	if pkg == "" || strings.Contains(pkg, "/") {
		return false
	}
	switch pkg {
	case "internal", "cmd":
		return false
	}
	return true
}

// findCycles returns the strongly connected components of a graph that
// have more than one member. Each component is sorted.
func findCycles(g map[string][]edge) [][]string {
	index := map[string]int{}
	low := map[string]int{}
	onStack := map[string]bool{}
	var stack []string
	var result [][]string
	counter := 0

	var strongConnect func(node string)
	strongConnect = func(node string) {
		index[node] = counter
		low[node] = counter
		counter++
		stack = append(stack, node)
		onStack[node] = true

		for _, e := range g[node] {
			w := e.to
			if _, ok := g[w]; !ok {
				continue // an import of something that is not a package here
			}
			if _, seen := index[w]; !seen {
				strongConnect(w)
				if low[w] < low[node] {
					low[node] = low[w]
				}
			} else if onStack[w] {
				if index[w] < low[node] {
					low[node] = index[w]
				}
			}
		}

		if low[node] == index[node] {
			var comp []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				comp = append(comp, w)
				if w == node {
					break
				}
			}
			if len(comp) > 1 {
				sort.Strings(comp)
				result = append(result, comp)
			}
		}
	}

	nodes := make([]string, 0, len(g))
	for n := range g {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)
	for _, n := range nodes {
		if _, seen := index[n]; !seen {
			strongConnect(n)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.Join(result[i], ",") < strings.Join(result[j], ",")
	})
	return result
}

// exempt reports whether an exception covers the violation.
func exempt(v violation) (exception, bool) {
	for _, e := range knownExceptions {
		if e.rule != v.rule {
			continue
		}
		if len(v.members) > 0 {
			if e.coversCycle(v.members) {
				return e, true
			}
			continue
		}
		if e.covers(v.from, v.to) && (!e.testOnly || strings.HasSuffix(v.file, "_test.go")) {
			return e, true
		}
	}
	return exception{}, false
}

// exceptionLive reports whether the exception still excuses something.
func exceptionLive(e exception, violations []violation) bool {
	for _, v := range violations {
		if v.rule != e.rule {
			continue
		}
		if len(v.members) > 0 {
			if e.coversCycle(v.members) {
				return true
			}
			continue
		}
		if e.covers(v.from, v.to) && (!e.testOnly || strings.HasSuffix(v.file, "_test.go")) {
			return true
		}
	}
	return false
}

// report prints the violations of one rule and fails the test with the
// count. The excused ones are printed too, so a decision stays visible.
func report(t *testing.T, rule, title string, violations []violation) {
	t.Helper()
	var kept, excused []violation
	for _, v := range dedupe(violations) {
		if _, ok := exempt(v); ok {
			excused = append(excused, v)
		} else {
			kept = append(kept, v)
		}
	}
	for _, v := range excused {
		e, _ := exempt(v)
		t.Logf("%s excused: %s — %s", rule, v.String(), e.reason)
	}
	if len(kept) == 0 {
		t.Logf("%s %s: ok (%d checked)", rule, title, len(violations))
		return
	}
	for _, v := range kept {
		t.Logf("%s %s: %s", rule, title, v.String())
	}
	t.Errorf("%s %s: %d violation(s)", rule, title, len(kept))
}

// dedupe sorts the violations and drops the repeats: an edge that two
// files make is one edge, and the file reported is the first one. A
// production edge and a test edge stay apart — an exception may cover
// one and not the other.
func dedupe(violations []violation) []violation {
	seen := map[string]violation{}
	for _, v := range violations {
		key := v.rule + "\x00" + v.from + "\x00" + v.to + "\x00" + edgeClass(v.file)
		if len(v.members) > 0 {
			key = v.rule + "\x00" + strings.Join(v.members, "\x00")
		}
		if cur, ok := seen[key]; !ok || v.file < cur.file {
			seen[key] = v
		}
	}
	out := make([]violation, 0, len(seen))
	for _, v := range seen {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].from != out[j].from {
			return out[i].from < out[j].from
		}
		return out[i].to < out[j].to
	})
	return out
}

// edgeClass tells a shipped edge from a test edge.
func edgeClass(file string) string {
	if strings.HasSuffix(file, "_test.go") {
		return "test"
	}
	return "prod"
}

// TestArchitecture is the tree's layering guard. It measures the whole
// module and asserts the rules AGENTS.md and
// docs/guide/architecture-tiers.md write down.
func TestArchitecture(t *testing.T) {
	root := findModuleRoot(".")
	if root == "" {
		// The test runs in the package's own directory, so this only
		// happens if the tree has no go.mod at all.
		if _, file, _, ok := runtime.Caller(0); ok {
			root = findModuleRoot(filepath.Dir(file))
		}
	}
	if root == "" {
		t.Fatal("cannot find the module root: no go.mod above the working directory")
	}
	tr, err := collect(root)
	if err != nil {
		t.Fatalf("collecting the tree: %v", err)
	}

	prodPkgs := tr.prodPkgs()
	testPkgs := tr.testPkgs()
	allPkgs := tr.allPkgs()
	internalProd, _ := internalPkgs(prodPkgs)
	_, internalTest := internalPkgs(testPkgs)
	internalProdCount, internalTestCount := len(internalProd), len(internalTest)

	// R0: every package under internal/ must be classified (in a tier,
	// registered as a host, or documented in unclassifiedSubsystems).
	var unclassified []violation
	for _, p := range allPkgs {
		if !strings.HasPrefix(p.path, "internal/") {
			continue
		}
		if tierOf(p.path) > 0 || isHost(p.path) {
			continue
		}
		if _, ok := unclassifiedSubsystems[p.path]; ok {
			continue
		}
		unclassified = append(unclassified, violation{
			rule: "R0",
			from: p.path,
			to:   "unclassified",
			file: "package must belong to a tier or host",
		})
	}
	t.Run("R0 all internal packages accounted for", func(t *testing.T) {
		report(t, "R0", "all internal packages accounted for", unclassified)
	})

	// R1: no upward edge. A tier-N package imports no tier-M package
	// with M > N. The rule governs the shipped graph; the test-only
	// graph is reported underneath, because a test may reach across the
	// tiers without shipping the dependency.
	var upward []violation
	var upwardInTests []violation
	for _, p := range allPkgs {
		tier := tierOf(p.path)
		if tier == 0 {
			continue
		}
		for to, file := range p.prod {
			if other := tierOf(to); other > tier {
				upward = append(upward, violation{rule: "R1", from: p.path, to: to, file: file})
			}
		}
		for to, file := range p.test {
			if other := tierOf(to); other > tier {
				upwardInTests = append(upwardInTests, violation{rule: "R1", from: p.path, to: to, file: file})
			}
		}
	}
	t.Run("R1 no upward edge", func(t *testing.T) {
		report(t, "R1", "no upward edge", upward)
	})
	if len(upwardInTests) > 0 {
		sort.Slice(upwardInTests, func(i, j int) bool {
			return upwardInTests[i].from < upwardInTests[j].from
		})
		for _, v := range upwardInTests {
			t.Logf("R1 test-graph upward edge (not a violation): %s", v.String())
		}
	}

	// R2 and R3: no tier package imports a host. The app tier is the one
	// tier AGENTS.md lets reach the turn engines, so its engine-host
	// edges are allowed and counted; the entry surfaces are above every
	// tier, so nothing imports them.
	var hostEdges []violation
	appEngineEdges := 0
	for _, p := range allPkgs {
		tier := tierOf(p.path)
		if tier == 0 {
			continue
		}
		for _, bucket := range []map[string]string{p.prod, p.test} {
			for to, file := range bucket {
				if !isHost(to) {
					continue
				}
				switch {
				case tier == 5:
					hostEdges = append(hostEdges, violation{rule: "R3", from: p.path, to: to, file: file})
				case tier == 4 && isEngineHost(to):
					appEngineEdges++
				case tier == 4 && isEntryHost(to):
					hostEdges = append(hostEdges, violation{rule: "R2", from: p.path, to: to, file: file})
				default:
					hostEdges = append(hostEdges, violation{rule: "R2", from: p.path, to: to, file: file})
				}
			}
		}
	}
	t.Run("R2 no tier imports a host", func(t *testing.T) {
		report(t, "R2", "no tier imports a host", hostEdges)
	})
	t.Run("R3 no ui tier imports a host", func(t *testing.T) {
		var ui []violation
		for _, v := range hostEdges {
			if v.rule == "R3" {
				ui = append(ui, v)
			}
		}
		report(t, "R3", "no ui tier imports a host", ui)
	})
	t.Logf("app tier -> engine-host edges (allowed by AGENTS.md): %d", appEngineEdges)

	// R4 and R5: no import cycle, in the production graph and in the
	// test-only graph.
	prodGraph := edges(prodPkgs, false)
	testGraph := edges(testPkgs, true)
	prodCycles := findCycles(prodGraph)
	testCycles := findCycles(testGraph)
	var prodViolations, testViolations []violation
	for _, comp := range prodCycles {
		prodViolations = append(prodViolations, violation{
			rule: "R4", from: comp[0], to: comp[1], members: comp,
		})
	}
	for _, comp := range testCycles {
		testViolations = append(testViolations, violation{
			rule: "R5", from: comp[0], to: comp[1], members: comp,
		})
	}
	t.Run("R4 no production import cycle", func(t *testing.T) {
		report(t, "R4", "no production import cycle", prodViolations)
	})
	t.Run("R5 no test-only import cycle", func(t *testing.T) {
		report(t, "R5", "no test-only import cycle", testViolations)
	})

	// R6: the public packages import no internal/ package.
	var publicEdges []violation
	for _, p := range allPkgs {
		if !isPublic(p.path) {
			continue
		}
		for _, bucket := range []map[string]string{p.prod, p.test} {
			for to, file := range bucket {
				if strings.HasPrefix(to, "internal/") {
					publicEdges = append(publicEdges, violation{rule: "R6", from: p.path, to: to, file: file})
				}
			}
		}
	}
	t.Run("R6 public packages import no internal package", func(t *testing.T) {
		report(t, "R6", "public package imports an internal package", publicEdges)
	})

	// An exception that excuses nothing is a stale decision: the rule
	// holds without it, so the entry should go.
	live := append([]violation{}, upward...)
	live = append(live, hostEdges...)
	live = append(live, prodViolations...)
	live = append(live, testViolations...)
	live = append(live, publicEdges...)
	for _, e := range knownExceptions {
		if !exceptionLive(e, live) {
			t.Logf("architecture: the %s exception for %s is stale — the rule no longer catches it; delete the entry",
				e.rule, strings.Join(e.pkgs, " <-> "))
		}
	}

	// The summary line, and the numbers the script prints next to it.
	prodEdgeCount, testEdgeCount := 0, 0
	for _, es := range prodGraph {
		prodEdgeCount += len(es)
	}
	for _, es := range testGraph {
		testEdgeCount += len(es)
	}
	t.Logf("architecture: %d production packages, %d test packages, %d production edges, %d test edges — all rules hold",
		len(prodPkgs), len(testPkgs), prodEdgeCount, testEdgeCount)
	t.Logf("architecture: internal/ subtree — %d production packages, %d test packages (devutils/tiergraph.py reports the same two numbers)",
		internalProdCount, internalTestCount)
	t.Log("architecture: internal/ tier -> tier edge counts (production), the table devutils/tiergraph.py prints:")
	for _, line := range tierEdgeTable(internalProd) {
		t.Log(line)
	}
	t.Logf("architecture: %d documented exception(s) in force", len(knownExceptions))
	for _, e := range knownExceptions {
		t.Logf("architecture:   %s exempts %s — %s", e.rule, strings.Join(e.pkgs, " <-> "), e.reason)
	}
}

// tierEdgeTable renders the tier -> tier edge counts of the production
// graph, in the format devutils/tiergraph.py prints them, so the two
// measurements can be compared line for line.
func tierEdgeTable(pkgs []*pkgInfo) []string {
	counts := map[[2]int]int{}
	for _, p := range pkgs {
		from := tierOf(p.path)
		if from == 0 {
			continue
		}
		for to := range p.prod {
			other := tierOf(to)
			if other == 0 {
				continue
			}
			counts[[2]int{from, other}]++
		}
	}
	var out []string
	for _, from := range tiers {
		for _, to := range tiers {
			c := counts[[2]int{from.number, to.number}]
			if c == 0 {
				continue
			}
			same := "  same"
			if from.number != to.number {
				same = ""
			}
			out = append(out, fmt.Sprintf("  %-8s (%d) -> %-8s (%d) : %4d%s",
				from.name, from.number, to.name, to.number, c, same))
		}
	}
	return out
}
