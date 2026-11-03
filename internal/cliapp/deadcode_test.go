// deadcode_test.go — the host carries no dead code.
//
// Cycle 381's 27th tranche removed twenty-nine functions from the host
// that nothing called any more: one-line forwarders into other
// packages, a test seam whose test was gone, and a package-level
// variable plus its setter that `cmd/wagyu` had used before that
// binary was removed.
//
// The compiler cannot catch this class of defect — Go does not
// complain about an unused function, only about an unused import or an
// unused local variable — so the host had accumulated two dozen of
// them, several of them lying in their own comments about who called
// them.
//
// This file is the guard. It parses the package with go/ast (a regex
// is not trustworthy for this — the measurement in cycle 382's plan
// §7 makes the same point) and asserts that every declaration the
// package makes is reachable from the package's own production code.
//
// Cycle 381, tranche 45: the guard stopped counting the test files as
// callers, and it stopped counting a dead caller as a caller.
//
//   - A production declaration needs a caller in a production file.
//     The test files still exercise the host, but a function only the
//     tests call is a function the *product* never runs — it reads as
//     a live seam and it is not one.
//   - Liveness is transitive. A declaration whose every production
//     caller is itself dead is dead too: `statusRow` is called only
//     by the tests, so the status-line helpers it calls are dead as
//     well.
//
// Four exclusions keep the guard honest:
//
//   - a method whose name also appears in an interface the package
//     declares may be called through that interface, so it is not
//     dead even if nothing calls it by name;
//   - `init` is called by the runtime, not by the package;
//   - a declaration that carries the "deadcode: staged" marker says
//     why it is kept, next to the declaration;
//   - an exported declaration is the package's API — it is alive
//     because a caller outside the package may hold it.
//
// A fifth is the allowlist below: a declaration that is deliberately
// kept although the product does not reach it yet — a recovered
// reference behaviour, a seam a later cycle wires up — carries its
// reason in the map, so the decision is recorded in one place rather
// than hidden in a comment. An allowlist entry that no longer matches
// a dead declaration is an error: the decision it recorded is over.
//
// The guard covers the host's own *unexported* surface. An exported
// function may be called from another package, so the host-local
// guard says nothing about it; the tree-wide dead-export sweep
// (cycle 381) is what resolves those through the import map. The
// public top-level packages are out of scope for both — an export
// nobody calls today is still a third party's API.
package cliapp

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// deadCodeAllowlist names the declarations the product does not
// reach, and the reason each one is kept.
//
// Each entry is a decision, not a hiding place: a name that is here
// because a later cycle wires it up says so; a name that is here
// because the reference has the behaviour says so too.
var deadCodeAllowlist = map[string]string{
	// renderer.go — the renderer seam the print-mode bypass is
	// measured against. The reference routes the TUI's dirty-rect
	// optimisation through it; the clone pins the contract in the
	// test and has not wired the runtime path yet.
	"applyRenderer":         "renderer seam: the print-mode dirty-rect bypass the reference has; not wired into the runtime path yet",
	"shouldConsultRenderer": "renderer seam: the print-mode bypass gate; not wired into the runtime path yet",

	// renderer.go — the escape-sequence optimisation switch the
	// reference reads from the environment. The clone reads it
	// through tuilib; the host's own name is the seam.
	"envDisableEscapeSequenceOptimizations": "renderer seam: the escape-sequence optimisation switch; the host names it, the runtime reads it through tuilib",

	// conversation_seed.go — the conversation seed's own knobs and
	// the mention hook, kept for the next cycle that seeds a
	// conversation from the store.
	"handleAtConversationMention": "conversation seed: the '@conversation:' mention hook; the seed path is not wired yet",

	// slash_statusline_external.go / tui_turn_blocks.go — the
	// external status-line's row rendering. The row is reachable
	// only through the status row, which the product does not reach
	// either.
	"statusRow":              "status row: the external status-line's row; the product does not render the custom row yet",
	"externalRenderedForRow": "status row: the external status-line's rendered row; reachable only through statusRow",
	"externalRenderOK":       "status row: the external status-line's readiness gate; reachable only through statusRow",

	// tui_viewport_scroll.go — the viewport's own constants and its
	// span type, reached through the ui tier's own package.
	"selHighlightOpen":     "viewport seam: the selection's opening span; the host names it, the ui tier renders it",

	// tui_run.go — the /help window's historical fixed size. The
	// live value is computed per terminal height; the constant is
	// kept so the cycle-132 value has a named home, and its own
	// declaration says "do not add new uses".
	"helpCommandsPageSize": "/help window: the historical fixed size; the live value is computed per terminal height",
}

