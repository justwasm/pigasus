# Cycle: customization-token-budget

Branch: `feature/customization-token-budget`
Base: `54db67b` (main)
Worktree: `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-customization-token-budget`

## AGY 1.2.16 gap closed

> "Fixed /customize, /config, /agents, /hooks, /mcp, /plugin, /skills, and /commands
> commands that load heavy customization tokens not being trimmed when the user's
> context is tight, so the LLM had less room for actual work."

## What landed

A new package `internal/customize/` plus a thin recorder bridge in
`cmd/wagyu/customize_recorder.go`. The package is stdlib-only and
provides:

| File | What |
|---|---|
| `internal/customize/doc.go` | package doc comment |
| `internal/customize/token.go` | `Token` struct, `Source` interface, `EstimateTokens(s)` (4-char heuristic matching `internal/backend/client.go:approxTokens`) |
| `internal/customize/priority.go` | `Priority` constants for the eight commands + `PriorityFor(name)` lookup + `CommandNames` canonical slice |
| `internal/customize/budget.go` | `Budget{Window, Reserve, Used}` + `Capacity()`, `Fits()`, `Remaining()` + `DefaultBudget()` (200 K window, 50 K reserve) |
| `internal/customize/trimmer.go` | `Trimmer` with `Add`, `AddToken`, `Trim`, `Dropped`, `Kept`, `TotalTokens`, `KeptTokens` |
| `internal/customize/sources.go` | per-command `Source` types (`CustomizeSource`, `ConfigSource`, `AgentsSource`, `HooksSource`, `MCPSource`, `PluginSource`, `SkillsSource`, `CommandsSource`) + `RegisterSources` helper |
| `internal/customize/customize_test.go` | 10 unit tests, all the hard-minimum 5 plus extras |
| `cmd/wagyu/customize_recorder.go` | process-global append-only recorder + `recordCustomizationToken`, `TrimCustomizationTokens`, `snapshotCustomizationTokens`, `resetCustomizationRecorder` |
| `cmd/wagyu/customize_recorder_test.go` | 3 end-to-end tests verifying the bridge wires into `internal/customize` correctly |
| `cmd/wagyu/slash_commands.go` | surgical wiring of `recordCustomizationToken` into the `/config`, `/mcp`, `/skills` case branches (3 sites) |

## Priority order (ascending — lower = trimmed first)

