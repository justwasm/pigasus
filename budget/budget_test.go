// budget_test.go — unit tests for the recovered localharness budget
// manager.
//
// The tests pin the load-bearing arithmetic in isolation: which limit
// fires first, what the scope window measures, how the baseline is
// subtracted, and that subagent spend stays out of the root trajectory's
// budget. A regression in any of those lands as one explicit failure here
// rather than as "the agent stopped early" in production.
//
// Standard library only — the repository's CI runs `go test ./cmd/...
// ./internal/...` with no test dependencies.
package budget

import (
	"fmt"
	"sync"
	"testing"
)

// TestLimitKindsExhaust pins every limit kind: the exhausted limit, the
// stop reason it maps to, and the verbatim error text.
//
// 【反编译】 The "used >= limit" comparison and the "a limit of 0 is not
// set" rule come from the reference's exhaustedLimitStopReason
// (0x101946130).
func TestLimitKindsExhaust(t *testing.T) {
	cases := []struct {
		name      string
		cfg       Config
		record    func(*Manager)
		wantKind  LimitKind
		wantLimit uint64
		wantUsed  uint64
		wantStop  StopReason
		wantErr   string
	}{
		{
			name:      "model calls",
			cfg:       Config{MaxModelCalls: 2},
			record:    func(m *Manager) { m.RecordModelCall(); m.RecordModelCall() },
			wantKind:  LimitModelCalls,
			wantLimit: 2,
			wantUsed:  2,
			wantStop:  StopReasonMaxModelCallsExceeded,
			wantErr:   "Model call budget exceeded (limit: 2). Do not emit more tool calls.",
		},
		{
			name:      "tool calls",
			cfg:       Config{MaxToolCalls: 3},
			record:    func(m *Manager) { m.RecordToolCall(); m.RecordToolCall(); m.RecordToolCall() },
			wantKind:  LimitToolCalls,
			wantLimit: 3,
			wantUsed:  3,
			wantStop:  StopReasonMaxToolCallsExceeded,
			wantErr:   "Tool call budget exceeded (limit: 3). Do not emit more tool calls.",
		},
		{
			name:      "input tokens",
			cfg:       Config{MaxInputTokens: 100},
			record:    func(m *Manager) { m.RecordUsage(100, 0) },
			wantKind:  LimitInputTokens,
			wantLimit: 100,
			wantUsed:  100,
			wantStop:  StopReasonMaxInputTokensExceeded,
			wantErr:   "Input token budget exceeded (limit: 100). Do not emit more tool calls.",
		},
		{
			name:      "output tokens",
			cfg:       Config{MaxOutputTokens: 50},
			record:    func(m *Manager) { m.RecordUsage(0, 50) },
			wantKind:  LimitOutputTokens,
			wantLimit: 50,
			wantUsed:  50,
			wantStop:  StopReasonMaxOutputTokensExceeded,
			wantErr:   "Output token budget exceeded (limit: 50). Do not emit more tool calls.",
		},
		{
			name:      "total tokens",
			cfg:       Config{MaxTotalTokens: 200},
			record:    func(m *Manager) { m.RecordUsage(120, 80) },
			wantKind:  LimitTotalTokens,
			wantLimit: 200,
			wantUsed:  200,
			wantStop:  StopReasonMaxTotalTokensExceeded,
			wantErr:   "Total token budget exceeded (limit: 200). Do not emit more tool calls.",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewManager(tc.cfg)
			tc.record(m)

			v, ok := m.LimitExhausted()
			if !ok {
				t.Fatalf("LimitExhausted() = not exhausted, want %v", tc.wantKind)
			}
			if v.Kind != tc.wantKind {
				t.Errorf("violation kind = %v, want %v", v.Kind, tc.wantKind)
			}
			if v.Limit != tc.wantLimit {
				t.Errorf("violation limit = %d, want %d", v.Limit, tc.wantLimit)
			}
			if v.Used != tc.wantUsed {
				t.Errorf("violation used = %d, want %d", v.Used, tc.wantUsed)
			}
			if got := m.StopReason(); got != tc.wantStop {
				t.Errorf("StopReason() = %v, want %v", got, tc.wantStop)
			}
			err := m.Check()
			if err == nil {
				t.Fatalf("Check() = nil, want an error")
			}
			if err.Error() != tc.wantErr {
				t.Errorf("Check() = %q, want %q", err.Error(), tc.wantErr)
			}
			// The violation must be recoverable as a typed error.
			var violation Violation
			if !asViolation(err, &violation) {
				t.Fatalf("Check() returned %T, want budget.Violation", err)
			}
			if violation.Kind != tc.wantKind {
				t.Errorf("typed violation kind = %v, want %v", violation.Kind, tc.wantKind)
			}
		})
	}
}

