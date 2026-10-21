package main

// tui_tools_step_title_test.go — pins the tool-step failure surface.
//
// AGY 1.2.10 changed the step title of a command that exits non-zero from
// `Errored` to `Failed`. Cycle 285 re-captured the whole step display against
// agy 1.4.3 and the surface moved again: the failure word is now lowercase
// and sits *after* the args, only in the collapsed form:
//
//	● Ran (echo boom; exit 2) failed (ctrl+o to expand)   ← collapsed, red ●
//	● Bash(false)                                          ← expanded, no word
//	  ⎿  Command exited with code 1. … (ctrl+o to collapse)
//
// This file regression-locks:
//
//  1. TestStepTitle_NonZeroExit_HasFailedLabel — a non-zero exit marks the
//     step failed (red bullet, " failed" suffix) while the expanded call
//     title stays clean.
//  2. TestStepTitle_ZeroExit_NoFailedLabel — the happy path keeps the teal
//     bullet and never grows a failure word.
//  3. TestStepTitle_GroupLabel_NoErroredConstabulary — the batch header is
//     free of both the legacy `Errored` wording and the failure suffix.
//  4. TestStepTitle_NoErroredStringInPackageGoSources — walks every
//     `*.go` file under cmd/wagyu/ (excluding testdata/, which holds
//     byte-exact AGY reference text we do not own) and asserts the literal
//     word `Errored` no longer appears.

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runCommandTitleArgs is the JSON arguments a tool call carrying a
// `false` command exit.
const runCommandTitleArgs = `{"CommandLine":"false"}`

// runCommandOKArgs is the JSON arguments a successful command (`true`,
// exit 0).
const runCommandOKArgs = `{"CommandLine":"true"}`

// runCommandResult builds the run_command result body for one exit code.
func runCommandResult(code string, output string) string {
	return "Created At: 2026-10-20T00:00:00+08:00\nCompleted At: 2026-10-20T00:00:01+08:00\n\n" +
		"The command exited with code " + code + ".\nOutput:\n" + output + "\n"
}

// TestStepTitle_NonZeroExit_HasFailedLabel pins the 1.4.3 failure surface:
// the collapsed summary carries a lowercase " failed" suffix and the red
// bullet, while the expanded call title stays `Bash(false)` with the exit
// code moved into the detail body.
func TestStepTitle_NonZeroExit_HasFailedLabel(t *testing.T) {
	step := newToolStep("run_command", json.RawMessage(runCommandTitleArgs),
		runCommandResult("1", ""), false)
	if !step.err {
		t.Fatalf("non-zero exit did not mark the step failed")
	}

	// (a) collapsed: red bullet + " failed" after the args.
	collapsed := strings.Join(toolStepLines(step, false, 0), "\n")
	if !strings.HasPrefix(collapsed, stepBulletErrOpen) {
		t.Fatalf("expected red error bullet, got %q", collapsed)
	}
	if !strings.Contains(collapsed, "Ran") || !strings.Contains(collapsed, "(false)") {
		t.Fatalf("collapsed summary missing args: %q", collapsed)
	}
	if !strings.Contains(collapsed, "failed") || strings.Contains(collapsed, "(Failed)") {
		t.Fatalf("collapsed failure word = %q; want lowercase \"failed\"", collapsed)
	}

	// (b) expanded: clean title, exit code in the detail body.
	expanded := strings.Join(toolStepLines(step, true, 0), "\n")
	if !strings.Contains(expanded, "Bash") || !strings.Contains(expanded, "(false)") {
		t.Fatalf("expanded title = %q; want Bash(false)", expanded)
	}
	if strings.Contains(expanded, "failed") || strings.Contains(expanded, "(Failed)") {
		t.Fatalf("expanded title should not carry a failure word:\n%s", expanded)
	}
	if !strings.Contains(expanded, "Command exited with code 1.") {
		t.Fatalf("expanded detail missing exit line:\n%s", expanded)
	}
	if !strings.Contains(expanded, "No output from the command.") {
		t.Fatalf("expanded detail missing empty-output line:\n%s", expanded)
	}
}

