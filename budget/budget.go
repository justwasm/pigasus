// Package budget re-implements the Antigravity localharness budget manager:
// the per-session limits on model calls, tool calls and tokens, the scope
// window a limit is evaluated against, the subagent usage that is kept
// outside the root trajectory's budget, and the stop reason a trajectory
// reports when a limit is exhausted.
//
// Provenance — Google's Antigravity `localharness` binary (Go,
// `google3/third_party/jetski_prod/localharness/localagent`). The evidence
// lives in `out/localharness/`:
//
//   - 【一手】 The wire contract. The recovered proto at
//     `out/localharness/proto/localharness.proto` declares `BudgetConfig`
//     with the fields `max_model_calls` (1, uint32), `max_tool_calls`
//     (2, uint32), `max_input_tokens` (3, uint64), `max_output_tokens`
//     (4, uint64), `max_total_tokens` (5, uint64) and `scope` (6), and the
//     enum `BudgetScope { BUDGET_SCOPE_UNSPECIFIED = 0,
//     BUDGET_SCOPE_LIFETIME = 1, BUDGET_SCOPE_FORWARD_LOOKING = 2 }`.
//     The stop reasons come from `TrajectoryStateUpdate.StopReason`:
//     `STOP_REASON_UNSPECIFIED = 0`,
//     `STOP_REASON_MAX_MODEL_CALLS_EXCEEDED = 1`,
//     `STOP_REASON_MAX_TOOL_CALLS_EXCEEDED = 2`,
//     `STOP_REASON_MAX_INPUT_TOKENS_EXCEEDED = 3`,
//     `STOP_REASON_MAX_OUTPUT_TOKENS_EXCEEDED = 4`,
//     `STOP_REASON_MAX_TOTAL_TOKENS_EXCEEDED = 5` and
//     `STOP_REASON_QUOTA_EXHAUSTED = 6`.
//
//   - 【反编译】 `exhaustedLimitStopReason` (0x101946130,
//     `out/localharness/dec/101946130_exhaustedLimitStopReason.c`) fixes the
//     limit semantics: every limit check is guarded by `0 < limit` (a limit of
//     zero means "not set"), a limit is exhausted when `limit <= used`, and
//     the usage snapshot it compares against is laid out as
//     {model calls, tool calls, input tokens, output tokens, total tokens}.
//
//   - 【反编译】 `stopReasonForUsage` (0x101962d60) adds stop reason 6
//     (`STOP_REASON_QUOTA_EXHAUSTED`) for provider-side quota errors and
//     shows the per-mode checks the reference performs.
//
//   - 【反编译】 `translateBudgetScope` (0x101952820) maps the three scope
//     values and rejects anything else with "unsupported budget scope: %v".
//
//   - 【反编译】 `MergeBudgetConfigs` (0x1019465d0) merges two BudgetConfigs
//     field by field using the proto presence bitmap.
//
//   - 【反编译】 `(*BudgetManager).usage` (0x101945b80) resolves the
//     evaluation window from the scope and subtracts the baseline with a
//     clamp at zero.
//
//   - 【strings】 The user-facing strings are verbatim string-table hits:
//     "Input token budget exceeded (limit: %d). Do not emit more tool calls.",
//     "Output token budget exceeded (limit: %d). Do not emit more tool calls.",
//     "Total token budget exceeded (limit: %d). Do not emit more tool calls.",
//     "Tool call budget exceeded (limit: %d). Do not emit more tool calls.",
//     "Model call budget exceeded (limit: %d). Do not emit more tool calls.",
//     "[TokenBudgetHook] Token spend reached budget (input=%d/%d,
//     output=%d/%d, total=%d/%d). Halting turn.",
//     "unsupported budget scope: %v" and "cannot set both budget_config and
//     top-level max_total_tokens or max_tool_calls".
//
// The upstream API surface that this clone mirrors — recovered from
// `out/localharness/dec/` — is `NewBudgetManagerWithProvider`,
// `(*BudgetManager).Config`, `.Baseline`, `.SetBaseline`, `.UpdateConfig`,
// `.ResolveEvaluationWindow`, `.SetSubagentLookup`, `.RetireSubagents`,
// `.allSubagents`, `.subagentUsage`, `.usage`, `.StopReason`,
// `.LimitExhausted`, `.HasBudget`, `.RootTrajectoryID` and the package-level
// `MergeBudgetConfigs` / `resolveEvaluationWindow`.
//
// The recovered proto message this package mirrors:
//
//	message BudgetConfig {
//	  uint32 max_model_calls  = 1;
//	  uint32 max_tool_calls   = 2;
//	  uint64 max_input_tokens = 3;
//	  uint64 max_output_tokens = 4;
//	  uint64 max_total_tokens = 5;
//	  BudgetScope scope       = 6;
//	}
//
// 硬约束 / Hard constraints:
//
//   - A limit of 0 is "not set": it never triggers a violation, in any scope.
//   - A limit is exhausted when the evaluated usage is **greater than or
//     equal to** the limit (`limit <= used`), matching the reference's
//     comparison.
//   - Subagent spend never counts against the root trajectory's budget.
//     `RecordSubagentUsage` only touches the subagent accounting.
//
// Deliberate divergences from the reference (chosen for this clone, and
// documented rather than silently applied):
//
//   - The clone checks the limits in the order model calls → tool calls →
//     input tokens → output tokens → total tokens. The binary's
//     `exhaustedLimitStopReason` checks output tokens *before* input tokens.
//     The clone's order is the one the local contract pins; see 存疑 below.
//   - `MergeBudgetConfigs` keeps the **tighter** (smaller non-zero) limit.
//     The binary's merge walks the proto presence bitmap and lets the second
//     config override the first field by field. See 存疑 below.
//
// 存疑 / Open questions:
//
//   - The binary checks the output-token limit before the input-token limit
//     (0x101946130, offsets +0x28 before +0x20). This clone reports the
//     input-token limit first. The two only differ when *both* token limits
//     are exhausted at the same instant; the binary's order is the
//     【反编译】 fact, the clone's order is the local contract.
//   - The binary's merge is an override, not a minimum. Nothing in the
//     recovered strings says which of the two is intended upstream.
//
// limitations:
//
//   - The reference resolves the evaluation window from a trajectory
//     provider (per-step usage snapshots); this clone counts flat counters,
//     so the "forward looking" window is a counter reset rather than a step
//     range.
//   - The reference's `STOP_REASON_QUOTA_EXHAUSTED` is raised from a
//     provider quota error, not from a budget limit. This package exposes
//     the constant but never produces it: nothing here talks to a model
//     API.
package budget

