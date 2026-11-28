// scenario_live_test.go — run the asserting scenarios, not just parse them.
//
// # Why this exists
//
// Every `【一手】` comment in this repo claims the same thing: this is what
// the reference binary does, observed live. The claim had no way to expire
// visibly — comments cannot fail a build, and the scenarios that could
// have caught the drift were never executed. `make align` runs
// `go test ./devutils/tuialign/...`, which parses every scenario and runs
// none of them; the only live run was one hard-coded scenario (26) inside
// verify.sh.
//
// So 47_status_mode_prefix sat at 93.3% parity, unnoticed, while the
// comments quoting it disagreed with each other about whether `auto` draws
// a prefix. Three comments cited three different "first-hand" observations
// that could not all be true.
//
// This file closes that hole: it executes every scenario that carries an
// assertion and fails the build when one fails. Scenarios without an
// assertion are skipped — a photograph nobody gates on is a report, not a
// test.
//
// # Why it skips instead of failing
//
// The live half needs the reference `agy` binary, `tmux`, and a built
// pigasus. None ships with the repository, and a checkout without them
// must still run `go test ./...`. The skip is loud and names what is
// missing, so a machine that *should* run these does not quietly pass.
//
// # Why a subprocess
//
// Scenario.Run writes the frame and the diff to stdout, which would
// interleave with the test log and make a failure unreadable. Driving the
// built binary also exercises the same entry point a person runs by hand,
// so this test cannot pass through a path the harness does not have.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// assertActions are the directives that make a scenario a test rather than
// a report. A scenario carrying none of them is skipped by the runner.
var assertActions = map[string]bool{
	"ASSERT_NOT_TEXT":      true,
	"ASSERT_PARITY":        true,
	"ASSERT_CURSOR_PARITY": true,
	"ASSERT_CURSOR_X":      true,
	"EXPECT_TEXT":          true,
	// MODE_CYCLE walks a declared lap and checks every landing against its
	// own row of that table, so it asserts rather than photographs.
	"MODE_CYCLE": true,
}

// referenceDrift names scenarios that fail because the *reference* moved,
// not because the clone regressed. Each entry states what changed, so the
// list cannot quietly collect excuses.
//
// This is the whole point of running the scenarios: a `【一手】` note is
// true of the version it was written against, and nothing in the repo
// noticed when 1.5.14 became 1.5.18. A scenario here still runs — it is
// reported as a skip with the reason — so the day somebody re-pins the
// reference, removing the entry is all it takes to re-arm the test.
var referenceDrift = map[string]string{
	// 42 asserts /swarm appears in *both* panes. It does on pigasus, but
	// agy 1.5.18 no longer offers /swarm (it was a 1.5.14 feature), so the
	// shared EXPECT_TEXT times out. Verified: the scenario passes with
	// `TARGET pigasus` and fails on `TARGET both`.
	"42_swarm_command_and_board.align": "agy 1.5.18 no longer ships /swarm (a 1.5.14 feature); the scenario passes against pigasus alone",
}

// TestScenariosPass executes every scenario that asserts something.
func TestScenariosPass(t *testing.T) {
	if testing.Short() {
		t.Skip("live TUI scenarios are skipped under -short")
	}
	_ = liveBinary(t, "AGY_REAL_BIN", filepath.Join(".local", "bin", "agy"),
		"the reference binary")
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed; the live scenarios need it")
	}
	pigasusBin := liveBinary(t, "PIGASUS_BIN", filepath.Join(os.TempDir(), "pigasus"),
		"the pigasus binary (scripts/tui-align.sh builds one)")

	// The harness drives itself as a subprocess, so the scenario runner has
	// to be a binary on disk. Building it here rather than assuming one is
	// what lets `go test` work on a clean checkout.
	tuialignBin := filepath.Join(t.TempDir(), "tuialign")
	build := exec.Command("go", "build", "-o", tuialignBin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build the harness: %v\n%s", err, out)
	}

	dir := filepath.Join("scenarios")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read scenarios: %v", err)
	}

	ran := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".align") {
			continue
		}
		name := e.Name()
		sc, err := ParseScenario(filepath.Join(dir, name))
		if err != nil {
			// A parse failure is TestEveryScenarioParses' business. Naming
			// it here as well would report one fault twice.
			continue
		}
		if !scenarioAsserts(sc) {
			continue
		}
		ran++
		drift, drifted := referenceDrift[name]
		t.Run(name, func(t *testing.T) {
			cmd := exec.Command(tuialignBin, "run", filepath.Join(dir, name))
			// The harness picks the reference binary up from the same env
			// the driver reads, so a caller pointing AGY_REAL_BIN elsewhere
			// is honoured rather than silently overridden.
			cmd.Env = append(os.Environ(), "PIGASUS_BIN="+pigasusBin)
			out, err := cmd.CombinedOutput()
			if err == nil {
				if drifted {
					// The excuse is stale: the scenario passes, so the
					// entry should go. Failing here is what keeps the
					// list from outliving the drift it describes.
					t.Fatalf("listed in referenceDrift as %q, but the scenario passes; remove the entry", drift)
				}
				return
			}
			if drifted {
				t.Skipf("known reference drift: %s", drift)
			}
			t.Fatalf("scenario %s failed: %v\n%s", name, err, tail(string(out), 40))
		})
	}
	if ran == 0 {
		t.Fatal("no scenario carries an assertion; the live suite would test nothing")
	}
}

// liveBinary resolves a binary the live suite needs, skipping the suite
// when it is absent.
func liveBinary(t *testing.T, env, rel, what string) string {
	t.Helper()
	if p := os.Getenv(env); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
		t.Skipf("%s=%s does not exist", env, p)
	}
	home, _ := os.UserHomeDir()
	p := rel
	if !filepath.IsAbs(p) {
		p = filepath.Join(home, rel)
	}
	if _, err := os.Stat(p); err != nil {
		t.Skipf("no %s at %s; set %s", what, p, env)
	}
	return p
}

// scenarioAsserts reports whether a scenario gates on anything.
func scenarioAsserts(sc *Scenario) bool {
	for _, c := range sc.Commands {
		if assertActions[c.Action] {
			return true
		}
	}
	return false
}

// tail keeps the end of a scenario's output, where the failure is.
func tail(s string, lines int) string {
	all := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(all) <= lines {
		return s
	}
	return fmt.Sprintf("... (%d lines omitted)\n%s",
		len(all)-lines, strings.Join(all[len(all)-lines:], "\n"))
}