// asViolation is a tiny stand-in for errors.As so the test file keeps to
// the standard library without importing errors for one call.
func asViolation(err error, out *Violation) bool {
	v, ok := err.(Violation)
	if ok {
		*out = v
	}
	return ok
}

// TestLimitNotExhaustedBelowLimit pins the boundary: one unit below the
// limit is not a violation, and the limit itself is.
func TestLimitNotExhaustedBelowLimit(t *testing.T) {
	m := NewManager(Config{MaxModelCalls: 2})
	m.RecordModelCall()

	if _, ok := m.LimitExhausted(); ok {
		t.Fatalf("LimitExhausted() = exhausted at 1 call with a limit of 2")
	}
	if err := m.Check(); err != nil {
		t.Fatalf("Check() = %v, want nil", err)
	}
	if got := m.StopReason(); got != StopReasonNone {
		t.Fatalf("StopReason() = %v, want StopReasonNone", got)
	}

	m.RecordModelCall()
	if _, ok := m.LimitExhausted(); !ok {
		t.Fatalf("LimitExhausted() = not exhausted at the limit of 2 calls")
	}
}

// TestZeroLimitIsUnset pins that a limit of 0 constrains nothing.
func TestZeroLimitIsUnset(t *testing.T) {
	m := NewManager(Config{})
	for i := 0; i < 10; i++ {
		m.RecordModelCall()
		m.RecordToolCall()
		m.RecordUsage(1000, 1000)
	}
	if m.HasBudget() {
		t.Errorf("HasBudget() = true, want false for a config without limits")
	}
	if _, ok := m.LimitExhausted(); ok {
		t.Errorf("LimitExhausted() = exhausted, want not exhausted")
	}
	if got := m.StopReason(); got != StopReasonNone {
		t.Errorf("StopReason() = %v, want StopReasonNone", got)
	}
}

// TestEvaluationOrder pins the precedence of the limit checks: model calls
// → tool calls → input tokens → output tokens → total tokens, first
// exhausted wins.
//
// The last case pins the input-before-output order of this clone. The
// binary checks output tokens first (0x101946130, +0x28 before +0x20) —
// see the 存疑 section of the package doc. The clone's order is the local
// contract and the two only differ when both token limits are exhausted at
// the same instant.
func TestEvaluationOrder(t *testing.T) {
	cases := []struct {
		name     string
		cfg      Config
		record   func(*Manager)
		wantKind LimitKind
	}{
		{
			name:     "model calls win over total tokens",
			cfg:      Config{MaxModelCalls: 1, MaxTotalTokens: 1},
			record:   func(m *Manager) { m.RecordModelCall(); m.RecordUsage(5, 5) },
			wantKind: LimitModelCalls,
		},
		{
			name:     "tool calls win over input tokens",
			cfg:      Config{MaxToolCalls: 1, MaxInputTokens: 1},
			record:   func(m *Manager) { m.RecordToolCall(); m.RecordUsage(5, 5) },
			wantKind: LimitToolCalls,
		},
		{
			name:     "input tokens win over output tokens",
			cfg:      Config{MaxInputTokens: 1, MaxOutputTokens: 1},
			record:   func(m *Manager) { m.RecordUsage(5, 5) },
			wantKind: LimitInputTokens,
		},
		{
			name:     "output tokens win over total tokens",
			cfg:      Config{MaxOutputTokens: 1, MaxTotalTokens: 1},
			record:   func(m *Manager) { m.RecordUsage(5, 5) },
			wantKind: LimitOutputTokens,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewManager(tc.cfg)
			tc.record(m)
			v, ok := m.LimitExhausted()
			if !ok {
				t.Fatalf("LimitExhausted() = not exhausted, want %v", tc.wantKind)
			}
			if v.Kind != tc.wantKind {
				t.Fatalf("violation kind = %v, want %v", v.Kind, tc.wantKind)
			}
		})
	}
}