import (
	"fmt"
	"sync"
)

// Scope selects which usage window a limit is evaluated against.
//
// The three values mirror the `BudgetConfig.BudgetScope` enum of the
// recovered proto (【一手】): unspecified (0), lifetime (1) and
// forward-looking (2).
type Scope int

const (
	// ScopeUnspecified is the zero value. It behaves like ScopeLifetime:
	// the whole lifetime of the manager is evaluated.
	ScopeUnspecified Scope = iota
	// ScopeLifetime evaluates the limit against everything spent since the
	// manager's baseline was taken.
	ScopeLifetime
	// ScopeForwardLooking evaluates the limit against the usage recorded
	// since the last NewWindow call — the budget re-opens on every window.
	ScopeForwardLooking
)

// String returns the scope's proto enum name. An out-of-range scope renders
// as "BUDGET_SCOPE(<n>)" so that a bad value is still readable in a log
// line.
//
// 【推测】 The reference has no such helper — its scope is an int32 that it
// formats with %v — so the exact rendering of an unknown value is this
// clone's choice.
func (s Scope) String() string {
	switch s {
	case ScopeUnspecified:
		return "BUDGET_SCOPE_UNSPECIFIED"
	case ScopeLifetime:
		return "BUDGET_SCOPE_LIFETIME"
	case ScopeForwardLooking:
		return "BUDGET_SCOPE_FORWARD_LOOKING"
	}
	return fmt.Sprintf("BUDGET_SCOPE(%d)", int(s))
}

// MarshalText renders the scope as its proto enum name, so that a Config
// survives a JSON/TOML round-trip through encoding.TextMarshaler.
//
// It fails for a scope outside the recovered enum with the reference's
// rejection message ("unsupported budget scope: %v", 【strings】).
func (s Scope) MarshalText() ([]byte, error) {
	switch s {
	case ScopeUnspecified:
		return []byte("BUDGET_SCOPE_UNSPECIFIED"), nil
	case ScopeLifetime:
		return []byte("BUDGET_SCOPE_LIFETIME"), nil
	case ScopeForwardLooking:
		return []byte("BUDGET_SCOPE_FORWARD_LOOKING"), nil
	}
	return nil, fmt.Errorf("unsupported budget scope: %v", int(s))
}

