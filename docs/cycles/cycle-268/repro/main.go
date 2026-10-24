// Command wg01-run drives docs/cycles/cycle-268/repro/wg01_pty_check.py
// against a built wagyu binary so the cycle 268 reproduction can be
// run end-to-end with a single `go run` invocation.
//
// The PTY harness (wg01_pty_check.py) is the primary end-to-end
// verification. The headless regression suite (cmd/wagyu/... and
// internal/mdrender/cycle268_width_test.go) pins the in-process
// behaviour on every axis the fix touches; the PTY harness
// confirms the wiring reaches the rendered bytes on the wire.
//
// Usage (run from the worktree root):
//
//	go run ./docs/cycles/cycle-268/repro -mode repro1 -bin /tmp/wagyu-c268
//	go run ./docs/cycles/cycle-268/repro -mode repro2 -bin /tmp/wagyu-final
//	go run ./docs/cycles/cycle-268/repro -mode repro3
//
// Three modes:
//
//	repro1 — post-fix binary (cycle 268). Expect exit 0; the wide
//	         PTY (250 cols) renders visible lines longer than the
//	         narrow PTY (80 cols) by more than 20 cells.
//	repro2 — pre-fix baseline (cycle 267). Expect exit 1; both PTYs
//	         cap at ~80 cells so the diff is < 20 and the harness
//	         reports the regression.
//	repro3 — headless unit tests only
//	         (internal/mdrender/cycle268_width_test.go + the
//	         updated mustANSI helper in safe_boundary_test.go). No
//	         PTY required; useful in CI environments without a TTY.
//
// Note on cwd: `go run ./docs/cycles/cycle-268/repro` builds in a
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
	mode := flag.String("mode", "repro1", `repro1 (post-fix binary, expect exit 0 — width respected) | repro2 (pre-fix baseline, expect exit 1) | repro3 (headless unit tests)`)
	bin := flag.String("bin", "/tmp/wagyu-c268", "path to the wagyu binary under test (repro1 / repro2)")
	harness := flag.String("harness", "", "explicit path to wg01_pty_check.py (defaults to ./wg01_pty_check.py next to this main.go)")
	flag.Parse()

	switch *mode {
	case "repro1":
		runPTYCheck(*bin, "post-fix binary", "Expect exit 0 — wide PTY renders visibly wider than narrow PTY", *harness)
	case "repro2":
		runPTYCheck(*bin, "pre-fix baseline (cycle 267)", "Expect exit 1 — both PTYs cap at ~80 cols", *harness)
	case "repro3":
		runUnitTests()
	default:
		fmt.Fprintf(os.Stderr, "unknown mode %q\n", *mode)
		os.Exit(2)
	}
}

// runPTYCheck resolves docs/cycles/cycle-268/repro/wg01_pty_check.py
// (or the explicit -harness path) and runs it against the binary.
// The harness is the authoritative end-to-end reproducer for the
// cycle 268 fix.
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

// runUnitTests executes the cycle 268 headless regression suite —
// no PTY, no bubbletea runtime. We cd to the worktree root
// (resolved via `go env GOMOD`) before invoking `go test` so the
// relative package paths `./cmd/wagyu/` and `./internal/mdrender/`
// resolve correctly regardless of the caller's cwd.
func runUnitTests() {
	fmt.Println("[repro3] running cycle 268 headless regression suite")
	gomod, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[repro3] go env GOMOD: %v\n", err)
		os.Exit(2)
	}
	gomod = gomod[:len(gomod)-1]
	root := filepath.Dir(string(gomod))

	// Cycle 268's own test file plus the safe_boundary regression
	// (the mustANSI helper in safe_boundary_test.go was updated
	// to use currentWidth() — its assertions still pass, but
	// running it here makes the suite complete).
	cycleTests := exec.Command("go", "test", "-vet=off", "-count=1", "-v",
		"-run", "TestGlamour_WidthPin_|TestGlamour_CurrentWidth_|TestGlamour_SetCurrentWidth_|TestGlamour_PrefixCache_|TestGlamour_AllVariantsHonorWidth|TestGlamour_StyleBytesSurviveWidth|TestGlamour_NoInfiniteRecursionOnZeroWidth",
		"./internal/mdrender/")
	cycleTests.Dir = root
	cycleTests.Stdout = os.Stdout
	cycleTests.Stderr = os.Stderr
	if err := cycleTests.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		fmt.Fprintf(os.Stderr, "[repro3] cycle 268 unit tests failed: %v\n", err)
		os.Exit(2)
	}

	// The full test suite to confirm no regressions.
	fullSuite := exec.Command("go", "test", "-vet=off", "-count=1",
		"./cmd/wagyu/", "./internal/...")
	fullSuite.Dir = root
	fullSuite.Stdout = os.Stdout
	fullSuite.Stderr = os.Stderr
	if err := fullSuite.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		fmt.Fprintf(os.Stderr, "[repro3] full test suite failed: %v\n", err)
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
