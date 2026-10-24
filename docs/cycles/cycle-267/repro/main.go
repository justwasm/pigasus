// Command wg01-run drives docs/cycles/cycle-267/repro/wg01_pty_check.py
// against a built wagyu binary so the cycle 267 reproduction can be
// run end-to-end with a single `go run` invocation.
//
// The PTY harness (wg01_pty_check.py) is the primary verification —
// the bug is purely visual (UV's diff-leaves-stale-pixels pathology
// at the streaming → done boundary), so headless in-process Update
// calls can't reproduce it. The PTY harness opens a real pty.fork(),
// drives the binary, captures the raw escape stream, and asserts the
// five post-ClearScreen signals:
//
//   1. \x1b[H\x1b[2J after state=done ← tea.ClearScreen trigger
//   2. no raw-chunk "mock reply f " leaked past state=done
//   3. response fragments "mock reply from model=" + "Gemini 3.8 Flash (Medium)" present
//   4. no spinner "Generating" leaked past state=done
//   5. Card 2 "Daily quota resets" visible (Card 1 transitioned)
//
// Usage (run from the worktree root):
//
//   go run ./docs/cycles/cycle-267/repro -mode repro1 -bin /tmp/wagyu-c267
//   go run ./docs/cycles/cycle-267/repro -mode repro2 -bin /tmp/wagyu-final
//   go run ./docs/cycles/cycle-267/repro -mode repro3
//
// Three modes:
//
//   repro1 — post-fix binary. Expect 5/5 PASS, exit 0.
//   repro2 — pre-fix baseline (cycle 266). Expect 3/5 PASS with
//            card2=False and clear-sig=False (this is the regression
//            the fix closes; running repro2 against the post-fix
//            binary should be a regression failure).
//   repro3 — run only the headless unit tests (cmd/wagyu/cycle267_streaming_clear_test.go),
//            no PTY. Useful in CI environments that don't have a TTY.
//
// Note on cwd: `go run ./docs/cycles/cycle-267/repro` builds in a
// temporary directory and the harnessPath() resolution depends on
// `go list -f '{{.Dir}}' .` working — which requires being inside
// the module root. If `go list` fails (e.g. `go run` was invoked
// from a non-module dir), pass -harness to point at the .py file
// directly.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
	mode := flag.String("mode", "repro1", `repro1 (post-fix binary, expect 5/5) | repro2 (pre-fix baseline, expect 3/5) | repro3 (headless unit tests)`)
	bin := flag.String("bin", "/tmp/wagyu-c267", "path to the wagyu binary under test (repro1 / repro2)")
	harness := flag.String("harness", "", "explicit path to wg01_pty_check.py (defaults to ./wg01_pty_check.py next to this main.go)")
	flag.Parse()

	switch *mode {
	case "repro1":
		runPTYCheck(*bin, "post-fix binary", "Expect 5/5 PASS, exit 0", *harness)
	case "repro2":
		runPTYCheck(*bin, "pre-fix baseline (cycle 266)", "Expect 3/5 PASS with card2=False and clear-sig=False", *harness)
	case "repro3":
		runUnitTests()
	default:
		fmt.Fprintf(os.Stderr, "unknown mode %q\n", *mode)
		os.Exit(2)
	}
}

// runPTYCheck resolves docs/cycles/cycle-267/repro/wg01_pty_check.py
// (or the explicit -harness path) and runs it against the binary.
// The harness is the authoritative end-to-end reproducer for the
// cycle 267 fix.
func runPTYCheck(bin, label, expected, harnessOverride string) {
	if _, err := os.Stat(bin); err != nil {
		fmt.Fprintf(os.Stderr, "[%s] binary %q missing: %v\n", label, bin, err)
		fmt.Fprintf(os.Stderr, "[%s] build it first: go build -o %s ./cmd/wagyu\n", label, bin)
		os.Exit(2)
	}

	var harness string
	if harnessOverride != "" {
		harness = harnessOverride
	} else {
		harness = harnessPath()
	}
	if _, err := os.Stat(harness); err != nil {
		fmt.Fprintf(os.Stderr, "[%s] harness %q missing: %v\n", label, harness, err)
		fmt.Fprintf(os.Stderr, "[%s] pass -harness /abs/path/to/wg01_pty_check.py to override\n", label)
		os.Exit(2)
	}

	fmt.Printf("[%s] running %s %s\n", label, harness, bin)
	fmt.Printf("[%s] %s\n", label, expected)
	cmd := exec.Command("python3", harness, bin)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			// Exit code 1 from the harness = "test failed" (e.g. a
			// pre-fix binary on repro1). Print and propagate so
			// callers can distinguish "ran but failed" from
			// "didn't run".
			fmt.Printf("[%s] harness exited %d (this may be expected for repro2)\n", label, exitErr.ExitCode())
			os.Exit(exitErr.ExitCode())
		}
		fmt.Fprintf(os.Stderr, "[%s] harness failed: %v\n", label, err)
		os.Exit(2)
	}
}

// runUnitTests executes the cycle 267 headless unit-test suite —
// no PTY, no bubbletea runtime. Useful in CI / sandbox where a TTY
// is not available. We cd to the worktree root (resolved via
// go env GOMOD) before invoking `go test` so the relative
// package path `./cmd/wagyu/` resolves correctly regardless of
// the caller's cwd.
func runUnitTests() {
	fmt.Println("[repro3] running cmd/wagyu/cycle267_streaming_clear_test.go (headless)")
	gomod, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[repro3] go env GOMOD: %v\n", err)
		os.Exit(2)
	}
	gomod = gomod[:len(gomod)-1]
	root := filepath.Dir(string(gomod))
	cmd := exec.Command("go", "test", "-vet=off", "-count=1", "-v",
		"-run", "TestTurnDoneMsg_StreamingClear|TestBackendDoneMsg_StreamingClear|TestTurnDoneMsg_QueueFlushKeepsClearScreen|TestTurnDoneMsg_StreamingClearContractIsStableAcrossResponseShapes",
		"./cmd/wagyu/")
	cmd.Dir = root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		fmt.Fprintf(os.Stderr, "[repro3] unit tests failed: %v\n", err)
		os.Exit(2)
	}
}

// harnessPath locates the wg01_pty_check.py sibling to this main.go
// source file via `go list -f '{{.Dir}}' .`. Note that `go run`
// copies the source to a temp dir before compilation, so `go list`
// inside the binary may return the temp dir instead of the
// worktree path — that path won't have the .py file alongside.
// Users in that situation should pass -harness explicitly. This
// function is best-effort; the explicit flag is the
// authoritative override.
func harnessPath() string {
	out, err := exec.Command("go", "list", "-f", "{{.Dir}}", ".").Output()
	if err != nil {
		return filepath.Join(".", "wg01_pty_check.py")
	}
	return filepath.Join(string(out[:len(out)-1]), "wg01_pty_check.py")
}