// ParseScope parses a proto enum name back into a Scope. It accepts exactly
// the three names MarshalText produces and rejects everything else with the
// reference's rejection message ("unsupported budget scope: %v",
// 【strings】).
//
// The comparison is exact and case-sensitive: the recovered enum names are
// upper-case, and the reference never parses a scope from a string.
func ParseScope(s string) (Scope, error) {
	switch s {
	case "BUDGET_SCOPE_UNSPECIFIED":
		return ScopeUnspecified, nil
	case "BUDGET_SCOPE_LIFETIME":
		return ScopeLifetime, nil
	case "BUDGET_SCOPE_FORWARD_LOOKING":
		return ScopeForwardLooking, nil
	}
	return ScopeUnspecified, fmt.Errorf("unsupported budget scope: %v", s)
}

// Config is the budget configuration. It mirrors the recovered
// `BudgetConfig` proto message field for field (【一手】).
//
// A limit of 0 means "not set": it constrains nothing, in any scope.
type Config struct {
	// MaxModelCalls caps the number of model calls.
	MaxModelCalls uint32
	// MaxToolCalls caps the number of tool calls.
	MaxToolCalls uint32
	// MaxInputTokens caps the accumulated input tokens.
	MaxInputTokens uint64
	// MaxOutputTokens caps the accumulated output tokens.
	MaxOutputTokens uint64
	// MaxTotalTokens caps the accumulated input + output tokens.
	MaxTotalTokens uint64
	// Scope selects the usage window the limits are evaluated against.
	Scope Scope
}

// HasLimits reports whether any limit is set. A config with no limits
// never produces a violation.
//
// 【反编译】 `(*BudgetManager).HasBudget` (0x101946270) answers the same
// question against the stored proto config.
func (c Config) HasLimits() bool {
	return c.MaxModelCalls != 0 ||
		c.MaxToolCalls != 0 ||
		c.MaxInputTokens != 0 ||
		c.MaxOutputTokens != 0 ||
		c.MaxTotalTokens != 0
}

// Merge merges other into c and returns the result: for every limit the
// tighter (smaller non-zero) value wins, and a zero on either side
// contributes no constraint.
//
// The scope of the result is the non-unspecified scope; when both sides set
// a scope and they differ, ScopeForwardLooking wins (it is the stricter
// window).
//
// See MergeBudgetConfigs for the semantics and for the divergence from the
// reference's presence-bitmap merge.
func (c Config) Merge(other Config) Config {
	return MergeBudgetConfigs(c, other)
}

// MergeBudgetConfigs merges two budget configs into the tighter of the two.
//
// For every limit field the smaller non-zero value wins — a zero means "not
// set" and therefore contributes no constraint. The scope of the result is
// the non-unspecified scope; when both sides set a scope and they differ,
// ScopeForwardLooking wins, because a forward-looking window is the
// stricter of the two.
//
// 【反编译】 The reference's `MergeBudgetConfigs` (0x1019465d0) merges
// differently: it walks the proto presence bitmap and lets the second
// config override the first field by field (so a *larger* limit in the
// second config wins). This clone keeps the tighter budget instead — see
// 存疑 in the package doc.
func MergeBudgetConfigs(a, b Config) Config {
	return Config{
		MaxModelCalls:   minLimit32(a.MaxModelCalls, b.MaxModelCalls),
		MaxToolCalls:    minLimit32(a.MaxToolCalls, b.MaxToolCalls),
		MaxInputTokens:  minLimit64(a.MaxInputTokens, b.MaxInputTokens),
		MaxOutputTokens: minLimit64(a.MaxOutputTokens, b.MaxOutputTokens),
		MaxTotalTokens:  minLimit64(a.MaxTotalTokens, b.MaxTotalTokens),
		Scope:           mergeScope(a.Scope, b.Scope),
	}
}

// minLimit32 returns the tighter of two limits, treating 0 as "not set".
func minLimit32(a, b uint32) uint32 {
	switch {
	case a == 0:
		return b
	case b == 0:
		return a
	case a < b:
		return a
	default:
		return b
	}
}