// declKind is what a declaration makes.
type declKind int

const (
	kindFunc declKind = iota
	kindValue
	kindType
)

// declSite is one declaration found in the package.
type declSite struct {
	name string
	kind declKind
	// exported is true when the name is outside the package's own
	// private surface.
	exported bool
	hasRecv  bool
	// isTest is true when the declaration lives in a `_test.go`
	// file. A test helper is called by the test files, so it is
	// exempt from the production-caller rule.
	isTest bool
	// staged is true when the declaration carries the
	// "deadcode: staged" marker: a seam that is deliberately kept
	// although nothing calls it yet, with the reason written next to
	// it.
	staged bool
	// pos is the *name*'s position, not the declaration's: the
	// declaration's own name is one of the identifier uses, and the
	// check below skips exactly this position.
	pos token.Position
	// alive is the transitive liveness verdict, filled in by
	// computeLiveness.
	alive bool
}

// useSite is one identifier use in the package.
type useSite struct {
	// caller is the index of the declaration the use sits in, or -1
	// when the use sits at package scope (a var initialiser, a
	// const expression, a type alias) — a package-scope use runs
	// when the package loads, so it is a live caller.
	caller int
	pos    token.Position
}

// pkgFile is one parsed file of the package.
type pkgFile struct {
	name   string
	isTest bool
	file   *ast.File
}

// packageDecls parses every Go file in the package directory and
// returns the declarations it finds, the identifiers used anywhere in
// the package (with the position of each use and the declaration it
// sits in), and the set of method names that appear in an interface
// declaration.
func packageDecls(t *testing.T) ([]declSite, map[string][]useSite, map[string]bool) {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	fset := token.NewFileSet()
	var files []pkgFile
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		path := filepath.Join(".", name)
		// parser.ParseComments: the "deadcode: staged" marker lives
		// in the declaration's doc comment, and the parser only
		// attaches doc comments when it is asked to keep them.
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		files = append(files, pkgFile{name: name, isTest: isTestFile(name), file: f})
	}

	// The declarations.
	var decls []declSite
	// The function bodies, so a use can be attributed to the
	// declaration that contains it.
	var bodies []funcBody
	interfaceMethods := map[string]bool{}

	for _, pf := range files {
		for _, d := range pf.file.Decls {
			switch dd := d.(type) {
			case *ast.FuncDecl:
				idx := len(decls)
				decls = append(decls, declSite{
					name:     dd.Name.Name,
					kind:     kindFunc,
					exported: isExported(dd.Name.Name),
					hasRecv:  dd.Recv != nil && len(dd.Recv.List) > 0,
					isTest:   pf.isTest,
					staged:   isStaged(dd.Doc),
					pos:      fset.Position(dd.Name.Pos()),
				})
				if dd.Body != nil {
					bodies = append(bodies, funcBody{
						start: dd.Body.Lbrace,
						end:   dd.Body.Rbrace,
						decl:  idx,
					})
				}
			case *ast.GenDecl:
				for _, s := range dd.Specs {
					switch sp := s.(type) {
					case *ast.ValueSpec:
						for _, id := range sp.Names {
							decls = append(decls, declSite{
								name:     id.Name,
								kind:     kindValue,
								exported: isExported(id.Name),
								isTest:   pf.isTest,
								pos:      fset.Position(id.Pos()),
							})
						}
					case *ast.TypeSpec:
						decls = append(decls, declSite{
							name:     sp.Name.Name,
							kind:     kindType,
							exported: isExported(sp.Name.Name),
							isTest:   pf.isTest,
							pos:      fset.Position(sp.Name.Pos()),
						})
					}
				}
			}
		}

		// The interface method names.
		ast.Inspect(pf.file, func(n ast.Node) bool {
			it, ok := n.(*ast.InterfaceType)
			if !ok {
				return true
			}
			for _, m := range it.Methods.List {
				for _, id := range m.Names {
					interfaceMethods[id.Name] = true
				}
			}
			return true
		})
	}

	// The identifier uses. A declaration's own name is an *ast.Ident
	// too, so the uses are counted positionally: the dead-code
	// check below compares the use positions against the
	// declaration's own position.
	uses := map[string][]useSite{}
	for _, pf := range files {
		if pf.isTest {
			// The test files are not callers of the product.
			continue
		}
		// A struct field's name and a composite literal's key are
		// not uses of a package-level name.
		fieldNames := map[token.Pos]bool{}
		ast.Inspect(pf.file, func(n ast.Node) bool {
			fld, ok := n.(*ast.Field)
			if !ok {
				return true
			}
			for _, id := range fld.Names {
				fieldNames[id.Pos()] = true
			}
			return true
		})
		ast.Inspect(pf.file, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			if fieldNames[id.Pos()] {
				return true
			}
			uses[id.Name] = append(uses[id.Name], useSite{
				caller: enclosingDecl(bodies, id.Pos()),
				pos:    fset.Position(id.Pos()),
			})
			return true
		})
	}

	return decls, uses, interfaceMethods
}

