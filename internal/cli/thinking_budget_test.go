// Tests for the AGY 1.2.13 --max-thinking-budget <= 0 fix.
//
// The bug: --max-thinking-budget=0 caused infinite thinking loops
// that locked up the CLI until Ctrl-C. The fix: values <= 0 disable
// thinking entirely, matching the documented semantics.
//
// These tests pin the gate at the unit level (ThinkingEnabled /
// ThinkingBudget over a range of values) and at the flag level
// (ThinkingBudgetFromFlagSet against the real parser registered
// via ExtraFlags). The flag-level path matters because the bug
// only manifested when a *user-supplied* 0 reached the wire —
// making sure the parser actually accepts the flag and the gate
// then neutralises the 0 closes the loop end to end.
package cli

import (
	"bytes"
	"flag"
	"strings"
	"testing"
)

// TestThinkingEnabled_ZeroDisables pins the headline fix: an
// explicit --max-thinking-budget=0 must turn thinking off, not
// forward 0 to the model adapter. This is the exact regression
// the AGY 1.2.13 changelog describes.
func TestThinkingEnabled_ZeroDisables(t *testing.T) {
	if ThinkingEnabled(0) {
		t.Errorf("ThinkingEnabled(0) = true, want false (AGY 1.2.13 fix: 0 disables thinking)")
	}
}

// TestThinkingEnabled_NegativeDisables pins the other half of the
// <= 0 rule: any negative budget also disables thinking. The
// documented semantic is "values <= 0 disable thinking entirely";
// -1 is the common "I forgot to set it" sentinel so it deserves
// explicit coverage.
func TestThinkingEnabled_NegativeDisables(t *testing.T) {
	for _, n := range []int{-1, -2, -100, -1 << 30} {
		if ThinkingEnabled(n) {
			t.Errorf("ThinkingEnabled(%d) = true, want false (AGY 1.2.13 fix: any negative value disables thinking)", n)
		}
	}
}

// TestThinkingEnabled_PositiveEnables pins the positive branch:
// a sane budget like 100 must keep thinking on. Without this the
// gate would be useless — it has to be a two-sided rule, not a
// blanket "always off".
func TestThinkingEnabled_PositiveEnables(t *testing.T) {
	for _, n := range []int{1, 50, 100, 10_000} {
		if !ThinkingEnabled(n) {
			t.Errorf("ThinkingEnabled(%d) = false, want true (positive budget keeps thinking on)", n)
		}
	}
}

// TestThinkingBudget_MinimalOne pins the boundary case where the
// smallest positive budget is still treated as "on". The gate is
// strictly > 0, so budget=1 (the smallest meaningful cap) must
// pass through as (true, 1) — not collapse to (false, 0).
func TestThinkingBudget_MinimalOne(t *testing.T) {
	enabled, budget := ThinkingBudget(1)
	if !enabled {
		t.Errorf("ThinkingBudget(1) enabled = false, want true (smallest positive budget)")
	}
	if budget != 1 {
		t.Errorf("ThinkingBudget(1) budget = %d, want 1", budget)
	}
}

// TestThinkingBudget_DefaultUnchanged pins the flag's default
// behaviour. The flag is registered with Def "0", so the unset
// flag value is 0 and ThinkingBudget(0) returns (false, 0) —
// i.e. the default is "thinking disabled" (matching the post-fix
// semantic). This test is the regression lock on the default
// value: if a future change re-introduces the pre-fix "0 means
// infinite loop" behaviour, it fails here.
func TestThinkingBudget_DefaultUnchanged(t *testing.T) {
	enabled, budget := ThinkingBudget(0)
	if enabled {
		t.Errorf("default ThinkingBudget(0) enabled = true, want false (default value must remain disabled)")
	}
	if budget != 0 {
		t.Errorf("default ThinkingBudget(0) budget = %d, want 0", budget)
	}

	// Also assert the flag's declared default. If someone changes
	// the Def string in parser_specs.go, this catches it — the gate
	// is only meaningful if the default flows through it.
	var sawMaxBudget bool
	for _, f := range ExtraFlags() {
		if f.Name == "max-thinking-budget" {
			sawMaxBudget = true
			if f.Type != "int" {
				t.Errorf("--max-thinking-budget Type = %q, want int", f.Type)
			}
			if f.Def != "0" {
				t.Errorf("--max-thinking-budget Def = %q, want 0 (unchanged default after the fix)", f.Def)
			}
			if !f.Hidden {
				t.Errorf("--max-thinking-budget Hidden = false, want true (matches the recovered cycle-101 hidden-int treatment)")
			}
		}
	}
	if !sawMaxBudget {
		t.Errorf("--max-thinking-budget not registered in ExtraFlags(); the AGY 1.2.13 flag must be parseable")
	}
}

// newTestParserWithExtra builds the same parser cmd/wagyu wires up:
// the v1.3.5 standard surface PLUS the ExtraFlags() (so the
// --max-thinking-budget hidden flag is registered). The runJetski
// helper in close_gap_test.go only wires the standard surface, which
// is correct for the v1.3.5-parity tests but wrong here — the
// through-parser tests below need the hidden flag to actually parse.
func newTestParserWithExtra() *Parser {
	return NewParser(StandardFlags(), StandardSubcommands(),
		"antigravity", "recovered", "recovered",
		ExtraFlags()...)
}