// minLimit64 returns the tighter of two limits, treating 0 as "not set".
func minLimit64(a, b uint64) uint64 {
	switch {
	case a == 0:
		return b
	case b == 0:
		return a
	case a < b:
		return a
	default:
		return b
	}
}

// mergeScope picks the scope of a merged config: the non-unspecified one
// wins, and a disagreement between lifetime and forward-looking resolves to
// forward-looking (the stricter window).
func mergeScope(a, b Scope) Scope {
	switch {
	case a == b:
		return a
	case a == ScopeUnspecified:
		return b
	case b == ScopeUnspecified:
		return a
	}
	return ScopeForwardLooking
}

// Usage is a usage snapshot: the counters a limit is compared against.
//
// The field order matches the usage snapshot the reference compares against
// in `exhaustedLimitStopReason` (【反编译】): model calls, tool calls, input
// tokens, output tokens, total tokens.
type Usage struct {
	// ModelCalls is the number of model calls.
	ModelCalls uint64
	// ToolCalls is the number of tool calls.
	ToolCalls uint64
	// InputTokens is the accumulated input token count.
	InputTokens uint64
	// OutputTokens is the accumulated output token count.
	OutputTokens uint64
	// TotalTokens is the accumulated total token count.
	TotalTokens uint64
}

// Add returns the sum of u and other, field by field. The total tokens are
// added as they are; Add never recomputes them from the input and output
// counters.
func (u Usage) Add(other Usage) Usage {
	return Usage{
		ModelCalls:   u.ModelCalls + other.ModelCalls,
		ToolCalls:    u.ToolCalls + other.ToolCalls,
		InputTokens:  u.InputTokens + other.InputTokens,
		OutputTokens: u.OutputTokens + other.OutputTokens,
		TotalTokens:  u.TotalTokens + other.TotalTokens,
	}
}

// Sub returns u minus other, field by field, saturating at zero: a counter
// that would go negative is reported as 0.
//
// The saturation is what makes a baseline larger than the usage it is
// subtracted from safe — the reference clamps the same subtraction at zero
// (【反编译】 `(*BudgetManager).usage`, 0x101945b80).
func (u Usage) Sub(other Usage) Usage {
	return Usage{
		ModelCalls:   subCounter(u.ModelCalls, other.ModelCalls),
		ToolCalls:    subCounter(u.ToolCalls, other.ToolCalls),
		InputTokens:  subCounter(u.InputTokens, other.InputTokens),
		OutputTokens: subCounter(u.OutputTokens, other.OutputTokens),
		TotalTokens:  subCounter(u.TotalTokens, other.TotalTokens),
	}
}

// subCounter subtracts b from a, saturating at zero.
func subCounter(a, b uint64) uint64 {
	if a <= b {
		return 0
	}
	return a - b
}

// LimitKind identifies which limit was exhausted.
type LimitKind int

const (
	// LimitNone means no limit was exhausted.
	LimitNone LimitKind = iota
	// LimitModelCalls is the model-call limit.
	LimitModelCalls
	// LimitToolCalls is the tool-call limit.
	LimitToolCalls
	// LimitInputTokens is the input-token limit.
	LimitInputTokens
	// LimitOutputTokens is the output-token limit.
	LimitOutputTokens
	// LimitTotalTokens is the total-token limit.
	LimitTotalTokens
)

// String returns the snake-case name of the limit kind.
//
// 【推测】 The reference has no such helper: it reports the limit through
// the "… budget exceeded (limit: %d)." strings and the stop reason. The
// names here mirror the proto field names (max_model_calls →
// "model_calls").
func (k LimitKind) String() string {
	switch k {
	case LimitNone:
		return "none"
	case LimitModelCalls:
		return "model_calls"
	case LimitToolCalls:
		return "tool_calls"
	case LimitInputTokens:
		return "input_tokens"
	case LimitOutputTokens:
		return "output_tokens"
	case LimitTotalTokens:
		return "total_tokens"
	}
	return "unknown"
}

// StopReason mirrors `TrajectoryStateUpdate.StopReason` (【一手】).
type StopReason int