// funcBody is the body span of one function declaration.
type funcBody struct {
	start, end token.Pos
	decl       int
}

// enclosingDecl returns the index of the declaration whose body
// contains pos, or -1 when the position is at package scope.
func enclosingDecl(bodies []funcBody, pos token.Pos) int {
	found := -1
	for _, b := range bodies {
		if pos < b.start || pos > b.end {
			continue
		}
		if found == -1 || (b.end-b.start) < (bodies[found].end-bodies[found].start) {
			found = b.decl
		}
	}
	return found
}

// computeLiveness fills in each declaration's transitive liveness
// verdict.
//
// A declaration is alive when some *live* declaration calls it, or
// when a package-scope expression uses it. The exported declarations,
// the interface-implementing methods and the staged declarations are
// the roots: they are alive because a caller outside the package may
// hold them. The allowlisted declarations are roots too — the code
// path they name exists, so what they call exists as well.
//
// The fixpoint is the greatest one: everything starts alive and the
// declarations whose every caller is dead are removed, over and over,
// until nothing changes.
//
// The extraRoots argument adds the allowlisted names to the root set.
// It is empty for the raw pass (which asks what is dead on its own
// merits) and carries the allowlist for the final pass.
func computeLiveness(decls []declSite, uses map[string][]useSite, interfaceMethods map[string]bool, extraRoots map[string]bool) {
	for i := range decls {
		decls[i].alive = true
	}

	for changed := true; changed; {
		changed = false
		for i := range decls {
			d := &decls[i]
			if !d.alive || d.isTest {
				continue
			}
			if d.exported {
				// A root.
				continue
			}
			if d.kind == kindFunc && d.hasRecv && interfaceMethods[d.name] {
				// It may be called through the interface.
				continue
			}
			if d.staged {
				// The declaration says why it is kept.
				continue
			}
			if extraRoots[d.name] {
				// The allowlist says why it is kept.
				continue
			}
			live := false
			for _, u := range uses[d.name] {
				if u.pos.Filename == d.pos.Filename && u.pos.Line == d.pos.Line && u.pos.Column == d.pos.Column {
					// The declaration's own name.
					continue
				}
				if u.caller == -1 {
					// A package-scope use runs when the package
					// loads.
					live = true
					break
				}
				if u.caller == i {
					// A recursive call is not a caller.
					continue
				}
				if decls[u.caller].alive {
					live = true
					break
				}
			}
			if !live {
				d.alive = false
				changed = true
			}
		}
	}
}

