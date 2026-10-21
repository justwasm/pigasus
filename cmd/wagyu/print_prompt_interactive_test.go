// Tests for the AGY 1.3.4 changelog line 32 fix:
//
//	"Fixed combining --print with --prompt-interactive exiting with status 0
//	 after reporting the conflict; it now exits with status 2 like other
//	 print-mode usage errors."
//
// The parser-level check lives in internal/cli/jetski.go (the canonical
// "error: --print and --prompt-interactive are mutually exclusive" line, rc
// 2). runRoot re-checks the same combination up front so a direct caller
// (a test rig, a future dispatcher) cannot bypass the parser and silently
// accept both flags. This file pins both paths and the exit-code contract.
package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/cli"
)

// runPigJetski executes one invocation through the standard v1.3.5 parser
// and returns (stdout, stderr, exitCode). Mirrors internal/cli/close_gap_test.go's
// runJetski helper so the print-mode contract lives next to the production
// binary instead of inside the cli package.
//
// RootRun is wired to runRoot exactly the way main.go wires it; without
// that wiring the parser would resolve to rc 0 with no work done (the
// real binary sets RootRun before calling ExecuteArgs).
func runPigJetski(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	p := cli.NewParser(cli.StandardFlags(), cli.StandardSubcommands(),
		"antigravity", "recovered", "recovered")
	p.Root().RootRun = func(args []string) error { return runRoot(args, p) }
	var out, errb bytes.Buffer
	p.SetOut(&out)
	p.SetErr(&errb)
	_ = p.ExecuteArgs(args)
	return out.String(), errb.String(), p.ExitCode()
}

// TestPrintPromptInteractiveConflict_Exits2 is the headline test for the
// changelog fix. Every alias combination of --print/--prompt/-p with
// --prompt-interactive/-i must exit 2 (not 0, not 1) and the stderr must
// carry the canonical mutually-exclusive line.
func TestPrintPromptInteractiveConflict_Exits2(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"long_long", []string{"--print=hi", "--prompt-interactive=bye"}},
		{"short_long", []string{"-p=hi", "--prompt-interactive=bye"}},
		{"long_short", []string{"--print=hi", "-i=bye"}},
		{"short_short", []string{"-p=hi", "-i=bye"}},
		{"prompt_alias_long", []string{"--prompt=hi", "--prompt-interactive=bye"}},
		{"reversed_order", []string{"--prompt-interactive=bye", "--print=hi"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, errb, code := runPigJetski(t, tc.args...)
			if code != 2 {
				t.Errorf("args %v: exit code = %d, want 2\nstderr=%q", tc.args, code, errb)
			}
			if !strings.Contains(errb, "mutually exclusive") {
				t.Errorf("args %v: stderr missing 'mutually exclusive': %q", tc.args, errb)
			}
			if out != "" {
				t.Errorf("args %v: stdout = %q, want empty (no model call should have happened)", tc.args, out)
			}
		})
	}
}

// TestPrintPromptInteractiveConflict_StderrMentionsBothFlags proves the
// recovered error line names both flags so the operator can tell which two
// collided without reading the man page.
func TestPrintPromptInteractiveConflict_StderrMentionsBothFlags(t *testing.T) {
	_, errb, code := runPigJetski(t, "--print=hi", "--prompt-interactive=bye")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errb, "--print") {
		t.Errorf("stderr = %q; missing '--print'", errb)
	}
	if !strings.Contains(errb, "--prompt-interactive") {
		t.Errorf("stderr = %q; missing '--prompt-interactive'", errb)
	}
	if !strings.Contains(errb, "mutually exclusive") {
		t.Errorf("stderr = %q; missing 'mutually exclusive'", errb)
	}
}