const (
	// StopReasonNone means no budget limit was exhausted.
	StopReasonNone StopReason = iota
	// StopReasonMaxModelCallsExceeded means the model-call limit was hit.
	StopReasonMaxModelCallsExceeded
	// StopReasonMaxToolCallsExceeded means the tool-call limit was hit.
	StopReasonMaxToolCallsExceeded
	// StopReasonMaxInputTokensExceeded means the input-token limit was hit.
	StopReasonMaxInputTokensExceeded
	// StopReasonMaxOutputTokensExceeded means the output-token limit was hit.
	StopReasonMaxOutputTokensExceeded
	// StopReasonMaxTotalTokensExceeded means the total-token limit was hit.
	StopReasonMaxTotalTokensExceeded
	// StopReasonQuotaExhausted means the *provider* ran out of quota. It is
	// not a budget limit: the reference raises it from a quota error
	// (【反编译】 `stopReasonForUsage`, 0x101962d60), never from a budget.
	StopReasonQuotaExhausted
)

// String returns the snake-case name of the stop reason.
//
// 【推测】 The reference has no such helper — the stop reason travels over
// the wire as its proto enum name, which ProtoName returns.
func (r StopReason) String() string {
	switch r {
	case StopReasonNone:
		return "unspecified"
	case StopReasonMaxModelCallsExceeded:
		return "max_model_calls_exceeded"
	case StopReasonMaxToolCallsExceeded:
		return "max_tool_calls_exceeded"
	case StopReasonMaxInputTokensExceeded:
		return "max_input_tokens_exceeded"
	case StopReasonMaxOutputTokensExceeded:
		return "max_output_tokens_exceeded"
	case StopReasonMaxTotalTokensExceeded:
		return "max_total_tokens_exceeded"
	case StopReasonQuotaExhausted:
		return "quota_exhausted"
	}
	return "unknown"
}

// ProtoName returns the wire name of the stop reason, e.g.
// "STOP_REASON_MAX_MODEL_CALLS_EXCEEDED". An out-of-range stop reason has
// no wire name and returns "".
func (r StopReason) ProtoName() string {
	switch r {
	case StopReasonNone:
		return "STOP_REASON_UNSPECIFIED"
	case StopReasonMaxModelCallsExceeded:
		return "STOP_REASON_MAX_MODEL_CALLS_EXCEEDED"
	case StopReasonMaxToolCallsExceeded:
		return "STOP_REASON_MAX_TOOL_CALLS_EXCEEDED"
	case StopReasonMaxInputTokensExceeded:
		return "STOP_REASON_MAX_INPUT_TOKENS_EXCEEDED"
	case StopReasonMaxOutputTokensExceeded:
		return "STOP_REASON_MAX_OUTPUT_TOKENS_EXCEEDED"
	case StopReasonMaxTotalTokensExceeded:
		return "STOP_REASON_MAX_TOTAL_TOKENS_EXCEEDED"
	case StopReasonQuotaExhausted:
		return "STOP_REASON_QUOTA_EXHAUSTED"
	}
	return ""
}

// Violation describes an exhausted budget limit.
type Violation struct {
	// Kind is the limit that was exhausted.
	Kind LimitKind
	// Limit is the configured limit that was hit.
	Limit uint64
	// Used is the usage that was measured against the limit.
	Used uint64
	// Scope is the scope the limit was evaluated in.
	Scope Scope
}

// Error renders the violation with the reference's verbatim wording
// (【strings】). The %d is the limit, not the usage.
func (v Violation) Error() string {
	switch v.Kind {
	case LimitModelCalls:
		return fmt.Sprintf("Model call budget exceeded (limit: %d). Do not emit more tool calls.", v.Limit)
	case LimitToolCalls:
		return fmt.Sprintf("Tool call budget exceeded (limit: %d). Do not emit more tool calls.", v.Limit)
	case LimitInputTokens:
		return fmt.Sprintf("Input token budget exceeded (limit: %d). Do not emit more tool calls.", v.Limit)
	case LimitOutputTokens:
		return fmt.Sprintf("Output token budget exceeded (limit: %d). Do not emit more tool calls.", v.Limit)
	case LimitTotalTokens:
		return fmt.Sprintf("Total token budget exceeded (limit: %d). Do not emit more tool calls.", v.Limit)
	}
	// 【推测】 The reference never renders a limit-less violation; this
	// clone keeps the sentence shape for the degenerate case.
	return fmt.Sprintf("Budget limit exceeded (limit: %d). Do not emit more tool calls.", v.Limit)
}

