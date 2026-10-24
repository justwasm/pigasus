# Cycle maxthinkingbudget-strip-via-flag — AGY 1.2.13 `--max-thinking-budget` ≤ 0 gate

## Goal

AGY 1.2.13 closes a UX hazard on the `--max-thinking-budget` flag:

> "Fixed the --max-thinking-budget flag accepting a 0 value, which
> caused infinite thinking loops that locked up the CLI until Ctrl-C;
> values ≤ 0 now disable thinking entirely, matching the documented
> semantics."

Cycle maxthinkingbudget-strip-via-flag wires the same gate into the
clone. The fix is a normalisation layer at the flag boundary: every
caller funnels the parsed `--max-thinking-budget` value through
`internal/cli.ThinkingEnabled()` (or the two-value
`ThinkingBudget()`) before forwarding to the model adapter, and any
value ≤ 0 collapses to "thinking disabled" instead of being passed
through verbatim.

## Changelog anchor

| Release | Line | Shape |
|---|---|---|
| 1.2.13 | 1 | `--max-thinking-budget=0` → infinite loop until Ctrl-C; ≤ 0 now disables thinking |

## Files

| Path | Role |
|---|---|
| `internal/cli/parser_specs.go` (+9 / −0) | `ExtraFlags()` gains a hidden `max-thinking-budget` int flag (Def `0`, Hidden true). The Def `0` deliberately matches the post-fix semantic — the unset default lands in the disabled branch — so users who want thinking on must pass `--max-thinking-budget=N` with `N > 0`. Hidden because the v1.3.5 `--help` visible surface does not list this flag (mirrors the cycle-101 hidden-int treatment of `-v`). |
| `internal/cli/thinking_budget.go` (new, 110 lines) | `ThinkingEnabled(int) bool` (the gate), `ThinkingBudget(int) (enabled, budget int)` (the two-value form so callers can avoid re-applying the rule), `ThinkingBudgetFromFlagSet(*flag.FlagSet) (enabled, budget int)` (the helper that looks up the flag on a parsed stdlib `*flag.FlagSet` and returns the gate verdict). Single source of truth: a future print-path / TUI consumer can call any of the three and cannot drift back to the pre-fix behaviour where a raw `0` reached the model adapter. |
| `internal/cli/thinking_budget_test.go` (new, 230 lines) | 11 tests pinning the gate: zero disables, negative disables, positive enables, minimal `1` keeps thinking on with budget `1`, default flag value unchanged (Def=`0`, type=`int`, hidden=true, registered in `ExtraFlags()`), end-to-end through-parser checks for `=0` / `=-1` / `=100` (each verifies the parsed value reaches the gate), bad-value (`=foo`) surfaces as parse error AND gate still returns disabled, nil `*flag.FlagSet` defensive path, missing-flag defensive path. |

**Net:** +349 lines across 3 files (2 new).

## Wire contract

The flag's parsed value is held as a normal `int` (parsed by
`strconv.Atoi` via the existing `intAtoiValue` wrapper in
`internal/cli/jetski.go`). The post-fix semantic is a pure predicate
at the boundary:

```go
// internal/cli/thinking_budget.go
func ThinkingEnabled(budget int) bool {
    return budget > 0
}

func ThinkingBudget(raw int) (enabled bool, budget int) {
    if raw > 0 {
        return true, raw
    }
    return false, 0
}
```

| User input | `ThinkingEnabled()` | `ThinkingBudget()` | Adapter behaviour |
|---|---|---|---|
| (flag unset, default `0`) | `false` | `(false, 0)` | skip `thinking_budget` field |
| `--max-thinking-budget=0` | `false` | `(false, 0)` | skip `thinking_budget` field (the AGY 1.2.13 fix) |
| `--max-thinking-budget=-1` | `false` | `(false, 0)` | skip `thinking_budget` field |
| `--max-thinking-budget=1` | `true` | `(true, 1)` | set `thinking_budget=1` |
| `--max-thinking-budget=100` | `true` | `(true, 100)` | set `thinking_budget=100` |
| `--max-thinking-budget=foo` | parser exit 2 (`strconv.Atoi`); if gate is asked afterwards, `(false, 0)` | parser exit 2; gate `(false, 0)` | parse error before any adapter call |

The `=foo` row is the failure-mode invariant: a parse error from
`strconv.Atoi` surfaces as exit code 2 with the standard
`invalid value "foo" for flag -max-thinking-budget: strconv.Atoi:
parsing "foo": invalid syntax` message (same shape as the cycle-101
`-v foo` case), and the gate's defensive `nil`-check keeps the
post-parse verdict at `(false, 0)` rather than panicking on a
half-parsed `*flag.FlagSet`.

## Why hidden, not visible

The flag is registered as `Hidden: true`. Two reasons:

1. The v1.3.5 binary's `--help` visible surface (21 flags) does not
   list `--max-thinking-budget`. Mirrors the cycle-101 `-v` flag
   treatment — hidden but parseable, so the parse-parity tests in
   `internal/cli/parser_test.go` (which assert the visible surface
   byte-for-byte) are unaffected.
