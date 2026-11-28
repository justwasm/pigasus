// evidence_test.go — the feature-diff table's `evidence` column.
//
// What the column is for
// ----------------------
// Each row of internal/cli/testdata/feature_diff_table.json names one
// parity claim and, in `evidence`, the files (or `docs/…` paths) where the
// clone implements it. The column is what a reader consults to check a row
// rather than trust it.
//
// What went wrong
// ---------------
// Nothing ever checked it, so it rotted silently. A count at 1.5.18 found
// 121 comma-separated paths across 103 features, of which 117 did not
// exist: the paths were written before the Actor migration and the tier
// split, and pointed at `cmd/wagyu/…`, `internal/agent/…`, `internal/convstore/…`
// and similar trees that have since moved or gone. The `Evidence` field is
// parsed by Entry and read by nobody, so no test noticed.
//
// The guard
// ---------
// TestEvidencePathsExist fails on a row whose evidence names a path that is
// gone. It tolerates a row that names no file (`cycle` alone, or prose), so
// the table need not claim a path it cannot point at.
//
// The port has since been done: every one of the 96 rows was re-pointed at
// the file that now owns the behaviour, so staleAtLanding is empty and the
// guard is unratcheted — a new dead path fails immediately, with no list to
// add it to.
//
// How the rows were re-pointed
// ----------------------------
// Not by hand, and not by guessing. `git log --diff-filter=D` plus
// `git show -M` resolved 41 rows through rename chains (some three hops:
// `cmd/wagyu/tui_run.go` → `internal/tui/substates.go`). For the 25 that
// were merged into another file rather than renamed, the last revision
// before deletion was read for its distinctive identifiers and `git grep`
// found the file that holds them now. The remaining 30 never existed in the
// history at all — they were written from memory — and each was traced from
// its row's own `notes` to the code that implements the behaviour.
//
// Three rows turned out not to be path rot at all, and re-pointing them
// would have buried the real finding:
//
//   - "MCP server readiness state machine" claimed the clone had three
//     `panelRowState` states. No such identifier has ever existed. The
//     enum (`cortex_pb.McpServerStatus`) carries PENDING/READY/ERROR, but
//     nothing outside the generated file uses it and `/mcp` renders
//     configured-state only. That is a gap, and the row now says so.
//   - "Vim numeric counts" claimed the clone parses a leading digit run. It
//     deliberately does not (vim/nocount_test.go), following the shipped
//     documentation — while the 1.5.18 binary still carries count-aware
//     helpers. Docs and binary disagree; the row records the conflict.
//   - "Subagent isolation (per-repo copy)" named a `WorktreeIsolation` symbol
//     that does not exist. What does exist is per-*subagent* isolation, which
//     is not the per-repo copy the row's name describes.
//
// The first is listed in gapRows and the second in contestedRows: open
// findings and an unresolved source conflict, not debt. Both lists are
// guarded (TestGapRowsClaimNoEvidence, TestClassifiedRowsExist) so a row
// cannot quietly leave the classification.
package feat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// staleAtLanding lists the rows whose evidence was already pointing at a
// path that no longer exists when this guard landed.
//
// Every entry is debt: the row is still a real claim, but the file it named
// moved or was deleted and nobody has re-pointed it yet. Shrinking this list
// is progress; growing it needs a reason in the same change.
var staleAtLanding = map[string]bool{}

// gapRows records rows that are *not* aligned: the reference has the
// behaviour and the clone does not, or the clone's implementation was never
// traced to a file. They carry no evidence path on purpose — naming a file
// would assert an implementation that does not exist.
//
// The list exists so a reader of the table can tell "no path yet" (debt) from
// "no path needed" (prose row). TestGapRowsClaimNoEvidence keeps the two
// halves honest: a row here must have an empty evidence, and a row with an
// empty evidence must be here.
var gapRows = map[string]bool{
	"MCP server readiness state machine": true,
}

// contestedRows records rows where the reference's two sources disagree
// with each other, so "aligned" has no single meaning and the row cannot be
// scored without picking a side. They keep an evidence path — the code that
// embodies the chosen side — and their `notes` carry the conflict.
//
// Vim counts: the 1.2.9 changelog announces count multipliers and the
// 1.5.18 binary still ships `extendWordRangeForCount`, but the shipped
// vim-editor-mode.md says "Commands take no count prefix". The clone follows
// the documentation (vim/nocount_test.go), which is a defensible reading and
// not a bug — but a reader comparing against the binary will see a
// difference, so the row is flagged rather than scored.
var contestedRows = map[string]bool{
	"Vim numeric counts": true,
}

// repoRoot walks up from the test's working directory to the module root.
func repoRoot(t *testing.T) string {
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

// evidencePaths splits one row's evidence into the paths it names.
//
// A path is anything that looks like a repository path: it contains a slash
// or ends in a Go/Markdown extension. Prose ("see cycle 300") is not a path
// and is skipped rather than reported as missing.
func evidencePaths(evidence string) []string {
	var out []string
	for _, part := range strings.Split(evidence, ",") {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		if !strings.Contains(p, "/") && !strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, ".md") {
			continue
		}
		out = append(out, p)
	}
	return out
}