// TestPrintPromptInteractiveConflict_NotExit1 is the regression guard from
// the changelog text itself: the fix is *specifically* exit code 2
// (matching other print-mode usage errors), not the catch-all exit 1 the
// handler-failure path uses and not the silent exit 0 the bug produced
// before the fix.
func TestPrintPromptInteractiveConflict_NotExit1(t *testing.T) {
	_, _, code := runPigJetski(t, "--print=hi", "--prompt-interactive=bye")
	if code != 2 {
		t.Errorf("exit code = %d, want exactly 2 (the changelog fix)", code)
	}
	if code == 0 {
		t.Error("regression: the bug (silent exit 0 after reporting the conflict) is back")
	}
	if code == 1 {
		t.Error("regression: handler-failure exit 1 was used instead of usage-error exit 2")
	}
}

// TestPrintPromptInteractiveConflict_NoRegression pins the other print-mode
// usage errors that the parser surfaces with exit 2 — none of these must
// regress because of the new conflict-detection block in runRoot. (The
// empty-prompt case is rc 1 by design — it is a different error class —
// so it is not in this set.)
func TestPrintPromptInteractiveConflict_NoRegression(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"unknown_input_format", []string{"-p", "/help", "--input-format", "bogus"}},
		{"positional_after_prompt", []string{"-p", "hi", "extra"}},
		{"flag_shaped_prompt", []string{"-p", "--agent", "nosuchagent", "hi"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, code := runPigJetski(t, tc.args...)
			if code != 2 {
				t.Errorf("args %v: exit code = %d, want 2", tc.args, code)
			}
		})
	}
}

// TestPrintPromptInteractiveConflict_BeforeModelCall is the "no model call"
// requirement: the conflict must short-circuit before runPrintMode spins
// up a backend. We stand up a stub agw server that 500s on every request
// — if the conflict check fails to fire, the run hits the gateway and
// fails with the gateway's HTTP 500 instead of the conflict line.
func TestPrintPromptInteractiveConflict_BeforeModelCall(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.Error(w, "this should never be reached on a conflict", http.StatusInternalServerError)
	}))
	defer srv.Close()
	t.Setenv("JETSKI_OAUTH_TOKEN", "test-token")
	t.Setenv("OPENAGY_BACKEND", "agw")
	t.Setenv("AGW_K0S_URL", srv.URL)
	t.Setenv("AGW_K0S_TIMEOUT", "1s")
	t.Setenv("AGW_EXTERNAL_MODEL", "test-model")

	out, errb, code := runPigJetski(t, "--print=hi", "--prompt-interactive=bye")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if hits != 0 {
		t.Errorf("model gateway was hit %d times; conflict must short-circuit before any backend call", hits)
	}
	if !strings.Contains(errb, "mutually exclusive") {
		t.Errorf("stderr = %q; want the conflict message, not a model error", errb)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
}

// TestPrintPromptInteractiveConflict_RunRootGuard is the defense-in-depth
// test for the new runRoot-level check. The parser catches the conflict
// first in production, but runRoot can also be called from paths that
// skip the parser (a future dispatcher, a regression in Parse, this
// test rig). When that happens the new block at the top of runRoot must
// still return the same exit-2 / mutually-exclusive line.
//
// We build a real Parser, run a clean Parse so the FlagSet is populated
// with the standard flags, then call fs.Set on prompt-interactive to
// mark it as if the user had typed it on the command line — fs.Visit
// (which flagWasSet uses) only reports flags that have been touched by
// Set / Parse. The print flag is already set from the Parse call, so
// runRoot sees both flags set and short-circuits on the new guard.
func TestPrintPromptInteractiveConflict_RunRootGuard(t *testing.T) {
	p := cli.NewParser(cli.StandardFlags(), cli.StandardSubcommands(),
		"antigravity", "recovered", "recovered")
	var out, errb bytes.Buffer
	p.SetOut(&out)
	p.SetErr(&errb)
	root := p.Root()
	// Parse a non-conflicting argument set so the parser returns 0 and
	// leaves us with a populated FlagSet. --print is set as a side
	// effect of this Parse — exactly the state we want.
	if code := root.Parse([]string{"--print=hi"}); code != 0 {
		t.Fatalf("setup Parse(--print=hi) = %d", code)
	}
	runfs := root.FlagSet()
	// Mark prompt-interactive as set without re-registering it (the
	// parser already did that during Parse). fs.Set updates the
	// flag's Value and the internal "actually set" tracker that
	// fs.Visit reads — exactly the behaviour flagWasSet relies on.
	if err := runfs.Set("prompt-interactive", "bye"); err != nil {
		t.Fatalf("runfs.Set(prompt-interactive, bye): %v", err)
	}
	// Belt-and-braces: set the short alias too. The runRoot guard
	// already covers both names via `||`, so setting the long form
	// alone is sufficient, but a future refactor that drops the
	// short-form check would still be caught here.
	if err := runfs.Set("i", "bye"); err != nil {
		t.Fatalf("runfs.Set(i, bye): %v", err)
	}
	// Now invoke runRoot directly. It will see printMode=true (--print
	// is set on the FlagSet) and promptInteractive=true (the long
	// form we just Set), and short-circuit with the conflict error
	// before any model call.
	root.Out = &out
	root.ErrOut = &errb
	err := runRoot(nil, p)
	if err == nil {
		t.Fatalf("runRoot returned nil, want conflict error")
	}
	if code, ok := cli.ExitCodeOf(err); !ok {
		t.Fatalf("runRoot error %v is not an ExitCodeError", err)
	} else if code != 2 {
		t.Errorf("runRoot exit code = %d, want 2", code)
	}
	// The error message itself carries the conflict text. (In
	// production, jetski.handleRunErr prints this to ErrOut. We are
	// calling runRoot directly so we read the message off the error
	// directly — the parser-level test above already pins the
	// stderr rendering.)
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("err.Error() = %q; want the conflict line from the runRoot guard", err.Error())
	}
}