// TestScopeLifetime pins the lifetime window: the evaluated usage is the
// cumulative usage minus the baseline.
func TestScopeLifetime(t *testing.T) {
	m := NewManager(Config{MaxModelCalls: 10, Scope: ScopeLifetime})
	m.RecordModelCall()
	m.RecordModelCall()

	if got := m.WindowUsage().ModelCalls; got != 2 {
		t.Fatalf("WindowUsage().ModelCalls = %d, want 2", got)
	}
	if got := m.Usage().ModelCalls; got != 2 {
		t.Fatalf("Usage().ModelCalls = %d, want 2", got)
	}

	// A baseline moves the reference point.
	m.SetBaseline(Usage{ModelCalls: 1})
	if got := m.WindowUsage().ModelCalls; got != 1 {
		t.Fatalf("WindowUsage().ModelCalls = %d after the baseline, want 1", got)
	}
	if got := m.Usage().ModelCalls; got != 2 {
		t.Fatalf("Usage().ModelCalls = %d, want 2 (the baseline does not touch the counters)", got)
	}
	if got := m.Baseline().ModelCalls; got != 1 {
		t.Fatalf("Baseline().ModelCalls = %d, want 1", got)
	}
}

// TestScopeUnspecifiedBehavesLikeLifetime pins that the zero scope
// evaluates the lifetime window.
func TestScopeUnspecifiedBehavesLikeLifetime(t *testing.T) {
	m := NewManager(Config{MaxModelCalls: 10})
	m.RecordModelCall()
	m.SetBaseline(Usage{ModelCalls: 1})

	if got := m.WindowUsage().ModelCalls; got != 0 {
		t.Fatalf("WindowUsage().ModelCalls = %d, want 0 (lifetime minus baseline)", got)
	}
}

// TestScopeForwardLooking pins that a forward-looking budget only counts
// the usage recorded since the last window, and that the baseline does not
// apply to it.
func TestScopeForwardLooking(t *testing.T) {
	m := NewManager(Config{MaxToolCalls: 2, Scope: ScopeForwardLooking})
	m.RecordToolCall()
	m.RecordToolCall()

	if _, ok := m.LimitExhausted(); !ok {
		t.Fatalf("LimitExhausted() = not exhausted at the tool-call limit of 2")
	}

	// A new window re-opens the budget. The cumulative counters keep
	// growing.
	m.NewWindow()
	if _, ok := m.LimitExhausted(); ok {
		t.Fatalf("LimitExhausted() = exhausted after NewWindow, want not exhausted")
	}
	if got := m.Usage().ToolCalls; got != 2 {
		t.Fatalf("Usage().ToolCalls = %d after NewWindow, want 2 (the cumulative counters keep growing)", got)
	}
	if got := m.WindowUsage().ToolCalls; got != 0 {
		t.Fatalf("WindowUsage().ToolCalls = %d after NewWindow, want 0", got)
	}

	// The baseline does not apply to the forward-looking window.
	m.SetBaseline(Usage{ToolCalls: 99})
	if got := m.WindowUsage().ToolCalls; got != 0 {
		t.Fatalf("WindowUsage().ToolCalls = %d, want 0 (the baseline does not apply)", got)
	}

	m.RecordToolCall()
	if got := m.WindowUsage().ToolCalls; got != 1 {
		t.Fatalf("WindowUsage().ToolCalls = %d, want 1", got)
	}
}

// TestResetWindowIsAnAliasOfNewWindow pins that ResetWindow zeroes the
// forward-looking window and nothing else.
func TestResetWindowIsAnAliasOfNewWindow(t *testing.T) {
	m := NewManager(Config{MaxToolCalls: 1, Scope: ScopeForwardLooking})
	m.RecordToolCall()
	m.RecordUsage(10, 10)

	m.ResetWindow()
	if got := m.WindowUsage().ToolCalls; got != 0 {
		t.Errorf("WindowUsage().ToolCalls = %d after ResetWindow, want 0", got)
	}
	if got := m.WindowUsage().TotalTokens; got != 0 {
		t.Errorf("WindowUsage().TotalTokens = %d after ResetWindow, want 0", got)
	}
	if got := m.Usage().ToolCalls; got != 1 {
		t.Errorf("Usage().ToolCalls = %d after ResetWindow, want 1", got)
	}
	if got := m.Usage().TotalTokens; got != 20 {
		t.Errorf("Usage().TotalTokens = %d after ResetWindow, want 20", got)
	}
}