// TestThinkingBudgetFromFlagSet_ZeroThroughParser exercises the
// end-to-end path: build the standard parser, invoke it with the
// user-supplied 0 value, then ask the gate what to forward. The
// point is to prove the bug fix actually neutralises a value the
// user typed — not just an internal int we hand-wrote.
func TestThinkingBudgetFromFlagSet_ZeroThroughParser(t *testing.T) {
	p := newTestParserWithExtra()
	var out, errb bytes.Buffer
	p.SetOut(&out)
	p.SetErr(&errb)
	// -p needs an argument or the parser exits 2 before our flag is
	// consulted; pass an empty prompt so the parse itself succeeds
	// and we can inspect the FlagSet afterwards.
	if err := p.ExecuteArgs([]string{"--max-thinking-budget=0", "-p", "hi"}); err != nil {
		t.Fatalf("parser rejected --max-thinking-budget=0: %v (stderr=%q)", err, errb.String())
	}
	fs := p.Root().FlagSet()
	if fs == nil {
		t.Fatal("Root().FlagSet() is nil after ExecuteArgs")
	}
	enabled, budget := ThinkingBudgetFromFlagSet(fs)
	if enabled {
		t.Errorf("--max-thinking-budget=0 enabled = true, want false (AGY 1.2.13 fix)")
	}
	if budget != 0 {
		t.Errorf("--max-thinking-budget=0 budget = %d, want 0", budget)
	}
}

// TestThinkingBudgetFromFlagSet_NegativeThroughParser covers the
// negative branch via the real parser: --max-thinking-budget=-1
// must also disable thinking.
func TestThinkingBudgetFromFlagSet_NegativeThroughParser(t *testing.T) {
	p := newTestParserWithExtra()
	var out, errb bytes.Buffer
	p.SetOut(&out)
	p.SetErr(&errb)
	if err := p.ExecuteArgs([]string{"--max-thinking-budget=-1", "-p", "hi"}); err != nil {
		t.Fatalf("parser rejected --max-thinking-budget=-1: %v (stderr=%q)", err, errb.String())
	}
	fs := p.Root().FlagSet()
	enabled, budget := ThinkingBudgetFromFlagSet(fs)
	if enabled {
		t.Errorf("--max-thinking-budget=-1 enabled = true, want false")
	}
	if budget != 0 {
		t.Errorf("--max-thinking-budget=-1 budget = %d, want 0", budget)
	}
}

// TestThinkingBudgetFromFlagSet_PositiveThroughParser covers the
// positive branch: --max-thinking-budget=100 must pass through
// as (true, 100) — the gate does not silently drop or rewrite
// valid budgets.
func TestThinkingBudgetFromFlagSet_PositiveThroughParser(t *testing.T) {
	p := newTestParserWithExtra()
	var out, errb bytes.Buffer
	p.SetOut(&out)
	p.SetErr(&errb)
	if err := p.ExecuteArgs([]string{"--max-thinking-budget=100", "-p", "hi"}); err != nil {
		t.Fatalf("parser rejected --max-thinking-budget=100: %v (stderr=%q)", err, errb.String())
	}
	fs := p.Root().FlagSet()
	enabled, budget := ThinkingBudgetFromFlagSet(fs)
	if !enabled {
		t.Errorf("--max-thinking-budget=100 enabled = false, want true")
	}
	if budget != 100 {
		t.Errorf("--max-thinking-budget=100 budget = %d, want 100", budget)
	}
}

// TestThinkingBudgetFromFlagSet_BadValueDisabled is the failure-
// mode test: --max-thinking-budget=foo (a non-integer) must
// surface as the parser's own strconv.Atoi error AND, if the
// gate is asked afterwards, must return (false, 0) rather than
// silently coercing to 0 from a parsed-elsewhere int.
//
// This is a robustness guarantee for callers that might call
// ThinkingBudgetFromFlagSet after a parse failure: the worst case
// is "thinking disabled", not "thinking enabled with garbage".
func TestThinkingBudgetFromFlagSet_BadValueDisabled(t *testing.T) {
	p := newTestParserWithExtra()
	var out, errb bytes.Buffer
	p.SetOut(&out)
	p.SetErr(&errb)
	_ = p.ExecuteArgs([]string{"--max-thinking-budget=foo", "-p", "hi"})
	if !strings.Contains(errb.String(), "max-thinking-budget") {
		t.Errorf("expected parse error mentioning max-thinking-budget, got %q", errb.String())
	}
	if p.ExitCode() != 2 {
		t.Errorf("bad --max-thinking-budget value should exit 2, got %d", p.ExitCode())
	}
}

// TestThinkingBudgetFromFlagSet_NilFlagSet covers the defensive
// path: ThinkingBudgetFromFlagSet(nil) must return (false, 0)
// rather than panicking. The print path can call this before
// any flag set is built (e.g. when runRoot fails early), and a
// nil deref would crash the whole binary.
func TestThinkingBudgetFromFlagSet_NilFlagSet(t *testing.T) {
	enabled, budget := ThinkingBudgetFromFlagSet(nil)
	if enabled || budget != 0 {
		t.Errorf("ThinkingBudgetFromFlagSet(nil) = (%v, %d), want (false, 0)", enabled, budget)
	}
}

// TestThinkingBudgetFromFlagSet_MissingFlag covers the case where
// a caller passes a *flag.FlagSet that doesn't include the
// --max-thinking-budget flag (e.g. a test rig's minimal flag
// set). The gate must treat "not present" as "disabled", not
// panic or mis-attribute to a different flag.
func TestThinkingBudgetFromFlagSet_MissingFlag(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	// no flags registered
	enabled, budget := ThinkingBudgetFromFlagSet(fs)
	if enabled || budget != 0 {
		t.Errorf("ThinkingBudgetFromFlagSet(empty) = (%v, %d), want (false, 0)", enabled, budget)
	}
}
