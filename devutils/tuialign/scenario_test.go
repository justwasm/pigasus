package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEveryScenarioParses makes the scenario corpus a checked-in contract:
// a `.align` file that no longer parses — a renamed action, an unclosed
// quote, a TARGET nobody handles — fails `go test` instead of failing the
// next person who runs the harness by hand.
//
// It also pins the shape every scenario needs to be runnable at all: a name,
// a boot, at least one mock turn, and a cleanup so a run cannot leave its
// tmux session behind.
func TestEveryScenarioParses(t *testing.T) {
	dir := filepath.Join("scenarios")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read scenarios: %v", err)
	}
	var seen int
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".align") {
			continue
		}
		seen++
		t.Run(e.Name(), func(t *testing.T) {
			sc, err := ParseScenario(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if sc.Name == "" {
				t.Error("scenario has no NAME")
			}
			switch sc.Target {
			case TargetAgy, TargetPigasus, TargetBoth:
			default:
				t.Errorf("unknown TARGET %q", sc.Target)
			}
			// Not every scenario needs a mock turn: the panel-only ones
			// (changelog, status, permissions) drive the UI without a
			// model. A boot and a cleanup every one of them needs — a
			// run that never cleans up leaves its tmux session behind.
			actions := map[string]bool{}
			for _, c := range sc.Commands {
				actions[c.Action] = true
			}
			if !actions["BOOT"] && !actions["BOOT_BOTH"] && !actions["LAUNCH"] {
				t.Error("scenario never boots a binary")
			}
			if !actions["CLEANUP"] {
				t.Error("scenario has no CLEANUP")
			}
		})
	}
	if seen == 0 {
		t.Fatal("no *.align scenarios found")
	}
}

// TestToolOutputScenarioGatesTheRegression pins that the preformatted-output
// scenario still carries both halves of its gate. A scenario that loses its
// EXPECT_TEXT/ASSERT_NOT_TEXT pair still "passes" — it just stops testing
// anything.
func TestToolOutputScenarioGatesTheRegression(t *testing.T) {
	sc, err := ParseScenario(filepath.Join("scenarios", "26_tool_output_preformatted.align"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var expect, assertNot bool
	for _, c := range sc.Commands {
		switch c.Action {
		case "EXPECT_TEXT":
			if len(c.Args) > 0 && strings.Contains(c.Args[0], "`json:") {
				expect = true
			}
		case "ASSERT_NOT_TEXT":
			if len(c.Args) > 0 && c.Args[0] == "int json:" {
				assertNot = true
			}
		}
	}
	if !expect {
		t.Error("scenario no longer asserts the backticked struct tag on screen")
	}
	if !assertNot {
		t.Error("scenario no longer forbids the markdown code-span form")
	}
}