// TestBaselineSubtraction pins the lifetime window's arithmetic: the
// cumulative usage minus the baseline, saturating at zero.
func TestBaselineSubtraction(t *testing.T) {
	m := NewManager(Config{MaxModelCalls: 10, Scope: ScopeLifetime})
	for i := 0; i < 5; i++ {
		m.RecordModelCall()
	}
	m.RecordUsage(100, 50)

	m.SetBaseline(Usage{ModelCalls: 3, InputTokens: 40, TotalTokens: 60})

	got := m.WindowUsage()
	if got.ModelCalls != 2 {
		t.Errorf("WindowUsage().ModelCalls = %d, want 2", got.ModelCalls)
	}
	if got.InputTokens != 60 {
		t.Errorf("WindowUsage().InputTokens = %d, want 60", got.InputTokens)
	}
	if got.OutputTokens != 50 {
		t.Errorf("WindowUsage().OutputTokens = %d, want 50", got.OutputTokens)
	}
	if got.TotalTokens != 90 {
		t.Errorf("WindowUsage().TotalTokens = %d, want 90", got.TotalTokens)
	}
}

// TestBaselineLargerThanUsageSaturates pins that a baseline above the
// cumulative usage yields a zero window instead of a negative one.
func TestBaselineLargerThanUsageSaturates(t *testing.T) {
	m := NewManager(Config{MaxModelCalls: 2})
	m.RecordModelCall()
	m.SetBaseline(Usage{ModelCalls: 50})

	if got := m.WindowUsage().ModelCalls; got != 0 {
		t.Fatalf("WindowUsage().ModelCalls = %d, want 0 (saturated)", got)
	}
	if _, ok := m.LimitExhausted(); ok {
		t.Fatalf("LimitExhausted() = exhausted, want not exhausted")
	}
}

// TestNewManagerWithBaseline pins that the lifetime window starts empty and
// counts only the spend recorded after construction.
func TestNewManagerWithBaseline(t *testing.T) {
	m := NewManagerWithBaseline(Config{MaxModelCalls: 2}, Usage{ModelCalls: 5, ToolCalls: 7})

	if got := m.Baseline().ModelCalls; got != 5 {
		t.Errorf("Baseline().ModelCalls = %d, want 5", got)
	}
	if got := m.WindowUsage().ModelCalls; got != 0 {
		t.Errorf("WindowUsage().ModelCalls = %d, want 0", got)
	}

	m.RecordModelCall()
	m.RecordModelCall()
	if _, ok := m.LimitExhausted(); !ok {
		t.Fatalf("LimitExhausted() = not exhausted at the model-call limit of 2")
	}
	if got := m.Usage().ModelCalls; got != 7 {
		t.Errorf("Usage().ModelCalls = %d, want 7 (the baseline plus the recorded calls)", got)
	}
}