// TestStepTitle_ZeroExit_NoFailedLabel pins the happy path: teal bullet,
// no failure word, and the output body in the reference's "Command output:"
// shape.
func TestStepTitle_ZeroExit_NoFailedLabel(t *testing.T) {
	step := newToolStep("run_command", json.RawMessage(runCommandOKArgs),
		runCommandResult("0", "one"), false)
	if step.err {
		t.Fatalf("zero exit marked the step failed")
	}

	collapsed := strings.Join(toolStepLines(step, false, 0), "\n")
	if !strings.HasPrefix(collapsed, stepBulletOpen) {
		t.Fatalf("zero-exit should render with the teal success bullet, got %q", collapsed)
	}
	if strings.Contains(collapsed, "failed") {
		t.Fatalf("zero-exit leaked a failure word: %q", collapsed)
	}

	expanded := strings.Join(toolStepLines(step, true, 0), "\n")
	if !strings.Contains(expanded, "Command exited with code 0.") ||
		!strings.Contains(expanded, "Command output:") ||
		!strings.Contains(expanded, "one ") {
		t.Fatalf("expanded detail = %q; want exit line + Command output body", expanded)
	}
	// The raw result's timestamps are model-facing only.
	if strings.Contains(expanded, "Created At") {
		t.Fatalf("expanded detail leaked the raw result timestamps:\n%s", expanded)
	}
}

// TestStepTitle_GroupLabel_NoErroredConstabulary pins the batch header for
// two parallel commands: "Ran 2 commands (a · b)", free of the legacy
// `Errored` wording and of any failure suffix.
func TestStepTitle_GroupLabel_NoErroredConstabulary(t *testing.T) {
	verb, count, detail := groupSummary([]toolStep{
		{kind: stepKindTool, name: "run_command", title: "Bash(true)"},
		{kind: stepKindTool, name: "run_command", title: "Bash(false)", err: true},
	})
	if verb != "Ran" || count != "2 commands" || detail != "(true · false)" {
		t.Fatalf("groupSummary = (%q, %q, %q); want (Ran, 2 commands, (true · false))", verb, count, detail)
	}
	joined := verb + " " + count + " " + detail
	if strings.Contains(joined, "Errored") || strings.Contains(joined, "Failed") || strings.Contains(joined, "failed") {
		t.Fatalf("group header carries failure wording: %q", joined)
	}
}

// TestStepTitle_NoErroredStringInPackageGoSources walks every .go
// file reachable from cmd/wagyu/ and asserts the literal string
// "Errored" no longer appears. testdata/ is excluded because the
// print_changelog.md it contains is the byte-exact copy of
// `agy -p /changelog` (used by TestPrintSlash_ChangelogExactSize and
// the parity harness) and references the legacy title on purpose.
//
// The test is package-scoped (cmd/wagyu) on purpose: the same
// constraint in other packages is out of scope for the AGY 1.2.10
// changelog gap, which is specifically about the tool-step surface.
func TestStepTitle_NoErroredStringInPackageGoSources(t *testing.T) {
	pkgDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}

	// selfFile is the file containing THIS test. The "Errored"
	// wording appears in this file on purpose — every comment
	// above and every failure-message format string names the
	// legacy label that is being checked for. Excluding the test
	// from itself is the only way to run the scan without the
	// scan trapping its own source.
	selfFile := "tui_tools_step_title_test.go"

	var hits []string
	walkErr := filepath.WalkDir(pkgDir, func(path string, d fs.DirEntry, err error) error {
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
		// Skip this file (see selfFile note above).
		if d.Name() == selfFile {
			return nil
		}

		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if perr != nil {
			return perr
		}

		rel, _ := filepath.Rel(pkgDir, path)

		// Walk comments — both // ... and /* ... */ — and flag any
		// that contain the legacy wording.
		for _, cg := range f.Comments {
			for _, c := range cg.List {
				if strings.Contains(c.Text, "Errored") {
					hits = append(hits, rel+":"+c.Text)
				}
			}
		}

		// Walk expression statements / basic literals and flag any
		// string literal that contains the legacy wording. Identifiers
		// are excluded — `Errored` is a word, not a token, and a
		// future Go identifier like `erroredFlag` is unrelated.
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			if strings.Contains(lit.Value, "Errored") {
				hits = append(hits, rel+":"+lit.Value)
			}
			return true
		})
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk: %v", walkErr)
	}

	if len(hits) > 0 {
		t.Fatalf("found legacy wording \"Errored\" in cmd/wagyu Go sources:\n  %s\nAGY 1.2.10 renamed that label to \"Failed\". testdata fixtures are excluded because they hold byte-exact AGY /changelog output we do not own.",
			strings.Join(hits, "\n  "))
	}
}
