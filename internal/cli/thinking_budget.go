// AGY 1.2.13 changelog gate for --max-thinking-budget.
//
// "Fixed the --max-thinking-budget flag accepting a 0 value, which
// caused infinite thinking loops that locked up the CLI until Ctrl-C;
// values <= 0 now disable thinking entirely, matching the documented
// semantics."
//
// This file is the single source of truth for the gate: any caller
// holding a parsed --max-thinking-budget value funnels it through
// ThinkingEnabled() / ThinkingBudget() before forwarding to the model
// adapter. Centralising the rule means a future consumer (the print
// path, the TUI, a test rig) cannot drift back to the pre-fix
// behaviour where a raw 0 was passed through.
//
// The flag is registered in parser_specs.go under ExtraFlags() as a
// hidden int with a default of 0. Zero is the post-fix "disabled"
// value, so the unset default itself lands in the disabled branch —
// users who want thinking on must pass --max-thinking-budget=N with
// N > 0.
package cli

import (
	"flag"
	"strconv"
)

// ThinkingEnabled reports whether thinking should be on for the
// given --max-thinking-budget value. The rule is the AGY 1.2.13
// fix: any non-positive budget (the unset default 0, an explicit 0,
// or any negative number) disables thinking. Only positive integer
// budgets keep thinking on.
//
// This is a pure function so callers do not have to remember the
// rule and so the test suite can pin it without spinning up a
// Parser.
func ThinkingEnabled(budget int) bool {
	return budget > 0
}

// ThinkingBudget returns the budget to forward to the model adapter,
// or 0 when thinking is disabled. The post-fix semantic is that 0
// is the wire-level "off" sentinel, so callers that always forward
// the returned int to a `thinking_budget` field get "thinking off"
// for free when the user passed a non-positive budget.
//
// Returns (enabled, budget). When enabled is false, budget is 0 and
// the caller should NOT set the field at all (or should set it to 0,
// depending on the wire contract). The two-value form lets callers
// that distinguish "no field" from "field = 0" do the right thing
// without re-applying the gate.
func ThinkingBudget(raw int) (enabled bool, budget int) {
	if raw > 0 {
		return true, raw
	}
	return false, 0
}

// ThinkingBudgetFromFlagSet looks up --max-thinking-budget on a
// parsed stdlib *flag.FlagSet and returns the (enabled, budget)
// pair the wire-level adapter should honour. Returns (false, 0)
// when the flag is not registered or its value is malformed.
//
// The flag value comes back as a string because the intAtoiValue
// wrapper (see internal/cli/jetski.go) keeps the raw text alongside
// the parsed int — we re-parse so a malformed value surfaces as
// disabled rather than silently coercing to 0.
func ThinkingBudgetFromFlagSet(fs *flag.FlagSet) (enabled bool, budget int) {
	if fs == nil {
		return false, 0
	}
	f := fs.Lookup("max-thinking-budget")
	if f == nil {
		return false, 0
	}
	s := f.Value.String()
	if s == "" {
		return false, 0
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return false, 0
	}
	return ThinkingBudget(n)
}