2. The reference accepts the flag but documents it out-of-band; we
   follow the same shape rather than fabricating visibility that
   would diverge from the recovered binary.

## Tests

```
$ go test -vet=off -count=1 -v -run 'TestThinking|TestBudget' ./internal/cli/
=== RUN   TestThinkingEnabled_ZeroDisables                              --- PASS
=== RUN   TestThinkingEnabled_NegativeDisables                          --- PASS
=== RUN   TestThinkingEnabled_PositiveEnables                           --- PASS
=== RUN   TestThinkingBudget_MinimalOne                                 --- PASS
=== RUN   TestThinkingBudget_DefaultUnchanged                           --- PASS
=== RUN   TestThinkingBudgetFromFlagSet_ZeroThroughParser               --- PASS
=== RUN   TestThinkingBudgetFromFlagSet_NegativeThroughParser           --- PASS
=== RUN   TestThinkingBudgetFromFlagSet_PositiveThroughParser           --- PASS
=== RUN   TestThinkingBudgetFromFlagSet_BadValueDisabled                --- PASS
=== RUN   TestThinkingBudgetFromFlagSet_NilFlagSet                      --- PASS
=== RUN   TestThinkingBudgetFromFlagSet_MissingFlag                     --- PASS
PASS
```

All 11 pass. The five mandated cases map to:

| Required case | Test |
|---|---|
| `--max-thinking-budget=0` disables thinking | `TestThinkingBudgetFromFlagSet_ZeroThroughParser` (+ `TestThinkingEnabled_ZeroDisables` at the unit level) |
| `--max-thinking-budget=-1` disables thinking | `TestThinkingBudgetFromFlagSet_NegativeThroughParser` (+ `TestThinkingEnabled_NegativeDisables`) |
| `--max-thinking-budget=100` works normally | `TestThinkingBudgetFromFlagSet_PositiveThroughParser` (+ `TestThinkingEnabled_PositiveEnables`) |
| `--max-thinking-budget=1` minimal | `TestThinkingBudget_MinimalOne` |
| Default flag value unchanged | `TestThinkingBudget_DefaultUnchanged` |

Six additional defensive tests cover the bad-value / nil /
missing-flag paths so callers cannot regress on the post-parse
verdict.

## Build & test commands

```
$ go build -o /tmp/wagyu-mtb ./cmd/wagyu                  # exit 0
$ gofmt -l internal/cli/thinking_budget.go                # (empty)
        internal/cli/thinking_budget_test.go
        internal/cli/parser_specs.go
$ go test -vet=off -count=1 \
      -run 'TestThinking|TestBudget|TestFlag|TestMax' \
      ./cmd/wagyu/ ./internal/.../                        # PASS
```

No tests outside the targeted run were touched; the existing
`internal/cli` test suite (28 tests across `parser_test.go`,
`close_gap_test.go`, `print_args_test.go`, `duration_flag_test.go`,
…) still passes:

```
$ go test -vet=off -count=1 ./internal/cli/                # ok 20.868s
```

## 硬约束 / Limitations

- The gate is implemented at the parser flag boundary; the
  downstream consumer that would actually forward the budget to the
  model adapter is **not** in this cycle. There is no
  `thinking_budget` field on the wire contract in `internal/agent/`
  or `internal/piagentbridge/` today — the flag exists and the gate
  works, but no caller yet wires the gate output into a request
  payload. A future cycle that adds the actual model-adapter
  plumbing (e.g. cycle maxthinkingbudget-wire) will consume
  `ThinkingBudgetFromFlagSet` and translate `(true, N)` into
  `request.ThinkingBudget = N`. Until then the gate is enforced on
  every parse but the wire stays untouched.
- The pre-existing int-default bug in `internal/cli/jetski.go`
  `newDestFor()` (the `n := 0` shadowing means any non-empty `Def`
  is validated but not applied — verified with a 20-line harness)
  is **not** fixed here. The bug is harmless for this flag because
  the Def is `0` (which is what we want), and touching the int
  default branch is out of scope. A future cycle that registers
  another int flag with a non-zero default will need to fix the
  missing `n, _ = strconv.Atoi(def)` assignment before that flag
  works.
- `internal/cli/parser_specs.go` is not on the AGENTS.md
  "pre-existing not-clean" list (`gofmt -l` reports it clean), so
  the targeted edit (one `FlagSpec` line + an 8-line comment) is
  in-spec. `cmd/wagyu/picker_view.go`, `cmd/wagyu/tui_v135_test.go`,
  and the ~50 internal files that are intentionally gofmt-dirty
  were not touched.
- The flag is `Hidden: true`. If AGY ships a visible
  `--max-thinking-budget` row in a later release's `--help` (e.g.
  1.2.14 or 1.3.x), this cycle's `ExtraFlags()` registration needs
  to move into `StandardFlags()` to match — out of scope here.
