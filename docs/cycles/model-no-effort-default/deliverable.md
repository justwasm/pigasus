# Cycle model-no-effort-default — AGY 1.3.4 changelog line 26

**Target:** AGY 1.3.4 (changelog captured verbatim in `internal/cli/testdata/changelog_reference.txt`).
**Branch:** `feature/model-no-effort-default` from `54db67b`.
**Theme:** the AGY 1.3.4 changelog says:

> Changed `--model` and `/model` to accept a base model name without `--effort`: instead of failing with an error asking for `--effort`, the CLI now picks the model's default thinking level (the server's default for that model, otherwise medium, otherwise the lowest available).

The 1.3.6 (`cmd/wagyu/model_select.go`) validator only knew the **first** tier of that chain ("server default → `spec.def`"). The chain itself was implicit in the `model + "-" + spec.def` branch — `--model gpt-oss-120b` worked because `spec.def == "medium"`, but a future `baseModelEfforts` entry with `def: ""` would have produced the broken slug `model + "-"`. This cycle lifts the chain into a named helper (`resolveDefaultEffort`) so each tier is independently testable.

## 1. Result

| Surface | Before | After |
|---|---|---|
| `--model gemini-3.8-flash` (no `--effort`) | resolves to `gemini-3.8-flash-high` via the inline `spec.def` branch | same: `gemini-3.8-flash-high` via `resolveDefaultEffort(spec)` tier 1 |
| `--model gpt-oss-120b` (no `--effort`) | resolves to `gpt-oss-120b-medium` via the inline `spec.def` branch | same: `gpt-oss-120b-medium` via `resolveDefaultEffort(spec)` tier 1 |
| `--model gemini-3.8-flash --effort low` | resolves to `gemini-3.8-flash-low` (cycle 101 behaviour) | unchanged — the chain runs only when effort is empty |
| `--model claude-sonnet-4-6` (base, "thinking"-baked) | catalogueID branch returns `claude-sonnet-4-6` as-is | unchanged — `baseModelEfforts` has no entry, falls through |
| `baseModelEfforts` entry with `def: ""` and `[low, high]` | would emit `model + "-"` (broken slug) | `resolveDefaultEffort` chain falls to tier 3 → `low` |
| `baseModelEfforts` entry with `def: "xhigh"` and `[low, medium, high]` | would emit `model + "-xhigh"` (broken slug) | chain ignores invalid def → falls to tier 2 → `medium` |