// StopReason returns the stop reason the violation maps to.
func (v Violation) StopReason() StopReason {
	switch v.Kind {
	case LimitModelCalls:
		return StopReasonMaxModelCallsExceeded
	case LimitToolCalls:
		return StopReasonMaxToolCallsExceeded
	case LimitInputTokens:
		return StopReasonMaxInputTokensExceeded
	case LimitOutputTokens:
		return StopReasonMaxOutputTokensExceeded
	case LimitTotalTokens:
		return StopReasonMaxTotalTokensExceeded
	}
	return StopReasonNone
}

// Manager tracks usage against a Config and reports when a limit is
// exhausted.
//
// The manager keeps three sets of counters (【推测】 the reference keeps the
// same three, but resolves them from a trajectory provider instead of flat
// counters):
//
//   - cumulative: everything the manager has recorded;
//   - window: everything recorded since the last NewWindow call;
//   - baseline: the reference point the lifetime window is measured from.
//
// The lifetime window is cumulative - baseline, the forward-looking window
// is the window counters. Subagent usage is accounted separately and never
// counts against the root trajectory.
//
// A Manager is safe for concurrent use: every field is guarded by an
// internal mutex, so the agent loop and a UI goroutine can share one.
type Manager struct {
	mu sync.Mutex

	// cfg is the current configuration.
	cfg Config
	// cumulative counts everything the manager has recorded.
	cumulative Usage
	// window counts everything recorded since the last NewWindow call.
	window Usage
	// baseline is the reference point of the lifetime window.
	baseline Usage
	// rootID is the id of the root trajectory.
	rootID string
	// subagents holds the per-trajectory usage of the subagents.
	subagents map[string]Usage
	// subagentTotal is the sum of every recorded subagent usage.
	subagentTotal Usage
	// lookup is the fallback resolver for subagent usage.
	lookup func(trajectoryID string) (Usage, bool)
}

// NewManager returns a Manager that tracks usage against cfg.
//
// The lifetime window starts empty (the baseline is zero) and the
// forward-looking window starts empty as well.
func NewManager(cfg Config) *Manager {
	return &Manager{
		cfg:       cfg,
		subagents: make(map[string]Usage),
	}
}

// NewManagerWithBaseline returns a Manager whose lifetime window starts
// empty: the given baseline is adopted as the manager's reference point, so
// the lifetime window counts only the spend recorded after construction.
//
// The baseline is an absolute snapshot of the usage the budget is measured
// from, so Usage() reports the baseline plus everything recorded since —
// the same coordinate system SetBaseline uses.
//
// 【推测】 The reference constructs its manager with a trajectory provider
// and takes the baseline later through SetBaseline; this constructor is the
// clone's way of expressing "a budget that starts now, on top of usage that
// already happened".
func NewManagerWithBaseline(cfg Config, baseline Usage) *Manager {
	return &Manager{
		cfg:        cfg,
		cumulative: baseline,
		baseline:   baseline,
		subagents:  make(map[string]Usage),
	}
}

// Config returns the manager's current configuration.
func (m *Manager) Config() Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg
}

// UpdateConfig replaces the manager's configuration. The counters, the
// baseline and the subagent accounting are left untouched.
func (m *Manager) UpdateConfig(cfg Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg = cfg
}

// Baseline returns the reference point the lifetime window is measured
// from.
func (m *Manager) Baseline() Usage {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.baseline
}

// SetBaseline sets the reference point the lifetime window is measured
// from. The baseline is an absolute snapshot of the cumulative usage, so
// setting it to the current Usage() restarts the lifetime window.
//
// The forward-looking window is not affected: it is measured from the last
// NewWindow call.
func (m *Manager) SetBaseline(u Usage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.baseline = u
}

// Usage returns the manager's raw cumulative counters — everything the
// manager has recorded.
//
// The lifetime window is Usage().Sub(Baseline()); use WindowUsage for the
// usage the current scope evaluates against.
func (m *Manager) Usage() Usage {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cumulative
}

// WindowUsage returns the usage the current scope evaluates against:
//
//   - ScopeLifetime and ScopeUnspecified: the cumulative usage minus the
//     baseline (everything spent since the baseline was taken);
//   - ScopeForwardLooking: the usage recorded since the last NewWindow call.
//
// The baseline does not apply to the forward-looking window: a
// forward-looking budget re-opens on every window.
func (m *Manager) WindowUsage() Usage {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.windowUsageLocked()
}