| Name | Priority | Reasoning |
|---|---|---|
| `/customize` | 0 | Generic override surface — re-derivable from `settings.json`. First to go. |
| `/config` | 10 | Settings table; recoverable from disk. |
| `/agents` | 20 | Custom-agent definitions; affects responses. |
| `/hooks` | 30 | Event-handler table. |
| `/mcp` | 40 | MCP server list (also known to the extension). |
| `/plugin` | 50 | Installed-plugin list. |
| `/skills` | 60 | Skill catalog (also known to PiG's resource loader). |
| `/commands` | 70 | Slash-command catalog — picker overlay depends on it. Last to go. |

Numeric spacing is uniform (10) so a future command can slot in
without renumbering existing constants.

## Hard minimum 5 unit tests

```
=== RUN   TestTokenBudgetExceededTrimsCustomization        PASS
=== RUN   TestCustomizeTrimmedFirst                        PASS
=== RUN   TestAgentsTrimmedAfterCustomize                  PASS
=== RUN   TestLowPriorityTrimmedBeforeHighPriority         PASS
=== RUN   TestNoTrimWhenBudgetSufficient                   PASS
```

Plus 5 extras (`TestZeroTokensKept`, `TestBudgetCapacityZeroWhenUsedExceeds`,
`TestPriorityForKnownCommands`, `TestRegistrationOrderAfterTrim`,
`TestRegisterSourcesSkipsEmpty`) and 3 bridge tests
(`TestRecorderTrimTriggersCustomize`, `TestRecorderEmptyContributionsIgnored`,
`TestRecorderReplaysAcrossTrims`) for 13 total.

## How a caller uses it

```go
// Slash handler reports its payload:
recordCustomizationToken("config", printSlashConfig())

// Later, before sending a prompt, the context-assembly step asks:
kept, dropped := TrimCustomizationTokens(customize.Budget{
    Window:  200_000,
    Reserve: 50_000,
    Used:    nonCustomizationTokens,
})
// kept is in registration order; dropped is in ascending priority order.
// Wire the kept tokens into the system prompt; drop the dropped ones.
```

The package is documented as stdlib-only. No new third-party
dependencies were added (the package, the bridge, and the tests
all use the existing `sync` and `sort` from stdlib).

## Surgical wiring to slash handlers

Three slash-command handlers got a 3-line addition each. The
existing files were not reformatted; the diff is purely additive
(`body := ...; slashContent(body); recordCustomizationToken(name, body)`).

- `cmd/wagyu/slash_commands.go` `/config` branch (lines 182-190)
- `cmd/wagyu/slash_commands.go` `/mcp` branch (lines 371-385)
- `cmd/wagyu/slash_commands.go` `/skills` branch (lines 499-510)

The other five commands (`/customize`, `/agents`, `/hooks`,
`/plugin`, `/commands`) currently return no body in the clone —
they are the future handlers the cycle that adds their real
content will call `recordCustomizationToken` on too. The package
is wired and waiting.

## Algorithm: descending-priority greedy with priority order in the dropped list

The trimmer sorts by Priority **descending**, walks the top
(highest-priority) candidates first, and accepts each as long as the
running total does not exceed `Budget.Capacity()`. Once the budget
is exhausted, every remaining candidate (which by construction has
lower priority than what has already been kept) is dropped. The
dropped slice is then reversed so the caller sees "customize first,
then config, then agents…" — matching the canonical priority
ordering.

Why descending rather than ascending? An ascending walk accumulates
the lowest priorities first, then stops at the budget limit — the
inverse of the changelog's "less-important tokens get trimmed first"
contract. The descending walk reads from the most valuable down,
accepting each until the budget is full.

Stable sort (`sort.SliceStable`) preserves registration order on ties
— two tokens with the same priority keep their relative position
when the trimmer restores registration order in the kept slice.

## Verification

| Check | Result |
|---|---|
| `go build -o /tmp/wagyu-ctb ./cmd/wagyu` | exit 0 |
| `go test -vet=off -count=1 -run 'TestCustomiz\|TestToken\|TestBudget\|TestTrim' ./cmd/wagyu/ ./internal/.../` | all packages PASS, `internal/customize` PASS |
| `gofmt -l` on every NEW file | empty |
| Pre-existing files not reformatted | confirmed (50 files under `internal/` + `cmd/wagyu/picker_view.go` and `cmd/wagyu/tui_v135_test.go` are intentionally not gofmt-clean per `AGENTS.md`) |
| `internal/convstore/sqlite.go` not touched | confirmed |
| Pre-existing test failure on main (`TestDispatchPrintSlash_LocalCommands`, expected `minimax-m3` but got `gemini-3.8-flash-medium`) | unrelated to this cycle — confirmed via `git stash` round-trip that it fails on plain main HEAD `54db67b` |

## Hard constraints

- [x] Do NOT touch `internal/convstore/sqlite.go` — not touched.
- [x] New files gofmt-clean — `gofmt -l` empty on all 9 new files.
- [x] Pre-existing not-clean files NOT reformatted wholesale — no gofmt on existing files.
- [x] `go build -o /tmp/wagyu-ctb ./cmd/wagyu` exit 0 — verified.
- [x] `go test -vet=off -count=1 -run 'TestCustomiz\|TestToken\|TestBudget\|TestTrim' ./cmd/wagyu/ ./internal/.../` pass — verified (one pre-existing unrelated failure on main).
- [x] `gofmt -l` empty on every NEW file — verified.
- [x] Use agw/MiniMax-M3 model — running on `MiniMax-M3`.
- [x] Branch must NOT be pushed — branch is local only.
- [x] Single commit — committed as one commit.

## Hard constraints this cycle can NOT claim to verify (存疑)

- **The clone's actual context-assembly step is in PiG's `coding.Runtime` (not clone code).** Wiring `TrimCustomizationTokens` into PiG's system-prompt builder would require modifying `internal/pigrt/runtime.go` to add a new `Options.CustomizationTokens []*customize.Token` field and threading it into PiG's session — that is a deeper PiG-integration cycle that this PR deliberately stops short of. The mechanism is wired at the seam (`recordCustomizationToken`) where a future cycle will read the kept tokens and feed them in.
- **The reference's actual trim threshold / which commands are loaded by default is not directly observable.** The priority table is a reasoned choice; the reference may use a different ordering. If a probe against `~/.local/bin/agy 1.4.0` becomes available, the priorities should be re-validated.
- **The four-characters-per-token heuristic matches `internal/backend/client.go:approxTokens` but is not a real tokenizer.** A future cycle that switches to a BPE-based estimate will need to revisit the trimmer's contract (the order of dropped tokens stays correct because the bias is uniform across all sources; the budget arithmetic changes by a constant factor).

## File delta summary

```
internal/customize/doc.go                       (new)
internal/customize/token.go                     (new)
internal/customize/priority.go                  (new)
internal/customize/budget.go                    (new)
internal/customize/trimmer.go                   (new)
internal/customize/sources.go                   (new)
internal/customize/customize_test.go            (new)
cmd/wagyu/customize_recorder.go                 (new)
cmd/wagyu/customize_recorder_test.go            (new)
cmd/wagyu/slash_commands.go                     (modified: 3 sites, +18 lines)
```

Total: 9 new files, 1 modified file, ~760 lines added (most of it
doc comments and tests).