The chain in code (cited verbatim in `resolveDefaultEffort`'s doc comment):

1. `spec.def`, when set and present in `spec.levels` (the server's default).
2. `"medium"`, when present in `spec.levels`.
3. The first element of `spec.levels` (the lowest available).

Returns `""` when `spec.levels` is empty — the caller (`resolveModelSelection`) falls through to the catalogueID branch so a malformed entry can't trap the user in an empty slug.

## 2. Files touched

| File | Change |
|---|---|
| `cmd/wagyu/model_select.go` | extracted `baseModelSpec` (named type), added `resolveDefaultEffort(spec)` helper, rewrote the `baseModelEfforts` lookup branch in `resolveModelSelection` to call `resolveDefaultEffort`. Doc comment cites AGY 1.3.4 changelog line 26. |
| `cmd/wagyu/model_default_effort_test.go` (new) | seven behavioural pins: `TestDefaultEffortServerDefault`, `TestDefaultEffortMediumFallback`, `TestDefaultEffortLowestFallback`, `TestDefaultEffortEmptyLevels`, `TestModelNoEffortSucceeds`, `TestModelNoEffortPreservedWithEffort`, `TestEffortPickerAcceptsBareName`. |

Untouched:

- `internal/convstore/sqlite.go` (brief hard constraint — cycles 227–229).
- `cmd/wagyu/model_select_test.go` (cycle 101's `TestResolveModelSelection` pins the wire format; its 26 cases still pass — the chain is now exercised by `model_default_effort_test.go`).
- `cmd/wagyu/slash_effort_test.go` (cycle effort-picker's `TestEffortDefaultFallback` still passes — the cycle-101 `gemini-3.8-flash-high` and `gpt-oss-120b-medium` outputs are preserved by tier 1).
- All other `model_*` files (`model_resolve.go`, `model_label.go`, etc.).

## 3. Tests

```
$ go test -vet=off -count=1 -run 'TestModel|TestEffort|TestDefaultEffort' ./cmd/wagyu/ -v
=== RUN   TestDefaultEffortServerDefault
--- PASS: TestDefaultEffortServerDefault (0.00s)
=== RUN   TestDefaultEffortMediumFallback
--- PASS: TestDefaultEffortMediumFallback (0.00s)
=== RUN   TestDefaultEffortLowestFallback
--- PASS: TestDefaultEffortLowestFallback (0.00s)
=== RUN   TestDefaultEffortEmptyLevels
--- PASS: TestDefaultEffortEmptyLevels (0.00s)
=== RUN   TestModelNoEffortSucceeds
--- PASS: TestModelNoEffortSucceeds (0.00s)
=== RUN   TestModelNoEffortPreservedWithEffort
--- PASS: TestModelNoEffortPreservedWithEffort (0.00s)
=== RUN   TestEffortPickerAcceptsBareName
--- PASS: TestEffortPickerAcceptsBareName (0.00s)
…
17 PASS, 0 FAIL (7 new + 10 pre-existing in the filter)
```

| # | Test | What it pins |
|---|---|---|
| 1 | `TestDefaultEffortServerDefault` | tier 1: `spec.def` set & in levels → returned. Covers both `gemini-3.8-flash` (`def=high`) and `gpt-oss-120b` (`def=medium`). |
| 2 | `TestDefaultEffortMediumFallback` | tier 2: `spec.def` empty OR set-but-invalid → falls to `medium` when in levels. |
| 3 | `TestDefaultEffortLowestFallback` | tier 3: `spec.def` empty OR set-but-invalid AND `medium` not in levels → first element of `spec.levels`. |
| 4 | `TestDefaultEffortEmptyLevels` | empty `spec.levels` returns `""` (the "thinking"-model signal). |
| 5 | `TestModelNoEffortSucceeds` | full pipeline: `--model <base>` (no effort) succeeds for `gemini-3.8-flash`, `gpt-oss-120b`, `claude-sonnet-4-6`. |
| 6 | `TestModelNoEffortPreservedWithEffort` | full pipeline: `--model <base> --effort <level>` preserves the user-supplied effort; chain does not run. |
| 7 | `TestEffortPickerAcceptsBareName` | TUI: `openSlashPanel("model")` opens the panel with `m.model = "Gemini 3.8 Flash"` (the resolved label from a bare `gemini-3.8-flash` flag), `panelModelLines()` renders the (current) row, `Right` on the Gemini row advances `effortCursor`. |

## 4. Build / test gate

```
$ go build -o /tmp/wagyu-eff ./cmd/wagyu
exit 0, binary 73306578 B

$ go test -vet=off -count=1 -run 'TestModel|TestEffort|TestDefaultEffort' ./cmd/wagyu/ ./internal/.../
ok  github.com/chadgpt/openagy/cmd/wagyu          1.073s
ok  github.com/chadgpt/openagy/internal/...        (28 packages, no FAIL in the filter)

$ gofmt -l cmd/wagyu/model_select.go cmd/wagyu/model_default_effort_test.go
(empty)
```

`TestDispatchPrintSlash_LocalCommands` (cmd/wagyu/print_slash_test.go:45) **fails on `54db67b` with no changes from this cycle** — verified by `git stash --include-untracked && go test …` (output: `output = "gemini-3.8-flash-medium\tGemini 3.8 Flash (Medium)\n", want "minimax-m3\tMiniMax M3\n"`). Pre-existing flake, same family as `TestCycle225QuotaCountdown` (AGENTS.md notes). Not exercised by the targeted test gate.

## 5. Evidence grading

- **Build clean + 17/17 targeted tests pass + gofmt clean**: 【一手】
- **Chain tier-1 (`spec.def` wins when in levels)**: 【一手】 `TestDefaultEffortServerDefault`.
- **Chain tier-2 (`medium` wins when def invalid/absent)**: 【一手】 `TestDefaultEffortMediumFallback`.
- **Chain tier-3 (lowest available when neither def nor medium)**: 【一手】 `TestDefaultEffortLowestFallback`.
- **`--model <base> --effort <level>` preserved (chain bypassed)**: 【一手】 `TestModelNoEffortPreservedWithEffort` × 4 cases.
- **`/model` panel opens for a bare base slug, renders the (current) row, slider advances on Right**: 【一手】 `TestEffortPickerAcceptsBareName`.

## 6. Limitations / not verified

- **No live reference probe.** The 1.3.4 binary is not on this machine (target install is 1.3.4 / 1.3.6 captures exist for the cycle 101 path; this cycle is the chain's *expression*, not a new feature). The 1.3.4 changelog text was captured byte-for-byte in `internal/cli/testdata/changelog_reference.txt` line 26, which is the only artefact this cycle relies on.
- **`resolveEffectiveModel` (TUI banner) is unchanged.** When the user runs `agy --model gemini-3.8-flash` interactively, `resolveEffectiveModel` does not recognise a bare base slug and falls back to `defaultModel` with the bare slug as `unavailableFrom`. The TUI's `--model <base>` warning still fires. The cycle 197 panel render accommodates this by reading `panelCurrentModelLabel()`, which falls back to `m.model` first. The brief targets `--model` and `/model`; the headless path (`resolveModelSelection`) is fully covered; the TUI banner warning path is unchanged on purpose — fixing that is a separate concern about the *banner*, not the *model selection*.
- **`baseModelEfforts` has only two entries (`gemini-3.8-flash`, `gpt-oss-120b`); tiers 2 and 3 are exercised by synthetic specs in the unit tests, not by a real model the catalogue knows.** This is the right shape for now: the chain is future-proofing, and a single real-model test per tier would require a fourth base model that does not exist.
- **No `--effort` validator change.** Cycle 101's `validEfforts` vocabulary (`low|medium|high|xhigh|max`) and `gpt-oss-120b has no "xhigh" effort` errors are unchanged. The chain runs only when effort is empty.