// windowUsageLocked computes the scope's window. The caller holds the lock.
func (m *Manager) windowUsageLocked() Usage {
	if m.cfg.Scope == ScopeForwardLooking {
		return m.window
	}
	return m.cumulative.Sub(m.baseline)
}

// NewWindow starts a fresh forward-looking window: the window counters go
// back to zero.
//
// The cumulative counters and the baseline are untouched, so the lifetime
// window keeps growing across windows.
func (m *Manager) NewWindow() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.window = Usage{}
}

// ResetWindow starts a fresh forward-looking window. It is an alias of
// NewWindow and zeroes nothing else — in particular neither the cumulative
// counters nor the baseline.
func (m *Manager) ResetWindow() {
	m.NewWindow()
}

// RecordModelCall records one model call against both the cumulative
// counters and the forward-looking window.
func (m *Manager) RecordModelCall() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cumulative.ModelCalls++
	m.window.ModelCalls++
}

// RecordToolCall records one tool call against both the cumulative counters
// and the forward-looking window.
func (m *Manager) RecordToolCall() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cumulative.ToolCalls++
	m.window.ToolCalls++
}

// RecordUsage records the token spend of one model call against both the
// cumulative counters and the forward-looking window. The total tokens grow
// by input+output.
func (m *Manager) RecordUsage(inputTokens, outputTokens uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cumulative.InputTokens += inputTokens
	m.cumulative.OutputTokens += outputTokens
	m.cumulative.TotalTokens += inputTokens + outputTokens
	m.window.InputTokens += inputTokens
	m.window.OutputTokens += outputTokens
	m.window.TotalTokens += inputTokens + outputTokens
}

// HasBudget reports whether any limit is set. A manager without limits
// never reports a violation.
func (m *Manager) HasBudget() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg.HasLimits()
}

// LimitExhausted evaluates the limits against the current scope's window
// and returns the first exhausted limit.
//
// The limits are checked in a fixed order — model calls, tool calls, input
// tokens, output tokens, total tokens — and the first exhausted one wins.
// A limit of 0 is not set and never triggers.
func (m *Manager) LimitExhausted() (Violation, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.limitExhaustedLocked()
}

// limitExhaustedLocked evaluates the limits. The caller holds the lock.
func (m *Manager) limitExhaustedLocked() (Violation, bool) {
	if !m.cfg.HasLimits() {
		return Violation{}, false
	}
	u := m.windowUsageLocked()
	scope := m.cfg.Scope
	if m.cfg.MaxModelCalls > 0 && u.ModelCalls >= uint64(m.cfg.MaxModelCalls) {
		return Violation{
			Kind:  LimitModelCalls,
			Limit: uint64(m.cfg.MaxModelCalls),
			Used:  u.ModelCalls,
			Scope: scope,
		}, true
	}
	if m.cfg.MaxToolCalls > 0 && u.ToolCalls >= uint64(m.cfg.MaxToolCalls) {
		return Violation{
			Kind:  LimitToolCalls,
			Limit: uint64(m.cfg.MaxToolCalls),
			Used:  u.ToolCalls,
			Scope: scope,
		}, true
	}
	if m.cfg.MaxInputTokens > 0 && u.InputTokens >= m.cfg.MaxInputTokens {
		return Violation{
			Kind:  LimitInputTokens,
			Limit: m.cfg.MaxInputTokens,
			Used:  u.InputTokens,
			Scope: scope,
		}, true
	}
	if m.cfg.MaxOutputTokens > 0 && u.OutputTokens >= m.cfg.MaxOutputTokens {
		return Violation{
			Kind:  LimitOutputTokens,
			Limit: m.cfg.MaxOutputTokens,
			Used:  u.OutputTokens,
			Scope: scope,
		}, true
	}
	if m.cfg.MaxTotalTokens > 0 && u.TotalTokens >= m.cfg.MaxTotalTokens {
		return Violation{
			Kind:  LimitTotalTokens,
			Limit: m.cfg.MaxTotalTokens,
			Used:  u.TotalTokens,
			Scope: scope,
		}, true
	}
	return Violation{}, false
}