// allowlistRoots returns the allowlist's names as a root set.
func allowlistRoots() map[string]bool {
	roots := make(map[string]bool, len(deadCodeAllowlist))
	for name := range deadCodeAllowlist {
		roots[name] = true
	}
	return roots
}

// TestNoDeadCodeInHost asserts that every declaration the host makes
// is reachable from the package's own production code.
//
// A declaration with no live caller is dead weight: it reads as a
// live seam and it is not one. The reference's own bytes are
// unaffected either way — this is a structural guard, not a
// behavioural one.
func TestNoDeadCodeInHost(t *testing.T) {
	decls, uses, interfaceMethods := packageDecls(t)
	computeLiveness(decls, uses, interfaceMethods, allowlistRoots())

	var dead []string
	for _, d := range decls {
		if d.name == "init" || d.name == "main" {
			continue
		}
		if d.exported {
			// An exported declaration may be called from another
			// package; the guard covers the host's own private
			// surface, which is where the dead code was. The
			// tree-wide sweep covers the exported half.
			continue
		}
		if d.isTest {
			// A test helper is called by the test files, which is
			// what it is for.
			continue
		}
		if d.kind == kindFunc && d.hasRecv && interfaceMethods[d.name] {
			// It may be called through the interface.
			continue
		}
		if d.staged {
			// The declaration says why it is kept.
			continue
		}
		if d.alive {
			continue
		}
		if _, ok := deadCodeAllowlist[d.name]; ok {
			// The map says why it is kept.
			continue
		}
		dead = append(dead, d.name+" ("+d.pos.Filename+":"+itoa(d.pos.Line)+")")
	}

	sort.Strings(dead)
	for _, d := range dead {
		t.Errorf("dead declaration (no live caller in a production file): %s", d)
	}
	if len(dead) > 0 {
		t.Errorf("%d dead declaration(s) in internal/cliapp — delete them, or give them a caller", len(dead))
	}
}

// TestDeadCodeAllowlistIsMinimal asserts that every allowlist entry
// still matches a dead declaration.
//
// An entry that no longer matches is a decision that has been
// overtaken: the declaration found its caller, or it was deleted. The
// entry would then record a fact that is no longer true.
func TestDeadCodeAllowlistIsMinimal(t *testing.T) {
	decls, uses, interfaceMethods := packageDecls(t)
	// The raw pass: the allowlist is not a root set here, so an
	// entry that names a live declaration is stale.
	computeLiveness(decls, uses, interfaceMethods, nil)

	needed := map[string]bool{}
	for _, d := range decls {
		if d.name == "init" || d.name == "main" || d.exported || d.isTest || d.staged {
			continue
		}
		if d.kind == kindFunc && d.hasRecv && interfaceMethods[d.name] {
			continue
		}
		if d.alive {
			continue
		}
		needed[d.name] = true
	}

	var stale []string
	for name := range deadCodeAllowlist {
		if !needed[name] {
			stale = append(stale, name)
		}
	}
	sort.Strings(stale)
	for _, name := range stale {
		t.Errorf("stale allowlist entry (the declaration is no longer dead): %s", name)
	}
	if len(stale) > 0 {
		t.Errorf("%d stale allowlist entr(y/ies) in internal/cliapp — delete them", len(stale))
	}
}

// isTestFile reports whether a file name belongs to the test files.
func isTestFile(name string) bool {
	return strings.HasSuffix(name, "_test.go")
}

// isStaged reports whether a declaration carries the "deadcode:
// staged" marker. The marker is an escape hatch for a seam that is
// deliberately kept although nothing calls it yet — the reason has to
// be written next to it, so the marker is not a way to hide dead
// code.
func isStaged(doc *ast.CommentGroup) bool {
	if doc == nil {
		return false
	}
	for _, c := range doc.List {
		if strings.Contains(c.Text, "deadcode: staged") {
			return true
		}
	}
	return false
}

// isExported reports whether a name is outside the package's own
// private surface.
func isExported(name string) bool {
	if name == "" {
		return false
	}
	c := name[0]
	return c >= 'A' && c <= 'Z'
}

// itoa formats a line number without pulling strconv into the test's
// import block for one call.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