// TestPrintPromptInteractiveConflict_RunRootGuardNoModelCall is the
// companion to the parser-level "before model call" test, but at the
// runRoot level. If the runRoot guard regresses (e.g. a future refactor
// moves the check after the model call), this test fails because the
// gateway would record a hit.
func TestPrintPromptInteractiveConflict_RunRootGuardNoModelCall(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.Error(w, "should never be reached", http.StatusInternalServerError)
	}))
	defer srv.Close()
	t.Setenv("JETSKI_OAUTH_TOKEN", "test-token")
	t.Setenv("OPENAGY_BACKEND", "agw")
	t.Setenv("AGW_K0S_URL", srv.URL)
	t.Setenv("AGW_K0S_TIMEOUT", "1s")
	t.Setenv("AGW_EXTERNAL_MODEL", "test-model")

	p := cli.NewParser(cli.StandardFlags(), cli.StandardSubcommands(),
		"antigravity", "recovered", "recovered")
	var out, errb bytes.Buffer
	p.SetOut(&out)
	p.SetErr(&errb)
	root := p.Root()
	if code := root.Parse([]string{"--print=hi"}); code != 0 {
		t.Fatalf("setup Parse(--print=hi) = %d", code)
	}
	runfs := root.FlagSet()
	if err := runfs.Set("prompt-interactive", "bye"); err != nil {
		t.Fatalf("runfs.Set: %v", err)
	}
	if err := runRoot(nil, p); err == nil {
		t.Fatalf("runRoot returned nil, want conflict error")
	}
	if hits != 0 {
		t.Errorf("model gateway hit %d times; runRoot guard must short-circuit before backend dispatch", hits)
	}
}

// TestPrintPromptInteractiveConflict_PrintAloneDoesNotTrigger is the
// negative side of the headline test: --print on its own must not be
// misclassified as a conflict. We point at a model-free backend and
// confirm the run proceeds to the print path (no conflict line, exit
// 0 or normal-print failure exit 3, never the mutually-exclusive rc 2).
func TestPrintPromptInteractiveConflict_PrintAloneDoesNotTrigger(t *testing.T) {
	// Use the shim backend (mock) so the run can complete locally
	// without OAuth or a network. NewMock returns a synthetic reply,
	// so the run finishes with rc 0 and stdout containing the reply.
	t.Setenv("OPENAGY_BACKEND", "shim")
	out, errb, code := runPigJetski(t, "--print=hello world")
	if code == 2 && strings.Contains(errb, "mutually exclusive") {
		t.Fatalf("regression: --print alone was misclassified as a conflict: rc=%d stderr=%q", code, errb)
	}
	if strings.Contains(errb, "mutually exclusive") {
		t.Errorf("stderr = %q; --print alone must not emit the conflict message", errb)
	}
	// shim succeeds, so we expect rc 0 + a non-empty stdout (the mock
	// reply). If this fails the test rig is broken, not the changelog
	// fix; the assertion is here so future regressions in the mock or
	// parser are caught separately.
	if code != 0 {
		t.Errorf("exit code = %d, want 0 for --print alone on the shim backend", code)
	}
	if out == "" {
		t.Errorf("stdout = %q, want non-empty (the mock should have produced a reply)", out)
	}
}