// StopReason returns the stop reason for the exhausted limit, or
// StopReasonNone when no limit is exhausted.
func (m *Manager) StopReason() StopReason {
	v, ok := m.LimitExhausted()
	if !ok {
		return StopReasonNone
	}
	return v.StopReason()
}

// Check returns the violation as an error when a limit is exhausted, and
// nil otherwise. The returned error is a Violation value, so a caller can
// type-assert it back to budget.Violation.
func (m *Manager) Check() error {
	v, ok := m.LimitExhausted()
	if !ok {
		return nil
	}
	return v
}

// SetRootTrajectoryID records the id of the root trajectory.
func (m *Manager) SetRootTrajectoryID(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rootID = id
}

// RootTrajectoryID returns the id of the root trajectory, or "" when none
// was set.
func (m *Manager) RootTrajectoryID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rootID
}

// RecordSubagentUsage records the usage a subagent trajectory produced.
//
// The usage is accounted per trajectory and in the subagent aggregate only:
// it never counts against the root trajectory's budget, because a
// subagent's spend is not the root trajectory's spend.
//
// 【反编译】 The reference tracks subagent usage separately too
// (`(*BudgetManager).subagentUsage` / `.allSubagents`, 0x1019456e0 /
// 0x101945340) and keeps it out of the root trajectory's counters.
func (m *Manager) RecordSubagentUsage(trajectoryID string, u Usage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.subagents == nil {
		m.subagents = make(map[string]Usage)
	}
	m.subagents[trajectoryID] = m.subagents[trajectoryID].Add(u)
	m.subagentTotal = m.subagentTotal.Add(u)
}

// SubagentUsage returns the usage recorded for a subagent trajectory.
//
// The lookup function installed with SetSubagentLookup is consulted when the
// manager has no entry for the trajectory; the second result reports
// whether the usage is known at all.
func (m *Manager) SubagentUsage(trajectoryID string) (Usage, bool) {
	m.mu.Lock()
	u, ok := m.subagents[trajectoryID]
	lookup := m.lookup
	m.mu.Unlock()
	if ok {
		return u, true
	}
	if lookup != nil {
		return lookup(trajectoryID)
	}
	return Usage{}, false
}

// AllSubagentUsage returns the sum of every recorded subagent usage. It
// never includes the root trajectory's own usage.
func (m *Manager) AllSubagentUsage() Usage {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.subagentTotal
}

// RetireSubagents drops the recorded subagent usage: the per-trajectory
// entries go away and the aggregate returns to zero.
//
// 【推测】 The reference's RetireSubagents takes the list of trajectory ids
// to retire (0x101945140); the clone's takes none and retires every
// subagent it knows about.
func (m *Manager) RetireSubagents() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subagents = make(map[string]Usage)
	m.subagentTotal = Usage{}
}

// SetSubagentLookup installs a fallback resolver for subagent usage. It is
// consulted by SubagentUsage when the manager has no entry for the
// trajectory.
//
// The resolver is called without the manager's lock held, so it may call
// back into the manager. A nil resolver removes the fallback.
func (m *Manager) SetSubagentLookup(fn func(trajectoryID string) (Usage, bool)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lookup = fn
}

// TokenBudgetHookFormat is the reference's token-budget hook log line
// (【strings】). TokenBudgetHookNotice fills it in.
const TokenBudgetHookFormat = "[TokenBudgetHook] Token spend reached budget (input=%d/%d, output=%d/%d, total=%d/%d). Halting turn."

// TokenBudgetHookNotice renders the reference's token-budget hook log line
// for the given usage and limit.
//
// 【strings】 The format is verbatim from the binary; the numbers are
// (used, limit) for the input, output and total token counters.
func TokenBudgetHookNotice(used, limit Usage) string {
	return fmt.Sprintf(TokenBudgetHookFormat,
		used.InputTokens, limit.InputTokens,
		used.OutputTokens, limit.OutputTokens,
		used.TotalTokens, limit.TotalTokens)
}

// ConflictingBudgetFieldsMessage is the reference's verbatim rejection
// message for a request that sets the budget both inside `budget_config`
// and through the top-level `max_total_tokens` / `max_tool_calls` fields
// (【strings】).
const ConflictingBudgetFieldsMessage = "cannot set both budget_config and top-level max_total_tokens or max_tool_calls"