// loadTable reads the feature-diff JSON from the module root.
//
// It does not go through SourceData: that loader caches in a sync.Once and
// resolves the file from the *process* cwd, so a guard that used it would
// report whatever the first caller happened to load, and would silently
// pass whenever the cwd put the file out of reach. The guard wants the file
// on disk, at its known path, every run.
func loadTable(t *testing.T, root string) *Table {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, featDiffDataFile))
	if err != nil {
		t.Fatalf("read %s: %v", featDiffDataFile, err)
	}
	var tbl Table
	if err := json.Unmarshal(raw, &tbl); err != nil {
		t.Fatalf("parse %s: %v", featDiffDataFile, err)
	}
	return &tbl
}

// TestEvidencePathsExist fails when a row names a path the repository does
// not have, unless the row is already recorded as stale.
func TestEvidencePathsExist(t *testing.T) {
	root := repoRoot(t)
	data := loadTable(t, root)

	var broken []string
	for _, e := range data.Features {
		var missing []string
		for _, p := range evidencePaths(e.Evidence) {
			if _, err := os.Stat(filepath.Join(root, p)); err != nil {
				missing = append(missing, p)
			}
		}
		if len(missing) == 0 {
			continue
		}
		if staleAtLanding[e.Name] {
			continue
		}
		broken = append(broken, e.Name+" -> "+strings.Join(missing, ", "))
	}
	sort.Strings(broken)
	if len(broken) > 0 {
		t.Errorf("%d feature row(s) name an evidence path that does not exist.\n"+
			"Re-point the row at the file that now owns the behaviour, or add the row to "+
			"staleAtLanding with a reason.\n  %s", len(broken), strings.Join(broken, "\n  "))
	}
}

// TestStaleAtLandingIsNotStale pins that the ratchet only shrinks by decay:
// a row listed as stale whose evidence is now fully live is a stale entry,
// and leaving it there would hide a real failure later.
func TestStaleAtLandingIsNotStaleAnymore(t *testing.T) {
	root := repoRoot(t)
	data := loadTable(t, root)
	live := map[string]bool{}
	for _, e := range data.Features {
		paths := evidencePaths(e.Evidence)
		if len(paths) == 0 {
			continue
		}
		all := true
		for _, p := range paths {
			if _, err := os.Stat(filepath.Join(root, p)); err != nil {
				all = false
				break
			}
		}
		if all {
			live[e.Name] = true
		}
	}
	for name := range staleAtLanding {
		if live[name] {
			t.Errorf("staleAtLanding lists %q, but its evidence paths all exist again: "+
				"drop it from the list", name)
		}
	}
}

// TestGapRowsClaimNoEvidence keeps gapRows and the table's empty-evidence
// rows in lockstep, in both directions.
//
// A row in gapRows that names a file asserts an implementation while being
// listed as a gap — one of the two is a lie. A row with no evidence that is
// not in gapRows is an unnamed debt: a reader cannot tell whether the row is
// prose (no path needed) or unfinished (no path found). Requiring the list
// makes that distinction explicit and keeps the row count visible.
func TestGapRowsClaimNoEvidence(t *testing.T) {
	root := repoRoot(t)
	data := loadTable(t, root)

	// Only clone:true rows are in scope. A clone:false row is a declared
	// gap by construction — the table already says the clone lacks the
	// feature — so it must not name evidence, and listing it here would
	// duplicate the `clone` column.
	empty := map[string]bool{}
	for _, e := range data.Features {
		if e.Clone && len(evidencePaths(e.Evidence)) == 0 {
			empty[e.Name] = true
		}
	}

	for name := range gapRows {
		if !empty[name] {
			t.Errorf("gapRows lists %q, but the row names evidence: "+
				"either it is implemented (drop it from gapRows) or the "+
				"path is wrong", name)
		}
	}
	for name := range empty {
		if !gapRows[name] {
			t.Errorf("row %q names no evidence but is not in gapRows: "+
				"record it as a gap, or point it at the file that owns it", name)
		}
	}
}

// TestClassifiedRowsExist keeps gapRows and contestedRows from outliving the
// rows they describe. A name that no longer matches a row would silently
// exempt nothing, and a renamed row would drop out of the classification
// without anyone noticing.
func TestClassifiedRowsExist(t *testing.T) {
	root := repoRoot(t)
	data := loadTable(t, root)

	present := map[string]bool{}
	for _, e := range data.Features {
		present[e.Name] = true
	}
	for name := range gapRows {
		if !present[name] {
			t.Errorf("gapRows lists %q, which is not a row in the table", name)
		}
	}
	for name := range contestedRows {
		if !present[name] {
			t.Errorf("contestedRows lists %q, which is not a row in the table", name)
		}
	}
}