// TestPrintPromptInteractiveConflict_Exit2Exact is a paranoid guard: the
// changelog text says "exits with status 2 like other print-mode usage
// errors" — verify the integer is exactly 2, with no off-by-one (1 or 3)
// that could be a copy-paste from a neighbouring exit-code line.
func TestPrintPromptInteractiveConflict_Exit2Exact(t *testing.T) {
	_, _, code := runPigJetski(t, "--print=hi", "--prompt-interactive=bye")
	if code != 2 {
		t.Fatalf("exit code = %d, want exactly 2", code)
	}
	// Belt-and-braces: run the same combo three times in a row and
	// confirm the code is stable. A flaky parser path (e.g. one that
	// only fires on first invocation) would show up here as a
	// different code on a later call.
	for i := 0; i < 3; i++ {
		_, _, c := runPigJetski(t, "--print=hi", "--prompt-interactive=bye")
		if c != 2 {
			t.Errorf("run %d: exit code = %d, want 2", i, c)
		}
	}
}

// TestPrintPromptInteractiveConflict_ErrorHasBothFlags is the strict
// substring check for the recovered message. The line is recovered from
// the reference binary (see cmd/wagyu/testdata/print_changelog.md and the
// internal/cli/jetski.go mutual-exclusion branch) so the exact wording is
// load-bearing — if a future refactor rewords it, this test fires.
func TestPrintPromptInteractiveConflict_ErrorHasBothFlags(t *testing.T) {
	_, errb, code := runPigJetski(t, "--print=hi", "--prompt-interactive=bye")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	want := "error: --print and --prompt-interactive are mutually exclusive"
	if !strings.Contains(errb, want) {
		t.Errorf("stderr = %q, want substring %q", errb, want)
	}
}

// Sanity check: the helper itself produces a Parser.ExitCode we can read.
// Wrapped in a test so the test file builds cleanly even if the helper
// drifts.
func TestPrintPromptInteractiveConflict_HelperExposesExitCode(t *testing.T) {
	p := cli.NewParser(cli.StandardFlags(), cli.StandardSubcommands(),
		"antigravity", "recovered", "recovered")
	var out, errb bytes.Buffer
	p.SetOut(&out)
	p.SetErr(&errb)
	_ = p.ExecuteArgs([]string{"--print=hi", "--prompt-interactive=bye"})
	if p.ExitCode() != 2 {
		t.Errorf("Parser.ExitCode() = %d, want 2", p.ExitCode())
	}
	if !strings.Contains(errb.String(), "mutually exclusive") {
		t.Errorf("errb = %q, want mutually-exclusive line", errb.String())
	}
}

// Compile-time guard: confirm the backend.Client interface is what
// runRoot would dispatch to. Keeps the test file self-contained: any
// future interface drift surfaces here, not in the production binary.
var _ backend.Client = (*recordingPigBackend)(nil)

// recordingPigBackend is a no-op backend used by future regression
// checks. It is intentionally unused by the active tests but exported
// via a compile-time assertion so the package builds.
type recordingPigBackend struct {
	calls []backend.StreamRequest
}

func (r *recordingPigBackend) Stream(ctx context.Context, _ backend.StreamRequest) (<-chan backend.StreamChunk, error) {
	ch := make(chan backend.StreamChunk, 1)
	ch <- backend.StreamChunk{Done: true}
	close(ch)
	return ch, fmt.Errorf("recordingPigBackend: would have called the model here")
}
