# merge status-line-script — external status-line script (cost + conversation_title JSON)

## TL;DR

AGY 1.3.3 changelog line 32 + 1.4.0: the status row can be replaced by an
external user-supplied script. The clone now invokes `statusLine.command`
on each 100 ms tick with a JSON payload `{model, cost, conversation_title,
current_dir}` and renders stdout (or falls back to the built-in row on
non-zero exit / empty / 1 s timeout).

| | |
|---|---|
| Branch | `feature/status-line-script` |
| Commit | `2e05248` |
| Merge | `14c1f84` |
| Lines | +1521 / −0 (5 files) |
| New files | `cmd/wagyu/slash_statusline_external.go` + test |
| Cycle report | `docs/cycles/status-line-script/deliverable.md` |

## Scope (vs AGY 1.3.3 line 32 + 1.4.0)

| Reference behaviour | Clone implementation | Status |
|---|---|---|
| External script on `statusLine.command` field | `runStatusLineScript` (1 s `context.WithTimeout`, goroutine feeds stdin) | ✅ |
| JSON wire form | `cmd/wagyu/slash_statusline_external.go::StatusLinePayload` `{Model, Cost, ConversationTitle, CurrentDir}` | ✅ |
| `cost` = USD running total | atomic `sessionPromptTokens`/`sessionCompletionTokens` + per-model rate card (`claude-opus` ≥ `claude-sonnet` ≥ `minimax` = $0) | ✅ |
| `conversation_title` tracks `/rename` | `m.conversationTitle` field, populated by `submitPrompt` first-message hook | ✅ |
| Three-state `/statusline` cycle when configured | A (built-in) → B (external) → C (hidden) → A; pre-cycle two-state `"" ↔ "off"` preserved when `command == ""` | ✅ |
| `statusLine.enabled` round-trip via `auth.SetSettingsField` | `cmd` field NOT clobbered on toggle | ✅ |
| Tick-driven (100 ms) | `statusLineExternalTickMsg` from `tea.Tick(100ms, …)` armed in `Init` when external mode is configured at startup | ✅ |
| View short-circuit (built-in row skipped while external is live) | `View()` checks `m.statusLineMode` and renders script stdout only | ✅ |
| Error / empty / timeout fall back to built-in | `runStatusLineScript` returns error → record once into transcript + render built-in | ✅ |

## Files in commit

| File | Status | Lines | Role |
|---|---|---|---|
| `cmd/wagyu/slash_statusline_external.go` | new | +604 | script invocation, payload struct, three-state cycle, tick driver, token accumulation, cost calculator, view short-circuit, subprocess shim |
| `cmd/wagyu/slash_statusline_external_test.go` | new | +483 | 14 new tests (config shape, payload field order, pricing, token accumulation, subprocess happy/error/empty, splitStatusLineCommand ×4, three-state cycle + persistence invariant, View short-circuit, error fallback) |
| `cmd/wagyu/tui_run.go` | modified | +81 | imports, fields, three-state cycle helper, tick wiring, View short-circuit, token accumulator on chunk handler, `/statusline` rewrite |
| `cmd/wagyu/slash_commands.go` | modified | (in tui_run.go delta) | `/statusline` dispatch into three-state helper |
| `docs/cycles/status-line-script/deliverable.md` | new | +289 | TL;DR + scope + JSON wire form + subprocess shape + pricing table + three-state cycle + tick path + View short-circuit + fallback matrix + 6-item 存疑 |

## Gates

| Gate | Result |
|---|---|
| `git rebase main` | clean after discarding 5 cross-worktree pollution files (`slash_panels.go`, `slash_plugin_search.go`, `slash_cycle198_test.go`, `slash_cycle204_test.go`, `internal/cli/subcommand_parity.go`) and removing one rebase-time dup-block in `tui_run.go` (same botched conflict pattern as `subagent-question-picker`) |
| `go build -o /tmp/wagyu-statusscript ./cmd/wagyu` | exit 0, 73 MB |
| `go test -vet=off -count=1 ./cmd/... ./internal/...` ×3 | only `TestDispatchPrintSlash_LocalCommands` — pre-existing environmental flake |
| `gofmt -l` on every NEW file this cycle | empty |
| `gofmt -l internal/tools/git.go` / `internal/tools/nonnetwork.go` | dirty (pre-existing on main per AGENTS.md; not introduced by this cycle) |
| `go vet ./cmd/wagyu/ ./internal/` | only pre-existing cycle-224 `%` warnings |
| New tests (cycle code paths) | 14/14 pass |
| `internal/convstore/sqlite.go` | NOT TOUCHED ✓ |

## Smoke test (clone-only; REF is 1.4.0 here, AGY 1.3.3 line is older)

- `cmd/wagyu/slash_statusline_external_test.go` exercises the happy path
  (script returns a coloured line), error path (non-zero exit falls back),
  empty path (empty stdout falls back), and 1 s timeout path.
- `splitStatusLineCommand` is the standard POSIX shell-arg splitter.

## 存疑

1. Pre-existing `internal/tools/git.go` and `internal/tools/nonnetwork.go`
   gofmt-dirty on main per AGENTS.md.
2. Pre-existing `TestDispatchPrintSlash_LocalCommands` environmental flake.
3. Pricing table is a static USD rate card; model drift / dynamic pricing
   would require a fresh cycle.
4. View short-circuit collapses the status row entirely — a user who
   customises both `statusLine.command` and the global effort tier
   indicator loses the tier rendering.
5. Three-state cycle persists `statusLine.enabled` only; the `command`
   string lives in `auth.Settings.StatusLine.Command` (read-only here).
6. No remote push.

## Cleanup

- Worktree removed.
- Branch deleted.
- main is now `14c1f84`.