// TestMergeBudgetConfigs pins the merge semantics: the tighter non-zero
// limit wins, a zero contributes nothing, and the forward-looking scope
// wins a scope disagreement.
func TestMergeBudgetConfigs(t *testing.T) {
	cases := []struct {
		name string
		a    Config
		b    Config
		want Config
	}{
		{
			name: "the smaller limit wins",
			a:    Config{MaxModelCalls: 10, MaxToolCalls: 4},
			b:    Config{MaxModelCalls: 3, MaxToolCalls: 8},
			want: Config{MaxModelCalls: 3, MaxToolCalls: 4},
		},
		{
			name: "a zero limit contributes no constraint",
			a:    Config{MaxTotalTokens: 100},
			b:    Config{MaxTotalTokens: 0},
			want: Config{MaxTotalTokens: 100},
		},
		{
			name: "a zero limit adopts the other side",
			a:    Config{MaxInputTokens: 0},
			b:    Config{MaxInputTokens: 42},
			want: Config{MaxInputTokens: 42},
		},
		{
			name: "the unspecified scope loses",
			a:    Config{Scope: ScopeLifetime},
			b:    Config{Scope: ScopeUnspecified},
			want: Config{Scope: ScopeLifetime},
		},
		{
			name: "the forward-looking scope wins a disagreement",
			a:    Config{Scope: ScopeLifetime},
			b:    Config{Scope: ScopeForwardLooking},
			want: Config{Scope: ScopeForwardLooking},
		},
		{
			name: "an equal scope survives",
			a:    Config{Scope: ScopeForwardLooking},
			b:    Config{Scope: ScopeForwardLooking},
			want: Config{Scope: ScopeForwardLooking},
		},
		{
			name: "two empty configs merge to an empty config",
			a:    Config{},
			b:    Config{},
			want: Config{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MergeBudgetConfigs(tc.a, tc.b)
			if got != tc.want {
				t.Fatalf("MergeBudgetConfigs() = %+v, want %+v", got, tc.want)
			}
			// Config.Merge must agree with the package-level helper.
			if got := tc.a.Merge(tc.b); got != tc.want {
				t.Fatalf("Config.Merge() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestConfigHasLimits pins which configs count as "has a budget".
func TestConfigHasLimits(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want bool
	}{
		{"empty", Config{}, false},
		{"scope only", Config{Scope: ScopeForwardLooking}, false},
		{"model calls", Config{MaxModelCalls: 1}, true},
		{"tool calls", Config{MaxToolCalls: 1}, true},
		{"input tokens", Config{MaxInputTokens: 1}, true},
		{"output tokens", Config{MaxOutputTokens: 1}, true},
		{"total tokens", Config{MaxTotalTokens: 1}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.HasLimits(); got != tc.want {
				t.Fatalf("HasLimits() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestSubagentUsageIsolation pins that subagent spend never counts against
// the root trajectory's budget, that the per-trajectory map and the
// aggregate work, that the lookup fallback is consulted, and that retiring
// the subagents drops their usage.
func TestSubagentUsageIsolation(t *testing.T) {
	m := NewManager(Config{MaxToolCalls: 1, MaxTotalTokens: 10})

	m.RecordSubagentUsage("sub-1", Usage{ToolCalls: 100, TotalTokens: 5000})
	m.RecordSubagentUsage("sub-2", Usage{ToolCalls: 3, TotalTokens: 7})
	m.RecordSubagentUsage("sub-1", Usage{ToolCalls: 1, TotalTokens: 2})

	// The root trajectory's counters are untouched.
	if got := m.Usage(); got != (Usage{}) {
		t.Errorf("Usage() = %+v, want the zero usage (subagent spend must not count)", got)
	}
	if _, ok := m.LimitExhausted(); ok {
		t.Errorf("LimitExhausted() = exhausted, want not exhausted (subagent spend must not count)")
	}

	// The per-trajectory map accumulates.
	got, ok := m.SubagentUsage("sub-1")
	if !ok {
		t.Fatalf("SubagentUsage(\"sub-1\") = not found, want the recorded usage")
	}
	if got.ToolCalls != 101 || got.TotalTokens != 5002 {
		t.Errorf("SubagentUsage(\"sub-1\") = %+v, want 101 tool calls and 5002 total tokens", got)
	}

	// The aggregate sums every subagent.
	all := m.AllSubagentUsage()
	if all.ToolCalls != 104 || all.TotalTokens != 5009 {
		t.Errorf("AllSubagentUsage() = %+v, want 104 tool calls and 5009 total tokens", all)
	}

	// The root trajectory id round-trips.
	m.SetRootTrajectoryID("root-1")
	if got := m.RootTrajectoryID(); got != "root-1" {
		t.Errorf("RootTrajectoryID() = %q, want \"root-1\"", got)
	}

	// The lookup fallback answers for an unknown trajectory.
	m.SetSubagentLookup(func(trajectoryID string) (Usage, bool) {
		if trajectoryID == "sub-3" {
			return Usage{ToolCalls: 9}, true
		}
		return Usage{}, false
	})
	got, ok = m.SubagentUsage("sub-3")
	if !ok {
		t.Fatalf("SubagentUsage(\"sub-3\") = not found, want the lookup's usage")
	}
	if got.ToolCalls != 9 {
		t.Errorf("SubagentUsage(\"sub-3\").ToolCalls = %d, want 9", got.ToolCalls)
	}
	if _, ok := m.SubagentUsage("sub-unknown"); ok {
		t.Errorf("SubagentUsage(\"sub-unknown\") = found, want not found")
	}

	// Retiring the subagents drops their usage.
	m.RetireSubagents()
	if got := m.AllSubagentUsage(); got != (Usage{}) {
		t.Errorf("AllSubagentUsage() = %+v after RetireSubagents, want the zero usage", got)
	}
	if _, ok := m.SubagentUsage("sub-1"); ok {
		t.Errorf("SubagentUsage(\"sub-1\") = found after RetireSubagents, want not found")
	}
}

// TestUsageAddAndSub pins the Usage arithmetic, including the saturating
// subtraction.
func TestUsageAddAndSub(t *testing.T) {
	a := Usage{ModelCalls: 1, ToolCalls: 2, InputTokens: 3, OutputTokens: 4, TotalTokens: 7}
	b := Usage{ModelCalls: 10, ToolCalls: 20, InputTokens: 30, OutputTokens: 40, TotalTokens: 70}

	sum := a.Add(b)
	wantSum := Usage{ModelCalls: 11, ToolCalls: 22, InputTokens: 33, OutputTokens: 44, TotalTokens: 77}
	if sum != wantSum {
		t.Fatalf("Add() = %+v, want %+v", sum, wantSum)
	}

	diff := b.Sub(a)
	wantDiff := Usage{ModelCalls: 9, ToolCalls: 18, InputTokens: 27, OutputTokens: 36, TotalTokens: 63}
	if diff != wantDiff {
		t.Fatalf("Sub() = %+v, want %+v", diff, wantDiff)
	}

	// The subtraction saturates at zero.
	zero := a.Sub(b)
	if zero != (Usage{}) {
		t.Fatalf("Sub() = %+v, want the zero usage (saturated)", zero)
	}

	// Equal snapshots subtract to zero.
	if got := a.Sub(a); got != (Usage{}) {
		t.Fatalf("Sub() of equal snapshots = %+v, want the zero usage", got)
	}
}

// TestScopeParseAndMarshal pins the scope round-trip and the reference's
// rejection message.
func TestScopeParseAndMarshal(t *testing.T) {
	cases := []struct {
		scope Scope
		name  string
	}{
		{ScopeUnspecified, "BUDGET_SCOPE_UNSPECIFIED"},
		{ScopeLifetime, "BUDGET_SCOPE_LIFETIME"},
		{ScopeForwardLooking, "BUDGET_SCOPE_FORWARD_LOOKING"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			text, err := tc.scope.MarshalText()
			if err != nil {
				t.Fatalf("MarshalText() error = %v", err)
			}
			if string(text) != tc.name {
				t.Fatalf("MarshalText() = %q, want %q", text, tc.name)
			}
			back, err := ParseScope(string(text))
			if err != nil {
				t.Fatalf("ParseScope(%q) error = %v", tc.name, err)
			}
			if back != tc.scope {
				t.Fatalf("ParseScope(%q) = %v, want %v", tc.name, back, tc.scope)
			}
			if got := tc.scope.String(); got != tc.name {
				t.Errorf("String() = %q, want %q", got, tc.name)
			}
		})
	}
}

// TestParseScopeRejectsUnknown pins the reference's rejection message.
func TestParseScopeRejectsUnknown(t *testing.T) {
	_, err := ParseScope("BUDGET_SCOPE_NOPE")
	if err == nil {
		t.Fatalf("ParseScope(\"BUDGET_SCOPE_NOPE\") = nil error, want a failure")
	}
	want := "unsupported budget scope: BUDGET_SCOPE_NOPE"
	if err.Error() != want {
		t.Fatalf("ParseScope() error = %q, want %q", err.Error(), want)
	}
}

// TestMarshalTextRejectsUnknownScope pins that an out-of-range scope is
// refused rather than silently rendered as the unspecified scope.
func TestMarshalTextRejectsUnknownScope(t *testing.T) {
	if _, err := Scope(7).MarshalText(); err == nil {
		t.Fatalf("Scope(7).MarshalText() = nil error, want a failure")
	}
}

// TestUpdateConfig pins that the configuration can be replaced at runtime
// and that the counters survive the swap.
func TestUpdateConfig(t *testing.T) {
	m := NewManager(Config{MaxModelCalls: 100})
	m.RecordModelCall()
	m.RecordModelCall()

	m.UpdateConfig(Config{MaxModelCalls: 2})
	if got := m.Config().MaxModelCalls; got != 2 {
		t.Fatalf("Config().MaxModelCalls = %d, want 2", got)
	}
	if _, ok := m.LimitExhausted(); !ok {
		t.Fatalf("LimitExhausted() = not exhausted after the config update, want exhausted")
	}
	if got := m.Usage().ModelCalls; got != 2 {
		t.Fatalf("Usage().ModelCalls = %d, want 2 (the counters survive the update)", got)
	}
}

// TestStopReasonProtoNames pins the wire names of the stop reasons.
func TestStopReasonProtoNames(t *testing.T) {
	cases := []struct {
		reason StopReason
		want   string
	}{
		{StopReasonNone, "STOP_REASON_UNSPECIFIED"},
		{StopReasonMaxModelCallsExceeded, "STOP_REASON_MAX_MODEL_CALLS_EXCEEDED"},
		{StopReasonMaxToolCallsExceeded, "STOP_REASON_MAX_TOOL_CALLS_EXCEEDED"},
		{StopReasonMaxInputTokensExceeded, "STOP_REASON_MAX_INPUT_TOKENS_EXCEEDED"},
		{StopReasonMaxOutputTokensExceeded, "STOP_REASON_MAX_OUTPUT_TOKENS_EXCEEDED"},
		{StopReasonMaxTotalTokensExceeded, "STOP_REASON_MAX_TOTAL_TOKENS_EXCEEDED"},
		{StopReasonQuotaExhausted, "STOP_REASON_QUOTA_EXHAUSTED"},
	}
	for _, tc := range cases {
		if got := tc.reason.ProtoName(); got != tc.want {
			t.Errorf("ProtoName() = %q, want %q", got, tc.want)
		}
	}
	if got := StopReason(42).ProtoName(); got != "" {
		t.Errorf("ProtoName() of an unknown stop reason = %q, want the empty string", got)
	}
}

// TestTokenBudgetHookNotice pins the reference's hook log line.
func TestTokenBudgetHookNotice(t *testing.T) {
	used := Usage{InputTokens: 10, OutputTokens: 20, TotalTokens: 30}
	limit := Usage{InputTokens: 100, OutputTokens: 200, TotalTokens: 300}
	want := "[TokenBudgetHook] Token spend reached budget (input=10/100, output=20/200, total=30/300). Halting turn."
	if got := TokenBudgetHookNotice(used, limit); got != want {
		t.Fatalf("TokenBudgetHookNotice() = %q, want %q", got, want)
	}
}

// TestConcurrentRecording hammers the recorder from many goroutines and
// asserts the exact totals — the mutex must not lose a single count.
//
// Run with -race to check the locking.
func TestConcurrentRecording(t *testing.T) {
	const (
		goroutines = 8
		iterations = 500
	)

	m := NewManager(Config{})
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				m.RecordModelCall()
				m.RecordToolCall()
				m.RecordUsage(2, 3)
				_ = m.Usage()
				_ = m.WindowUsage()
				_, _ = m.LimitExhausted()
			}
		}()
	}
	wg.Wait()

	total := uint64(goroutines * iterations)
	if got := m.Usage().ModelCalls; got != total {
		t.Errorf("Usage().ModelCalls = %d, want %d", got, total)
	}
	if got := m.Usage().ToolCalls; got != total {
		t.Errorf("Usage().ToolCalls = %d, want %d", got, total)
	}
	if got := m.Usage().InputTokens; got != 2*total {
		t.Errorf("Usage().InputTokens = %d, want %d", got, 2*total)
	}
	if got := m.Usage().OutputTokens; got != 3*total {
		t.Errorf("Usage().OutputTokens = %d, want %d", got, 3*total)
	}
	if got := m.Usage().TotalTokens; got != 5*total {
		t.Errorf("Usage().TotalTokens = %d, want %d", got, 5*total)
	}
}

// TestConcurrentSubagentRecording hammers the subagent recorder as well.
func TestConcurrentSubagentRecording(t *testing.T) {
	const (
		goroutines = 8
		iterations = 100
	)

	m := NewManager(Config{})
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				m.RecordSubagentUsage(fmt.Sprintf("sub-%d", id), Usage{ToolCalls: 1})
			}
		}(g)
	}
	wg.Wait()

	if got := m.AllSubagentUsage().ToolCalls; got != goroutines*iterations {
		t.Fatalf("AllSubagentUsage().ToolCalls = %d, want %d", got, goroutines*iterations)
	}
	if got := m.Usage(); got != (Usage{}) {
		t.Fatalf("Usage() = %+v, want the zero usage (subagent spend must not count)", got)
	}
}
